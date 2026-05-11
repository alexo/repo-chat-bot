package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	anthropicURL     = "https://api.anthropic.com/v1/messages"
	anthropicVersion = "2023-06-01"
	maxToolRounds    = 8
	maxTokens        = 4096
)

const systemPrompt = `You are an assistant that answers questions using the contents of a git repository as your knowledge base.

Use the tools to discover what's in the repo (list_files), read specific files (read_file), or search across files (grep). Ground every claim in the actual file contents — quote or cite paths when it helps. If a question can't be answered from the repo, say so plainly rather than inventing.

Keep answers concise and chat-friendly — this is a Telegram conversation, not a report.`

// anthropicToolSchemas advertises the bot's tools in Anthropic's tool format.
// Other adapters define their tools in whatever shape their provider expects.
var anthropicToolSchemas = []map[string]any{
	{
		"name":        "list_files",
		"description": "List every file in the repository, relative to the repo root.",
		"input_schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		"name":        "read_file",
		"description": "Read the full text contents of a file in the repository.",
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path relative to the repo root, e.g. 'README.md' or 'docs/intro.md'",
				},
			},
			"required": []string{"path"},
		},
	},
	{
		"name":        "grep",
		"description": "Case-insensitive substring search across all repo files. Returns matching lines with file:line prefixes.",
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Literal substring to find (not a regex).",
				},
			},
			"required": []string{"pattern"},
		},
	},
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result fields
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type apiRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system"`
	Tools     []map[string]any `json:"tools,omitempty"`
	Messages  []message        `json:"messages"`
}

type apiResponse struct {
	StopReason string         `json:"stop_reason"`
	Content    []contentBlock `json:"content"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type AnthropicProvider struct {
	apiKey string
	model  string
	repo   *Repo
	http   *http.Client
}

func NewAnthropicProvider(apiKey, model string, repo *Repo) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		repo:   repo,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

// Ask runs a full tool-use loop and returns the final text reply along with
// the conversation history extended by the user turn and assistant reply.
func (p *AnthropicProvider) Ask(ctx context.Context, history []Turn, userText string) (string, []Turn, error) {
	messages := turnsToMessages(history)
	messages = append(messages, message{
		Role:    "user",
		Content: []contentBlock{{Type: "text", Text: userText}},
	})

	for round := 0; round < maxToolRounds; round++ {
		resp, err := p.call(ctx, messages)
		if err != nil {
			return "", nil, err
		}

		messages = append(messages, message{Role: "assistant", Content: resp.Content})

		if resp.StopReason != "tool_use" {
			reply := collectText(resp.Content)
			newHistory := append(history,
				Turn{Role: "user", Text: userText},
				Turn{Role: "assistant", Text: reply},
			)
			return reply, newHistory, nil
		}

		var toolResults []contentBlock
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			result := dispatch(p.repo, block.Name, block.Input)
			toolResults = append(toolResults, contentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   result,
			})
		}
		messages = append(messages, message{Role: "user", Content: toolResults})
	}

	return "", nil, fmt.Errorf("exceeded %d tool rounds without final answer", maxToolRounds)
}

func turnsToMessages(history []Turn) []message {
	out := make([]message, 0, len(history))
	for _, t := range history {
		out = append(out, message{
			Role:    t.Role,
			Content: []contentBlock{{Type: "text", Text: t.Text}},
		})
	}
	return out
}

func (p *AnthropicProvider) call(ctx context.Context, messages []message) (*apiResponse, error) {
	body, err := json.Marshal(apiRequest{
		Model:     p.model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Tools:     anthropicToolSchemas,
		Messages:  messages,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed apiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(raw))
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("anthropic %s: %s", parsed.Error.Type, parsed.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic http %d: %s", resp.StatusCode, string(raw))
	}
	return &parsed, nil
}

func collectText(blocks []contentBlock) string {
	var buf bytes.Buffer
	for _, b := range blocks {
		if b.Type == "text" {
			buf.WriteString(b.Text)
		}
	}
	return buf.String()
}

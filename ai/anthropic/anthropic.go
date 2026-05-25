// Package anthropic implements ai.Provider against the Anthropic Messages API.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/alexo/repo-chat-bot/ai"
)

const (
	endpoint = "https://api.anthropic.com/v1/messages"
	version  = "2023-06-01"
)

// toolSchemas advertises the bot's tools in Anthropic's native shape.
var toolSchemas = []map[string]any{
	{
		"name":        ai.ToolListFiles,
		"description": "List every file in the repository, relative to the repo root.",
		"input_schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		"name":        ai.ToolReadFile,
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
		"name":        ai.ToolGrep,
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

type Provider struct {
	apiKey string
	model  string
	debug  bool
	kb     ai.KnowledgeBase
	http   *http.Client
}

func New(apiKey, model string, debug bool, kb ai.KnowledgeBase) *Provider {
	return &Provider{
		apiKey: apiKey,
		model:  model,
		debug:  debug,
		kb:     kb,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *Provider) debugf(format string, args ...any) {
	if p.debug {
		log.Printf("llm[anthropic]: "+format, args...)
	}
}

// Ask runs a full tool-use loop and returns the final text reply along with
// the conversation history extended by the user turn and assistant reply.
func (p *Provider) Ask(ctx context.Context, history []ai.Turn, userText string) (string, []ai.Turn, error) {
	p.debugf("ask start: history=%d user_text_len=%d model=%s", len(history), len(userText), p.model)
	messages := turnsToMessages(history)
	messages = append(messages, message{
		Role:    "user",
		Content: []contentBlock{{Type: "text", Text: userText}},
	})

	for round := 0; round < ai.MaxToolRounds; round++ {
		p.debugf("round=%d send messages=%d", round+1, len(messages))
		resp, err := p.call(ctx, messages)
		if err != nil {
			return "", nil, err
		}

		messages = append(messages, message{Role: "assistant", Content: resp.Content})
		p.debugf("round=%d stop_reason=%s content_blocks=%d", round+1, resp.StopReason, len(resp.Content))

		if resp.StopReason != "tool_use" {
			reply := collectText(resp.Content)
			p.debugf("ask complete: reply_len=%d", len(reply))
			newHistory := append(history,
				ai.Turn{Role: "user", Text: userText},
				ai.Turn{Role: "assistant", Text: reply},
			)
			return reply, newHistory, nil
		}

		var toolResults []contentBlock
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			p.debugf("tool call: name=%s id=%s", block.Name, block.ID)
			result := ai.Dispatch(p.kb, block.Name, block.Input)
			p.debugf("tool result: name=%s result_len=%d", block.Name, len(result))
			toolResults = append(toolResults, contentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   result,
			})
		}
		messages = append(messages, message{Role: "user", Content: toolResults})
	}

	return "", nil, fmt.Errorf("exceeded %d tool rounds without final answer", ai.MaxToolRounds)
}

func turnsToMessages(history []ai.Turn) []message {
	out := make([]message, 0, len(history))
	for _, t := range history {
		out = append(out, message{
			Role:    t.Role,
			Content: []contentBlock{{Type: "text", Text: t.Text}},
		})
	}
	return out
}

func (p *Provider) call(ctx context.Context, messages []message) (*apiResponse, error) {
	body, err := json.Marshal(apiRequest{
		Model:     p.model,
		MaxTokens: ai.MaxTokens,
		System:    ai.SystemPrompt,
		Tools:     toolSchemas,
		Messages:  messages,
	})
	if err != nil {
		return nil, err
	}
	p.debugf("http request: body_bytes=%d", len(body))

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", version)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	p.debugf("http response: status=%d body_bytes=%d", resp.StatusCode, len(raw))

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

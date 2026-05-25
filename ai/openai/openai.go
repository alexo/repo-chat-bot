// Package openai implements ai.Provider against the OpenAI chat-completions
// wire format. The same client targets any compatible endpoint — GitHub
// Models, OpenRouter, vLLM, etc. — by varying baseURL.
package openai

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
	githubURL     = "https://models.inference.ai.azure.com/chat/completions"
	openRouterURL = "https://openrouter.ai/api/v1/chat/completions"
)

// toolSchemas advertises the bot's tools in OpenAI's native shape.
var toolSchemas = []map[string]any{
	{
		"type": "function",
		"function": map[string]any{
			"name":        ai.ToolListFiles,
			"description": "List every file in the repository, relative to the repo root.",
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	},
	{
		"type": "function",
		"function": map[string]any{
			"name":        ai.ToolReadFile,
			"description": "Read the full text contents of a file in the repository.",
			"parameters": map[string]any{
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
	},
	{
		"type": "function",
		"function": map[string]any{
			"name":        ai.ToolGrep,
			"description": "Case-insensitive substring search across all repo files. Returns matching lines with file:line prefixes.",
			"parameters": map[string]any{
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
	},
}

type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	ToolID    string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type request struct {
	Model    string           `json:"model"`
	Messages []message        `json:"messages"`
	Tools    []map[string]any `json:"tools,omitempty"`
}

type response struct {
	Choices []struct {
		Message      message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Provider speaks OpenAI's chat-completions protocol against any compatible
// endpoint. `label` is used only in error messages so callers can tell GitHub
// Models failures from OpenRouter failures at a glance.
type Provider struct {
	label   string
	baseURL string
	apiKey  string
	model   string
	debug   bool
	kb      ai.KnowledgeBase
	http    *http.Client
}

// New constructs a Provider pointing at any OpenAI-compatible endpoint.
func New(label, baseURL, apiKey, model string, debug bool, kb ai.KnowledgeBase) *Provider {
	return &Provider{
		label:   label,
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		debug:   debug,
		kb:      kb,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *Provider) debugf(format string, args ...any) {
	if p.debug {
		log.Printf("llm[%s]: "+format, append([]any{p.label}, args...)...)
	}
}

// NewGitHubProvider targets GitHub Models with the supplied PAT.
func NewGitHubProvider(apiKey, model string, debug bool, kb ai.KnowledgeBase) *Provider {
	return New("github", githubURL, apiKey, model, debug, kb)
}

// NewOpenRouterProvider targets OpenRouter with the supplied API key.
func NewOpenRouterProvider(apiKey, model string, debug bool, kb ai.KnowledgeBase) *Provider {
	return New("openrouter", openRouterURL, apiKey, model, debug, kb)
}

func (p *Provider) Ask(ctx context.Context, history []ai.Turn, userText string) (string, []ai.Turn, error) {
	p.debugf("ask start: history=%d user_text_len=%d model=%s", len(history), len(userText), p.model)
	messages := []message{{Role: "system", Content: ai.SystemPrompt}}
	for _, t := range history {
		messages = append(messages, message{Role: t.Role, Content: t.Text})
	}
	messages = append(messages, message{Role: "user", Content: userText})

	for round := 0; round < ai.MaxToolRounds; round++ {
		p.debugf("round=%d send messages=%d", round+1, len(messages))
		resp, err := p.call(ctx, messages)
		if err != nil {
			return "", nil, err
		}

		choice := resp.Choices[0]
		messages = append(messages, choice.Message)
		p.debugf("round=%d finish_reason=%s tool_calls=%d", round+1, choice.FinishReason, len(choice.Message.ToolCalls))

		if choice.FinishReason != "tool_calls" {
			reply := choice.Message.Content
			p.debugf("ask complete: reply_len=%d", len(reply))
			newHistory := append(history,
				ai.Turn{Role: "user", Text: userText},
				ai.Turn{Role: "assistant", Text: reply},
			)
			return reply, newHistory, nil
		}

		for _, tc := range choice.Message.ToolCalls {
			p.debugf("tool call: name=%s id=%s", tc.Function.Name, tc.ID)
			result := ai.Dispatch(p.kb, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			p.debugf("tool result: name=%s result_len=%d", tc.Function.Name, len(result))
			messages = append(messages, message{
				Role:    "tool",
				ToolID:  tc.ID,
				Content: result,
			})
		}
	}

	return "", nil, fmt.Errorf("%s: exceeded %d tool rounds", p.label, ai.MaxToolRounds)
}

func (p *Provider) call(ctx context.Context, messages []message) (*response, error) {
	body, err := json.Marshal(request{
		Model:    p.model,
		Messages: messages,
		Tools:    toolSchemas,
	})
	if err != nil {
		return nil, err
	}
	p.debugf("http request: body_bytes=%d", len(body))

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		p.debugf("http response: status=%d body_bytes=%d", resp.StatusCode, len(raw))
		return nil, fmt.Errorf("%s http %d: %s", p.label, resp.StatusCode, string(raw))
	}

	var parsed response
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	p.debugf("http response: status=%d choices=%d", resp.StatusCode, len(parsed.Choices))
	if parsed.Error != nil {
		return nil, fmt.Errorf("%s error: %s", p.label, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("%s returned no choices", p.label)
	}
	return &parsed, nil
}

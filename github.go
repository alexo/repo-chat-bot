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
	githubURL = "https://models.inference.ai.azure.com/chat/completions"
)

// githubToolSchemas defines the tools in OpenAI's format, which GitHub Models uses.
var githubToolSchemas = []map[string]any{
	{
		"type": "function",
		"function": map[string]any{
			"name":        "list_files",
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
			"name":        "read_file",
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
			"name":        "grep",
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

type githubMessage struct {
	Role      string               `json:"role"`
	Content   string               `json:"content,omitempty"`
	ToolCalls []githubToolCall     `json:"tool_calls,omitempty"`
	ToolID    string               `json:"tool_call_id,omitempty"`
}

type githubToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type githubRequest struct {
	Model    string          `json:"model"`
	Messages []githubMessage `json:"messages"`
	Tools    []map[string]any `json:"tools,omitempty"`
}

type githubResponse struct {
	Choices []struct {
		Message      githubMessage `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type GitHubProvider struct {
	apiKey string
	model  string
	repo   *Repo
	http   *http.Client
}

func NewGitHubProvider(apiKey, model string, repo *Repo) *GitHubProvider {
	return &GitHubProvider{
		apiKey: apiKey,
		model:  model,
		repo:   repo,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *GitHubProvider) Ask(ctx context.Context, history []Turn, userText string) (string, []Turn, error) {
	messages := []githubMessage{
		{Role: "system", Content: systemPrompt},
	}
	for _, t := range history {
		messages = append(messages, githubMessage{Role: t.Role, Content: t.Text})
	}
	messages = append(messages, githubMessage{Role: "user", Content: userText})

	for round := 0; round < maxToolRounds; round++ {
		resp, err := p.call(ctx, messages)
		if err != nil {
			return "", nil, err
		}

		choice := resp.Choices[0]
		messages = append(messages, choice.Message)

		if choice.FinishReason != "tool_calls" {
			reply := choice.Message.Content
			newHistory := append(history,
				Turn{Role: "user", Text: userText},
				Turn{Role: "assistant", Text: reply},
			)
			return reply, newHistory, nil
		}

		for _, tc := range choice.Message.ToolCalls {
			result := dispatch(p.repo, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			messages = append(messages, githubMessage{
				Role:    "tool",
				ToolID:  tc.ID,
				Content: result,
			})
		}
	}

	return "", nil, fmt.Errorf("exceeded %d tool rounds", maxToolRounds)
}

func (p *GitHubProvider) call(ctx context.Context, messages []githubMessage) (*githubResponse, error) {
	body, err := json.Marshal(githubRequest{
		Model:    p.model,
		Messages: messages,
		Tools:    githubToolSchemas,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", githubURL, bytes.NewReader(body))
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
		return nil, fmt.Errorf("github http %d: %s", resp.StatusCode, string(raw))
	}

	var parsed githubResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("github error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("github returned no choices")
	}
	return &parsed, nil
}


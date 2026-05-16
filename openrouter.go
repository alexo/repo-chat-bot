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

const openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

type OpenRouterProvider struct {
	apiKey string
	model  string
	repo   *Repo
	http   *http.Client
}

func NewOpenRouterProvider(apiKey, model string, repo *Repo) *OpenRouterProvider {
	return &OpenRouterProvider{
		apiKey: apiKey,
		model:  model,
		repo:   repo,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenRouterProvider) Ask(ctx context.Context, history []Turn, userText string) (string, []Turn, error) {
	messages := []githubMessage{{Role: "system", Content: systemPrompt}}
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

func (p *OpenRouterProvider) call(ctx context.Context, messages []githubMessage) (*githubResponse, error) {
	body, err := json.Marshal(githubRequest{
		Model:    p.model,
		Messages: messages,
		Tools:    githubToolSchemas,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openRouterURL, bytes.NewReader(body))
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
		return nil, fmt.Errorf("openrouter http %d: %s", resp.StatusCode, string(raw))
	}

	var parsed githubResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("openrouter error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openrouter returned no choices")
	}
	return &parsed, nil
}


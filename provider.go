package main

import (
	"context"
	"fmt"
)

// Turn is a single user-visible exchange in the conversation. Intermediate
// tool calls a provider makes inside an Ask are not persisted across turns —
// each Ask runs its own tool loop against the current history.
type Turn struct {
	Role string // "user" or "assistant"
	Text string
}

// LLMProvider abstracts over any chat model the bot can talk to. Adapters
// translate Turns into their native format, execute whatever tool-use loop
// they need, and return the final assistant text plus appended history.
type LLMProvider interface {
	Ask(ctx context.Context, history []Turn, userText string) (reply string, newHistory []Turn, err error)
}

func NewProvider(name, apiKey, model string, repo *Repo) (LLMProvider, error) {
	switch name {
	case "anthropic", "":
		return NewAnthropicProvider(apiKey, model, repo), nil
	case "github":
		return NewGitHubProvider(apiKey, model, repo), nil
	case "openrouter":
		return NewOpenRouterProvider(apiKey, model, repo), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q", name)
	}
}

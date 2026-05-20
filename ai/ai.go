// Package ai defines the chat-with-tool-use abstraction used by the bot.
// Concrete LLM backends live in subpackages (ai/anthropic, ai/openai).
package ai

import "context"

// Turn is a single user-visible exchange in the conversation. Intermediate
// tool calls a provider makes inside an Ask are not persisted across turns —
// each Ask runs its own tool loop against the current history.
type Turn struct {
	Role string // "user" or "assistant"
	Text string
}

// Provider abstracts over any chat model the bot can talk to. Adapters
// translate Turns into their native format, execute whatever tool-use loop
// they need, and return the final assistant text plus appended history.
type Provider interface {
	Ask(ctx context.Context, history []Turn, userText string) (reply string, newHistory []Turn, err error)
}

// SystemPrompt is the persona the bot adopts in every conversation. Kept here
// (not per-provider) so all backends share the same behavior.
const SystemPrompt = `You are an assistant that answers questions using the contents of a git repository as your knowledge base.

Use the tools to discover what's in the repo (list_files), read specific files (read_file), or search across files (grep). Ground every claim in the actual file contents — quote or cite paths when it helps. If a question can't be answered from the repo, say so plainly rather than inventing.

Keep answers concise and chat-friendly — this is a Telegram conversation, not a report.`

// MaxToolRounds caps how many tool-call cycles a single Ask may run before
// giving up. Generous enough for "list, read, grep, answer" without runaways.
const MaxToolRounds = 8

// MaxTokens is the response length cap passed to providers that expect one.
const MaxTokens = 4096

# Architecture

This document describes the architecture of `repo-chat-bot`: a chat assistant that answers questions using files from a local repository-like knowledge base.

## 1. High-Level Overview

`repo-chat-bot` is a long-running Go service with:

- Chat adapters:
  - Telegram (long polling)
  - Slack (Socket Mode)
- LLM provider abstraction:
  - Anthropic
  - GitHub Models
  - OpenRouter
- Local tool layer over a filesystem root (`REPO_PATH`):
  - `list_files`
  - `read_file`
  - `grep`

The bot receives a user question, runs an LLM tool-calling loop, executes tool calls against the local knowledge base, then returns a grounded answer back to chat.

## 2. Core Components

## 2.1 `main.go` (application bootstrap)

Responsibilities:

- Loads environment from `.env` (via `godotenv`) and validates config.
- Initializes `Repo` using `REPO_PATH`.
- Creates an `LLMProvider` based on `LLM_PROVIDER`.
- Starts Telegram and Slack adapters.
- Keeps process alive until shutdown signal.

Notable behavior:

- Telegram startup is resilient: Telegram failures do not crash Slack operation.
- Logs effective `REPO_PATH`, selected LLM provider, and selected model on startup.

## 2.2 `config.go` (configuration)

`LoadConfig()` reads and validates:

- `TELEGRAM_BOT_TOKEN`
- `SLACK_APP_TOKEN`
- `SLACK_BOT_TOKEN`
- `SLACK_DEBUG`
- `LLM_PROVIDER`
- `LLM_API_KEY`
- `LLM_MODEL`
- `REPO_PATH`
- `ALLOWED_USER_IDS`

Default provider/model logic:

- `anthropic` -> `claude-sonnet-4-6`
- `github` -> `gpt-4o`
- `openrouter` -> `openai/gpt-4o-mini`

## 2.3 `provider.go` (provider abstraction)

Defines:

- `Turn` conversation unit (`role`, `text`)
- `LLMProvider` interface:
  - `Ask(ctx, history, userText) (reply, newHistory, err)`

Factory:

- `NewProvider(...)` selects one adapter implementation by provider name.

## 2.4 LLM adapters

### Anthropic adapter (`claude.go`)

- Uses Anthropic Messages API (`/v1/messages`).
- Sends tool schema in Anthropic format.
- Executes a bounded tool loop (`maxToolRounds`).

### GitHub adapter (`github.go`)

- Uses OpenAI-compatible chat completions endpoint used by GitHub Models.
- Sends tools in OpenAI function-calling format.
- Executes tool calls and feeds tool results back.

### OpenRouter adapter (`openrouter.go`)

- Uses OpenRouter chat completions endpoint.
- Reuses OpenAI-compatible request/response shape and tool loop pattern.

## 2.5 `repo.go` (knowledge base access)

This layer exposes a safe, local filesystem API:

- `ListFiles()` recursively lists files under `REPO_PATH`
- `ReadFile(relPath)` reads a single file
- `Grep(pattern)` searches text lines across files

Security/safety:

- Prevents path escape outside root via normalization and relative checks.
- Skips certain directories (`.git`, `node_modules`, hidden dirs).

## 2.6 `tools.go` (tool dispatch)

Maps provider-requested tool names and inputs to concrete `Repo` calls.

Typical mapping:

- `list_files` -> `Repo.ListFiles()`
- `read_file` -> `Repo.ReadFile(path)`
- `grep` -> `Repo.Grep(pattern)`

## 2.7 Chat adapters

### Telegram flow (`main.go`)

- Receives message update.
- Applies `ALLOWED_USER_IDS` authorization.
- Supports `/reset` to clear per-chat history.
- Calls `LLMProvider.Ask(...)` and sends response in chunks.

### Slack flow (`slack.go`)

- Uses Socket Mode event stream.
- Handles:
  - `app_mention`
  - thread messages in tracked threads
- Supports `/reset` in Slack context.
- Converts common Markdown to Slack-friendly formatting before posting.
- Optional debug logging via `SLACK_DEBUG`.

Thread behavior:

- First mention in a thread tracks that thread key.
- Subsequent user messages in the same thread can be answered without mention.

## 3. Runtime Data Flow

1. User sends message (Telegram or Slack).
2. Adapter resolves chat/thread context and history.
3. Adapter calls `LLMProvider.Ask(...)`.
4. Provider sends prompt/history/tools to selected LLM endpoint.
5. If LLM requests tool(s), provider dispatches tool calls via `dispatch(...)`.
6. Tool results are fed back to the model.
7. Final assistant text returned to adapter.
8. Adapter posts response and stores updated conversation history.

## 3.1 LLM Interaction Flow (Detailed)

Each provider adapter (`ai/anthropic`, `ai/openai`) follows the same logical pattern:

1. Build the initial request context:
   - system prompt (`ai.SystemPrompt`)
   - prior conversation turns (`history`)
   - current user message (`userText`)
   - tool schema definitions (`list_files`, `read_file`, `grep`)
2. Send request to the LLM endpoint.
3. If the model responds with tool calls:
   - execute each tool locally against the knowledge base
   - append tool results as messages
   - send another request with the expanded context
4. Repeat until model returns final natural-language output or max rounds is reached.

This is intentionally provider-neutral: Anthropic and OpenAI-compatible backends use different wire formats, but identical loop semantics.

## 3.2 How Context Is Used

Context is cumulative within a single `Ask(...)` call.

The model sees, in order:

- System instructions (behavior and grounding rules)
- Prior persisted chat turns for that conversation
- Current user question
- Intermediate assistant tool-call messages
- Tool results returned by the app

Important:

- The full knowledge base is **not** sent up front.
- The model pulls data on demand via tools.
- Only files/lines surfaced by tool calls become part of request context.

## 3.3 Token Usage Impact

Token usage is driven by repeated request expansion during the tool loop.

Main token cost drivers:

- Size of existing conversation history
- Number of tool rounds (`ai.MaxToolRounds`)
- Size of tool outputs (full file reads can be large)
- Length of final response
- Extra provider-side formatting overhead

Practical implications:

- More rounds -> more cumulative prompt tokens in subsequent calls.
- Large `read_file` output can dominate cost and latency.
- Re-asking similar questions in long chats can become progressively expensive.

## 3.4 Token Optimization Strategies

Current design already helps by avoiding full-repo prompt stuffing. Additional tuning options:

- Keep responses concise in prompt policy.
- Encourage targeted tool use (`grep` before broad `read_file`).
- Trim or summarize long conversation history (or add rolling window).
- Add file-size guards/chunked reads for very large files.
- Limit max tool rounds for cost-sensitive environments.
- Use cheaper/faster default models for high-volume channels.

## 3.5 Observability for LLM Flow

With `LLM_DEBUG=true`, providers emit optional diagnostics such as:

- round number and message count
- finish/stop reason
- tool call names and result lengths
- HTTP status and payload-size hints

This helps correlate latency/cost with concrete tool-loop behavior.

## 4. State Model

In-memory state only:

- Per-chat history stored in process memory (`sync.Map`).
- Slack tracked-thread map stored in process memory.

Implication:

- Restart resets chat history and tracked thread metadata.

## 5. Deployment Model

Containerized app (`Dockerfile`) with runtime env configuration.

Key deployment requirement:

- Knowledge base must exist at `REPO_PATH` inside runtime environment (usually mounted volume).

Common patterns:

- Fly.io/Railway volume mounted at `/app/repo`
- Startup/manual `git clone` into mounted path
- Periodic refresh (`git pull` or sync pipeline)

## 6. Design Trade-offs

Strengths:

- Simple architecture, easy to operate.
- Provider-agnostic LLM abstraction.
- Grounded answers from explicit tools over local files.

Current limits:

- In-memory history only (non-persistent).
- File/grep retrieval can degrade on very large corpora.
- No built-in scheduler for automatic KB refresh.

## 7. Suggested Evolution Path

- Persist conversation state (Redis).
- Add observability (`slog`, metrics, health endpoint).
- Add configurable KB sync/update mechanism.
- Introduce optional indexing/vector retrieval for very large multi-repo knowledge bases.

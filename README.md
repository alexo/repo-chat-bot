# repo-chat-bot

Proof-of-concept Telegram bot that uses a local **git repository as a knowledge base**. Ask questions in chat; the bot reads files, greps content, and answers using Claude. Written in Go, ships as a single static binary, deployable to Fly.io or Railway.

Point it at any repo — docs, notes, code, datasets — and it becomes a chat interface for that content.

## Architecture

```
Telegram ──(long-poll)──▶ main.go
                            │
                            ├─ allowlist check (ALLOWED_USER_IDS)
                            ├─ per-chat in-memory conversation history
                            └─ claude.go ──HTTPS──▶ api.anthropic.com
                                  │
                                  └─ tool loop: list_files / read_file / grep
                                            ▼
                                        repo.go (sandboxed reads under REPO_PATH)
```

The bot talks to the Anthropic Messages API directly over HTTPS — no Go SDK dependency to chase. Tool use is a small loop: Claude requests a tool, Go runs it locally against the repo, the result is fed back, repeat until Claude is done.

## Files

| File         | Purpose                                                       |
| ------------ | ------------------------------------------------------------- |
| `main.go`    | Telegram polling, message handling, allowlist, chunking       |
| `claude.go`  | Anthropic Messages API client + tool-use loop                 |
| `tools.go`   | Tool schemas advertised to Claude and the dispatcher          |
| `repo.go`    | Path-safe file reads scoped to `REPO_PATH`                    |
| `config.go`  | Env var loading and validation                                |
| `Dockerfile` | Multi-stage build → tiny alpine image                         |
| `fly.toml`   | Fly.io app config with a persistent volume for the repo       |

## Local run

```bash
cp .env.example .env
# fill in TELEGRAM_BOT_TOKEN, ANTHROPIC_API_KEY, REPO_PATH, ALLOWED_USER_IDS
export $(grep -v '^#' .env | xargs)
go mod tidy
go run .
```

Get your Telegram user ID from `@userinfobot`. Create the bot via `@BotFather`. `REPO_PATH` is the absolute path to any git checkout on your machine — the bot only reads inside that root.

## Deploy: Fly.io

```bash
fly launch --no-deploy           # accept the existing fly.toml
fly volumes create repo_data --region fra --size 1
fly secrets set \
  TELEGRAM_BOT_TOKEN=... \
  ANTHROPIC_API_KEY=... \
  ALLOWED_USER_IDS=123456789
fly deploy
```

The target repo needs to be on the volume at `/app/repo`. Simplest options:

1. `fly ssh console` → `git clone <repo-url> /app/repo` once. Add a periodic `git pull` later if you want fresh data.
2. Bake a deploy key into the image and have an entrypoint script clone on first boot.

## Deploy: Railway

1. New project → Deploy from Repo → point at this directory.
2. Railway auto-detects the Dockerfile.
3. Add the same env vars under the service's Variables tab.
4. For repo data: attach a Railway volume mounted at `/app/repo` and SSH in to clone, or bake the files into the image at build time if they're public/non-sensitive.

## How the tool loop works

Each user message kicks off a loop:

1. POST the conversation history to `/v1/messages` with the three tools declared.
2. If `stop_reason == "tool_use"`, dispatch each tool call locally, append a `tool_result` user message, and loop.
3. Otherwise, extract the text blocks and send them back to Telegram.

Capped at 8 tool rounds per question — enough for "list the repo, read the relevant file, grep for context" without runaways.

## Tuning for your repo

The system prompt in `claude.go` is generic ("answer questions using the contents of a git repository"). If you're pointing this at a specific kind of repo (codebase, docs site, personal notes), customize the prompt — telling Claude *what kind of repo this is* and *which files matter most* dramatically improves answers and reduces wasted tool calls.

## Hardening for production

- Swap in-memory `sync.Map` history for Redis (Upstash, Railway Redis). Persist last ~20 turns per chat.
- Add a `git pull` tool (or a periodic background fetch) so commits flow in without a redeploy.
- Strip Claude's Markdown to Telegram-compatible MarkdownV2 before sending, then set `parse_mode`.
- Add structured logging (`slog`) and a `/health` HTTP endpoint if your platform wants one.
- Rate-limit per chat to cap accidental API spend.
- For private/sensitive repos: pin `ALLOWED_USER_IDS` tightly and never expose the webhook publicly.

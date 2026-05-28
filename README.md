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

Two paths — pick by whether you want to exercise the Go binary directly or the production-equivalent docker image.

### Bare Go (fastest iteration)

```bash
cp .env.example .env
# fill in TELEGRAM_BOT_TOKEN, LLM_API_KEY, REPO_PATH, ALLOWED_USER_IDS, AI_ENABLED=true
export $(grep -v '^#' .env | xargs)
go mod tidy
go run .
```

### Docker (mirrors production)

`scripts/build-local.sh` cross-compiles the bot on the host and bakes it into the distroless runtime image. The host-side compile is what makes this work behind corporate TLS-interception proxies — the in-container `Dockerfile` path needs CA trust that local builds usually don't have.

```bash
./scripts/build-local.sh                # → repo-chat-bot:local (~13 MB)

docker run --rm \
  --env-file .env \
  -v "$PWD:/app/repo:ro" \
  -p 127.0.0.1:8080:8080 \
  repo-chat-bot:local
```

Pass a tag to label the image, or force an arch when you're building on Apple Silicon for an amd64 VM:

```bash
./scripts/build-local.sh dev-2026-01    # → repo-chat-bot:dev-2026-01
GOARCH=amd64 ./scripts/build-local.sh   # cross-arch
```

CI uses the standard multi-stage `Dockerfile` (`docker build .`); that's the canonical path when network access to the Go module proxy is unrestricted.

### Getting credentials

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

## Knowledge-base sync from object storage

If you'd rather not keep the KB on disk (or git-clone it), the bot can pull it
from S3 or OCI Object Storage on a polling interval. Set `KB_STORAGE_PROVIDER`
to `s3` or `oci` and configure the rest of the `KB_*` vars in `.env.example`.

The sync writes each pull into a fresh `<KB_SYNC_BASE_DIR>/kb.<timestamp>/`
directory, builds a manifest of ETags, and atomically flips the
`<KB_SYNC_BASE_DIR>/current` symlink — readers never see a half-written tree.
Unchanged files are hard-linked from the previous snapshot, so only changed
objects hit the network. Two snapshots are retained for safe in-flight reads.

S3 credentials use the standard AWS chain (env vars / `~/.aws/credentials` /
IAM role). OCI uses `~/.oci/config` by default, or set `KB_OCI_AUTH=instance`
to use instance principal on an OCI compute VM.

## Healthcheck endpoint

The bot can expose a tiny HTTP server with a `/healthz` liveness probe — useful for Docker `HEALTHCHECK`, Fly checks, Oracle Cloud load-balancer probes, and Kubernetes liveness/readiness wiring.

Enable it via env vars:

```env
HEALTHCHECK_ENABLED=true
HEALTHCHECK_PORT=8080       # optional, defaults to 8080
```

When enabled, the server binds to all interfaces (`0.0.0.0:$HEALTHCHECK_PORT`) and responds on two routes:

```
GET /healthz   →  200 OK
               →  body: "ok"

GET /featurez  →  200 OK
               →  body: {"telegram":false,"slack":false,"ai":false,"kbsync":false,"healthcheck":true}
```

`/featurez` reports feature-toggle state as booleans only — no tokens, paths, model names, or ports — useful when log access is gated and you need to confirm what's actually enabled in a deployment.

Verify locally:

```bash
HEALTHCHECK_ENABLED=true ./repo-chat-bot &
curl -s http://localhost:8080/healthz    # → ok
curl -s http://localhost:8080/featurez   # → {"ai":false,"healthcheck":true,...}
```

Docker integration:

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1
```

The `z` suffix follows the Google/Kubernetes convention (`/healthz`, `/readyz`, `/livez`) — a namespacing trick so ops endpoints don't collide with application routes. The endpoint is disabled by default; turning it on is the only way it binds a port.

## Hardening for production

- Swap in-memory `sync.Map` history for Redis (Upstash, Railway Redis). Persist last ~20 turns per chat.
- Strip Claude's Markdown to Telegram-compatible MarkdownV2 before sending, then set `parse_mode`.
- Add structured logging (`slog`) for production observability.
- Rate-limit per chat to cap accidental API spend.
- For private/sensitive repos: pin `ALLOWED_USER_IDS` tightly and never expose the webhook publicly.
- If you enable the healthcheck endpoint on a public VM, restrict the port at the firewall/security-list level. `/healthz` is harmless and `/featurez` returns booleans only (no tokens, paths, model names, or ports), but neither needs to be world-reachable — scope the rule to your orchestrator's probe range.

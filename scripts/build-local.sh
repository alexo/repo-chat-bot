#!/usr/bin/env bash
# Build a runnable docker image of repo-chat-bot locally — without going
# through `go mod download` inside the build container (which breaks behind
# corporate TLS-interception proxies).
#
# Strategy: cross-compile the Go binary on the host (where the corp CA is
# already trusted), then build the tiny distroless runtime image via
# Dockerfile.local.
#
# Usage:
#   scripts/build-local.sh                  # → repo-chat-bot:local
#   scripts/build-local.sh my-tag           # → repo-chat-bot:my-tag
#   GOARCH=amd64 scripts/build-local.sh     # force a specific arch
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

TAG="${1:-local}"
IMAGE="repo-chat-bot:${TAG}"

# Auto-detect target arch from the local docker engine unless GOARCH is set.
if [ -z "${GOARCH:-}" ]; then
  DOCKER_ARCH="$(docker info --format '{{.Architecture}}' 2>/dev/null || echo 'x86_64')"
  case "$DOCKER_ARCH" in
    aarch64|arm64) GOARCH=arm64 ;;
    x86_64|amd64)  GOARCH=amd64 ;;
    *)             echo "warn: unknown docker arch '$DOCKER_ARCH', defaulting to amd64" >&2; GOARCH=amd64 ;;
  esac
fi

VERSION="${VERSION:-local-$(git rev-parse --short HEAD 2>/dev/null || echo dirty)}"
RELEASE_DATE="${RELEASE_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

echo "→ Cross-compiling for linux/${GOARCH} (version=${VERSION})..."
GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 \
  go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.ReleaseDate=${RELEASE_DATE}" \
    -o repo-chat-bot-linux \
    .

echo "→ Building image ${IMAGE}..."
docker build -f Dockerfile.local -t "$IMAGE" .

echo ""
echo "✓ Built ${IMAGE}"
docker images "$IMAGE" --format 'table {{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}'
echo ""
echo "Run with:"
echo "  docker run --rm --env-file .env.local -v \"\$PWD:/app/repo:ro\" -p 127.0.0.1:8080:8080 ${IMAGE}"

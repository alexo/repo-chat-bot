#!/usr/bin/env bash
# Runs on the VM. Streamed in over SSH by deploy.sh / the Deploy workflow.
#
# Required env: IMAGE
# Optional env: GHCR_USER, GHCR_TOKEN (needed for private GHCR packages)
set -euo pipefail

: "${IMAGE:?IMAGE env var required (e.g. ghcr.io/owner/repo:tag)}"

cd /opt/repo-chat-bot

if [ -n "${GHCR_TOKEN:-}" ]; then
  : "${GHCR_USER:?GHCR_USER required when GHCR_TOKEN is set}"
  printf '%s' "$GHCR_TOKEN" | docker login ghcr.io -u "$GHCR_USER" --password-stdin
fi

cat > compose.ghcr.yml <<EOF
services:
  bot:
    image: ${IMAGE}
    pull_policy: always
EOF

docker compose -f compose.yml -f compose.ghcr.yml pull bot
docker compose -f compose.yml -f compose.ghcr.yml up -d --remove-orphans bot
docker compose -f compose.yml -f compose.ghcr.yml ps
docker compose -f compose.yml -f compose.ghcr.yml logs --tail=40 bot

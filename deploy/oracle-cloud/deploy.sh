#!/usr/bin/env bash
# Deploys a GHCR image of repo-chat-bot to the Oracle Cloud VM by streaming
# remote-deploy.sh over SSH. Works identically from a laptop or from CI.
#
# Usage:
#   ./deploy.sh [TAG]      # TAG defaults to "latest"
#
# Required env:
#   OCI_HOST               VM hostname or IP
#
# Optional env:
#   OCI_USER               SSH user (default: ubuntu)
#   SSH_KEY                Path to private key (default: ssh's own default)
#   GHCR_USER              GHCR username (default: $USER)
#   GHCR_TOKEN             PAT with read:packages (only for private packages)
#   IMAGE_NAME             Full image repo (default: derived from `git remote`)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE_SCRIPT="${SCRIPT_DIR}/remote-deploy.sh"

: "${OCI_HOST:?OCI_HOST env var required}"
OCI_USER="${OCI_USER:-ubuntu}"
TAG="${1:-latest}"

if [ -z "${IMAGE_NAME:-}" ]; then
  REMOTE_URL="$(git -C "$SCRIPT_DIR" config --get remote.origin.url 2>/dev/null || true)"
  REPO_PATH="$(printf '%s' "$REMOTE_URL" \
    | sed -E 's#^(git@github.com:|https?://github.com/)##; s#\.git$##' \
    | tr '[:upper:]' '[:lower:]')"
  if [ -z "$REPO_PATH" ]; then
    echo "deploy.sh: cannot infer IMAGE_NAME from git remote; set IMAGE_NAME explicitly" >&2
    exit 1
  fi
  IMAGE_NAME="ghcr.io/${REPO_PATH}"
fi

IMAGE="${IMAGE_NAME}:${TAG}"
echo "→ Deploying ${IMAGE} to ${OCI_USER}@${OCI_HOST}"

SSH_OPTS=()
[ -n "${SSH_KEY:-}" ] && SSH_OPTS+=(-i "$SSH_KEY" -o IdentitiesOnly=yes)

ssh ${SSH_OPTS[@]+"${SSH_OPTS[@]}"} "${OCI_USER}@${OCI_HOST}" \
  env IMAGE="$IMAGE" \
      GHCR_USER="${GHCR_USER:-$USER}" \
      GHCR_TOKEN="${GHCR_TOKEN:-}" \
      bash -s < "$REMOTE_SCRIPT"

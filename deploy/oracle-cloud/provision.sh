#!/usr/bin/env bash
# Provision an Always-Free Oracle Cloud VM that runs repo-chat-bot.
#
# Prereqs:
#   - OCI CLI installed and `oci setup config` completed (default profile, region set).
#   - A VCN with a regional public subnet you can launch into.
#   - An SSH public key on this machine.
#
# Required env vars:
#   COMPARTMENT_OCID   compartment to launch the instance into
#   SUBNET_OCID        regional public subnet OCID
#
# Optional env vars (with defaults):
#   SSH_PUBLIC_KEY_PATH   ~/.ssh/id_ed25519.pub
#   INSTANCE_NAME         repo-chat-bot
#   OCPUS                 1                 (A1.Flex, free tier allows up to 4)
#   MEMORY_GB             6                 (A1.Flex, free tier allows up to 24)
#   BOOT_VOLUME_GB        50
#   AVAILABILITY_DOMAIN   <auto-picked>
#   BOT_REPO_URL          ""                cloned into /opt/repo-chat-bot/src on first boot
#   KB_REPO_URL           ""                cloned into /opt/repo-chat-bot/repo on first boot
#   DEPLOY_USER           opc               OS user that owns /opt/repo-chat-bot and runs docker
#
# Usage:
#   COMPARTMENT_OCID=ocid1.compartment.oc1... \
#   SUBNET_OCID=ocid1.subnet.oc1... \
#   BOT_REPO_URL=https://github.com/you/repo-chat-bot.git \
#   ./provision.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

require() {
  local var=$1
  if [ -z "${!var:-}" ]; then
    echo "error: $var is required" >&2
    exit 1
  fi
}

require COMPARTMENT_OCID
require SUBNET_OCID

command -v oci >/dev/null || { echo "error: oci CLI not found. https://docs.oracle.com/iaas/Content/API/SDKDocs/cliinstall.htm" >&2; exit 1; }
command -v jq  >/dev/null || { echo "error: jq is required" >&2; exit 1; }

SSH_PUBLIC_KEY_PATH="${SSH_PUBLIC_KEY_PATH:-$HOME/.ssh/id_ed25519.pub}"
[ -f "$SSH_PUBLIC_KEY_PATH" ] || { echo "error: SSH public key not found at $SSH_PUBLIC_KEY_PATH" >&2; exit 1; }

INSTANCE_NAME="${INSTANCE_NAME:-repo-chat-bot}"
OCPUS="${OCPUS:-1}"
MEMORY_GB="${MEMORY_GB:-6}"
BOOT_VOLUME_GB="${BOOT_VOLUME_GB:-50}"
BOT_REPO_URL="${BOT_REPO_URL:-}"
KB_REPO_URL="${KB_REPO_URL:-}"
DEPLOY_USER="${DEPLOY_USER:-opc}"

echo ">> Resolving availability domain..."
if [ -z "${AVAILABILITY_DOMAIN:-}" ]; then
  AVAILABILITY_DOMAIN=$(oci iam availability-domain list \
    --compartment-id "$COMPARTMENT_OCID" \
    --query 'data[0].name' --raw-output)
fi
echo "   AD: $AVAILABILITY_DOMAIN"

echo ">> Resolving latest Oracle Linux 8 aarch64 image..."
IMAGE_OCID=$(oci compute image list \
  --compartment-id "$COMPARTMENT_OCID" \
  --operating-system "Oracle Linux" \
  --operating-system-version "8" \
  --shape "VM.Standard.A1.Flex" \
  --sort-by TIMECREATED --sort-order DESC --limit 1 \
  --query 'data[0].id' --raw-output)
[ -n "$IMAGE_OCID" ] || { echo "error: no A1.Flex image found in this region" >&2; exit 1; }
echo "   image: $IMAGE_OCID"

echo ">> Rendering cloud-init..."
RENDERED="$(mktemp -t repo-chat-bot-cloud-init.XXXXXX.yml)"
COMPOSE_TMP="$(mktemp -t repo-chat-bot-compose.XXXXXX.yml)"
trap 'rm -f "$RENDERED" "$COMPOSE_TMP"' EXIT

# Embed compose.yml under `content: |` with 6-space indent.
# Using sed `r` to insert the file at the placeholder is portable across BSD/GNU sed.
sed 's/^/      /' "$SCRIPT_DIR/compose.yml" > "$COMPOSE_TMP"

sed -e "/{{COMPOSE_YML}}/{
  r $COMPOSE_TMP
  d
}" \
    -e "s|{{BOT_REPO_URL}}|${BOT_REPO_URL}|g" \
    -e "s|{{KB_REPO_URL}}|${KB_REPO_URL}|g" \
    -e "s|{{DEPLOY_USER}}|${DEPLOY_USER}|g" \
    "$SCRIPT_DIR/cloud-init.tmpl.yml" > "$RENDERED"

echo ">> Launching instance '$INSTANCE_NAME' (${OCPUS} OCPU / ${MEMORY_GB} GB)..."
LAUNCH_JSON=$(oci compute instance launch \
  --compartment-id "$COMPARTMENT_OCID" \
  --availability-domain "$AVAILABILITY_DOMAIN" \
  --shape "VM.Standard.A1.Flex" \
  --shape-config "{\"ocpus\": $OCPUS, \"memoryInGBs\": $MEMORY_GB}" \
  --image-id "$IMAGE_OCID" \
  --display-name "$INSTANCE_NAME" \
  --ssh-authorized-keys-file "$SSH_PUBLIC_KEY_PATH" \
  --user-data-file "$RENDERED" \
  --boot-volume-size-in-gbs "$BOOT_VOLUME_GB" \
  --create-vnic-details "{\"subnetId\":\"$SUBNET_OCID\",\"assignPublicIp\":true,\"displayName\":\"$INSTANCE_NAME-vnic\"}" \
  --wait-for-state RUNNING)

INSTANCE_ID=$(echo "$LAUNCH_JSON" | jq -r '.data.id')
echo "   instance: $INSTANCE_ID"

echo ">> Fetching public IP..."
PUBLIC_IP=$(oci compute instance list-vnics \
  --instance-id "$INSTANCE_ID" \
  --query 'data[0]."public-ip"' --raw-output)

cat <<EOF

>> Done.

  Instance:   $INSTANCE_ID
  Public IP:  $PUBLIC_IP
  SSH:        ssh $DEPLOY_USER@$PUBLIC_IP

Cloud-init runs on first boot (~1-2 minutes). Then:

  ssh $DEPLOY_USER@$PUBLIC_IP
  sudo -u $DEPLOY_USER nano /opt/repo-chat-bot/.env       # fill in tokens
  cd /opt/repo-chat-bot && docker compose up -d --build
  docker compose logs -f bot

If A1.Flex capacity errors occur during launch, retry with a different
AVAILABILITY_DOMAIN or wait and retry.
EOF

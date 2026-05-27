# Automated deploy: Oracle Cloud (Always Free)

One script + cloud-init. Provisions an A1.Flex VM, installs Docker, drops a
compose stack, and (optionally) clones the bot source and your knowledge base.

For the click-by-click manual path, see [docs/deploy-oracle-cloud.md](../../docs/deploy-oracle-cloud.md).

## Files

| File | Purpose |
|------|---------|
| `provision.sh` | Calls `oci compute instance launch` with a rendered cloud-init. |
| `cloud-init.tmpl.yml` | First-boot config: installs Docker, lays out `/opt/repo-chat-bot`, drops `.env` stub and a 6-hour KB-refresh cron. |
| `compose.yml` | Bot service (`build: ./src`, mounts `./repo` read-only, pins `REPO_PATH=/app/repo`). |
| `deploy.sh` | Canonical deploy entry point. Streams `remote-deploy.sh` to the VM over SSH. Used by both laptop and CI. |
| `remote-deploy.sh` | Runs on the VM: writes the GHCR compose override and `docker compose pull && up -d`. Never stored on the VM — piped in via stdin on every deploy. |

## Prerequisites

1. [Oracle Cloud account](https://www.oracle.com/cloud/free/).
2. [`oci` CLI installed](https://docs.oracle.com/iaas/Content/API/SDKDocs/cliinstall.htm), then `oci setup config` (set default region too).
3. `jq` installed locally.
4. A VCN with a **regional public subnet** in your compartment. The default
   tenancy VCN works; otherwise create one in the OCI console.
5. SSH keypair on this machine (`~/.ssh/id_ed25519` by default).

## Run

```bash
export COMPARTMENT_OCID=ocid1.compartment.oc1...
export SUBNET_OCID=ocid1.subnet.oc1...
export BOT_REPO_URL=https://github.com/<you>/repo-chat-bot.git    # optional
export KB_REPO_URL=https://github.com/<you>/your-kb.git           # optional

./provision.sh
```

The script prints the public IP and the next steps. After cloud-init finishes
(usually ~1–2 minutes after the VM enters `RUNNING`):

```bash
ssh opc@<public-ip>
sudo -u opc nano /opt/repo-chat-bot/.env       # fill tokens / keys
cd /opt/repo-chat-bot
docker compose up -d --build
docker compose logs -f bot
```

## Tuning

All knobs are env vars:

| Var | Default | Notes |
|-----|---------|-------|
| `OCPUS` | `1` | Up to 4 free across all A1 instances in your tenancy. |
| `MEMORY_GB` | `6` | Up to 24 free total. 1 GB is enough for this bot. |
| `BOOT_VOLUME_GB` | `50` | Free tier covers 200 GB total. |
| `INSTANCE_NAME` | `repo-chat-bot` | Display name in the console. |
| `AVAILABILITY_DOMAIN` | auto | Set explicitly if you hit capacity errors. |
| `SSH_PUBLIC_KEY_PATH` | `~/.ssh/id_ed25519.pub` | |
| `DEPLOY_USER` | `opc` | OS user that owns `/opt/repo-chat-bot` and runs Docker. Use `ubuntu` for an Ubuntu image. |

## Deploying / updating the bot

The canonical entry point is `deploy.sh`. It pulls the published image from
GHCR and rolls the container — same script CI uses, so laptop and CI deploys
are byte-for-byte identical.

```bash
# From the repo root, on your laptop:
export OCI_HOST=<vm-ip>
export OCI_USER=ubuntu                       # default: ubuntu
export GHCR_TOKEN=<pat-with-read:packages>   # only if package is private
# export SSH_KEY=~/.ssh/id_ed25519           # default: ssh's own default
# export IMAGE_NAME=ghcr.io/<owner>/<repo>   # default: derived from git remote

./deploy/oracle-cloud/deploy.sh              # → :latest
./deploy/oracle-cloud/deploy.sh sha-abc1234  # specific build
./deploy/oracle-cloud/deploy.sh v1.0.0       # tagged release
```

`deploy.sh` SSHes into the VM and pipes `remote-deploy.sh` in over stdin —
nothing is left behind on the VM beyond the running container and the
`compose.ghcr.yml` override that pins the image.

### Local-build fallback

If you'd rather skip GHCR and build on the VM (e.g. for an ad-hoc patch):

```bash
ssh ubuntu@<vm>
cd /opt/repo-chat-bot/src && git pull
cd /opt/repo-chat-bot && docker compose up -d --build
```

The included cron job refreshes `/opt/repo-chat-bot/repo` (the KB) every 6
hours and restarts the container. Edit `/etc/cron.d/repo-chat-bot-kb-refresh`
to change the cadence or disable it.

## Common issues

- **"Out of host capacity" on launch.** A1.Flex is heavily in demand. Retry, or
  set `AVAILABILITY_DOMAIN` to a different AD in your region.
- **SSH hangs.** The VCN's security list needs an ingress rule for TCP 22 from
  your IP (or `0.0.0.0/0`). The default VCN has this by default; custom VCNs
  don't.
- **Bot can't reach the network.** Oracle Linux ships an iptables FORWARD REJECT
  rule that can block Docker's bridge. Cloud-init restarts Docker so it
  reinserts its rules above the reject; if you still see issues, run
  `sudo iptables -L FORWARD -n --line-numbers` and confirm Docker's rules are
  above the REJECT line.
- **`.env` not picked up.** `docker compose up -d` re-reads `.env` only on
  recreate. Use `docker compose up -d --force-recreate` after edits.

## Continuous deploy from GitHub

The workflow at `.github/workflows/deploy-oracle-cloud.yml` resolves the image
tag from the trigger, configures SSH from secrets, and invokes the same
`deploy.sh` you'd run locally. There's no separate deploy logic in the workflow.

Triggers:

- **Auto** — on successful completion of "Publish Docker image" on `main`. The
  exact commit SHA is deployed (tag `sha-<short>`).
- **Manual** — `workflow_dispatch` with an `image_tag` input (defaults to
  `latest`). Use this to roll back: pass a previous `sha-<short>` or `v<x.y.z>`.

### Required secrets (Settings → Secrets and variables → Actions)

| Secret | Value |
|--------|-------|
| `OCI_HOST` | VM public IP (or DNS) |
| `OCI_USER` | `opc` (Oracle Linux) or `ubuntu` |
| `OCI_SSH_PRIVATE_KEY` | Private key matching the public key uploaded to the instance. Include the `-----BEGIN/END-----` lines. |
| `OCI_SSH_KNOWN_HOSTS` | *Optional but recommended.* Lines for the VM's host keys. Best generated from the VM itself: `ssh <user>@<host> 'for f in /etc/ssh/ssh_host_*_key.pub; do read -r alg key _ < "$f"; printf "%s %s %s\n" "<host>" "$alg" "$key"; done'`. If unset, the workflow falls back to TOFU via `ssh-keyscan` and prints a warning. |
| `GHCR_PULL_TOKEN` | *Optional.* PAT with `read:packages` scope. Only needed if the GHCR package is private. |

### Environment

The workflow targets a `production` GitHub Environment. Create it under
Settings → Environments to opt into approval gates, deployment branches, or
environment-scoped secrets. Without it, the workflow still runs — `environment:`
just becomes a label.

### What `remote-deploy.sh` does on the VM

1. `docker login ghcr.io` (only if `GHCR_TOKEN` is set).
2. Writes `/opt/repo-chat-bot/compose.ghcr.yml` pinning `image: <IMAGE>` with `pull_policy: always`.
3. `docker compose -f compose.yml -f compose.ghcr.yml pull bot`
4. `docker compose -f compose.yml -f compose.ghcr.yml up -d --remove-orphans bot`
5. Tails the last 40 log lines for confirmation.

The VM's `compose.yml` (with `build: ./src`) still works for local-built
deploys; the override file only takes effect when both files are passed.

## Teardown

```bash
oci compute instance terminate --instance-id <instance-ocid> --force \
  --preserve-boot-volume false
```

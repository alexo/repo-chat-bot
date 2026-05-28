# Deploy on Oracle Cloud (Always Free Tier)

Run `repo-chat-bot` on OCI's Always Free tier at zero infrastructure cost.

Two paths:

- **Automated** — see [`deploy/oracle-cloud/`](../deploy/oracle-cloud/README.md). One script + cloud-init.
- **Manual (this doc)** — useful if you don't want to install the OCI CLI, or if you prefer to walk the console once before automating.

## Always Free allocation

| Resource | Free allocation |
|----------|----------------|
| ARM VM (Ampere A1.Flex) | Up to 4 instances, 24 GB RAM total, 4 OCPUs total |
| Block storage | 200 GB total |
| Outbound data | 10 TB / month |
| Public IPv4 | 1 reserved IP per tenancy |

This bot needs ~0.5 OCPU, ~1 GB RAM, persistent disk for `/app/repo`, and outbound HTTPS only. **Infrastructure cost: $0.** Only your LLM provider bills.

## Gotchas to know before you start

1. **A1.Flex capacity is constrained.** "Out of host capacity" is common on launch. Retry, or pick a different Availability Domain in your region.
2. **Reserve the public IP.** The default public IP is ephemeral and can change after some failure modes. Reserving one is free (1 per tenancy).
3. **iptables FORWARD REJECT on Oracle Linux.** OL ships a `REJECT` on `FORWARD`. Docker normally inserts its bridge rules above it; if you see container egress issues, restart Docker (`sudo systemctl restart docker`).
4. **Security lists ≠ host firewall.** Allowing a port in the VCN security list does not open it on the host. This bot only needs outbound, so it usually doesn't matter — except when you flip on `HEALTHCHECK_ENABLED` and want an OCI Load Balancer or external probe to reach `/healthz` and `/featurez` on `HEALTHCHECK_PORT`. Then you need *both* the VCN security list rule *and* the host firewall (`firewalld` / `ufw`) open. It's the #1 OCI surprise. `/featurez` returns booleans only (no secrets), so external exposure is low-risk reconnaissance value; still, scope the security list rule tightly when possible.

---

## Prerequisites

1. [Oracle Cloud account](https://www.oracle.com/cloud/free/) (card required for signup; free tier is genuinely free).
2. SSH keypair on your machine.
3. `.env` values ready (Telegram token, Slack tokens, LLM key, allowed user IDs).

---

## Step 1 — Create an A1.Flex VM

OCI Console → Compute → Instances → **Create Instance**:

- **Name**: `repo-chat-bot`
- **Image**: Oracle Linux 8 (aarch64) or Ubuntu 22.04 Minimal aarch64
- **Shape**: `VM.Standard.A1.Flex` — **1 OCPU**, **1 GB** (enough; bump later if needed)
- **Networking**: use the default VCN/subnet, or one you've created. **Assign public IPv4 address: Yes.**
- **SSH key**: upload your public key
- **Boot volume**: 50 GB (default)

Click **Create**. Wait for **Running**. Note the **Public IP**.

> Optional but recommended: Console → Networking → IP Management → **Reserve a public IP** and **Attach** it to this instance's primary VNIC. Same IP across reboots, still free.

---

## Step 2 — Open SSH

Default VCN security lists already allow TCP 22 from `0.0.0.0/0`. If you created a custom VCN, add that ingress rule. (Scope down to your IP if you can.)

```bash
ssh opc@<PUBLIC_IP>          # Oracle Linux
# or
ssh ubuntu@<PUBLIC_IP>       # Ubuntu
```

---

## Step 3 — Install Docker + compose plugin

### Oracle Linux 8

```bash
sudo dnf install -y dnf-utils git
sudo dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
sudo systemctl enable --now docker
sudo usermod -aG docker opc
exit          # log out so docker group takes effect
ssh opc@<PUBLIC_IP>
```

### Ubuntu 22.04

```bash
sudo apt-get update
sudo apt-get install -y docker.io docker-compose-v2 git
sudo systemctl enable --now docker
sudo usermod -aG docker ubuntu
exit
ssh ubuntu@<PUBLIC_IP>
```

Verify: `docker compose version` (should report a v2.x version).

---

## Step 4 — Lay out files

```bash
sudo mkdir -p /opt/repo-chat-bot/{src,repo}
sudo chown -R $USER:$USER /opt/repo-chat-bot
cd /opt/repo-chat-bot

# Bot source
git clone https://github.com/<you>/repo-chat-bot.git src

# Knowledge base
git clone <your-kb-repo-url> repo
# Or layer multiple sources:
# git clone <repo-a> repo/source-a
# git clone <repo-b> repo/source-b
```

---

## Step 5 — Drop a `compose.yml`

Use the one from `deploy/oracle-cloud/compose.yml` in your bot repo, or paste this into `/opt/repo-chat-bot/compose.yml`:

```yaml
services:
  bot:
    build:
      context: ./src
    container_name: repo-chat-bot
    restart: unless-stopped
    env_file: .env
    volumes:
      - ./repo:/app/repo:ro
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

---

## Step 6 — Create `.env`

```bash
cat > /opt/repo-chat-bot/.env <<'EOF'
TELEGRAM_BOT_TOKEN=
ALLOWED_USER_IDS=
LLM_PROVIDER=openrouter
LLM_API_KEY=
LLM_MODEL=openai/gpt-4o-mini
REPO_PATH=/app/repo
SLACK_APP_TOKEN=
SLACK_BOT_TOKEN=
SLACK_DEBUG=false
EOF
chmod 600 /opt/repo-chat-bot/.env
nano /opt/repo-chat-bot/.env       # fill in values
```

> Avoid pasting secrets directly on the command line — they end up in shell history. Use `nano`, or `scp` a prepared file from your laptop.

---

## Step 7 — Start the stack

```bash
cd /opt/repo-chat-bot
docker compose up -d --build
docker compose logs -f bot
```

You should see:

```
repo-chat-bot version=... release_date=...
config toggles: ai=true telegram=false slack=true kbsync=false healthcheck=false
SUCCESS: Loaded REPO_PATH from config: /app/repo
SUCCESS: Repo root resolved to: /app/repo
SUCCESS: LLM provider: openrouter
slack bot starting
repo-chat-bot is running (press Ctrl+C to exit)
Slack bot: connected (hello received)
```

The `config toggles:` line is your one-line summary of what's enabled — grep for it in logs to confirm the deployed instance matches your `.env`.

`restart: unless-stopped` plus `systemctl enable docker` means the container comes back automatically after a reboot. Verify with `sudo reboot`, wait, SSH in, `docker compose ps`.

---

## Step 8 — Keep it updated

**Bot code:**

```bash
cd /opt/repo-chat-bot/src && git pull
cd /opt/repo-chat-bot && docker compose up -d --build
```

**Knowledge base** (set this once):

```bash
sudo tee /etc/cron.d/repo-chat-bot-kb-refresh > /dev/null <<'EOF'
0 */6 * * * opc cd /opt/repo-chat-bot/repo && git pull --ff-only && /usr/bin/docker compose -f /opt/repo-chat-bot/compose.yml restart bot
EOF
```

Adjust user (`opc` → `ubuntu`) and cadence to taste.

---

## Alternative: pull a prebuilt image from GHCR

If you build the image in CI and push to GHCR, swap the `build:` block for `image:`:

```yaml
services:
  bot:
    image: ghcr.io/<you>/repo-chat-bot:latest
    container_name: repo-chat-bot
    restart: unless-stopped
    env_file: .env
    volumes:
      - ./repo:/app/repo:ro
```

Then deploy with `docker compose pull && docker compose up -d`. Add a `watchtower` sidecar if you want automatic pulls.

---

## Alternative: native binary + systemd

Skip Docker entirely:

```bash
sudo dnf install -y golang        # OL8
cd /opt/repo-chat-bot/src
go build -o /opt/repo-chat-bot/bot .

sudo tee /etc/systemd/system/repo-chat-bot.service > /dev/null <<'EOF'
[Unit]
Description=repo-chat-bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=opc
WorkingDirectory=/opt/repo-chat-bot
EnvironmentFile=/opt/repo-chat-bot/.env
ExecStart=/opt/repo-chat-bot/bot
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now repo-chat-bot
journalctl -u repo-chat-bot -f
```

---

## Cost summary

| Component | Monthly cost |
|-----------|-------------|
| VM (A1.Flex, 1 OCPU / 1–6 GB) | $0 |
| Boot volume (50 GB) | $0 |
| Reserved public IPv4 | $0 |
| Outbound traffic (up to 10 TB) | $0 |
| LLM provider usage | variable, billed by provider |
| **Infrastructure total** | **$0** |

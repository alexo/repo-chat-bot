# Automated deploy: Oracle Cloud (Always Free)

One script + cloud-init. Provisions an A1.Flex VM, installs Docker, drops a
compose stack, and (optionally) clones the bot source and your knowledge base.

For the click-by-click manual path, see [docs/deploy-oracle-cloud.md](../../docs/deploy-oracle-cloud.md).

## Files

| File | Purpose |
|------|---------|
| `provision.sh` | Calls `oci compute instance launch` with a rendered cloud-init. |
| `cloud-init.tmpl.yml` | First-boot config: installs Docker, lays out `/opt/repo-chat-bot`, drops `.env` stub and a 6-hour KB-refresh cron. |
| `compose.yml` | Bot service (`build: ./src`, mounts `./repo` read-only). |

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

## Updating the bot

On the VM:

```bash
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

## Teardown

```bash
oci compute instance terminate --instance-id <instance-ocid> --force \
  --preserve-boot-volume false
```

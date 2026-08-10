#!/usr/bin/env bash
# Builds the panel and replaces the binary on the router-mode test VM.
# Frontend first: it is embedded via go:embed, so a stale dist/ ships silently.
set -euo pipefail

VM_KEY="${VM_KEY:-$HOME/nasnet-router-vm/id_vm}"
VM_PORT="${VM_PORT:-2222}"
VM_HOST="${VM_HOST:-root@127.0.0.1}"
VM_BIN="${VM_BIN:-/usr/local/bin/nasnet-panel}"
GOARCH="${GOARCH:-arm64}"
SKIP_WEB="${SKIP_WEB:-0}"

cd "$(dirname "$0")"
OUT=$(mktemp -t nasnet-panel-XXXX)
trap 'rm -f "$OUT" "$OUT.gz"' EXIT

# IdentityAgent=none: with several keys loaded the agent offers them first and
# sshd cuts the connection at "Too many authentication failures".
SSH=(ssh -o StrictHostKeyChecking=no -o IdentitiesOnly=yes -o IdentityAgent=none
     -o ConnectTimeout=15 -i "$VM_KEY" -p "$VM_PORT" "$VM_HOST")

say() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }

if [[ "$SKIP_WEB" != "1" ]]; then
    say "Building the frontend"
    pnpm --dir "$PWD/web-panel" build
fi

say "Building the panel for linux/$GOARCH"
GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 go build -o "$OUT" .
printf 'built %s (%s)\n' "$OUT" "$(du -h "$OUT" | cut -f1)"

say "Checking the VM is reachable"
if ! "${SSH[@]}" true 2>/dev/null; then
    echo "Cannot reach $VM_HOST:$VM_PORT." >&2
    echo "Start it with: ~/nasnet-router-vm/vm start" >&2
    echo "If it is running, the guest may have lost its nft table — the reply to an" >&2
    echo "inbound connection then leaves by the wrong uplink. Recover over the serial" >&2
    echo "console: cd ~/nasnet-router-vm && ./con 'systemctl restart nasnet-panel'" >&2
    exit 1
fi

say "Installing"
# Piped in rather than scp'd so this works with no extra tooling on the guest.
gzip -c "$OUT" | "${SSH[@]}" "
    set -e
    systemctl stop nasnet-panel || true
    gunzip -c > '$VM_BIN.new'
    chmod 755 '$VM_BIN.new'
    mv '$VM_BIN.new' '$VM_BIN'
    systemctl reset-failed nasnet-panel 2>/dev/null || true
    systemctl start nasnet-panel
"

say "Waiting for the panel to answer"
for _ in $(seq 1 20); do
    if "${SSH[@]}" 'curl -sf -m3 -o /dev/null http://127.0.0.1:9761/' 2>/dev/null; then
        echo "panel is up"
        break
    fi
    sleep 2
done

say "Status"
"${SSH[@]}" '
    systemctl is-active nasnet-panel
    journalctl -u nasnet-panel --since "1 min ago" --no-pager \
        | grep -iE "router mode active|degraded|fatal" | tail -5 || true
'

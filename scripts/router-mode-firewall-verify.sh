#!/usr/bin/env bash
# Verifies filter_in with a drop policy is recoverable. Run on the target: the
# only change here that can lock an operator out, so it gets its own proof.
set -u

PANEL_PORT="${PANEL_PORT:-9761}"
DOMESTIC="${DOMESTIC_IF:?set DOMESTIC_IF}"
SECONDARY="${SECONDARY_IF:?set SECONDARY_IF}"

hdr() { printf '\n===== %s =====\n' "$1"; }
addr_of() { ip -4 -o addr show "$1" | awk '{print $4}' | cut -d/ -f1 | head -1; }

hdr "F1 baseline: the panel answers on both uplinks"
for ifc in "$DOMESTIC" "$SECONDARY"; do
    addr=$(addr_of "$ifc")
    [ -n "$addr" ] && curl -sS -m5 -o /dev/null -w "$ifc ($addr): %{http_code}\n" \
        "http://${addr}:${PANEL_PORT}/" || echo "$ifc: no address"
done

hdr "F2 enable filter_in through the API and DO NOT confirm"
echo "MANUAL: in the panel, turn on 'Close the box to unsolicited traffic' and let"
echo "the 90s window expire. Then check the marker existed and is now gone:"
ls -la /var/lib/nasnet/net-pending.json 2>&1 || echo "marker cleared"
journalctl -u nasnet-netrollback --since "3 minutes ago" --no-pager | tail -20

hdr "F3 after the auto-revert, the panel answers on both uplinks again"
for ifc in "$DOMESTIC" "$SECONDARY"; do
    addr=$(addr_of "$ifc")
    [ -n "$addr" ] && curl -sS -m5 -o /dev/null -w "$ifc ($addr): %{http_code}\n" \
        "http://${addr}:${PANEL_PORT}/" || echo "$ifc: no address"
done
nft list chain inet nasnet filter_in 2>&1 | head -3 || echo "ok: filter_in is gone"

hdr "F4 now enable it AND confirm; the panel must stay up on the domestic uplink"
echo "MANUAL: turn it on again and click Keep these settings."
sleep 5
nft list chain inet nasnet filter_in

hdr "F5 the panel port is closed on the secondary uplink and open on the domestic one"
for ifc in "$DOMESTIC" "$SECONDARY"; do
    addr=$(addr_of "$ifc")
    [ -n "$addr" ] && timeout 5 curl -sS -m5 -o /dev/null \
        -w "$ifc ($addr): %{http_code}\n" "http://${addr}:${PANEL_PORT}/" \
        || echo "$ifc: no answer (expected for the secondary uplink)"
done

hdr "F6 every enabled inbound has an accept"
nft list chain inet nasnet filter_in | grep -E 'dport [0-9]+ accept'
echo "Cross-check against the panel's enabled inbound list. Any inbound without"
echo "an accept is a broken VPN for whoever uses it."

hdr "F7 SSH"
ss -tlnp | grep ':22 ' && echo "listening" || echo "no sshd"
nft list chain inet nasnet filter_in | grep -q 'dport 22' \
    && echo "port 22 accepted" \
    || echo "NOTE: port 22 is NOT accepted on an uplink by design — reach the box"
echo "     via the LAN or the management port, or add a port forward to 127.0.0.1:22"
echo "     (which asks for a typed CONFIRM) if SSH over an uplink is your only path."

#!/usr/bin/env bash
# Stage 1 deliverable check. Run on the deployed 24.04 box as root, with both
# uplinks assigned and confirmed.
set -u

DOMESTIC="${DOMESTIC_IF:?set DOMESTIC_IF to the domestic uplink kernel name}"
SECONDARY="${SECONDARY_IF:?set SECONDARY_IF to the secondary uplink kernel name}"

hdr() { printf '\n===== %s =====\n' "$1"; }

# V7 deletes a default route and restarts the panel, so refuse to run at all
# unless router mode is actually deployed. Half-running these on a box that
# never applied would mutate it for nothing.
hdr "V0 preconditions"
fatal=0
if ! nft list table inet nasnet >/dev/null 2>&1; then
    echo "FAIL: table inet nasnet is absent — router mode has not applied here"
    fatal=1
fi
if ! ip rule show | grep -q 'lookup 201'; then
    echo "FAIL: no group rules installed"
    fatal=1
fi
for i in "$DOMESTIC" "$SECONDARY"; do
    ip link show "$i" >/dev/null 2>&1 || { echo "FAIL: $i does not exist"; fatal=1; }
done
if [[ $fatal -ne 0 ]]; then
    echo
    echo "Refusing to continue: V7 removes a default route and restarts the panel."
    exit 1
fi
echo ok

hdr "V1 main holds no default route"
ip route show table main | grep -E '^default' && echo "FAIL: main has a default" || echo "ok"

hdr "V2 the complete rule list, in order"
ip rule show

hdr "V3 both uplink tables carry a default"
ip route show table 201; ip route show table 202

hdr "V4 foreign-marked traffic leaves by the secondary uplink"
ip route get 1.1.1.1 mark 0x20000
hdr "V5 domestic-marked traffic leaves by the domestic uplink"
ip route get 1.1.1.1 mark 0x10000

hdr "V6 the box's own unmarked traffic works and prefers the secondary"
ip route get 1.1.1.1
sudo -u nobody curl -sS -m8 -o /dev/null -w 'curl rc=%{http_code}\n' https://example.com

hdr "V7 apt and ssh survive a dead secondary uplink"
ip route del default table 202
ip route get 1.1.1.1     # expect the domestic device, NOT unreachable
sudo -u nobody curl -sS -m8 -o /dev/null -w 'curl rc=%{http_code}\n' https://example.com
systemctl restart nasnet-panel   # the reconciler puts the route back
sleep 8; ip route show table 202

hdr "V8 the owned nft table, and only it"
nft list tables
nft list table inet nasnet

hdr "V9 every mask is 0x0fffffff, never 0x00ffffff"
nft list table inet nasnet | grep -oE '0x[0-9a-f]+' | sort -u
# 0xfffffff is MaskAll (0x0fffffff). A bare 24-bit mask would zero the pin.
nft list table inet nasnet | grep -qE '0x0*ffffff($|[^f])' && \
    echo "FAIL: a 24-bit mask is present" || echo "ok"

hdr "V10 conntrack carries an ingress pin on inbound-initiated flows only"
conntrack -L -o extended 2>/dev/null | grep -oE 'mark=[0-9]+' | sort -u | head

hdr "V11 sysctls"
for k in \
  net.ipv4.conf.all.rp_filter "net.ipv4.conf.${DOMESTIC}.rp_filter" \
  "net.ipv4.conf.${SECONDARY}.rp_filter" net.ipv4.tcp_fwmark_accept \
  net.ipv4.fwmark_reflect "net.ipv4.conf.${DOMESTIC}.arp_ignore" \
  "net.ipv4.conf.${DOMESTIC}.arp_announce"; do
    sysctl -n "$k" | xargs printf '%s = %s\n' "$k"
done

hdr "V12 the box resolves, with split DNS per link"
resolvectl status | grep -A3 -E "Link .*(${DOMESTIC}|${SECONDARY})"
getent hosts deb.debian.org; echo "rc=$?"

hdr "V13 netplan is aside and NetworkManager is masked"
ls /etc/netplan/ /etc/netplan.disabled/ 2>&1
systemctl is-enabled NetworkManager 2>&1
ls /run/systemd/network/ | grep netplan && echo "FAIL: stale netplan files" || echo "ok"

hdr "V14 the dead-man timer is running and idle"
systemctl is-active nasnet-netrollback.timer
ls -la /var/lib/nasnet/net-pending.json 2>&1 || echo "ok: no pending apply"

hdr "V15 xray sees direct-foreign first and never restarts on a network event"
jq -r '.outbounds[].tag' /usr/local/etc/xray/config.json 2>/dev/null | head -5
systemctl show nasnet-panel -p AmbientCapabilities

hdr "V16 failover does not drop a live connection"
echo "MANUAL: start a download through a VPN client, unplug the secondary uplink,"
echo "confirm the download stalls and resumes rather than resetting, and that"
echo "journalctl -u nasnet-panel shows no xray restart."
echo
echo "Restart count in the last 5 minutes:"
journalctl -u nasnet-panel --since "5 minutes ago" 2>/dev/null | grep -ci restart

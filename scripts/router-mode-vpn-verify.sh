#!/usr/bin/env bash
# Verifies the WireGuard VPN and its kill switch on a running router-mode box.
#
# Runs ON the box. Stands up a WireGuard server in a network namespace, drives
# the panel's own API to point the tunnel at it, and asserts what the kernel
# actually ends up doing. The golden tests prove the strings; only this proves
# the kernel accepts them.
#
# wireguard-tools is installed here for the *server* side of the test. The panel
# itself never needs it: it speaks netlink.
set -uo pipefail

PANEL_URL="${PANEL_URL:-http://127.0.0.1:9761}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:?set ADMIN_PASS}"

WG_IF="nasnet-wg0"
WG_TABLE=203
NS="wgsrv"
SRV_HOST_IP="10.99.0.1"
SRV_NS_IP="10.99.0.2"
SRV_PORT=51820
TUN_SRV="10.66.0.1"
TUN_CLIENT="10.66.0.2"

pass=0; fail=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail+1)); }
step() { printf '\n\033[1m%s\033[0m\n' "$1"; }
check(){ if eval "$2" >/dev/null 2>&1; then ok "$1"; else bad "$1"; fi; }

JAR=$(mktemp)
api() { # api METHOD PATH [BODY]
    local m=$1 p=$2 b=${3:-}
    if [[ -n "$b" ]]; then
        curl -sS -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' \
             -d "$b" "$PANEL_URL$p"
    else
        curl -sS -b "$JAR" -c "$JAR" -X "$m" "$PANEL_URL$p"
    fi
}

cleanup() {
    ip netns del "$NS" 2>/dev/null
    ip link del wgveth 2>/dev/null
    ip route del "$SRV_NS_IP/32" table nasnet-secondary 2>/dev/null
    rm -f "$JAR"
}
trap cleanup EXIT

step "W0 — preflight"
check "the kernel has WireGuard" "modprobe wireguard && ip link add wgprobe type wireguard && ip link del wgprobe"
command -v wg >/dev/null 2>&1 || { apt-get install -y -q wireguard-tools >/dev/null 2>&1; }
check "wg is available for the test server" "command -v wg"
check "the panel answers" "curl -sf $PANEL_URL/api/v1/health -o /dev/null || curl -sf $PANEL_URL -o /dev/null"

login=$(api POST /api/v1/auth/admin-login "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
case "$login" in *'"success":true'*) ok "logged in" ;; *) bad "login: $login"; exit 1 ;; esac

step "W1 — a WireGuard server in a namespace"
SRV_PRIV=$(wg genkey); SRV_PUB=$(printf '%s' "$SRV_PRIV" | wg pubkey)
CLI_PRIV=$(wg genkey); CLI_PUB=$(printf '%s' "$CLI_PRIV" | wg pubkey)

ip netns del "$NS" 2>/dev/null
ip netns add "$NS"
ip link add wgveth type veth peer name wgveth-ns
ip link set wgveth-ns netns "$NS"
ip addr add "$SRV_HOST_IP/24" dev wgveth
ip link set wgveth up
ip -n "$NS" addr add "$SRV_NS_IP/24" dev wgveth-ns
ip -n "$NS" link set wgveth-ns up
ip -n "$NS" link set lo up

ip netns exec "$NS" ip link add wg0 type wireguard
printf '%s' "$SRV_PRIV" > /tmp/srv.key
ip netns exec "$NS" wg set wg0 listen-port "$SRV_PORT" private-key /tmp/srv.key \
    peer "$CLI_PUB" allowed-ips "$TUN_CLIENT/32"
ip netns exec "$NS" ip addr add "$TUN_SRV/24" dev wg0
ip netns exec "$NS" ip link set wg0 up
rm -f /tmp/srv.key
check "the test server is listening" "ip netns exec $NS wg show wg0 | grep -q 'listening port'"

# The tunnel's transport carries the secondary pin, so it resolves out of that
# uplink's table. Give the table a way to reach the test server.
ip route replace "$SRV_NS_IP/32" dev wgveth table nasnet-secondary
check "the server is reachable from the secondary table" \
    "ip route get $SRV_NS_IP mark 0x2000000 | grep -q wgveth"

step "W2 — import a config"
created=$(api POST /api/v1/network/vpn/profiles \
  "{\"name\":\"testbed\",\"raw\":\"[Interface]\nPrivateKey = $CLI_PRIV\nAddress = $TUN_CLIENT/32\nDNS = $TUN_SRV\n\n[Peer]\nPublicKey = $SRV_PUB\nAllowedIPs = 0.0.0.0/0\nEndpoint = $SRV_NS_IP:$SRV_PORT\n\"}")
case "$created" in *'"success":true'*) ok "config imported" ;; *) bad "import: $created"; exit 1 ;; esac
PROFILE_ID=$(printf '%s' "$created" | sed -n 's/.*"data":{"id":\([0-9]*\).*/\1/p')
[[ -n "$PROFILE_ID" ]] && ok "profile id $PROFILE_ID" || { bad "no profile id"; exit 1; }

# A config that would run a shell command must be refused by name, never run.
evil=$(api POST /api/v1/network/vpn/parse \
  "{\"raw\":\"[Interface]\nPrivateKey = $CLI_PRIV\nAddress = $TUN_CLIENT/32\nPostUp = touch /tmp/pwned\n\n[Peer]\nPublicKey = $SRV_PUB\nEndpoint = 1.2.3.4:51820\n\"}")
case "$evil" in *PostUp*) ok "a scripted config is refused by name" ;; *) bad "scripted config: $evil" ;; esac
check "and the command never ran" "! test -e /tmp/pwned"

step "W3 — turn it on"
act=$(api POST /api/v1/network/vpn/activate "{\"profile_id\":$PROFILE_ID}")
case "$act" in *'"success":true'*) ok "activation applied" ;; *) bad "activate: $act"; exit 1 ;; esac
api POST /api/v1/network/confirm '{}' >/dev/null
sleep 3

check "the tunnel interface exists" "ip link show $WG_IF"
check "it carries the tunnel address" "ip -br addr show $WG_IF | grep -q $TUN_CLIENT"
check "table $WG_TABLE has a default into it" "ip route show table $WG_TABLE | grep -q \"default dev $WG_IF\""
check "the dish subnet stays reachable" "ip route show table $WG_TABLE | grep -q 192.168.100.0/24"
check "foreign traffic resolves into the tunnel" \
    "ip route get 1.1.1.1 mark 0x20000 | grep -q $WG_IF"
check "unmarked traffic prefers the tunnel" "ip rule show | grep -q '^32000:.*lookup nasnet-vpn'"
check "the foreign group no longer names the raw uplink" \
    "! ip rule show | grep -E '^1[5-9][0-9]:' | grep -q nasnet-secondary"

step "W4 — the handshake"
for _ in $(seq 1 10); do
    ip netns exec "$NS" wg show wg0 latest-handshakes | grep -qv '	0$' && break
    sleep 2
done
check "the server saw a handshake" \
    "ip netns exec $NS wg show wg0 latest-handshakes | grep -qv '	0\$'"
check "traffic crosses the tunnel" "ping -c 2 -W 3 -I $WG_IF $TUN_SRV"
status=$(api GET /api/v1/network/vpn/status)
case "$status" in *'"connected":true'*) ok "the panel reports it connected" ;; *) bad "status: $status" ;; esac

step "W5 — the kill switch"
check "killswitch_out exists" "nft list chain inet nasnet killswitch_out"
check "killswitch_fwd exists" "nft list chain inet nasnet killswitch_fwd"
check "it drops last" "nft list chain inet nasnet killswitch_out | tail -3 | grep -q drop"
check "the tunnel's own transport is exempt" \
    "nft list chain inet nasnet killswitch_out | grep -q '0x02000000.*udp'"
check "the bootstrap resolvers are a fixed set" \
    "nft list set inet nasnet doh_bootstrap | grep -q 1.1.1.1"
check "the LAN cannot leave by the raw uplink" \
    "! nft list chain inet nasnet filter_fwd | grep -q '\"enp0s3\"'"

step "W6 — DNS moved inside"
check "the LAN's foreign resolver is on the tunnel" \
    "grep -q \"@$WG_IF\" /etc/dnsmasq.d/nasnet-lan.conf"
check "no plaintext resolver on the raw uplink" \
    "! grep -qE 'server=[^/]*@enp0s3' /etc/dnsmasq.d/nasnet-lan.conf"
check "the secondary uplink unit carries no resolver" \
    "! grep -qE '^DNS=' /etc/systemd/network/10-nasnet-wan-secondary.network"

step "W7 — turn it off"
deact=$(api POST /api/v1/network/vpn/deactivate '{}')
case "$deact" in *'"success":true'*) ok "deactivation applied" ;; *) bad "deactivate: $deact" ;; esac
api POST /api/v1/network/confirm '{}' >/dev/null
sleep 3

check "the tunnel interface is gone" "! ip link show $WG_IF"
check "its table is empty" "! ip route show table $WG_TABLE | grep -q default"
# The whole point: with no tunnel, foreign traffic dies rather than leaking.
check "foreign traffic is blackholed, not leaked" \
    "! ip route get 1.1.1.1 mark 0x20000 2>/dev/null | grep -qE 'enp0s3|dev'"
check "the kill switch is still armed" "nft list chain inet nasnet killswitch_out"
check "the LAN still reaches the domestic uplink" "ip route get 1.1.1.1 mark 0x10000 | grep -q enp0s2"

# The two slowest checks, and the two that matter most when something has
# already gone wrong. Skip with SKIP_SLOW=1.
if [[ "${SKIP_SLOW:-0}" != "1" ]]; then
    step "W8 — the tunnel survives a restart"
    api POST /api/v1/network/vpn/activate "{\"profile_id\":$PROFILE_ID}" >/dev/null
    api POST /api/v1/network/confirm '{}' >/dev/null
    sleep 2
    ip link del "$WG_IF" 2>/dev/null
    systemctl restart nasnet-panel
    sleep 12
    check "the tunnel came back on its own" "ip link show $WG_IF"
    check "its table came back" "ip route show table $WG_TABLE | grep -q 'default dev $WG_IF'"
    check "the dish route came back" "ip route show table $WG_TABLE | grep -q 192.168.100"

    step "W9 — an unconfirmed activation reverts itself"
    JAR=$(mktemp)
    api POST /api/v1/auth/admin-login "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" >/dev/null
    api POST /api/v1/network/vpn/deactivate '{}' >/dev/null
    api POST /api/v1/network/confirm '{}' >/dev/null
    sleep 2
    api POST /api/v1/network/vpn/activate "{\"profile_id\":$PROFILE_ID}" >/dev/null
    # Deliberately not confirming: the dead-man runs out of process, because a
    # bad network apply is most likely to break the panel itself.
    for _ in $(seq 1 24); do
        sleep 5
        ip link show "$WG_IF" >/dev/null 2>&1 || break
    done
    check "the tunnel was reverted" "! ip link show $WG_IF"
    check "the rules were reverted" "! ip rule show | grep -qE '^150:.*nasnet-vpn'"
    check "the profile is inactive again" \
        "api GET /api/v1/network/vpn/status | grep -q '\"active_profile_id\":null'"
fi

step "W10 — cleanup"
api DELETE "/api/v1/network/vpn/profiles/$PROFILE_ID" >/dev/null
check "the profile is gone" "! api GET /api/v1/network/vpn/profiles | grep -q testbed"

printf '\n\033[1m%d passed, %d failed\033[0m\n' "$pass" "$fail"
[[ $fail -eq 0 ]]

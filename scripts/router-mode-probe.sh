#!/usr/bin/env bash
# Router-mode kernel behaviour probes. Run on the 24.04 target as root.
# Prints a labelled block per probe; paste the whole output into
# docs/superpowers/plans/router-mode-probe-results.md.
#
# Probes that mutate link, rule, route, tc or iptables state do so inside a
# throwaway network namespace. Nothing here touches host routing: P4's and P7's
# rules sit at pref 30/50/51 and 32000/32001, all above `main` (32766), so on a
# live box they would capture real egress and can cut the session the probe is
# running over.
set -u

hdr() { printf '\n===== %s =====\n' "$1"; }

# Run a script fragment in a private netns named after the probe, then destroy it.
in_netns() {
  local ns="$1"; shift
  ip netns del "$ns" 2>/dev/null
  if ! ip netns add "$ns"; then
    echo "SKIPPED: cannot create netns $ns"
    return 1
  fi
  ip netns exec "$ns" sh -c "$*"
  ip netns del "$ns"
}

hdr "P0 target identity (expect 6.8.x / systemd 255)"
uname -r; systemctl --version | head -1; . /etc/os-release; echo "$ID $VERSION_ID"
[ "$(id -u)" -eq 0 ] || echo "WARNING: not root — every probe below will fail"
for t in ip iptables tc dnsmasq ss strings; do
  command -v "$t" >/dev/null || echo "MISSING TOOL: $t"
done

hdr "P1 does an unmasked CONNMARK --restore-mark clobber the whole word?"
# Counters, not `-j LOG`: a fresh netns has no nf_log logger bound, so the LOG
# target fires but writes nowhere and dmesg stays empty.
# The probe bit must sit OUTSIDE 0x0fffffff (top nibble). CONNMARK restore is
# `nfmark = (nfmark & ~nfmask) ^ (ctmark & ctmask)`, so a bit *inside* the mask
# is zeroed by both variants and comparing them proves nothing.
in_netns cm '
  ip link set lo up
  probe() {
    iptables -t mangle -A PREROUTING -m mark --mark 0x1000f000/0xffffffff -m comment --comment word-intact
    iptables -t mangle -A PREROUTING -m mark --mark 0x10000000/0xf0000000 -m comment --comment top-nibble-kept
    iptables -t mangle -A PREROUTING -m mark --mark 0x0/0xffffffff        -m comment --comment word-zeroed
    ping -c2 -W1 127.0.0.1 >/dev/null 2>&1
    iptables -t mangle -L PREROUTING -vn
  }
  echo "-- unmasked restore:"
  iptables -t mangle -A PREROUTING -j MARK --set-xmark 0x1000f000/0xffffffff
  iptables -t mangle -A PREROUTING -j CONNMARK --restore-mark
  probe

  echo "-- restore masked 0x0fffffff:"
  iptables -t mangle -F PREROUTING
  iptables -t mangle -A PREROUTING -j MARK --set-xmark 0x1000f000/0xffffffff
  iptables -t mangle -A PREROUTING -j CONNMARK --restore-mark \
      --nfmask 0x0fffffff --ctmask 0x0fffffff
  probe'
# expect: unmasked -> word-zeroed counts,     top-nibble-kept 0
#         masked   -> top-nibble-kept counts, word-zeroed 0

hdr "P2 tc classid minor limit (confirms tiers must not be shifted)"
in_netns tcm '
  ip link add dummy0 type dummy && ip link set dummy0 up
  tc qdisc add dev dummy0 root handle 1: htb default 99
  tc class add dev dummy0 parent 1: classid 1:128000 htb rate 1mbit; echo "shifted rc=$?"
  tc class add dev dummy0 parent 1: classid 1:500    htb rate 1mbit; echo "unshifted rc=$?"
  tc filter add dev dummy0 parent 1: protocol ip prio 1 \
      handle 0x10/0xffff fw classid 1:500; echo "narrow mask rc=$?"
  tc filter add dev dummy0 parent 1: protocol ip prio 2 \
      handle 0x20/0x0fffffff fw classid 1:500; echo "wide mask rc=$?"
  # `tc filter show` never prints the word "mask" — the mask rides in the handle.
  tc filter show dev dummy0'
# expect: shifted rc!=0, unshifted rc=0, handles print as 0x10/0xffff and 0x20/0xfffffff

hdr "P3 dnsmasq nftset — FEATURE detect, never version-sniff"
dnsmasq --version | head -1
dnsmasq --test --nftset=/example.com/inet#filter#myset; echo "rc=$?"

hdr "P4 rule fall-through and the pref 30 suppressor"
in_netns p4 '
  ip link set lo up
  ip rule add pref 30 lookup main suppress_prefixlength 0
  ip rule add fwmark 0x01000000/0x0f000000 lookup 201 pref 50
  ip rule add fwmark 0x01000000/0x0f000000 blackhole    pref 51
  ip route add 10.77.0.0/24 dev lo table main
  echo "-- with pref 30:";    ip route get 10.77.0.5 mark 0x01000000
  ip rule del pref 30
  echo "-- without pref 30:"; ip route get 10.77.0.5 mark 0x01000000'
# expect: with -> dev lo ; without -> blackhole / RTNETLINK "Invalid argument"

hdr "P5 tcp_fwmark_accept — does an accepted socket inherit the SYN mark?"
# The sysctl is per-netns, so the A/B runs below flip it inside a throwaway
# namespace and never touch the host's value.
echo "-- host value (for the record):"; sysctl net.ipv4.tcp_fwmark_accept

p5_case() {
  ip netns exec p5s sysctl -qw net.ipv4.tcp_fwmark_accept="$1"
  ip netns exec p5s iptables -t mangle -Z
  ip netns exec p5c sh -c 'echo p5-probe | timeout 3 nc -w1 10.90.0.1 9351' >/dev/null 2>&1
  echo "-- tcp_fwmark_accept=$1"
  ip netns exec p5s iptables -t mangle -L OUTPUT -vn
}

for ns in p5s p5c; do ip netns del $ns 2>/dev/null; ip netns add $ns; done
ip link add p5sv type veth peer name p5cv
ip link set p5sv netns p5s; ip link set p5cv netns p5c
ip netns exec p5s sh -c 'ip link set lo up; ip addr add 10.90.0.1/24 dev p5sv; ip link set p5sv up'
ip netns exec p5c sh -c 'ip link set lo up; ip addr add 10.90.0.2/24 dev p5cv; ip link set p5cv up'
# Pin an ingress group into the SYN's mark, then count how many reply packets
# leave still carrying it. Same field P4 exercises: 0x01000000/0x0f000000.
ip netns exec p5s iptables -t mangle -A PREROUTING -p tcp --dport 9351 \
    -j MARK --set-xmark 0x01000000/0x0f000000
ip netns exec p5s iptables -t mangle -A OUTPUT -p tcp --sport 9351 \
    -m comment --comment reply-total
ip netns exec p5s iptables -t mangle -A OUTPUT -p tcp --sport 9351 \
    -m mark --mark 0x01000000/0x0f000000 -m comment --comment reply-mark-inherited
ip netns exec p5s sh -c \
    'socat -T3 TCP-LISTEN:9351,reuseaddr,fork SYSTEM:"echo p5-payload" >/dev/null 2>&1 &'
sleep 1
p5_case 0
p5_case 1
pkill -f 'TCP-LISTEN:9351' 2>/dev/null
for ns in p5s p5c; do ip netns del $ns 2>/dev/null; done
# expect: =0 -> reply-mark-inherited 0
#         =1 -> reply-mark-inherited == reply-total (child socket carries the mark)

hdr "P6 resolver survival after a takeover (run only post-takeover)"
readlink -f /etc/resolv.conf; resolvectl status | head -30
getent hosts deb.debian.org; echo "rc=$?"
ss -lunp | grep ':53'

hdr "P7 does the kernel walk an ordered fallback when a table empties?"
# Both tables must actually hold a distinguishable default, or every `route get`
# falls straight through to main and the verdict is meaningless.
in_netns p7 '
  ip link set lo up
  for i in 1 2; do ip link add p7d$i type dummy && ip link set p7d$i up; done
  ip route add default dev p7d2 table 202
  ip route add default dev p7d1 table 201
  ip rule add pref 32000 lookup 202
  ip rule add pref 32001 lookup 201
  ip rule show
  echo "-- both up:";    ip route get 1.1.1.1
  ip route del default dev p7d2 table 202
  echo "-- 202 empty:";  ip route get 1.1.1.1'
# expect: "both up" names p7d2; "202 empty" names p7d1, NOT "unreachable"

hdr "P8 does the running xray set IP_PKTINFO on UDP inbounds?"
XRAY=$(command -v xray || echo ./bin/xray)
"$XRAY" version | head -1
echo "-- symbol presence only, NOT a verdict:"
strings "$XRAY" | grep -ci 'IP_PKTINFO\|readFrom\|oobSize' || echo 0

# Behavioural test. Topology deliberately asymmetric: the client's source
# address is off BOTH server subnets, and the server holds only a default route
# via link B. A wildcard-bound UDP socket that ignores IP_PKTINFO therefore
# answers with link B's address (10.92.0.1) instead of the address the request
# arrived on (10.91.0.1).
#
#   client 10.99.0.9/32 (lo) --- p8ca 10.91.0.2 <-> 10.91.0.1 p8sa --- server
#                            \-- p8cb 10.92.0.2 <-> 10.92.0.1 p8sb --/
#                                        (server default via 10.92.0.2)
#
# A connected route to the client would pin the reply source correctly by
# accident and make a broken responder look correct — hence the /32 off-subnet
# source. socat runs first as a falsification control: it does not set
# ip-pktinfo, so if the harness cannot catch socat it cannot clear xray.

cat > /tmp/p8-xray.json <<'JSON'
{
  "log": { "loglevel": "warning" },
  "inbounds": [{
    "tag": "udp-in",
    "listen": "0.0.0.0",
    "port": 9451,
    "protocol": "dokodemo-door",
    "settings": { "address": "127.0.0.1", "port": 9452, "network": "udp" }
  }],
  "outbounds": [{ "tag": "direct", "protocol": "freedom" }]
}
JSON

cat > /tmp/p8-resp-control.sh <<'SH'
exec socat -T5 UDP4-RECVFROM:9451,fork,reuseaddr EXEC:/bin/cat
SH

cat > /tmp/p8-resp-xray.sh <<SH
socat -T5 UDP4-RECVFROM:9452,fork,reuseaddr EXEC:/bin/cat &
exec "$XRAY" run -c /tmp/p8-xray.json
SH

p8_setup() {
  for ns in p8s p8c; do ip netns del $ns 2>/dev/null; ip netns add $ns; done
  ip link add p8sa type veth peer name p8ca
  ip link add p8sb type veth peer name p8cb
  ip link set p8sa netns p8s; ip link set p8sb netns p8s
  ip link set p8ca netns p8c; ip link set p8cb netns p8c
  ip netns exec p8s sh -c '
    ip link set lo up
    ip addr add 10.91.0.1/24 dev p8sa; ip link set p8sa up
    ip addr add 10.92.0.1/24 dev p8sb; ip link set p8sb up
    ip route add default via 10.92.0.2 dev p8sb'
  ip netns exec p8c sh -c '
    ip link set lo up
    ip addr add 10.91.0.2/24 dev p8ca; ip link set p8ca up
    ip addr add 10.92.0.2/24 dev p8cb; ip link set p8cb up
    ip addr add 10.99.0.9/32 dev lo
    sysctl -qw net.ipv4.conf.all.rp_filter=0
    sysctl -qw net.ipv4.conf.default.rp_filter=0'
}

p8_teardown() {
  pkill -f 'UDP4-RECVFROM:945' 2>/dev/null
  pkill -f p8-xray.json 2>/dev/null
  for ns in p8s p8c; do ip netns del $ns 2>/dev/null; done
}

p8_run() {
  label="$1"; responder="$2"
  p8_setup
  ip netns exec p8s sh "$responder" >/dev/null 2>&1 &
  sleep 1
  rm -f /tmp/p8.pcap
  ip netns exec p8c timeout 5 tcpdump -nn -i any -w /tmp/p8.pcap udp >/dev/null 2>&1 &
  tpid=$!
  sleep 1
  ip netns exec p8c sh -c \
    'echo p8-probe | timeout 2 socat -T2 - UDP4-SENDTO:10.91.0.1:9451,bind=10.99.0.9:40000' \
    >/dev/null 2>&1
  wait $tpid 2>/dev/null
  echo "-- $label"
  ip netns exec p8s ip route get 10.99.0.9 2>&1 | head -1
  tcpdump -nn -r /tmp/p8.pcap 2>/dev/null
  p8_teardown
}

p8_run "CONTROL: socat wildcard UDP, no ip-pktinfo (must answer from 10.92.0.1)" \
       /tmp/p8-resp-control.sh
p8_run "xray dokodemo-door UDP inbound on 0.0.0.0:9451" \
       /tmp/p8-resp-xray.sh
rm -f /tmp/p8-xray.json /tmp/p8-resp-control.sh /tmp/p8-resp-xray.sh /tmp/p8.pcap
# Read the SOURCE address of the reply to 10.99.0.9:40000.
#   src 10.92.0.1 -> responder ignores IP_PKTINFO: single-uplink only
#   src 10.91.0.1 -> responder pins the arrival address: dual-uplink capable
# The control must show 10.92.0.1, or the harness proves nothing about xray.

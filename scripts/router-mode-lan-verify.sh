#!/usr/bin/env bash
# Stage 2 deliverable check: a device plugged into the LAN gets transparent split
# routing with no client software. Run on the target with the LAN enabled.
set -u

LAN_IF="${LAN_IF:-lan0}"
DOMESTIC="${DOMESTIC_IF:?set DOMESTIC_IF}"
SECONDARY="${SECONDARY_IF:?set SECONDARY_IF}"
DOMESTIC_TEST_IP="${DOMESTIC_TEST_IP:?set DOMESTIC_TEST_IP to an address inside the domestic geoip set}"

hdr() { printf '\n===== %s =====\n' "$1"; }
lan_addr() { ip -4 -o addr show "$LAN_IF" | awk '{print $4}' | cut -d/ -f1; }

hdr "L1 the bridge exists, has its address, and is up without a member"
ip -d link show "$LAN_IF"
ip -4 addr show "$LAN_IF"

hdr "L2 forwarding and loose rp_filter on every interface in the path"
for k in net.ipv4.ip_forward net.ipv4.conf.all.rp_filter \
         "net.ipv4.conf.${LAN_IF}.rp_filter" "net.ipv4.conf.${DOMESTIC}.rp_filter" \
         "net.ipv4.conf.${SECONDARY}.rp_filter"; do
    printf '%s = %s\n' "$k" "$(sysctl -n "$k")"
done

hdr "L3 dnsmasq serves the LAN and resolved still serves the box"
ss -lunp | grep ':53'
resolvectl query deb.debian.org >/dev/null 2>&1 && echo "box resolves: ok" || echo "box resolves: FAIL"
dig +short +time=3 example.com @"$(lan_addr)" | head -2
echo "Both listeners must be present. DNSStubListener is never set: bind-interfaces"
echo "already keeps dnsmasq off 127.0.0.53."

hdr "L4 both classification layers"
echo "geoip prefixes (compiled at boot):"
nft -j list set inet nasnet ir_v4 | jq '.nftables[1].set.elem | length'
echo "domain-resolved addresses (populated by dnsmasq, empty until a client resolves):"
nft list set inet nasnet ir_dom_v4 2>&1 | head -8
dnsmasq --test --nftset=/example.com/inet#nasnet#probe >/dev/null 2>&1 \
    && echo "--nftset supported: domain layer active" \
    || echo "--nftset NOT supported: geoip layer only (expected on some builds)"
echo "Resolving a .ir name must add addresses to ir_dom_v4:"
dig +short +time=3 irna.ir @"$(lan_addr)" >/dev/null
nft list set inet nasnet ir_dom_v4 | grep -c elements

# `iif` needs carrier, so with nothing plugged in the kernel answers EINVAL.
# Fall back to the mark-only lookup, which is the half the rules actually decide.
route_for_mark() {
    local dest="$1" mark="$2"
    ip route get "$dest" from "$(lan_addr)" iif "$LAN_IF" mark "$mark" 2>/dev/null \
        || { echo "(no carrier on $LAN_IF — checking the mark alone)"
             ip route get "$dest" mark "$mark"; }
}

hdr "L5 a LAN-sourced packet to a domestic address routes out the domestic uplink"
route_for_mark "$DOMESTIC_TEST_IP" 0x10000
hdr "L6 and a foreign one out the secondary uplink"
route_for_mark 1.1.1.1 0x20000

hdr "L7 forward chain is drop, and LAN->uplink is accepted"
nft list chain inet nasnet filter_fwd

hdr "L8 masquerade on uplink egress only"
nft list chain inet nasnet nat_post

hdr "L9 the domestic segment cannot route into the LAN"
echo "MANUAL: from a host on the domestic segment, add a route to the LAN CIDR via"
echo "this box's domestic address and try to reach a LAN host. Expect no answer."

hdr "L10 LAN clients carry no tier mark and lan0 has no tier qdiscs"
tc qdisc show dev "$LAN_IF"
echo "expect no htb/fq_codel tier classes — LAN devices have no user identity"

hdr "L11 conntrack shows LAN flows carrying a group mark and no tier"
conntrack -L -o extended 2>/dev/null | grep -E 'src=10\.77\.' | head -5

hdr "L12 END TO END, MANUAL"
echo "On a laptop plugged into the LAN member port, with NO client software:"
echo "  1. it gets a lease in the DHCP range and this box as its resolver"
echo "  2. a domestic site loads and reports a domestic-ISP egress address"
echo "  3. a foreign site loads and reports the Starlink egress address"
echo "  4. restart nasnet-panel: browsing must NOT be interrupted — that is why"
echo "     classification is in the kernel and not a TPROXY inbound"

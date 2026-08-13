#!/bin/bash
# A virtual switch on a LAN port, with fake devices behind it.
#
# Topology, matching what an operator actually plugs in:
#
#   lan0 (nasnet's bridge)
#     ├── enp0s4        the real LAN port
#     └── swtrunk ──────┐ one "cable"
#                       │
#                     sw0   the switch
#                    ╱  │  ╲
#                 dev1 dev2 dev3   each in its own netns
#
# Every device is learned on lan0 through the single swtrunk port, which is the
# whole point: with a switch behind a port, all of them report that one port.
#
# Usage: fake-switch.sh up | down | status
set -u

LAN_BRIDGE="${LAN_BRIDGE:-lan0}"
SW=sw0
TRUNK=swtrunk

# mac:hostname — two real vendor prefixes and one randomized, so the device
# list shows a vendor, a name, and the "cannot be named" case.
DEVICES=(
    "b8:27:eb:11:00:01:media-server"
    "dc:a6:32:11:00:02:office-printer"
    "b2:9a:44:11:00:03:someones-phone"
)

down() {
    for i in 1 2 3; do
        ip netns del "dev$i" 2>/dev/null
        ip link del "sw$i" 2>/dev/null
    done
    ip link del "$TRUNK" 2>/dev/null
    ip link del "$SW" 2>/dev/null
    echo "switch removed"
}

up() {
    if [ ! -d "/sys/class/net/$LAN_BRIDGE" ]; then
        echo "no $LAN_BRIDGE — enable the LAN and give a port the LAN role first" >&2
        exit 1
    fi
    down >/dev/null 2>&1

    ip link add "$SW" type bridge
    # A Linux bridge always sources some traffic of its own, so it is learned on
    # lan0 and shows up in the list. That is not wrong — a managed switch is a
    # device on the network too — so give it a switch vendor's prefix and let it
    # appear as what it is. Only an unmanaged switch would be invisible.
    ip link set "$SW" address 00:0f:b5:00:00:99
    ip link set "$SW" up
    sysctl -qw "net.ipv6.conf.$SW.disable_ipv6=1" 2>/dev/null

    # The cable from the switch into the LAN.
    ip link add "$TRUNK" type veth peer name "${TRUNK}p"
    ip link set "$TRUNK" master "$LAN_BRIDGE"
    ip link set "$TRUNK" up
    ip link set "${TRUNK}p" master "$SW"
    ip link set "${TRUNK}p" up
    for n in "$TRUNK" "${TRUNK}p"; do
        sysctl -qw "net.ipv6.conf.$n.disable_ipv6=1" 2>/dev/null
    done

    local i=0
    for entry in "${DEVICES[@]}"; do
        i=$((i + 1))
        local mac="${entry%:*}" host="${entry##*:}"
        ip netns add "dev$i"
        ip link add "sw$i" type veth peer name "eth0" netns "dev$i"
        ip link set "sw$i" master "$SW"
        ip link set "sw$i" up
        sysctl -qw "net.ipv6.conf.sw$i.disable_ipv6=1" 2>/dev/null
        ip netns exec "dev$i" ip link set eth0 address "$mac"
        ip netns exec "dev$i" ip link set eth0 up
        ip netns exec "dev$i" ip link set lo up
        echo "  dev$i  $mac  $host"
    done

    # Let each take a lease, retrying: udhcpc races a freshly-up bridge port.
    printf '#!/bin/sh\nexit 0\n' > /tmp/udhcpc-noop.sh
    chmod +x /tmp/udhcpc-noop.sh
    i=0
    for entry in "${DEVICES[@]}"; do
        i=$((i + 1))
        local host="${entry##*:}"
        for try in 1 2 3; do
            ip netns exec "dev$i" busybox udhcpc -i eth0 -n -q \
                -x "hostname:$host" -t 3 -T 2 -s /tmp/udhcpc-noop.sh >/dev/null 2>&1 && break
            sleep 1
        done
        # Traffic, so the bridge learns the MAC and the device reads as present.
        ip netns exec "dev$i" ping -c2 -W1 10.77.0.1 >/dev/null 2>&1
    done
    echo
    status
}

status() {
    echo "== leases =="
    cat /var/lib/misc/dnsmasq.leases 2>/dev/null || echo "  (none)"
    echo "== what $LAN_BRIDGE has learned, per port =="
    bridge -s fdb show br "$LAN_BRIDGE" 2>/dev/null |
        grep -v -e permanent -e ' self ' | awk '{print "  " $1 "  on " $3 "  " $4 " " $5}'
}

case "${1:-up}" in
    up) up ;;
    down) down ;;
    status) status ;;
    *) echo "usage: $0 up|down|status" >&2; exit 1 ;;
esac

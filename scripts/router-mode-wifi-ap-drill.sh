#!/usr/bin/env bash
# Stage 3 AP drill on mac80211_hwsim: hostapd beacons on the first radio,
# bridged into br-test, and a client on the second radio (own netns)
# associates and takes a lease.
#
# Needs: iw hostapd wpasupplicant iwd dnsmasq-base isc-dhcp-client and
# mac80211_hwsim (linux-modules-extra-$(uname -r) on Ubuntu; the Hetzner cloud
# kernel does not ship it). hwsim radios are real mac80211 devices, so hostapd
# beacons and iwd associates for real. What it cannot do is carry a phone or
# survive a reboot — those stay manual, on hardware.
#
# Run one drill per fresh boot. Both reload mac80211_hwsim to start clean, but a
# phy whose netdev a previous run destroyed (netns teardown does that) cannot
# always be recovered without a reboot, and iwd then reports "No default
# interface for wiphy N".

set -u

for t in iw hostapd wpa_supplicant dnsmasq; do
    command -v "$t" >/dev/null || { echo "missing $t"; exit 1; }
done
modinfo mac80211_hwsim >/dev/null 2>&1 || { echo "no mac80211_hwsim for $(uname -r)"; exit 1; }
D=/tmp/wifi-drill
mkdir -p "$D"
PASS=0; FAIL=0
ok()  { echo "  ok   $*"; PASS=$((PASS+1)); }
bad() { echo "  FAIL $*"; FAIL=$((FAIL+1)); }
hdr() { printf '\n== %s\n' "$1"; }

cleanup() {
    pkill -f "hostapd $D/ap.conf" 2>/dev/null
    pkill -f 'dnsmasq.*br-test' 2>/dev/null
    ip netns pids wifi-client 2>/dev/null | xargs -r kill 2>/dev/null
    ip netns del wifi-client 2>/dev/null
    ip link del br-test 2>/dev/null
}
trap cleanup EXIT

hdr "setup"
cleanup 2>/dev/null
# Start from a known state: a previous drill's netns teardown can leave a phy
# with no netdev at all.
ip netns list 2>/dev/null | awk '{print $1}' | while read -r ns; do
    [ -n "$ns" ] && ip netns del "$ns" 2>/dev/null
done
pkill -f wpa_supplicant 2>/dev/null
systemctl stop iwd 2>/dev/null
modprobe -r mac80211_hwsim 2>/dev/null || { sleep 2; modprobe -r mac80211_hwsim 2>/dev/null; }
sleep 2
modprobe mac80211_hwsim radios=2 || { echo "no hwsim"; exit 1; }
sleep 3
# Whatever the module numbered them this boot
# Walk interface -> phy, the direction that is always populated
declare -A IF_OF_PHY
for l in /sys/class/net/*/phy80211; do
    [ -e "$l" ] || continue
    IF_OF_PHY["$(basename "$(readlink -f "$l")")"]="$(basename "$(dirname "$l")")"
done
mapfile -t PHYS < <(printf '%s\n' "${!IF_OF_PHY[@]}" | sort)
[ "${#PHYS[@]}" -ge 2 ] || { echo "need 2 radios, have ${#PHYS[@]}"; exit 1; }
AP_PHY="${PHYS[0]}"; CL_PHY="${PHYS[1]}"
AP_IF="${IF_OF_PHY[$AP_PHY]}"; CL_IF="${IF_OF_PHY[$CL_PHY]}"
echo "  AP  $AP_PHY/$AP_IF"
echo "  cli $CL_PHY/$CL_IF"
[ -n "$AP_IF" ] && [ -n "$CL_IF" ] || { echo "could not resolve interface names"; exit 1; }
iw reg set DE; sleep 1

# The AP joins a bridge, exactly as it joins lan0 in the real thing
ip link add br-test type bridge
ip addr add 10.77.0.1/24 dev br-test
ip link set br-test up
ok "bridge br-test up"

hdr "hostapd starts and reaches AP-ENABLED"
cat > "$D/ap.conf" <<CONF
interface=$AP_IF
bridge=br-test
driver=nl80211
ctrl_interface=/var/run/hostapd
ctrl_interface_group=0

ssid=nasnet-test
country_code=DE
ieee80211d=1
hw_mode=g
channel=6

ieee80211n=1
wmm_enabled=1

auth_algs=1
wpa=2
wpa_key_mgmt=WPA-PSK SAE
ieee80211w=1
rsn_pairwise=CCMP
wpa_passphrase=hunter2hunter2
CONF
hostapd -B -t -f "$D/hostapd.log" "$D/ap.conf"
sleep 4
grep -q 'AP-ENABLED' "$D/hostapd.log" && ok "AP-ENABLED" || bad "no AP-ENABLED"
grep -q 'not allowed for AP mode' "$D/hostapd.log" && bad "channel refused" || ok "no channel refusal"
grep -E 'WPA3|SAE|Enabling' "$D/hostapd.log" | tail -3 | sed 's/^/    /'

hdr "the AP is a bridge port with no address of its own"
iw dev "$AP_IF" info | grep -qE 'type AP' && ok "$AP_IF type AP" || bad "$AP_IF not in AP mode"
ip -d link show "$AP_IF" | grep -q "master br-test" && ok "enslaved to br-test by hostapd" \
    || bad "not a bridge port"
[ -z "$(ip -4 -o addr show "$AP_IF")" ] && ok "no inet address on $AP_IF" \
    || bad "$AP_IF has its own address"

hdr "DHCP on the bridge"
dnsmasq --interface=br-test --bind-interfaces --port=0 \
    --dhcp-range=10.77.0.100,10.77.0.200,1h --pid-file="$D/dnsmasq.pid" 2>"$D/dnsmasq.err"
if [ -s "$D/dnsmasq.pid" ]; then ok "dnsmasq on br-test"; else
    bad "dnsmasq did not start"; sed 's/^/    /' "$D/dnsmasq.err"; fi

hdr "a client associates and gets a lease"
ip netns add wifi-client
iw phy "$CL_PHY" set netns name wifi-client
wpa_passphrase nasnet-test hunter2hunter2 > "$D/client.conf"
ip netns exec wifi-client ip link set "$CL_IF" up
ip netns exec wifi-client wpa_supplicant -B -i "$CL_IF" -c "$D/client.conf" -f "$D/client.log"
sleep 8

ip netns exec wifi-client iw dev "$CL_IF" link | head -4 | sed 's/^/    /'
if ip netns exec wifi-client iw dev "$CL_IF" link | grep -q 'Connected to'; then
    ok "client associated"
else
    bad "client did not associate"; tail -8 "$D/client.log" | sed 's/^/    /'
fi

# The load-bearing claim: mac80211 clears carrier when unassociated
CARRIER=$(ip netns exec wifi-client cat "/sys/class/net/$CL_IF/carrier" 2>/dev/null)
[ "$CARRIER" = "1" ] && ok "carrier=1 while associated" || bad "carrier=$CARRIER while associated"

ip netns exec wifi-client dhclient -1 -v "$CL_IF" 2>&1 | tail -2 | sed 's/^/    /'
LEASE=$(ip netns exec wifi-client ip -4 -o addr show "$CL_IF" | grep -o '10\.77\.0\.[0-9]*' | head -1)
[ -n "$LEASE" ] && ok "lease $LEASE from the bridge range" || bad "no lease"
if [ -n "$LEASE" ]; then
    ip netns exec wifi-client ping -c 2 -W 2 10.77.0.1 >/dev/null 2>&1 \
        && ok "pings the bridge" || bad "cannot reach the bridge"
fi

hdr "the client is on the bridge, which is how the device list sees it"
CMAC=$(ip netns exec wifi-client cat "/sys/class/net/$CL_IF/address")
bridge fdb show br br-test | grep -qi "$CMAC" && ok "MAC $CMAC in the br-test fdb" \
    || bad "client MAC $CMAC not in the fdb"

hdr "deauth clears carrier"
ip netns exec wifi-client pkill wpa_supplicant
sleep 4
CARRIER=$(ip netns exec wifi-client cat "/sys/class/net/$CL_IF/carrier" 2>/dev/null)
[ "$CARRIER" = "0" ] && ok "carrier=0 after deauth" || bad "carrier=$CARRIER after deauth"

printf '\n== ap drill: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]

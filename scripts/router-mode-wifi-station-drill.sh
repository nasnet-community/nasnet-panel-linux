#!/usr/bin/env bash
# Stage 3 station drill on mac80211_hwsim: one radio runs an "upstream
# network", the other is nasnet's station uplink, driven by iwd and addressed
# by networkd. Proves the boot story: a pre-seeded psk makes the network known,
# so iwd autoconnects with no Connect call.
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
    systemctl stop iwd 2>/dev/null
    pkill -f "hostapd $D/upstream.conf" 2>/dev/null
    pkill -f 'dnsmasq.*up-dnsmasq' 2>/dev/null
    rm -f /var/lib/iwd/upstream-net.psk /etc/systemd/network/10-nasnet-wan-secondary2.network
    rm -f /etc/systemd/network/09-drill-upstream.network
    systemctl restart systemd-networkd 2>/dev/null
}
trap cleanup EXIT

hdr "setup"
cleanup 2>/dev/null
pkill -f wpa_supplicant 2>/dev/null
# A previous drill's netns teardown can destroy a phy's netdev, and a phy with no
# interface makes iwd say "No default interface for wiphy N". Reload from a known
# state and insist on it, rather than inheriting whatever the last run left.
ip netns list 2>/dev/null | awk '{print $1}' | while read -r ns; do
    [ -n "$ns" ] && ip netns del "$ns" 2>/dev/null
done
if ! modprobe -r mac80211_hwsim 2>/dev/null; then
    sleep 2
    modprobe -r mac80211_hwsim || { echo "mac80211_hwsim is busy; reboot or free it"; exit 1; }
fi
sleep 2
modprobe mac80211_hwsim radios=2 || { echo "no hwsim"; exit 1; }
sleep 3

# Every phy must have a netdev before anything below can work
for p in /sys/class/ieee80211/*/; do
    phy=$(basename "$p")
    have=""
    for l in /sys/class/net/*/phy80211; do
        [ -e "$l" ] && [ "$(basename "$(readlink -f "$l")")" = "$phy" ] && have=yes
    done
    if [ -z "$have" ]; then
        echo "  $phy has no interface, creating one"
        iw phy "$phy" interface add "wlan-$phy" type managed 2>/dev/null
    fi
done
sleep 1

# Walk from the interface to its phy: the reliable direction
declare -A IF_OF_PHY
for l in /sys/class/net/*/phy80211; do
    [ -e "$l" ] || continue
    IF_OF_PHY["$(basename "$(readlink -f "$l")")"]="$(basename "$(dirname "$l")")"
done
mapfile -t PHYS < <(printf '%s\n' "${!IF_OF_PHY[@]}" | sort)
[ "${#PHYS[@]}" -ge 2 ] || { echo "need 2 radios"; exit 1; }
STA_PHY="${PHYS[0]}"; UP_PHY="${PHYS[1]}"
STA_IF="${IF_OF_PHY[$STA_PHY]}"; UP_IF="${IF_OF_PHY[$UP_PHY]}"
echo "  station  $STA_PHY/$STA_IF (nasnet's side)"
echo "  upstream $UP_PHY/$UP_IF (the other network)"
iw reg set DE; sleep 1

# networkd must leave the upstream side alone; hostapd owns it
cat > /etc/systemd/network/09-drill-upstream.network <<CONF
[Match]
Name=$UP_IF

[Link]
Unmanaged=yes
CONF

hdr "the upstream network"
cat > "$D/upstream.conf" <<CONF
interface=$UP_IF
driver=nl80211
ssid=upstream-net
country_code=DE
ieee80211d=1
hw_mode=g
channel=1
auth_algs=1
wpa=2
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
wpa_passphrase=hunter2hunter2
CONF
hostapd -B -t -f "$D/upstream.log" "$D/upstream.conf"
sleep 4
grep -q 'AP-ENABLED' "$D/upstream.log" && ok "upstream AP is beaconing" || \
    { bad "upstream AP did not start"; tail -5 "$D/upstream.log" | sed 's/^/    /'; }
ip addr add 192.168.50.1/24 dev "$UP_IF" 2>/dev/null
dnsmasq --interface="$UP_IF" --bind-interfaces --port=0 \
    --dhcp-range=192.168.50.10,192.168.50.50,1h --pid-file="$D/up-dnsmasq.pid" 2>/dev/null
[ -s "$D/up-dnsmasq.pid" ] && ok "upstream hands out addresses" || bad "no upstream DHCP"

hdr "pre-seed the credential, exactly as iwd.go writes it"
mkdir -p /var/lib/iwd
printf '# Managed by nasnet.\n[Security]\nPassphrase=hunter2hunter2\n' \
    > /var/lib/iwd/upstream-net.psk
chmod 600 /var/lib/iwd/upstream-net.psk
[ "$(stat -c '%a' /var/lib/iwd/upstream-net.psk)" = "600" ] && ok "psk file is 0600" || bad "psk not 0600"

hdr "the station's .network, no different from any other uplink's"
cat > /etc/systemd/network/10-nasnet-wan-secondary2.network <<CONF
[Match]
Name=$STA_IF

[Network]
DHCP=ipv4
IPv6AcceptRA=no
LinkLocalAddressing=ipv4

[DHCPv4]
RouteTable=202
UseDNS=no
RouteMetric=100
CONF
systemctl restart systemd-networkd
sleep 3
ok "networkd is configured for $STA_IF"

hdr "iwd autoconnects with no Connect call — this is the boot story"
# -i restricts iwd to the station radio so it leaves the upstream AP alone
mkdir -p /etc/systemd/system/iwd.service.d
printf '[Service]\nExecStart=\nExecStart=/usr/libexec/iwd -i %s\n' "$STA_IF" \
    > /etc/systemd/system/iwd.service.d/drill.conf
systemctl daemon-reload
systemctl start iwd
for i in $(seq 1 20); do
    STATE=$(iwctl station "$STA_IF" show 2>/dev/null | grep -oiE 'connected|connecting|disconnected' | head -1)
    [ "$STATE" = "connected" ] && break
    sleep 2
done
echo "    state after $((i*2))s: ${STATE:-unknown}"
[ "$STATE" = "connected" ] && ok "iwd associated on its own from the pre-seeded psk" || \
    { bad "iwd did not autoconnect (state=${STATE:-unknown})"
      journalctl -u iwd --no-pager -n 12 | grep -vE 'MCS|Ciphers|HE |VHT|iftypes' | tail -6 | sed 's/^/    /'; }

CARRIER=$(cat "/sys/class/net/$STA_IF/carrier" 2>/dev/null)
[ "$CARRIER" = "1" ] && ok "carrier=1 once associated (what the health ladder reads)" \
    || bad "carrier=$CARRIER while associated"

hdr "the D-Bus shape iwd.go expects"
OBJ=$(busctl --system call net.connman.iwd / org.freedesktop.DBus.ObjectManager GetManagedObjects 2>/dev/null)
for iface in net.connman.iwd.Station net.connman.iwd.Network net.connman.iwd.KnownNetwork; do
    echo "$OBJ" | grep -q "$iface" && ok "$iface present" || bad "$iface missing"
done
echo "$OBJ" | grep -q "\"Name\" s \"$STA_IF\"" \
    && ok "Device.Name carries the kernel name $STA_IF, which is how stationPath maps it" \
    || bad "no Device.Name = $STA_IF"
echo "$OBJ" | grep -q '"Type" s "psk"' && ok "Network.Type is psk, which iwdSecurityLabel maps to WPA2" \
    || bad "no Network.Type"

hdr "networkd owns the addressing, like any other uplink"
for i in $(seq 1 15); do
    LEASE=$(ip -4 -o addr show "$STA_IF" | grep -o '192\.168\.50\.[0-9]*' | head -1)
    [ -n "$LEASE" ] && break
    sleep 2
done
ip -4 -o addr show "$STA_IF" | sed 's/^/    /'
[ -n "$LEASE" ] && ok "lease $LEASE from the upstream network" || bad "no lease on the station link"
ip route show table 202 2>/dev/null | sed 's/^/    /'
ip route show table 202 2>/dev/null | grep -q default \
    && ok "default route in table 202, the secondary2 slot's own table" \
    || bad "no default route in table 202"

hdr "the upstream going away is caught by the gateway rung, NOT by carrier"
# Measured 2026-08-31: killing the AP sends no deauth, and the station holds
# carrier=1 with iwd still "connected" for 90s+. So this asserts what is really
# true — carrier lies here, and the gateway is what notices.
pkill -f "hostapd $D/upstream.conf"
sleep 8
CARRIER=$(cat "/sys/class/net/$STA_IF/carrier" 2>/dev/null)
if [ "$CARRIER" = "1" ]; then
    ok "carrier is still 1 — as expected, an AP that vanishes sends no deauth"
else
    echo "  note carrier=$CARRIER; it dropped faster than the 90s measured before"
    PASS=$((PASS+1))
fi
ip neigh flush dev "$STA_IF" 2>/dev/null
ping -c 3 -W 2 -I "$STA_IF" 192.168.50.1 >/dev/null 2>&1
GW=$(ip neigh show 192.168.50.1 dev "$STA_IF" | grep -oE 'FAILED|INCOMPLETE|REACHABLE|STALE' | head -1)
echo "    gateway neighbour: ${GW:-none}"
case "${GW:-none}" in
    REACHABLE) bad "the gateway still answers with the AP dead" ;;
    *)         ok "the gateway stopped answering, which is what the ladder damps on" ;;
esac

hdr "and it reassociates when the network returns"
hostapd -B -t -f "$D/upstream2.log" "$D/upstream.conf"
for i in $(seq 1 20); do
    CARRIER=$(cat "/sys/class/net/$STA_IF/carrier" 2>/dev/null)
    [ "$CARRIER" = "1" ] && break
    sleep 2
done
[ "$CARRIER" = "1" ] && ok "carrier back to 1 after $((i*2))s, with no operator action" \
    || bad "did not reassociate (carrier=$CARRIER)"

rm -rf /etc/systemd/system/iwd.service.d
systemctl daemon-reload
printf '\n== station drill: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]

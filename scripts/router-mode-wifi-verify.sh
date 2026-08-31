#!/usr/bin/env bash
# Stage 3 deliverable check: a phone joins the box's Wi-Fi and gets split
# routing; a station radio is an ordinary secondary WAN. Run on the target with
# the LAN enabled and an AP configured. Works against mac80211_hwsim.
set -u

AP_IF="${AP_IF:?set AP_IF to the AP radio kernel interface name, e.g. wlan0}"
LAN_IF="${LAN_IF:-lan0}"
PHY="${PHY:-phy0}"
STA_IF="${STA_IF:-}"   # optional: the station radio, if one is configured

hdr() { printf '\n===== %s =====\n' "$1"; }

# A missing tool must not read as a pass: every ok below assumes these ran
missing=""
for tool in iw ip bridge systemctl journalctl stat; do
    command -v "$tool" >/dev/null || missing="$missing $tool"
done
if [ -n "$missing" ]; then
    echo "FAIL: not on the target box, or packages missing:$missing"
    echo "Run this on the router itself. Nothing below can be trusted."
    exit 1
fi

hdr "W1 the regulatory domain is a real country, not the world default"
iw reg get | head -5
iw reg get | grep -q 'country 00' && echo "FAIL: regdomain is 00 — an AP cannot start" || echo "ok"

hdr "W2 the radio reports AP support and at least one beaconable channel"
iw phy "$PHY" info | sed -n '/Supported interface modes/,/^\s*[A-Z]/p' | head -12
iw phy "$PHY" info | grep -E '\* [0-9]+(\.[0-9]+)? MHz' \
    | grep -v -E 'no IR|radar detection|disabled' | head -8

hdr "W3 hostapd is running and the interface is in AP mode"
systemctl is-active hostapd
iw dev "$AP_IF" info | grep -E 'type|channel|ssid'

hdr "W4 the AP is a port of the LAN bridge"
bridge link show | grep "$AP_IF" || echo "FAIL: $AP_IF is not a bridge port"
ip -d link show "$AP_IF" | grep -o "master $LAN_IF" || echo "FAIL: no bridge master"

hdr "W5 the AP has NO addressing and NO .network unit of its own"
ip -4 addr show "$AP_IF"
echo "expect no inet address on $AP_IF; the address belongs to $LAN_IF"
# Member units match on PermanentMACAddress, so search by the radio's MAC too.
AP_MAC="$(cat "/sys/class/net/$AP_IF/address" 2>/dev/null)"
grep -liE "$AP_IF|$AP_MAC" /etc/systemd/network/*nasnet-lanmember*.network 2>/dev/null \
    && echo "FAIL: a Bridge= unit fights hostapd for the port" || echo "ok: no unit"

hdr "W6 no AP+STA concurrency anywhere"
iw dev | grep -E 'Interface|type' | paste - -
echo "expect never one AP plus one managed interface on the same phy"

hdr "W7 hostapd's own view: it reached AP-ENABLED, not a channel refusal"
journalctl -u hostapd --since "10 minutes ago" --no-pager | tail -25
journalctl -u hostapd --since "10 minutes ago" --no-pager \
    | grep -q 'not allowed for AP mode' \
    && echo "FAIL: channel refused — the regdomain was set too late or the band has no clear channel" \
    || echo "ok"

hdr "W8 no file holding a PSK is world-readable"
for f in /etc/hostapd/nasnet-ap.conf /var/lib/iwd/*.psk; do
    [ -e "$f" ] || continue
    mode="$(stat -c '%a' "$f")"
    printf '%s %s ' "$f" "$mode"
    [ "$mode" = "600" ] && echo "ok" || echo "FAIL: loose permissions on a secret"
done

hdr "W9 the security the AP actually offers"
grep -E 'wpa_key_mgmt|ieee80211w|rsn_pairwise|wpa=' /etc/hostapd/nasnet-ap.conf
echo "expect WPA-PSK SAE + ieee80211w=1 on a SAE-capable binary, WPA-PSK otherwise; CCMP always"

hdr "W10 station mode is an ordinary secondary WAN"
if [ -n "$STA_IF" ]; then
    systemctl is-active iwd
    # carrier is the deauth signal: mac80211 clears it when the link drops
    cat "/sys/class/net/$STA_IF/carrier" 2>/dev/null | sed 's/^/carrier: /'
    iwctl station "$STA_IF" show 2>/dev/null | head -8
    networkctl status "$STA_IF" --no-pager 2>/dev/null | grep -E 'Address|Gateway' | head -4
else
    echo "skipped: set STA_IF to check a station uplink"
fi

hdr "W11 END TO END, MANUAL"
echo "On a phone joining the box's Wi-Fi, with NO client software:"
echo "  1. it associates and gets a lease from the LAN DHCP range"
echo "  2. a domestic site loads and reports a domestic-ISP egress address"
echo "  3. a foreign site loads and reports a secondary-WAN egress address"
echo "  4. the phone appears in the connected-devices list (randomized-MAC marker expected)"
echo "  5. restart nasnet-panel: browsing must NOT be interrupted"
echo "  6. reboot the box: the AP must come back with no operator action"
echo
echo "Step 3 is the deliverable. Steps 5 and 6 are what make it an appliance."

#!/usr/bin/env bash
# Asserts the router-mode additions are present in nasnet-tool.sh. Greps the
# script, never runs the installer, so it works on any platform.
set -euo pipefail

TOOL="${1:-nasnet-tool.sh}"
fails=0

check() {
    local desc="$1" pattern="$2"
    if grep -qF -- "$pattern" "$TOOL"; then
        printf 'ok   %s\n' "$desc"
    else
        printf 'FAIL %s (missing: %s)\n' "$desc" "$pattern"
        fails=$((fails + 1))
    fi
}

check "router packages in the apt list"        'nftables dnsmasq hostapd iw ethtool conntrack'
check "dnsmasq/hostapd left disabled"          'systemctl disable --now dnsmasq hostapd'
check "ambient capabilities on the panel unit"  'AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW'
check "capability bounding set"                 'CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE'
check "no-new-privileges"                       'NoNewPrivileges=yes'
check "router mode env var"                     'NASNET_ROUTER_MODE=1'
check "netrollback service"                     'nasnet-netrollback.service'
check "netrollback timer"                       'nasnet-netrollback.timer'
check "rollback command"                        'net rollback --if-expired'
check "rollback env file"                       'EnvironmentFile=INSTALL_DIR_PLACEHOLDER/.env'
check "timer enabled"                           'systemctl enable --now nasnet-netrollback.timer'
# One process serves DNS and DHCP, so a crash must not be permanent.
check "dnsmasq restarts on failure"             'Restart=always'
check "dnsmasq never gives up retrying"         'StartLimitIntervalSec=0'

# Every bare-metal install before this shipped without xray.
check "xray installed where the panel looks"    'XRAY_BINARY="/usr/local/bin/xray"'
check "xray config dir created"                 'XRAY_CONFIG_DIR="/usr/local/etc/xray"'
check "xray version pinned to the vendored one" 'XRAY_VERSION="${XRAY_VERSION:-26.2.6}"'
check "xray installed on a fresh deploy"        'install_xray_core || step_warn'
check "the vendored copy is checksummed"        'Vendored xray-core failed its checksum'
check "a download fallback exists"              'releases/download/v${XRAY_VERSION}/Xray-linux-${suffix}.zip'
check "unzip is a declared dependency"          'wget curl unzip build-essential'
check "geofiles are seeded"                     '_install_geofiles'

# The installer must NOT touch the network: the takeover is the first apply.
for forbidden in 'mv /etc/netplan' 'systemctl mask NetworkManager' 'DNSStubListener=no'; do
    if grep -qF -- "$forbidden" "$TOOL"; then
        printf 'FAIL installer must not touch the network (found: %s)\n' "$forbidden"
        fails=$((fails + 1))
    else
        printf 'ok   installer does not %s\n' "$forbidden"
    fi
done

bash -n "$TOOL" && printf 'ok   %s parses\n' "$TOOL"

if [[ $fails -gt 0 ]]; then
    printf '\n%d check(s) failed\n' "$fails"
    exit 1
fi
printf '\nall checks passed\n'

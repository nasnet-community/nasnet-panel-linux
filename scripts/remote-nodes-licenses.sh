#!/bin/sh
# Collects what the Remote Nodes image carries in /licenses: this repository's
# LICENSE, a NOTICES file with the licence of Go itself and of every module the
# hub and the bridge link, and a short README. Dockerfile.remote-nodes runs it,
# and then adds web-notices.txt, which the web stage writes (npm run notices in
# cmd/remote-nodes/web): the licences of the page's libraries, of the styles its
# CSS gets from Tailwind, and of its font.
#
#   scripts/remote-nodes-licenses.sh OUTDIR [package ...]
#
# Run it from the repository root with the GOOS, GOARCH and CGO_ENABLED of the
# build, since they decide which modules get linked. The modules are read from
# the module cache, so build first. SOURCE_URL overrides the repository the
# README names.
set -eu

out=${1:?usage: remote-nodes-licenses.sh OUTDIR [package ...]}
shift
[ $# -gt 0 ] || set -- ./cmd/remote-nodes ./cmd/remote-nodes-bridge
source_url=${SOURCE_URL:-https://github.com/nasnet-community/nasnet-panel-linux}

# These two are listed whether or not a program links them yet: the installer
# (MIT) and Xray-core (MPL-2.0) are what the plugin is built on.
always="github.com/nasnet-community/mtproto-remote-plugin github.com/xtls/xray-core"

mkdir -p "$out"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# one "module version dir" line per module, std and this module left out
go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' "$@" >"$tmp/linked"
for m in $always; do
	info=$(go list -m -f '{{.Version}} {{.Dir}}' "$m")
	case $info in
	*" /"*) ;;
	*) # not in the cache yet
		go mod download "$m"
		info=$(go list -m -f '{{.Version}} {{.Dir}}' "$m")
		;;
	esac
	echo "$m $info" >>"$tmp/linked"
done
LC_ALL=C sort -u "$tmp/linked" | grep . >"$tmp/modules"

notices=$out/NOTICES
cat >"$notices" <<EOF
The licences of the software built into the programs of the V2ray/Xray
plugin. The programs themselves are free software under the GNU Affero
General Public License, version 3 or later: see LICENSE.

EOF

files=0
add() { # heading file
	{
		echo "================================================================"
		echo "$1"
		echo "================================================================"
		echo
		cat "$2"
		echo
	} >>"$notices"
	files=$((files + 1))
}

goroot=$(go env GOROOT)
for f in LICENSE PATENTS; do
	add "Go $(go env GOVERSION), the standard library and runtime: $f" "$goroot/$f"
done

missing=0
while read -r mod ver dir; do
	find "$dir" -maxdepth 1 -type f \( -iname '*licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' -o -iname 'patents*' \) ! -name '*.go' |
		LC_ALL=C sort >"$tmp/files"
	if [ ! -s "$tmp/files" ]; then
		echo "remote-nodes-licenses: no licence file in $mod $ver ($dir)" >&2
		missing=1
		continue
	fi
	while read -r f; do
		add "$mod $ver: ${f##*/}" "$f"
	done <"$tmp/files"
done <"$tmp/modules"
[ "$missing" = 0 ] || exit 1

cp LICENSE "$out/LICENSE"

cat >"$out/README" <<EOF
V2ray/Xray plugin: licences and source

This image holds two programs, /remote-nodes (the hub) and
/remote-nodes-bridge. Both are built from the source in
$source_url
at the commit the image's org.opencontainers.image.revision label names
(org.opencontainers.image.source names the repository). To read the commit:

  docker inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' IMAGE

LICENSE  The GNU Affero General Public License, version 3 or later: the
         licence of the programs themselves.
NOTICES  The licence of Go, and of every third-party module linked into the
         programs, each with the version it was built with. It includes the
         plugin installer (github.com/nasnet-community/mtproto-remote-plugin,
         MIT) and Xray-core (github.com/xtls/xray-core, Mozilla Public
         License 2.0). Xray-core's source is at https://github.com/XTLS/Xray-core,
         at the commit or tag its version in NOTICES names.
web-notices.txt
         The licence of every library built into the web page, with the
         version it was built with, the licences of Tailwind CSS and
         modern-normalize, whose base styles are in the page's CSS, and
         the notice of the Vazirmatn font the page embeds a subset of.

The hub shows these files as one text at /licenses, under the address of its
page; the Notices link in the page's About card opens it. It reads them from
LICENSES_DIR, which the image sets to /licenses.
EOF

echo "remote-nodes-licenses: $(wc -l <"$tmp/modules" | tr -d ' ') modules, $files licence files, $(wc -c <"$notices" | tr -d ' ') bytes in $notices"

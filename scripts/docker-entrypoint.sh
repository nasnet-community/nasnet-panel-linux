#!/bin/sh
# Runtime ownership fix for bind-mounted data directories.
# Host bind mounts (e.g. ./data/backups) inherit host ownership; the
# container's appuser (uid 1000) cannot write unless we chown them here.
# Runs as root, then drops to appuser via su-exec.
set -e

for d in /app/data/backups /app/data/acme; do
    if [ -d "$d" ]; then
        chown -R 1000:1000 "$d" 2>/dev/null || true
    fi
done

# The single binary runs xray locally. Binding privileged ports (80/443) and
# TC bandwidth shaping and Binding privileged ports need root
if [ "${NASNET_RUN_AS_ROOT:-0}" = "1" ]; then
    exec "$@"
fi

exec su-exec appuser:appgroup "$@"

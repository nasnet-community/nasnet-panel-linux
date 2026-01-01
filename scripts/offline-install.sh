#!/usr/bin/env bash
# offline-install.sh — Bootstrap script for offline nasnet-panel installation
# This script is bundled inside the offline tarball and handles:
#   - Fresh install (default)
#   - Update (--update)
#   - Rollback (--rollback)
set -euo pipefail

# ── Constants ────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="/usr/local/nasnet-panel"
PGSQL_INSTALL_DIR="/usr/local/nasnet-pgsql"
ROLLBACK_DIR="$INSTALL_DIR/rollback"

BACKEND_SERVICE="nasnet-panel"
PGSQL_SERVICE="nasnet-postgresql"

# ── Colors ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
RESET='\033[0m'

info()  { echo -e "  ${CYAN}ℹ${RESET} $*"; }
ok()    { echo -e "  ${GREEN}✓${RESET} $*"; }
warn()  { echo -e "  ${YELLOW}⚠${RESET} $*"; }
fail()  { echo -e "  ${RED}✗${RESET} $*"; }
header(){ echo ""; echo -e "  ${BOLD}── $* ──${RESET}"; echo ""; }

# ── CLI Parsing ──────────────────────────────────────────────────────────────
MODE="install"
case "${1:-}" in
    --update)   MODE="update" ;;
    --rollback) MODE="rollback" ;;
    --help|-h)
        echo "Usage: sudo ./install.sh [--update|--rollback]"
        echo ""
        echo "  (default)    Fresh install — deploy all artifacts and run setup wizard"
        echo "  --update     Update existing installation from this bundle"
        echo "  --rollback   Restore previous version from rollback backup"
        exit 0
        ;;
esac

# ── Preflight ────────────────────────────────────────────────────────────────
preflight() {
    header "Preflight Checks"

    # Must be root
    if [[ $EUID -ne 0 ]]; then
        fail "This script must be run as root (use sudo)"
        exit 1
    fi
    ok "Running as root"

    # Must be Linux
    if [[ "$(uname -s)" != "Linux" ]]; then
        fail "This bundle only supports Linux"
        exit 1
    fi
    ok "Linux detected"

    # Must have systemd
    if ! command -v systemctl &>/dev/null; then
        fail "systemctl not found — systemd is required"
        exit 1
    fi
    ok "systemd available"

    # Read bundle manifest
    if [[ ! -f "$SCRIPT_DIR/.bundle-manifest" ]]; then
        fail "Missing .bundle-manifest — is this an offline bundle directory?"
        exit 1
    fi

    # Parse manifest (simple grep-based, no jq dependency)
    BUNDLE_VERSION=$(grep '"version"' "$SCRIPT_DIR/.bundle-manifest" | sed 's/.*: *"\([^"]*\)".*/\1/')
    BUNDLE_ARCH=$(grep '"arch"' "$SCRIPT_DIR/.bundle-manifest" | sed 's/.*: *"\([^"]*\)".*/\1/')
    BUNDLE_PGSQL_VERSION=$(grep '"pgsql_version"' "$SCRIPT_DIR/.bundle-manifest" | sed 's/.*: *"\([^"]*\)".*/\1/')

    ok "Bundle: ${BUNDLE_VERSION} (${BUNDLE_ARCH})"

    # Architecture check
    local sys_arch
    case "$(uname -m)" in
        x86_64)       sys_arch="amd64" ;;
        aarch64|arm64) sys_arch="arm64" ;;
        *) fail "Unsupported architecture: $(uname -m)"; exit 1 ;;
    esac

    if [[ "$sys_arch" != "$BUNDLE_ARCH" ]]; then
        fail "Architecture mismatch: bundle is ${BUNDLE_ARCH} but this system is ${sys_arch}"
        exit 1
    fi
    ok "Architecture: ${sys_arch}"

    # Check for curl (used for health checks)
    if ! command -v curl &>/dev/null; then
        warn "curl not found — health checks will be skipped"
    fi

    # Disk space check (warn if < 500 MB free in /usr/local)
    local free_kb
    free_kb=$(df -k /usr/local 2>/dev/null | tail -1 | awk '{print $4}')
    if [[ -n "$free_kb" ]] && (( free_kb < 512000 )); then
        warn "Low disk space: $(( free_kb / 1024 )) MB free in /usr/local (recommend >= 500 MB)"
    fi

    # Verify checksums
    header "Verifying Checksums"
    if [[ ! -f "$SCRIPT_DIR/checksums.txt" ]]; then
        fail "Missing checksums.txt"
        exit 1
    fi

    local check_ok=true
    while IFS= read -r line; do
        local expected_hash expected_file
        expected_hash=$(echo "$line" | awk '{print $1}')
        expected_file=$(echo "$line" | awk '{print $2}')
        if [[ -f "$SCRIPT_DIR/$expected_file" ]]; then
            local actual_hash
            actual_hash=$(sha256sum "$SCRIPT_DIR/$expected_file" | awk '{print $1}')
            if [[ "$actual_hash" == "$expected_hash" ]]; then
                ok "${expected_file}"
            else
                fail "${expected_file} — checksum MISMATCH"
                check_ok=false
            fi
        else
            fail "${expected_file} — file not found"
            check_ok=false
        fi
    done < "$SCRIPT_DIR/checksums.txt"

    if ! $check_ok; then
        fail "Checksum verification failed — bundle may be corrupted"
        exit 1
    fi
    ok "All checksums verified"
}

# ── Install PostgreSQL ───────────────────────────────────────────────────────
install_postgresql() {
    header "Extracting PostgreSQL Standalone"

    local pgsql_tarball
    pgsql_tarball=$(find "$SCRIPT_DIR/runtime" \( -name 'postgresql-*.tar.gz' -o -name 'pgsql-*.tar.gz' \) -print | head -1)

    if [[ -z "$pgsql_tarball" ]]; then
        fail "No PostgreSQL tarball found in runtime/"
        exit 1
    fi

    info "Extracting PostgreSQL to ${PGSQL_INSTALL_DIR}..."
    rm -rf "$PGSQL_INSTALL_DIR"
    mkdir -p "$PGSQL_INSTALL_DIR"

    # EnterpriseDB tarballs have a top-level pgsql/ directory
    tar -xzf "$pgsql_tarball" -C "$PGSQL_INSTALL_DIR" --strip-components=1

    # Create postgres system user if needed
    if id postgres &>/dev/null; then
        ok "postgres system user already exists"
    else
        info "Creating postgres system user..."
        useradd --system --shell /usr/sbin/nologin --home-dir "$PGSQL_INSTALL_DIR" postgres
        ok "postgres system user created"
    fi

    # Set ownership (but don't init data dir — deferred to wizard)
    chown -R postgres:postgres "$PGSQL_INSTALL_DIR"

    # Verify
    if LD_LIBRARY_PATH="$PGSQL_INSTALL_DIR/lib" "$PGSQL_INSTALL_DIR/bin/pg_ctl" --version &>/dev/null; then
        ok "PostgreSQL $(LD_LIBRARY_PATH="$PGSQL_INSTALL_DIR/lib" "$PGSQL_INSTALL_DIR/bin/pg_ctl" --version | awk '{print $NF}') extracted"
    else
        fail "PostgreSQL extraction verification failed"
        exit 1
    fi

    # Create log directory
    mkdir -p /var/log
    touch /var/log/nasnet-postgresql.log
    chown postgres:postgres /var/log/nasnet-postgresql.log
}

# ── Deploy Artifacts ─────────────────────────────────────────────────────────
deploy_artifacts() {
    header "Deploying Artifacts to ${INSTALL_DIR}"

    local run_user="${SUDO_USER:-root}"
    local run_group
    run_group=$(id -gn "$run_user" 2>/dev/null || echo "$run_user")

    # Create directory structure
    mkdir -p "$INSTALL_DIR"/{bin/{agent,xray},data/{backups,acme}}

    # Hub binary
    cp "$SCRIPT_DIR/bin/nasnet-panel" "$INSTALL_DIR/bin/nasnet-panel"
    chmod +x "$INSTALL_DIR/bin/nasnet-panel"
    ok "nasnet-panel binary"

    # Agent binaries (both arches)
    cp "$SCRIPT_DIR"/bin/agent/nasnet-agent-* "$INSTALL_DIR/bin/agent/"
    chmod +x "$INSTALL_DIR"/bin/agent/*
    ok "Agent binaries (amd64 + arm64)"

    # Xray binaries — placed into versioned subdirectory so BinaryManager can find them
    local xray_version
    xray_version=$(grep '"xray_version"' "$SCRIPT_DIR/.bundle-manifest" | sed 's/.*: *"\([^"]*\)".*/\1/')
    if [[ -z "$xray_version" ]]; then
        fail "Missing xray_version in .bundle-manifest"
        exit 1
    fi
    mkdir -p "$INSTALL_DIR/bin/xray/v${xray_version}"
    for f in "$SCRIPT_DIR"/bin/xray/xray-*; do
        local dest="$INSTALL_DIR/bin/xray/v${xray_version}/$(basename "$f")"
        cp "$f" "$dest"
        chmod +x "$dest"
        # Generate SHA256 checksum file expected by BinaryManager
        sha256sum "$dest" | awk '{print $1}' > "${dest}.sha256"
    done
    ok "Xray binaries (v${xray_version}, amd64 + arm64)"

    # Web panel is embedded in the Go binary — no separate deployment needed

    # nasnet-tool (Go TUI binary)
    if [[ -f "$SCRIPT_DIR/bin/nasnet-tool" ]]; then
        cp "$SCRIPT_DIR/bin/nasnet-tool" "$INSTALL_DIR/bin/nasnet-tool"
        chmod +x "$INSTALL_DIR/bin/nasnet-tool"
        ok "nasnet-tool binary"
    fi

    # nasnet-tool.sh (bash fallback)
    cp "$SCRIPT_DIR/nasnet-tool.sh" "$INSTALL_DIR/nasnet-tool.sh"
    chmod +x "$INSTALL_DIR/nasnet-tool.sh"
    ok "nasnet-tool.sh"

    # Version marker
    echo "$BUNDLE_VERSION" > "$INSTALL_DIR/.version"

    # Bundle manifest (for future reference / offline mode detection)
    cp "$SCRIPT_DIR/.bundle-manifest" "$INSTALL_DIR/.bundle-manifest"

    # Set ownership
    chown -R "$run_user:$run_group" "$INSTALL_DIR"

    ok "All artifacts deployed to ${INSTALL_DIR}"
}

# ── Backup Current Installation ──────────────────────────────────────────────
backup_current() {
    header "Backing Up Current Installation"

    if [[ ! -d "$INSTALL_DIR" ]]; then
        warn "No existing installation found at ${INSTALL_DIR}"
        return 1
    fi

    rm -rf "$ROLLBACK_DIR"
    mkdir -p "$ROLLBACK_DIR"/bin/agent
    mkdir -p "$ROLLBACK_DIR"/bin/xray

    # Back up binaries
    [[ -f "$INSTALL_DIR/bin/nasnet-panel" ]] && cp "$INSTALL_DIR/bin/nasnet-panel" "$ROLLBACK_DIR/bin/"
    cp "$INSTALL_DIR"/bin/agent/nasnet-agent-* "$ROLLBACK_DIR/bin/agent/" 2>/dev/null || true
    cp -r "$INSTALL_DIR"/bin/xray/. "$ROLLBACK_DIR/bin/xray/" 2>/dev/null || true

    # Back up PostgreSQL binaries (not data — data is preserved in place)
    if [[ -d "$PGSQL_INSTALL_DIR/bin" ]]; then
        mkdir -p "$ROLLBACK_DIR/pgsql"
        cp -r "$PGSQL_INSTALL_DIR/bin" "$ROLLBACK_DIR/pgsql/bin" 2>/dev/null || true
        cp -r "$PGSQL_INSTALL_DIR/lib" "$ROLLBACK_DIR/pgsql/lib" 2>/dev/null || true
        cp -r "$PGSQL_INSTALL_DIR/share" "$ROLLBACK_DIR/pgsql/share" 2>/dev/null || true
    fi

    # Back up version marker
    [[ -f "$INSTALL_DIR/.version" ]] && cp "$INSTALL_DIR/.version" "$ROLLBACK_DIR/.version"

    ok "Current installation backed up to ${ROLLBACK_DIR}"
}

# ── Database Backup ──────────────────────────────────────────────────────────
backup_database() {
    header "Creating Database Backup"

    local backup_dir="$INSTALL_DIR/data/backups"
    mkdir -p "$backup_dir"
    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)

    # Detect database type from .env
    local db_driver="postgres"
    if [[ -f "$INSTALL_DIR/.env" ]]; then
        db_driver=$(grep '^DB_DRIVER=' "$INSTALL_DIR/.env" 2>/dev/null | cut -d= -f2 | tr -d '"' || echo "postgres")
    fi

    if [[ "$db_driver" == "sqlite" ]]; then
        local db_path
        db_path=$(grep '^DB_PATH=' "$INSTALL_DIR/.env" 2>/dev/null | cut -d= -f2 | tr -d '"' || echo "$INSTALL_DIR/data/nasnet_panel.db")
        if [[ -f "$db_path" ]]; then
            cp "$db_path" "$backup_dir/pre_update_${timestamp}.db"
            ok "SQLite backup: pre_update_${timestamp}.db"
        else
            warn "SQLite database not found at ${db_path} — skipping backup"
        fi
    else
        # Try bundled pg_dump first, then system pg_dump
        local pg_dump_bin=""
        if [[ -x "$PGSQL_INSTALL_DIR/bin/pg_dump" ]]; then
            pg_dump_bin="$PGSQL_INSTALL_DIR/bin/pg_dump"
        elif command -v pg_dump &>/dev/null; then
            pg_dump_bin="pg_dump"
        fi

        if [[ -n "$pg_dump_bin" ]]; then
            local db_name
            db_name=$(grep '^DB_NAME=' "$INSTALL_DIR/.env" 2>/dev/null | cut -d= -f2 | tr -d '"' || echo "nasnet_panel")
            if sudo -u postgres "$pg_dump_bin" "$db_name" > "$backup_dir/pre_update_${timestamp}.sql" 2>/dev/null; then
                ok "PostgreSQL backup: pre_update_${timestamp}.sql"
            else
                warn "PostgreSQL backup failed — continuing anyway"
            fi
        else
            warn "No pg_dump available — skipping database backup"
        fi
    fi
}

# ── Update Flow ──────────────────────────────────────────────────────────────
do_update() {
    header "Updating nasnet-panel to ${BUNDLE_VERSION}"

    if [[ ! -d "$INSTALL_DIR" ]]; then
        fail "No existing installation found at ${INSTALL_DIR}"
        fail "Use './install.sh' (without --update) for fresh install"
        exit 1
    fi

    local current_version="unknown"
    [[ -f "$INSTALL_DIR/.version" ]] && current_version=$(cat "$INSTALL_DIR/.version")
    info "Current: ${current_version} → New: ${BUNDLE_VERSION}"

    backup_database
    backup_current

    # Stop services
    header "Stopping Services"
    systemctl stop "$BACKEND_SERVICE" 2>/dev/null && ok "${BACKEND_SERVICE} stopped" || true

    # Deploy new artifacts
    deploy_artifacts

    # Update PostgreSQL binaries if installed and version changed (preserve data)
    if [[ -d "$PGSQL_INSTALL_DIR/data" ]]; then
        local current_pgsql_version=""
        if [[ -x "$PGSQL_INSTALL_DIR/bin/pg_ctl" ]]; then
            current_pgsql_version=$(LD_LIBRARY_PATH="$PGSQL_INSTALL_DIR/lib" "$PGSQL_INSTALL_DIR/bin/pg_ctl" --version 2>/dev/null | awk '{print $NF}')
        fi

        if [[ "$current_pgsql_version" == *"$BUNDLE_PGSQL_VERSION"* ]]; then
            ok "PostgreSQL ${current_pgsql_version} — up to date"
        else
            # Stop bundled PostgreSQL before replacing binaries
            systemctl stop "$PGSQL_SERVICE" 2>/dev/null || true

            local pgsql_tarball
            pgsql_tarball=$(find "$SCRIPT_DIR/runtime" \( -name 'postgresql-*.tar.gz' -o -name 'pgsql-*.tar.gz' \) -print | head -1)
            if [[ -n "$pgsql_tarball" ]]; then
                info "Updating PostgreSQL binaries (preserving data)..."
                local tmp_data="/tmp/nasnet-pgsql-data-$$"
                mv "$PGSQL_INSTALL_DIR/data" "$tmp_data"
                rm -rf "$PGSQL_INSTALL_DIR"
                mkdir -p "$PGSQL_INSTALL_DIR"
                tar -xzf "$pgsql_tarball" -C "$PGSQL_INSTALL_DIR" --strip-components=1
                mv "$tmp_data" "$PGSQL_INSTALL_DIR/data"
                chown -R postgres:postgres "$PGSQL_INSTALL_DIR"
                ok "PostgreSQL binaries updated (data preserved)"
            fi

            # Restart bundled PostgreSQL
            systemctl start "$PGSQL_SERVICE" 2>/dev/null || true
        fi
    fi

    # Start services
    header "Starting Services"
    systemctl start "$BACKEND_SERVICE" 2>/dev/null && ok "${BACKEND_SERVICE} started" || fail "${BACKEND_SERVICE} start failed"

    # Health check
    header "Health Check"
    local app_port="9761"
    if [[ -f "$INSTALL_DIR/.env" ]]; then
        app_port=$(grep '^APP_PORT=' "$INSTALL_DIR/.env" 2>/dev/null | cut -d= -f2 | tr -d '"' || echo "9761")
    fi

    local retries=0
    local max_retries=15
    while (( retries < max_retries )); do
        if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
            ok "Backend health check passed"
            break
        fi
        sleep 2
        retries=$(( retries + 1 ))
    done
    if (( retries >= max_retries )); then
        warn "Backend health check did not pass within $(( max_retries * 2 ))s"
        info "Check logs: journalctl -u ${BACKEND_SERVICE} -n 30"
    fi

    # Summary
    header "Update Complete"
    echo -e "  ${BOLD}${current_version}${RESET} → ${GREEN}${BOLD}${BUNDLE_VERSION}${RESET}"
    echo ""
    info "Rollback available: sudo ./install.sh --rollback"
}

# ── Rollback Flow ────────────────────────────────────────────────────────────
do_rollback() {
    header "Rolling Back to Previous Version"

    if [[ ! -d "$ROLLBACK_DIR" ]]; then
        fail "No rollback data found at ${ROLLBACK_DIR}"
        fail "Rollback is only available after an update"
        exit 1
    fi

    local rollback_version="unknown"
    [[ -f "$ROLLBACK_DIR/.version" ]] && rollback_version=$(cat "$ROLLBACK_DIR/.version")
    info "Restoring version: ${rollback_version}"

    # Stop services
    header "Stopping Services"
    systemctl stop "$BACKEND_SERVICE" 2>/dev/null && ok "${BACKEND_SERVICE} stopped" || true
    systemctl stop "$PGSQL_SERVICE" 2>/dev/null || true

    # Restore binaries
    header "Restoring Binaries"
    [[ -f "$ROLLBACK_DIR/bin/nasnet-panel" ]] && cp "$ROLLBACK_DIR/bin/nasnet-panel" "$INSTALL_DIR/bin/nasnet-panel" && ok "nasnet-panel binary"
    cp "$ROLLBACK_DIR"/bin/agent/nasnet-agent-* "$INSTALL_DIR/bin/agent/" 2>/dev/null && ok "Agent binaries" || true
    rm -rf "$INSTALL_DIR/bin/xray" && cp -r "$ROLLBACK_DIR/bin/xray" "$INSTALL_DIR/bin/xray" 2>/dev/null && ok "Xray binaries" || true

    # Web panel is embedded in the Go binary — no separate restore needed

    # Restore PostgreSQL binaries (preserve data dir)
    if [[ -d "$ROLLBACK_DIR/pgsql/bin" ]]; then
        cp -r "$ROLLBACK_DIR/pgsql/bin"/* "$PGSQL_INSTALL_DIR/bin/" 2>/dev/null || true
        cp -r "$ROLLBACK_DIR/pgsql/lib"/* "$PGSQL_INSTALL_DIR/lib/" 2>/dev/null || true
        cp -r "$ROLLBACK_DIR/pgsql/share"/* "$PGSQL_INSTALL_DIR/share/" 2>/dev/null || true
        chown -R postgres:postgres "$PGSQL_INSTALL_DIR"
        ok "PostgreSQL binaries"
    fi

    # Restore version marker
    [[ -f "$ROLLBACK_DIR/.version" ]] && cp "$ROLLBACK_DIR/.version" "$INSTALL_DIR/.version"

    # Start services
    header "Starting Services"
    systemctl start "$PGSQL_SERVICE" 2>/dev/null || true
    systemctl start "$BACKEND_SERVICE" 2>/dev/null && ok "${BACKEND_SERVICE} started" || fail "${BACKEND_SERVICE} start failed"

    # Health check
    local app_port="9761"
    if [[ -f "$INSTALL_DIR/.env" ]]; then
        app_port=$(grep '^APP_PORT=' "$INSTALL_DIR/.env" 2>/dev/null | cut -d= -f2 | tr -d '"' || echo "9761")
    fi

    local retries=0
    while (( retries < 10 )); do
        if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
            ok "Backend health check passed"
            break
        fi
        sleep 2
        retries=$(( retries + 1 ))
    done

    header "Rollback Complete"
    info "Restored version: ${rollback_version}"
}

# ── Fresh Install Flow ───────────────────────────────────────────────────────
do_install() {
    header "Installing nasnet-panel ${BUNDLE_VERSION}"

    # Check for existing installation
    if [[ -d "$INSTALL_DIR" ]] && [[ -f "$INSTALL_DIR/bin/nasnet-panel" ]]; then
        local current_version="unknown"
        [[ -f "$INSTALL_DIR/.version" ]] && current_version=$(cat "$INSTALL_DIR/.version")
        warn "Existing installation detected (${current_version}) at ${INSTALL_DIR}"
        echo ""
        echo -e "  To update: ${BOLD}sudo ./install.sh --update${RESET}"
        echo ""
        read -rp "  Continue with fresh install anyway? This will overwrite existing files. [y/N] " confirm
        if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
            info "Cancelled"
            exit 0
        fi
    fi

    install_postgresql
    deploy_artifacts

    # Launch nasnet-tool.sh in offline mode for interactive setup
    header "Launching Setup Wizard"
    info "Handing off to nasnet-tool.sh for interactive configuration..."
    echo ""

    exec "$INSTALL_DIR/nasnet-tool.sh" --offline
}

# ── Main ─────────────────────────────────────────────────────────────────────
echo ""
echo -e "  ${BOLD}nasnet-panel Offline Installer${RESET}"
echo -e "  ${DIM}─────────────────────────────${RESET}"
echo ""

preflight

case "$MODE" in
    install)  do_install ;;
    update)   do_update ;;
    rollback) do_rollback ;;
esac

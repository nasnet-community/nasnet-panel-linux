#!/usr/bin/env bash
# nasnet-tool.sh - Interactive Admin Operations Tool for nasnet-panel
# Pure bash TUI with arrow-key navigation, colored output, spinners, and formatted tables.
set -euo pipefail

# ──────────────────────────────────────────────────────────────────────────────
# Section 1: Constants
# ──────────────────────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
BACKUP_DIR="$PROJECT_DIR/data/backups"
ENV_FILE="$PROJECT_DIR/.env"
ENV_EXAMPLE="$PROJECT_DIR/.env.example"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.yml"
SQLITE_COMPOSE_FILE="$PROJECT_DIR/docker-compose.sqlite.yml"

CONTAINER_BACKEND="nasnet_panel_backend"
CONTAINER_DB="nasnet_panel_db"

SYSTEMD_SERVICE="nasnet-panel"
SYSTEMD_UNIT_FILE="/etc/systemd/system/${SYSTEMD_SERVICE}.service"
# Wizard-created services (bare-metal deploy)
BACKEND_SERVICE="nasnet-panel"
INSTALL_DIR="/usr/local/nasnet-panel"
BACKEND_BINARY="$INSTALL_DIR/bin/nasnet-panel"

# xray-core. Compiled into the panel (internal/agent/config), not read from .env.
XRAY_BINARY="/usr/local/bin/xray"
XRAY_CONFIG_DIR="/usr/local/etc/xray"
# Must match the panel default and bin/xray/, or we ship a core it never expects.
XRAY_VERSION="${XRAY_VERSION:-26.2.6}"

# GitHub release auto-update settings
GITHUB_REPO="${GITHUB_REPO:-nasnet-community/nasnet-panel-linux}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"

# Offline mode (set by --offline flag or detected from .bundle-manifest)
OFFLINE_MODE="${OFFLINE_MODE:-false}"
PGSQL_INSTALL_DIR="/usr/local/nasnet-pgsql"
PGSQL_SERVICE="nasnet-postgresql"

# Ensure common tool paths are available (Go, Node via nvm/fnm, pnpm, etc.)
# /etc/profile.d/ scripts are only sourced by login shells, so non-login
# sub-shells spawned by run_logged/bash -c won't see them.
[[ -d /usr/local/go/bin ]] && export PATH="/usr/local/go/bin:$PATH"
[[ -d "$HOME/.local/bin" ]] && export PATH="$HOME/.local/bin:$PATH"

# Colors
RESET='\033[0m'
BOLD='\033[1m'
DIM='\033[2m'
UNDERLINE='\033[4m'
INVERT='\033[7m'

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'

BG_RED='\033[41m'
BG_GREEN='\033[42m'
BG_BLUE='\033[44m'

# Symbols
CHECK="${GREEN}✔${RESET}"
CROSS="${RED}✘${RESET}"
ARROW="${CYAN}▸${RESET}"
DOT="${DIM}·${RESET}"
WARN="${YELLOW}⚠${RESET}"

# ──────────────────────────────────────────────────────────────────────────────
# Section 2: UI Primitives
# ──────────────────────────────────────────────────────────────────────────────

# draw_box "Title"
draw_box() {
    local title="$1"
    local width=46
    local pad=$(( (width - ${#title} - 2) / 2 ))
    local pad_r=$(( width - ${#title} - 2 - pad ))
    echo ""
    echo -e "  ${CYAN}╔$(printf '═%.0s' $(seq 1 $width))╗${RESET}"
    echo -e "  ${CYAN}║${RESET}$(printf ' %.0s' $(seq 1 $pad))${BOLD}${WHITE}${title}${RESET}$(printf ' %.0s' $(seq 1 $pad_r))  ${CYAN}║${RESET}"
    echo -e "  ${CYAN}╚$(printf '═%.0s' $(seq 1 $width))╝${RESET}"
    echo ""
}

# draw_header "Section Name"
draw_header() {
    local title="$1"
    echo ""
    echo -e "  ${BOLD}${CYAN}── ${title} ──${RESET}"
    echo ""
}

# draw_separator
draw_separator() {
    echo -e "  ${DIM}$(printf '─%.0s' $(seq 1 50))${RESET}"
}

# step_ok "message"
step_ok() {
    echo -e "  ${CHECK} $1"
}

# step_fail "message"
step_fail() {
    echo -e "  ${CROSS} $1"
}

# step_warn "message"
step_warn() {
    echo -e "  ${WARN} $1"
}

# step_info "message"
step_info() {
    echo -e "  ${ARROW} $1"
}

# spinner "message" command [args...]
# Runs command in background and shows animated spinner
spinner() {
    local msg="$1"
    shift
    local frames=('⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏')
    local tmp
    tmp=$(mktemp)

    # Run command in background, capture output
    "$@" > "$tmp" 2>&1 &
    local pid=$!
    local i=0

    tput civis 2>/dev/null || true
    while kill -0 "$pid" 2>/dev/null; do
        printf "\r  ${CYAN}%s${RESET} %s" "${frames[$i]}" "$msg"
        i=$(( (i + 1) % ${#frames[@]} ))
        sleep 0.1
    done

    wait "$pid"
    local exit_code=$?
    tput cnorm 2>/dev/null || true

    if [[ $exit_code -eq 0 ]]; then
        printf "\r  ${CHECK} %s\n" "$msg"
    else
        printf "\r  ${CROSS} %s\n" "$msg"
        local output
        output=$(cat "$tmp")
        if [[ -n "$output" ]]; then
            echo -e "    ${DIM}${output}${RESET}" | head -5
        fi
    fi

    rm -f "$tmp"
    return $exit_code
}

# run_logged "message" command [args...]
# Runs command with stdout/stderr streamed live, shows ✔/✘ after.
run_logged() {
    local msg="$1"
    shift
    echo ""
    echo -e "  ${CYAN}▸${RESET} ${BOLD}${msg}${RESET}"
    echo -e "  ${DIM}$(printf '─%.0s' $(seq 1 50))${RESET}"

    if "$@"; then
        echo -e "  ${DIM}$(printf '─%.0s' $(seq 1 50))${RESET}"
        echo -e "  ${CHECK} ${msg}"
        return 0
    else
        echo -e "  ${DIM}$(printf '─%.0s' $(seq 1 50))${RESET}"
        echo -e "  ${CROSS} ${msg}"
        return 1
    fi
}

# draw_table header_row data_rows...
# Usage: draw_table "Col1|Col2|Col3" "val1|val2|val3" "val4|val5|val6"
draw_table() {
    local header="$1"
    shift
    local rows=("$@")

    IFS='|' read -ra hcols <<< "$header"
    local ncols=${#hcols[@]}

    # Calculate column widths
    local -a widths=()
    for (( c=0; c<ncols; c++ )); do
        widths[$c]=${#hcols[$c]}
    done

    for row in "${rows[@]}"; do
        IFS='|' read -ra rcols <<< "$row"
        for (( c=0; c<ncols; c++ )); do
            local val="${rcols[$c]:-}"
            # Strip ANSI codes for length calculation
            local stripped
            stripped=$(echo -e "$val" | sed 's/\x1b\[[0-9;]*m//g')
            if (( ${#stripped} > ${widths[$c]} )); then
                widths[$c]=${#stripped}
            fi
        done
    done

    # Add padding
    for (( c=0; c<ncols; c++ )); do
        widths[$c]=$(( ${widths[$c]} + 2 ))
    done

    # Build top border
    local border_top="  ┌"
    local border_mid="  ├"
    local border_bot="  └"
    for (( c=0; c<ncols; c++ )); do
        local seg
        seg=$(printf '─%.0s' $(seq 1 "${widths[$c]}"))
        border_top+="$seg"
        border_mid+="$seg"
        border_bot+="$seg"
        if (( c < ncols - 1 )); then
            border_top+="┬"
            border_mid+="┼"
            border_bot+="┴"
        fi
    done
    border_top+="┐"
    border_mid+="┤"
    border_bot+="┘"

    echo -e "  ${DIM}${border_top#  }${RESET}"

    # Header row
    local hline="  │"
    for (( c=0; c<ncols; c++ )); do
        local w=${widths[$c]}
        local val=" ${hcols[$c]}"
        local pad=$(( w - ${#val} ))
        hline+="${BOLD}${val}$(printf ' %.0s' $(seq 1 $pad))${RESET}│"
    done
    echo -e "$hline"

    echo -e "  ${DIM}${border_mid#  }${RESET}"

    # Data rows
    for row in "${rows[@]}"; do
        IFS='|' read -ra rcols <<< "$row"
        local rline="  │"
        for (( c=0; c<ncols; c++ )); do
            local w=${widths[$c]}
            local val=" ${rcols[$c]:-}"
            local stripped
            stripped=$(echo -e "$val" | sed 's/\x1b\[[0-9;]*m//g')
            local pad=$(( w - ${#stripped} ))
            if (( pad < 0 )); then pad=0; fi
            rline+="${val}$(printf ' %.0s' $(seq 1 $pad))│"
        done
        echo -e "$rline"
    done

    echo -e "  ${DIM}${border_bot#  }${RESET}"
}

# press_any_key
press_any_key() {
    echo ""
    echo -ne "  ${DIM}Press any key to continue...${RESET}"
    read -rsn1
    echo ""
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 3: Menu Engine
# ──────────────────────────────────────────────────────────────────────────────

# arrow_menu "Title" result_var "Option 1" "Option 2" ...
# Sets the named variable to the selected index (0-based), or -1 if escaped.
# Compatible with Bash 3.2+ (no namerefs).
arrow_menu() {
    local title="$1"
    local result_var="$2"
    shift 2
    local options=("$@")
    local count=${#options[@]}
    local selected=0

    tput civis 2>/dev/null || true

    while true; do
        clear
        draw_box "nasnet-panel Admin Tool"

        if [[ -n "$title" ]]; then
            echo -e "  ${BOLD}${WHITE}${title}${RESET}"
            echo ""
        fi

        for (( i=0; i<count; i++ )); do
            if [[ $i -eq $selected ]]; then
                echo -e "  ${INVERT}${CYAN} ▸ ${options[$i]} ${RESET}"
            else
                echo -e "    ${DIM}${options[$i]}${RESET}"
            fi
        done

        echo ""
        echo -e "  ${DIM}↑/↓ Navigate  ⏎ Select  q/Esc Back${RESET}"

        read -rsn1 key
        case "$key" in
            $'\x1b')
                local seq=""
                read -rsn1 -t 1 _c1 || true
                read -rsn1 -t 1 _c2 || true
                seq="${_c1:-}${_c2:-}"
                case "$seq" in
                    '[A') # Up arrow
                        [[ $selected -gt 0 ]] && selected=$(( selected - 1 )) || true
                        ;;
                    '[B') # Down arrow
                        [[ $selected -lt $(( count - 1 )) ]] && selected=$(( selected + 1 )) || true
                        ;;
                esac
                # Bare Escape (no follow-up sequence)
                if [[ -z "$seq" ]]; then
                    eval "$result_var=-1"
                    tput cnorm 2>/dev/null || true
                    return
                fi
                ;;
            'q'|'Q')
                eval "$result_var=-1"
                tput cnorm 2>/dev/null || true
                return
                ;;
            '') # Enter
                eval "$result_var=$selected"
                tput cnorm 2>/dev/null || true
                return
                ;;
            'k') # vim up
                [[ $selected -gt 0 ]] && selected=$(( selected - 1 )) || true
                ;;
            'j') # vim down
                [[ $selected -lt $(( count - 1 )) ]] && selected=$(( selected + 1 )) || true
                ;;
        esac
    done
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 4: Environment Loading
# ──────────────────────────────────────────────────────────────────────────────

load_env() {
    if [[ -f "$ENV_FILE" ]]; then
        # Temporarily disable nounset/errexit — .env values may contain
        # characters that bash interprets (e.g. $2a in bcrypt hashes).
        # Single-quoted values handle most cases, but this is a safety net.
        set +eu
        set -a
        # shellcheck disable=SC1090
        source "$ENV_FILE"
        set +a
        set -eu
    fi
    # For systemd deployments, runtime data lives in INSTALL_DIR
    if [[ "${DEPLOY_MODE:-}" == "systemd" ]] && [[ -d "$INSTALL_DIR" ]]; then
        BACKUP_DIR="$INSTALL_DIR/data/backups"
    fi
}

get_db_driver()   { echo "${DB_DRIVER:-postgres}"; }
is_sqlite()       { [[ "$(get_db_driver)" == "sqlite" ]]; }
get_db_user()     { echo "${DB_USER:-postgres}"; }
get_db_password() { echo "${DB_PASSWORD:-postgres}"; }
get_db_name()     { echo "${DB_NAME:-nasnet_panel}"; }
get_db_host()     { echo "${DB_HOST:-localhost}"; }
get_db_port()     { echo "${DB_PORT:-5432}"; }
get_db_path()     { echo "${DB_PATH:-/app/data/nasnet_panel.db}"; }
get_app_port()    { echo "${APP_PORT:-9761}"; }

# Export VERSION/COMMIT/BUILD_TIME so compose can interpolate them
# into build.args for the app service.
_export_build_env() {
    local v c
    v=$(git -C "$PROJECT_DIR" describe --tags --always --dirty 2>/dev/null || echo "dev")
    c=$(git -C "$PROJECT_DIR" rev-parse --short HEAD 2>/dev/null || echo "unknown")
    export VERSION="$v"
    export COMMIT="$c"
    export BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}

# UID/GID baked into the Dockerfile for the appuser. Host bind mounts must
# be writable by this UID.
CONTAINER_APP_UID=1000

# Create host bind-mount dirs with ownership matching the container appuser.
# Falls back to chmod 0777 when chown fails (non-root invocation).
_prepare_docker_bind_mounts() {
    local dirs=("$PROJECT_DIR/data/backups")
    local d
    for d in "${dirs[@]}"; do
        mkdir -p "$d" 2>/dev/null || sudo mkdir -p "$d"
        if ! chown "$CONTAINER_APP_UID:$CONTAINER_APP_UID" "$d" 2>/dev/null; then
            if ! sudo chown "$CONTAINER_APP_UID:$CONTAINER_APP_UID" "$d" 2>/dev/null; then
                chmod 0777 "$d" 2>/dev/null || sudo chmod 0777 "$d"
            fi
        fi
    done
}

_compose_cmd_with_files() {
    local compose_cmd
    compose_cmd=$(get_compose_cmd)
    if is_sqlite; then
        $compose_cmd -f "$COMPOSE_FILE" -f "$SQLITE_COMPOSE_FILE" --project-directory "$PROJECT_DIR" "$@"
    else
        $compose_cmd -f "$COMPOSE_FILE" --project-directory "$PROJECT_DIR" "$@"
    fi
}

is_docker_running() {
    docker info &>/dev/null
}

get_compose_cmd() {
    if docker compose version &>/dev/null 2>&1; then
        echo "docker compose"
    elif command -v docker-compose &>/dev/null; then
        echo "docker-compose"
    else
        echo ""
    fi
}

require_docker() {
    if ! is_docker_running; then
        step_fail "Docker is not running"
        press_any_key
        return 1
    fi
    local compose_cmd
    compose_cmd=$(get_compose_cmd)
    if [[ -z "$compose_cmd" ]]; then
        step_fail "docker compose not found"
        press_any_key
        return 1
    fi
    return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 5: Mode-Aware DB Helpers
# ──────────────────────────────────────────────────────────────────────────────

# Get current deploy mode (defaults to docker)
_deploy_mode() {
    echo "${DEPLOY_MODE:-docker}"
}

# Sync .env to INSTALL_DIR when in systemd mode
_sync_env_to_install_dir() {
    if [[ "$(_deploy_mode)" == "systemd" ]] && [[ -d "$INSTALL_DIR" ]]; then
        sudo cp "$ENV_FILE" "$INSTALL_DIR/.env"
        sudo chmod 600 "$INSTALL_DIR/.env"
        local run_user="${SUDO_USER:-$(whoami)}"
        sudo chown "$run_user:" "$INSTALL_DIR/.env" 2>/dev/null || true
    fi
}

# Execute SQL command (mode-aware, driver-aware)
psql_exec() {
    if is_sqlite; then
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            sqlite3 "$(get_db_path)" "$@"
        else
            docker exec -i "$CONTAINER_BACKEND" sqlite3 "$(get_db_path)" "$@"
        fi
    else
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            PGPASSWORD="$(get_db_password)" psql \
                -h "$(get_db_host)" \
                -U "$(get_db_user)" \
                -d "$(get_db_name)" \
                "$@"
        else
            docker exec -i "$CONTAINER_DB" psql \
                -U "$(get_db_user)" \
                -d "$(get_db_name)" \
                "$@"
        fi
    fi
}

# Database dump (mode-aware, driver-aware)
pg_dump_exec() {
    if is_sqlite; then
        # SQLite: output is binary copy, not SQL dump
        return 0
    fi
    if [[ "$(_deploy_mode)" == "systemd" ]]; then
        PGPASSWORD="$(get_db_password)" pg_dump \
            -h "$(get_db_host)" \
            -U "$(get_db_user)" \
            -d "$(get_db_name)" \
            --no-owner \
            --no-privileges
    else
        docker exec "$CONTAINER_DB" pg_dump \
            -U "$(get_db_user)" \
            -d "$(get_db_name)" \
            --no-owner \
            --no-privileges
    fi
}

# Dump database to a file (for use with spinner)
pg_dump_to_file() {
    local filepath="$1"
    if is_sqlite; then
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            cp "$(get_db_path)" "$filepath"
        else
            docker cp "$CONTAINER_BACKEND:$(get_db_path)" "$filepath"
        fi
    else
        pg_dump_exec > "$filepath"
    fi
}

# Restore database from file (for use with spinner)
psql_restore_from_file() {
    local filepath="$1"
    if is_sqlite; then
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            cp "$filepath" "$(get_db_path)"
        else
            docker cp "$filepath" "$CONTAINER_BACKEND:$(get_db_path)"
        fi
    else
        cat "$filepath" | psql_exec --single-transaction -q
    fi
}

# Check if database is accessible (mode-aware, driver-aware)
require_db() {
    if is_sqlite; then
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            if [[ ! -f "$(get_db_path)" ]]; then
                step_fail "SQLite database file not found at $(get_db_path)"
                press_any_key
                return 1
            fi
        else
            if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_BACKEND}$"; then
                step_fail "Backend container (${CONTAINER_BACKEND}) is not running"
                press_any_key
                return 1
            fi
        fi
    else
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            if ! PGPASSWORD="$(get_db_password)" pg_isready -h "$(get_db_host)" -U "$(get_db_user)" &>/dev/null; then
                step_fail "PostgreSQL is not running or not accessible"
                press_any_key
                return 1
            fi
        else
            if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_DB}$"; then
                step_fail "Database container (${CONTAINER_DB}) is not running"
                press_any_key
                return 1
            fi
        fi
    fi
    return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6: Confirmation Helpers
# ──────────────────────────────────────────────────────────────────────────────

# confirm_action "message"
# Returns 0 if confirmed, 1 otherwise
confirm_action() {
    local msg="$1"
    echo ""
    echo -ne "  ${YELLOW}${msg}${RESET} [y/N] "
    read -r answer
    [[ "$answer" =~ ^[Yy]$ ]]
}

# confirm_dangerous "message" "CONFIRMATION_WORD"
# Returns 0 if the user types the exact confirmation word
confirm_dangerous() {
    local msg="$1"
    local word="$2"
    echo ""
    echo -e "  ${BG_RED}${WHITE}${BOLD} DANGER ${RESET} ${RED}${msg}${RESET}"
    echo ""
    echo -ne "  Type ${BOLD}${word}${RESET} to confirm: "
    read -r input
    [[ "$input" == "$word" ]]
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6.5: Setup Wizard — Utility Functions
# ──────────────────────────────────────────────────────────────────────────────

wizard_gen_secret() {
    openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | od -An -tx1 | tr -d ' \n' | head -c 64
}

wizard_gen_password() {
    openssl rand -base64 24 2>/dev/null | tr -d '\n' || head -c 32 /dev/urandom | base64 | tr -d '\n/+=' | head -c 32
}

wizard_gen_bcrypt() {
    local password="$1"
    if command -v htpasswd &>/dev/null; then
        htpasswd -nbBC 10 "" "$password" | tr -d ':\n' | sed 's/$2y/$2a/'
    elif [[ -x "$INSTALL_DIR/bin/nasnet-panel" ]]; then
        "$INSTALL_DIR/bin/nasnet-panel" hash-password "$password" 2>/dev/null
    elif is_docker_running; then
        docker run --rm httpd:2-alpine htpasswd -nbBC 10 "" "$password" 2>/dev/null | tr -d ':\n' | sed 's/$2y/$2a/'
    else
        echo ""
        return 1
    fi
}

wizard_detect_ip() {
    local ip=""
    if [[ "$OFFLINE_MODE" == "true" ]]; then
        # Air-gapped: skip external APIs, use local detection only
        ip=$(hostname -I 2>/dev/null | awk '{print $1}') || ip=""
    else
        ip=$(curl -sf --max-time 5 https://api.ipify.org 2>/dev/null) \
            || ip=$(curl -sf --max-time 5 https://ifconfig.me 2>/dev/null) \
            || ip=$(hostname -I 2>/dev/null | awk '{print $1}') \
            || ip=""
    fi
    echo "$ip"
}

wizard_install_docker() {
    if [[ "$(uname)" != "Linux" ]]; then
        step_fail "Automatic Docker install is only supported on Linux"
        step_info "Please install Docker manually: https://docs.docker.com/get-docker/"
        return 1
    fi

    step_info "Installing Docker via official script..."
    if curl -fsSL https://get.docker.com | sh; then
        step_ok "Docker installed"
        # Start Docker service
        if command -v systemctl &>/dev/null; then
            sudo systemctl enable docker --now 2>/dev/null || true
        fi
        # Add current user to docker group
        if [[ -n "${SUDO_USER:-}" ]]; then
            sudo usermod -aG docker "$SUDO_USER" 2>/dev/null || true
            step_info "Added $SUDO_USER to docker group (re-login required for group change)"
        fi
        return 0
    else
        step_fail "Docker installation failed"
        return 1
    fi
}

wizard_read_env_value() {
    local key="$1"
    local file="${2:-$ENV_FILE}"
    if [[ -f "$file" ]]; then
        grep "^${key}=" "$file" 2>/dev/null | head -1 | cut -d'=' -f2-
    fi
}

wizard_mask_secret() {
    local val="$1"
    local show="${2:-4}"
    if [[ ${#val} -le $show ]]; then
        printf '%s' "****"
    else
        printf '%s%s' "${val:0:$show}" "$(printf '*%.0s' $(seq 1 $(( ${#val} - show ))))"
    fi
}

# Generate a random available port in the range 10000-60000.
# Tries up to 20 times to find an unused port.
# Usage: local port; port=$(wizard_random_port)
wizard_random_port() {
    local port attempts=0
    while (( attempts < 20 )); do
        port=$(( RANDOM % 50001 + 10000 ))
        # Check if port is in use via ss, netstat, or /proc/net fallback
        if command -v ss &>/dev/null; then
            if ! ss -tlnH 2>/dev/null | grep -qE ":${port}\b"; then
                echo "$port"
                return 0
            fi
        elif command -v netstat &>/dev/null; then
            if ! netstat -tln 2>/dev/null | grep -qE ":${port}\b"; then
                echo "$port"
                return 0
            fi
        else
            # No tool available — accept the port and hope for the best
            echo "$port"
            return 0
        fi
        (( attempts++ ))
    done
    # Exhausted attempts — return last generated port
    echo "$port"
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6.6: Setup Wizard — Prerequisite Installers
# ──────────────────────────────────────────────────────────────────────────────

# Required Go version (major.minor only, for download URL)
WIZARD_GO_VERSION="1.26"
# Required Node.js major version
WIZARD_NODE_MAJOR="22"

# Detect deployment mode from existing .env
wizard_get_deploy_mode() {
    if [[ -f "$ENV_FILE" ]]; then
        grep "^DEPLOY_MODE=" "$ENV_FILE" 2>/dev/null | cut -d'=' -f2- || echo ""
    fi
}

# ── Docker prerequisites ─────────────────────────────────────────────────────

wizard_prereqs_docker() {
    draw_header "Prerequisites — Docker Mode"

    # Check Docker
    local docker_ok=false
    if command -v docker &>/dev/null && docker info &>/dev/null 2>&1; then
        local docker_ver
        docker_ver=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
        step_ok "Docker ${docker_ver}"
        docker_ok=true
    else
        step_fail "Docker not found or not running"
        if confirm_action "Install Docker now?"; then
            if wizard_install_docker; then
                docker_ok=true
            fi
        fi
    fi

    if ! $docker_ok; then
        step_fail "Docker is required — cannot continue"
        return 1
    fi

    # Check Docker Compose
    local compose_cmd
    compose_cmd=$(get_compose_cmd)
    if [[ -n "$compose_cmd" ]]; then
        local compose_ver
        compose_ver=$(docker compose version --short 2>/dev/null || docker-compose version --short 2>/dev/null || echo "unknown")
        step_ok "Docker Compose ${compose_ver}"
    else
        step_fail "Docker Compose not found — cannot continue"
        step_info "Install: https://docs.docker.com/compose/install/"
        return 1
    fi

    # Check openssl
    if command -v openssl &>/dev/null; then
        step_ok "openssl"
    else
        step_warn "openssl not found — will use fallback for secret generation"
    fi

    # Check htpasswd
    if command -v htpasswd &>/dev/null; then
        step_ok "htpasswd"
    else
        if is_docker_running; then
            step_warn "htpasswd not found — will use Docker fallback for bcrypt"
        else
            step_fail "htpasswd not found and Docker not available for fallback"
            step_info "Install: apt install apache2-utils"
            return 1
        fi
    fi

    # Check git
    if command -v git &>/dev/null; then
        step_ok "git"
    else
        step_warn "git not found — updates will not work"
    fi

    return 0
}

# ── Systemd / bare-metal prerequisites ────────────────────────────────────────

wizard_prereqs_systemd() {
    local db_driver="${1:-postgres}"
    draw_header "Prerequisites — Systemd (Bare-Metal) Mode"

    # ── Offline mode: skip all downloads, just verify deployed artifacts ────
    if [[ "$OFFLINE_MODE" == "true" ]]; then
        draw_header "Prerequisites — Offline Mode"

        if [[ "$(uname)" != "Linux" ]]; then
            step_fail "Only Linux is supported"
            return 1
        fi
        step_ok "Linux"

        if ! command -v systemctl &>/dev/null; then
            step_fail "systemctl not found — systemd is required"
            return 1
        fi
        step_ok "systemd"

        if [[ -x "$INSTALL_DIR/bin/nasnet-panel" ]]; then
            step_ok "nasnet-panel binary"
        else
            step_fail "nasnet-panel binary not found at ${INSTALL_DIR}/bin/nasnet-panel"
            return 1
        fi

        if [[ "$db_driver" != "sqlite" ]]; then
            if [[ -x "$PGSQL_INSTALL_DIR/bin/pg_ctl" ]]; then
                step_ok "PostgreSQL standalone (${PGSQL_INSTALL_DIR})"
            else
                step_fail "PostgreSQL not found at ${PGSQL_INSTALL_DIR}"
                return 1
            fi
        else
            step_ok "SQLite — no database server needed"
        fi

        echo ""
        step_ok "All prerequisites verified (offline mode)"
        return 0
    fi

    if [[ "$(uname)" != "Linux" ]]; then
        step_fail "Systemd mode is only supported on Linux"
        return 1
    fi

    if ! command -v systemctl &>/dev/null; then
        step_fail "systemctl not found — systemd is not available on this system"
        return 1
    fi

    if ! command -v apt-get &>/dev/null; then
        step_fail "apt-get not found — only Debian/Ubuntu is supported for automatic setup"
        step_info "For other distros, install dependencies manually and use Docker mode"
        return 1
    fi

    # ── apt update ────────────────────────────────────────────────────────
    if run_logged "Running apt update" sudo apt-get update; then
        true
    else
        step_fail "apt update failed"
        return 1
    fi

    # ── System packages ───────────────────────────────────────────────────
    local SYSTEM_PKGS=(git openssl apache2-utils ca-certificates wget curl unzip build-essential)
    SYSTEM_PKGS+=(nftables dnsmasq hostapd iw ethtool conntrack)
    local to_install=()

    for pkg in "${SYSTEM_PKGS[@]}"; do
        if dpkg -s "$pkg" &>/dev/null; then
            step_ok "${pkg}"
        else
            to_install+=("$pkg")
        fi
    done

    if [[ ${#to_install[@]} -gt 0 ]]; then
        if run_logged "Installing system packages: ${to_install[*]}" sudo apt-get install -y "${to_install[@]}"; then
            true
        else
            step_fail "Failed to install system packages"
            return 1
        fi
    fi

    # nasnet owns dnsmasq's and hostapd's lifecycle
    sudo systemctl disable --now dnsmasq hostapd 2>/dev/null || true
    step_ok "dnsmasq and hostapd left to nasnet"

    # One dnsmasq process serves both DNS and DHCP, and Ubuntu ships Restart=no,
    # so a crash takes the LAN down for good. StartLimitIntervalSec=0 because the
    # usual crash is "the bridge has no address yet" — it must keep retrying
    # until the address appears, not give up after five tries.
    sudo mkdir -p /etc/systemd/system/dnsmasq.service.d
    # StartLimitIntervalSec belongs to [Unit]; systemd ignores it under [Service].
    sudo tee /etc/systemd/system/dnsmasq.service.d/10-nasnet.conf >/dev/null <<'DNSMASQ_RESTART'
# Managed by nasnet.
[Unit]
StartLimitIntervalSec=0

[Service]
Restart=always
RestartSec=2
DNSMASQ_RESTART
    sudo systemctl daemon-reload 2>/dev/null || true
    step_ok "dnsmasq set to restart on failure"

    echo ""

    # ── Go ────────────────────────────────────────────────────────────────
    local go_ok=false
    if command -v go &>/dev/null; then
        local go_ver
        go_ver=$(go version 2>/dev/null | awk '{print $3}')
        local go_minor
        go_minor=$(echo "$go_ver" | sed 's/go//' | cut -d. -f1-2)
        if [[ "$go_minor" == "$WIZARD_GO_VERSION" ]]; then
            step_ok "Go ${go_ver}"
            go_ok=true
        else
            step_warn "Go ${go_ver} found but ${WIZARD_GO_VERSION}.x required"
        fi
    fi

    if ! $go_ok; then
        step_info "Installing Go ${WIZARD_GO_VERSION}..."
        local arch
        arch=$(dpkg --print-architecture 2>/dev/null || echo "amd64")

        # Dynamically resolve the latest patch version for the required major.minor
        local go_full_version
        go_full_version=$(curl -fsSL "https://go.dev/dl/?mode=json" 2>/dev/null \
            | grep -o '"version": *"go'"${WIZARD_GO_VERSION}"'\.[0-9]*"' \
            | head -1 \
            | grep -o 'go[0-9.]*')

        if [[ -z "$go_full_version" ]]; then
            step_fail "Failed to resolve latest Go ${WIZARD_GO_VERSION}.x patch version"
            step_info "Download manually: https://go.dev/dl/"
            return 1
        fi

        step_info "Resolved ${go_full_version}"
        local go_tarball="${go_full_version}.linux-${arch}.tar.gz"
        local go_url="https://go.dev/dl/${go_tarball}"

        if run_logged "Downloading ${go_full_version}" curl -fSL -o "/tmp/${go_tarball}" "$go_url"; then
            true
        else
            step_fail "Failed to download Go from ${go_url}"
            step_info "Download manually: https://go.dev/dl/"
            return 1
        fi

        # Remove old Go installation and extract new one
        sudo rm -rf /usr/local/go
        if run_logged "Extracting Go to /usr/local/go" sudo tar -C /usr/local -xzf "/tmp/${go_tarball}"; then
            rm -f "/tmp/${go_tarball}"
        else
            step_fail "Failed to extract Go"
            return 1
        fi

        # Ensure Go is in PATH for current session
        export PATH="/usr/local/go/bin:$PATH"

        # Add to profile if not already there
        if ! grep -q '/usr/local/go/bin' /etc/profile.d/golang.sh 2>/dev/null; then
            echo 'export PATH="/usr/local/go/bin:$PATH"' | sudo tee /etc/profile.d/golang.sh >/dev/null
            step_ok "Go PATH added to /etc/profile.d/golang.sh"
        fi

        local installed_ver
        installed_ver=$(go version 2>/dev/null | awk '{print $3}')
        step_ok "Go ${installed_ver} ready"
    fi

    echo ""

    # ── Node.js ───────────────────────────────────────────────────────────
    local node_ok=false
    if command -v node &>/dev/null; then
        local node_ver
        node_ver=$(node --version 2>/dev/null)
        local node_major
        node_major=$(echo "$node_ver" | sed 's/v//' | cut -d. -f1)
        if [[ "$node_major" == "$WIZARD_NODE_MAJOR" ]]; then
            step_ok "Node.js ${node_ver}"
            node_ok=true
        else
            step_warn "Node.js ${node_ver} found but v${WIZARD_NODE_MAJOR}.x required"
        fi
    fi

    if ! $node_ok; then
        step_info "Installing Node.js ${WIZARD_NODE_MAJOR}.x..."
        if run_logged "Setting up NodeSource repository" bash -c "curl -fsSL https://deb.nodesource.com/setup_${WIZARD_NODE_MAJOR}.x | sudo -E bash -"; then
            true
        else
            step_fail "Failed to add NodeSource repository"
            return 1
        fi

        if run_logged "Installing Node.js" sudo apt-get install -y nodejs; then
            local installed_node
            installed_node=$(node --version 2>/dev/null)
            step_ok "Node.js ${installed_node} installed"
        else
            step_fail "Failed to install Node.js"
            return 1
        fi
    fi

    # ── pnpm ──────────────────────────────────────────────────────────────
    if command -v pnpm &>/dev/null; then
        local pnpm_ver
        pnpm_ver=$(pnpm --version 2>/dev/null)
        step_ok "pnpm ${pnpm_ver}"
    else
        step_info "Installing pnpm..."
        if run_logged "Installing pnpm" sudo npm install -g pnpm; then
            step_ok "pnpm $(pnpm --version 2>/dev/null) installed"
        else
            step_fail "Failed to install pnpm"
            return 1
        fi
    fi

    echo ""

    # ── Database engine ───────────────────────────────────────────────────
    if [[ "$db_driver" == "sqlite" ]]; then
        step_ok "SQLite selected — no separate database to install"
        sudo mkdir -p "$INSTALL_DIR/data"
    else
        # ── PostgreSQL ────────────────────────────────────────────────────
        local pg_ok=false
        if command -v psql &>/dev/null && systemctl is-active postgresql &>/dev/null; then
            local pg_ver
            pg_ver=$(psql --version 2>/dev/null | awk '{print $3}' | cut -d. -f1)
            step_ok "PostgreSQL ${pg_ver} (running)"
            pg_ok=true
        elif command -v psql &>/dev/null; then
            step_warn "PostgreSQL installed but not running"
            step_info "Starting PostgreSQL..."
            if sudo systemctl enable postgresql --now 2>/dev/null; then
                step_ok "PostgreSQL started"
                pg_ok=true
            else
                step_fail "Failed to start PostgreSQL"
            fi
        fi

        if ! $pg_ok; then
            step_info "Installing PostgreSQL..."

            # Add official PostgreSQL APT repository for latest version
            if ! [[ -f /etc/apt/sources.list.d/pgdg.list ]]; then
                step_info "Adding PostgreSQL APT repository..."
                if run_logged "Adding PostgreSQL APT repository" bash -c "
                    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo gpg --dearmor -o /usr/share/keyrings/postgresql-keyring.gpg 2>/dev/null &&
                    echo 'deb [signed-by=/usr/share/keyrings/postgresql-keyring.gpg] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main' | sudo tee /etc/apt/sources.list.d/pgdg.list >/dev/null &&
                    sudo apt-get update
                "; then
                    true
                else
                    step_warn "Could not add official repo — will use distro version"
                fi
            fi

            if run_logged "Installing PostgreSQL" sudo apt-get install -y postgresql postgresql-client; then
                step_ok "PostgreSQL installed"
            else
                step_fail "Failed to install PostgreSQL"
                return 1
            fi

            # Enable and start
            if sudo systemctl enable postgresql --now 2>/dev/null; then
                step_ok "PostgreSQL enabled and started"
            else
                step_fail "Failed to start PostgreSQL"
                return 1
            fi
        fi
    fi

    echo ""
    step_ok "All prerequisites installed"
    return 0
}

# Set up PostgreSQL database and user for systemd mode
wizard_setup_postgres() {
    local db_user="$1"
    local db_pass="$2"
    local db_name="$3"

    step_info "Configuring PostgreSQL database..."

    # Check if user exists
    local user_exists
    user_exists=$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='${db_user}'" 2>/dev/null || echo "")

    if [[ "$user_exists" == "1" ]]; then
        step_ok "Database user '${db_user}' exists"
        # Update password
        sudo -u postgres psql -c "ALTER USER ${db_user} WITH PASSWORD '${db_pass}';" &>/dev/null
        step_ok "Password updated for '${db_user}'"
    else
        if sudo -u postgres psql -c "CREATE USER ${db_user} WITH PASSWORD '${db_pass}';" &>/dev/null; then
            step_ok "Database user '${db_user}' created"
        else
            step_fail "Failed to create database user"
            return 1
        fi
    fi

    # Check if database exists
    local db_exists
    db_exists=$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='${db_name}'" 2>/dev/null || echo "")

    if [[ "$db_exists" == "1" ]]; then
        step_ok "Database '${db_name}' exists"
    else
        if sudo -u postgres psql -c "CREATE DATABASE ${db_name} OWNER ${db_user};" &>/dev/null; then
            step_ok "Database '${db_name}' created"
        else
            step_fail "Failed to create database"
            return 1
        fi
    fi

    # Grant privileges
    sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE ${db_name} TO ${db_user};" &>/dev/null
    step_ok "Privileges granted"

    return 0
}

# Set up bundled PostgreSQL for offline mode
wizard_setup_postgres_offline() {
    local db_user="$1"
    local db_pass="$2"
    local db_name="$3"

    step_info "Setting up bundled PostgreSQL..."

    local pg_bin="$PGSQL_INSTALL_DIR/bin"
    local pg_data="$PGSQL_INSTALL_DIR/data"

    # EnterpriseDB standalone binaries need LD_LIBRARY_PATH for shared libs
    export LD_LIBRARY_PATH="$PGSQL_INSTALL_DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

    # Verify bundled PostgreSQL exists
    if [[ ! -x "$pg_bin/pg_ctl" ]]; then
        step_fail "Bundled PostgreSQL not found at ${PGSQL_INSTALL_DIR}"
        step_info "Run install.sh first to extract PostgreSQL"
        return 1
    fi

    # Check if port 5432 is already in use
    if ss -tlnp 2>/dev/null | grep -q ':5432 '; then
        step_warn "Port 5432 is already in use"
        echo ""
        echo -e "  An existing PostgreSQL may be running."
        echo -e "  You can use the existing instance instead of the bundled one."
        echo ""
        local pg_choice
        arrow_menu "PostgreSQL setup" pg_choice \
            "Use existing PostgreSQL on port 5432" \
            "Stop existing and use bundled PostgreSQL" \
            "← Cancel"

        case $pg_choice in
            0)
                # Use existing — just create user/database via system psql
                step_info "Using existing PostgreSQL..."
                wizard_setup_postgres "$db_user" "$db_pass" "$db_name"
                return $?
                ;;
            1)
                step_info "Stopping existing PostgreSQL..."
                systemctl stop postgresql 2>/dev/null || true
                ;;
            *)
                return 1
                ;;
        esac
    fi

    # Initialize data directory if not already done
    if [[ ! -f "$pg_data/PG_VERSION" ]]; then
        step_info "Initializing PostgreSQL data directory..."
        # Write password to temp file (process substitution doesn't work through sudo)
        local pw_file
        pw_file=$(mktemp)
        echo "$db_pass" > "$pw_file"
        chmod 644 "$pw_file"
        if sudo -u postgres "$pg_bin/initdb" -D "$pg_data" --auth=md5 --pwfile="$pw_file" --username=postgres &>/dev/null; then
            rm -f "$pw_file"
        else
            rm -f "$pw_file"
            # Retry without --pwfile (older pg versions)
            sudo -u postgres "$pg_bin/initdb" -D "$pg_data" &>/dev/null || {
                step_fail "Failed to initialize PostgreSQL data directory"
                return 1
            }
        fi
        step_ok "Data directory initialized"

        # Configure pg_hba.conf for local connections
        cat > "$pg_data/pg_hba.conf" << 'HBAEOF'
# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             postgres                                peer
local   all             all                                     md5
host    all             all             127.0.0.1/32            md5
host    all             all             ::1/128                 md5
HBAEOF
        chown postgres:postgres "$pg_data/pg_hba.conf"
        step_ok "pg_hba.conf configured"

        # Configure postgresql.conf
        cat >> "$pg_data/postgresql.conf" << 'CONFEOF'

# nasnet-panel offline bundle settings
listen_addresses = 'localhost'
port = 5432
max_connections = 100
shared_buffers = 128MB
log_destination = 'stderr'
logging_collector = off
CONFEOF
        chown postgres:postgres "$pg_data/postgresql.conf"
        step_ok "postgresql.conf configured"
    else
        step_ok "Data directory already initialized"
    fi

    # Create systemd service
    step_info "Creating ${PGSQL_SERVICE}.service..."
    sudo tee "/etc/systemd/system/${PGSQL_SERVICE}.service" > /dev/null << PGSVCEOF
[Unit]
Description=nasnet-panel PostgreSQL
After=network.target

[Service]
Type=forking
User=postgres
Group=postgres
Environment=LD_LIBRARY_PATH=${PGSQL_INSTALL_DIR}/lib
ExecStart=${pg_bin}/pg_ctl start -D ${pg_data} -l /var/log/nasnet-postgresql.log
ExecStop=${pg_bin}/pg_ctl stop -D ${pg_data} -m fast
ExecReload=${pg_bin}/pg_ctl reload -D ${pg_data}
TimeoutSec=60

[Install]
WantedBy=multi-user.target
PGSVCEOF
    sudo systemctl daemon-reload
    step_ok "${PGSQL_SERVICE}.service created"

    # Start PostgreSQL
    if sudo systemctl enable "$PGSQL_SERVICE" --now 2>/dev/null; then
        step_ok "PostgreSQL started"
    else
        step_fail "Failed to start PostgreSQL"
        step_info "Check logs: journalctl -u ${PGSQL_SERVICE} -n 20"
        step_info "Also check: cat /var/log/nasnet-postgresql.log"
        return 1
    fi

    # Wait for PostgreSQL to be ready
    local retries=0
    while (( retries < 10 )); do
        if sudo -u postgres "$pg_bin/pg_isready" -q 2>/dev/null; then
            break
        fi
        sleep 1
        retries=$(( retries + 1 ))
    done

    if ! sudo -u postgres "$pg_bin/pg_isready" -q 2>/dev/null; then
        step_fail "PostgreSQL did not become ready within 10 seconds"
        return 1
    fi

    # Create database user and database
    local user_exists
    user_exists=$(sudo -u postgres "$pg_bin/psql" -tAc "SELECT 1 FROM pg_roles WHERE rolname='${db_user}'" 2>/dev/null || echo "")

    if [[ "$user_exists" == "1" ]]; then
        step_ok "Database user '${db_user}' exists"
        sudo -u postgres "$pg_bin/psql" -c "ALTER USER ${db_user} WITH PASSWORD '${db_pass}';" &>/dev/null
        step_ok "Password updated for '${db_user}'"
    else
        if sudo -u postgres "$pg_bin/psql" -c "CREATE USER ${db_user} WITH PASSWORD '${db_pass}';" &>/dev/null; then
            step_ok "Database user '${db_user}' created"
        else
            step_fail "Failed to create database user"
            return 1
        fi
    fi

    local db_exists
    db_exists=$(sudo -u postgres "$pg_bin/psql" -tAc "SELECT 1 FROM pg_database WHERE datname='${db_name}'" 2>/dev/null || echo "")

    if [[ "$db_exists" == "1" ]]; then
        step_ok "Database '${db_name}' exists"
    else
        if sudo -u postgres "$pg_bin/psql" -c "CREATE DATABASE ${db_name} OWNER ${db_user};" &>/dev/null; then
            step_ok "Database '${db_name}' created"
        else
            step_fail "Failed to create database"
            return 1
        fi
    fi

    sudo -u postgres "$pg_bin/psql" -c "GRANT ALL PRIVILEGES ON DATABASE ${db_name} TO ${db_user};" &>/dev/null
    step_ok "Privileges granted"

    return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6.7: Setup Wizard — Build & Start Helpers
# ──────────────────────────────────────────────────────────────────────────────

# detect_arch echoes the Go-style arch name, or fails loudly.
detect_arch() {
    case "$(uname -m)" in
        x86_64)        echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             return 1 ;;
    esac
}

# install_xray_core puts the core where the panel expects it. Prefers the copy
# vendored in the repo, falls back to the upstream release.
install_xray_core() {
    local arch
    if ! arch=$(detect_arch); then
        step_warn "Unsupported architecture $(uname -m) — skipping xray-core"
        return 0
    fi

    sudo mkdir -p "$XRAY_CONFIG_DIR"

    local vendored="${PROJECT_DIR:-}/bin/xray/v${XRAY_VERSION}/xray-linux-${arch}"
    local want=""
    [[ -f "${vendored}.sha256" ]] && want=$(cut -d' ' -f1 < "${vendored}.sha256")

    # Don't reinstall an unchanged core under a running panel.
    if [[ -x "$XRAY_BINARY" && -n "$want" ]]; then
        local have
        have=$(sudo sha256sum "$XRAY_BINARY" 2>/dev/null | cut -d' ' -f1)
        if [[ "$have" == "$want" ]]; then
            step_ok "xray-core ${XRAY_VERSION} already installed"
            _install_geofiles
            return 0
        fi
    fi

    if [[ -f "$vendored" ]]; then
        if [[ -n "$want" ]]; then
            local got
            got=$(sha256sum "$vendored" | cut -d' ' -f1)
            if [[ "$got" != "$want" ]]; then
                step_fail "Vendored xray-core failed its checksum — refusing to install it"
                return 1
            fi
        fi
        sudo install -m 755 "$vendored" "$XRAY_BINARY"
        step_ok "xray-core ${XRAY_VERSION} installed (${arch}, from the bundle)"
        _install_geofiles
        return 0
    fi

    _download_xray_core "$arch"
}

# Only reached when the repo copy is absent: an auto-update run, or a tarball
# without bin/xray.
_download_xray_core() {
    local arch="$1" suffix
    case "$arch" in
        arm64) suffix="arm64-v8a" ;;
        *)     suffix="64" ;;
    esac

    if ! command -v unzip &>/dev/null; then
        if ! run_logged "Installing unzip" sudo apt-get install -y unzip; then
            step_fail "unzip is needed to unpack xray-core"
            return 1
        fi
    fi

    local url="https://github.com/XTLS/Xray-core/releases/download/v${XRAY_VERSION}/Xray-linux-${suffix}.zip"
    local tmp
    tmp=$(mktemp -d)

    if ! run_logged "Downloading xray-core ${XRAY_VERSION}" curl -fL -o "${tmp}/xray.zip" "$url"; then
        step_fail "Could not download xray-core from ${url}"
        rm -rf "$tmp"
        return 1
    fi
    if ! run_logged "Unpacking xray-core" unzip -o "${tmp}/xray.zip" xray -d "$tmp"; then
        step_fail "Could not unpack xray-core"
        rm -rf "$tmp"
        return 1
    fi

    sudo install -m 755 "${tmp}/xray" "$XRAY_BINARY"
    rm -rf "$tmp"
    step_ok "xray-core ${XRAY_VERSION} installed (${arch}, downloaded)"
    _install_geofiles
}

# Seeds geoip.dat/geosite.dat where xray looks. A geosite rule with no file
# stops xray from starting at all.
_install_geofiles() {
    local src="${PROJECT_DIR:-}/pkg/geofiles/embedded"
    local seeded=false
    local f
    for f in geoip.dat geosite.dat; do
        if [[ -s "${src}/${f}" && ! -s "${XRAY_CONFIG_DIR}/${f}" ]]; then
            sudo cp "${src}/${f}" "${XRAY_CONFIG_DIR}/${f}"
            sudo chmod 644 "${XRAY_CONFIG_DIR}/${f}"
            seeded=true
        fi
    done
    $seeded && step_ok "Geofiles seeded in ${XRAY_CONFIG_DIR}"
    return 0
}

wizard_deploy_artifacts() {
    local run_user="${SUDO_USER:-$(whoami)}"
    local run_group
    run_group=$(id -gn "$run_user" 2>/dev/null || echo "$run_user")

    # Offline mode: artifacts already deployed by install.sh — only sync .env
    if [[ "$OFFLINE_MODE" == "true" ]]; then
        step_info "Offline mode — syncing configuration..."

        # Copy .env to install dir
        if [[ -f "$ENV_FILE" ]]; then
            sudo cp "$ENV_FILE" "$INSTALL_DIR/.env"
            sudo chmod 600 "$INSTALL_DIR/.env"
        fi

        # Set ownership
        sudo chown -R "$run_user:$run_group" "$INSTALL_DIR"

        step_ok "Configuration synced to ${INSTALL_DIR}"
        return 0
    fi

    step_info "Deploying to ${INSTALL_DIR}..."

    # Create directory structure
    sudo mkdir -p "$INSTALL_DIR"/{bin/{agent,xray},data/{backups,acme}}

    # Copy backend binary
    sudo cp "$PROJECT_DIR/nasnet-panel" "$INSTALL_DIR/bin/nasnet-panel"
    sudo chmod +x "$INSTALL_DIR/bin/nasnet-panel"

    # Copy agent binaries
    if [[ -d "$PROJECT_DIR/bin/agent" ]]; then
        sudo mkdir -p "$INSTALL_DIR/bin/agent"
        sudo cp "$PROJECT_DIR"/bin/agent/nasnet-agent-* "$INSTALL_DIR/bin/agent/"
        sudo chmod +x "$INSTALL_DIR"/bin/agent/nasnet-agent-*
        step_ok "Agent binaries deployed"
    fi

    # Has to be here before the service starts, or every config push fails.
    install_xray_core || step_warn "xray-core was not installed — the panel will start but xray will not"

    # Copy .env
    sudo cp "$ENV_FILE" "$INSTALL_DIR/.env"
    sudo chmod 600 "$INSTALL_DIR/.env"

    # Write version marker
    local _deploy_version
    _deploy_version=$(cd "$PROJECT_DIR" && git describe --tags --always 2>/dev/null || echo "unknown")
    echo "$_deploy_version" | sudo tee "$INSTALL_DIR/.version" >/dev/null

    # Set ownership
    sudo chown -R "$run_user:$run_group" "$INSTALL_DIR"

    step_ok "Deployed to ${INSTALL_DIR}"
}

wizard_build_start_docker() {
    draw_header "Building & Starting Services (Docker)"

    _prepare_docker_bind_mounts
    _export_build_env

    if run_logged "Building backend image" _compose_cmd_with_files build app; then
        true
    else
        step_fail "Backend build failed"
        step_info "Run manually: docker compose build app"
        return 1
    fi

    if run_logged "Starting containers" _compose_cmd_with_files up -d; then
        true
    else
        step_fail "Failed to start services"
        step_info "Check with: docker compose ps"
        return 1
    fi

    # Wait for health checks
    echo ""
    step_info "Waiting for services to become healthy..."
    local retries=0
    local max_retries=30
    while (( retries < max_retries )); do
        local healthy_count
        healthy_count=$(docker ps --filter "name=nasnet_panel" --filter "health=healthy" --format '{{.Names}}' 2>/dev/null | wc -l | tr -d ' ')
        if (( healthy_count >= 2 )); then
            break
        fi
        sleep 2
        retries=$(( retries + 1 ))
        printf "\r  ${CYAN}⠋${RESET} Waiting for health checks... (%d/%ds)" $(( retries * 2 )) $(( max_retries * 2 ))
    done
    printf "\r%60s\r" ""  # clear line

    echo ""
    action_view_status_inline
    return 0
}


# === Network apply dead-man === #
install_netrollback_units() {
    step_info "Installing network apply dead-man..."

    local roll_env_db=""
    if ! is_sqlite; then
        roll_env_db="Environment=DB_HOST=localhost"
    fi

    sudo tee /etc/systemd/system/nasnet-netrollback.service > /dev/null << 'ROLLSVC'
[Unit]
Description=nasnet network apply dead-man
Documentation=https://github.com/nasnet-community/nasnet-panel-linux

[Service]
Type=oneshot
User=root
WorkingDirectory=INSTALL_DIR_PLACEHOLDER
EnvironmentFile=INSTALL_DIR_PLACEHOLDER/.env
ROLL_ENV_DB_PLACEHOLDER
ExecStart=INSTALL_DIR_PLACEHOLDER/bin/nasnet-panel net rollback --if-expired
ROLLSVC
    sudo sed -i "s|INSTALL_DIR_PLACEHOLDER|${INSTALL_DIR}|g" \
        /etc/systemd/system/nasnet-netrollback.service
    sudo sed -i "s|^ROLL_ENV_DB_PLACEHOLDER\$|${roll_env_db}|" \
        /etc/systemd/system/nasnet-netrollback.service

    sudo tee /etc/systemd/system/nasnet-netrollback.timer > /dev/null << 'ROLLTMR'
[Unit]
Description=nasnet network apply dead-man

[Timer]
OnBootSec=10s
OnUnitActiveSec=10s
AccuracySec=1s

[Install]
WantedBy=timers.target
ROLLTMR

    sudo systemctl daemon-reload
    sudo systemctl enable --now nasnet-netrollback.timer
    step_ok "nasnet-netrollback.timer enabled (10s tick, no-ops without a marker)"
}


wizard_build_start_systemd() {
    local app_port="${1:-9761}"

    draw_header "Building & Starting Services (Systemd)"

    if [[ "$OFFLINE_MODE" == "true" ]]; then
        # ── Offline: skip all builds, artifacts already deployed by install.sh ──
        step_ok "Offline mode — using pre-built artifacts from bundle"
        echo ""
    else
    # ── Build frontend (embedded in Go binary via go:embed) ─────────────
    if run_logged "Building frontend" bash -c "cd '$PROJECT_DIR/web-panel' && pnpm install && pnpm build"; then
        step_ok "Frontend built"
    else
        step_fail "Frontend build failed"
        return 1
    fi

    # ── Build Go backend ──────────────────────────────────────────────────
    if run_logged "Downloading Go modules" bash -c "cd '$PROJECT_DIR' && go mod download"; then
        true
    else
        step_fail "Failed to download Go modules"
        return 1
    fi

    # ── Download Iran geofiles for embedding ─────────────────────────────
    if run_logged "Downloading Iran geofiles" bash -c "cd '$PROJECT_DIR' && make geofiles"; then
        step_ok "Iran geofiles ready"
    else
        step_fail "Failed to download Iran geofiles"
        return 1
    fi

    local cgo_enabled=0
    is_sqlite && cgo_enabled=1
    if run_logged "Building nasnet-panel binary" bash -c "cd '$PROJECT_DIR' && CGO_ENABLED=$cgo_enabled go build -ldflags='-w -s' -o nasnet-panel ."; then
        step_ok "Binary built: ${PROJECT_DIR}/nasnet-panel"
    else
        step_fail "Backend build failed"
        return 1
    fi

    # ── Build agent binaries ─────────────────────────────────────────────
    if run_logged "Building agent binaries" bash -c "cd '$PROJECT_DIR' && make build-agent"; then
        step_ok "Agent binaries built"
    else
        step_fail "Agent binary build failed"
        return 1
    fi

    # ── Deploy artifacts to install directory ─────────────────────────────
    echo ""
    draw_header "Deploying to ${INSTALL_DIR}"
    wizard_deploy_artifacts
    fi  # end online/offline branch

    echo ""

    # ── Create systemd services ───────────────────────────────────────────
    draw_header "Installing Systemd Services"

    # Determine the user to run services as (current user or SUDO_USER)
    local run_user="${SUDO_USER:-$(whoami)}"
    local run_group
    run_group=$(id -gn "$run_user" 2>/dev/null || echo "$run_user")

    # Backend service
    step_info "Creating ${BACKEND_SERVICE}.service..."

    local svc_after="network-online.target"
    local svc_requires=""
    local svc_env_db=""
    if ! is_sqlite; then
        local pg_svc_name="postgresql.service"
        if [[ "$OFFLINE_MODE" == "true" ]]; then
            pg_svc_name="${PGSQL_SERVICE}.service"
        fi
        svc_after="network-online.target ${pg_svc_name}"
        svc_requires="Requires=${pg_svc_name}"
        svc_env_db="Environment=DB_HOST=localhost"
    fi

    sudo tee "/etc/systemd/system/${BACKEND_SERVICE}.service" > /dev/null << SVCEOF
[Unit]
Description=nasnet-panel Backend API
Documentation=https://github.com/nasnet-community/nasnet-panel-linux
After=${svc_after}
${svc_requires}
Wants=network-online.target

[Service]
Type=simple
User=${run_user}
Group=${run_group}
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${INSTALL_DIR}/.env
${svc_env_db}
ExecStart=${INSTALL_DIR}/bin/nasnet-panel serve
Restart=always
RestartSec=5
LimitNOFILE=65536

# Router mode needs netlink, nft and SO_MARK. xray inherits thes
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
Environment=NASNET_ROUTER_MODE=1

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${BACKEND_SERVICE}

[Install]
WantedBy=multi-user.target
SVCEOF
    step_ok "${BACKEND_SERVICE}.service created"

    install_netrollback_units

    # Reload systemd
    sudo systemctl daemon-reload
    step_ok "Systemd daemon reloaded"

    # ── Enable and start services ─────────────────────────────────────────
    echo ""
    step_info "Starting services..."

    sudo systemctl enable "$BACKEND_SERVICE" --now 2>/dev/null
    sleep 2

    if systemctl is-active "$BACKEND_SERVICE" &>/dev/null; then
        step_ok "${BACKEND_SERVICE} is running"
    else
        step_fail "${BACKEND_SERVICE} failed to start"
        step_info "Check logs: journalctl -u ${BACKEND_SERVICE} -n 20"
    fi

    # ── Health check ──────────────────────────────────────────────────────
    echo ""
    step_info "Waiting for services to become healthy..."
    local retries=0
    local max_retries=15
    while (( retries < max_retries )); do
        if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
            break
        fi
        sleep 2
        retries=$(( retries + 1 ))
        printf "\r  ${CYAN}⠋${RESET} Waiting for backend... (%d/%ds)" $(( retries * 2 )) $(( max_retries * 2 ))
    done
    printf "\r%60s\r" ""

    # Show status
    echo ""
    local rows=()
    local api_status pg_status

    if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
        api_status="${GREEN}● Healthy${RESET}"
    else
        api_status="${RED}● Unreachable${RESET}"
    fi
    rows+=("nasnet-panel|http://localhost:${app_port}|${api_status}")

    if is_sqlite; then
        rows+=("SQLite|embedded|${GREEN}● Ready${RESET}")
    else
        if systemctl is-active postgresql &>/dev/null; then
            pg_status="${GREEN}● Running${RESET}"
        else
            pg_status="${RED}● Stopped${RESET}"
        fi
        rows+=("PostgreSQL|systemd|${pg_status}")
    fi

    draw_table "Service|Endpoint|Status" "${rows[@]}"

    return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6.8: Setup Wizard — Write .env Helper
# ──────────────────────────────────────────────────────────────────────────────

# wizard_write_env <deploy_mode> <header_comment>
# All WIZ_* variables must be set before calling this.
wizard_write_env() {
    local deploy_mode="$1"
    local header_comment="$2"

    local db_driver="${WIZ_DB_DRIVER:-postgres}"

    cat > "$ENV_FILE" << ENVEOF
# ── nasnet-panel Configuration ──────────────────────────────────────────────────
# ${header_comment}
# Mode: ${WIZ_MODE}  |  Deploy: ${deploy_mode}  |  DB: ${db_driver}

# Deployment
DEPLOY_MODE=${deploy_mode}

# Application
APP_ENV=production
APP_PORT=${WIZ_APP_PORT}
APP_BASE_URL=${WIZ_APP_BASE_URL}
SUB_PANEL_URL=${WIZ_SUB_PANEL_URL}
APP_PANEL_BASE_PATH=${WIZ_BASE_PATH}

# Database
DB_DRIVER=${db_driver}
ENVEOF

    if [[ "$db_driver" == "sqlite" ]]; then
        local db_path
        if [[ "$deploy_mode" == "systemd" ]]; then
            db_path="$INSTALL_DIR/data/nasnet_panel.db"
        else
            db_path="/app/data/nasnet_panel.db"
        fi
        cat >> "$ENV_FILE" << ENVEOF
DB_PATH=${db_path}
ENVEOF
    else
        cat >> "$ENV_FILE" << ENVEOF
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=${WIZ_DB_PASSWORD}
DB_NAME=nasnet_panel
DB_SSL_MODE=disable
ENVEOF
    fi

    cat >> "$ENV_FILE" << ENVEOF

# Telegram Bot
TELEGRAM_ENABLED=${WIZ_TELEGRAM_ENABLED}
TELEGRAM_BOT_TOKEN=${WIZ_BOT_TOKEN}
BOT_MODE=polling
WEBHOOK_URL=

# Telegram Proxy (optional)
TELEGRAM_PROXY_ENABLED=false
TELEGRAM_PROXY_TYPE=socks5
TELEGRAM_PROXY_HOST=
TELEGRAM_PROXY_PORT=1080
TELEGRAM_PROXY_USERNAME=
TELEGRAM_PROXY_PASSWORD=

# Logging
LOG_LEVEL=info
LOG_FORMAT=text

# Admin
ADMIN_IDS=${WIZ_ADMIN_IDS}
ADMIN_USERNAME=admin
ADMIN_PASSWORD_HASH='${WIZ_ADMIN_HASH}'

# TLS (optional — leave empty for auto ACME, or set paths for custom certs)
TLS_CERT_FILE=
TLS_KEY_FILE=

# ACME / Let's Encrypt
ACME_ENABLED=${WIZ_ACME_ENABLED:-false}
ACME_EMAIL=${WIZ_ACME_EMAIL}
ACME_CACHE_DIR=${WIZ_ACME_CACHE_DIR:-/app/data/acme}
ACME_STAGING=${WIZ_ACME_STAGING}
ACME_AUTO_RENEW=true

# JWT Authentication
JWT_SECRET_KEY=${WIZ_JWT_SECRET}
JWT_ACCESS_EXPIRY=60
JWT_REFRESH_EXPIRY=168
JWT_COOKIE_DOMAIN=${WIZ_COOKIE_DOMAIN}
JWT_COOKIE_SECURE=${WIZ_COOKIE_SECURE}

# Metrics
METRICS_ENABLED=true
METRICS_PATH=/metrics
METRICS_USERNAME=
METRICS_PASSWORD=

# Prometheus
PROMETHEUS_PORT=9090
PROMETHEUS_TARGET=${WIZ_PROM_TARGET:-app:${WIZ_APP_PORT}}
PROMETHEUS_SCRAPE_INTERVAL=5s
PROMETHEUS_RETENTION=15d
ENVEOF

    chmod 600 "$ENV_FILE"
    step_ok ".env written (permissions: 600)"
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6.9: Setup Wizard — Access Mode Prompts (shared)
# ──────────────────────────────────────────────────────────────────────────────

# Sets: WIZ_MODE, WIZ_APP_BASE_URL, WIZ_SUB_PANEL_URL,
#       WIZ_BASE_PATH, WIZ_COOKIE_DOMAIN,
#       WIZ_COOKIE_SECURE, WIZ_ACME_STAGING, WIZ_ACME_EMAIL
# Requires: WIZ_APP_PORT to be set
# Returns 1 if user cancels
wizard_prompt_access_mode() {
    echo -e "  ${BOLD}How will users access this server?${RESET}"
    echo ""
    echo -e "  ${CYAN}Domain mode${RESET}  — You have a domain pointing to this server"
    echo -e "  ${CYAN}IP mode${RESET}      — Access via server IP address (HTTP, no TLS)"
    echo ""

    local mode_choice
    arrow_menu "Select mode" mode_choice \
        "Domain mode (recommended)" \
        "IP-only mode" \
        "← Cancel"

    [[ $mode_choice -eq -1 || $mode_choice -eq 2 ]] && return 1

    if [[ $mode_choice -eq 0 ]]; then
        # ── Domain Mode ──
        WIZ_MODE="domain"

        # ── Protocol ──
        echo ""
        echo -e "  ${BOLD}Protocol:${RESET}"
        echo -e "  ${CYAN}HTTPS${RESET} — Secure, requires TLS certificate (auto or manual)"
        echo -e "  ${CYAN}HTTP${RESET}  — No encryption (use if TLS is handled by a reverse proxy)"
        echo ""

        local proto_choice
        arrow_menu "Select protocol" proto_choice \
            "HTTPS (recommended)" \
            "HTTP"

        local WIZ_PROTO="https"
        WIZ_COOKIE_SECURE="true"
        if [[ $proto_choice -eq 1 ]]; then
            WIZ_PROTO="http"
            WIZ_COOKIE_SECURE="false"
        fi

        # ── API domain & port ──
        echo ""
        echo -ne "  ${CYAN}API domain${RESET} (e.g. api.example.com): "
        read -r WIZ_API_DOMAIN
        if [[ -z "$WIZ_API_DOMAIN" ]]; then
            step_fail "API domain is required"
            return 1
        fi

        local default_api_port="$WIZ_APP_PORT"
        echo -ne "  ${CYAN}API port${RESET} [${default_api_port}]: "
        read -r api_port_input
        if [[ -n "$api_port_input" ]]; then
            WIZ_APP_PORT="$api_port_input"
        fi

        # ── Panel domain & port ──
        echo ""
        echo -ne "  ${CYAN}Panel domain${RESET} [${WIZ_API_DOMAIN}]: "
        read -r WIZ_PANEL_DOMAIN
        [[ -z "$WIZ_PANEL_DOMAIN" ]] && WIZ_PANEL_DOMAIN="$WIZ_API_DOMAIN"

        # ── Panel base path (auto-generated for security) ──
        WIZ_BASE_PATH="/$(head -c 64 /dev/urandom | base64 | tr -dc 'a-z0-9' | head -c 6)"
        echo ""
        step_info "A random panel path has been generated for security."
        step_info "Your admin panel will be at: ${GREEN}${WIZ_PROTO}://${WIZ_PANEL_DOMAIN}:${WIZ_APP_PORT}${WIZ_BASE_PATH}${RESET}"
        echo -ne "  ${CYAN}Panel base path${RESET} [${WIZ_BASE_PATH}]: "
        read -r bp_input
        if [[ "$bp_input" == "none" ]]; then
            WIZ_BASE_PATH=""
        elif [[ -n "$bp_input" ]]; then
            WIZ_BASE_PATH="$bp_input"
            WIZ_BASE_PATH="${WIZ_BASE_PATH%/}"
            [[ "$WIZ_BASE_PATH" != /* ]] && WIZ_BASE_PATH="/${WIZ_BASE_PATH}"
        fi

        WIZ_DOMAIN="$WIZ_API_DOMAIN"
        WIZ_APP_BASE_URL="${WIZ_PROTO}://${WIZ_API_DOMAIN}:${WIZ_APP_PORT}"
        WIZ_SUB_PANEL_URL="${WIZ_PROTO}://${WIZ_PANEL_DOMAIN}:${WIZ_APP_PORT}${WIZ_BASE_PATH}"
        WIZ_COOKIE_DOMAIN=""
        WIZ_ACME_STAGING="false"

        echo ""
        step_info "Derived URLs:"
        echo -e "    APP_BASE_URL  = ${GREEN}${WIZ_APP_BASE_URL}${RESET}"
        echo -e "    SUB_PANEL_URL = ${GREEN}${WIZ_SUB_PANEL_URL}${RESET}"
        echo ""

        if confirm_action "Override any derived URLs?"; then
            echo ""
            echo -ne "  ${CYAN}APP_BASE_URL${RESET} [${WIZ_APP_BASE_URL}]: "
            read -r override
            [[ -n "$override" ]] && WIZ_APP_BASE_URL="$override"

            echo -ne "  ${CYAN}SUB_PANEL_URL${RESET} [${WIZ_SUB_PANEL_URL}]: "
            read -r override
            [[ -n "$override" ]] && WIZ_SUB_PANEL_URL="$override"
        fi

        # Ask if the app should handle TLS via ACME (Let's Encrypt).
        # Users behind a reverse proxy (nginx, Caddy) should say no.
        WIZ_ACME_EMAIL=""
        WIZ_ACME_ENABLED="false"
        if [[ "$WIZ_PROTO" == "https" ]]; then
            echo ""
            echo -e "  ${DIM}If you use a reverse proxy (nginx/Caddy) for TLS, choose No.${RESET}"
            if confirm_action "Issue TLS certificate via Let's Encrypt (ACME)?"; then
                echo -ne "  ${CYAN}Email for Let's Encrypt${RESET}: "
                read -r WIZ_ACME_EMAIL

                if [[ -z "$WIZ_ACME_EMAIL" || ! "$WIZ_ACME_EMAIL" =~ "@" ]]; then
                    step_fail "A valid email is required for Let's Encrypt"
                    return 1
                fi
                WIZ_ACME_ENABLED="true"
            fi
        fi

    else
        # ── IP Mode ──
        WIZ_MODE="ip"

        step_info "Detecting server IP..."
        local WIZ_IP
        WIZ_IP=$(wizard_detect_ip)

        if [[ -n "$WIZ_IP" ]]; then
            step_ok "Detected: ${WIZ_IP}"
        else
            step_warn "Could not auto-detect IP"
        fi

        echo -ne "  ${CYAN}Server IP${RESET} [${WIZ_IP}]: "
        read -r ip_override
        [[ -n "$ip_override" ]] && WIZ_IP="$ip_override"

        if [[ -z "$WIZ_IP" ]]; then
            step_fail "Server IP is required"
            return 1
        fi

        # ── Panel base path (auto-generated for security) ──
        WIZ_BASE_PATH="/$(head -c 64 /dev/urandom | base64 | tr -dc 'a-z0-9' | head -c 6)"
        echo ""
        step_info "A random panel path has been generated for security."
        step_info "Your admin panel will be at: ${GREEN}http://${WIZ_IP}:${WIZ_APP_PORT}${WIZ_BASE_PATH}${RESET}"
        echo -ne "  ${CYAN}Panel base path${RESET} [${WIZ_BASE_PATH}]: "
        read -r bp_input
        if [[ "$bp_input" == "none" ]]; then
            WIZ_BASE_PATH=""
        elif [[ -n "$bp_input" ]]; then
            WIZ_BASE_PATH="$bp_input"
            WIZ_BASE_PATH="${WIZ_BASE_PATH%/}"
            [[ "$WIZ_BASE_PATH" != /* ]] && WIZ_BASE_PATH="/${WIZ_BASE_PATH}"
        fi
        WIZ_APP_BASE_URL="http://${WIZ_IP}:${WIZ_APP_PORT}"
        WIZ_SUB_PANEL_URL="http://${WIZ_IP}:${WIZ_APP_PORT}${WIZ_BASE_PATH}"
        WIZ_COOKIE_DOMAIN=""
        WIZ_COOKIE_SECURE="false"
        WIZ_ACME_STAGING="true"
        WIZ_ACME_ENABLED="false"
        WIZ_ACME_EMAIL=""

        echo ""
        step_info "Derived URLs:"
        echo -e "    APP_BASE_URL  = ${GREEN}${WIZ_APP_BASE_URL}${RESET}"
        echo -e "    SUB_PANEL_URL = ${GREEN}${WIZ_SUB_PANEL_URL}${RESET}"
        echo ""
    fi

    return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6.10: Setup Wizard — Main Flows
# ──────────────────────────────────────────────────────────────────────────────

wizard_install() {
    clear
    draw_box "nasnet-panel Installation Wizard"

    if [[ -f "$ENV_FILE" ]]; then
        step_warn "An .env file already exists at ${ENV_FILE}"
        if ! confirm_action "Overwrite it? (existing secrets will be lost)"; then
            step_info "Cancelled — use Reconfigure to modify existing config"
            press_any_key
            return
        fi
    fi

    # ── Step 1: Deployment Mode ───────────────────────────────────────────
    # Offline mode: force systemd, skip deployment choice
    if [[ "$OFFLINE_MODE" == "true" ]]; then
        local WIZ_DEPLOY_MODE="systemd"
        step_info "Offline mode — using systemd deployment"
    else
    draw_header "Step 1: Deployment Mode"

    echo -e "  ${BOLD}How do you want to run nasnet-panel?${RESET}"
    echo ""
    echo -e "  ${CYAN}Docker${RESET}   — Runs in containers (Docker + Docker Compose required)"
    echo -e "  ${CYAN}Systemd${RESET}  — Runs natively on this machine (Debian/Ubuntu, installs Go/Node/PostgreSQL)"
    echo ""

    local deploy_choice
    arrow_menu "Select deployment" deploy_choice \
        "Docker (recommended)" \
        "Systemd (bare-metal)" \
        "← Cancel"

    [[ $deploy_choice -eq -1 || $deploy_choice -eq 2 ]] && return

    local WIZ_DEPLOY_MODE=""
    if [[ $deploy_choice -eq 0 ]]; then
        WIZ_DEPLOY_MODE="docker"
    else
        WIZ_DEPLOY_MODE="systemd"
    fi
    fi  # end offline/online deploy mode selection

    # ── Step 2: Database Engine ──────────────────────────────────────────
    draw_header "Step 2: Database Engine"

    echo -e "  ${BOLD}Which database engine do you want to use?${RESET}"
    echo ""
    echo -e "  ${CYAN}PostgreSQL${RESET}  — Full-featured, best for production"
    echo -e "  ${CYAN}SQLite${RESET}      — Zero-config, single-file, good for small deployments"
    echo ""

    local db_engine_choice
    arrow_menu "Select database engine" db_engine_choice \
        "PostgreSQL (recommended)" \
        "SQLite (lightweight)" \
        "← Cancel"

    [[ $db_engine_choice -eq -1 || $db_engine_choice -eq 2 ]] && return

    local WIZ_DB_DRIVER=""
    if [[ $db_engine_choice -eq 0 ]]; then
        WIZ_DB_DRIVER="postgres"
    else
        WIZ_DB_DRIVER="sqlite"
    fi

    # ── Step 3: Prerequisites ─────────────────────────────────────────────
    draw_header "Step 3: Prerequisites"

    if [[ "$WIZ_DEPLOY_MODE" == "docker" ]]; then
        if ! wizard_prereqs_docker; then
            press_any_key
            return
        fi
    else
        if ! wizard_prereqs_systemd "$WIZ_DB_DRIVER"; then
            press_any_key
            return
        fi
    fi

    echo ""

    # ── Step 4: Access Mode ───────────────────────────────────────────────
    draw_header "Step 4: Access Mode"

    local WIZ_MODE="" WIZ_DOMAIN="" WIZ_BASE_PATH=""
    local WIZ_APP_BASE_URL="" WIZ_SUB_PANEL_URL=""
    local WIZ_COOKIE_DOMAIN="" WIZ_COOKIE_SECURE="" WIZ_ACME_STAGING="" WIZ_ACME_ENABLED="" WIZ_ACME_EMAIL=""
    local WIZ_APP_PORT
    WIZ_APP_PORT=$(wizard_random_port)
    step_info "Generated random port — APP_PORT: ${BOLD}${WIZ_APP_PORT}${RESET}"

    if ! wizard_prompt_access_mode; then
        press_any_key
        return
    fi

    # ── Step 5: Essential Configuration ───────────────────────────────────
    draw_header "Step 5: Telegram & Admin"

    local WIZ_TELEGRAM_ENABLED="true"
    local WIZ_BOT_TOKEN=""
    local WIZ_ADMIN_IDS=""

    if confirm_action "Enable Telegram bot?"; then
        echo -ne "  ${CYAN}Telegram bot token${RESET} (from @BotFather): "
        read -r WIZ_BOT_TOKEN

        if [[ -z "$WIZ_BOT_TOKEN" ]]; then
            step_fail "Bot token is required when Telegram is enabled"
            press_any_key
            return
        fi

        echo -ne "  ${CYAN}Admin Telegram ID${RESET} (your numeric ID): "
        read -r WIZ_ADMIN_IDS

        if [[ -z "$WIZ_ADMIN_IDS" ]] || ! [[ "$WIZ_ADMIN_IDS" =~ ^[0-9,\ ]+$ ]]; then
            step_fail "A valid numeric Telegram ID is required"
            press_any_key
            return
        fi
    else
        WIZ_TELEGRAM_ENABLED="false"
        step_ok "Telegram bot disabled — running in web-panel-only mode"
    fi

    echo ""
    echo -e "  ${BOLD}Admin panel password${RESET} (for web dashboard login)"
    while true; do
        echo -ne "  ${CYAN}Password${RESET}: "
        read -rs WIZ_ADMIN_PASS
        echo ""

        if [[ ${#WIZ_ADMIN_PASS} -lt 6 ]]; then
            step_fail "Password must be at least 6 characters — try again"
            continue
        fi

        echo -ne "  ${CYAN}Confirm password${RESET}: "
        read -rs WIZ_ADMIN_PASS2
        echo ""

        if [[ "$WIZ_ADMIN_PASS" != "$WIZ_ADMIN_PASS2" ]]; then
            step_fail "Passwords do not match — try again"
            continue
        fi

        break
    done

    # ── Step 6: Generate Secrets ──────────────────────────────────────────
    draw_header "Step 6: Generating Secrets"

    local WIZ_JWT_SECRET
    WIZ_JWT_SECRET=$(wizard_gen_secret)
    step_ok "JWT secret key generated (${#WIZ_JWT_SECRET} chars)"

    local WIZ_DB_PASSWORD=""
    if [[ "$WIZ_DB_DRIVER" != "sqlite" ]]; then
        WIZ_DB_PASSWORD=$(wizard_gen_password)
        step_ok "Database password generated"
    else
        step_ok "SQLite — no database password needed"
    fi

    local WIZ_ADMIN_HASH
    step_info "Generating bcrypt hash for admin password..."
    WIZ_ADMIN_HASH=$(wizard_gen_bcrypt "$WIZ_ADMIN_PASS")

    if [[ -z "$WIZ_ADMIN_HASH" ]]; then
        step_fail "Failed to generate bcrypt hash"
        step_info "Install apache2-utils: apt install apache2-utils"
        press_any_key
        return
    fi
    step_ok "Admin password hash generated"

    # ── Systemd: set up PostgreSQL database ───────────────────────────────
    if [[ "$WIZ_DEPLOY_MODE" == "systemd" && "$WIZ_DB_DRIVER" != "sqlite" ]]; then
        echo ""
        if [[ "$OFFLINE_MODE" == "true" ]]; then
            if ! wizard_setup_postgres_offline "postgres" "$WIZ_DB_PASSWORD" "nasnet_panel"; then
                press_any_key
                return
            fi
        else
            if ! wizard_setup_postgres "postgres" "$WIZ_DB_PASSWORD" "nasnet_panel"; then
                press_any_key
                return
            fi
        fi
    fi

    # ── Step 7: Review ────────────────────────────────────────────────────
    draw_header "Step 7: Review Configuration"

    local masked_token
    if [[ -n "$WIZ_BOT_TOKEN" ]]; then
        masked_token=$(wizard_mask_secret "$WIZ_BOT_TOKEN" 8)
    else
        masked_token="(disabled)"
    fi
    local masked_jwt
    masked_jwt=$(wizard_mask_secret "$WIZ_JWT_SECRET" 8)

    local review_rows=(
        "Deploy|${WIZ_DEPLOY_MODE}"
        "DB Engine|${WIZ_DB_DRIVER}"
        "Mode|${WIZ_MODE}"
        "APP_BASE_URL|${WIZ_APP_BASE_URL}"
        "SUB_PANEL_URL|${WIZ_SUB_PANEL_URL}"
        "Panel Path|${WIZ_BASE_PATH:-(none)}"
        "JWT_COOKIE_DOMAIN|${WIZ_COOKIE_DOMAIN:-(empty)}"
        "JWT_COOKIE_SECURE|${WIZ_COOKIE_SECURE}"
        "ACME_ENABLED|${WIZ_ACME_ENABLED}"
        "ACME_STAGING|${WIZ_ACME_STAGING}"
        "ACME_EMAIL|${WIZ_ACME_EMAIL:-(none)}"
        "TELEGRAM_ENABLED|${WIZ_TELEGRAM_ENABLED}"
        "BOT_TOKEN|${masked_token}"
        "ADMIN_IDS|${WIZ_ADMIN_IDS:-(none)}"
        "ADMIN_USERNAME|admin"
        "JWT_SECRET|${masked_jwt}"
    )
    if [[ "$WIZ_DB_DRIVER" != "sqlite" ]]; then
        local masked_db_pass
        masked_db_pass=$(wizard_mask_secret "$WIZ_DB_PASSWORD" 4)
        review_rows+=("DB_PASSWORD|${masked_db_pass}")
    fi

    draw_table "Setting|Value" "${review_rows[@]}"

    echo ""
    if ! confirm_action "Write this configuration to .env and start services?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # ── Step 8: Write .env ────────────────────────────────────────────────
    draw_header "Step 8: Writing Configuration"

    # Set deployment-specific defaults
    local WIZ_ACME_CACHE_DIR="/app/data/acme"
    local WIZ_PROM_TARGET="app:${WIZ_APP_PORT}"

    if [[ "$WIZ_DEPLOY_MODE" == "systemd" ]]; then
        WIZ_ACME_CACHE_DIR="$INSTALL_DIR/data/acme"
        WIZ_PROM_TARGET="localhost:${WIZ_APP_PORT}"
    fi

    wizard_write_env "$WIZ_DEPLOY_MODE" "Generated by nasnet-tool.sh wizard on $(date '+%Y-%m-%d %H:%M:%S')"

    # Reload env
    load_env

    # ── Step 9: Build & Start ─────────────────────────────────────────────
    if [[ "$WIZ_DEPLOY_MODE" == "docker" ]]; then
        if ! wizard_build_start_docker; then
            press_any_key
            return
        fi
    else
        if ! wizard_build_start_systemd "$WIZ_APP_PORT"; then
            press_any_key
            return
        fi
    fi

    # ── Post-install Summary ──────────────────────────────────────────────
    echo ""
    draw_header "Installation Complete!"

    echo -e "  ${BOLD}${GREEN}nasnet-panel is now running!${RESET}"
    echo ""
    echo -e "  ${BOLD}Deploy Mode:${RESET} ${CYAN}${WIZ_DEPLOY_MODE}${RESET}"
    echo -e "  ${BOLD}Web Panel:${RESET}   ${CYAN}${WIZ_SUB_PANEL_URL}${RESET}"
    echo -e "  ${BOLD}Backend API:${RESET} ${CYAN}${WIZ_APP_BASE_URL}${RESET}"
    echo -e "  ${BOLD}Login:${RESET}       admin / (your password)"
    echo ""
    echo -e "  ${BOLD}Next steps:${RESET}"
    echo -e "    1. Open the web panel URL above in your browser"
    echo -e "    2. Log in with admin and the password you set"
    if [[ "$WIZ_TELEGRAM_ENABLED" == "true" ]]; then
        echo -e "    3. Send /start to your Telegram bot"
    fi
    echo ""
    if [[ "$WIZ_MODE" == "domain" ]]; then
        echo -e "  ${DIM}TLS certificates will be auto-provisioned on first request.${RESET}"
    fi
    if [[ "$WIZ_DEPLOY_MODE" == "systemd" ]]; then
        echo -e "  ${DIM}Installed to:  ${INSTALL_DIR}${RESET}"
        echo -e "  ${DIM}Service logs:  journalctl -u ${BACKEND_SERVICE} -f${RESET}"
        echo -e "  ${DIM}Config: ${INSTALL_DIR}/.env${RESET}"
    else
        echo -e "  ${DIM}Config: ${ENV_FILE}${RESET}"
    fi
    echo -e "  ${DIM}Manage: ./nasnet-tool.sh${RESET}"

    press_any_key
}

wizard_reconfigure() {
    clear
    draw_box "nasnet-panel Reconfigure"

    if [[ ! -f "$ENV_FILE" ]]; then
        step_fail "No .env file found — run Fresh Install first"
        press_any_key
        return
    fi

    # Load existing values
    load_env

    local EXISTING_JWT="${JWT_SECRET_KEY:-}"
    local EXISTING_DB_PASS="${DB_PASSWORD:-}"
    local EXISTING_ADMIN_HASH="${ADMIN_PASSWORD_HASH:-}"
    local EXISTING_BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
    local EXISTING_ADMIN_IDS="${ADMIN_IDS:-}"
    local WIZ_TELEGRAM_ENABLED="${TELEGRAM_ENABLED:-false}"
    local WIZ_DEPLOY_MODE
    WIZ_DEPLOY_MODE=$(wizard_get_deploy_mode)
    [[ -z "$WIZ_DEPLOY_MODE" ]] && WIZ_DEPLOY_MODE="docker"
    local WIZ_DB_DRIVER="${DB_DRIVER:-postgres}"

    draw_header "Reconfigure — Secrets Preserved"
    step_ok "JWT secret, DB password, and admin hash will be kept"
    step_info "Deployment mode: ${BOLD}${WIZ_DEPLOY_MODE}${RESET}"
    step_info "Database engine: ${BOLD}${WIZ_DB_DRIVER}${RESET}"
    echo ""

    # ── Access Mode ───────────────────────────────────────────────────────
    draw_header "Access Mode"

    local WIZ_MODE="" WIZ_DOMAIN="" WIZ_BASE_PATH=""
    local WIZ_APP_BASE_URL="" WIZ_SUB_PANEL_URL=""
    local WIZ_COOKIE_DOMAIN="" WIZ_COOKIE_SECURE="" WIZ_ACME_STAGING="" WIZ_ACME_ENABLED="" WIZ_ACME_EMAIL=""
    local WIZ_APP_PORT
    if [[ -n "${APP_PORT:-}" ]]; then
        WIZ_APP_PORT="$APP_PORT"
    else
        WIZ_APP_PORT=$(wizard_random_port)
    fi

    if ! wizard_prompt_access_mode; then
        press_any_key
        return
    fi

    # ── Optionally change Telegram settings ──────────────────────────────
    echo ""
    echo -e "  Telegram bot: ${BOLD}${WIZ_TELEGRAM_ENABLED}${RESET}"
    if [[ "$WIZ_TELEGRAM_ENABLED" == "true" ]]; then
        local masked_token
        masked_token=$(wizard_mask_secret "$EXISTING_BOT_TOKEN" 8)
        echo -e "  Current bot token: ${DIM}${masked_token}${RESET}"
        if confirm_action "Change Telegram bot token?"; then
            echo -ne "  ${CYAN}New bot token${RESET}: "
            read -r new_token
            [[ -n "$new_token" ]] && EXISTING_BOT_TOKEN="$new_token"
        fi
    fi
    if confirm_action "Toggle Telegram bot (currently: ${WIZ_TELEGRAM_ENABLED})?"; then
        if [[ "$WIZ_TELEGRAM_ENABLED" == "true" ]]; then
            WIZ_TELEGRAM_ENABLED="false"
            step_ok "Telegram bot will be disabled"
        else
            WIZ_TELEGRAM_ENABLED="true"
            if [[ -z "$EXISTING_BOT_TOKEN" ]]; then
                echo -ne "  ${CYAN}Telegram bot token${RESET} (from @BotFather): "
                read -r EXISTING_BOT_TOKEN
                if [[ -z "$EXISTING_BOT_TOKEN" ]]; then
                    step_fail "Bot token is required when enabling Telegram"
                    press_any_key
                    return
                fi
            fi
            if [[ -z "$EXISTING_ADMIN_IDS" ]]; then
                echo -ne "  ${CYAN}Admin Telegram ID${RESET} (your numeric ID): "
                read -r EXISTING_ADMIN_IDS
                if [[ -z "$EXISTING_ADMIN_IDS" ]] || ! [[ "$EXISTING_ADMIN_IDS" =~ ^[0-9,\ ]+$ ]]; then
                    step_fail "A valid numeric Telegram ID is required"
                    press_any_key
                    return
                fi
            fi
            step_ok "Telegram bot will be enabled"
        fi
    fi

    # ── Review ────────────────────────────────────────────────────────────
    draw_header "Review Changes"

    local review_bot_token
    if [[ "$WIZ_TELEGRAM_ENABLED" == "true" && -n "$EXISTING_BOT_TOKEN" ]]; then
        review_bot_token=$(wizard_mask_secret "$EXISTING_BOT_TOKEN" 8)
    else
        review_bot_token="(disabled)"
    fi

    draw_table "Setting|Value" \
        "Deploy|${WIZ_DEPLOY_MODE}" \
        "DB Engine|${WIZ_DB_DRIVER}" \
        "Mode|${WIZ_MODE}" \
        "APP_BASE_URL|${WIZ_APP_BASE_URL}" \
        "SUB_PANEL_URL|${WIZ_SUB_PANEL_URL}" \
        "JWT_COOKIE_DOMAIN|${WIZ_COOKIE_DOMAIN:-(empty)}" \
        "JWT_COOKIE_SECURE|${WIZ_COOKIE_SECURE}" \
        "ACME_ENABLED|${WIZ_ACME_ENABLED}" \
        "ACME_STAGING|${WIZ_ACME_STAGING}" \
        "TELEGRAM_ENABLED|${WIZ_TELEGRAM_ENABLED}" \
        "BOT_TOKEN|${review_bot_token}" \
        "ADMIN_IDS|${EXISTING_ADMIN_IDS:-(none)}" \
        "Secrets|${DIM}(preserved from existing .env)${RESET}"

    echo ""

    if ! confirm_action "Apply these changes?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # ── Write .env ────────────────────────────────────────────────────────
    # Re-use existing secrets
    local WIZ_BOT_TOKEN="$EXISTING_BOT_TOKEN"
    local WIZ_ADMIN_IDS="$EXISTING_ADMIN_IDS"
    local WIZ_JWT_SECRET="$EXISTING_JWT"
    local WIZ_DB_PASSWORD="$EXISTING_DB_PASS"
    local WIZ_ADMIN_HASH="$EXISTING_ADMIN_HASH"

    local WIZ_ACME_CACHE_DIR="/app/data/acme"
    local WIZ_PROM_TARGET="app:${WIZ_APP_PORT}"
    if [[ "$WIZ_DEPLOY_MODE" == "systemd" ]]; then
        WIZ_ACME_CACHE_DIR="$INSTALL_DIR/data/acme"
        WIZ_PROM_TARGET="localhost:${WIZ_APP_PORT}"
    fi

    wizard_write_env "$WIZ_DEPLOY_MODE" "Reconfigured by nasnet-tool.sh wizard on $(date '+%Y-%m-%d %H:%M:%S')"

    # Reload env
    load_env

    # ── Rebuild & Restart ─────────────────────────────────────────────────
    draw_header "Rebuilding & Restarting"

    if [[ "$WIZ_DEPLOY_MODE" == "docker" ]]; then
        if run_logged "Recreating containers" _compose_cmd_with_files up -d --force-recreate; then
            true
        else
            step_fail "Failed to restart services"
        fi

        echo ""
        action_view_status_inline
    else
        # Systemd: rebuild binary, deploy, restart services
        if run_logged "Rebuilding nasnet-panel" bash -c "cd '$PROJECT_DIR' && make build"; then
            step_ok "Binary rebuilt"
        else
            step_fail "Build failed"
        fi

        # Stop services before deploying (binary can't be overwritten while running)
        step_info "Stopping services..."
        sudo systemctl stop "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend stopped" || true

        # Deploy updated artifacts to install directory
        wizard_deploy_artifacts

        step_info "Starting services..."
        sudo systemctl start "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend started" || step_fail "Backend start failed"
    fi

    echo ""
    step_ok "Reconfiguration complete"
    press_any_key
}

action_auto_update() {
    clear
    draw_box "nasnet-panel Auto-Update (GitHub Release)"

    # ── Detect current version ────────────────────────────────────────────
    local current_version="unknown"
    if [[ -f "$INSTALL_DIR/.version" ]]; then
        current_version=$(cat "$INSTALL_DIR/.version")
    elif [[ -x "$BACKEND_BINARY" ]]; then
        current_version=$("$BACKEND_BINARY" --version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+[^ ]*' || echo "unknown")
    fi

    step_info "Current version: ${BOLD}${current_version}${RESET}"

    # ── Fetch latest release from GitHub ──────────────────────────────────
    draw_header "Checking GitHub Releases"

    local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local curl_args=(-s -f)
    if [[ -n "$GITHUB_TOKEN" ]]; then
        curl_args+=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
    fi

    local release_json
    if ! release_json=$(curl "${curl_args[@]}" "$api_url" 2>/dev/null); then
        step_fail "Failed to fetch latest release from GitHub"
        step_info "If this is a private repo, set GITHUB_TOKEN environment variable"
        press_any_key
        return
    fi

    # Parse tag_name without jq
    local latest_version
    latest_version=$(echo "$release_json" | grep -m1 '"tag_name"' | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [[ -z "$latest_version" ]]; then
        step_fail "Could not parse release tag from GitHub API response"
        press_any_key
        return
    fi

    step_info "Latest release: ${BOLD}${latest_version}${RESET}"

    # ── Compare versions ──────────────────────────────────────────────────
    if [[ "$current_version" == "$latest_version" ]]; then
        step_ok "Already up to date!"
        press_any_key
        return
    fi

    echo ""
    echo -e "  ${BOLD}${current_version}${RESET} → ${GREEN}${BOLD}${latest_version}${RESET}"
    echo ""

    if ! confirm_action "Download and install ${latest_version}?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # ── Detect architecture ───────────────────────────────────────────────
    local arch
    case "$(uname -m)" in
        x86_64)  arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *)
            step_fail "Unsupported architecture: $(uname -m)"
            press_any_key
            return
            ;;
    esac
    step_info "Architecture: linux/${arch}"

    # ── Parse download URLs ───────────────────────────────────────────────
    # Extract browser_download_url entries
    local all_urls
    all_urls=$(echo "$release_json" | grep '"browser_download_url"' | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    _get_asset_url() {
        echo "$all_urls" | grep "$1" | head -1
    }

    local hub_url agent_url checksums_url
    hub_url=$(_get_asset_url "nasnet-panel-linux-${arch}")
    agent_url=$(_get_asset_url "nasnet-agent-linux-${arch}")
    checksums_url=$(_get_asset_url "checksums.txt")

    if [[ -z "$hub_url" || -z "$agent_url" || -z "$checksums_url" ]]; then
        step_fail "Could not find all required assets for linux/${arch} in release ${latest_version}"
        press_any_key
        return
    fi

    # ── Download assets ───────────────────────────────────────────────────
    draw_header "Downloading Assets"

    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf '$tmp_dir'" RETURN

    local dl_args=(-fL)
    if [[ -n "$GITHUB_TOKEN" ]]; then
        dl_args+=(-H "Authorization: Bearer ${GITHUB_TOKEN}" -H "Accept: application/octet-stream")
    fi

    local dl_ok=true
    for asset_name in "nasnet-panel-linux-${arch}" "nasnet-agent-linux-${arch}" "checksums.txt"; do
        local url
        url=$(_get_asset_url "$asset_name")
        if run_logged "Downloading ${asset_name}" curl "${dl_args[@]}" -o "${tmp_dir}/${asset_name}" "$url"; then
            step_ok "${asset_name}"
        else
            step_fail "Failed to download ${asset_name}"
            dl_ok=false
        fi
    done

    if ! $dl_ok; then
        step_fail "Some downloads failed — aborting"
        press_any_key
        return
    fi

    # ── Verify checksums ──────────────────────────────────────────────────
    draw_header "Verifying Checksums"

    local checksum_ok=true
    while IFS= read -r line; do
        local expected_hash expected_file
        expected_hash=$(echo "$line" | awk '{print $1}')
        expected_file=$(echo "$line" | awk '{print $2}')
        # Only verify files we downloaded
        if [[ -f "${tmp_dir}/${expected_file}" ]]; then
            local actual_hash
            actual_hash=$(sha256sum "${tmp_dir}/${expected_file}" | awk '{print $1}')
            if [[ "$actual_hash" == "$expected_hash" ]]; then
                step_ok "${expected_file} checksum OK"
            else
                step_fail "${expected_file} checksum MISMATCH"
                checksum_ok=false
            fi
        fi
    done < "${tmp_dir}/checksums.txt"

    if ! $checksum_ok; then
        step_fail "Checksum verification failed — aborting"
        press_any_key
        return
    fi

    # ── Pre-update backup ─────────────────────────────────────────────────
    draw_header "Pre-Update Backup"

    local db_running=false
    if is_sqlite; then
        [[ -f "$(get_db_path)" ]] && db_running=true
    else
        systemctl is-active postgresql &>/dev/null && db_running=true
    fi

    if $db_running; then
        local bk_dir="${INSTALL_DIR}/data/backups"
        sudo mkdir -p "$bk_dir"
        local backup_ext="sql"
        is_sqlite && backup_ext="db"
        local backup_file="pre_autoupdate_$(date +%Y%m%d_%H%M%S).${backup_ext}"
        if run_logged "Creating database backup" pg_dump_to_file "${bk_dir}/${backup_file}"; then
            step_ok "Backup: ${backup_file}"
        else
            step_warn "Backup failed — continuing anyway"
        fi
    else
        step_warn "Database not running — skipping backup"
    fi

    # ── Stop services ─────────────────────────────────────────────────────
    draw_header "Stopping Services"

    sudo systemctl stop "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend stopped" || true

    # ── Deploy artifacts ──────────────────────────────────────────────────
    draw_header "Deploying ${latest_version}"

    local run_user="${SUDO_USER:-$(whoami)}"
    local run_group
    run_group=$(id -gn "$run_user" 2>/dev/null || echo "$run_user")

    sudo mkdir -p "$INSTALL_DIR"/{bin/{agent,xray},data/{backups,acme}}

    # Hub binary
    sudo cp "${tmp_dir}/nasnet-panel-linux-${arch}" "$INSTALL_DIR/bin/nasnet-panel"
    sudo chmod +x "$INSTALL_DIR/bin/nasnet-panel"
    step_ok "nasnet-panel binary deployed"

    # Agent binary
    sudo cp "${tmp_dir}/nasnet-agent-linux-${arch}" "$INSTALL_DIR/bin/agent/nasnet-agent-linux-${arch}"
    sudo chmod +x "$INSTALL_DIR/bin/agent/nasnet-agent-linux-${arch}"
    step_ok "nasnet-agent binary deployed"

    # Also on update: a box installed before xray shipped has none.
    install_xray_core || step_warn "xray-core was not installed — the panel will start but xray will not"

    # Write version marker
    echo "$latest_version" | sudo tee "$INSTALL_DIR/.version" >/dev/null

    # Set ownership
    sudo chown -R "$run_user:$run_group" "$INSTALL_DIR"

    # ── Restart services ──────────────────────────────────────────────────
    draw_header "Starting Services"

    sudo systemctl start "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend started" || step_fail "Backend start failed"

    # ── Verify health ─────────────────────────────────────────────────────
    load_env
    local app_port="${APP_PORT:-9761}"
    local health_url="http://127.0.0.1:${app_port}/health"

    echo ""
    step_info "Waiting for health check..."
    local retries=0
    local max_retries=15
    local healthy=false
    while (( retries < max_retries )); do
        if curl -sf "$health_url" >/dev/null 2>&1; then
            healthy=true
            break
        fi
        sleep 2
        retries=$(( retries + 1 ))
    done

    if $healthy; then
        step_ok "Health check passed"
    else
        step_warn "Health check did not pass within $(( max_retries * 2 ))s — service may still be starting"
    fi

    # ── Summary ───────────────────────────────────────────────────────────
    echo ""
    draw_header "Update Complete"
    echo -e "  ${BOLD}${current_version}${RESET} → ${GREEN}${BOLD}${latest_version}${RESET}"
    echo ""

    local be_status
    be_status=$(systemctl is-active "$BACKEND_SERVICE" 2>/dev/null || echo "inactive")
    local be_colored
    [[ "$be_status" == "active" ]] && be_colored="${GREEN}● active${RESET}" || be_colored="${RED}● ${be_status}${RESET}"
    draw_table "Service|Status" "${BACKEND_SERVICE}|${be_colored}"

    # Update cached version
    _CURRENT_VERSION="$latest_version"
    _UPDATE_BEHIND=0

    press_any_key
}

wizard_update() {
    clear
    draw_box "nasnet-panel Update"

    # Check prerequisites
    if ! command -v git &>/dev/null; then
        step_fail "git is not installed"
        press_any_key
        return
    fi

    load_env
    local deploy_mode
    deploy_mode=$(wizard_get_deploy_mode)
    [[ -z "$deploy_mode" ]] && deploy_mode="docker"

    # Validate deployment tool is available
    if [[ "$deploy_mode" == "docker" ]]; then
        local compose_cmd
        compose_cmd=$(get_compose_cmd)
        if [[ -z "$compose_cmd" ]]; then
            step_fail "docker compose not found"
            press_any_key
            return
        fi
    fi

    # Show current version
    draw_header "Current Version"

    local current_version
    current_version=$(cd "$PROJECT_DIR" && git describe --tags --always --dirty 2>/dev/null || echo "unknown")
    local current_branch
    current_branch=$(cd "$PROJECT_DIR" && git branch --show-current 2>/dev/null || echo "unknown")
    local current_commit
    current_commit=$(cd "$PROJECT_DIR" && git rev-parse --short HEAD 2>/dev/null || echo "unknown")

    step_info "Version:  ${BOLD}${current_version}${RESET}"
    step_info "Branch:   ${current_branch}"
    step_info "Commit:   ${current_commit}"
    step_info "Deploy:   ${deploy_mode}"

    # Fetch updates
    draw_header "Checking for Updates"

    step_info "Fetching from remote..."
    if ! (cd "$PROJECT_DIR" && git fetch origin "$current_branch" 2>/dev/null); then
        step_fail "Failed to fetch updates"
        press_any_key
        return
    fi

    local local_hash
    local_hash=$(cd "$PROJECT_DIR" && git rev-parse HEAD 2>/dev/null)
    local remote_hash
    remote_hash=$(cd "$PROJECT_DIR" && git rev-parse "origin/${current_branch}" 2>/dev/null)

    local needs_pull=false
    local rebuild_only=false

    if [[ "$local_hash" == "$remote_hash" ]]; then
        # Git is up to date — check if a rebuild is needed
        # This handles the case where the user manually ran 'git pull'
        local needs_rebuild=false
        if [[ "$deploy_mode" == "systemd" ]]; then
            if [[ ! -f "$BACKEND_BINARY" ]]; then
                needs_rebuild=true
            elif [[ "$PROJECT_DIR/go.sum" -nt "$BACKEND_BINARY" ]] || \
                 [[ "$PROJECT_DIR/go.mod" -nt "$BACKEND_BINARY" ]] || \
                 [[ -n "$(find "$PROJECT_DIR" -name '*.go' -newer "$BACKEND_BINARY" -print -quit 2>/dev/null)" ]]; then
                needs_rebuild=true
            fi
        else
            # Docker mode: check if images exist
            local app_image
            app_image=$(_compose_cmd_with_files images app --format '{{.ID}}' 2>/dev/null || true)
            if [[ -z "$app_image" ]]; then
                needs_rebuild=true
            fi
        fi

        if $needs_rebuild; then
            step_info "Code is up to date but artifacts need rebuilding"
            echo ""
            if ! confirm_action "Rebuild and redeploy?"; then
                step_info "Cancelled"
                press_any_key
                return
            fi
            rebuild_only=true
        else
            step_ok "Already up to date!"
            press_any_key
            return
        fi
    else
        needs_pull=true

        # Show what will change
        local behind_count
        behind_count=$(cd "$PROJECT_DIR" && git rev-list HEAD.."origin/${current_branch}" --count 2>/dev/null || echo "?")
        step_info "${behind_count} commit(s) behind origin/${current_branch}"

        echo ""
        step_info "New commits:"
        (cd "$PROJECT_DIR" && git log --oneline HEAD.."origin/${current_branch}" 2>/dev/null | head -10) | while IFS= read -r line; do
            echo -e "    ${DIM}${line}${RESET}"
        done

        local latest_version
        latest_version=$(cd "$PROJECT_DIR" && git describe --tags --always "origin/${current_branch}" 2>/dev/null || echo "$remote_hash")

        echo ""
        echo -e "  ${BOLD}${current_version}${RESET} → ${GREEN}${BOLD}${latest_version}${RESET}"
        echo ""

        if ! confirm_action "Apply update?"; then
            step_info "Cancelled"
            press_any_key
            return
        fi
    fi

    # ── Pre-update backup ─────────────────────────────────────────────────
    draw_header "Pre-Update Backup"

    local db_running=false
    if is_sqlite; then
        if [[ "$deploy_mode" == "docker" ]]; then
            docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_BACKEND}$" && db_running=true
        else
            [[ -f "$(get_db_path)" ]] && db_running=true
        fi
    else
        if [[ "$deploy_mode" == "docker" ]]; then
            docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_DB}$" && db_running=true
        else
            systemctl is-active postgresql &>/dev/null && db_running=true
        fi
    fi

    if $db_running; then
        mkdir -p "$BACKUP_DIR"
        local backup_ext="sql"
        is_sqlite && backup_ext="db"
        local backup_file="pre_update_$(date +%Y%m%d_%H%M%S).${backup_ext}"
        if [[ "$deploy_mode" == "docker" ]] || is_sqlite; then
            if run_logged "Creating database backup" pg_dump_to_file "${BACKUP_DIR}/${backup_file}"; then
                step_ok "Backup: ${backup_file}"
            else
                step_warn "Backup failed — continuing anyway"
            fi
        else
            if run_logged "Creating database backup" bash -c "sudo -u postgres pg_dump '${DB_NAME:-nasnet_panel}' > '${BACKUP_DIR}/${backup_file}'"; then
                step_ok "Backup: ${backup_file}"
            else
                step_warn "Backup failed — continuing anyway"
            fi
        fi
    else
        step_warn "Database not running — skipping backup"
    fi

    # ── Pull updates (skip if rebuild-only) ───────────────────────────────
    if $needs_pull; then
        draw_header "Pulling Updates"

        if run_logged "Pulling changes" bash -c "cd '$PROJECT_DIR' && git pull --rebase origin '$current_branch'"; then
            true
        else
            step_fail "git pull failed — you may have local changes"
            step_info "Try: git stash && git pull --rebase origin ${current_branch} && git stash pop"
            press_any_key
            return
        fi
    fi

    # ── Rebuild & Restart ─────────────────────────────────────────────────
    draw_header "Rebuilding"

    if [[ "$deploy_mode" == "docker" ]]; then
        _prepare_docker_bind_mounts
        _export_build_env
        if run_logged "Building backend image" _compose_cmd_with_files build app; then
            true
        else
            step_fail "Backend build failed"
        fi

        if run_logged "Recreating containers" _compose_cmd_with_files up -d --force-recreate; then
            true
        else
            step_fail "Failed to restart services"
        fi
    else
        # Systemd: rebuild binary, deploy, and restart
        if run_logged "Building nasnet-panel" bash -c "cd '$PROJECT_DIR' && make build"; then
            true
        else
            step_fail "Build failed"
        fi

        if run_logged "Building agent binaries" bash -c "cd '$PROJECT_DIR' && make build-agent"; then
            true
        else
            step_fail "Agent binary build failed"
        fi

        # Stop services before deploying (binary can't be overwritten while running)
        echo ""
        step_info "Stopping services..."
        sudo systemctl stop "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend stopped" || true

        # Deploy updated artifacts to install directory
        wizard_deploy_artifacts

        echo ""
        step_info "Starting services..."
        sudo systemctl start "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend started" || step_fail "Backend start failed"
    fi

    # ── Post-update Summary ───────────────────────────────────────────────
    echo ""
    draw_header "Update Complete"

    local new_version
    new_version=$(cd "$PROJECT_DIR" && git describe --tags --always 2>/dev/null || echo "unknown")

    echo -e "  ${BOLD}${current_version}${RESET} → ${GREEN}${BOLD}${new_version}${RESET}"
    echo ""
    if [[ "$deploy_mode" == "docker" ]]; then
        action_view_status_inline
    else
        local be_status
        be_status=$(systemctl is-active "$BACKEND_SERVICE" 2>/dev/null || echo "inactive")
        local be_colored
        [[ "$be_status" == "active" ]] && be_colored="${GREEN}● active${RESET}" || be_colored="${RED}● ${be_status}${RESET}"
        draw_table "Service|Status" "${BACKEND_SERVICE}|${be_colored}"
    fi

    # Reset update indicator after successful update
    _CURRENT_VERSION=$(cd "$PROJECT_DIR" && git describe --tags --always 2>/dev/null || echo "")
    _UPDATE_BEHIND=0

    press_any_key
}

wizard_change_ports() {
    clear
    draw_box "Change Ports"

    if [[ ! -f "$ENV_FILE" ]]; then
        step_fail "No .env file found — run Fresh Install first"
        press_any_key
        return
    fi

    load_env

    local deploy_mode
    deploy_mode=$(wizard_get_deploy_mode)
    [[ -z "$deploy_mode" ]] && deploy_mode="docker"

    local old_app_port="${APP_PORT:-9761}"

    draw_header "Current Port"
    draw_table "Service|Port" \
        "APP_PORT|${old_app_port}"

    echo ""
    echo -ne "  ${CYAN}New APP_PORT${RESET} [${old_app_port}]: "
    read -r input_port
    local new_app_port="$old_app_port"
    if [[ -n "$input_port" ]]; then
        if ! [[ "$input_port" =~ ^[0-9]+$ ]] || (( input_port < 1 || input_port > 65535 )); then
            step_fail "Invalid port number: ${input_port}"
            press_any_key
            return
        fi
        new_app_port="$input_port"
    fi

    # Check if anything changed
    if [[ "$new_app_port" == "$old_app_port" ]]; then
        step_info "No changes — port remains the same"
        press_any_key
        return
    fi

    # ── Derive new URLs ──────────────────────────────────────────────────
    local old_base_url="${APP_BASE_URL:-}"
    local old_sub_url="${SUB_PANEL_URL:-}"
    local old_prom_target="${PROMETHEUS_TARGET:-}"

    # Replace old ports in URLs with new ports
    local new_base_url="${old_base_url//:${old_app_port}/:${new_app_port}}"
    local new_sub_url="${old_sub_url//:${old_app_port}/:${new_app_port}}"
    local new_prom_target="${old_prom_target//:${old_app_port}/:${new_app_port}}"

    # ── Review ───────────────────────────────────────────────────────────
    draw_header "Review Changes"

    local rows=()
    rows+=("APP_PORT|${old_app_port} → ${GREEN}${new_app_port}${RESET}")
    [[ "$new_base_url" != "$old_base_url" ]] && rows+=("APP_BASE_URL|${new_base_url}")
    [[ "$new_sub_url" != "$old_sub_url" ]] && rows+=("SUB_PANEL_URL|${new_sub_url}")
    [[ "$new_prom_target" != "$old_prom_target" ]] && rows+=("PROMETHEUS_TARGET|${new_prom_target}")

    draw_table "Variable|New Value" "${rows[@]}"

    echo ""

    if ! confirm_action "Apply port changes?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # ── Update .env ──────────────────────────────────────────────────────
    draw_header "Updating Configuration"

    sed -i.bak "s|^APP_PORT=.*|APP_PORT=${new_app_port}|" "$ENV_FILE"

    [[ "$new_base_url" != "$old_base_url" ]] && \
        sed -i.bak "s|^APP_BASE_URL=.*|APP_BASE_URL=${new_base_url}|" "$ENV_FILE"
    [[ "$new_sub_url" != "$old_sub_url" ]] && \
        sed -i.bak "s|^SUB_PANEL_URL=.*|SUB_PANEL_URL=${new_sub_url}|" "$ENV_FILE"
    [[ "$new_prom_target" != "$old_prom_target" ]] && \
        sed -i.bak "s|^PROMETHEUS_TARGET=.*|PROMETHEUS_TARGET=${new_prom_target}|" "$ENV_FILE"

    rm -f "${ENV_FILE}.bak"
    step_ok ".env updated"

    # Sync to install dir for systemd
    _sync_env_to_install_dir

    # Reload env
    load_env

    # ── Restart ──────────────────────────────────────────────────────────
    draw_header "Restarting"

    if [[ "$deploy_mode" == "docker" ]]; then
        if run_logged "Recreating containers" _compose_cmd_with_files up -d --force-recreate; then
            true
        else
            step_fail "Failed to restart services"
        fi

        echo ""
        action_view_status_inline
    else
        # Stop, deploy, start
        step_info "Stopping services..."
        sudo systemctl stop "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend stopped" || true

        wizard_deploy_artifacts

        echo ""
        step_info "Starting services..."
        sudo systemctl start "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend started" || step_fail "Backend start failed"

        # Health check
        echo ""
        step_info "Waiting for backend to become healthy..."
        local retries=0
        local max_retries=15
        while (( retries < max_retries )); do
            if curl -sf --max-time 3 "http://localhost:${new_app_port}/health/ready" &>/dev/null; then
                break
            fi
            sleep 2
            retries=$(( retries + 1 ))
            printf "\r  ${CYAN}⠋${RESET} Waiting... (%d/%ds)" $(( retries * 2 )) $(( max_retries * 2 ))
        done
        printf "\r%60s\r" ""

        echo ""
        if curl -sf --max-time 3 "http://localhost:${new_app_port}/health/ready" &>/dev/null; then
            step_ok "Backend health check passed"
        else
            step_warn "Backend health check failed — check logs: journalctl -u ${BACKEND_SERVICE} -n 30"
        fi
    fi

    echo ""
    draw_header "Port Change Complete"
    draw_table "Service|Port" \
        "APP_PORT|${new_app_port}"

    press_any_key
}

wizard_change_urls() {
    clear
    draw_box "Change URLs"

    if [[ ! -f "$ENV_FILE" ]]; then
        step_fail "No .env file found — run Fresh Install first"
        press_any_key
        return
    fi

    load_env

    local deploy_mode
    deploy_mode=$(_deploy_mode)

    # ── Read current values ───────────────────────────────────────────────
    local old_base_url="${APP_BASE_URL:-}"
    local old_sub_url="${SUB_PANEL_URL:-}"
    local old_base_path="${APP_PANEL_BASE_PATH:-}"
    local old_cookie_secure="${JWT_COOKIE_SECURE:-false}"
    local old_cookie_domain="${JWT_COOKIE_DOMAIN:-}"
    local old_acme_enabled="${ACME_ENABLED:-false}"

    local app_port
    app_port=$(get_app_port)

    # ── Detect current mode ───────────────────────────────────────────────
    local current_mode="domain"
    local current_proto="https"
    local current_host=""

    if [[ "$old_base_url" =~ ^https?://([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+) ]]; then
        current_mode="ip"
        current_proto="http"
        current_host="${BASH_REMATCH[1]}"
    elif [[ "$old_base_url" =~ ^(https?)://([^:/]+) ]]; then
        current_mode="domain"
        current_proto="${BASH_REMATCH[1]}"
        current_host="${BASH_REMATCH[2]}"
    fi

    local mode_label="IP (HTTP)"
    if [[ "$current_mode" == "domain" ]]; then
        if [[ "$current_proto" == "https" ]]; then
            mode_label="Domain (HTTPS)"
        else
            mode_label="Domain (HTTP)"
        fi
    fi

    # ── Show current configuration ────────────────────────────────────────
    draw_header "Current Configuration"
    draw_table "Setting|Value" \
        "Server URL|${old_base_url:-(empty)}" \
        "Panel URL|${old_sub_url:-(empty)}" \
        "Mode|${mode_label}"

    # ── Ask: IP or Domain? ────────────────────────────────────────────────
    draw_header "Server Address"
    echo ""
    echo -e "  How is this server accessed?"
    echo -e "  ${CYAN}1)${RESET} IP address (e.g. 192.168.7.6)"
    echo -e "  ${CYAN}2)${RESET} Domain name (e.g. example.com)"
    echo ""

    local default_choice=1
    [[ "$current_mode" == "domain" ]] && default_choice=2

    echo -ne "  Choice [${default_choice}]: "
    read -r mode_input
    local chosen_mode_num="${mode_input:-$default_choice}"

    local new_mode="ip"
    [[ "$chosen_mode_num" == "2" ]] && new_mode="domain"

    # ── Collect server address ────────────────────────────────────────────
    local new_host=""
    local new_proto="http"

    if [[ "$new_mode" == "ip" ]]; then
        local default_ip="$current_host"
        echo ""
        echo -ne "  ${CYAN}Server IP${RESET} [${default_ip:-(none)}]: "
        read -r ip_input
        new_host="${ip_input:-$default_ip}"

        # Validate IPv4
        if [[ -z "$new_host" ]]; then
            step_error "Server IP is required"
            press_any_key
            return
        fi
        if ! [[ "$new_host" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]]; then
            step_error "Invalid IP address: ${new_host}"
            press_any_key
            return
        fi
        # Check octet ranges
        local i
        for i in 1 2 3 4; do
            if (( BASH_REMATCH[i] > 255 )); then
                step_error "Invalid IP address: ${new_host} (octet ${BASH_REMATCH[i]} > 255)"
                press_any_key
                return
            fi
        done

        new_proto="http"
    else
        local default_domain="$current_host"
        echo ""
        echo -ne "  ${CYAN}Domain${RESET} [${default_domain:-(none)}]: "
        read -r domain_input
        new_host="${domain_input:-$default_domain}"

        # Validate domain
        if [[ -z "$new_host" ]]; then
            step_error "Domain is required"
            press_any_key
            return
        fi
        if [[ "$new_host" == *"://"* ]] || [[ "$new_host" == *":"* ]] || [[ "$new_host" == *"/"* ]]; then
            step_error "Enter the domain only (no http://, port, or path). Example: example.com"
            press_any_key
            return
        fi
        if ! [[ "$new_host" == *.* ]]; then
            step_error "Invalid domain: ${new_host} (must contain at least one dot)"
            press_any_key
            return
        fi
        if [[ "$new_host" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            step_error "That looks like an IP address — choose option 1 for IP mode"
            press_any_key
            return
        fi

        # Ask protocol
        local https_default="Y/n"
        [[ "$current_proto" == "http" && "$current_mode" == "domain" ]] && https_default="y/N"
        echo -ne "  ${CYAN}Use HTTPS?${RESET} [${https_default}]: "
        read -r https_input

        if [[ "$https_default" == "Y/n" ]]; then
            # Default is Yes
            if [[ "$https_input" =~ ^[nN] ]]; then
                new_proto="http"
            else
                new_proto="https"
            fi
        else
            # Default is No
            if [[ "$https_input" =~ ^[yY] ]]; then
                new_proto="https"
            else
                new_proto="http"
            fi
        fi
    fi

    # ── Base path prompt ──────────────────────────────────────────────────
    echo ""
    draw_header "Panel Path (optional)"
    echo -e "  ${DIM}Secret path prefix to hide the panel (e.g. /myp4nel).${RESET}"
    echo -e "  ${DIM}Leave empty to keep current. Type 'none' to clear.${RESET}"
    echo -ne "  ${CYAN}Panel path${RESET} [${old_base_path:-(none)}]: "
    read -r path_input

    local new_base_path
    if [[ "$path_input" == "none" || "$path_input" == "''" || "$path_input" == '""' ]]; then
        new_base_path=""
    elif [[ -z "$path_input" ]]; then
        new_base_path="$old_base_path"
    else
        new_base_path="$path_input"
    fi
    # Validate path
    if [[ -n "$new_base_path" ]]; then
        if [[ "$new_base_path" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9] ]] || [[ "$new_base_path" =~ ^[a-zA-Z0-9]+\.[a-zA-Z] ]]; then
            step_error "Panel path must be a URL path (e.g. /mypath), not a hostname or IP"
            press_any_key
            return
        fi
        new_base_path="${new_base_path%/}"
        [[ "$new_base_path" != /* ]] && new_base_path="/${new_base_path}"
    fi

    # ── Auto-derive all URLs ──────────────────────────────────────────────
    local new_base_url new_sub_url

    if [[ "$new_mode" == "ip" ]]; then
        new_base_url="http://${new_host}:${app_port}"
        new_sub_url="http://${new_host}:${app_port}${new_base_path}"
    else
        # Domain mode: include ports only if non-standard
        if [[ "$new_proto" == "https" ]]; then
            new_base_url="https://${new_host}"
            new_sub_url="https://${new_host}${new_base_path}"
        else
            # HTTP domain: include ports
            new_base_url="http://${new_host}:${app_port}"
            new_sub_url="http://${new_host}:${app_port}${new_base_path}"
        fi
    fi

    # ── Mode-switch side effects ──────────────────────────────────────────
    local new_cookie_secure="$old_cookie_secure"
    local new_cookie_domain="$old_cookie_domain"
    local new_acme_enabled="$old_acme_enabled"
    local mode_switched=false

    if [[ "$new_mode" == "ip" && "$current_mode" == "domain" ]]; then
        mode_switched=true
        new_cookie_secure="false"
        new_cookie_domain=""
        new_acme_enabled="false"
    elif [[ "$new_mode" == "domain" && "$current_mode" == "ip" ]]; then
        mode_switched=true
        new_cookie_domain="$new_host"
        if [[ "$new_proto" == "https" ]]; then
            new_cookie_secure="true"
            new_acme_enabled="true"
        else
            new_cookie_secure="false"
            new_acme_enabled="false"
        fi
    elif [[ "$new_mode" == "domain" && "$current_mode" == "domain" ]]; then
        # Same mode but possibly different protocol or domain
        new_cookie_domain="$new_host"
        if [[ "$new_proto" == "https" ]]; then
            new_cookie_secure="true"
        else
            new_cookie_secure="false"
        fi
    fi

    # ── Build review table ────────────────────────────────────────────────
    _show_url_review() {
        local rows=()
        if [[ "$new_base_url" != "$old_base_url" ]]; then
            rows+=("APP_BASE_URL|${old_base_url:-(empty)} → ${GREEN}${new_base_url}${RESET}")
        else
            rows+=("APP_BASE_URL|${new_base_url} ${DIM}(unchanged)${RESET}")
        fi
        if [[ "$new_sub_url" != "$old_sub_url" ]]; then
            rows+=("SUB_PANEL_URL|${old_sub_url:-(empty)} → ${GREEN}${new_sub_url}${RESET}")
        else
            rows+=("SUB_PANEL_URL|${new_sub_url} ${DIM}(unchanged)${RESET}")
        fi

        if $mode_switched; then
            rows+=("JWT_COOKIE_SECURE|${old_cookie_secure} → ${GREEN}${new_cookie_secure}${RESET}")
            rows+=("JWT_COOKIE_DOMAIN|${old_cookie_domain:-(empty)} → ${GREEN}${new_cookie_domain:-(empty)}${RESET}")
            rows+=("ACME_ENABLED|${old_acme_enabled} → ${GREEN}${new_acme_enabled}${RESET}")
        fi

        draw_table "Variable|Value" "${rows[@]}"
    }

    # ── Check if anything changed ─────────────────────────────────────────
    if [[ "$new_base_url" == "$old_base_url" && \
          "$new_sub_url" == "$old_sub_url" && "$new_base_path" == "$old_base_path" && \
          "$mode_switched" == "false" ]]; then
        step_info "No changes — URLs remain the same"
        press_any_key
        return
    fi

    # ── Review ────────────────────────────────────────────────────────────
    echo ""
    draw_header "Review Changes"
    _show_url_review

    echo ""
    step_info "This will update both .env and the database settings"

    if $mode_switched; then
        local old_mode_label="$mode_label"
        local new_mode_label="IP (HTTP)"
        if [[ "$new_mode" == "domain" ]]; then
            [[ "$new_proto" == "https" ]] && new_mode_label="Domain (HTTPS)" || new_mode_label="Domain (HTTP)"
        fi
        step_warn "Switching from ${old_mode_label} to ${new_mode_label}"
    fi

    echo ""
    echo -e "  ${DIM}Type 'advanced' to set each URL individually.${RESET}"
    echo -ne "  Apply changes? [y/N]: "
    read -r confirm_input

    # ── Advanced mode ─────────────────────────────────────────────────────
    if [[ "$confirm_input" == "advanced" ]]; then
        echo ""
        draw_header "Advanced: Set URLs Individually"
        echo -e "  ${DIM}Press Enter to keep the derived value.${RESET}"
        echo ""

        echo -e "  ${DIM}Base URL for subscription config links${RESET}"
        echo -ne "  ${CYAN}APP_BASE_URL${RESET} [${new_base_url}]: "
        read -r adv_input; [[ -n "$adv_input" ]] && new_base_url="$adv_input"

        echo -e "  ${DIM}Web panel URL for browser redirects from /sub/ links${RESET}"
        echo -ne "  ${CYAN}SUB_PANEL_URL${RESET} [${new_sub_url}]: "
        read -r adv_input; [[ -n "$adv_input" ]] && new_sub_url="$adv_input"

        echo ""
        draw_header "Review Changes (Advanced)"
        _show_url_review

        echo ""

        if ! confirm_action "Apply URL changes?"; then
            step_info "Cancelled"
            press_any_key
            return
        fi
    elif [[ ! "$confirm_input" =~ ^[yY] ]]; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # ── Update .env ───────────────────────────────────────────────────────
    draw_header "Updating Configuration"

    # Helper: update or insert a .env variable
    _env_set() {
        local key="$1" val="$2"
        if grep -q "^${key}=" "$ENV_FILE"; then
            sed -i.bak "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
        else
            echo "${key}=${val}" >> "$ENV_FILE"
        fi
    }

    [[ "$new_base_url" != "$old_base_url" ]]         && _env_set APP_BASE_URL "$new_base_url"
    [[ "$new_sub_url" != "$old_sub_url" ]]           && _env_set SUB_PANEL_URL "$new_sub_url"
    [[ "$new_base_path" != "$old_base_path" ]]       && _env_set APP_PANEL_BASE_PATH "$new_base_path"

    # Mode-switch side effects
    if $mode_switched; then
        _env_set JWT_COOKIE_SECURE "$new_cookie_secure"
        _env_set JWT_COOKIE_DOMAIN "$new_cookie_domain"
        _env_set ACME_ENABLED "$new_acme_enabled"
    fi

    rm -f "${ENV_FILE}.bak"
    step_ok ".env updated"

    # Sync to install dir for systemd
    _sync_env_to_install_dir

    # ── Update database settings ──────────────────────────────────────────
    if require_db; then
        local db_ok=true
        local sql=""
        [[ "$new_base_url" != "$old_base_url" ]] && \
            sql+="UPDATE settings SET value='${new_base_url}' WHERE key='app_base_url';"
        [[ "$new_sub_url" != "$old_sub_url" ]] && \
            sql+="UPDATE settings SET value='${new_sub_url}' WHERE key='sub_panel_url';"

        if [[ -n "$sql" ]]; then
            if is_sqlite; then
                psql_exec "$sql" 2>/dev/null || db_ok=false
            else
                echo "$sql" | psql_exec -q 2>/dev/null || db_ok=false
            fi
        fi

        if $db_ok; then
            step_ok "Database settings updated"
        else
            step_warn "Failed to update database settings — they will sync on next restart"
        fi
    else
        step_warn "Database not reachable — settings will sync from .env on next restart"
    fi

    # Reload env
    load_env

    # ── Restart backend ───────────────────────────────────────────────────
    draw_header "Restarting Backend"

    if [[ "$deploy_mode" == "docker" ]]; then
        if run_logged "Restarting backend" _compose_cmd_with_files restart app; then
            true
        else
            step_fail "Failed to restart backend"
        fi

        echo ""
        action_view_status_inline
    else
        step_info "Restarting backend service..."
        sudo systemctl restart "$BACKEND_SERVICE" 2>/dev/null && step_ok "Backend restarted" || step_fail "Backend restart failed"

        # Health check
        echo ""
        step_info "Waiting for backend to become healthy..."
        local retries=0
        local max_retries=15
        while (( retries < max_retries )); do
            if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
                break
            fi
            sleep 2
            retries=$(( retries + 1 ))
            printf "\r  ${CYAN}⠋${RESET} Waiting... (%d/%ds)" $(( retries * 2 )) $(( max_retries * 2 ))
        done
        printf "\r%60s\r" ""

        if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
            step_ok "Backend health check passed"
        else
            step_warn "Backend health check failed — check logs: journalctl -u ${BACKEND_SERVICE} -n 30"
        fi
    fi

    echo ""
    draw_header "URL Change Complete"
    draw_table "Variable|Value" \
        "APP_BASE_URL|${new_base_url}" \
        "SUB_PANEL_URL|${new_sub_url}"

    press_any_key
}

wizard_view_config() {
    clear
    draw_header "Current Configuration"

    if [[ ! -f "$ENV_FILE" ]]; then
        step_fail "No .env file found"
        press_any_key
        return
    fi

    load_env

    local rows=()
    local _val

    _val=$(wizard_get_deploy_mode);               rows+=("DEPLOY_MODE|${_val:-${DIM}(unset — docker assumed)${RESET}}")

    rows+=(" | ")

    _val="${APP_ENV:-}";                      rows+=("APP_ENV|${_val}")
    _val="${APP_PORT:-9761}";                  rows+=("APP_PORT|${_val}")
    _val="${APP_BASE_URL:-}";                  rows+=("APP_BASE_URL|${_val}")
    _val="${SUB_PANEL_URL:-}";                 rows+=("SUB_PANEL_URL|${_val}")

    rows+=(" | ")

    _val="${DB_DRIVER:-postgres}";              rows+=("DB_DRIVER|${_val}")
    if [[ "${DB_DRIVER:-postgres}" == "sqlite" ]]; then
        _val="${DB_PATH:-}";                   rows+=("DB_PATH|${_val}")
    else
        _val="${DB_HOST:-}";                   rows+=("DB_HOST|${_val}")
        _val="${DB_PORT:-5432}";               rows+=("DB_PORT|${_val}")
        _val="${DB_USER:-}";                   rows+=("DB_USER|${_val}")
        _val=$(wizard_mask_secret "${DB_PASSWORD:-}" 4); rows+=("DB_PASSWORD|${_val}")
        _val="${DB_NAME:-}";                   rows+=("DB_NAME|${_val}")
    fi

    rows+=(" | ")

    _val="${TELEGRAM_ENABLED:-false}";           rows+=("TELEGRAM_ENABLED|${_val}")
    _val=$(wizard_mask_secret "${TELEGRAM_BOT_TOKEN:-}" 8); rows+=("BOT_TOKEN|${_val}")
    _val="${BOT_MODE:-}";                      rows+=("BOT_MODE|${_val}")
    _val="${ADMIN_IDS:-}";                     rows+=("ADMIN_IDS|${_val}")
    _val="${ADMIN_USERNAME:-}";                rows+=("ADMIN_USERNAME|${_val}")
    _val=$(wizard_mask_secret "${ADMIN_PASSWORD_HASH:-}" 6); rows+=("ADMIN_PASS_HASH|${_val}")

    rows+=(" | ")

    _val=$(wizard_mask_secret "${JWT_SECRET_KEY:-}" 8); rows+=("JWT_SECRET|${_val}")
    _val="${JWT_COOKIE_DOMAIN:-${DIM}(empty)${RESET}}"; rows+=("JWT_COOKIE_DOMAIN|${_val}")
    _val="${JWT_COOKIE_SECURE:-}";             rows+=("JWT_COOKIE_SECURE|${_val}")

    rows+=(" | ")

    _val="${ACME_ENABLED:-false}";               rows+=("ACME_ENABLED|${_val}")
    _val="${ACME_EMAIL:-${DIM}(none)${RESET}}"; rows+=("ACME_EMAIL|${_val}")
    _val="${ACME_STAGING:-}";                  rows+=("ACME_STAGING|${_val}")
    _val="${METRICS_ENABLED:-}";               rows+=("METRICS_ENABLED|${_val}")

    draw_table "Variable|Value" "${rows[@]}"

    echo ""
    step_info "File: ${ENV_FILE}"
    press_any_key
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 6.11: Setup Wizard — Menu
# ──────────────────────────────────────────────────────────────────────────────

menu_setup_wizard() {
    while true; do
        local choice
        arrow_menu "Setup Wizard" choice \
            "Fresh Install" \
            "Reconfigure" \
            "Update" \
            "Change Ports" \
            "Change URLs" \
            "View Config" \
            "← Back"

        case $choice in
            0) wizard_install ;;
            1) wizard_reconfigure ;;
            2) wizard_update ;;
            3) wizard_change_ports ;;
            4) wizard_change_urls ;;
            5) wizard_view_config ;;
            *) return ;;
        esac
    done
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 7: Service Actions
# ──────────────────────────────────────────────────────────────────────────────

action_start_services() {
    clear
    draw_header "Start Services"
    require_docker || return 0

    step_info "Starting all services..."
    if spinner "Starting containers" _compose_cmd_with_files up -d; then
        step_ok "All services started"
    else
        step_fail "Failed to start services"
    fi

    echo ""
    action_view_status_inline
    press_any_key
}

action_stop_services() {
    clear
    draw_header "Stop Services"
    require_docker || return 0

    if ! confirm_action "Stop all services?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    if spinner "Stopping containers" _compose_cmd_with_files down; then
        step_ok "All services stopped"
    else
        step_fail "Failed to stop services"
    fi
    press_any_key
}

action_restart_service() {
    local choice
    if is_sqlite; then
        arrow_menu "Restart Service" choice \
            "All Services" \
            "Backend" \
            "← Back"

        [[ $choice -eq -1 || $choice -eq 2 ]] && return
    else
        arrow_menu "Restart Service" choice \
            "All Services" \
            "Backend" \
            "Database" \
            "← Back"

        [[ $choice -eq -1 || $choice -eq 3 ]] && return
    fi

    clear
    draw_header "Restart Service"
    require_docker || return 0

    local target=""
    if is_sqlite; then
        case $choice in
            0) target="" ;;
            1) target="app" ;;
        esac
    else
        case $choice in
            0) target="" ;;
            1) target="app" ;;
            2) target="postgres" ;;
        esac
    fi

    local label
    [[ -z "$target" ]] && label="all services" || label="$target"

    if spinner "Restarting ${label}" _compose_cmd_with_files restart $target; then
        step_ok "Restarted ${label}"
    else
        step_fail "Failed to restart ${label}"
    fi
    press_any_key
}

action_view_status_inline() {
    require_docker || return 0

    local rows=()
    local format='{{.Names}}|{{.Status}}|{{.Ports}}'

    while IFS='|' read -r name status ports; do
        [[ -z "$name" ]] && continue
        # Color the status
        local colored_status
        if echo "$status" | grep -q "Up"; then
            if echo "$status" | grep -q "(healthy)"; then
                colored_status="${GREEN}● Healthy${RESET}"
            else
                colored_status="${YELLOW}● Up${RESET}"
            fi
        else
            colored_status="${RED}● Down${RESET}"
        fi
        # Shorten ports
        local short_ports
        short_ports=$(echo "$ports" | sed 's/0\.0\.0\.0://g' | sed 's/:::/ /g' | cut -c1-30)
        rows+=("${name}|${colored_status}|${short_ports}")
    done < <(docker ps -a --filter "name=nasnet_panel" --format "$format" 2>/dev/null)

    if [[ ${#rows[@]} -eq 0 ]]; then
        step_warn "No nasnet-panel containers found"
        return
    fi

    draw_table "Container|Status|Ports" "${rows[@]}"
}

action_view_status() {
    clear
    draw_header "Service Status"
    action_view_status_inline
    press_any_key
}

action_view_logs() {
    local choice
    if is_sqlite; then
        arrow_menu "View Logs" choice \
            "Backend  (app)" \
            "All Services" \
            "← Back"

        [[ $choice -eq -1 || $choice -eq 2 ]] && return

        require_docker || return 0

        local target=""
        case $choice in
            0) target="app" ;;
            1) target="" ;;
        esac
    else
        arrow_menu "View Logs" choice \
            "Backend  (app)" \
            "Database (postgres)" \
            "All Services" \
            "← Back"

        [[ $choice -eq -1 || $choice -eq 3 ]] && return

        require_docker || return 0

        local target=""
        case $choice in
            0) target="app" ;;
            1) target="postgres" ;;
            2) target="" ;;
        esac
    fi

    clear
    echo -e "  ${DIM}Streaming logs... Press Ctrl+C to stop${RESET}"
    echo ""
    _compose_cmd_with_files logs --tail=100 -f $target || true
    press_any_key
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 8: Database Actions
# ──────────────────────────────────────────────────────────────────────────────

action_db_backup() {
    clear
    draw_header "Create Database Backup"
    require_db || return 0

    mkdir -p "$BACKUP_DIR"
    local ext="sql"
    local spinner_msg="Running pg_dump"
    if is_sqlite; then
        ext="db"
        spinner_msg="Copying SQLite database"
    fi
    local filename
    filename="backup_$(date +%Y%m%d_%H%M%S).${ext}"
    local filepath="${BACKUP_DIR}/${filename}"

    step_info "Creating backup: ${filename}"

    if spinner "$spinner_msg" pg_dump_to_file "${filepath}"; then
        local size
        size=$(du -h "$filepath" | cut -f1)
        step_ok "Backup created: ${BOLD}${filename}${RESET} (${size})"
    else
        rm -f "$filepath"
        step_fail "Backup failed"
    fi
    press_any_key
}

action_db_restore() {
    clear
    draw_header "Restore Database"
    require_db || return 0

    # List available backups
    local backups=()
    if [[ -d "$BACKUP_DIR" ]]; then
        while IFS= read -r -d '' f; do
            backups+=("$(basename "$f")")
        done < <(find "$BACKUP_DIR" \( -name '*.sql' -o -name '*.db' \) -type f -print0 | sort -rz)
    fi

    if [[ ${#backups[@]} -eq 0 ]]; then
        step_warn "No backups found in ${BACKUP_DIR}"
        press_any_key
        return
    fi

    # Add back option
    backups+=("← Back")

    local choice
    arrow_menu "Select backup to restore" choice "${backups[@]}"

    [[ $choice -eq -1 || $choice -eq $(( ${#backups[@]} - 1 )) ]] && return

    local selected="${backups[$choice]}"

    clear
    draw_header "Restore: ${selected}"

    if ! confirm_dangerous "This will DROP the current database and restore from backup." "RESTORE"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # Create safety backup first
    mkdir -p "$BACKUP_DIR"
    local safety_ext="sql"
    is_sqlite && safety_ext="db"
    local safety
    safety="pre_restore_$(date +%Y%m%d_%H%M%S).${safety_ext}"

    step_info "Creating safety backup..."
    if spinner "Safety backup" pg_dump_to_file "${BACKUP_DIR}/${safety}"; then
        step_ok "Safety backup: ${safety}"
    else
        step_fail "Safety backup failed - aborting restore"
        press_any_key
        return
    fi

    if is_sqlite; then
        # SQLite: overwrite the database file
        step_info "Restoring from ${selected}..."
        if spinner "Restoring database" psql_restore_from_file "${BACKUP_DIR}/${selected}"; then
            step_ok "Database restored successfully"
            step_info "Safety backup available: ${safety}"
            echo ""
            step_warn "You may need to restart the backend for changes to take effect"
        else
            step_fail "Restore failed - safety backup: ${safety}"
        fi
    else
        # Drop and recreate schema
        step_info "Dropping schema..."
        if psql_exec -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" &>/dev/null; then
            step_ok "Schema reset"
        else
            step_fail "Schema reset failed - safety backup: ${safety}"
            press_any_key
            return
        fi

        # Restore
        step_info "Restoring from ${selected}..."
        if spinner "Restoring database" psql_restore_from_file "${BACKUP_DIR}/${selected}"; then
            step_ok "Database restored successfully"
            step_info "Safety backup available: ${safety}"
            echo ""
            step_warn "You may need to restart the backend for changes to take effect"
        else
            step_fail "Restore failed - safety backup: ${safety}"
        fi
    fi
    press_any_key
}

action_db_wipe() {
    clear
    draw_header "Wipe Database"
    require_db || return 0

    echo -e "  ${RED}${BOLD}This will permanently destroy ALL data!${RESET}"
    if is_sqlite; then
        echo -e "  ${DIM}This deletes the SQLite database file.${RESET}"
    else
        echo -e "  ${DIM}This drops and recreates the public schema.${RESET}"
    fi
    echo -e "  ${DIM}The application will recreate tables on restart.${RESET}"
    echo ""

    if ! confirm_dangerous "Permanently wipe all database data?" "WIPE-ALL-DATA"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # Safety backup
    mkdir -p "$BACKUP_DIR"
    local safety_ext="sql"
    is_sqlite && safety_ext="db"
    local safety
    safety="pre_wipe_$(date +%Y%m%d_%H%M%S).${safety_ext}"

    step_info "Creating safety backup before wipe..."
    if spinner "Safety backup" pg_dump_to_file "${BACKUP_DIR}/${safety}"; then
        step_ok "Safety backup: ${safety}"
    else
        step_fail "Safety backup failed - aborting"
        press_any_key
        return
    fi

    step_info "Wiping database..."
    if is_sqlite; then
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            rm -f "$(get_db_path)"
        else
            docker exec "$CONTAINER_BACKEND" rm -f "$(get_db_path)"
        fi
        step_ok "Database wiped"
        step_info "Safety backup: ${safety}"
        echo ""
        step_warn "Restart the backend to recreate tables"
    else
        if psql_exec -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" &>/dev/null; then
            step_ok "Database wiped"
            step_info "Safety backup: ${safety}"
            echo ""
            step_warn "Restart the backend to recreate tables"
        else
            step_fail "Wipe failed"
        fi
    fi
    press_any_key
}

action_db_shell() {
    clear
    draw_header "Database Shell"
    require_db || return 0

    if is_sqlite; then
        echo -e "  ${DIM}Entering sqlite3 session... Type .quit to exit${RESET}"
        echo ""
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            sqlite3 "$(get_db_path)" || true
        else
            docker exec -it "$CONTAINER_BACKEND" sqlite3 "$(get_db_path)" || true
        fi
    else
        echo -e "  ${DIM}Entering psql session... Type \\q to exit${RESET}"
        echo ""
        if [[ "$(_deploy_mode)" == "systemd" ]]; then
            PGPASSWORD="$(get_db_password)" psql \
                -h "$(get_db_host)" \
                -U "$(get_db_user)" \
                -d "$(get_db_name)" || true
        else
            docker exec -it "$CONTAINER_DB" psql \
                -U "$(get_db_user)" \
                -d "$(get_db_name)" || true
        fi
    fi
    press_any_key
}

action_db_list_backups() {
    clear
    draw_header "Database Backups"

    if [[ ! -d "$BACKUP_DIR" ]]; then
        step_warn "Backup directory does not exist: ${BACKUP_DIR}"
        press_any_key
        return
    fi

    local rows=()
    local total_size=0

    while IFS= read -r f; do
        [[ -z "$f" ]] && continue
        local name
        name=$(basename "$f")
        local size_bytes
        size_bytes=$(stat -f%z "$f" 2>/dev/null || stat -c%s "$f" 2>/dev/null || echo "0")
        local size_human
        size_human=$(du -h "$f" | cut -f1)
        local mtime
        mtime=$(stat -f "%Sm" -t "%Y-%m-%d %H:%M" "$f" 2>/dev/null || date -r "$f" "+%Y-%m-%d %H:%M" 2>/dev/null || echo "unknown")
        rows+=("${name}|${size_human}|${mtime}")
        total_size=$(( total_size + size_bytes ))
    done < <(find "$BACKUP_DIR" \( -name '*.sql' -o -name '*.db' \) -type f | sort -r)

    if [[ ${#rows[@]} -eq 0 ]]; then
        step_warn "No backups found"
    else
        draw_table "Filename|Size|Date" "${rows[@]}"
        echo ""
        local total_human
        total_human=$(echo "$total_size" | awk '{
            if ($1 >= 1073741824) printf "%.1f GB", $1/1073741824
            else if ($1 >= 1048576) printf "%.1f MB", $1/1048576
            else if ($1 >= 1024) printf "%.1f KB", $1/1024
            else printf "%d B", $1
        }')
        step_info "${#rows[@]} backup(s), total: ${total_human}"
    fi
    press_any_key
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 9: Monitoring Actions
# ──────────────────────────────────────────────────────────────────────────────

action_health_check() {
    clear
    draw_header "Health Check"

    local rows=()
    local deploy_mode
    deploy_mode=$(_deploy_mode)
    local app_port
    app_port=$(get_app_port)

    # Check Backend API
    local api_status
    if curl -sf --max-time 5 "http://localhost:${app_port}/health/ready" &>/dev/null; then
        api_status="${GREEN}● Healthy${RESET}"
    else
        api_status="${RED}● Unreachable${RESET}"
    fi
    rows+=("Backend API|http://localhost:${app_port}|${api_status}")

    # Check Database (mode-aware, driver-aware)
    local db_status
    if is_sqlite; then
        if [[ "$deploy_mode" == "systemd" ]]; then
            if [[ -f "$(get_db_path)" ]]; then
                db_status="${GREEN}● Ready${RESET}"
            else
                db_status="${YELLOW}● No DB file${RESET}"
            fi
            rows+=("Database|SQLite ($(get_db_path))|${db_status}")
        else
            if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_BACKEND}$"; then
                db_status="${GREEN}● Ready${RESET}"
            else
                db_status="${RED}● Container down${RESET}"
            fi
            rows+=("Database|SQLite (embedded)|${db_status}")
        fi
    else
        if [[ "$deploy_mode" == "systemd" ]]; then
            if PGPASSWORD="$(get_db_password)" pg_isready -h "$(get_db_host)" -U "$(get_db_user)" &>/dev/null; then
                db_status="${GREEN}● Ready${RESET}"
            else
                db_status="${RED}● Unreachable${RESET}"
            fi
            rows+=("Database|PostgreSQL (local)|${db_status}")
        else
            if docker exec "$CONTAINER_DB" pg_isready -U "$(get_db_user)" &>/dev/null; then
                db_status="${GREEN}● Ready${RESET}"
            else
                db_status="${RED}● Unreachable${RESET}"
            fi
            rows+=("Database|${CONTAINER_DB}|${db_status}")
        fi
    fi

    # Infrastructure status (mode-aware)
    if [[ "$deploy_mode" == "systemd" ]]; then
        local be_status be_colored
        be_status=$(systemctl is-active "$BACKEND_SERVICE" 2>/dev/null || echo "inactive")
        [[ "$be_status" == "active" ]] && be_colored="${GREEN}● Running${RESET}" || be_colored="${RED}● ${be_status}${RESET}"
        rows+=("${BACKEND_SERVICE}|systemd|${be_colored}")
    else
        local docker_status
        if is_docker_running; then
            docker_status="${GREEN}● Running${RESET}"
        else
            docker_status="${RED}● Not running${RESET}"
        fi
        rows+=("Docker|daemon|${docker_status}")
    fi

    draw_table "Service|Endpoint|Status" "${rows[@]}"
    press_any_key
}

action_system_info() {
    clear
    draw_header "System Info"

    local rows=()
    local deploy_mode
    deploy_mode=$(_deploy_mode)

    # Runtime info (mode-aware)
    if [[ "$deploy_mode" == "docker" ]]; then
        local docker_ver compose_ver
        docker_ver=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "N/A")
        compose_ver=$(docker compose version --short 2>/dev/null || docker-compose version --short 2>/dev/null || echo "N/A")
        rows+=("Deploy Mode|Docker")
        rows+=("Docker|${docker_ver}")
        rows+=("Docker Compose|${compose_ver}")
    else
        local go_ver node_ver
        go_ver=$(go version 2>/dev/null | awk '{print $3}' || echo "N/A")
        node_ver=$(node --version 2>/dev/null || echo "N/A")
        rows+=("Deploy Mode|Systemd (bare-metal)")
        rows+=("Go|${go_ver}")
        rows+=("Node.js|${node_ver}")
        rows+=("Install Dir|${INSTALL_DIR}")
    fi

    # Database info (driver-aware)
    if is_sqlite; then
        rows+=("Database|SQLite")
        local db_path
        db_path="$(get_db_path)"
        if [[ "$deploy_mode" == "systemd" && -f "$db_path" ]]; then
            local db_size
            db_size=$(du -h "$db_path" 2>/dev/null | cut -f1 || echo "N/A")
            rows+=("Database Size|${db_size}")
        elif [[ "$deploy_mode" == "docker" ]]; then
            local db_size
            db_size=$(docker exec "$CONTAINER_BACKEND" du -h "$db_path" 2>/dev/null | cut -f1 || echo "N/A")
            rows+=("Database Size|${db_size}")
        fi
    else
        local pg_ver db_size
        pg_ver=$(psql_exec -tAc "SELECT version();" 2>/dev/null | head -1 | cut -d' ' -f1-2 || echo "N/A")
        rows+=("PostgreSQL|${pg_ver}")

        db_size=$(psql_exec -tAc "SELECT pg_size_pretty(pg_database_size(current_database()));" 2>/dev/null || echo "N/A")
        rows+=("Database Size|${db_size}")

        # Table row counts (mode-aware via psql_exec)
        local table_counts
        table_counts=$(psql_exec -tAc "
            SELECT string_agg(tablename || ': ' || cnt::text, ', ')
            FROM (
                SELECT tablename, (xpath('/row/cnt/text()', xml_count))[1]::text::int AS cnt
                FROM (
                    SELECT tablename, query_to_xml('SELECT count(*) AS cnt FROM ' || quote_ident(tablename), false, false, '') AS xml_count
                    FROM pg_tables
                    WHERE schemaname = 'public'
                    ORDER BY tablename
                ) t
            ) s;" 2>/dev/null || echo "N/A")
        if [[ "$table_counts" != "N/A" && -n "$table_counts" ]]; then
            rows+=("Table Rows|${table_counts:0:60}...")
        fi
    fi

    # Git info
    local git_ver git_branch
    git_ver=$(cd "$PROJECT_DIR" && git describe --tags --always --dirty 2>/dev/null || echo "N/A")
    git_branch=$(cd "$PROJECT_DIR" && git branch --show-current 2>/dev/null || echo "N/A")
    rows+=("Git Version|${git_ver} (${git_branch})")

    # System uptime
    local sys_uptime
    sys_uptime=$(uptime | sed 's/.*up //' | sed 's/,.*load.*//' | xargs)
    rows+=("System Uptime|${sys_uptime}")

    draw_table "Component|Value" "${rows[@]}"
    press_any_key
}

action_disk_usage() {
    clear
    draw_header "Disk Usage"

    local rows=()
    local deploy_mode
    deploy_mode=$(_deploy_mode)

    if [[ "$deploy_mode" == "systemd" ]]; then
        # Install directory
        if [[ -d "$INSTALL_DIR" ]]; then
            local install_size
            install_size=$(du -sh "$INSTALL_DIR" 2>/dev/null | cut -f1)
            rows+=("${INSTALL_DIR}/|${install_size}")
        fi

        # Binary size
        if [[ -f "$INSTALL_DIR/bin/nasnet-panel" ]]; then
            local bin_size
            bin_size=$(du -h "$INSTALL_DIR/bin/nasnet-panel" 2>/dev/null | cut -f1)
            rows+=("Backend binary|${bin_size}")
        fi

        # Database data
        if is_sqlite; then
            if [[ -f "$(get_db_path)" ]]; then
                local sqlite_size
                sqlite_size=$(du -h "$(get_db_path)" 2>/dev/null | cut -f1)
                rows+=("SQLite database|${sqlite_size}")
            fi
        else
            local pg_data
            pg_data=$(sudo du -sh /var/lib/postgresql 2>/dev/null | cut -f1 || echo "")
            [[ -n "$pg_data" ]] && rows+=("PostgreSQL data|${pg_data}")
        fi
    else
        # Data directory
        if [[ -d "$PROJECT_DIR/data" ]]; then
            local data_size
            data_size=$(du -sh "$PROJECT_DIR/data" 2>/dev/null | cut -f1)
            rows+=("data/|${data_size}")
        fi
    fi

    # Backups (works for both)
    if [[ -d "$BACKUP_DIR" ]]; then
        local backup_size backup_count
        backup_size=$(du -sh "$BACKUP_DIR" 2>/dev/null | cut -f1)
        backup_count=$(find "$BACKUP_DIR" \( -name '*.sql' -o -name '*.db' \) -type f 2>/dev/null | wc -l | xargs)
        rows+=("Backups|${backup_size} (${backup_count} files)")
    fi

    if [[ "$deploy_mode" == "docker" ]]; then
        # Docker volumes
        local vol_pg
        vol_pg=$(docker system df -v 2>/dev/null | grep "nasnet.*postgres_data" | awk '{print $4}' | head -1)
        [[ -n "$vol_pg" ]] && rows+=("Docker: postgres_data|${vol_pg}")

        local vol_acme
        vol_acme=$(docker system df -v 2>/dev/null | grep "nasnet.*acme_data" | awk '{print $4}' | head -1)
        [[ -n "$vol_acme" ]] && rows+=("Docker: acme_data|${vol_acme}")

        # Docker images
        local img_total
        img_total=$(docker images --filter "reference=*nasnet*" --format '{{.Repository}}:{{.Tag}} {{.Size}}' 2>/dev/null | head -5)
        if [[ -n "$img_total" ]]; then
            while IFS= read -r line; do
                [[ -n "$line" ]] && rows+=("Image: $(echo "$line" | awk '{print $1}')|$(echo "$line" | awk '{print $2}')")
            done <<< "$img_total"
        fi

        # Docker total disk usage
        local docker_total
        docker_total=$(docker system df --format '{{.Type}}\t{{.Size}}' 2>/dev/null)
        if [[ -n "$docker_total" ]]; then
            while IFS=$'\t' read -r dtype dsize; do
                rows+=("Docker ${dtype}|${dsize}")
            done <<< "$docker_total"
        fi
    fi

    if [[ ${#rows[@]} -gt 0 ]]; then
        draw_table "Path / Resource|Size" "${rows[@]}"
    else
        step_warn "Could not determine disk usage"
    fi
    press_any_key
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 11: Security Actions
# ──────────────────────────────────────────────────────────────────────────────

action_reset_admin_password() {
    clear
    draw_header "Reset Admin Password"

    if ! command -v htpasswd &>/dev/null; then
        # Try to use docker/openssl as fallback
        if ! command -v openssl &>/dev/null; then
            step_fail "Neither htpasswd nor openssl found - cannot hash password"
            step_info "Install apache2-utils: apt install apache2-utils"
            press_any_key
            return
        fi
    fi

    while true; do
        echo -ne "  ${CYAN}New password: ${RESET}"
        read -rs password
        echo ""

        if [[ ${#password} -lt 6 ]]; then
            step_fail "Password must be at least 6 characters — try again"
            continue
        fi

        echo -ne "  ${CYAN}Confirm password: ${RESET}"
        read -rs password2
        echo ""

        if [[ "$password" != "$password2" ]]; then
            step_fail "Passwords do not match — try again"
            continue
        fi

        break
    done

    # Generate bcrypt hash
    local hash
    if command -v htpasswd &>/dev/null; then
        hash=$(htpasswd -nbBC 10 "" "$password" | tr -d ':\n' | sed 's/$2y/$2a/')
    else
        step_fail "htpasswd not available for bcrypt hashing"
        step_info "Install apache2-utils: apt install apache2-utils"
        press_any_key
        return
    fi

    # Update .env file (single-quote hash to prevent bash $-expansion on source)
    if [[ -f "$ENV_FILE" ]]; then
        if grep -q "^ADMIN_PASSWORD_HASH=" "$ENV_FILE"; then
            # Escape special chars in hash for sed
            local escaped_hash
            escaped_hash=$(printf '%s\n' "$hash" | sed 's/[&/\]/\\&/g')
            sed -i.bak "s|^ADMIN_PASSWORD_HASH=.*|ADMIN_PASSWORD_HASH='${escaped_hash}'|" "$ENV_FILE"
            step_ok "Updated ADMIN_PASSWORD_HASH in .env"
        else
            echo "ADMIN_PASSWORD_HASH='${hash}'" >> "$ENV_FILE"
            step_ok "Added ADMIN_PASSWORD_HASH to .env"
        fi
    fi

    # Sync .env to install directory if systemd mode
    _sync_env_to_install_dir

    # Update database if running (mode-aware, driver-aware)
    local deploy_mode db_accessible=false
    deploy_mode=$(_deploy_mode)
    if is_sqlite; then
        if [[ "$deploy_mode" == "systemd" ]]; then
            [[ -f "$(get_db_path)" ]] && db_accessible=true
        else
            docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_BACKEND}$" && db_accessible=true
        fi
    else
        if [[ "$deploy_mode" == "systemd" ]]; then
            PGPASSWORD="$(get_db_password)" pg_isready -h "$(get_db_host)" -U "$(get_db_user)" &>/dev/null && db_accessible=true
        else
            docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_DB}$" && db_accessible=true
        fi
    fi

    if $db_accessible; then
        local escaped_db_hash
        escaped_db_hash=$(printf '%s' "$hash" | sed "s/'/''/g")
        if psql_exec -c "UPDATE admin_credentials SET password_hash = '${escaped_db_hash}' WHERE username = '$(grep "^ADMIN_USERNAME=" "$ENV_FILE" 2>/dev/null | cut -d= -f2 || echo admin)';" &>/dev/null; then
            step_ok "Updated password in database"
        else
            step_warn "Could not update database (table may not exist yet)"
        fi
    fi

    echo ""
    step_warn "Restart the backend for changes to take effect"
    press_any_key
}

action_create_admin_user() {
    clear
    draw_header "Create Admin User"
    require_db || return 0

    echo -ne "  ${CYAN}Username: ${RESET}"
    read -r username

    if [[ -z "$username" ]]; then
        step_fail "Username is required"
        press_any_key
        return
    fi

    echo -ne "  ${CYAN}Telegram ID (or 0 for none): ${RESET}"
    read -r telegram_id
    telegram_id="${telegram_id:-0}"

    # Validate telegram_id is numeric
    if ! [[ "$telegram_id" =~ ^[0-9]+$ ]]; then
        step_fail "Telegram ID must be a number"
        press_any_key
        return
    fi

    # If telegram_id is 0, use negative timestamp as placeholder
    local db_telegram_id="$telegram_id"
    if [[ "$telegram_id" == "0" ]]; then
        db_telegram_id="-$(date +%s%N | head -c 15)"
    fi

    step_info "Creating admin user: ${username} (telegram_id: ${telegram_id})"

    local sql="INSERT INTO users (telegram_id, username, first_name, is_admin, created_at, updated_at)
VALUES (${db_telegram_id}, '${username}', '${username}', true, NOW(), NOW())
ON CONFLICT DO NOTHING
RETURNING id;"

    local result
    result=$(psql_exec -tAc "$sql" 2>&1)

    if [[ -n "$result" && "$result" =~ ^[0-9]+$ ]]; then
        step_ok "Admin user created with ID: ${result}"
    else
        step_fail "Failed to create user: ${result}"
    fi
    press_any_key
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 12: Maintenance Actions
# ──────────────────────────────────────────────────────────────────────────────

action_clean_docker_logs() {
    clear
    draw_header "Clean Docker Logs"
    require_docker || return 0

    step_info "Current Docker log sizes:"
    echo ""

    local total_before=0
    local rows=()

    for container in "$CONTAINER_BACKEND" "$CONTAINER_DB"; do
        local log_path
        log_path=$(docker inspect --format='{{.LogPath}}' "$container" 2>/dev/null || echo "")
        if [[ -n "$log_path" && -f "$log_path" ]]; then
            local size
            size=$(du -h "$log_path" 2>/dev/null | cut -f1)
            local size_bytes
            size_bytes=$(stat -f%z "$log_path" 2>/dev/null || stat -c%s "$log_path" 2>/dev/null || echo "0")
            total_before=$(( total_before + size_bytes ))
            rows+=("${container}|${size}")
        else
            rows+=("${container}|${DIM}No log file${RESET}")
        fi
    done

    draw_table "Container|Log Size" "${rows[@]}"

    if [[ $total_before -eq 0 ]]; then
        step_info "No logs to clean"
        press_any_key
        return
    fi

    if ! confirm_action "Truncate all container logs?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    for container in "$CONTAINER_BACKEND" "$CONTAINER_DB"; do
        local log_path
        log_path=$(docker inspect --format='{{.LogPath}}' "$container" 2>/dev/null || echo "")
        if [[ -n "$log_path" && -f "$log_path" ]]; then
            if sudo truncate -s 0 "$log_path" 2>/dev/null || truncate -s 0 "$log_path" 2>/dev/null; then
                step_ok "Truncated logs for ${container}"
            else
                step_fail "Failed to truncate ${container} (may need sudo)"
            fi
        fi
    done

    local total_human
    total_human=$(echo "$total_before" | awk '{
        if ($1 >= 1073741824) printf "%.1f GB", $1/1073741824
        else if ($1 >= 1048576) printf "%.1f MB", $1/1048576
        else if ($1 >= 1024) printf "%.1f KB", $1/1024
        else printf "%d B", $1
    }')
    echo ""
    step_ok "Space reclaimed: ~${total_human}"
    press_any_key
}

action_clean_old_backups() {
    clear
    draw_header "Clean Old Backups"

    if [[ ! -d "$BACKUP_DIR" ]]; then
        step_warn "No backup directory found"
        press_any_key
        return
    fi

    local backups=()
    while IFS= read -r f; do
        [[ -n "$f" ]] && backups+=("$f")
    done < <(find "$BACKUP_DIR" \( -name '*.sql' -o -name '*.db' \) -type f | sort -r)

    local count=${#backups[@]}
    if [[ $count -eq 0 ]]; then
        step_info "No backups found"
        press_any_key
        return
    fi

    step_info "Found ${count} backup(s)"
    echo -ne "  ${CYAN}How many to keep? ${RESET}[10]: "
    read -r keep
    keep="${keep:-10}"

    if ! [[ "$keep" =~ ^[0-9]+$ ]]; then
        step_fail "Invalid number"
        press_any_key
        return
    fi

    if (( count <= keep )); then
        step_info "Only ${count} backups exist, keeping all"
        press_any_key
        return
    fi

    local to_delete=$(( count - keep ))
    echo ""
    step_info "Will delete ${to_delete} oldest backup(s):"

    # Show what will be deleted (oldest first = end of sorted list)
    for (( i=keep; i<count; i++ )); do
        local name
        name=$(basename "${backups[$i]}")
        local size
        size=$(du -h "${backups[$i]}" | cut -f1)
        echo -e "    ${RED}✘${RESET} ${name} (${size})"
    done

    if ! confirm_action "Delete these ${to_delete} backup(s)?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    local deleted=0
    for (( i=keep; i<count; i++ )); do
        if rm -f "${backups[$i]}"; then
            deleted=$(( deleted + 1 ))
        fi
    done

    step_ok "Deleted ${deleted} backup(s)"
    press_any_key
}

action_docker_prune() {
    clear
    draw_header "Docker Prune"
    require_docker || return 0

    step_info "Reclaimable space estimate:"
    echo ""

    docker system df 2>/dev/null || true

    echo ""
    if ! confirm_action "Run docker system prune (removes unused images, networks, build cache)?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    echo ""
    if spinner "Pruning Docker" docker system prune -f; then
        step_ok "Docker prune complete"
    else
        step_fail "Prune failed"
    fi

    echo ""
    step_info "Updated disk usage:"
    docker system df 2>/dev/null || true
    press_any_key
}

action_clean_journal_logs() {
    clear
    draw_header "Clean Journal Logs"

    if ! command -v journalctl &>/dev/null; then
        step_fail "journalctl not found"
        press_any_key
        return
    fi

    local total_size
    total_size=$(journalctl --disk-usage 2>/dev/null | awk '{print $NF}' || echo "unknown")
    step_info "Total journal usage: ${BOLD}${total_size}${RESET}"

    local rows=()
    local be_size
    be_size=$(journalctl -u "$BACKEND_SERVICE" --disk-usage 2>/dev/null | awk '{print $NF}' || echo "N/A")
    rows+=("${BACKEND_SERVICE}|${be_size}")
    draw_table "Service|Log Size" "${rows[@]}"

    echo ""
    if ! confirm_action "Vacuum journal logs (keep last 7 days)?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    if sudo journalctl --vacuum-time=7d 2>/dev/null; then
        step_ok "Journal logs vacuumed (kept last 7 days)"
    else
        step_fail "Failed to vacuum journal logs"
    fi

    local new_size
    new_size=$(journalctl --disk-usage 2>/dev/null | awk '{print $NF}' || echo "unknown")
    step_info "Journal usage now: ${BOLD}${new_size}${RESET}"
    press_any_key
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 12.5: Uninstall Actions
# ──────────────────────────────────────────────────────────────────────────────

action_uninstall_docker() {
    clear
    draw_header "Uninstall (Docker)"

    echo -e "  ${RED}${BOLD}This will remove all Docker containers, volumes, and images for nasnet-panel.${RESET}"
    echo -e "  ${DIM}The source code and nasnet-tool.sh will NOT be removed.${RESET}"
    echo ""

    if ! confirm_dangerous "Uninstall nasnet-panel Docker deployment?" "UNINSTALL"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    if [[ -n "$(get_compose_cmd)" ]]; then
        step_info "Stopping and removing containers + volumes..."
        if _compose_cmd_with_files down -v 2>/dev/null; then
            step_ok "Containers and volumes removed"
        else
            step_fail "Failed to remove containers"
        fi
    fi

    # Remove Docker images
    echo ""
    if confirm_action "Also remove Docker images?"; then
        step_info "Removing images..."
        docker images --filter "reference=*nasnet*" -q 2>/dev/null | xargs -r docker rmi -f 2>/dev/null || true
        step_ok "Images removed"
    fi

    # Remove systemd wrapper service if exists
    if [[ -f "$SYSTEMD_UNIT_FILE" ]]; then
        step_info "Removing systemd wrapper service..."
        sudo systemctl stop "$SYSTEMD_SERVICE" 2>/dev/null || true
        sudo systemctl disable "$SYSTEMD_SERVICE" 2>/dev/null || true
        sudo rm -f "$SYSTEMD_UNIT_FILE"
        sudo systemctl daemon-reload 2>/dev/null || true
        step_ok "Systemd wrapper removed"
    fi

    # Remove .env
    echo ""
    if confirm_action "Remove .env configuration file?"; then
        rm -f "$ENV_FILE" "$ENV_FILE.bak"
        step_ok ".env removed"
    fi

    echo ""
    step_ok "Docker uninstallation complete"
    step_info "Source code remains at: ${PROJECT_DIR}"
    press_any_key
}

action_uninstall_systemd() {
    clear
    draw_header "Uninstall (Systemd)"

    echo -e "  ${RED}${BOLD}This will remove nasnet-panel services and installed files.${RESET}"
    echo -e "  ${DIM}The source code and nasnet-tool.sh will NOT be removed.${RESET}"
    echo ""

    if [[ -d "$INSTALL_DIR" ]]; then
        step_info "Install directory: ${INSTALL_DIR}"
    fi
    echo ""

    if ! confirm_dangerous "Uninstall nasnet-panel systemd deployment?" "UNINSTALL"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # Stop and remove services
    for svc in "$BACKEND_SERVICE"; do
        local unit_file="/etc/systemd/system/${svc}.service"
        if [[ -f "$unit_file" ]]; then
            step_info "Stopping ${svc}..."
            sudo systemctl stop "$svc" 2>/dev/null || true
            sudo systemctl disable "$svc" 2>/dev/null || true
            sudo rm -f "$unit_file"
            step_ok "${svc} removed"
        fi
    done
    sudo systemctl daemon-reload 2>/dev/null || true
    step_ok "Systemd daemon reloaded"

    # Remove install directory
    if [[ -d "$INSTALL_DIR" ]]; then
        step_info "Removing ${INSTALL_DIR}..."
        sudo rm -rf "$INSTALL_DIR"
        step_ok "Install directory removed"
    fi

    # Optionally drop database
    echo ""
    if is_sqlite; then
        if confirm_action "Also remove the SQLite database file?"; then
            local db_path
            db_path="$(get_db_path)"
            if [[ -f "$db_path" ]]; then
                rm -f "$db_path"
                step_ok "SQLite database removed"
            else
                step_info "No SQLite database file found"
            fi
        fi
    else
        if confirm_action "Also drop the PostgreSQL database and user?"; then
            local db_name db_user
            db_name=$(get_db_name)
            db_user=$(get_db_user)

            step_info "Dropping database '${db_name}'..."
            sudo -u postgres psql -c "DROP DATABASE IF EXISTS ${db_name};" &>/dev/null && step_ok "Database dropped" || step_fail "Failed to drop database"

            if [[ "$db_user" != "postgres" ]]; then
                step_info "Dropping user '${db_user}'..."
                sudo -u postgres psql -c "DROP USER IF EXISTS ${db_user};" &>/dev/null && step_ok "User dropped" || step_fail "Failed to drop user"
            fi
        fi
    fi

    # Remove .env
    echo ""
    if confirm_action "Remove .env configuration file?"; then
        rm -f "$ENV_FILE" "$ENV_FILE.bak"
        step_ok ".env removed"
    fi

    echo ""
    step_ok "Systemd uninstallation complete"
    step_info "Source code remains at: ${PROJECT_DIR}"
    press_any_key
}

action_uninstall() {
    local deploy_mode
    deploy_mode=$(_deploy_mode)

    if [[ "$deploy_mode" == "systemd" ]]; then
        action_uninstall_systemd
    else
        action_uninstall_docker
    fi
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 13: Systemd Service Actions
# ──────────────────────────────────────────────────────────────────────────────

require_systemd() {
    if ! command -v systemctl &>/dev/null; then
        step_fail "systemctl not found — systemd is not available on this system"
        press_any_key
        return 1
    fi
    return 0
}

# Detect which systemd services are installed.
# Sets SYSTEMD_SERVICES array and SYSTEMD_DEPLOY_TYPE ("wizard" | "compose" | "").
detect_systemd_services() {
    SYSTEMD_SERVICES=()
    SYSTEMD_DEPLOY_TYPE=""

    # Wizard-created services (bare-metal deploy)
    local be_unit="/etc/systemd/system/${BACKEND_SERVICE}.service"
    if [[ -f "$be_unit" ]]; then
        SYSTEMD_SERVICES+=("$BACKEND_SERVICE")
        SYSTEMD_DEPLOY_TYPE="wizard"
        return 0
    fi

    # Legacy Docker Compose wrapper service
    if [[ -f "$SYSTEMD_UNIT_FILE" ]]; then
        SYSTEMD_SERVICES+=("$SYSTEMD_SERVICE")
        SYSTEMD_DEPLOY_TYPE="compose"
        return 0
    fi

    return 1
}

# Color a systemd active state
_color_active() {
    local state="$1"
    case "$state" in
        active)   echo -e "${GREEN}● active${RESET}" ;;
        inactive) echo -e "${DIM}● inactive${RESET}" ;;
        failed)   echo -e "${RED}● failed${RESET}" ;;
        *)        echo -e "${YELLOW}● ${state}${RESET}" ;;
    esac
}

_color_enabled() {
    local state="$1"
    case "$state" in
        enabled)  echo -e "${GREEN}enabled${RESET}" ;;
        disabled) echo -e "${DIM}disabled${RESET}" ;;
        *)        echo -e "${YELLOW}${state}${RESET}" ;;
    esac
}

# Show current systemd unit status in a table (supports multiple services)
systemd_status_inline() {
    if ! detect_systemd_services; then
        step_warn "No nasnet-panel systemd services found"
        return
    fi

    local rows=()
    for svc in "${SYSTEMD_SERVICES[@]}"; do
        local active enabled
        active=$(systemctl is-active "$svc" 2>/dev/null || echo "unknown")
        enabled=$(systemctl is-enabled "$svc" 2>/dev/null || echo "unknown")
        rows+=("${svc}|$(_color_active "$active")|$(_color_enabled "$enabled")")
    done

    draw_table "Service|Active|Boot" "${rows[@]}"
}

action_systemd_install() {
    clear
    draw_header "Install Systemd Service"
    require_systemd || return 0

    # If wizard services already exist, tell the user
    if detect_systemd_services && [[ "$SYSTEMD_DEPLOY_TYPE" == "wizard" ]]; then
        step_ok "Wizard-managed services are already installed:"
        for svc in "${SYSTEMD_SERVICES[@]}"; do
            step_info "  /etc/systemd/system/${svc}.service"
        done
        echo ""
        step_info "Use 'View Status' to check them, or 'Delete Service' to remove"
        press_any_key
        return
    fi

    # Docker Compose wrapper install (legacy / docker deploy mode)
    if [[ -f "$SYSTEMD_UNIT_FILE" ]]; then
        step_warn "Unit file already exists at ${SYSTEMD_UNIT_FILE}"
        if ! confirm_action "Overwrite existing unit file?"; then
            step_info "Cancelled"
            press_any_key
            return
        fi
    fi

    local compose_cmd
    compose_cmd=$(get_compose_cmd)
    if [[ -z "$compose_cmd" ]]; then
        step_fail "docker compose not found — cannot create service"
        press_any_key
        return
    fi

    # Resolve full paths for the unit file
    local compose_bin
    compose_bin=$(command -v docker)
    local compose_args="compose -f ${COMPOSE_FILE}"
    if is_sqlite; then
        compose_args="${compose_args} -f ${SQLITE_COMPOSE_FILE}"
    fi
    compose_args="${compose_args} --project-directory ${PROJECT_DIR}"

    step_info "Creating unit file: ${SYSTEMD_UNIT_FILE}"

    sudo tee "$SYSTEMD_UNIT_FILE" > /dev/null << EOF
[Unit]
Description=nasnet-panel (Docker Compose)
Documentation=https://github.com/nasnet-community/nasnet-panel-linux
After=network-online.target docker.service
Requires=docker.service
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=${PROJECT_DIR}
ExecStart=${compose_bin} ${compose_args} up -d
ExecStop=${compose_bin} ${compose_args} down
ExecReload=${compose_bin} ${compose_args} restart
TimeoutStartSec=120
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
EOF

    if [[ $? -eq 0 ]]; then
        step_ok "Unit file written"
    else
        step_fail "Failed to write unit file (need sudo?)"
        press_any_key
        return
    fi

    step_info "Reloading systemd daemon..."
    if sudo systemctl daemon-reload; then
        step_ok "Daemon reloaded"
    else
        step_fail "daemon-reload failed"
        press_any_key
        return
    fi

    echo ""
    systemd_status_inline
    echo ""
    step_info "Use 'Enable Service' to start on boot, then 'Start Service' to run now"
    press_any_key
}

action_systemd_start() {
    clear
    draw_header "Start Systemd Service"
    require_systemd || return 0

    if ! detect_systemd_services; then
        step_fail "No services installed — run 'Install Service' or the setup wizard first"
        press_any_key
        return
    fi

    for svc in "${SYSTEMD_SERVICES[@]}"; do
        step_info "Starting ${svc}..."
        if sudo systemctl start "$svc"; then
            step_ok "${svc} started"
        else
            step_fail "Failed to start ${svc}"
        fi
    done

    echo ""
    systemd_status_inline
    press_any_key
}

action_systemd_stop() {
    clear
    draw_header "Stop Systemd Service"
    require_systemd || return 0

    if ! detect_systemd_services; then
        step_fail "No services installed"
        press_any_key
        return
    fi

    local svc_names
    svc_names=$(printf '%s, ' "${SYSTEMD_SERVICES[@]}")
    svc_names="${svc_names%, }"

    if ! confirm_action "Stop ${svc_names}?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    for svc in "${SYSTEMD_SERVICES[@]}"; do
        step_info "Stopping ${svc}..."
        if sudo systemctl stop "$svc"; then
            step_ok "${svc} stopped"
        else
            step_fail "Failed to stop ${svc}"
        fi
    done

    echo ""
    systemd_status_inline
    press_any_key
}

action_systemd_enable() {
    clear
    draw_header "Enable Systemd Service"
    require_systemd || return 0

    if ! detect_systemd_services; then
        step_fail "No services installed — run 'Install Service' or the setup wizard first"
        press_any_key
        return
    fi

    for svc in "${SYSTEMD_SERVICES[@]}"; do
        step_info "Enabling ${svc} to start on boot..."
        if sudo systemctl enable "$svc"; then
            step_ok "${svc} enabled"
        else
            step_fail "Failed to enable ${svc}"
        fi
    done

    echo ""
    systemd_status_inline
    press_any_key
}

action_systemd_disable() {
    clear
    draw_header "Disable Systemd Service"
    require_systemd || return 0

    if ! detect_systemd_services; then
        step_fail "No services installed"
        press_any_key
        return
    fi

    local svc_names
    svc_names=$(printf '%s, ' "${SYSTEMD_SERVICES[@]}")
    svc_names="${svc_names%, }"

    if ! confirm_action "Disable ${svc_names} from starting on boot?"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    for svc in "${SYSTEMD_SERVICES[@]}"; do
        step_info "Disabling ${svc}..."
        if sudo systemctl disable "$svc"; then
            step_ok "${svc} disabled"
        else
            step_fail "Failed to disable ${svc}"
        fi
    done

    echo ""
    systemd_status_inline
    press_any_key
}

action_systemd_status() {
    clear
    draw_header "Systemd Service Status"
    require_systemd || return 0

    if ! detect_systemd_services; then
        step_warn "No nasnet-panel systemd services found"
        step_info "Use the setup wizard to install with systemd, or 'Install Service' for Docker Compose wrapper"
        press_any_key
        return
    fi

    systemd_status_inline
    echo ""

    draw_separator
    echo ""
    step_info "Full status output:"
    echo ""
    for svc in "${SYSTEMD_SERVICES[@]}"; do
        echo -e "  ${BOLD}── ${svc} ──${RESET}"
        systemctl status "$svc" --no-pager 2>&1 || true
        echo ""
    done
    press_any_key
}

action_systemd_delete() {
    clear
    draw_header "Delete Systemd Service"
    require_systemd || return 0

    if ! detect_systemd_services; then
        step_info "No services installed — nothing to delete"
        press_any_key
        return
    fi

    echo ""
    systemd_status_inline
    echo ""

    local svc_names
    svc_names=$(printf '%s, ' "${SYSTEMD_SERVICES[@]}")
    svc_names="${svc_names%, }"

    if ! confirm_dangerous "This will stop, disable, and remove: ${svc_names}" "DELETE"; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    for svc in "${SYSTEMD_SERVICES[@]}"; do
        local unit_file="/etc/systemd/system/${svc}.service"

        # Stop if running
        local active_state
        active_state=$(systemctl is-active "$svc" 2>/dev/null || echo "unknown")
        if [[ "$active_state" == "active" ]]; then
            step_info "Stopping ${svc}..."
            sudo systemctl stop "$svc" || true
            step_ok "${svc} stopped"
        fi

        # Disable if enabled
        local enabled_state
        enabled_state=$(systemctl is-enabled "$svc" 2>/dev/null || echo "unknown")
        if [[ "$enabled_state" == "enabled" ]]; then
            step_info "Disabling ${svc}..."
            sudo systemctl disable "$svc" || true
            step_ok "${svc} disabled"
        fi

        # Remove unit file
        if [[ -f "$unit_file" ]]; then
            step_info "Removing ${unit_file}..."
            if sudo rm -f "$unit_file"; then
                step_ok "Unit file removed"
            else
                step_fail "Failed to remove ${unit_file}"
            fi
        fi
    done

    step_info "Reloading systemd daemon..."
    sudo systemctl daemon-reload || true
    step_ok "Daemon reloaded"

    echo ""
    step_ok "Systemd service(s) completely removed"
    if [[ "$SYSTEMD_DEPLOY_TYPE" == "compose" ]]; then
        step_info "Containers are still present — use 'Docker Services > Stop' to remove them"
    fi

    # Offer to clean up install directory
    if [[ "$SYSTEMD_DEPLOY_TYPE" == "wizard" ]] && [[ -d "$INSTALL_DIR" ]]; then
        echo ""
        if confirm_action "Also remove installed files at ${INSTALL_DIR}?"; then
            sudo rm -rf "$INSTALL_DIR"
            step_ok "Removed ${INSTALL_DIR}"
        else
            step_info "Keeping ${INSTALL_DIR} (you can remove it manually)"
        fi
    fi
    press_any_key
}

action_systemd_rebuild() {
    clear
    draw_box "Rebuild & Redeploy"
    require_systemd || return 0

    if ! detect_systemd_services; then
        step_info "No systemd services installed — nothing to rebuild"
        step_info "Use the setup wizard or 'Install Service' first"
        press_any_key
        return
    fi

    if [[ "$SYSTEMD_DEPLOY_TYPE" != "wizard" ]]; then
        step_warn "Rebuild is only available for bare-metal (wizard) deployments"
        step_info "For Docker Compose wrapper services, use the Update option instead"
        press_any_key
        return
    fi

    load_env

    # Show current state
    draw_header "Current Status"
    systemd_status_inline

    if [[ -f "$BACKEND_BINARY" ]]; then
        local bin_date
        bin_date=$(date -r "$BACKEND_BINARY" '+%Y-%m-%d %H:%M:%S' 2>/dev/null || stat -c '%y' "$BACKEND_BINARY" 2>/dev/null | cut -d. -f1 || echo "unknown")
        step_info "Binary last built: ${bin_date}"
    else
        step_warn "No existing binary found at ${BACKEND_BINARY}"
    fi

    # Let user choose what to rebuild
    echo ""
    local rebuild_choice
    arrow_menu "What to rebuild?" rebuild_choice \
        "Everything (backend + agents + frontend)" \
        "Backend only" \
        "Agents only" \
        "Frontend (rebuild binary with embedded SPA)" \
        "Backend + Agents" \
        "← Cancel"

    [[ $rebuild_choice -eq -1 || $rebuild_choice -eq 5 ]] && return

    local do_backend=false
    local do_agents=false
    local do_webpanel=false

    case $rebuild_choice in
        0) do_backend=true; do_agents=true; do_webpanel=true ;;
        1) do_backend=true ;;
        2) do_agents=true ;;
        3) do_webpanel=true ;;
        4) do_backend=true; do_agents=true ;;
    esac

    echo ""
    if ! confirm_action "Rebuild and redeploy? Services will be restarted."; then
        step_info "Cancelled"
        press_any_key
        return
    fi

    # ── Build phase ──────────────────────────────────────────────────────
    draw_header "Building"

    local build_failed=false

    # Always build frontend before backend (Go binary embeds web-panel/dist via go:embed)
    if ($do_backend || $do_webpanel) && ! $build_failed; then
        if run_logged "Building frontend" bash -c "cd '$PROJECT_DIR/web-panel' && pnpm install && pnpm build"; then
            step_ok "Frontend built"
        else
            step_fail "Frontend build failed"
            build_failed=true
        fi
    fi

    if $do_backend && ! $build_failed; then
        local cgo_enabled=0
        is_sqlite && cgo_enabled=1

        if run_logged "Downloading Go modules" bash -c "cd '$PROJECT_DIR' && go mod download"; then
            true
        else
            step_fail "Failed to download Go modules"
            build_failed=true
        fi

        if ! $build_failed; then
            if run_logged "Building backend binary" bash -c "cd '$PROJECT_DIR' && CGO_ENABLED=$cgo_enabled go build -ldflags='-w -s' -o nasnet-panel ."; then
                step_ok "Backend binary built"
            else
                step_fail "Backend build failed"
                build_failed=true
            fi
        fi
    fi

    if $do_agents && ! $build_failed; then
        if run_logged "Building agent binaries" bash -c "cd '$PROJECT_DIR' && make build-agent"; then
            step_ok "Agent binaries built"
        else
            step_fail "Agent binary build failed"
            build_failed=true
        fi
    fi

    # If only webpanel was selected (not backend), still need to rebuild Go binary to embed new frontend
    if $do_webpanel && ! $do_backend && ! $build_failed; then
        local cgo_enabled=0
        is_sqlite && cgo_enabled=1
        if run_logged "Rebuilding backend binary with new frontend" bash -c "cd '$PROJECT_DIR' && CGO_ENABLED=$cgo_enabled go build -ldflags='-w -s' -o nasnet-panel ."; then
            step_ok "Binary rebuilt with embedded frontend"
        else
            step_fail "Build failed"
            build_failed=true
        fi
    fi

    if $build_failed; then
        echo ""
        step_fail "Build failed — aborting deploy"
        press_any_key
        return
    fi

    # ── Stop services ────────────────────────────────────────────────────
    echo ""
    draw_header "Deploying"

    step_info "Stopping services..."
    for svc in "${SYSTEMD_SERVICES[@]}"; do
        sudo systemctl stop "$svc" 2>/dev/null && step_ok "${svc} stopped" || true
    done

    # ── Deploy artifacts ─────────────────────────────────────────────────
    wizard_deploy_artifacts

    # ── Restart services ─────────────────────────────────────────────────
    echo ""
    step_info "Starting services..."
    for svc in "${SYSTEMD_SERVICES[@]}"; do
        sudo systemctl start "$svc" 2>/dev/null && step_ok "${svc} started" || step_fail "${svc} start failed"
    done

    # ── Health check ─────────────────────────────────────────────────────
    echo ""
    local app_port
    app_port=$(get_app_port)
    step_info "Waiting for backend to become healthy..."
    local retries=0
    local max_retries=15
    while (( retries < max_retries )); do
        if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
            break
        fi
        sleep 2
        retries=$(( retries + 1 ))
        printf "\r  ${CYAN}⠋${RESET} Waiting... (%d/%ds)" $(( retries * 2 )) $(( max_retries * 2 ))
    done
    printf "\r%60s\r" ""

    # ── Final status ─────────────────────────────────────────────────────
    echo ""
    draw_header "Rebuild Complete"
    systemd_status_inline

    if curl -sf --max-time 3 "http://localhost:${app_port}/health/ready" &>/dev/null; then
        step_ok "Backend health check passed"
    else
        step_warn "Backend health check failed — check logs: journalctl -u ${BACKEND_SERVICE} -n 30"
    fi

    press_any_key
}

# ──────────────────────────────────────────────────────────────────────────────
# Section 14: Category Menus & Main Menu
# ──────────────────────────────────────────────────────────────────────────────

menu_services() {
    while true; do
        local choice
        arrow_menu "Docker Services" choice \
            "Start Services" \
            "Stop Services" \
            "Restart Service" \
            "View Status" \
            "View Logs" \
            "← Back"

        case $choice in
            0) action_start_services ;;
            1) action_stop_services ;;
            2) action_restart_service ;;
            3) action_view_status ;;
            4) action_view_logs ;;
            *) return ;;
        esac
    done
}

menu_database() {
    while true; do
        local choice
        arrow_menu "Database" choice \
            "Create Backup" \
            "Restore Backup" \
            "Wipe Database" \
            "Open DB Shell" \
            "List Backups" \
            "← Back"

        case $choice in
            0) action_db_backup ;;
            1) action_db_restore ;;
            2) action_db_wipe ;;
            3) action_db_shell ;;
            4) action_db_list_backups ;;
            *) return ;;
        esac
    done
}

menu_monitoring() {
    while true; do
        local choice
        arrow_menu "Monitoring" choice \
            "Health Check" \
            "System Info" \
            "Disk Usage" \
            "← Back"

        case $choice in
            0) action_health_check ;;
            1) action_system_info ;;
            2) action_disk_usage ;;
            *) return ;;
        esac
    done
}

menu_security() {
    while true; do
        local choice
        arrow_menu "Security" choice \
            "Reset Admin Password" \
            "Create Admin User" \
            "← Back"

        case $choice in
            0) action_reset_admin_password ;;
            1) action_create_admin_user ;;
            *) return ;;
        esac
    done
}

menu_maintenance() {
    local deploy_mode
    deploy_mode=$(_deploy_mode)

    while true; do
        local choice
        if [[ "$deploy_mode" == "docker" ]]; then
            arrow_menu "Maintenance" choice \
                "Clean Docker Logs" \
                "Clean Old Backups" \
                "Docker Prune" \
                "← Back"
            case $choice in
                0) action_clean_docker_logs ;;
                1) action_clean_old_backups ;;
                2) action_docker_prune ;;
                *) return ;;
            esac
        else
            arrow_menu "Maintenance" choice \
                "Clean Journal Logs" \
                "Clean Old Backups" \
                "← Back"
            case $choice in
                0) action_clean_journal_logs ;;
                1) action_clean_old_backups ;;
                *) return ;;
            esac
        fi
    done
}

menu_systemd() {
    while true; do
        local choice
        arrow_menu "Systemd Services" choice \
            "View Status" \
            "Install Service" \
            "Rebuild & Redeploy" \
            "Start Service" \
            "Stop Service" \
            "Enable (boot)" \
            "Disable (boot)" \
            "Delete Service" \
            "← Back"

        case $choice in
            0) action_systemd_status ;;
            1) action_systemd_install ;;
            2) action_systemd_rebuild ;;
            3) action_systemd_start ;;
            4) action_systemd_stop ;;
            5) action_systemd_enable ;;
            6) action_systemd_disable ;;
            7) action_systemd_delete ;;
            *) return ;;
        esac
    done
}

# ──────────────────────────────────────────────────────────────────────────────
# Main Menu & Entry Point
# ──────────────────────────────────────────────────────────────────────────────

cleanup() {
    tput cnorm 2>/dev/null || true
    echo ""
}

main() {
    trap cleanup EXIT

    # CLI subcommand support
    case "${1:-}" in
        install)
            load_env
            wizard_install
            exit 0
            ;;
        reconfigure)
            load_env
            wizard_reconfigure
            exit 0
            ;;
        update)
            load_env
            wizard_update
            exit 0
            ;;
        auto-update)
            load_env
            action_auto_update
            exit 0
            ;;
        config|view-config)
            load_env
            wizard_view_config
            exit 0
            ;;
        --offline)
            OFFLINE_MODE=true
            load_env 2>/dev/null || true
            wizard_install
            exit 0
            ;;
    esac

    # Auto-detect offline mode if bundle manifest exists
    if [[ -f "$INSTALL_DIR/.bundle-manifest" ]]; then
        OFFLINE_MODE=true
    fi

    # Load environment
    load_env

    # Check current version and available updates
    if [[ -f "$INSTALL_DIR/.version" ]]; then
        _CURRENT_VERSION=$(cat "$INSTALL_DIR/.version")
    else
        _CURRENT_VERSION=$(cd "$PROJECT_DIR" && git describe --tags --always 2>/dev/null || echo "")
    fi
    _UPDATE_BEHIND=0
    if [[ "$OFFLINE_MODE" != "true" ]]; then
        local _ub
        _ub=$(cd "$PROJECT_DIR" && git branch --show-current 2>/dev/null || echo "")
        if [[ -n "$_ub" ]]; then
            (cd "$PROJECT_DIR" && timeout 5 git fetch origin "$_ub" 2>/dev/null) || true
            local _lh _rh
            _lh=$(cd "$PROJECT_DIR" && git rev-parse HEAD 2>/dev/null || echo "")
            _rh=$(cd "$PROJECT_DIR" && git rev-parse "origin/${_ub}" 2>/dev/null || echo "")
            if [[ -n "$_lh" && -n "$_rh" && "$_lh" != "$_rh" ]]; then
                _UPDATE_BEHIND=$(cd "$PROJECT_DIR" && git rev-list HEAD.."origin/${_ub}" --count 2>/dev/null || echo "0")
            fi
        fi
    fi

    # First-run detection: if no .env exists, offer to run wizard
    if [[ ! -f "$ENV_FILE" ]]; then
        clear
        draw_box "nasnet-panel — First Run"
        echo -e "  ${YELLOW}No .env file found.${RESET}"
        echo -e "  ${DIM}It looks like this is a fresh installation.${RESET}"
        echo ""
        if confirm_action "Run the installation wizard now?"; then
            wizard_install
        fi
    fi

    while true; do
        local deploy_mode
        deploy_mode=$(_deploy_mode)

        # Version + update indicator for menu title
        local menu_title=""
        if [[ -n "$_CURRENT_VERSION" ]]; then
            menu_title="v${_CURRENT_VERSION}"
            if [[ $_UPDATE_BEHIND -gt 0 ]]; then
                menu_title="${menu_title}  ·  ${_UPDATE_BEHIND} update(s) available"
            fi
        fi

        # Update button label
        local update_label="Update"
        if [[ $_UPDATE_BEHIND -gt 0 ]]; then
            update_label="Update  (${_UPDATE_BEHIND} new)"
        fi

        # Build menu dynamically based on deploy mode
        local items=() actions=()
        items+=("Setup Wizard");      actions+=("menu_setup_wizard")
        items+=("$update_label");     actions+=("wizard_update")
        items+=("Auto-Update (Release)"); actions+=("action_auto_update")

        if [[ "$deploy_mode" == "docker" ]]; then
            items+=("Docker Services"); actions+=("menu_services")
        else
            items+=("Systemd Services"); actions+=("menu_systemd")
        fi

        items+=("Database");          actions+=("menu_database")
        items+=("Monitoring");        actions+=("menu_monitoring")
        items+=("Security");          actions+=("menu_security")
        items+=("Maintenance");       actions+=("menu_maintenance")
        items+=("Uninstall");         actions+=("action_uninstall")
        items+=("Exit");              actions+=("")

        local choice
        arrow_menu "$menu_title" choice "${items[@]}"

        if [[ $choice -ge 0 && $choice -lt $(( ${#items[@]} - 1 )) ]]; then
            "${actions[$choice]}"
        else
            clear; echo -e "  ${DIM}Goodbye!${RESET}"; echo ""; exit 0
        fi
    done
}

main "$@"

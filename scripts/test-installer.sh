#!/usr/bin/env bash
# Mock-only regression tests. Never invokes apt, systemctl, sudo, or installers.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT
fail() { echo "FAIL: $*" >&2; return 1; }
contains() { grep -Fq -- "$1" "$2" || fail "missing $1 in $2"; }
absent() { if grep -Fq -- "$1" "$2"; then fail "unexpected $1 in $2"; fi; }
setup() {
    source "$ROOT/nasnet-tool.sh"
    PROJECT_DIR="$TEST_DIR/$case_name"; mkdir -p "$PROJECT_DIR"
    INSTALL_DIR="$PROJECT_DIR/install"; mkdir -p "$INSTALL_DIR"
    ENV_FILE="$PROJECT_DIR/.env"; EVENT_LOG="$PROJECT_DIR/events"; : > "$EVENT_LOG"
    OFFLINE_MODE=false; NASNET_HEALTH_ATTEMPTS=1
    clear() { :; }; press_any_key() { :; }; draw_box() { :; }; draw_header() { :; }; draw_table() { :; }
    show_service_logs() { :; }; action_view_status_inline() { :; }
    sudo() { echo "unexpected sudo: $*" >> "$EVENT_LOG"; return 99; }
    systemctl() { echo "unexpected systemctl: $*" >> "$EVENT_LOG"; return 99; }
    curl() { echo "unexpected curl: $*" >> "$EVENT_LOG"; return 99; }
}
answers() {
    WIZ_DEPLOY_MODE=systemd; WIZ_DB_DRIVER=postgres; WIZ_INSTALL_METHOD=release
    WIZ_ROUTER_MODE=false; WIZ_MODE=domain; WIZ_BASE_PATH=/secret
    WIZ_APP_PORT=12345; WIZ_APP_BASE_URL=https://panel.example:12345
    WIZ_SUB_PANEL_URL=https://panel.example:12345/secret
    WIZ_COOKIE_DOMAIN=''; WIZ_COOKIE_SECURE=true; WIZ_ACME_ENABLED=true
    WIZ_ACME_STAGING=false; WIZ_ACME_EMAIL=admin@example.com
    WIZ_TELEGRAM_ENABLED=false; WIZ_BOT_TOKEN=''; WIZ_ADMIN_IDS=''
    WIZ_ADMIN_HASH='$2a$10$already-hashed'; WIZ_JWT_SECRET=existing-jwt
    WIZ_DB_USER=nasnet_panel; WIZ_DB_PASSWORD=existing-password
    WIZ_DB_HOST=localhost; WIZ_DB_PORT=5432; WIZ_DB_NAME=nasnet_panel
    WIZ_PGSQL_SERVICE_NAME=postgresql; WIZ_INSTALL_STATUS=pending
    WIZ_ADMIN_PASS=''; WIZ_TLS_CERT_FILE=''; WIZ_TLS_KEY_FILE=''
}
test_root_sudo() {
    runuser() { printf '%s\n' "$*" >> "$EVENT_LOG"; }
    _nasnet_root_sudo -u postgres psql -c 'SELECT 1' || return 1
    contains '-u postgres -- psql -c SELECT 1' "$EVENT_LOG" || return 1
    sudo() { _nasnet_root_sudo "$@"; }; export -f sudo _nasnet_root_sudo runuser
    export EVENT_LOG
    bash -c 'sudo -E true && sudo -u postgres psql' || return 1
    contains '-u postgres -- psql' "$EVENT_LOG"
}
test_tls_probe() {
    answers; APP_BASE_URL="$WIZ_APP_BASE_URL"; ACME_ENABLED=true
    curl() { printf '%s\n' "$*" >> "$EVENT_LOG"; return 60; }
    if _health_ok 12345; then fail 'bad certificate accepted'; return 1; fi
    contains '--resolve panel.example:12345:127.0.0.1' "$EVENT_LOG" || return 1
    absent '-k' "$EVENT_LOG" || return 1
    absent 'http://' "$EVENT_LOG"
}
test_panel_marker() {
    SUB_PANEL_URL=https://panel.example/secret
    local html='<html>Welcome to nginx</html>'
    curl() { local out=''; while [[ $# -gt 0 ]]; do if [[ "$1" == -o ]]; then out="$2"; shift; fi; shift; done; printf '%s' "$html" > "$out"; printf '200 text/html'; }
    if _panel_url_ok; then fail 'unrelated web server accepted'; return 1; fi
    html='<html><title>NasNet Panel</title><script>window.__CONFIG__={}</script></html>'
    _panel_url_ok
}
test_readiness_failure() {
    APP_PORT=12345; DEPLOY_MODE=systemd
    _health_ok() { return 1; }; _panel_url_ok() { echo public >> "$EVENT_LOG"; return 0; }
    if wizard_verify_install; then fail 'unready service accepted'; return 1; fi
    absent public "$EVENT_LOG"
}
test_path_validation() {
    answers; WIZ_SUB_PANEL_URL=https://panel.example/wrong
    if wizard_validate_access; then fail 'wrong path accepted'; return 1; fi
    WIZ_SUB_PANEL_URL=https://panel.example/secret; wizard_validate_access
}
test_config_roundtrip() {
    answers; WIZ_DB_HOST=db.example; WIZ_DB_PORT=6543; WIZ_PGSQL_SERVICE_NAME=external
    WIZ_TLS_CERT_FILE='/etc/certs/panel.pem'; WIZ_TLS_KEY_FILE='/etc/certs/panel.key'
    WIZ_DB_PASSWORD='apostrophe'"'"' dollar$ backtick` slash\ quote"'
    local expected="$WIZ_DB_PASSWORD"
    wizard_write_env systemd test || return 1
    source "$ENV_FILE"
    [[ "$DB_PASSWORD" == "$expected" && "$DB_USER" == nasnet_panel && "$DB_HOST" == db.example && "$DB_PORT" == 6543 ]] || return 1
    [[ "$INSTALL_METHOD" == release && "$PGSQL_SERVICE_NAME" == external && "$TLS_KEY_FILE" == /etc/certs/panel.key ]] || return 1
    # Reconfiguration callers do not define these WIZ_* fields.
    unset WIZ_DB_USER WIZ_DB_HOST WIZ_DB_PORT WIZ_PGSQL_SERVICE_NAME WIZ_TLS_CERT_FILE WIZ_TLS_KEY_FILE WIZ_INSTALL_METHOD
    wizard_write_env systemd reconfigure || return 1
    source "$ENV_FILE"
    [[ "$DB_USER" == nasnet_panel && "$DB_HOST" == db.example && "$TLS_KEY_FILE" == /etc/certs/panel.key ]]
}
test_config_failure() {
    answers; ENV_FILE="$PROJECT_DIR/missing/.env"
    if wizard_write_env systemd test; then fail 'failed config write accepted'; return 1; fi
}
test_cancel_before_changes() {
    answers; wizard_write_env systemd test >/dev/null || return 1
    arrow_menu() { printf -v "$2" 0; }; confirm_action() { return 1; }
    wizard_apply_install() { echo mutation >> "$EVENT_LOG"; return 0; }
    if wizard_install; then fail 'cancel reported success'; return 1; fi
    [[ ! -s "$EVENT_LOG" ]]
}
test_template_rejected() {
    answers; WIZ_ADMIN_HASH=''; wizard_write_env systemd test >/dev/null || return 1
    arrow_menu() { printf -v "$2" 0; }; confirm_action() { return 0; }
    wizard_apply_install() { echo mutation >> "$EVENT_LOG"; return 0; }
    if wizard_install; then fail 'empty saved password accepted'; return 1; fi
    [[ ! -s "$EVENT_LOG" ]]
}
test_prereq_stops_install() {
    answers
    wizard_prereqs_systemd() { echo prereq >> "$EVENT_LOG"; return 1; }
    wizard_write_env() { echo config >> "$EVENT_LOG"; return 0; }
    wizard_build_start_systemd() { echo build >> "$EVENT_LOG"; return 0; }
    if wizard_apply_install; then fail 'failed prerequisite accepted'; return 1; fi
    absent config "$EVENT_LOG" && absent build "$EVENT_LOG"
}
test_write_stops_install() {
    answers
    wizard_prereqs_systemd() { return 0; }; wizard_write_env() { return 1; }
    wizard_setup_postgres() { echo database >> "$EVENT_LOG"; }
    if wizard_apply_install; then fail 'failed config write accepted'; return 1; fi
    absent database "$EVENT_LOG"
}
test_start_failure() {
    answers; OFFLINE_MODE=true; DB_DRIVER=sqlite
    wizard_deploy_artifacts() { return 0; }; install_xray_core() { return 0; }
    sudo() { if [[ "$1" == tee ]]; then cat >/dev/null; elif [[ "$*" == 'systemctl restart '* ]]; then return 1; fi; }
    wizard_verify_install() { echo verified >> "$EVENT_LOG"; return 0; }
    if wizard_build_start_systemd 12345; then fail 'failed service start accepted'; return 1; fi
    absent verified "$EVENT_LOG"
}
test_deploy_failure() {
    answers; OFFLINE_MODE=true
    wizard_deploy_artifacts() { return 1; }; install_xray_core() { echo xray >> "$EVENT_LOG"; }
    if wizard_build_start_systemd 12345; then fail 'failed deploy accepted'; return 1; fi
    [[ ! -s "$EVENT_LOG" ]]
}
test_no_access_bypass() {
    answers; wizard_write_env systemd test >/dev/null || return 1
    local menu_count=0
    arrow_menu() { menu_count=$((menu_count+1)); case $menu_count in 1) printf -v "$2" 0 ;; 2) printf -v "$2" 1 ;; *) printf -v "$2" 2 ;; esac; }
    confirm_action() { return 0; }; sudo() { return 0; }
    wizard_apply_install() { WIZ_PROVISIONED=false; return 1; }
    wizard_verify_install() { echo verified >> "$EVENT_LOG"; return 0; }
    if wizard_install; then fail 'access-only bypass accepted unfinished provisioning'; return 1; fi
    absent verified "$EVENT_LOG"
}
test_xray_dirs() {
    XRAY_CONFIG_DIR="$PROJECT_DIR/etc-xray"; XRAY_LOG_DIR="$PROJECT_DIR/log-xray"; XRAY_STATE_DIR="$PROJECT_DIR/state"
    sudo() { echo "$*" >> "$EVENT_LOG"; }
    wizard_prepare_xray_dirs || return 1
    contains "chown -R" "$EVENT_LOG" && contains "$XRAY_STATE_DIR" "$EVENT_LOG" && contains 'chmod 750' "$EVENT_LOG"
}
test_offline_xray() {
    OFFLINE_MODE=true; XRAY_BINARY="$PROJECT_DIR/xray"; XRAY_CONFIG_DIR="$PROJECT_DIR/config"
    mkdir -p "$INSTALL_DIR/bin/xray/v$XRAY_VERSION"
    printf binary > "$INSTALL_DIR/bin/xray/v$XRAY_VERSION/xray-linux-amd64"
    detect_arch() { echo amd64; }; wizard_prepare_xray_dirs() { :; }; _install_geofiles() { :; }
    sudo() { echo "$*" >> "$EVENT_LOG"; }
    _download_xray_core() { echo download >> "$EVENT_LOG"; return 1; }
    install_xray_core || return 1
    contains "install -m 755 $INSTALL_DIR/bin/xray/v$XRAY_VERSION/xray-linux-amd64" "$EVENT_LOG" || return 1
    absent download "$EVENT_LOG"
}
test_pg_preserves_role() {
    answers
    sudo() { echo "$*" >> "$EVENT_LOG"; echo 1; }
    psql() { return 0; }
    wizard_setup_postgres nasnet_panel existing-password nasnet_panel || return 1
    absent 'ALTER USER' "$EVENT_LOG" && absent 'CREATE USER' "$EVENT_LOG"
}
test_existing_pg_dependency() {
    PGSQL_INSTALL_DIR="$PROJECT_DIR/pgsql"; mkdir -p "$PGSQL_INSTALL_DIR/bin"
    printf '#!/bin/sh\nexit 0\n' > "$PGSQL_INSTALL_DIR/bin/pg_ctl"; chmod +x "$PGSQL_INSTALL_DIR/bin/pg_ctl"
    systemctl() { [[ "$*" == 'is-active --quiet postgresql' ]]; }
    arrow_menu() { printf -v "$2" 0; }
    wizard_setup_postgres() { echo setup >> "$EVENT_LOG"; return 0; }
    wizard_setup_postgres_offline nasnet_panel existing-password nasnet_panel || return 1
    [[ "$WIZ_PGSQL_SERVICE_NAME" == postgresql ]]
}
for case_name in root_sudo tls_probe panel_marker readiness_failure path_validation config_roundtrip config_failure cancel_before_changes template_rejected prereq_stops_install write_stops_install start_failure deploy_failure no_access_bypass xray_dirs offline_xray pg_preserves_role existing_pg_dependency; do
    if (setup; "test_$case_name"); then echo "PASS $case_name"; else echo "FAIL $case_name" >&2; exit 1; fi
done

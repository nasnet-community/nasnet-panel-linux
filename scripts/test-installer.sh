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
    arrow_menu() { if [[ "$1" == "Review installation" ]]; then printf -v "$2" -2; else printf -v "$2" 0; fi; }; confirm_action() { return 1; }
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
    arrow_menu() { menu_count=$((menu_count+1)); case $menu_count in 1|2) printf -v "$2" 0 ;; 3) printf -v "$2" 1 ;; *) printf -v "$2" 2 ;; esac; }
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
# Scripted menu choices exercise real wizard navigation without touching the host.
# An unexpected prompt aborts the test instead of allowing a runaway loop.
scripted_menu() {
    local expected="${menu_script[$menu_index]:-unexpected}" title="${1}" target="${2}"
    [[ "$title" == "${expected%|*}" ]] || { fail "menu $menu_index: expected ${expected%|*}, got $title"; exit 1; }
    menu_index=$((menu_index+1))
    printf -v "$target" '%s' "${expected##*|}"
}
test_navigation_role_back() {
    answers; WIZ_NAVIGATION=true
    local menu_index=0 menu_script=(
        'Deployment|0' 'How will you use this device?|1' 'Database|-1'
        'How will you use this device?|0' 'Database|0' 'Install method|1'
        'Enable Telegram bot?|2' 'Install method|0' 'Enable Telegram bot?|1'
        'Review installation|0'
    )
    arrow_menu() { scripted_menu "$@"; }
    local access_visits=0
    wizard_prompt_access_mode() { access_visits=$((access_visits+1)); [[ $access_visits -ne 2 ]] || return 2; }
    wizard_collect_install_settings <<< '' || return 1
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_ROUTER_MODE" == false && "$WIZ_INSTALL_METHOD" == release ]] || return 1
    [[ "$WIZ_ADMIN_HASH" == '$2a$10$already-hashed' && "$WIZ_APP_PORT" == 12345 && "$WIZ_BASE_PATH" == /secret ]] || return 1
    [[ ! -s "$EVENT_LOG" && ! -f "$ENV_FILE" ]]
}
test_navigation_docker_back() {
    answers; WIZ_NAVIGATION=true
    local menu_index=0 menu_script=(
        'Deployment|0' 'How will you use this device?|1' 'Database|2'
        'How will you use this device?|2' 'Deployment|1' 'Database|2'
        'Deployment|1' 'Database|1' 'Enable Telegram bot?|1' 'Review installation|0'
    )
    arrow_menu() { scripted_menu "$@"; }
    wizard_prompt_access_mode() { return 0; }
    wizard_collect_install_settings <<< '' || return 1
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_DEPLOY_MODE" == docker && "$WIZ_DB_DRIVER" == sqlite ]] || return 1
    [[ "$WIZ_ROUTER_MODE" == false && "$WIZ_INSTALL_METHOD" == source && "$WIZ_DB_USER" == postgres ]]
}
test_navigation_offline_back() {
    answers; OFFLINE_MODE=true; WIZ_NAVIGATION=true
    local menu_index=0 menu_script=(
        'How will you use this device?|-1' 'How will you use this device?|0'
        'Database|0' 'Database|1' 'Enable Telegram bot?|1' 'Review installation|0'
    )
    arrow_menu() { scripted_menu "$@"; }
    local access_visits=0
    wizard_prompt_access_mode() { access_visits=$((access_visits+1)); [[ $access_visits -ne 1 ]] || return 2; }
    wizard_collect_install_settings <<< '' || return 1
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_INSTALL_METHOD" == offline && "$WIZ_DB_DRIVER" == sqlite ]]
}
test_navigation_review_back() {
    answers; WIZ_NAVIGATION=true; WIZ_ADMIN_HASH=''; WIZ_ADMIN_PASS=''
    local menu_index=0 menu_script=(
        'Deployment|0' 'How will you use this device?|0' 'Database|0' 'Install method|0'
        'Enable Telegram bot?|1' 'Review installation|1'
        'Enable Telegram bot?|1' 'Review installation|-2'
    )
    arrow_menu() { scripted_menu "$@"; }
    wizard_prompt_access_mode() { return 0; }
    if wizard_collect_install_settings <<'INPUT'
secret123
secret123
:back

INPUT
    then fail 'cancel at review accepted'; return 1; fi
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_ADMIN_PASS" == secret123 ]] || return 1
    [[ ! -s "$EVENT_LOG" && ! -f "$ENV_FILE" ]]
}
test_navigation_text_commands() {
    local WIZ_NAVIGATION=true value=retained status=0
    wizard_read_answer value <<< ':back' || status=$?
    [[ $status -eq 2 && "$value" == retained ]] || return 1
    status=0; wizard_read_answer value true <<< ':cancel' || status=$?
    [[ $status -eq 3 && "$value" == retained ]] || return 1
    wizard_read_answer value <<< '' || return 1
    [[ "$value" == retained ]] || return 1
    status=0; wizard_read_answer value < /dev/null || status=$?
    [[ $status -eq 3 ]]
}
test_navigation_access_back() {
    answers
    local WIZ_API_DOMAIN='' WIZ_PANEL_DOMAIN='' WIZ_PATH_GENERATED=true
    local WIZ_DERIVED_API='' WIZ_DERIVED_PANEL='' WIZ_ACCESS_PROTO=https
    local menu_index=0 menu_script=(
        'How will users access this server?|0' 'Select protocol|-1'
        'How will users access this server?|0' 'Select protocol|1'
        'Select protocol|1' 'Public URLs|0'
    )
    arrow_menu() { scripted_menu "$@"; }
    # Back from the first domain prompt returns to protocol; back from the
    # panel-domain text prompt returns to port, retaining the entered domain.
    wizard_prompt_access_mode <<'INPUT'
:back
api.example.com

:back

panel.example.com

INPUT
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_API_DOMAIN" == api.example.com ]] || return 1
    [[ "$WIZ_APP_BASE_URL" == http://api.example.com:12345 && "$WIZ_SUB_PANEL_URL" == http://panel.example.com:12345/secret ]] || return 1
    [[ "$WIZ_TLS_CERT_FILE" == '' && "$WIZ_ACME_ENABLED" == false ]]
}
test_navigation_access_tls() {
    answers
    local WIZ_API_DOMAIN=api.example.com WIZ_PANEL_DOMAIN=panel.example.com WIZ_PATH_GENERATED=true
    local WIZ_DERIVED_API='' WIZ_DERIVED_PANEL='' WIZ_ACCESS_PROTO=https
    local menu_index=0 menu_script=(
        'How will users access this server?|0' 'Select protocol|0' 'Public URLs|0'
        'HTTPS termination|2' 'HTTPS termination|0'
    )
    arrow_menu() { scripted_menu "$@"; }
    # Certificate input Back returns to TLS selection; ACME clears stale files.
    WIZ_TLS_CERT_FILE=/old/cert; WIZ_TLS_KEY_FILE=/old/key
    wizard_prompt_access_mode <<'INPUT'




:back
admin@example.com
INPUT
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_ACME_ENABLED" == true ]] || return 1
    [[ "$WIZ_TLS_CERT_FILE" == '' && "$WIZ_TLS_KEY_FILE" == '' ]]
}
test_navigation_access_cancel() {
    answers
    local menu_index=0 menu_script=('How will users access this server?|-2') status=0
    arrow_menu() { scripted_menu "$@"; }
    wizard_prompt_access_mode || status=$?
    [[ $status -eq 3 && ! -s "$EVENT_LOG" ]]
}
test_navigation_saved_back() {
    answers; wizard_write_env systemd test >/dev/null || return 1
    local before="$(cat "$ENV_FILE")" menu_index=0 menu_script=(
        "Existing configuration at ${ENV_FILE}|0" 'Review installation|1' 'Deployment|-2'
    )
    arrow_menu() { scripted_menu "$@"; }
    wizard_apply_install() { echo mutation >> "$EVENT_LOG"; }
    if wizard_install; then fail 'cancel after saved review accepted'; return 1; fi
    [[ $menu_index -eq ${#menu_script[@]} && "$(cat "$ENV_FILE")" == "$before" && ! -s "$EVENT_LOG" ]]
}
test_navigation_access_preserves_urls() {
    answers
    local WIZ_API_DOMAIN=api.example.com WIZ_PANEL_DOMAIN=panel.example.com WIZ_PATH_GENERATED=true
    local WIZ_DERIVED_API='' WIZ_DERIVED_PANEL='' WIZ_ACCESS_PROTO=https
    local menu_index=0 menu_script=(
        'How will users access this server?|0' 'Select protocol|0' 'Public URLs|0' 'HTTPS termination|1'
        'How will users access this server?|0' 'Select protocol|0' 'Public URLs|0' 'HTTPS termination|1'
    )
    arrow_menu() { scripted_menu "$@"; }
    wizard_prompt_access_mode <<'INPUT' || return 1




https://api.example.com:8443
https://panel.example.com:8443/secret
INPUT
    wizard_prompt_access_mode <<'INPUT' || return 1






INPUT
    [[ $menu_index -eq ${#menu_script[@]} ]] || return 1
    [[ "$WIZ_APP_BASE_URL" == https://api.example.com:8443 && "$WIZ_SUB_PANEL_URL" == https://panel.example.com:8443/secret ]]
}
test_navigation_install_after_review() {
    local menu_index=0 menu_script=(
        'Deployment|0' 'How will you use this device?|0' 'Database|1' 'Install method|0'
        'How will users access this server?|1' 'Public URLs|0' 'Enable Telegram bot?|1'
        'Review installation|0'
    )
    arrow_menu() { scripted_menu "$@"; }
    wizard_random_port() { echo 12345; }; wizard_detect_ip() { echo 192.0.2.1; }
    sudo() { [[ "$*" == -v ]]; }
    wizard_apply_install() {
        [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_ADMIN_PASS" == secret123 ]] || return 1
        [[ "$WIZ_SUB_PANEL_URL" == http://192.0.2.1:12345/secret && "$WIZ_DB_DRIVER" == sqlite ]] || return 1
        WIZ_ADMIN_HASH=fixture-hash; WIZ_JWT_SECRET=fixture-secret
        echo applied >> "$EVENT_LOG"
    }
    _sync_env_to_install_dir() { :; }
    wizard_install <<'INPUT' || return 1

/secret
secret123
secret123
INPUT
    contains applied "$EVENT_LOG" || return 1
    contains 'INSTALL_STATUS=complete' "$ENV_FILE"
}
test_password_masking() {
    local WIZ_NAVIGATION=true value='' status=0
    wizard_read_answer value true <<< 'a b\$c' > "$PROJECT_DIR/mask" || return 1
    [[ "$value" == 'a b\$c' ]] || return 1
    [[ "$(cat "$PROJECT_DIR/mask")" == '******' ]] || return 1
    # Backspace/Delete erase one star, including when the input is empty.
    wizard_read_answer value true <<< $'\177abc\177\bd\n' > "$PROJECT_DIR/mask" || return 1
    [[ "$value" == ad ]] || return 1
    [[ "$(cat "$PROJECT_DIR/mask")" == $'***\b \b\b \b*' ]] || return 1
    wizard_read_answer value true <<< $'abc\025xy\033[DZ' > "$PROJECT_DIR/mask" || return 1
    [[ "$value" == xyZ ]] || return 1
    [[ "$(cat "$PROJECT_DIR/mask")" == $'***\b \b\b \b\b \b***' ]] || return 1
    wizard_read_answer value true <<< $'éی\177界' > "$PROJECT_DIR/mask" || return 1
    [[ "$value" == 'é界' ]] || return 1
    [[ "$(cat "$PROJECT_DIR/mask")" == $'**\b \b*' ]] || return 1
    wizard_read_answer value true <<< ':back' > "$PROJECT_DIR/mask" || status=$?
    [[ $status -eq 2 && "$value" == é界 ]] || return 1
    status=0; wizard_read_answer value true < /dev/null > "$PROJECT_DIR/mask" || status=$?
    [[ $status -eq 3 && "$value" == é界 ]]
}
test_password_validation_visible() {
    answers; WIZ_NAVIGATION=true; WIZ_ADMIN_HASH=''; WIZ_ADMIN_PASS=''
    local menu_index=0 menu_script=('Review installation|0')
    arrow_menu() { scripted_menu "$@"; }
    # Simulate screen clearing by discarding prior output. Every new prompt
    # must retain the preceding failure after its redraw.
    clear() { printf 'SCREEN\n'; }
    wizard_collect_install_settings password > "$PROJECT_DIR/password-screen" <<'INPUT' || return 1
short
secret123
wrong123
correct123
correct123
INPUT
    [[ "$WIZ_ADMIN_PASS" == correct123 && $menu_index -eq 1 ]] || return 1
    contains 'Password is too short (at least 6 characters)' "$PROJECT_DIR/password-screen" || return 1
    contains 'Passwords do not match' "$PROJECT_DIR/password-screen" || return 1
    absent secret123 "$PROJECT_DIR/password-screen" || return 1
    absent correct123 "$PROJECT_DIR/password-screen" || return 1
    # Errors belong to the second and third screens, not the erased ones.
    awk '/^SCREEN$/ { screen++ }
         /Password is too short/ { if (screen != 2) exit 1; short_seen=1 }
         /Passwords do not match/ { if (screen != 3) exit 1; mismatch_seen=1 }
         END { if (!short_seen || !mismatch_seen) exit 1 }' "$PROJECT_DIR/password-screen"
}
test_navigation_menu_keys() {
    local key_result=-1 ARROW_MENU_DEFAULT=1 WIZ_NAVIGATION=true status=0
    tput() { :; }
    arrow_menu 'Example' key_result 'First' 'Second' <<< '' > /dev/null
    [[ $key_result -eq 1 ]] || return 1
    wizard_navigation_menu 'Example' key_result 'First' <<< q > /dev/null || status=$?
    [[ $status -eq 2 ]] || return 1
    status=0
    wizard_navigation_menu 'Example' key_result 'First' < /dev/null > /dev/null || status=$?
    [[ $status -eq 3 ]]
}
for case_name in password_masking password_validation_visible navigation_access_preserves_urls navigation_install_after_review navigation_role_back navigation_docker_back navigation_offline_back navigation_review_back navigation_text_commands navigation_access_back navigation_access_tls navigation_access_cancel navigation_saved_back navigation_menu_keys root_sudo tls_probe panel_marker readiness_failure path_validation config_roundtrip config_failure cancel_before_changes template_rejected prereq_stops_install write_stops_install start_failure deploy_failure no_access_bypass xray_dirs offline_xray pg_preserves_role existing_pg_dependency; do
    if (setup; "test_$case_name"); then echo "PASS $case_name"; else echo "FAIL $case_name" >&2; exit 1; fi
done

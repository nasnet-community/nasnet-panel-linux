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
        'Deployment|0' 'How will you use this device?|0' 'Database|-1'
        'How will you use this device?|1' 'Database|1' 'Install method|1'
        'Install method|0'
        'Review installation|0'
    )
    arrow_menu() { scripted_menu "$@"; }
    local access_visits=0
    wizard_prompt_access_mode() { access_visits=$((access_visits+1)); [[ $access_visits -ne 1 ]] || return 2; }
    wizard_collect_install_settings <<< '' || return 1
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_ROUTER_MODE" == false && "$WIZ_INSTALL_METHOD" == release ]] || return 1
    [[ "$WIZ_ADMIN_HASH" == '$2a$10$already-hashed' && "$WIZ_APP_PORT" == 12345 && "$WIZ_BASE_PATH" == /secret ]] || return 1
    [[ ! -s "$EVENT_LOG" && ! -f "$ENV_FILE" ]]
}
test_navigation_docker_back() {
    answers; WIZ_NAVIGATION=true
    local menu_index=0 menu_script=(
        'Deployment|0' 'How will you use this device?|0' 'Database|2'
        'How will you use this device?|2' 'Deployment|1' 'Database|2'
        'Deployment|1' 'Database|0' 'Review installation|0'
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
        'How will you use this device?|-1' 'How will you use this device?|1'
        'Database|1' 'Database|0' 'Review installation|0'
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
        'Deployment|0' 'How will you use this device?|1' 'Database|1' 'Install method|0'
        'Review installation|1'
        'Review installation|-2'
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
        'Deployment|0' 'How will you use this device?|1' 'Database|0' 'Install method|0'
        'How will users access this server?|1' 'Panel HTTPS|0' 'Public URLs|0'
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
    curl() { printf '{"status":"ok","tls_fingerprint":"AB:CD:EF"}'; }
    wizard_install <<'INPUT' > "$PROJECT_DIR/ip-output" || return 1

/secret
secret123
secret123
INPUT
    contains applied "$EVENT_LOG" || return 1
    contains 'INSTALL_STATUS=complete' "$ENV_FILE" || return 1
    # Subscription links stay on http; the panel and nodes use https.
    contains 'SUB_PANEL_URL=http://192.0.2.1:12345/secret' "$ENV_FILE" || return 1
    contains 'TLS_SELF_SIGNED=true' "$ENV_FILE" || return 1
    contains 'AGENT_HUB_URL=https://192.0.2.1:12345' "$ENV_FILE" || return 1
    contains 'Web panel: https://192.0.2.1:12345/secret' "$PROJECT_DIR/ip-output"
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
test_first_run_default_yes() {
    confirm_action 'Run the installation wizard now?' yes <<< '' > "$PROJECT_DIR/confirm" || return 1
    contains '[Y/n]' "$PROJECT_DIR/confirm" || return 1
    if confirm_action 'Run the installation wizard now?' yes <<< n > /dev/null; then return 1; fi
    if confirm_action 'Other confirmation' <<< '' > "$PROJECT_DIR/confirm"; then return 1; fi
    contains '[y/N]' "$PROJECT_DIR/confirm" || return 1
    if confirm_action 'Run the installation wizard now?' yes < /dev/null > /dev/null; then return 1; fi
}
test_router_address_detection() {
    local SSH_CONNECTION='198.51.100.4 1234 192.168.8.1 22'
    local address_list=$'2: eth0    inet 192.168.8.1/24 scope global eth0\n3: mgmt0    inet 192.168.99.1/24 scope global mgmt0'
    ip() { printf '%s\n' "$address_list"; }
    [[ "$(wizard_detect_router_ip)" == 192.168.99.1 ]] || return 1
    address_list=$'2: docker0    inet 172.17.0.1/16 scope global docker0\n3: eth1    inet 192.168.4.1/24 scope global eth1\n4: eth0    inet 192.168.8.1/24 scope global eth0'
    [[ "$(wizard_detect_router_ip)" == 192.168.8.1 ]] || return 1
    SSH_CONNECTION=''
    [[ "$(wizard_detect_router_ip)" == 192.168.4.1 ]] || return 1
    address_list=$'2: br-abc123    inet 172.18.0.1/16 scope global br-abc123\n3: br-lan    inet 192.168.5.1/24 scope global br-lan'
    [[ "$(wizard_detect_router_ip)" == 192.168.5.1 ]] || return 1
    address_list=''
    if wizard_detect_router_ip; then fail 'missing router address accepted'; return 1; fi
    [[ ! -s "$EVENT_LOG" ]]
}
test_router_review_back() {
    answers; WIZ_NAVIGATION=true; WIZ_ROUTER_MODE=true; WIZ_ADMIN_HASH=''; WIZ_ADMIN_PASS=''
    local menu_index=0 menu_script=(
        'Deployment|0' 'How will you use this device?|0' 'Database|0' 'Install method|0'
        'Panel HTTPS|0' 'Review installation|1' 'Panel HTTPS|-1' 'Install method|0'
        'Panel HTTPS|1' 'Review installation|-2'
    )
    arrow_menu() { scripted_menu "$@"; }
    wizard_detect_router_ip() { echo 192.168.4.1; }
    wizard_prompt_access_mode() { fail 'Router requested an address'; exit 1; }
    wizard_read_answer() { fail 'Router requested credentials'; exit 1; }
    wizard_gen_password() { echo premature-secret-generation >> "$EVENT_LOG"; return 1; }
    if wizard_collect_install_settings < /dev/null; then return 1; fi
    [[ $menu_index -eq ${#menu_script[@]} && "$WIZ_SELF_SIGNED" == false ]] || return 1
    [[ ! -s "$EVENT_LOG" && ! -f "$ENV_FILE" ]]
}
test_router_defaults_and_credentials() {
    local menu_index=0 menu_script=(
        'Deployment|0' 'How will you use this device?|0' 'Database|0' 'Install method|0'
        'Panel HTTPS|0' 'Review installation|0'
    )
    arrow_menu() {
        if [[ "$1" == 'How will you use this device?' ]]; then
            [[ "$3" == Router && "$ARROW_MENU_DEFAULT" == 0 ]] || exit 1
        elif [[ "$1" == 'Panel HTTPS' ]]; then
            [[ "$3" == 'HTTPS with a self-signed certificate (recommended)' && "$ARROW_MENU_DEFAULT" == 0 ]] || exit 1
        elif [[ "$1" == Database ]]; then
            [[ "$3" == 'SQLite (recommended)' && "$ARROW_MENU_DEFAULT" == 0 ]] || exit 1
        fi
        scripted_menu "$@"
    }
    wizard_detect_router_ip() { echo 192.168.4.1; }
    wizard_prompt_access_mode() { fail 'Router requested an address'; exit 1; }
    wizard_read_answer() { fail 'Router requested credentials'; exit 1; }
    wizard_prereqs_systemd() { [[ "$1" == sqlite ]]; }
    wizard_gen_password() { echo generated >> "$EVENT_LOG"; echo fixture-generated-password; }
    wizard_gen_secret() { echo fixture-jwt; }
    wizard_gen_bcrypt() { [[ "$1" == fixture-generated-password ]] || return 1; echo fixture-hash; }
    wizard_open_firewall() { :; }
    wizard_build_start_systemd() { WIZ_PROVISIONED=true; return 0; }
    _sync_env_to_install_dir() { :; }
    curl() { printf '{"status":"ok","tls_fingerprint":"AB:CD:EF"}'; }
    sudo() {
        case "$1" in
            -v) return 0 ;;
            mkdir|install) command "$@" ;;
            *) fail "unexpected sudo: $*"; return 1 ;;
        esac
    }
    wizard_install < /dev/null > "$PROJECT_DIR/router-output" || return 1
    [[ $menu_index -eq ${#menu_script[@]} ]] || return 1
    contains 'APP_BASE_URL=http://192.168.4.1:9761' "$ENV_FILE" || return 1
    contains 'SUB_PANEL_URL=http://192.168.4.1:9761' "$ENV_FILE" || return 1
    contains 'ROUTER_MODE=true' "$ENV_FILE" || return 1
    contains 'TLS_SELF_SIGNED=true' "$ENV_FILE" || return 1
    contains 'AGENT_HUB_URL=' "$ENV_FILE" || return 1
    absent 'AGENT_HUB_URL=https' "$ENV_FILE" || return 1
    contains 'Web panel: https://192.168.4.1:9761' "$PROJECT_DIR/router-output" || return 1
    contains 'AB:CD:EF' "$PROJECT_DIR/router-output" || return 1
    contains 'DB_DRIVER=sqlite' "$ENV_FILE" || return 1
    contains 'TELEGRAM_ENABLED=false' "$ENV_FILE" || return 1
    absent fixture-generated-password "$ENV_FILE" || return 1
    contains 'Password: fixture-generated-password' "$PROJECT_DIR/router-output" || return 1
    contains 'ADMIN_PASSWORD="fixture-generated-password"' "$INSTALL_DIR/admin-credentials.env" || return 1
    local mode
    mode=$(stat -f '%Lp' "$INSTALL_DIR/admin-credentials.env" 2>/dev/null) || mode=$(stat -c '%a' "$INSTALL_DIR/admin-credentials.env")
    [[ "$mode" == 600 ]] || return 1
    # Resume an interrupted saved installation; it must keep the same password.
    menu_index=0; menu_script=("Existing configuration at ${ENV_FILE}|0" 'Review installation|0')
    wizard_install < /dev/null > "$PROJECT_DIR/retry-output" || return 1
    [[ "$(cat "$EVENT_LOG")" == generated ]] || return 1
    contains 'Saved login details:' "$PROJECT_DIR/retry-output"
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
test_unit_runs_as_root() {
    answers; BACKEND_UNIT_FILE="$PROJECT_DIR/unit"
    sudo() { if [[ "$1" == tee ]]; then cat > "$2"; else echo "$*" >> "$EVENT_LOG"; fi; }
    write_backend_unit > /dev/null || return 1
    contains 'User=root' "$BACKEND_UNIT_FILE" || return 1
    contains 'Group=root' "$BACKEND_UNIT_FILE" || return 1
    contains 'CapabilityBoundingSet=CAP_NET_BIND_SERVICE' "$BACKEND_UNIT_FILE" || return 1
    absent 'NASNET_ROUTER_MODE' "$BACKEND_UNIT_FILE"
}
test_router_unit_caps() {
    answers; WIZ_ROUTER_MODE=true; BACKEND_UNIT_FILE="$PROJECT_DIR/unit"
    sudo() { if [[ "$1" == tee ]]; then cat > "$2"; else echo "$*" >> "$EVENT_LOG"; fi; }
    write_backend_unit > /dev/null || return 1
    contains 'User=root' "$BACKEND_UNIT_FILE" || return 1
    contains 'CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE' "$BACKEND_UNIT_FILE" || return 1
    contains 'Environment=NASNET_ROUTER_MODE=1' "$BACKEND_UNIT_FILE"
}
test_old_unit_switched_to_root() {
    answers; DEPLOY_MODE=systemd; WIZ_ROUTER_MODE=true; BACKEND_UNIT_FILE="$PROJECT_DIR/unit"
    printf '[Service]\nUser=ubuntu\nEnvironment=NASNET_ROUTER_MODE=1\n' > "$BACKEND_UNIT_FILE"
    sudo() { if [[ "$1" == tee ]]; then cat > "$2"; else echo "$*" >> "$EVENT_LOG"; fi; }
    wizard_prepare_xray_dirs() { echo xray-dirs >> "$EVENT_LOG"; }
    install_netrollback_units() { echo netrollback >> "$EVENT_LOG"; }
    root_service_stale || { fail 'login-user unit not seen as stale'; return 1; }
    ensure_root_service > /dev/null || return 1
    contains 'User=root' "$BACKEND_UNIT_FILE" || return 1
    contains "chown -R root:root $INSTALL_DIR" "$EVENT_LOG" || return 1
    contains "chmod 600 $INSTALL_DIR/.env" "$EVENT_LOG" || return 1
    contains xray-dirs "$EVENT_LOG" && contains netrollback "$EVENT_LOG" && contains 'systemctl daemon-reload' "$EVENT_LOG"
}
test_root_unit_left_alone() {
    answers; DEPLOY_MODE=systemd; BACKEND_UNIT_FILE="$PROJECT_DIR/unit"
    printf '[Service]\nUser=root\n' > "$BACKEND_UNIT_FILE"
    if root_service_stale; then fail 'root unit seen as stale'; return 1; fi
    ensure_root_service || return 1
    [[ ! -s "$EVENT_LOG" ]]
}
test_docker_unit_ignored() {
    answers; DEPLOY_MODE=docker; BACKEND_UNIT_FILE="$PROJECT_DIR/unit"
    printf '[Service]\nUser=ubuntu\n' > "$BACKEND_UNIT_FILE"
    if root_service_stale; then fail 'docker wrapper unit seen as stale'; return 1; fi
}
test_update_system_writes_units() {
    answers; DEPLOY_MODE=systemd; WIZ_ROUTER_MODE=true; BACKEND_UNIT_FILE="$PROJECT_DIR/unit"
    sudo() { if [[ "$1" == tee ]]; then cat > "$2"; else echo "$*" >> "$EVENT_LOG"; fi; }
    install_netrollback_units() { echo netrollback >> "$EVENT_LOG"; }
    update_system_files > /dev/null || return 1
    contains 'User=root' "$BACKEND_UNIT_FILE" || return 1
    contains 'Environment=NASNET_ROUTER_MODE=1' "$BACKEND_UNIT_FILE" || return 1
    contains netrollback "$EVENT_LOG" || return 1
    contains "mv -f /usr/local/bin/.nasnet-new" "$EVENT_LOG" || return 1
    contains 'systemctl daemon-reload' "$EVENT_LOG" || return 1
    absent 'restart' "$EVENT_LOG"
}
fake_panel() {
    BACKEND_BINARY="$PROJECT_DIR/nasnet-panel"
    cat > "$BACKEND_BINARY" <<FAKE
#!/usr/bin/env bash
echo "panel \$*" >> "$EVENT_LOG"
[[ "\$1 \$2" == "update status" ]] && printf '{\n  "current": "v1.1.0",\n  "available": {\n    "version": "v1.2.0",\n    "notes": "a } b"\n  }\n}\n'
exit 0
FAKE
    chmod +x "$BACKEND_BINARY"
}
test_auto_update_delegates() {
    fake_panel; echo v1.1.0 > "$INSTALL_DIR/.version"
    sudo() { "$@"; }
    action_auto_update > /dev/null || return 1
    contains 'panel update apply' "$EVENT_LOG" || return 1
    absent 'unexpected' "$EVENT_LOG"
}
test_menu_reads_the_panel_check() {
    fake_panel
    [[ "$(panel_offered_update)" == v1.2.0 ]] || { fail "offered: $(panel_offered_update)"; return 1; }
    absent 'update check' "$EVENT_LOG"
}
test_release_manifest_fields() {
    printf '{\n  "schema": 1,\n  "version": "v1.2.0",\n  "assets": {\n    "nasnet-panel-offline-linux-amd64.tar.gz": {\n      "sha256": "%s",\n      "size": 5\n    },\n    "nasnet-tool.sh": {\n      "sha256": "%s",\n      "size": 7\n    }\n  }\n}\n' \
        "$(printf 'a%.0s' {1..64})" "$(printf 'b%.0s' {1..64})" > "$PROJECT_DIR/release.json"
    [[ "$(manifest_field "$PROJECT_DIR/release.json" version)" == v1.2.0 ]] || { fail version; return 1; }
    [[ "$(manifest_sha "$PROJECT_DIR/release.json" nasnet-tool.sh)" == "$(printf 'b%.0s' {1..64})" ]] || { fail tool; return 1; }
    [[ "$(manifest_sha "$PROJECT_DIR/release.json" nasnet-panel-offline-linux-amd64.tar.gz)" == "$(printf 'a%.0s' {1..64})" ]] || { fail bundle; return 1; }
    [[ -z "$(manifest_sha "$PROJECT_DIR/release.json" nasnet-panel-offline-linux-arm64.tar.gz)" ]]
}
test_unverifiable_signature_is_said() {
    RELEASE_KEYS=()
    : > "$PROJECT_DIR/f"; : > "$PROJECT_DIR/f.sig"
    local rc=0
    verify_release_sig "$PROJECT_DIR/f" "$PROJECT_DIR/f.sig" || rc=$?
    [[ $rc -eq 2 ]] || { fail "rc $rc"; return 1; }
}
test_release_signature_checked() {
    openssl version 2>/dev/null | grep -q '^OpenSSL 3' || { echo "  (no OpenSSL 3 here, skipped)"; return 0; }
    local d="$PROJECT_DIR"
    openssl genpkey -algorithm ed25519 -out "$d/k.pem" 2>/dev/null || return 1
    RELEASE_KEYS=("$(openssl pkey -in "$d/k.pem" -pubout -outform DER 2>/dev/null | base64 | tr -d '\n')")
    echo release > "$d/f"
    openssl pkeyutl -sign -inkey "$d/k.pem" -rawin -in "$d/f" 2>/dev/null | base64 > "$d/f.sig" || return 1
    verify_release_sig "$d/f" "$d/f.sig" || { fail 'good signature refused'; return 1; }
    echo tampered > "$d/f"
    if verify_release_sig "$d/f" "$d/f.sig"; then fail 'bad signature accepted'; return 1; fi
}
test_cli_fetch_takes_the_release_copy() {
    RELEASE_KEYS=()
    local tool="$PROJECT_DIR/tool.sh" sha
    printf '#!/usr/bin/env bash\necho new\n' > "$tool"
    sha=$(sha256sum "$tool" | awk '{print $1}')
    printf '{"version":"v1.2.0","assets":{"nasnet-tool.sh":{"sha256":"%s","size":1}}}' "$sha" > "$PROJECT_DIR/rel.json"
    curl() {
        local out="" url="${*: -1}"
        while [[ $# -gt 0 ]]; do [[ "$1" == -o ]] && out="$2"; shift; done
        echo "curl $url" >> "$EVENT_LOG"
        case "$url" in
            */releases/latest/download/release.json) cp "$PROJECT_DIR/rel.json" "$out" ;;
            */releases/latest/download/release.json.sig) echo sig > "$out" ;;
            */releases/latest/download/nasnet-tool.sh) cp "$tool" "$out" ;;
            *) return 22 ;;
        esac
    }
    fetch_release_tool "$PROJECT_DIR/got" || { fail 'release copy refused'; return 1; }
    contains 'echo new' "$PROJECT_DIR/got" || return 1
    absent 'raw.githubusercontent.com' "$EVENT_LOG" || return 1
    printf '#!/usr/bin/env bash\necho evil\n' > "$tool"
    if fetch_release_tool "$PROJECT_DIR/got"; then fail 'a copy that does not match was taken'; return 1; fi
}
test_sources_without_home() {
    # the panel's update runner is a bare systemd unit: no HOME
    ( unset HOME; set -u; source "$ROOT/nasnet-tool.sh" ) >/dev/null 2>&1 || { fail 'the script needs HOME'; return 1; }
}
RESET_HASH='$2a$10$YN/ZfhKoWOGMv98B.8KvTO.q6TzrGFve34zI9kEwTQ42lCcaobdC2'
# Default install: systemd + SQLite. The fake panel binary logs how it was run.
reset_fixture() {
    DEPLOY_MODE=systemd; DB_DRIVER=sqlite; DB_PATH="$INSTALL_DIR/data/nasnet_panel.db"
    BACKEND_BINARY="$INSTALL_DIR/bin/nasnet-panel"; mkdir -p "$INSTALL_DIR/data" "$INSTALL_DIR/bin"
    printf '%s\n' 'ADMIN_USERNAME=admin' "ADMIN_PASSWORD_HASH='\$2a\$10\$old-hash'" 'OTHER=kept' > "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    printf 'ADMIN_USERNAME=admin\nADMIN_PASSWORD="generated-at-install"\n' > "$INSTALL_DIR/admin-credentials.env"
    wizard_gen_bcrypt() { [[ "$1" == new-password-1 ]] && printf '%s' "$RESET_HASH"; }
    _sync_env_to_install_dir() { echo synced >> "$EVENT_LOG"; }
}
fake_panel_binary() {
    {
        echo '#!/usr/bin/env bash'
        echo '{ echo "args=$*"; echo "cwd=$PWD"; echo "driver=${DB_DRIVER:-} path=${DB_PATH:-} dbpass=${DB_PASSWORD:-}"; echo "stdin=$(cat)"; } >> "'"$EVENT_LOG"'"'
        [[ -z "${2:-}" ]] || printf 'echo %q >&2\n' "$2"
        echo "exit ${1:-0}"
    } > "$BACKEND_BINARY"
    chmod +x "$BACKEND_BINARY"
}
reset_password() {
    action_reset_admin_password > "$PROJECT_DIR/reset-screen" 2>&1 <<'INPUT'
new-password-1
new-password-1
INPUT
}
reset_env_hash() { (set +u; source "$ENV_FILE"; printf '%s' "$ADMIN_PASSWORD_HASH"); }
logged_before() {
    local first second
    first=$(grep -Fn -m1 -- "$1" "$3" | cut -d: -f1); second=$(grep -Fn -m1 -- "$2" "$3" | cut -d: -f1)
    [[ -n "$first" && -n "$second" && $first -lt $second ]] || fail "$1 not before $2 in $3"
}
env_value() { (set +u; source "$ENV_FILE"; printf '%s' "${!1:-}"); }
# An install whose password was later changed in the panel: that settings row
# wins over .env, so a password chosen in the wizard must reach it too.
# $1 deploy mode, $2 database; RERUN_CHOICE picks the existing-config menu entry.
wizard_rerun_fixture() {
    answers; WIZ_DEPLOY_MODE="$1"; WIZ_DB_DRIVER="$2"; WIZ_ADMIN_HASH='$2a$10$old-hash'
    RERUN_DEPLOY="$1"; RERUN_DRIVER="$2"; RERUN_CHOICE=1; RERUN_ROUTER=false
    [[ "$1" == systemd ]] || WIZ_INSTALL_METHOD=source
    wizard_write_env "$1" test > /dev/null || return 1
    BACKEND_BINARY="$INSTALL_DIR/bin/nasnet-panel"; mkdir -p "$INSTALL_DIR/data" "$INSTALL_DIR/bin"
    : > "$INSTALL_DIR/data/nasnet_panel.db"; fake_panel_binary 0
    wizard_prereqs_systemd() { :; }; wizard_prereqs_docker() { :; }; wizard_setup_postgres() { :; }
    wizard_clone_repo() { :; }; wizard_open_firewall() { :; }; _sync_env_to_install_dir() { :; }
    wizard_gen_secret() { echo fixture-jwt; }; wizard_gen_password() { echo fixture-generated-password; }
    wizard_gen_bcrypt() { [[ "$1" == new-password-1 || "$1" == fixture-generated-password ]] && printf '%s' "$RESET_HASH"; }
    wizard_build_start_systemd() { WIZ_PROVISIONED=true; echo started >> "$EVENT_LOG"; }
    wizard_build_start_docker() { WIZ_PROVISIONED=true; echo started >> "$EVENT_LOG"; }
    sudo() { case "$1" in -v) return 0 ;; mkdir|install) command "$@" ;; *) echo "sudo $*" >> "$EVENT_LOG" ;; esac; }
    docker() {
        case "$1" in
            ps) echo nasnet_panel_backend ;;
            exec) echo "docker $* stdin=$(cat)" >> "$EVENT_LOG" ;;
        esac
    }
    # "Choose new settings" asks for the password again; saved settings start at review.
    wizard_collect_install_settings() {
        [[ "${1:-}" != review ]] || return 0
        answers; WIZ_DEPLOY_MODE="$RERUN_DEPLOY"; WIZ_DB_DRIVER="$RERUN_DRIVER"; WIZ_ROUTER_MODE="$RERUN_ROUTER"
        WIZ_JWT_SECRET=''; WIZ_ADMIN_HASH=''; WIZ_ADMIN_PASS=new-password-1
        [[ "$RERUN_ROUTER" != true ]] || WIZ_ADMIN_PASS=''
    }
    arrow_menu() {
        case "$1" in
            'Existing configuration at'*) printf -v "$2" %s "$RERUN_CHOICE" ;;
            'Retry installation') printf -v "$2" 2 ;;
            *) fail "unexpected menu $1"; exit 1 ;;
        esac
    }
}
rerun_wizard() { wizard_install < /dev/null > "$PROJECT_DIR/wizard-screen" 2>&1; }
test_reset_password_takes_effect() {
    reset_fixture; fake_panel_binary 0; : > "$DB_PATH"
    reset_password || return 1
    # The panel binary writes the row login reads on every request.
    contains 'args=set-admin-password-hash' "$EVENT_LOG" && contains "cwd=$INSTALL_DIR" "$EVENT_LOG" || return 1
    contains "driver=sqlite path=$DB_PATH" "$EVENT_LOG" && contains "stdin=$RESET_HASH" "$EVENT_LOG" || return 1
    [[ "$(reset_env_hash)" == "$RESET_HASH" ]] || return 1
    [[ $(grep -c '^ADMIN_PASSWORD_HASH=' "$ENV_FILE") -eq 1 ]] && contains 'OTHER=kept' "$ENV_FILE" || return 1
    [[ ! -e "$ENV_FILE.bak" && ! -e "$INSTALL_DIR/admin-credentials.env" ]] && contains synced "$EVENT_LOG" || return 1
    local mode
    mode=$(stat -f '%Lp' "$ENV_FILE" 2>/dev/null) || mode=$(stat -c '%a' "$ENV_FILE")
    [[ "$mode" == 600 ]] || return 1
    contains 'It works now, no restart needed' "$PROJECT_DIR/reset-screen" || return 1
    absent 'Restart the backend' "$PROJECT_DIR/reset-screen" && absent 'Could not' "$PROJECT_DIR/reset-screen" || return 1
    absent new-password-1 "$PROJECT_DIR/reset-screen"
}
test_reset_password_failure_changes_nothing() {
    reset_fixture; fake_panel_binary 1 'Error: connect to database: disk I/O error'; : > "$DB_PATH"
    local before; before=$(cat "$ENV_FILE")
    reset_password || return 1
    contains 'Could not save the new password in the panel database: Error: connect to database: disk I/O error' "$PROJECT_DIR/reset-screen" || return 1
    contains 'The password was not changed' "$PROJECT_DIR/reset-screen" || return 1
    [[ "$(cat "$ENV_FILE")" == "$before" && -f "$INSTALL_DIR/admin-credentials.env" ]] && absent synced "$EVENT_LOG"
}
test_reset_password_before_first_start() {
    reset_fixture; fake_panel_binary 0
    reset_password || return 1
    # No database yet: .env is all the panel will read, and nothing creates the file.
    absent args= "$EVENT_LOG" && [[ ! -e "$DB_PATH" ]] || return 1
    [[ "$(reset_env_hash)" == "$RESET_HASH" ]] || return 1
    contains 'the panel uses it when it starts' "$PROJECT_DIR/reset-screen"
}
# Older panel binaries lack the subcommand: fall back to SQL on the real schema
# (GORM's DDL for settings), replacing a password set earlier in the panel.
test_reset_password_sql_fallback_sqlite() {
    command -v sqlite3 > /dev/null || { echo 'SKIP: sqlite3 not installed'; return 0; }
    reset_fixture; fake_panel_binary 1 'Error: unknown command "set-admin-password-hash" for "nasnet-panel"'
    sqlite3 "$DB_PATH" 'CREATE TABLE `settings` (`key` text,`value` text,`type` text,`category` text,`description` text,`label` text,`sensitive` numeric DEFAULT false,`requires_restart` numeric DEFAULT false,PRIMARY KEY (`key`));' || return 1
    sqlite3 "$DB_PATH" "INSERT INTO settings VALUES ('admin_password_hash', 'panel-set-hash', 'string', 'admin', 'd', 'l', 0, 0);" || return 1
    reset_password || return 1
    [[ "$(sqlite3 "$DB_PATH" "SELECT value FROM settings WHERE key = 'admin_password_hash';")" == "$RESET_HASH" ]] || return 1
    contains 'It works now' "$PROJECT_DIR/reset-screen" || return 1
    # No row yet (password never changed in the panel): insert one.
    sqlite3 "$DB_PATH" "DELETE FROM settings;" && : > "$EVENT_LOG" || return 1
    reset_password || return 1
    [[ "$(sqlite3 "$DB_PATH" "SELECT value || '|' || category || '|' || sensitive FROM settings;")" == "$RESET_HASH|admin|0" ]]
}
test_reset_password_postgres_systemd() {
    reset_fixture; fake_panel_binary 0; DB_DRIVER=postgres; DB_PASSWORD='db-s3cret'
    reset_password || return 1
    contains 'driver=postgres path=' "$EVENT_LOG" && contains 'dbpass=db-s3cret' "$EVENT_LOG" || return 1
    # Passed in the environment, never as an argument visible in ps.
    contains 'args=set-admin-password-hash' "$EVENT_LOG" && absent 'args=set-admin-password-hash db-s3cret' "$EVENT_LOG" || return 1
    contains 'It works now' "$PROJECT_DIR/reset-screen"
}
test_reset_password_docker() {
    reset_fixture; DEPLOY_MODE=docker; DB_DRIVER=postgres
    local running="nasnet_panel_backend" panel_binary=new
    docker() {
        case "$1" in
            ps) printf '%s\n' $running ;;
            exec)
                echo "docker $* stdin=$(cat)" >> "$EVENT_LOG"
                if [[ "$*" == *' ./main '* && "$panel_binary" == old ]]; then
                    echo 'Error: unknown command "set-admin-password-hash" for "nasnet-panel"' >&2; return 1
                fi ;;
        esac
    }
    reset_password || return 1
    contains "docker exec -i nasnet_panel_backend ./main set-admin-password-hash stdin=$RESET_HASH" "$EVENT_LOG" || return 1
    contains 'It works now' "$PROJECT_DIR/reset-screen" || return 1
    # Old image: the same upsert through psql in the database container.
    : > "$EVENT_LOG"; panel_binary=old; running="nasnet_panel_backend nasnet_panel_db"
    reset_password || return 1
    contains 'docker exec -i nasnet_panel_db psql -U postgres -d nasnet_panel -v ON_ERROR_STOP=1 -q -c INSERT INTO settings' "$EVENT_LOG" || return 1
    contains "'admin_password_hash', '$RESET_HASH'" "$EVENT_LOG" && contains 'ON CONFLICT ("key") DO UPDATE SET "value" = excluded."value";' "$EVENT_LOG" || return 1
    contains 'It works now' "$PROJECT_DIR/reset-screen" || return 1
    # Stopped container: say so and change nothing.
    : > "$EVENT_LOG"; running=''; local before; before=$(cat "$ENV_FILE")
    reset_password || return 1
    contains 'is not running; start it and retry' "$PROJECT_DIR/reset-screen" && [[ "$(cat "$ENV_FILE")" == "$before" ]]
}
# Panel login is a single admin; users.is_admin only marks Telegram bot admins,
# and the old action could not create a login (it was PostgreSQL-only, too).
test_security_menu_has_no_create_admin() {
    local items=''
    arrow_menu() { items="$*"; printf -v "$2" %s -1; }
    menu_security || return 1
    [[ "$items" == *'Reset Admin Password'* && "$items" != *'Create Admin User'* ]] || return 1
    if declare -F action_create_admin_user > /dev/null; then fail 'action_create_admin_user still defined'; return 1; fi
}
test_wizard_new_password_systemd_sqlite() {
    wizard_rerun_fixture systemd sqlite || return 1
    rerun_wizard || return 1
    # Stored by the panel binary once the service runs, then recorded as done.
    logged_before started 'args=set-admin-password-hash' "$EVENT_LOG" || return 1
    contains "driver=sqlite path=$INSTALL_DIR/data/nasnet_panel.db" "$EVENT_LOG" && contains "stdin=$RESET_HASH" "$EVENT_LOG" || return 1
    [[ "$(env_value ADMIN_PASSWORD_HASH)" == "$RESET_HASH" && "$(env_value ADMIN_PASSWORD_PENDING)" == false ]] || return 1
    absent new-password-1 "$PROJECT_DIR/wizard-screen"
}
test_wizard_new_password_systemd_postgres() {
    wizard_rerun_fixture systemd postgres || return 1
    rerun_wizard || return 1
    logged_before started 'args=set-admin-password-hash' "$EVENT_LOG" || return 1
    contains 'driver=postgres path=' "$EVENT_LOG" && contains 'dbpass=existing-password' "$EVENT_LOG" || return 1
    contains "stdin=$RESET_HASH" "$EVENT_LOG" && [[ "$(env_value ADMIN_PASSWORD_PENDING)" == false ]]
}
test_wizard_new_password_docker() {
    local driver
    for driver in postgres sqlite; do
        (
            wizard_rerun_fixture docker "$driver" || exit 1
            rerun_wizard || exit 1
            logged_before started "docker exec -i nasnet_panel_backend ./main set-admin-password-hash stdin=$RESET_HASH" "$EVENT_LOG" || exit 1
            [[ "$(env_value ADMIN_PASSWORD_PENDING)" == false ]]
        ) || { fail "docker $driver"; return 1; }
        rm -rf "$INSTALL_DIR" && mkdir -p "$INSTALL_DIR" && : > "$EVENT_LOG"
    done
}
# A router's "Choose new settings" shows a generated password; it must work.
test_wizard_generated_password_router() {
    wizard_rerun_fixture systemd sqlite || return 1
    RERUN_ROUTER=true
    rerun_wizard || return 1
    contains 'Password: fixture-generated-password' "$PROJECT_DIR/wizard-screen" || return 1
    logged_before started "stdin=$RESET_HASH" "$EVENT_LOG"
}
# A fresh install has no row yet; the panel reads .env until one is saved.
test_wizard_password_fresh_install() {
    wizard_rerun_fixture systemd sqlite || return 1
    rm -f "$ENV_FILE" "$INSTALL_DIR/data/nasnet_panel.db"
    rerun_wizard || return 1
    contains started "$EVENT_LOG" && absent 'args=' "$EVENT_LOG" || return 1
    [[ "$(env_value ADMIN_PASSWORD_HASH)" == "$RESET_HASH" && "$(env_value ADMIN_PASSWORD_PENDING)" == false ]] || return 1
    # Once the panel has created its database, the same hash goes there too.
    rm -f "$ENV_FILE"; : > "$EVENT_LOG"
    wizard_build_start_systemd() { WIZ_PROVISIONED=true; : > "$INSTALL_DIR/data/nasnet_panel.db"; echo started >> "$EVENT_LOG"; }
    rerun_wizard || return 1
    logged_before started "stdin=$RESET_HASH" "$EVENT_LOG"
}
# Keeping the saved password must not undo a change made in the panel since.
test_wizard_saved_settings_keep_panel_password() {
    wizard_rerun_fixture systemd sqlite || return 1
    RERUN_CHOICE=0
    rerun_wizard || return 1
    contains started "$EVENT_LOG" && absent 'args=' "$EVENT_LOG" || return 1
    [[ "$(env_value ADMIN_PASSWORD_HASH)" == '$2a$10$old-hash' && "$(env_value ADMIN_PASSWORD_PENDING)" == false ]]
}
# A failed save is retried in the same run, or on the next run with saved settings.
test_wizard_password_store_retry() {
    wizard_rerun_fixture systemd sqlite || return 1
    fake_panel_binary 1 'Error: connect to database: disk I/O error'
    if rerun_wizard; then fail 'unsaved password reported complete'; return 1; fi
    contains 'not saved in the panel database: Error: connect to database: disk I/O error' "$PROJECT_DIR/wizard-screen" || return 1
    [[ "$(env_value ADMIN_PASSWORD_HASH)" == "$RESET_HASH" && "$(env_value ADMIN_PASSWORD_PENDING)" == true ]] || return 1
    : > "$EVENT_LOG"; fake_panel_binary 0; RERUN_CHOICE=0
    rerun_wizard || return 1
    logged_before started "stdin=$RESET_HASH" "$EVENT_LOG" && [[ "$(env_value ADMIN_PASSWORD_PENDING)" == false ]] || return 1
    # "Retry access checks only" also saves it once the panel answers.
    : > "$EVENT_LOG"; fake_panel_binary 1 'Error: database is locked'; RERUN_CHOICE=1
    arrow_menu() {
        case "$1" in
            'Existing configuration at'*) printf -v "$2" %s "$RERUN_CHOICE" ;;
            'Retry installation') : > "$EVENT_LOG"; fake_panel_binary 0; printf -v "$2" 1 ;;
        esac
    }
    wizard_verify_install() { echo verified >> "$EVENT_LOG"; }
    rerun_wizard || return 1
    logged_before verified "stdin=$RESET_HASH" "$EVENT_LOG" && [[ "$(env_value ADMIN_PASSWORD_PENDING)" == false ]]
}
for case_name in first_run_default_yes router_address_detection router_review_back router_defaults_and_credentials password_masking password_validation_visible navigation_access_preserves_urls navigation_install_after_review navigation_role_back navigation_docker_back navigation_offline_back navigation_review_back navigation_text_commands navigation_access_back navigation_access_tls navigation_access_cancel navigation_saved_back navigation_menu_keys root_sudo tls_probe panel_marker readiness_failure path_validation config_roundtrip config_failure cancel_before_changes template_rejected prereq_stops_install write_stops_install start_failure deploy_failure no_access_bypass xray_dirs offline_xray pg_preserves_role existing_pg_dependency unit_runs_as_root router_unit_caps old_unit_switched_to_root root_unit_left_alone docker_unit_ignored update_system_writes_units auto_update_delegates menu_reads_the_panel_check release_manifest_fields unverifiable_signature_is_said release_signature_checked cli_fetch_takes_the_release_copy sources_without_home reset_password_takes_effect reset_password_failure_changes_nothing reset_password_before_first_start reset_password_sql_fallback_sqlite reset_password_postgres_systemd reset_password_docker security_menu_has_no_create_admin wizard_new_password_systemd_sqlite wizard_new_password_systemd_postgres wizard_new_password_docker wizard_generated_password_router wizard_password_fresh_install wizard_saved_settings_keep_panel_password wizard_password_store_retry; do
    if (setup; "test_$case_name"); then echo "PASS $case_name"; else echo "FAIL $case_name" >&2; exit 1; fi
done

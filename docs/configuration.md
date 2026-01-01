# Configuration

nasnet-panel is configured entirely through **environment variables**, loaded from a `.env` file in the working directory (or from the real environment if no file is present). Start from [`.env.example`](../.env.example), which is also what the guided installer generates.

> **Panel-editable settings.** Some values are mirrored into the database and can be changed from the web panel's **Settings** page. When a database setting exists, it **overrides** the `.env` value. Settings such as ACME, the Telegram proxy, metrics auth, TLS cert paths, and the panel base path are read from the database at startup, so a change there takes effect **after a restart**.

## Required

| Variable | Default | Description |
|----------|---------|-------------|
| `ADMIN_USERNAME` | `admin` | Web panel login username. |
| `ADMIN_PASSWORD_HASH` | — | **bcrypt** hash of the panel password. Generate with `htpasswd -nbBC 10 "" "pw" \| tr -d ':\n' \| sed 's/$2y/$2a/'`. |
| `JWT_SECRET_KEY` | — | Secret for signing JWTs. **Must be ≥ 32 characters.** `openssl rand -hex 32`. |
| `APP_BASE_URL` | — | Public base URL used in subscription links and cookies (e.g. `https://panel.example.com` or `http://1.2.3.4:9761`). |

## Application

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_ENV` | `production` | `production` or `development`. |
| `APP_PORT` | `9761` | HTTP listen port. |
| `APP_PANEL_BASE_PATH` | _(empty)_ | Optional path prefix for the admin panel (e.g. `/x7k2m9`) to keep it off the web root. |
| `SUB_PANEL_URL` | _(empty)_ | If set, browser hits to `/sub/:key` are redirected here (a user-facing subscription page). |
| `CORS_ORIGINS` | _(empty)_ | Comma-separated list of allowed CORS origins. |
| `BACKUP_DIR` | `./data/backups` | Directory for database backups. |
| `DEPLOY_MODE` | `docker` | `docker` or `systemd`. Set by the installer; affects how reconfigure/update behave. |

## Database

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DRIVER` | `postgres` | `postgres` or `sqlite`. |
| `DB_HOST` | `localhost` | PostgreSQL host (Compose sets this to `postgres`). |
| `DB_PORT` | `5432` | PostgreSQL port. |
| `DB_USER` | `postgres` | PostgreSQL user. |
| `DB_PASSWORD` | `postgres` | PostgreSQL password. **Change this.** |
| `DB_NAME` | `nasnet_panel` | Database name. |
| `DB_SSL_MODE` | `disable` | PostgreSQL SSL mode. |
| `DB_PATH` | `./data/nasnet_panel.db` | SQLite file path (used when `DB_DRIVER=sqlite`). |
| `DB_MAX_OPEN_CONNS` | `25` | Max open DB connections. |
| `DB_MAX_IDLE_CONNS` | `10` | Max idle DB connections. |
| `DB_CONN_MAX_LIFETIME_MINUTES` | `5` | Max connection lifetime. |
| `DB_CONN_MAX_IDLE_MINUTES` | `3` | Max idle time before a connection is closed. |

## TLS & ACME

See [TLS, ACME & Domains](./tls-acme-and-domains.md) for the full story.

| Variable | Default | Description |
|----------|---------|-------------|
| `ACME_ENABLED` | `false` | Enable automatic Let's Encrypt certificate issuance. |
| `ACME_EMAIL` | _(empty)_ | Contact email for Let's Encrypt (required when ACME is enabled). |
| `ACME_STAGING` | `false` | Use the Let's Encrypt staging CA (for testing). |
| `ACME_CACHE_DIR` | `/app/data/acme` | Where issued certificates are cached. |
| `ACME_AUTO_RENEW` | `true` | Automatically renew before expiry. |
| `TLS_CERT_FILE` | _(empty)_ | Path to a manual TLS certificate (bypasses ACME). |
| `TLS_KEY_FILE` | _(empty)_ | Path to the manual TLS private key. |
| `JWT_COOKIE_DOMAIN` | _(empty)_ | Cookie domain (e.g. `example.com`); empty for IP mode. |
| `JWT_COOKIE_SECURE` | `false` | Set `true` when serving over HTTPS. |

## JWT

| Variable | Default | Description |
|----------|---------|-------------|
| `JWT_ACCESS_EXPIRY` | `60` | Access-token lifetime, in **minutes**. |
| `JWT_REFRESH_EXPIRY` | `168` | Refresh-token lifetime, in **hours** (168 = 7 days). |

## Telegram

See [Telegram Bot](./telegram.md).

| Variable | Default | Description |
|----------|---------|-------------|
| `TELEGRAM_ENABLED` | `false` | Enable the Telegram bot. |
| `TELEGRAM_BOT_TOKEN` | — | Bot token from [@BotFather](https://t.me/BotFather) (required when enabled). |
| `ADMIN_IDS` | — | Comma-separated Telegram user IDs granted bot admin rights. |
| `BOT_MODE` | `polling` | `polling` or `webhook`. |
| `WEBHOOK_URL` | _(empty)_ | Public webhook URL (used when `BOT_MODE=webhook`). |
| `TELEGRAM_PROXY_ENABLED` | `false` | Route bot traffic through a SOCKS5 proxy. |
| `TELEGRAM_PROXY_TYPE` | `socks5` | Proxy type. |
| `TELEGRAM_PROXY_HOST` | _(empty)_ | Proxy host. |
| `TELEGRAM_PROXY_PORT` | `1080` | Proxy port. |
| `TELEGRAM_PROXY_USERNAME` | _(empty)_ | Proxy username. |
| `TELEGRAM_PROXY_PASSWORD` | _(empty)_ | Proxy password. |

## Metrics

See [Monitoring & Alerting](./monitoring-and-alerting.md).

| Variable | Default | Description |
|----------|---------|-------------|
| `METRICS_ENABLED` | `true` | Expose Prometheus metrics. |
| `METRICS_PATH` | `/metrics` | Metrics endpoint path. |
| `METRICS_USERNAME` | _(empty)_ | Basic-auth username (empty = no auth). |
| `METRICS_PASSWORD` | _(empty)_ | Basic-auth password. |

The bundled Prometheus container reads a few extra Compose-only variables: `PROMETHEUS_PORT` (default `9090`), `PROMETHEUS_RETENTION` (`15d`), `PROMETHEUS_SCRAPE_INTERVAL` (`5s`), and `PROMETHEUS_TARGET`.

## Xray

| Variable | Default | Description |
|----------|---------|-------------|
| `XRAY_API_TIMEOUT` | `5` | Timeout (seconds) for Xray gRPC API calls. |
| `XRAY_INBOUND_TAG` | `vmess-in` | Default inbound tag for legacy single-inbound setups. |

## Logging

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. Also panel-editable. |
| `LOG_FORMAT` | `text` | `text` or `json`. |

---

## Minimal example

```dotenv
APP_ENV=production
APP_PORT=9761
APP_BASE_URL=https://panel.example.com

ADMIN_USERNAME=admin
ADMIN_PASSWORD_HASH=$2a$10$....your-bcrypt-hash....
JWT_SECRET_KEY=replace-with-openssl-rand-hex-32

DB_DRIVER=postgres
DB_PASSWORD=replace-with-strong-password

ACME_ENABLED=true
ACME_EMAIL=you@example.com
JWT_COOKIE_DOMAIN=example.com
JWT_COOKIE_SECURE=true

TELEGRAM_ENABLED=false
```

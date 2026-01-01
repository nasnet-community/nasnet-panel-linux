# Admin Panel

The admin panel is a React single-page app **embedded in the `nasnet-panel` binary** and served at your `APP_BASE_URL`. There is nothing extra to deploy — when `nasnet-panel` is running, the panel is available.

> [!NOTE]
> **Screenshot placeholder.** Add images under [`./assets/`](./assets/) (e.g. `panel-dashboard.png`, `panel-server.png`) and reference them here.
>
> <!-- ![Dashboard](./assets/panel-dashboard.png) -->

## Signing in

Browse to `APP_BASE_URL` and log in with `ADMIN_USERNAME` and the password whose bcrypt hash you set in `ADMIN_PASSWORD_HASH`. Sessions use JWT cookies; set `JWT_COOKIE_SECURE=true` and `JWT_COOKIE_DOMAIN` when serving over HTTPS on a domain.

To keep the panel off the web root, set `APP_PANEL_BASE_PATH` (e.g. `/x7k2m9`) so it is served under that prefix.

## What's inside

| Area | What you do there |
|------|-------------------|
| **Dashboard** | At-a-glance stats — online users, traffic, and server health — on a customizable widget layout. |
| **Server** | Control the server's Xray — inbounds, outbounds, and routing; view logs and live traffic/system stats; open an in-browser terminal. |
| **Users** | Manage accounts, admin flags; link Telegram identities. |
| **Subscriptions** | Create, edit, suspend, renew; inspect usage, devices, and IPs. |
| **Alerts** | Define and tune alert rules; review fired events. |
| **Audit log** | A searchable record of admin actions. |
| **Support chat** | Talk to users in-app. |
| **Settings** | Panel-editable configuration (ACME, proxy, metrics auth, base path, log level, …). |
| **Backups** | Create and restore database backups. |

## Notable conveniences

- **Live server terminal** — an in-browser terminal (xterm) to the server for maintenance, without a separate SSH client.
- **Config editor** — a Monaco-based editor for Xray configuration with syntax highlighting.
- **Charts** — traffic and usage trends over time.
- **Customizable dashboard** — drag-and-drop widget layout.

## Security tips

- Always run the panel over **HTTPS** in production (ACME or a reverse proxy) and set the secure-cookie variables.
- Use a strong admin password and a long random `JWT_SECRET_KEY`.
- Consider `APP_PANEL_BASE_PATH` to reduce drive-by discovery.
- Protect the metrics endpoint with `METRICS_USERNAME` / `METRICS_PASSWORD`.

See [TLS, ACME & Domains](./tls-acme-and-domains.md) for serving the panel securely, and [Monitoring & Alerting](./monitoring-and-alerting.md) for the metrics and alerts surfaces.

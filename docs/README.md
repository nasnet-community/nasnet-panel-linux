# NasNet Panel (nasnet-panel) — Documentation

Welcome to the documentation for **NasNet Panel**, the self-hosted control panel (binary name `nasnet-panel`) for running and managing Xray VPN subscriptions on a single server.

If you are new here, read the pages in this order:

1. **[Architecture](./architecture.md)** — understand the single-binary design before you deploy.
2. **[Installation](./installation.md)** — get the panel running with Docker, systemd, or a prebuilt binary.
3. **[Configuration](./configuration.md)** — set the environment variables that matter.
4. **[Server & Xray](./nodes-and-agents.md)** — how the panel supervises the local Xray-core process.
5. **[Subscriptions & Clients](./subscriptions-and-clients.md)** — start handing out access.

## All guides

### Get running
- [Installation](./installation.md) — Docker, systemd, the `nasnet-tool` wizard, release binaries, offline bundle, build from source
- [Configuration](./configuration.md) — every `.env` variable, its default, and what it does
- [TLS, ACME & Domains](./tls-acme-and-domains.md) — automatic and manual certificates, running behind a reverse proxy

### Operate the server
- [Architecture](./architecture.md) — single-binary design, layering, event bus, database
- [Server & Xray](./nodes-and-agents.md) — supervising the local Xray-core process, version updates, server operations
- [Protocols & Routing](./protocols-and-routing.md) — inbounds, outbounds, REALITY, WireGuard, routing & balancing rules

### Serve subscribers
- [Subscriptions & Clients](./subscriptions-and-clients.md) — subscription links, formats, supported client apps
- [Telegram Bot](./telegram.md) — bot setup, webhook vs polling
- [Admin Panel](./admin-panel.md) — a tour of the web panel

### Keep it healthy
- [Monitoring & Alerting](./monitoring-and-alerting.md) — Prometheus metrics, alert rules, notifications
- [Backup & Restore](./backup-and-restore.md) — database backups and restore
- [Troubleshooting & FAQ](./troubleshooting-and-faq.md) — common problems and answers

### Build & contribute
- [Development](./development.md) — project layout, building, frontends, protobuf, tests

## Components at a glance

| Component | Binary | Role |
|-----------|--------|------|
| Panel | `nasnet-panel` | The whole app: HTTP API, embedded web + subscriber panel, Telegram bot, scheduler, database, and supervision of the local Xray-core process |
| Tool | `nasnet-tool.sh` | Interactive installer & operations TUI |

> **Assets.** Screenshots referenced in these docs live under [`./assets/`](./assets/). They are placeholders — add your own images with the same filenames and they will render automatically.

# Monitoring & Alerting

NasNet Panel ships with first-class observability: **Prometheus metrics**, a rule-based **alert engine**, multi-channel **notifications**, an **audit log**, and in-app **support chat**.

## Prometheus metrics

The panel exposes Prometheus metrics at `METRICS_PATH` (default `/metrics`), all under the `nasnet_` namespace.

```dotenv
METRICS_ENABLED=true
METRICS_PATH=/metrics
METRICS_USERNAME=prometheus     # optional basic auth (recommended)
METRICS_PASSWORD=change-me
```

> **Full catalog:** the complete metric reference — every metric, its labels, and how it's collected — lives in [`pkg/metrics/METRICS.md`](../pkg/metrics/METRICS.md).

What's measured, at a glance:

| Group | Examples |
|-------|----------|
| **HTTP** | request totals, latency histogram, in-flight requests |
| **Business** | users, subscriptions |
| **Server (Xray & host)** | host CPU / memory / disk, active connections, traffic, online users, Xray core uptime |
| **Provisioning** | queue depth, tasks processed (success/failure) |
| **Certificates** | seconds until expiry, per domain |
| **Database** | connection-pool usage |
| **Scheduler** | per-task duration and error counts |
| **Event bus** | events published, active subscribers |

### Runtime toggle

Besides the startup `METRICS_ENABLED` env var, the panel has a **Metrics Enabled** toggle (Settings → Server) that enables/disables collection **without a restart** — the collector reads the `metrics_enabled` database setting on every scheduler tick.

### Bundled Prometheus

The Docker Compose stack includes a Prometheus container preconfigured to scrape the app. It honors a few Compose-only variables: `PROMETHEUS_PORT` (default `9090`), `PROMETHEUS_RETENTION` (`15d`), `PROMETHEUS_SCRAPE_INTERVAL` (`5s`), and `PROMETHEUS_TARGET`. If you set `METRICS_USERNAME`/`METRICS_PASSWORD`, the bundled Prometheus is wired to use them. Point Grafana at this Prometheus to build dashboards.

## Alerting

The built-in **alert engine** evaluates rules against the event stream and the periodic stats, and fires when conditions are met.

- **Default rules** are seeded on first start (idempotent), giving you a useful starter set to enable (e.g. Xray crash loop, high CPU, high disk).
- Rules have **state** (so the engine knows when a condition starts and clears) and produce **events** you can review in the panel.
- When a rule fires it publishes a `system.alert` event, which flows to the notification dispatcher.

Manage and tune rules from the panel's **Alerts** area.

## Notifications

The notification dispatcher routes events (and alerts) to whichever channels you enable in Settings:

| Channel | Notes |
|---------|-------|
| **Telegram** | Sends to admins via the bot (requires the bot enabled). |
| **Discord** | Posts to a Discord webhook. |
| **Webhook** | POSTs a JSON payload to any URL you provide. |

Common notifications: Xray down/recovered, subscription created/expiring/expired, and system alerts.

## Audit log

Every administrative action (settings changes, subscription edits, server & Xray operations, backup & restore, …) is written to an **audit log** you can browse and search in the panel. This is your record of *who did what, when*.

## Support chat

The panel includes an in-app **support chat** so users and admins can message each other over WebSockets, with message reactions. Old messages are cleaned up on a schedule. It's a lightweight alternative to running a separate helpdesk.

## Health endpoint

The panel serves a readiness probe at **`/health/ready`** (used by the Docker health check). Point your uptime monitor or load balancer at it.

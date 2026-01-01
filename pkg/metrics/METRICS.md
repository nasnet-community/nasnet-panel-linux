# Prometheus Metrics Reference

NasNet Panel exposes Prometheus-compatible metrics at a configurable HTTP endpoint. This document covers configuration, the complete metric catalog, collection architecture, and runtime controls.

## Table of Contents

- [Configuration](#configuration)
- [Runtime Toggle](#runtime-toggle)
- [Metric Catalog](#metric-catalog)
  - [HTTP](#http)
  - [Business](#business)
  - [Nodes](#nodes)
  - [Provisioning](#provisioning)
  - [Certificates](#certificates)
  - [Database](#database)
  - [Scheduler](#scheduler)
  - [Event Bus](#event-bus)
- [Collection Architecture](#collection-architecture)
- [Files](#files)

---

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `METRICS_ENABLED` | `true` | Initialize the metrics subsystem at startup. When `false`, no registry is created, no `/metrics` endpoint is registered, and zero runtime overhead is incurred. |
| `METRICS_PATH` | `/metrics` | HTTP path where the Prometheus scrape endpoint is served. |

These are startup-time settings. Changing them requires a server restart.

## Runtime Toggle

The admin panel exposes a **Metrics Enabled** toggle under **Settings > Server**. This corresponds to the `metrics_enabled` database setting and allows enabling/disabling collection **without restarting the server**.

The metrics collector reads this setting from the database on every scheduler tick (~5 seconds) and updates an in-memory `atomic.Bool` flag. All instrumentation points check this flag before recording.

| State | Behavior |
|---|---|
| Env `METRICS_ENABLED=false` | Metrics never initialized. No endpoint, no overhead. |
| Env `METRICS_ENABLED=true` + DB `metrics_enabled=true` | Fully active. All metrics collected and exposed. |
| Env `METRICS_ENABLED=true` + DB `metrics_enabled=false` | Endpoint exists but returns stale/empty data. No collection overhead. |

---

## Metric Catalog

All metrics use the namespace prefix **`nasnet`**.

### HTTP

Instrumented by `GinMiddleware()` on every HTTP request.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_http_requests_total` | Counter | `method`, `path`, `status` | Total HTTP requests processed. |
| `nasnet_http_request_duration_seconds` | Histogram | `method`, `path`, `status` | Request latency in seconds. Uses default Prometheus buckets. |
| `nasnet_http_requests_in_flight` | Gauge | — | Number of HTTP requests currently being handled. |

### Business

Collected every ~5 seconds by the periodic collector via database queries.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_users_total` | Gauge | `category` | User counts. |
| `nasnet_subscriptions_total` | Gauge | `status` | Subscription counts. |

**Label values:**

| Metric | Label | Values |
|---|---|---|
| `users_total` | `category` | `total`, `active`, `banned`, `admin` |
| `subscriptions_total` | `status` | `total`, `active`, `expired` |

### Nodes

Node-level resource metrics. **Online/offline counts** are collected periodically by the collector. **Per-node resource gauges** are updated in real time via the EventBus when `node.stats_updated` events arrive.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_nodes_total` | Gauge | `status` | Node counts (`online`, `offline`). |
| `nasnet_node_cpu_percent` | Gauge | `node_id`, `node_name` | CPU usage percentage (0–100). |
| `nasnet_node_memory_percent` | Gauge | `node_id`, `node_name` | Memory usage percentage (0–100). |
| `nasnet_node_disk_percent` | Gauge | `node_id`, `node_name` | Disk usage percentage (0–100). |
| `nasnet_node_tcp_connections` | Gauge | `node_id`, `node_name` | Active TCP connections. |
| `nasnet_node_udp_connections` | Gauge | `node_id`, `node_name` | Active UDP connections. |
| `nasnet_node_traffic_bytes` | Gauge | `node_id`, `node_name`, `direction` | Cumulative traffic in bytes (`up`, `down`). |
| `nasnet_node_online_users` | Gauge | `node_id`, `node_name` | Connected user count. |
| `nasnet_node_xray_uptime_seconds` | Gauge | `node_id`, `node_name` | Xray process uptime in seconds. |

### Provisioning

Queue depth is collected periodically. Task completion counters are incremented inline by the provisioning worker.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_provisioning_queue_depth` | Gauge | `status` | Queue size (`pending`, `processing`, `failed`). |
| `nasnet_provisioning_tasks_processed_total` | Counter | `type`, `result` | Completed provisioning tasks. |

**Label values:**

| Label | Values |
|---|---|
| `type` | `add_user`, `remove_user` |
| `result` | `success`, `failure` |

### Certificates

Collected every ~5 seconds from the `snis` table.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_certificate_expiry_seconds` | Gauge | `domain` | Seconds until certificate expiry. Negative values mean the certificate has expired. |

### Database

Collected every ~5 seconds from Go's `sql.DB` connection pool stats.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_db_open_connections` | Gauge | `state` | Connection pool usage (`in_use`, `idle`). |
| `nasnet_db_max_connections` | Gauge | — | Connection pool maximum. |

### Scheduler

The `observeTask()` wrapper records duration and errors for every scheduled task.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_scheduler_task_duration_seconds` | Histogram | `task` | Task execution time. Uses default Prometheus buckets. |
| `nasnet_scheduler_task_errors_total` | Counter | `task` | Total task failures. |

**Task names:**

| `task` value | What it does |
|---|---|
| `reconcile_nodes` | Sync inbound configurations with nodes. |
| `sync_usage` | Sync data usage from Xray to DB. |
| `expire_subscriptions` | Check and expire subscriptions by date/data limit. |
| `reconcile_users` | Enforce plan compliance on nodes. |
| `cert_renewals` | Check and auto-renew ACME certificates. |
| `sync_node_stats` | Collect node resource stats via gRPC. |

### Event Bus

Counters and gauges for the internal SSE event system. Wired via callback hooks on the EventBus to avoid circular imports.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `nasnet_events_published_total` | Counter | `type` | Total events published. |
| `nasnet_eventbus_subscribers` | Gauge | — | Active SSE subscriber count. |

**Event types:**

| `type` value | Trigger |
|---|---|
| `node.online` | Node came online. |
| `node.offline` | Node went offline. |
| `node.stats_updated` | Node stats refreshed. |
| `subscription.created` | New subscription created. |
| `subscription.expiring` | Subscription nearing expiry. |
| `subscription.expired` | Subscription expired. |
| `system.alert` | System-level alert. |

---

## Collection Architecture

Metrics are populated through four mechanisms:

### 1. Periodic Collector (every ~5s)

The `Collector` struct runs on every scheduler tick. It queries the database for business stats, node statuses, provisioning queue depths, certificate expiries, and connection pool stats, then updates the corresponding Prometheus gauges.

It also reads the `metrics_enabled` DB setting on each tick to sync the runtime enable/disable flag.

### 2. Real-Time Event Listener

`StartEventListener()` subscribes to the EventBus and listens for `node.stats_updated` events. When an event arrives, per-node resource gauges (CPU, memory, disk, connections, traffic, uptime) are updated immediately. When metrics are disabled, events are still consumed (to prevent channel backup) but gauge updates are skipped.

### 3. HTTP Middleware

`GinMiddleware()` wraps every HTTP request. It tracks in-flight requests, total request counts, and request duration. When metrics are disabled, the middleware calls `c.Next()` and returns immediately with zero overhead.

### 4. Inline Instrumentation

- **Scheduler**: `observeTask()` records task duration and errors for each scheduled task.
- **Provisioning Worker**: Increments `provisioning_tasks_processed_total` on task success/failure.
- **EventBus Hooks**: `OnPublish` and `OnSubscriberChange` callbacks increment event counters and update the subscriber gauge.

---

## Files

```
pkg/metrics/
  metrics.go          Metric definitions, registry init, Enabled flag
  middleware.go        Gin HTTP middleware
  collector.go        Periodic collector (business stats, DB pool, certs)
  event_listener.go   Real-time node stats via EventBus
  stats_provider.go   Database queries for the collector

pkg/scheduler/
  scheduler.go        observeTask() wrapper for scheduler metrics

pkg/events/
  bus.go              EventBus with OnPublish/OnSubscriberChange hooks
  types.go            Event type constants and payload structs

internal/provisioning/worker/
  worker.go           Inline provisioning task counters

cmd/root.go           Initialization wiring and EventBus hook setup
config/config.go      MetricsConfig struct (Enabled, Path)
transport/http/
  server.go           /metrics endpoint registration and middleware setup

internal/setting/usecase/
  setting_usecase.go  metrics_enabled DB setting seed
```

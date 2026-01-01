# Server & Xray

NasNet Panel runs on a **single server** and manages a local **Xray-core** process as a supervised child process. There is no separate agent to install and no remote nodes — the panel and the proxy core live on the same host. This page covers how the panel supervises Xray, version management, and the per-server operations available from the panel.

## The Xray process

- The panel starts Xray-core as a **child process** and keeps it alive with a **watchdog** (restart-on-crash with backoff).
- Configuration you build in the panel — inbounds, outbounds, routing and balancing rules — is compiled into an Xray config and applied to the running core in-process. See [Protocols & Routing](./protocols-and-routing.md).
- The panel reads stats and manages proxy users through Xray's local gRPC API (default `127.0.0.1:10085`).

## Xray version management

From the **Xray Binaries** page you can download, switch, and update the Xray-core binary. The panel caches binaries and can fetch a requested version on demand, so you can pin or upgrade the core without touching the host by hand.

## Server operations

From the **Server** page (or the Telegram bot) you can:

- Push / sync inbounds, outbounds, routing and balancing rules.
- Start / stop / restart Xray and view live logs.
- View live traffic, access logs, and system stats (CPU, memory, network, uptime) plus history.
- Open a live **web terminal** and manage the host over **SSH**.
- Manage **geofiles** (geoip / geosite).
- Run a gated **nuke / wipe** for teardown.

## Health

The panel's monitor tracks whether Xray is running and the host is healthy, and raises events on crash and recovery. Those events feed [alerting and notifications](./monitoring-and-alerting.md).

# Protocols & Routing

The panel runs a local **Xray-core** process whose configuration it generates from your **inbounds**, **outbounds**, and **routing/balancing rules**. You edit these in the panel, which builds the core config and applies it to the running Xray-core process.

## Inbounds (what users connect to)

Supported inbound protocols:

| Protocol | Notes |
|----------|-------|
| **VLESS** | Recommended; pairs with REALITY / XTLS Vision for modern setups. |
| **VMess** | Classic Xray protocol. |
| **Trojan** | TLS-based. |
| **Shadowsocks** | AEAD ciphers. |

Inbounds support the usual Xray **transports** (TCP, WebSocket, gRPC, HTTP/2, …) and **security** layers:

- **TLS** — standard TLS with your certificate or an ACME-issued one.
- **REALITY** — TLS camouflage that borrows a real target site's handshake (no certificate of your own required).
- **XTLS / Vision** flow control for VLESS.

Each inbound has a **tag** that routing rules and subscriptions reference.

## Outbounds (where traffic goes)

Outbounds define egress from the server:

- **Freedom** — direct egress (the common default).
- **Blackhole** — drop traffic (used to block matched routes).
- **Proxy chaining** — forward through an upstream proxy (e.g. SOCKS or another proxy server) for multi-hop egress.

## Routing rules

Routing rules decide which outbound a connection takes. A rule can match on:

- **GeoIP** and **GeoSite** data (embedded geo data files),
- destination **domains** and **IP ranges**,
- source **inbound tags**,
- specific **user emails**.

Rules are evaluated in order; the first match wins. Typical uses: send a region's traffic direct, route everything else through an outbound, or blackhole ad/abuse domains.

> The geo data files (`geoip.dat` / `geosite.dat`) are embedded into the build via `make geofiles`. See the [Development guide](./development.md).

## Balancing rules

Balancing rules spread traffic across **multiple outbounds** for redundancy and load distribution — useful when the server has several upstreams. A balancing rule references a set of outbound tags and a selection strategy.

## DNS settings

DNS configuration lets you control how the core resolves names (custom resolvers, DNS-over-HTTPS/TLS, per-domain overrides), which matters for both performance and correct geo-routing.

## Reverse proxy

You can define **reverse proxy** entries to expose internal services through the core's routing — handy for forwarding traffic to an internal target as part of your routing setup.

## WireGuard

In addition to the Xray protocols, the panel manages **WireGuard** peers. Peers are allocated addresses (IPAM), rendered into the generated configuration, and tied to subscription lifecycle — suspended when a subscription is paused or expires and resumed when it's reactivated. WireGuard keys are generated and stored by the panel.

## SNI management

The **SNI** feature stores the server names used by TLS/REALITY inbounds and link generation. Centralizing SNI values lets you reuse and update them across your inbounds and hosts.

## Hosts & host templates

A **host** is the externally-reachable address/port/SNI combination that subscription links are built from — it lets the link a user sees differ from the inbound's real binding (e.g. point clients at a CDN or a different domain). **Host templates** let you apply a consistent host configuration across many inbounds.

## Putting it together

1. Create one or more **inbounds** (protocol + transport + security + tag).
2. Optionally add **outbounds** and **routing/balancing rules** to control egress.
3. Define **hosts** so subscription links resolve to the right address/SNI.
4. Make inbounds available to subscribers through their **subscription** (see [Subscriptions & Clients](./subscriptions-and-clients.md)).
5. The panel generates the Xray-core config and applies it locally; subscribers receive matching links.

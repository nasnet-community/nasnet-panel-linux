# Installation

This guide covers every supported way to install **nasnet-panel**. Pick one:

- [Docker Compose](#docker-compose) — recommended; runs the app, database, and Prometheus together.
- [Guided installer (`nasnet-tool.sh`)](#guided-installer) — interactive wizard for Docker or systemd.
- [Prebuilt release binaries](#prebuilt-release-binaries) — drop-in binaries for `systemd`/manual setups.
- [Offline bundle](#offline-bundle) — release archive with PostgreSQL and Xray-core included.
- [Build from source](#build-from-source) — for contributors.

For how the panel supervises its local Xray-core process once it's up, see [Server & Xray](./nodes-and-agents.md).

---

## Prerequisites

- A 64-bit Linux server (`amd64` or `arm64`).
- A domain name (recommended) so the panel can get an automatic TLS certificate. You can run on a bare IP over HTTP, but HTTPS is strongly preferred — secure cookies require it.
- One open inbound port for the panel (default **`9761`**), plus whatever ports your proxy inbounds will use.
- For Docker installs: **Docker Engine** and the **Docker Compose** plugin.

You will also need two generated secrets and an admin password hash (used by every install method):

```bash
# JWT secret (must be >= 32 characters)
openssl rand -hex 32

# Admin panel password hash (bcrypt)
htpasswd -nbBC 10 "" "your-password" | tr -d ':\n' | sed 's/$2y/$2a/'
```

---

## Docker Compose

The repository ships a `docker-compose.yml` that runs three services: the app (`nasnet_panel_backend`), **PostgreSQL 16** (`nasnet_panel_db`), and **Prometheus** (`nasnet_panel_prometheus`).

```bash
git clone https://github.com/nasnet-community/nasnet-panel-linux.git
cd nasnet-panel-linux

cp .env.example .env
# Edit .env — at minimum set:
#   ADMIN_USERNAME, ADMIN_PASSWORD_HASH, JWT_SECRET_KEY, APP_BASE_URL, DB_PASSWORD

docker compose up -d
docker compose logs -f app    # watch startup
```

When the `app` container reports healthy, open `APP_BASE_URL` (e.g. `http://your-ip:9761`) and log in with your admin credentials.

The app container exposes a readiness probe at **`/health/ready`**, which Compose uses for its health check.

### SQLite instead of PostgreSQL

For small single-server setups you can use SQLite and skip the database container. Set in `.env`:

```dotenv
DB_DRIVER=sqlite
DB_PATH=/app/data/nasnet_panel.db
```

Then start with the override file (it removes the Postgres dependency and mounts a SQLite volume):

```bash
docker compose -f docker-compose.yml -f docker-compose.sqlite.yml up -d
```

### Persisted data

Compose creates named volumes for `postgres_data`, `acme_data`, and `prometheus_data`, and bind-mounts `./data/backups` for database backups. Back these up before upgrades — see [Backup & Restore](./backup-and-restore.md).

---

## Guided installer

`nasnet-tool.sh` is an interactive terminal UI that wraps the whole lifecycle: it checks prerequisites, generates `.env` (secrets, admin hash, URLs, ports), and brings the service up in either **Docker** or **systemd** mode.

```bash
git clone https://github.com/nasnet-community/nasnet-panel-linux.git
cd nasnet-panel-linux
./nasnet-tool.sh
```

From the menu you can **install**, **reconfigure**, **update**, **back up / restore**, and **uninstall**. The chosen deployment mode is recorded as `DEPLOY_MODE` in `.env` so later actions behave correctly.

> The tool can also auto-update from GitHub Releases (`nasnet-community/nasnet-panel-linux`). Set a `GITHUB_TOKEN` in the environment if you hit API rate limits.

---

## Prebuilt release binaries

Each tagged release publishes static Linux binaries (no CGO) for `amd64` and `arm64`:

| Binary | Purpose |
|--------|---------|
| `nasnet-panel-linux-<arch>` | the panel (web panel embedded) |
| `nasnet-tool-linux-<arch>` | the operations tool |

Download from the [Releases page](https://github.com/nasnet-community/nasnet-panel-linux/releases), then run the panel with a `.env` file in the working directory:

```bash
chmod +x nasnet-panel-linux-amd64
cp .env.example .env       # then edit it
./nasnet-panel-linux-amd64 serve
```

Run it under `systemd` for production so it restarts on boot and on failure. The guided installer can generate the unit for you, or write your own that runs `nasnet-panel serve` with the `.env` loaded.

---

## Offline bundle

For servers without outbound internet access, releases also include an **offline bundle** (`nasnet-panel-offline-linux-<arch>`) that packages the panel together with a standalone **PostgreSQL** and the **Xray-core** binary, so you can install without pulling anything at runtime.

---

## Build from source

You need **Go 1.26+**, **Node 22+**, and **pnpm**. The web panel is embedded into the Go binary via `go:embed`, so it must be built **before** `go build`.

```bash
make web        # build the admin panel (web-panel → dist)
make geofiles   # fetch the geoip/geosite data files for embedding
make build      # builds frontends + geofiles, then `go build -o main .`

./main serve
```

See the [Development guide](./development.md) for the full toolchain, protobuf generation, and tests.

---

## First login & next steps

1. Open `APP_BASE_URL` and sign in with `ADMIN_USERNAME` / your password.
2. Review **Settings** (some env values are also editable in the panel and stored in the database).
3. Check the local Xray core and push your inbounds — [Server & Xray](./nodes-and-agents.md).
4. Create clients and hand out subscription links — [Subscriptions & Clients](./subscriptions-and-clients.md).

## Updating

- **Docker:** `git pull` (or pull new images) and `docker compose up -d --build`.
- **systemd / binary:** replace the binary with the new release and restart the service.
- **Either:** use `nasnet-tool.sh` → *Update*.

Database schema migrations run automatically on startup. **Always take a backup first.**

## Uninstalling

Use `nasnet-tool.sh` → *Uninstall*, or for Docker simply `docker compose down` (add `-v` to also delete data volumes — this is destructive).

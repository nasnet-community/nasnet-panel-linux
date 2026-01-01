# Getting Started — Install NasNet Panel

**🇬🇧 English** · [🇮🇷 فارسی](./getting-started.fa.md)

A simple, step-by-step guide to get NasNet Panel running on your server. When you finish, you'll open the panel in your browser and log in as admin.

Pick **one** of the two paths below. If you're not sure, use **Option A**.

---

## Before you start

You need:

- ✅ A **64-bit Linux server** (`amd64` or `arm64`) — for example a fresh Ubuntu/Debian VPS.
- ✅ **Root or `sudo` access** on that server (log in over SSH).
- ✅ Either a **domain name** pointed at the server (recommended — enables automatic HTTPS) **or** just the server's **IP address** (works over plain HTTP).
- ✅ One free port for the panel — default **`9761`** (open it in your firewall / provider panel).

That's it. The installer takes care of Docker, the database, and all secrets for you.

---

## Option A — Guided installer (easiest) ⭐

One script asks a few questions and does everything: installs Docker if missing, generates all secrets and your admin password, writes the config, and starts the panel.

```bash
# 1. Download the project
git clone https://github.com/nasnet-community/nasnet-panel-linux.git
cd nasnet-panel-linux

# 2. Run the installer (sudo — it may install Docker and set up services)
sudo ./nasnet-tool.sh
```

The wizard will ask you:

1. **How is the server reached?** — pick **IP address** or **Domain name**.
2. **Admin username and password** — you type them; the tool hashes the password for you.
3. **Reverse proxy?** — answer **No** unless you already run nginx/Caddy for TLS.

Then it installs everything and starts the panel. When it finishes, it prints your **panel URL** — open it and log in. Done. ✅

> Later, run `sudo ./nasnet-tool.sh` again anytime to **update**, **back up / restore**, **reconfigure**, or **uninstall** — all from the same menu.

---

## Option B — Docker Compose (manual)

Choose this if you already have **Docker** + the **Docker Compose** plugin and prefer to edit the config yourself.

```bash
# 1. Download the project
git clone https://github.com/nasnet-community/nasnet-panel-linux.git
cd nasnet-panel-linux

# 2. Create your config file
cp .env.example .env
```

**3. Generate two secrets.** Run these and copy each result:

```bash
# JWT secret (goes into JWT_SECRET_KEY)
openssl rand -hex 32

# Admin password hash (goes into ADMIN_PASSWORD_HASH)
htpasswd -nbBC 10 "" "your-password" | tr -d ':\n' | sed 's/$2y/$2a/'
```

**4. Edit `.env`** and set at least these five values:

| Key | Set it to |
|-----|-----------|
| `ADMIN_USERNAME` | The login name you want |
| `ADMIN_PASSWORD_HASH` | The hash from step 3 |
| `JWT_SECRET_KEY` | The random string from step 3 |
| `APP_BASE_URL` | `https://your-domain` **or** `http://your-ip:9761` |
| `DB_PASSWORD` | Any strong password for the database |

**5. Start it:**

```bash
docker compose up -d
docker compose logs -f app     # watch startup; Ctrl+C to stop watching
```

When the log shows the app is ready, open your `APP_BASE_URL` and log in. ✅

<details>
<summary><b>Prefer SQLite (no separate database)?</b></summary>

For a small setup you can skip PostgreSQL. In `.env` set:

```dotenv
DB_DRIVER=sqlite
DB_PATH=/app/data/nasnet_panel.db
```

Then start with the SQLite override file:

```bash
docker compose -f docker-compose.yml -f docker-compose.sqlite.yml up -d
```
</details>

---

## First login

1. Open your panel URL (from the installer, or your `APP_BASE_URL`).
2. Sign in with your **admin username** and **password**.
3. You're in! Next, set up your proxy and hand out subscriptions:
   - [Server & Xray](./nodes-and-agents.md) — turn on the proxy core and add inbounds.
   - [Subscriptions & Clients](./subscriptions-and-clients.md) — create users and share links.

---

## Keeping it running

- **Update:** `sudo ./nasnet-tool.sh` → **Update**, or (Docker) `git pull && docker compose up -d --build`.
- **Back up first** — schema migrations run automatically on update. See [Backup & Restore](./backup-and-restore.md).
- **Uninstall:** `sudo ./nasnet-tool.sh` → **Uninstall**, or (Docker) `docker compose down` (`-v` also deletes data — destructive).

---

## Something went wrong?

- Check the logs: `docker compose logs -f app` (or `sudo ./nasnet-tool.sh` → view logs).
- See [Troubleshooting & FAQ](./troubleshooting-and-faq.md).
- Need a different setup (bare binary, systemd, offline server, build from source)? The full [Installation reference](./installation.md) covers every method.

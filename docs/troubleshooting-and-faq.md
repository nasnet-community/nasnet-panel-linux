# Troubleshooting & FAQ

## Troubleshooting

### I can't log into the panel
- `ADMIN_PASSWORD_HASH` must be a **bcrypt** hash, not a plaintext password. Generate it with:
  ```bash
  htpasswd -nbBC 10 "" "your-password" | tr -d ':\n' | sed 's/$2y/$2a/'
  ```
- Confirm `ADMIN_USERNAME` matches what you're typing.
- After changing `.env`, **restart** the panel.

### The panel loads over HTTP but login/cookies don't stick
- Secure cookies need HTTPS. Either enable ACME, provide a certificate, or run behind a TLS-terminating proxy, then set `JWT_COOKIE_SECURE=true` and `JWT_COOKIE_DOMAIN`.
- If you're on a bare IP for testing, set `JWT_COOKIE_SECURE=false` and leave `JWT_COOKIE_DOMAIN` empty. See [TLS, ACME & Domains](./tls-acme-and-domains.md).

### The Telegram bot won't start
- The panel logs the bot error and keeps running — find the error in the panel logs.
- Verify `TELEGRAM_ENABLED=true` and that `TELEGRAM_BOT_TOKEN` is correct.
- If the panel's network can't reach the Telegram API, configure the SOCKS5 proxy variables (`TELEGRAM_PROXY_*`). See [Telegram](./telegram.md).
- In webhook mode, `WEBHOOK_URL` must be a reachable public HTTPS URL.

### ACME / Let's Encrypt fails to issue a certificate
- DNS for the domain in `APP_BASE_URL` must resolve to this server, and the ACME challenge must reach it.
- Set `ACME_STAGING=true` to test without hitting Let's Encrypt **rate limits**, then switch to production once it works.
- Make sure `ACME_EMAIL` is set — issuance is disabled without it.
- The `acme_data` volume must be writable and persistent.

### Metrics endpoint returns nothing
- `METRICS_ENABLED` must be `true` at startup.
- The panel's **Metrics Enabled** toggle (Settings → Server) can disable collection at runtime even when the env var is on. See [Monitoring & Alerting](./monitoring-and-alerting.md).
- If you set `METRICS_USERNAME`/`METRICS_PASSWORD`, your scraper must send basic auth.

### A subscription link returns empty / clients show no servers
- Check the subscription isn't **expired** or **over its data limit**.
- Ensure inbounds are configured and that Xray-core is **running** — if Xray is down or has no inbounds, the link has no servers to return. See [Server & Xray](./nodes-and-agents.md).
- Confirm `APP_BASE_URL` is correct — it's the base for the `/sub/{key}` link.

### Database / migration errors on startup
- Migrations run automatically on boot; the logs will name the failing step.
- For PostgreSQL, verify `DB_*` connection settings and that the database is reachable and healthy.
- **Restore from a backup** if a migration leaves the database in a bad state — and report the issue.

## FAQ

**PostgreSQL or SQLite — which should I use?**
PostgreSQL is the default and recommended for production. SQLite is great for trials and lightweight deployments (set `DB_DRIVER=sqlite`).

**Do I have to use Telegram?**
No. The bot is optional (`TELEGRAM_ENABLED=false`). The web panel and subscription links work without it.

**Can I run without a domain?**
Yes for testing (plain HTTP on an IP), but secure cookies require HTTPS — use a domain for production.

**What's the default port?**
`9761` (`APP_PORT`).

**Where is my data stored?**
In PostgreSQL or the SQLite file (`DB_PATH`). Backups go to `BACKUP_DIR`. ACME certificates live in `ACME_CACHE_DIR`. In Docker these are named volumes / bind mounts.

**How do I update safely?**
Take a backup, deploy the new binary/image, restart. Migrations apply automatically. See [Installation → Updating](./installation.md#updating).

**Where's the metrics catalog?**
The full list of Prometheus metrics is in [`pkg/metrics/METRICS.md`](../pkg/metrics/METRICS.md).

---

Still stuck? Open an issue at [github.com/nasnet-community/nasnet-panel-linux/issues](https://github.com/nasnet-community/nasnet-panel-linux/issues) with your panel logs (redact secrets) and a description of what you expected.

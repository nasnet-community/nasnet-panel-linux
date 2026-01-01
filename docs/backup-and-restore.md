# Backup & Restore

This page covers protecting your data with backups and restoring them.

## Backups

The panel can produce database backups into `BACKUP_DIR` (default `./data/backups`, bind-mounted in Docker). Backups work for both PostgreSQL and SQLite.

You can create and download backups from:

- the **panel** (Settings / Backups area), and
- the **Telegram bot** (admin backup command).

**Always take a backup before upgrading**, since schema migrations run automatically on startup.

> In Docker, make sure the `./data/backups` bind mount (and your database volume) are included in your host-level backup routine so they survive a server loss.

## Restore

Restoring re-imports a backup into the database.

- For **SQLite**, a restore swaps the database file and the process restarts. Because a restored database may carry deployment-specific settings (URLs, ports, tokens) from a *different* server, the panel drops a reseed marker and, on the next start, re-applies your local environment settings over the restored values — so your current `.env` wins for deployment-specific keys. This prevents a restored backup from pointing your panel at the wrong URL.
- For **PostgreSQL**, restore into the database the panel connects to (a standard `pg_restore`/`psql` workflow against the `nasnet_panel` database), then restart the panel.

After any restore, verify your `APP_BASE_URL` and admin credentials.

## Disaster-recovery checklist

- [ ] Database backups are produced regularly and copied **off** the server.
- [ ] The `acme_data` volume (issued certificates) is backed up, or you can re-issue.
- [ ] Your `.env` (secrets: `JWT_SECRET_KEY`, `DB_PASSWORD`, admin hash) is stored securely.
- [ ] You've tested a restore at least once on a throwaway host.

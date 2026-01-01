# Telegram Bot

The Telegram bot is **optional**. It gives admins a remote control for the panel and gives users a self-serve view of their subscription — without leaving Telegram.

If the bot fails to start (bad token, network), the panel logs the error and keeps running — the web panel and API are unaffected.

## Enabling the bot

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy its token.
2. Find your Telegram numeric user ID (e.g. via [@userinfobot](https://t.me/userinfobot)).
3. Set in `.env`:

```dotenv
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=123456:ABC-your-token
ADMIN_IDS=11111111,22222222     # comma-separated admin Telegram IDs
```

4. Restart the panel. The IDs in `ADMIN_IDS` are synced as admins in the database on startup.

## Polling vs webhook

| Mode | Set | Notes |
|------|-----|-------|
| **Polling** (default) | `BOT_MODE=polling` | The panel long-polls Telegram. No public URL or inbound port needed. Simplest. |
| **Webhook** | `BOT_MODE=webhook` + `WEBHOOK_URL=https://your-domain/...` | Telegram pushes updates to your URL. Requires a public HTTPS endpoint. |

## Routing the bot through a proxy

If the panel's network can't reach the Telegram Bot API directly, route the bot's API traffic through a SOCKS5 proxy:

```dotenv
TELEGRAM_PROXY_ENABLED=true
TELEGRAM_PROXY_TYPE=socks5
TELEGRAM_PROXY_HOST=127.0.0.1
TELEGRAM_PROXY_PORT=1080
TELEGRAM_PROXY_USERNAME=
TELEGRAM_PROXY_PASSWORD=
```

These proxy settings are also editable from the panel's Settings page (the database value overrides `.env`).

## What admins can do from the bot

- Manage users and subscriptions; create manual subscriptions.
- Manage inbounds, outbounds, and routing / balancing rules on the server.
- Manage SNI values.
- Toggle maintenance mode and broadcast messages.
- Export data, trigger backups, and review the audit log.
- View system status.

## What users can do from the bot

- View their subscription: link, QR code, remaining data and time, and devices.

## Linking a web account to Telegram

The panel supports link tokens that associate a panel/user account with a Telegram identity, so the same user is recognized across the web and Telegram surfaces.

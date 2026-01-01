# TLS, ACME & Domains

You should serve the panel over **HTTPS** in production: secure cookies require it. There are three ways to get TLS, and the app picks one at startup in this order: **manual certificate files → ACME → plain HTTP**.

## Option 1 — Automatic certificates (ACME / Let's Encrypt)

The app can obtain and renew a certificate for your domain automatically.

```dotenv
APP_BASE_URL=https://panel.example.com
ACME_ENABLED=true
ACME_EMAIL=you@example.com
ACME_STAGING=false          # true while testing, to avoid rate limits
ACME_AUTO_RENEW=true
ACME_CACHE_DIR=/app/data/acme
JWT_COOKIE_DOMAIN=example.com
JWT_COOKIE_SECURE=true
```

On startup the app parses the hostname from `APP_BASE_URL`, obtains a certificate for it, serves HTTPS, and keeps the certificate renewed. Issued certificates are cached in `ACME_CACHE_DIR` (a persistent volume in Docker) so restarts don't re-issue.

> Requirements: the domain's DNS must point at the server, and the ACME challenge must be able to reach it. Use `ACME_STAGING=true` first to validate your setup without burning Let's Encrypt rate limits, then switch to production.

ACME settings are also **panel-editable** (stored in the database, applied on restart), so you can flip ACME on/off or change the email from Settings without editing `.env`.

## Option 2 — Bring your own certificate

If you already have a certificate (e.g. a wildcard, or one from another CA), point the app at the PEM files:

```dotenv
TLS_CERT_FILE=/path/to/fullchain.pem
TLS_KEY_FILE=/path/to/privkey.pem
```

Manual certificate files take precedence over ACME. The cert/key paths are also panel-editable.

## Option 3 — Behind a reverse proxy (recommended for complex setups)

Terminate TLS at a reverse proxy (nginx, Caddy, Traefik, a CDN) and let the app speak plain HTTP locally. Set `APP_BASE_URL` to the **public HTTPS** URL even though the app listens on HTTP — leave `ACME_ENABLED=false` and don't set the manual cert files.

```dotenv
APP_BASE_URL=https://panel.example.com
ACME_ENABLED=false
JWT_COOKIE_SECURE=true
JWT_COOKIE_DOMAIN=example.com
```

Example nginx server block:

```nginx
server {
    listen 443 ssl http2;
    server_name panel.example.com;

    ssl_certificate     /etc/letsencrypt/live/panel.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/panel.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:9761;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSockets (live terminal, charts, support chat, SSE)
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }
}
```

The panel uses WebSockets and server-sent events for the live terminal, charts, and chat, so make sure your proxy passes `Upgrade`/`Connection` headers and uses generous timeouts.

## Running on a bare IP (no domain)

You can run over plain HTTP on an IP for testing:

```dotenv
APP_BASE_URL=http://203.0.113.10:9761
ACME_ENABLED=false
JWT_COOKIE_SECURE=false
JWT_COOKIE_DOMAIN=
```

This is fine for a quick trial, but secure cookies won't work without HTTPS — use a domain for anything real.

## Quick reference

| Goal | Set |
|------|-----|
| Auto HTTPS on a domain | `ACME_ENABLED=true`, `ACME_EMAIL`, `APP_BASE_URL=https://…` |
| Own certificate | `TLS_CERT_FILE`, `TLS_KEY_FILE` |
| Behind a proxy/CDN | `APP_BASE_URL=https://…`, ACME off, secure cookies on |
| Plain IP (testing) | `APP_BASE_URL=http://ip:port`, ACME off, secure cookies off |

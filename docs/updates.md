# Updates

The panel updates itself from **Settings → Updates**. It looks for a new release every 6 hours and installs only when you click **Update…**, now or at a time you pick.

## What you see

- **Panel version** — the status, the version you run, the one on offer and its size, and when it last checked. **Check now** looks at once, whether or not automatic checks are on.
- **What's new** — the release notes of the version on offer, from the signed release.
- **Other parts** — node agents behind the version this panel ships, nodes whose Xray is older or newer than the one this panel was tested with, and the built-in geofiles. Each row links to Nodes with those nodes selected; nothing there updates from Settings.
- **Update settings** — automatic checks, the channel, a mirror, and the route a download takes.
- **History** — every update and rollback on this server, with how long it took or why it failed.

While an update is on offer, Settings has an amber dot in the sidebar and the header shows **Update to vX**. Admins also get a notification once per version (Telegram by default; Discord and webhooks under **Settings → Telegram & alerts → Alert events → Panel**).

## How an update runs

An update is one job. You can close the tab; it keeps going.

1. **Checks** — this is a systemd install running as root, no network change is waiting for confirmation, no backup or restore is running, and there's room for the download.
2. **Download**, resuming where it stopped.
3. **Signature** — the download must match the signed `release.json`, and every file in the bundle its signed list.
4. **Database backup**, named `pre_update_<from>_to_<to>_<time>.sql` in Backups. The newest three are kept.
5. **Unpack**, and the release's Xray goes into the panel's cache.
6. **Test** — the new binary has to start and read this install's settings and database.
7. **System files** — the release rewrites the panel's service unit and the `nasnet` command; the old ones are saved first.
8. **Switch** — the agents, `nasnet-tool.sh` and the panel binary are replaced, each in one step.
9. **Restart**.
10. **Health check** — within 2 minutes the new version must answer with its database up and, if it ran before, the built-in Xray running. If it doesn't, or it crashes 3 times, the old version and every saved file are put back and it restarts. The notification says why.

The panel, the bot, subscription pages and the built-in node's Xray are down for about a minute. Remote nodes keep serving users, and reverse nodes reconnect on their own.

Steps 7 to 10 run outside the panel (in a separate systemd unit), so they survive the restart and can undo a version that never starts.

## Update at a time

Pick **At a time** in the update dialog. The time is your browser's. The panel runs the same checks then; if they fail, or the panel was down at that time and more than 10 minutes have passed, the update is skipped and admins are told why. **Cancel…** on the Scheduled row drops it.

## Roll back

For 7 days after an update, **Roll back…** puts the previous version's binaries and system files back and restarts. The database is left as it is: the older version ignores what the newer one added, and the pre-update backup is in Backups if you need it. If the older version doesn't come up, the newer one is put back and you're told.

## Channels

- **Stable** offers releases only.
- **Beta** also offers prereleases such as `v1.2.0-rc.1`.

Updates never go backwards. Turning beta off while you run a beta waits for the next stable release.

Some releases need an earlier one first. The section then names the version to install before it.

## Servers that can't reach GitHub

The check and the download try, in order:

1. github.com directly;
2. the outbound proxy, when **Panel updates** is ticked under **Settings → Panel → Outbound proxy → Used for**;
3. the mirror, when one is set in **Update settings**: a URL prefix in front of `https://github.com/...` links, such as a GitHub proxy you run.

Or upload the update file: download `nasnet-panel-offline-linux-<arch>.tar.gz` from the release page on a computer that can reach GitHub, then **Upload…** in Panel version. The file is checked against its signature before anything else, and refused if it's for another architecture or older than what you run. Unused uploads are deleted after a day.

## Docker and source installs

These don't update from the panel. The section shows what to run instead:

- **Docker:** `git pull && docker compose up -d --build`, in the folder with `docker-compose.yml`.
- **Source build:** `sudo nasnet`, then **Update**.
- **An install that doesn't run as root yet:** `sudo nasnet`, then **Auto-Update**, once. It also switches the install to root; after that, updates run from the panel.

## From the command line

`sudo nasnet` → **Auto-Update** runs the same updater. The panel binary has it too:

```bash
sudo nasnet-panel update check            # look for a release now
sudo nasnet-panel update status           # version, last check, current job
sudo nasnet-panel update apply            # install the newest release
sudo nasnet-panel update apply --version v1.2.0
sudo nasnet-panel update apply --file nasnet-panel-offline-linux-amd64.tar.gz
sudo nasnet-panel update rollback         # within 7 days
```

`check` and `status` take `--json`.

## The built-in node's Xray

The built-in node runs the Xray version this panel was tested with, from the panel's own cache, and moves with each panel update. Picking another version in its Xray dialog keeps that version through panel updates and rollbacks until you pick **Follow the panel**. A version newer than the tested one is allowed; the dialog and **Other parts** say it isn't tested with this panel.

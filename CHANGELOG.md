# Changelog

All notable changes to this project are documented in release notes under [`docs/releases/`](./docs/releases/).

This file follows the spirit of [Keep a Changelog](https://keepachangelog.com/) and [Semantic Versioning](https://semver.org/).

## How releases are cut

Pushing a `v*` tag runs [`.github/workflows/release.yml`](./.github/workflows/release.yml): it builds frontends, compiles `nasnet-panel` / `nasnet-tool` for `linux/amd64` and `linux/arm64`, and publishes offline bundles.

## Released versions

| Version | Notes |
|---------|--------|
| [v1.3.0](./docs/releases/v1.3.0.md) | Domestic dial path, admin password reset, agent packaging |
| [v1.2.0](./docs/releases/v1.2.0.md) | See release notes |
| [v1.0.0](./docs/releases/v1.0.0.md) | First stable line: remote nodes, tunnels, router mode |

For unreleased work on `main`, read merged PR titles until the next tag ships.

## Unreleased

Changes merged to `main` after the latest tag belong here until a release note is written under `docs/releases/`.

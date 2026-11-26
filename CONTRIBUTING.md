# Contributing

Thanks for helping improve NasNet Panel Linux.

## Before you start

1. Search [existing issues](https://github.com/nasnet-community/nasnet-panel-linux/issues) and PRs.
2. For larger changes, open an issue first so maintainers can agree on direction.
3. This repo is the **Linux** VPN/tunnel panel. MikroTik / Starlink UI work belongs in [nasnet-panel](https://github.com/nasnet-community/nasnet-panel).

## Development setup

Follow **[docs/development.md](./docs/development.md)** for toolchain, layout, build, and tests.

Short path:

```bash
git clone https://github.com/nasnet-community/nasnet-panel-linux.git
cd nasnet-panel-linux
cp .env.example .env   # fill admin hash, JWT, APP_BASE_URL
make web geofiles
go test ./...
```

Frontend-only: `cd web-panel && pnpm install && pnpm test`.

## Pull requests

- Branch from `main`; keep the PR focused on one concern.
- Match surrounding code style; prefer small, reviewable diffs.
- Add or update tests when behavior changes.
- Update docs (README / `docs/`) when operators need to know about the change.
- Do not commit secrets, local `.env`, or generated `web-panel/dist` unless the project already tracks that artifact for a reason.
- First-time fork PRs need a maintainer to **Approve and run workflows** before CI starts — mention that in the PR if checks stay pending.

## Commit messages

Use a short imperative summary (optionally Conventional Commit prefixes like `feat:`, `fix:`, `docs:`). Explain *why* in the body when the diff is not obvious.

## Code of conduct

Be respectful in issues and reviews. Harassment or abuse is not welcome.

## License

By contributing you agree your changes are licensed under the same [AGPL-3.0](./LICENSE) terms as the project.

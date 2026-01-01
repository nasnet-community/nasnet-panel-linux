# Development

This guide is for contributors and anyone building from source.

## Prerequisites

- **Go** 1.26+
- **Node** 22+ and **pnpm** (the Docker build pins `pnpm@9.15.4`)
- **protoc** + `protoc-gen-go` + `protoc-gen-go-grpc` (only needed if you regenerate gRPC code)
- **make**

## Project layout

```
.
├── main.go              # entry point; wires the embedded SPA, calls cmd.Execute()
├── embed.go             # go:embed of web-panel/dist (admin + subscriber SPA)
├── cmd/                 # CLI commands
│   ├── root.go          #   `nasnet-panel serve` — starts the HTTP API + Telegram bot
│   ├── bootstrap.go     #   dependency injection (initRepositories / initUsecases)
│   ├── hash.go          #   `hash-password` subcommand (bcrypt)
│   ├── add_xray_user/   #   standalone helper to add an Xray user via the local API
│   └── nasnet-tool/     #   the nasnet-tool installer binary (TUI; logic in internal/tool)
├── config/              # env-based configuration + validation
├── internal/            # feature modules (clean architecture)
│   └── <feature>/
│       ├── domain/      #   entities + interfaces
│       ├── usecase/     #   business logic
│       ├── repository/  #   GORM data access
│       └── delivery/    #   http/ and telegram/ handlers
│   ├── agent/           # local Xray supervision (process, server, ssh, stats, tc, traffic, …)
│   └── tool/            # logic behind the nasnet-tool installer TUI
├── pkg/                 # reusable libraries (jwt, acme, events, scheduler, metrics, xray, …)
├── transport/           # http server + telegram bot routing
├── proto/               # protobuf definitions
├── prometheus/          # Prometheus config template
├── scripts/             # install/build helper scripts
└── web-panel/           # admin SPA (React + Vite + TS)
```

Each feature module follows the same `domain → usecase → repository → delivery` layering described in [Architecture](./architecture.md).

## Building

The admin panel SPA is embedded into the Go binary via `go:embed`, so **`web-panel/dist` must exist before `go build`.** The `Makefile` builds the frontend in the right order:

```bash
make web        # cd web-panel && pnpm install && pnpm build
make geofiles   # download the geoip/geosite data for embedding
make build      # web + geofiles, then `go build -o main .`

./main serve
```

Version metadata is injected via `-ldflags` (`Version`, `Commit`, `BuildTime`); `make` derives these from git automatically.

### Regenerating protobuf

```bash
make proto      # runs protoc for proto/node_agent.proto → pkg/agent/pb
```

See [`proto/README.md`](../proto/README.md) for the toolchain setup.

## Running locally

1. `cp .env.example .env` and fill in the essentials (see [Configuration](./configuration.md)).
2. Use SQLite for a quick start: `DB_DRIVER=sqlite`, `DB_PATH=./data/nasnet_panel.db`.
3. Build the frontend once (`make web`), then `go run . serve` (or `./main serve`).

For frontend work, run the Vite dev server inside `web-panel/` (`pnpm dev`) and point it at your running panel.

## Tests

```bash
go test ./...                 # backend unit tests (extensive)
cd web-panel && pnpm test     # frontend tests (vitest)
```

The backend test suite is thorough — there are `*_test.go` files throughout `internal/` and `pkg/`, including race tests (`go test -race ./...`). Run the full suite before opening a PR.

## CI & releases

- **CI** runs on pushes/PRs (`.github/workflows/ci.yml`) — build and tests.
- **Releases** are cut by pushing a tag matching `v*` (`.github/workflows/release.yml`). The release job builds the frontends, compiles `nasnet-panel` and `nasnet-tool` for `linux/amd64` and `linux/arm64`, and assembles **offline bundles** that include a standalone PostgreSQL and the Xray-core binary.

## Conventions

- Keep transport concerns in `delivery/` and business logic in `usecase/`; `domain/` must not import frameworks.
- Talk to the database only through repositories.
- Emit domain events from usecases rather than calling other subsystems directly where possible — subscribers (metrics, alerts, notifications) pick them up.
- Money is stored as **integer cents**; use the `pkg/money` helpers.
- New persistent models must be registered for auto-migration in `cmd/root.go`.

## Adding a feature module

1. Create `internal/<feature>/domain` with the entity and interfaces.
2. Implement `repository` against GORM.
3. Implement `usecase` with the business rules.
4. Add `delivery/http` (and/or `delivery/telegram`) handlers.
5. Construct it in `cmd/bootstrap.go` and register routes in `transport/`.
6. Register any new model for auto-migration in `cmd/root.go`.
7. Add tests at each layer.

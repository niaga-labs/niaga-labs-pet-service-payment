# Kilat Pet Delivery - service-payment

Money: the escrow saga over a booking lifecycle, the Stripe anti-corruption layer, runner cash-outs, promo codes and subscriptions.
Jira project **KPD** - GitHub `niaga-labs/niaga-labs-pet-service-payment` - stack **Go 1.24 - Gin - GORM - PostgreSQL - Kafka**. Global rules live in `~/.claude/`;
this file only adds what is specific here.

## Orient here first

- `.claude/memory/project_state.md` - **resume here** (`/continue` reads it, `/recap` rewrites it).
- `README.md` - how to run it. `CHANGELOG.md` - what changed.
- The workspace map: `~/Documents/kilat-pet-delivery/CLAUDE.md`.

## Commands

| Task | Command |
|---|---|
| install | `go mod download` |
| run | `go run ./cmd/server` (copy `.env.example` to `.env` first) |
| test | `go test ./...` |
| integration tests | `go test -tags integration ./...` - needs Docker (testcontainers) |
| lint | `gofmt -l . && go vet ./...` |
| build | `go build ./...` |
| migrate | `go run ./cmd/migrate` - applies `migrations/` and exits |

Needs the dev-infra stack: Postgres database `kilat_payment`, Kafka on `localhost:9092` -> `cd ~/Documents/dev-infra; ./dev.ps1 up kilat`.

## Conventions that differ from the global rules

- **Ticket branches and PRs** - company repo, never commit on `main` (`branch-guard` enforces it).
- **One migration path.** `migrations/` owns the schema in every environment including development, and `cmd/server` applies it at startup. There is deliberately no GORM AutoMigrate branch - that is what let six services drift (KPD-56 through KPD-61).
- Protected paths (never edited in place, see `.claude/protected-paths.txt`): `migrations/*.sql`.

## Testing

`go test ./...` - 11 passing (7 in internal/handler, 4 in internal/rail). Four of the six test files are behind the `integration` tag.

## Where things are

- `cmd/server` - `cmd/migrate` - `internal/saga/payment_saga.go` the escrow saga - `internal/rail` payout rails, with a simulated one for development - `internal/domain/{payment,promo,subscription}`

## Worth knowing

- On this Windows laptop `go test ./...` reports internal/rail as FAIL because Application Control blocks the test binary in the Go build cache. Compile it out of the cache instead: `go test -c -o /tmp/rail.test.exe ./internal/rail/`. Not a code failure.

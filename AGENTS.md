# kbp-sqlite

Thin Go wrapper around `database/sql` + [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) (CGo-free). Exposes an options-based `Open` API plus two shortcut constructors: `OpenWAL` (production defaults) and `OpenMemory` (in-memory, for tests).

## Build & Test

```sh
make build   # go build
make test    # go test -v ./...
make dep     # go mod tidy
```

Tests create real `.db` files in the working directory; the `setup()` helper handles creation and cleanup (including `-shm`/`-wal` WAL side files). For tests that don't need file persistence, prefer `sqlite.OpenMemory(migrations)` — no `setup()` or cleanup needed.

## Architecture

Single-file library: all public API lives in `sqlite.go`; tests in `sqlite_test.go` (external `sqlite_test` package).

## Conventions

- **Options pattern**: every configuration is an `Option func(*DB)`. `WithPRAGMA(name, value)` is the primitive; all other `WithXxx()` helpers delegate to it.
- **`Open()` panics** on any startup failure (bad DSN, PRAGMA error, migration error). This is intentional — misconfiguration at startup should crash fast.
- **`BeginImmediate`**: `database/sql` always opens an implicit `BEGIN` (deferred). To acquire the write lock upfront, `Begin` does `ROLLBACK; BEGIN IMMEDIATE` inside the same `sql.Tx`. Don't change this pattern.
- **Migrations**: SQL files in `migrations/` are applied in lexicographic order and tracked in a `migrations` table (`WITHOUT ROWID`). Each file runs exactly once in its own `BeginImmediate` transaction. New migrations get the next numeric prefix (e.g. `0004_...sql`).
- **`WithDSN`**: checks `os.Getenv(value)` first; falls back to using the string as a literal DSN. `OpenWAL("DATABASE_DSN", mig)` works both as an env-var name (production) and a file path (tests).
- **No CGo**: the driver is `modernc.org/sqlite`; never introduce `mattn/go-sqlite3` or other CGo-based drivers.
- **Test helpers**: `insertUsers`, `selectUsers`, `selectUser`, `deleteUsers` are package-level helpers shared across sub-tests. Follow the `test` struct + `execute` pattern for ordered sub-tests sharing one DB.

## Pitfalls

- **`OpenWAL` uses `locking_mode=EXCLUSIVE`**: only one process can open the file at a time. Use `Open()` with custom options for multi-process or read-only consumer scenarios.
- **`Error` has no `Unwrap()`**: use a type assertion `err.(*sqlite.Error)` to inspect fields — `errors.As` / `errors.Is` will not traverse through it.
- **`Open()` clears the migration list** after running: storing the `*DB` and calling `Open` again with the same option set is not supported.

## Key Files

- `sqlite.go` — entire public API
- `migrations/` — embedded SQL migration scripts
- `README.md` — full usage examples and PRAGMA option table

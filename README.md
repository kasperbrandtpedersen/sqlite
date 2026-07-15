# sqlite

Simple SQLite wrappers for Go using [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) (CGo-free).

## Install

```sh
go get github.com/kasperbrandtpedersen/sqlite
```

## Usage

### Quick start with WAL mode(recommended for production)

```go
//go:embed migrations/*.sql
var migrations embed.FS

db := sqlite.OpenWAL("DATABASE_DSN", migrations)
defer db.Close()
```

`OpenWAL` reads the DSN from the `DATABASE_DSN` environment variable (or uses the value directly if no matching env var exists) and opens the database with sensible production defaults: WAL journal mode, NORMAL synchronous, 64 MB cache, 128 MB mmap, foreign keys on, in-memory temp store, exclusive locking, 4 threads, and a 1 s busy timeout.

### Custom configuration

```go
db := sqlite.Open(
    sqlite.WithDSN("DATABASE_DSN"),
    sqlite.WithMigrations(migrations),
    sqlite.WithJournalModeWAL(),
    sqlite.WithForeignKeysON(),
    sqlite.WithBusyTimeout5S(),
)
defer db.Close()
```

### Migrations

Place numbered SQL files in a `migrations/` directory and embed them:

```
migrations/
  0001_init.sql
  0002_add_column.sql
```

```go
//go:embed migrations/*.sql
var migrations embed.FS

sqlite.WithMigrations(migrations)          // uses "migrations/" directory
sqlite.WithMigrationsDir(migrations, "db/migrations") // custom directory
```

Migrations are applied in lexicographic order and tracked in a `migrations` table so each script runs exactly once.

### Querying

```go
ctx := context.Background()

rows, err := db.Query(ctx, "SELECT id, name FROM users WHERE age > ?", 18)

row := db.QueryRow(ctx, "SELECT name FROM users WHERE id = ?", 1)

_, err = db.Exec(ctx, "DELETE FROM users WHERE id = ?", 42)
```

### Transactions

`Begin` opens an `IMMEDIATE` transaction, preventing write conflicts in WAL mode.

```go
tx, err := db.Begin(ctx)
if err != nil { ... }
defer tx.Rollback()

// ... use tx ...

tx.Commit()
```

## PRAGMA options

| Option | PRAGMA |
|---|---|
| `WithSynchronousOFF/NORMAL/FULL/EXTRA` | `synchronous` |
| `WithJournalModeWAL` | `journal_mode = WAL` |
| `WithJournalSizeLimit64MB` | `journal_size_limit` |
| `WithCacheSize64MB` | `cache_size` |
| `WithMmapSize128MB / WithMmapSize20GB` | `mmap_size` |
| `WithForeignKeysON/OFF` | `foreign_keys` |
| `WithTempStoreMEMORY` | `temp_store` |
| `WithLockingModeNORMAL/EXCLUSIVE` | `locking_mode` |
| `WithThreads4 / WithThreads0` | `threads` |
| `WithBusyTimeout1S / WithBusyTimeout5S` | `busy_timeout` |
| `WithPRAGMA(name, value)` | any PRAGMA |

## License

[MIT](LICENSE)

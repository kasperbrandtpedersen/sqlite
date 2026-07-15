// Package sqlite wraps database/sql with opinionated defaults and embedded-FS migration support for SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// Error is a constant string sentinel for SQLite errors.
type Error string

// Error returns the string value of the error.
func (e Error) Error() string { return string(e) }

// DB is a SQLite connection with applied pragmas and migration history.
type DB struct {
	db                *sql.DB
	dsn               string
	pragmas           map[string]string
	migrations        []Migration
	appliedMigrations []string
	isMemory          bool
}

// Migration is a named SQL script applied exactly once, tracked in the migrations table.
type Migration struct {
	Name   string
	Script string
}

// Option configures a DB before it is opened.
type Option func(*DB)

// WithPRAGMA sets an arbitrary SQLite PRAGMA at open time.
func WithPRAGMA(name, value string) Option {
	return func(db *DB) {
		db.pragmas[name] = value
	}
}

func WithSynchronousOFF() Option    { return WithPRAGMA("synchronous", "OFF") }
func WithSynchronousNORMAL() Option { return WithPRAGMA("synchronous", "NORMAL") }
func WithSynchronousFULL() Option   { return WithPRAGMA("synchronous", "FULL") }
func WithSynchronousEXTRA() Option  { return WithPRAGMA("synchronous", "EXTRA") }

func WithCacheSize64MB() Option { return WithPRAGMA("cache_size", "-65536") }

func WithMmapSize20GB() Option  { return WithPRAGMA("mmap_size", "20000000000") }
func WithMmapSize128MB() Option { return WithPRAGMA("mmap_size", "134217728") }

func WithJournalModeWAL() Option { return WithPRAGMA("journal_mode", "WAL") }

func WithJournalSizeLimit64MB() Option { return WithPRAGMA("journal_size_limit", "67108864") }

func WithForeignKeysON() Option  { return WithPRAGMA("foreign_keys", "ON") }
func WithForeignKeysOFF() Option { return WithPRAGMA("foreign_keys", "OFF") }

func WithTempStoreMEMORY() Option { return WithPRAGMA("temp_store", "MEMORY") }

func WithLockingModeNORMAL() Option    { return WithPRAGMA("locking_mode", "NORMAL") }
func WithLockingModeEXCLUSIVE() Option { return WithPRAGMA("locking_mode", "EXCLUSIVE") }

func WithThreads4() Option { return WithPRAGMA("threads", "4") }
func WithThreads0() Option { return WithPRAGMA("threads", "0") }

func WithBusyTimeout5S() Option { return WithPRAGMA("busy_timeout", "5000") }
func WithBusyTimeout1S() Option { return WithPRAGMA("busy_timeout", "1000") }

// WithMigration appends a single migration to the run list.
func WithMigration(m Migration) Option {
	return func(db *DB) {
		db.migrations = append(db.migrations, m)
	}
}

// WithMigrations loads all .sql files from the "migrations" directory of the embedded FS.
func WithMigrations(files embed.FS) Option {
	return WithMigrationsDir(files, "migrations")
}

// WithMigrationsDir loads all .sql files from the given directory of the embedded FS.
func WithMigrationsDir(files embed.FS, dir string) Option {
	return func(db *DB) {
		entries, err := files.ReadDir(dir)
		if err != nil {
			panic(fmt.Errorf("read migrations directory %q: %w", dir, err))
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			file := fmt.Sprintf("%s/%s", dir, entry.Name())
			content, err := files.ReadFile(file)

			if err != nil {
				panic(fmt.Errorf("read migration file **%q**: %w", file, err))
			}

			migration := Migration{
				Name:   entry.Name(),
				Script: string(content),
			}

			db.migrations = append(db.migrations, migration)
		}
	}
}

// WithDSN sets the DSN from an environment variable, falling back to the literal value if unset.
func WithDSN(envVarOrValue string) Option {
	return func(db *DB) {
		dsn := os.Getenv(envVarOrValue)

		if dsn == "" {
			dsn = envVarOrValue
		}

		db.dsn = dsn
	}
}

// Open applies options, sets pragmas, runs pending migrations, and returns a ready DB. Panics on failure.
func Open(opts ...Option) *DB {
	db := &DB{
		pragmas: make(map[string]string),
	}

	for _, opt := range opts {
		opt(db)
	}

	sql, err := sql.Open("sqlite", db.dsn)

	if err != nil {
		panic(fmt.Errorf("open database: %w", err))
	}

	db.db = sql
	ctx := context.Background()

	for key, value := range db.pragmas {
		stmt := fmt.Sprintf("PRAGMA %s = %s;", key, value)

		if _, err := db.Exec(ctx, stmt); err != nil {
			panic(fmt.Errorf("write execute **%q**: %w", stmt, err))
		}
	}

	if err := migrate(ctx, db); err != nil {
		panic(err)
	} else {
		db.migrations = nil
	}

	if strings.HasPrefix(db.dsn, ":memory:") {
		db.isMemory = true
	}

	return db
}

// Default opens a DB with production-ready defaults: WAL journal, NORMAL sync, foreign keys, and exclusive locking.
func Default(dsnEnvVar string, migrations embed.FS) *DB {
	return Open(
		WithDSN(dsnEnvVar),
		WithMigrations(migrations),

		WithJournalModeWAL(),
		WithSynchronousNORMAL(),
		WithCacheSize64MB(),
		WithMmapSize128MB(),
		WithForeignKeysON(),
		WithTempStoreMEMORY(),
		WithLockingModeEXCLUSIVE(),
		WithThreads4(),
		WithBusyTimeout1S(),
	)
}

// Memory opens an in-memory DB with shared cache and foreign keys enabled. Useful for testing.
func Memory(migrations embed.FS) *DB {
	return Open(
		WithDSN(":memory:?cache=shared"),
		WithMigrations(migrations),

		WithForeignKeysON(),
	)
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.db.Close()
}

// String returns a debug summary of the DSN, active pragmas, and applied migrations.
func (db *DB) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "dsn: %s\n", db.dsn)

	keys := make([]string, 0, len(db.pragmas))
	for k := range db.pragmas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "pragma: %s=%s\n", k, db.pragmas[k])
	}

	for _, m := range db.appliedMigrations {
		fmt.Fprintf(&b, "migration: %s\n", m)
	}

	return b.String()
}

// Exec runs a write statement and returns the result.
func (db *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.db.ExecContext(ctx, query, args...)
}

// Begin starts a write transaction. Tries to use BEGIN IMMEDIATE to acquire the write lock upfront and avoid deadlocks.
func (db *DB) Begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := db.db.BeginTx(ctx, nil)

	if err != nil {
		return nil, err
	}

	// ROLLBACK ends the implicit BEGIN from BeginTx, then BEGIN IMMEDIATE acquires the write lock immediately.
	_, err = tx.ExecContext(ctx, "ROLLBACK; BEGIN IMMEDIATE")

	if err != nil {
		tx.Rollback()

		return nil, err
	}

	return tx, nil
}

// Query runs a read statement and returns multiple rows.
func (db *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.db.QueryContext(ctx, query, args...)
}

// QueryRow runs a read statement expected to return at most one row.
func (db *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return db.db.QueryRowContext(ctx, query, args...)
}

// migrate creates the migrations table if absent and applies each pending migration in name order.
func migrate(ctx context.Context, db *DB) error {
	const query = "CREATE TABLE IF NOT EXISTS migrations (name TEXT PRIMARY KEY, at TIMESTAMP DEFAULT CURRENT_TIMESTAMP) WITHOUT ROWID;"

	if _, err := db.db.Exec(query); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	sort.Slice(db.migrations, func(i, j int) bool {
		return db.migrations[i].Name < db.migrations[j].Name
	})

	for _, migration := range db.migrations {
		tx, err := db.Begin(ctx)

		if err != nil {
			return err
		}
		defer tx.Rollback()

		if err := migration.apply(tx); err != nil {
			return fmt.Errorf("apply migration **%q**: %w", migration.Name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration **%q**: %w", migration.Name, err)
		} else {
			db.appliedMigrations = append(db.appliedMigrations, migration.Name)
		}
	}

	return nil
}

// apply runs the migration script and records it; a no-op if already applied.
func (m *Migration) apply(tx *sql.Tx) error {
	var count int

	err := tx.QueryRow("SELECT COUNT(1) FROM migrations WHERE name = ?;", m.Name).Scan(&count)

	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	if _, err := tx.Exec(m.Script); err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO migrations (name) VALUES (?);", m.Name); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return nil
}

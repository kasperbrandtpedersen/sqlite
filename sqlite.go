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
type Error struct {
	// Area is the functional area of the error, e.g. "migrations", "transaction".
	Area string
	// Action is the specific action that failed, e.g. "open", "apply", "commit".
	Action string
	// Err is the underlying error returned by the database/sql driver.
	Err error
	// Msg is a format string describing the error, with Params applied.
	Msg string
	// Params are the values to apply to Msg.
	Params []any
}

// Unwrap returns the underlying error, enabling errors.Is and errors.As to traverse the chain.
func (err *Error) Unwrap() error {
	return err.Err
}

// Error returns a formatted string describing the error, including the area, action, message, and underlying error.
func (err *Error) Error() string {
	msg := fmt.Sprintf(err.Msg, err.Params...)

	return fmt.Sprintf("sqlite: %s: %s: %s: %v", err.Area, err.Action, msg, err.Err)
}

// String returns the same value as Error(), allowing fmt.Stringer to be used for logging and debugging.
func (err *Error) String() string {
	return err.Error()
}

// Raise creates a new Error with the given area, action, message, underlying error, and optional parameters.
func Raise(area, action, msg string, err error, params ...any) error {
	return &Error{
		Area:   area,
		Action: action,
		Err:    err,
		Msg:    msg,
		Params: params,
	}
}

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

// WithSynchronousOFF sets synchronous=OFF for maximum write speed. Skips all fsyncs; data may be lost on power failure.
func WithSynchronousOFF() Option { return WithPRAGMA("synchronous", "OFF") }

// WithSynchronousNORMAL sets synchronous=NORMAL. Safe in WAL mode; fsyncs on WAL checkpoints only.
func WithSynchronousNORMAL() Option { return WithPRAGMA("synchronous", "NORMAL") }

// WithSynchronousFULL sets synchronous=FULL. Fsyncs on every write; safe in all journal modes.
func WithSynchronousFULL() Option { return WithPRAGMA("synchronous", "FULL") }

// WithSynchronousEXTRA sets synchronous=EXTRA. Like FULL but also fsyncs the directory after rename; maximum durability.
func WithSynchronousEXTRA() Option { return WithPRAGMA("synchronous", "EXTRA") }

// WithCacheSize64MB sets the page cache to 64 MB (negative value interpreted as kibibytes by SQLite).
func WithCacheSize64MB() Option { return WithPRAGMA("cache_size", "-65536") }

// WithMmapSize20GB enables memory-mapped I/O up to 20 GB.
func WithMmapSize20GB() Option { return WithPRAGMA("mmap_size", "20000000000") }

// WithMmapSize128MB enables memory-mapped I/O up to 128 MB.
func WithMmapSize128MB() Option { return WithPRAGMA("mmap_size", "134217728") }

// WithJournalModeWAL switches to Write-Ahead Logging, enabling concurrent reads during writes.
func WithJournalModeWAL() Option { return WithPRAGMA("journal_mode", "WAL") }

// WithJournalSizeLimit64MB caps the WAL file at 64 MB; the next checkpoint will truncate it back to this limit.
func WithJournalSizeLimit64MB() Option { return WithPRAGMA("journal_size_limit", "67108864") }

// WithForeignKeysON enables enforcement of foreign key constraints.
func WithForeignKeysON() Option { return WithPRAGMA("foreign_keys", "ON") }

// WithForeignKeysOFF disables enforcement of foreign key constraints.
func WithForeignKeysOFF() Option { return WithPRAGMA("foreign_keys", "OFF") }

// WithTempStoreMEMORY keeps temporary tables and indices in memory instead of on disk.
func WithTempStoreMEMORY() Option { return WithPRAGMA("temp_store", "MEMORY") }

// WithLockingModeNORMAL releases file locks after each transaction, allowing multiple processes to share the file.
func WithLockingModeNORMAL() Option { return WithPRAGMA("locking_mode", "NORMAL") }

// WithLockingModeEXCLUSIVE holds an exclusive file lock for the lifetime of the connection; prevents other processes from opening the database.
func WithLockingModeEXCLUSIVE() Option { return WithPRAGMA("locking_mode", "EXCLUSIVE") }

// WithThreads4 allows SQLite to use up to 4 auxiliary threads for sorting and indexing operations.
func WithThreads4() Option { return WithPRAGMA("threads", "4") }

// WithThreads0 disables auxiliary threads; all work is performed on the calling goroutine.
func WithThreads0() Option { return WithPRAGMA("threads", "0") }

// WithBusyTimeout5S sets the busy-handler sleep to 5 seconds before returning SQLITE_BUSY.
func WithBusyTimeout5S() Option { return WithPRAGMA("busy_timeout", "5000") }

// WithBusyTimeout1S sets the busy-handler sleep to 1 second before returning SQLITE_BUSY.
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
			panic(Raise("migrations", "read_dir", "read migrations directory %q", err, dir))
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			file := fmt.Sprintf("%s/%s", dir, entry.Name())
			content, err := files.ReadFile(file)

			if err != nil {
				panic(Raise("migrations", "read_file", "read migration file %q", err, file))
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
		panic(Raise("open", "open", "open database", err))
	}

	db.db = sql
	ctx := context.Background()

	for key, value := range db.pragmas {
		stmt := fmt.Sprintf("PRAGMA %s = %s;", key, value)

		if _, err := db.Exec(ctx, stmt); err != nil {
			panic(Raise("open", "pragma", "execute pragma %q", err, stmt))
		}
	}

	if err := migrate(ctx, db); err != nil {
		panic(Raise("open", "migrate", "run migrations", err))
	} else {
		db.migrations = nil
	}

	if strings.HasPrefix(db.dsn, ":memory:") {
		db.isMemory = true
	}

	return db
}

// OpenWAL opens a DB with production-ready defaults: WAL journal, NORMAL sync, foreign keys, and exclusive locking.
func OpenWAL(dsnEnvVar string, migrations embed.FS) *DB {
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

// OpenMemory opens an in-memory DB with shared cache and foreign keys enabled. Useful for testing.
func OpenMemory(migrations embed.FS) *DB {
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
		return nil, Raise("transaction", "begin", "begin transaction", err)
	}

	// ROLLBACK ends the implicit BEGIN from BeginTx, then BEGIN IMMEDIATE acquires the write lock immediately.
	_, err = tx.ExecContext(ctx, "ROLLBACK; BEGIN IMMEDIATE")

	if err != nil {
		tx.Rollback()

		return nil, Raise("transaction", "begin_immediate", "begin immediate transaction", err)
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

// ExecContext implements the sql.DB interface for compatibility with database/sql. It runs a write statement and returns the result.
func (db *DB) ExecContext(ctx context.Context, sql string, params ...any) (sql.Result, error) {
	return db.Exec(ctx, sql, params...)
}

// PrepareContext implements the sql.DB interface for compatibility with database/sql. It prepares a statement for later execution.
func (db *DB) PrepareContext(ctx context.Context, sql string) (*sql.Stmt, error) {
	return db.db.PrepareContext(ctx, sql)
}

// QueryContext implements the sql.DB interface for compatibility with database/sql. It runs a read statement and returns multiple rows.
func (db *DB) QueryContext(ctx context.Context, sql string, params ...any) (*sql.Rows, error) {
	return db.Query(ctx, sql, params...)
}

// QueryRowContext implements the sql.DB interface for compatibility with database/sql. It runs a read statement expected to return at most one row.
func (db *DB) QueryRowContext(ctx context.Context, sql string, params ...any) *sql.Row {
	return db.QueryRow(ctx, sql, params...)

}

// migrate creates the migrations table if absent and applies each pending migration in name order.
func migrate(ctx context.Context, db *DB) error {
	const query = "CREATE TABLE IF NOT EXISTS migrations (name TEXT PRIMARY KEY, at TIMESTAMP DEFAULT CURRENT_TIMESTAMP) WITHOUT ROWID;"

	if _, err := db.db.Exec(query); err != nil {
		return Raise("migrations", "create_table", "create migrations table", err)
	}

	sort.Slice(db.migrations, func(i, j int) bool {
		return db.migrations[i].Name < db.migrations[j].Name
	})

	for _, migration := range db.migrations {
		tx, err := db.Begin(ctx)

		if err != nil {
			return Raise("migrations", "begin", "begin transaction", err)
		}
		defer tx.Rollback()

		if err := migration.apply(tx); err != nil {
			return Raise("migrations", "apply", "apply migration %q", err, migration.Name)
		}

		if err := tx.Commit(); err != nil {
			return Raise("migrations", "commit", "commit migration %q", err, migration.Name)
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
		return Raise("migration", "check", "check if migration %q was applied", err, m.Name)
	}

	if count > 0 {
		return nil
	}

	if _, err := tx.Exec(m.Script); err != nil {
		return Raise("migration", "execute", "execute migration %q", err, m.Name)
	}

	if _, err := tx.Exec("INSERT INTO migrations (name) VALUES (?);", m.Name); err != nil {
		return Raise("migration", "record", "record migration %q", err, m.Name)
	}

	return nil
}

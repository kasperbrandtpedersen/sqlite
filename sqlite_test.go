package sqlite_test

import (
	"embed"
	"os"
	"testing"

	"github.com/kasperbrandtpedersen/sqlite"
)

//go:embed test_migrations/*.sql
var migrations embed.FS

func setup(t *testing.T, name string) *sqlite.DB {
	t.Helper()

	os.Remove(name + "-shm")
	os.Remove(name + "-wal")
	os.Remove(name)

	db := sqlite.Default(name, migrations)

	t.Cleanup(func() {
		db.Close()

		os.Remove(name + "-shm")
		os.Remove(name + "-wal")
		os.Remove(name)
	})

	return db
}

type test struct {
	name string
	db   *sqlite.DB
	fn   func(t *testing.T, db *sqlite.DB)
}

func (ts test) execute(t *testing.T) {
	ts.fn(t, ts.db)
}

package sqlite_test

import (
	"embed"
	"os"
	"testing"

	"github.com/kasperbrandtpedersen/sqlite"
)

//go:embed migrations/*.sql
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

func insertUsers(t *testing.T, db *sqlite.DB) {
	// TODO insert 5 users
	// use db.Begin to get Tx and then Tx.Exec with prepared statement
}

func selectUsers(t *testing.T, db *sqlite.DB) {
	// TODO select users and check num of rows returned
	// use db.Query
}

func selectUser(t *testing.T, db *sqlite.DB) {
	// TODO select a single user and check result
	// use db.QueryRow
}

func deleteUsers(t *testing.T, db *sqlite.DB) {
	// TODO delete all users and check num of rows affected
	// use db.Exec
}

func TestUser(t *testing.T) {
	db := setup(t, "test_user.db")
	tests := []test{
		{
			name: "insert users",
			db:   db,
			fn:   insertUsers,
		},
		{
			name: "select users",
			db:   db,
			fn:   selectUsers,
		},
		{
			name: "select user",
			db:   db,
			fn:   selectUser,
		},
		{
			name: "delete users",
			db:   db,
			fn:   deleteUsers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.execute(t)
		})
	}
}

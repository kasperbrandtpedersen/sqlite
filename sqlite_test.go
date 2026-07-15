package sqlite_test

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/kasperbrandtpedersen/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// setup creates a clean DB file, opens it with migrations, and registers t.Cleanup to remove it.
func setup(t *testing.T, name string) *sqlite.DB {
	t.Helper()

	os.Remove(name + "-shm")
	os.Remove(name + "-wal")
	os.Remove(name)

	db := sqlite.OpenWAL(name, migrations)

	t.Cleanup(func() {
		db.Close()

		os.Remove(name + "-shm")
		os.Remove(name + "-wal")
		os.Remove(name)
	})

	return db
}

// test is a named sub-test with a shared DB and an assertion function.
type test struct {
	name string
	db   *sqlite.DB
	fn   func(t *testing.T, db *sqlite.DB)
}

func (ts test) execute(t *testing.T) {
	ts.fn(t, ts.db)
}

// insertUsers inserts 5 users via a prepared statement inside a transaction.
func insertUsers(t *testing.T, db *sqlite.DB) {
	ctx := t.Context()
	tx, err := db.Begin(ctx)

	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT INTO users (name, age) VALUES (?, ?)")

	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	for i := 1; i <= 5; i++ {
		if _, err := stmt.ExecContext(ctx, fmt.Sprintf("user%d", i), i*10); err != nil {
			t.Fatalf("insert user%d: %v", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// selectUsers asserts that exactly 5 rows are returned from the users table.
func selectUsers(t *testing.T, db *sqlite.DB) {
	rows, err := db.Query(t.Context(), "SELECT id, name, age FROM users")

	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	count := 0

	for rows.Next() {
		count++
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if count != 5 {
		t.Errorf("expected 5 rows, got %d", count)
	}
}

// selectUser asserts that a single user can be fetched by name with correct field values.
func selectUser(t *testing.T, db *sqlite.DB) {
	var name string
	var age int

	err := db.QueryRow(t.Context(), "SELECT name, age FROM users WHERE name = ?", "user1").Scan(&name, &age)

	if err != nil {
		t.Fatalf("query row: %v", err)
	}

	if name != "user1" {
		t.Errorf("expected name %q, got %q", "user1", name)
	}

	if age != 10 {
		t.Errorf("expected age 10, got %d", age)
	}
}

// deleteUsers deletes all users and asserts 5 rows were affected.
func deleteUsers(t *testing.T, db *sqlite.DB) {
	result, err := db.Exec(t.Context(), "DELETE FROM users")

	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	affected, err := result.RowsAffected()

	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}

	if affected != 5 {
		t.Errorf("expected 5 rows affected, got %d", affected)
	}
}

func TestRaise(t *testing.T) {
	sentinel := fmt.Errorf("underlying error")
	err := sqlite.Raise("myarea", "myaction", "something failed with %q", sentinel, "param1")

	var sqlErr *sqlite.Error

	if !errors.As(err, &sqlErr) {
		t.Fatal("errors.As: expected *sqlite.Error")
	}

	if sqlErr.Area != "myarea" {
		t.Errorf("Area: got %q, want %q", sqlErr.Area, "myarea")
	}

	if sqlErr.Action != "myaction" {
		t.Errorf("Action: got %q, want %q", sqlErr.Action, "myaction")
	}

	if sqlErr.Err != sentinel {
		t.Errorf("Err: got %v, want %v", sqlErr.Err, sentinel)
	}

	if !errors.Is(err, sentinel) {
		t.Error("errors.Is: expected to find sentinel via Unwrap")
	}

	want := `sqlite: myarea: myaction: something failed with "param1": underlying error`

	if err.Error() != want {
		t.Errorf("Error():\n got  %q\n want %q", err.Error(), want)
	}
}

func TestOpenMemory(t *testing.T) {
	db := sqlite.OpenMemory(migrations)
	t.Cleanup(func() { db.Close() })

	tests := []test{
		{name: "insert users", db: db, fn: insertUsers},
		{name: "select users", db: db, fn: selectUsers},
		{name: "select user", db: db, fn: selectUser},
		{name: "delete users", db: db, fn: deleteUsers},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.execute(t)
		})
	}
}

func TestOpenWAL(t *testing.T) {
	db := setup(t, "test_users.db")
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

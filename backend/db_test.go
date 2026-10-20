package handler

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenDBPragmasOnEveryConnection(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	// Hold the first connection so the second comes fresh from the pool.
	first, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var fk, timeout int
	if err := second.QueryRowContext(ctx, "SELECT * FROM pragma_foreign_keys, pragma_busy_timeout").Scan(&fk, &timeout); err != nil {
		t.Fatal(err)
	}
	if fk != 1 || timeout != 5000 {
		t.Errorf("foreign_keys=%d busy_timeout=%d, want 1 and 5000", fk, timeout)
	}
}

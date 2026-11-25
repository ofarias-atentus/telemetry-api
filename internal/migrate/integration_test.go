//go:build integration

package migrate

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Requiere una base VACÍA y desechable en TEST_MIGRATE_DATABASE_URL (sin
// parámetros pool_*): aplica todas las migraciones y no limpia al terminar.
func TestMigrateIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MIGRATE_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_MIGRATE_DATABASE_URL no está definida")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if _, err := Status(ctx, db); !errors.Is(err, ErrNotVersioned) {
		t.Fatalf("Status on empty database: err = %v, want ErrNotVersioned (la base debe estar vacía)", err)
	}

	applied, err := Up(ctx, db)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(applied) != int(LatestVersion) {
		t.Errorf("applied = %v, want %d versions", applied, LatestVersion)
	}

	got, err := Status(ctx, db)
	if err != nil || got != LatestVersion {
		t.Fatalf("Status = (%d, %v), want (%d, nil)", got, err, LatestVersion)
	}

	again, err := Up(ctx, db)
	if err != nil || len(again) != 0 {
		t.Errorf("second Up = (%v, %v), want no-op", again, err)
	}

	if _, err := Baseline(ctx, db); !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("Baseline on versioned database: err = %v, want ErrVersionMismatch", err)
	}
}

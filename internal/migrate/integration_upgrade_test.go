//go:build integration

package migrate

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Requiere una base desechable en TEST_MIGRATE_DATABASE_URL (sin parámetros
// pool_*). Borra y recrea el esquema public antes de empezar. Parte del
// esquema original (versión 1) con datos sucios y lo lleva hasta la última versión.
func TestMigrateUpgradeLegacyDataIntegration(t *testing.T) {
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

	for _, stmt := range []string{"DROP SCHEMA public CASCADE", "CREATE SCHEMA public"} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	provider, err := newProvider(db)
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatalf("UpTo(1): %v", err)
	}

	seed := []string{
		`INSERT INTO devices (device_id, alias_name, platform, model, os_version, host_pc, host_ip, host_location, country, battery_design_cap, updated_at) VALUES
			('d1', NULL, 'ios', 'iPhone 12,1', '17.4', 'pc-1', '10.0.0.1', 'Rack-A', 'chile', 3000, '2024-01-01T00:00:00Z'),
			('d2', 'tablet', 'android', NULL, NULL, 'pc-1', '10.0.0.2', '', ' Perú ', NULL, '2024-02-01T00:00:00Z'),
			('d3', NULL, 'android', NULL, NULL, 'pc-2', '', NULL, 'US', 0, NULL),
			('d-bad', NULL, 'ios', NULL, NULL, 'pc-3', '10.0.0.3', NULL, 'Atlantis', NULL, '2024-03-01T00:00:00Z')`,
		`INSERT INTO devices (device_id, platform, host_pc, host_ip, host_location, country, updated_at) VALUES
			('d4-old', 'ios', 'pc-4', '10.0.0.4', 'Rack-Old', 'US', '2024-01-01T00:00:00Z'),
			('d4-new', 'ios', 'pc-4', '10.0.0.5', 'Rack-New', 'US', '2024-02-01T00:00:00Z'),
			('d4-null', 'ios', 'pc-4', '10.0.0.6', 'Rack-Null', 'US', NULL)`,
		`INSERT INTO device_telemetry (time, device_id, battery_pct, temp_celsius, cycle_count, is_charging) VALUES
			('2024-01-01T00:00:00Z', 'd1', 55.5, 'NaN', 10, true),
			('2024-01-02T00:00:00Z', 'd2', NULL, NULL, NULL, NULL)`,
	}
	for _, stmt := range seed {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	_, err = Up(ctx, db)
	if err == nil {
		t.Fatal("Up with an unmappable country: err = nil, want an error")
	}
	if !strings.Contains(err.Error(), "Atlantis") || !strings.Contains(err.Error(), "1 filas") {
		t.Errorf("err = %v, want it to list the value and its row count", err)
	}
	if got, err := Status(ctx, db); err != nil || got != 3 {
		t.Fatalf("Status after failed Up = (%d, %v), want (3, nil)", got, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE devices SET country = 'Argentina' WHERE device_id = 'd-bad'`); err != nil {
		t.Fatalf("fix country: %v", err)
	}
	if _, err := Up(ctx, db); err != nil {
		t.Fatalf("Up after fix: %v", err)
	}
	if got, err := Status(ctx, db); err != nil || got != LatestVersion {
		t.Fatalf("Status = (%d, %v), want (%d, nil)", got, err, LatestVersion)
	}

	var hosts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM hosts`).Scan(&hosts); err != nil || hosts != 4 {
		t.Errorf("hosts = (%d, %v), want 4", hosts, err)
	}

	var ip string
	var loc *string
	err = db.QueryRowContext(ctx, `SELECT host_ip, host_location FROM hosts WHERE host_pc = 'pc-1'`).Scan(&ip, &loc)
	if err != nil || ip != "10.0.0.2" || loc == nil || *loc != "Rack-A" {
		t.Errorf("pc-1 = (%q, %v, %v), want latest device ip and latest non-empty location Rack-A", ip, loc, err)
	}

	loc = nil
	err = db.QueryRowContext(ctx, `SELECT host_ip, host_location FROM hosts WHERE host_pc = 'pc-2'`).Scan(&ip, &loc)
	if err != nil || ip != "" || loc != nil {
		t.Errorf("pc-2 = (%q, %v, %v), want empty ip and NULL location", ip, loc, err)
	}

	loc = nil
	err = db.QueryRowContext(ctx, `SELECT host_ip, host_location FROM hosts WHERE host_pc = 'pc-4'`).Scan(&ip, &loc)
	if err != nil || ip != "10.0.0.5" || loc == nil || *loc != "Rack-New" {
		t.Errorf("pc-4 = (%q, %v, %v), want the latest dated device (NULL updated_at sorts last): ip 10.0.0.5, location Rack-New", ip, loc, err)
	}

	var attrs string
	err = db.QueryRowContext(ctx, `SELECT attributes::text FROM devices WHERE device_id = 'd1'`).Scan(&attrs)
	if err != nil || strings.Contains(attrs, "alias_name") || !strings.Contains(attrs, `"battery_design_cap": 3000`) {
		t.Errorf("d1 attributes = (%q, %v)", attrs, err)
	}

	countries := map[string]string{"d1": "CL", "d2": "PE", "d3": "US", "d-bad": "AR"}
	for id, want := range countries {
		var got string
		if err := db.QueryRowContext(ctx, `SELECT country FROM devices WHERE device_id = $1`, id).Scan(&got); err != nil || got != want {
			t.Errorf("country of %s = (%q, %v), want %q", id, got, err, want)
		}
	}

	var data string
	err = db.QueryRowContext(ctx, `SELECT data::text FROM device_telemetry WHERE device_id = 'd1'`).Scan(&data)
	if err != nil || !strings.Contains(data, `"battery_pct": 55.50`) || !strings.Contains(data, `"is_charging": true`) {
		t.Errorf("d1 telemetry data = (%q, %v)", data, err)
	}
	err = db.QueryRowContext(ctx, `SELECT data::text FROM device_telemetry WHERE device_id = 'd2'`).Scan(&data)
	if err != nil || data != "{}" {
		t.Errorf("d2 telemetry data = (%q, %v), want {}", data, err)
	}
}

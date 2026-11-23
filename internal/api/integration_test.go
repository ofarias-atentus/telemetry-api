//go:build integration

package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationIngest(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no está definida")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	deviceID := fmt.Sprintf("itest-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM device_telemetry WHERE device_id = $1", deviceID); err != nil {
			t.Errorf("cleanup telemetry: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM devices WHERE device_id = $1", deviceID); err != nil {
			t.Errorf("cleanup device: %v", err)
		}
	})

	ts := httptest.NewServer(NewServer(pool).Routes())
	defer ts.Close()

	post := func(path, body string) (int, string) {
		t.Helper()
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading response of POST %s: %v", path, err)
		}
		return resp.StatusCode, string(got)
	}

	deviceBody := func(alias, platform, status string) string {
		return fmt.Sprintf(
			`[{"device_id":%q,"alias_name":%q,"platform":%q,"model":"m","os_version":"17",`+
				`"host_name":"pc","host_ip":"10.0.0.1","host_location":"loc","country":"CL",`+
				`"battery_design_cap":4000,"status":%q}]`,
			deviceID, alias, platform, status,
		)
	}

	tests := []struct {
		name     string
		path     string
		body     string
		wantCode int
		wantBody string
	}{
		{
			name:     "device insert",
			path:     "/api/v1/devices",
			body:     deviceBody("alias-1", "ios", "online"),
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name:     "device upsert",
			path:     "/api/v1/devices",
			body:     deviceBody("alias-2", "android", "offline"),
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name: "telemetry insert",
			path: "/api/v1/telemetry",
			body: fmt.Sprintf(
				`[{"time":"2024-05-01T10:00:00Z","device_id":%q,"battery_pct":80.5,"battery_health_pct":95,`+
					`"temp_celsius":30.2,"voltage_v":4.1,"cycle_count":120,"is_charging":true,`+
					`"cpu_usage":12.5,"ram_usage":40.5}]`,
				deviceID,
			),
			wantCode: http.StatusCreated,
			wantBody: `{"status":"created", "records": 1}`,
		},
	}

	for _, tt := range tests {
		code, body := post(tt.path, tt.body)
		if code != tt.wantCode {
			t.Fatalf("%s: status = %d, want %d (body %q)", tt.name, code, tt.wantCode, body)
		}
		if body != tt.wantBody {
			t.Fatalf("%s: body = %q, want %q", tt.name, body, tt.wantBody)
		}
	}

	var (
		alias, platform, status, hostPC, country string
		batteryDesignCap                         int
	)
	err = pool.QueryRow(
		ctx,
		"SELECT alias_name, platform, status, host_pc, country, battery_design_cap FROM devices WHERE device_id = $1",
		deviceID,
	).Scan(&alias, &platform, &status, &hostPC, &country, &batteryDesignCap)
	if err != nil {
		t.Fatalf("query device: %v", err)
	}

	deviceChecks := []struct {
		name string
		got  any
		want any
	}{
		{name: "alias_name updated", got: alias, want: "alias-2"},
		{name: "status updated", got: status, want: "offline"},
		{name: "platform kept", got: platform, want: "ios"},
		{name: "host_pc from host_name", got: hostPC, want: "pc"},
		{name: "country", got: country, want: "CL"},
		{name: "battery_design_cap", got: batteryDesignCap, want: 4000},
	}
	for _, c := range deviceChecks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}

	var telemetry int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM device_telemetry WHERE device_id = $1",
		deviceID,
	).Scan(&telemetry); err != nil {
		t.Fatalf("count telemetry: %v", err)
	}
	if telemetry != 1 {
		t.Errorf("telemetry rows = %d, want 1", telemetry)
	}
}

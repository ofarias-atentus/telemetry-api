//go:build integration

package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Supone que el esquema ya está aplicado con `atdevice-api migrate up`; crea y
// borra sus propias filas itest-*.
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

	nano := time.Now().UnixNano()
	deviceID := fmt.Sprintf("itest-%d", nano)
	secondID := deviceID + "-second"
	legacyID := deviceID + "-legacy"
	hostPC := fmt.Sprintf("itest-host-%d", nano)
	legacyHost := hostPC + "-legacy"
	narniaHost := hostPC + "-narnia"

	countryCases := []struct {
		id      string
		country string
		want    string
	}{
		{deviceID + "-country-1", "Chile", "CL"},
		{deviceID + "-country-2", " cl ", "CL"},
		{deviceID + "-country-3", "CL", "CL"},
		{deviceID + "-country-4", "Narnia", ""},
		{deviceID + "-country-5", "Perú", "PE"},
		{deviceID + "-country-6", "peru", "PE"},
		{deviceID + "-country-7", "MÉXICO", "MX"},
		{deviceID + "-country-8", "PERÚ", "PE"},
	}
	deviceIDs := []string{deviceID, secondID, legacyID}
	for _, c := range countryCases {
		deviceIDs = append(deviceIDs, c.id)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM device_telemetry WHERE device_id = $1", deviceID); err != nil {
			t.Errorf("cleanup telemetry: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM devices WHERE device_id = ANY($1)", deviceIDs); err != nil {
			t.Errorf("cleanup device: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM hosts WHERE host_pc = ANY($1)", []string{hostPC, legacyHost, narniaHost}); err != nil {
			t.Errorf("cleanup hosts: %v", err)
		}
	})

	ts := httptest.NewServer(NewServer(pool, nil).Routes())
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

	deviceBody := func(id, platform, status, hostIP, location, attributes string) string {
		return fmt.Sprintf(
			`[{"device_id":%q,"platform":%q,"host_name":%q,"host_ip":%q,`+
				`"country":"CL","status":%q%s,"attributes":%s}]`,
			id, platform, hostPC, hostIP, status, location, attributes,
		)
	}
	legacyBody := fmt.Sprintf(
		`[{"device_id":%q,"alias_name":"legacy-alias","platform":"android","model":"lm","os_version":"14",`+
			`"host_name":%q,"host_ip":"10.0.0.2","host_location":"lab","country":"CL",`+
			`"battery_design_cap":3000,"status":"online"}]`,
		legacyID, legacyHost,
	)

	var countryEntries []string
	for _, c := range countryCases {
		host := hostPC
		if c.want == "" {
			host = narniaHost
		}
		countryEntries = append(countryEntries, fmt.Sprintf(
			`{"device_id":%q,"platform":"ios","host_name":%q,"host_ip":"10.0.0.9","country":%q,"status":"online"}`,
			c.id, host, c.country,
		))
	}
	countryBody := "[" + strings.Join(countryEntries, ",") + "]"

	tests := []struct {
		name     string
		path     string
		body     string
		wantCode int
		wantBody string
	}{
		{
			name: "device insert",
			path: "/api/v1/devices",
			body: deviceBody(deviceID, "ios", "online", "10.0.0.1", `,"host_location":"lab-1"`,
				`{"alias_name":"alias-1","model":"m1","battery_design_cap":4000,"extra":"x"}`),
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name: "device upsert without host_location",
			path: "/api/v1/devices",
			body: deviceBody(deviceID, "android", "offline", "10.0.0.9", ``,
				`{"alias_name":"alias-2","model":"m2","battery_design_cap":5000,"sim":"y"}`),
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name:     "second device on the same host",
			path:     "/api/v1/devices",
			body:     deviceBody(secondID, "ios", "online", "10.0.0.9", ``, `{}`),
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name:     "devices with country names and an unknown one",
			path:     "/api/v1/devices",
			body:     countryBody,
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name:     "legacy device insert",
			path:     "/api/v1/devices",
			body:     legacyBody,
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name: "telemetry insert legacy",
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
		{
			name: "telemetry insert data",
			path: "/api/v1/telemetry",
			body: fmt.Sprintf(
				`[{"time":"2024-05-02T10:00:00Z","device_id":%q,`+
					`"data":{"battery_pct":70,"storage_free_gb":12.5}}]`,
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
		platform, status, devHostPC, country string
		attributes                           map[string]any
	)
	err = pool.QueryRow(
		ctx,
		"SELECT platform, status, host_pc, country, attributes FROM devices WHERE device_id = $1",
		deviceID,
	).Scan(&platform, &status, &devHostPC, &country, &attributes)
	if err != nil {
		t.Fatalf("query device: %v", err)
	}

	var hostIP string
	var hostLocation *string
	if err := pool.QueryRow(
		ctx,
		"SELECT host_ip, host_location FROM hosts WHERE host_pc = $1",
		hostPC,
	).Scan(&hostIP, &hostLocation); err != nil {
		t.Fatalf("query host: %v", err)
	}
	if hostIP != "10.0.0.9" {
		t.Errorf("host_ip = %q, want %q", hostIP, "10.0.0.9")
	}
	if hostLocation == nil || *hostLocation != "lab-1" {
		t.Errorf("host_location = %v, want lab-1 kept", hostLocation)
	}

	var hostRows int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM hosts WHERE host_pc = ANY($1)",
		[]string{hostPC, legacyHost, narniaHost},
	).Scan(&hostRows); err != nil {
		t.Fatalf("count hosts: %v", err)
	}
	if hostRows != 2 {
		t.Errorf("host rows = %d, want 2 (no host for the unknown country)", hostRows)
	}

	var sameHostDevices int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM devices WHERE host_pc = $1",
		hostPC,
	).Scan(&sameHostDevices); err != nil {
		t.Fatalf("count devices of host: %v", err)
	}
	if wantOnHost := 2 + len(countryCases) - 1; sameHostDevices != wantOnHost {
		t.Errorf("devices on host = %d, want %d", sameHostDevices, wantOnHost)
	}

	storedCountries := map[string]string{}
	rows, err := pool.Query(
		ctx,
		"SELECT device_id, country FROM devices WHERE device_id = ANY($1)",
		deviceIDs,
	)
	if err != nil {
		t.Fatalf("query countries: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, code string
		if err := rows.Scan(&id, &code); err != nil {
			t.Fatalf("scan country: %v", err)
		}
		storedCountries[id] = code
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate countries: %v", err)
	}
	for _, c := range countryCases {
		got, stored := storedCountries[c.id]
		if c.want == "" {
			if stored {
				t.Errorf("%q: stored with country %q, want not stored", c.country, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%q: country = %q, want %q", c.country, got, c.want)
		}
	}

	deviceChecks := []struct {
		name string
		got  any
		want any
	}{
		{name: "status updated", got: status, want: "offline"},
		{name: "platform kept", got: platform, want: "ios"},
		{name: "host_pc from host_name", got: devHostPC, want: hostPC},
		{name: "country", got: country, want: "CL"},
		{name: "alias_name updated", got: attributes["alias_name"], want: "alias-2"},
		{name: "model kept", got: attributes["model"], want: "m1"},
		{name: "battery_design_cap kept", got: attributes["battery_design_cap"], want: 4000.0},
		{name: "extra kept", got: attributes["extra"], want: "x"},
		{name: "sim added", got: attributes["sim"], want: "y"},
	}
	for _, c := range deviceChecks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}

	var legacyAttrs map[string]any
	if err := pool.QueryRow(
		ctx,
		"SELECT attributes FROM devices WHERE device_id = $1",
		legacyID,
	).Scan(&legacyAttrs); err != nil {
		t.Fatalf("query legacy device: %v", err)
	}
	wantLegacy := map[string]any{
		"alias_name": "legacy-alias", "model": "lm", "os_version": "14", "battery_design_cap": 3000.0,
	}
	if !reflect.DeepEqual(legacyAttrs, wantLegacy) {
		t.Errorf("legacy attributes = %#v, want %#v", legacyAttrs, wantLegacy)
	}

	var telemetry int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM device_telemetry WHERE device_id = $1",
		deviceID,
	).Scan(&telemetry); err != nil {
		t.Fatalf("count telemetry: %v", err)
	}
	if telemetry != 2 {
		t.Errorf("telemetry rows = %d, want 2", telemetry)
	}

	dataByTime := []struct {
		name string
		time string
		want map[string]any
	}{
		{
			name: "legacy record",
			time: "2024-05-01T10:00:00Z",
			want: map[string]any{
				"battery_pct":        80.5,
				"battery_health_pct": 95.0,
				"temp_celsius":       30.2,
				"voltage_v":          4.1,
				"cycle_count":        120.0,
				"is_charging":        true,
				"cpu_usage":          12.5,
				"ram_usage":          40.5,
			},
		},
		{
			name: "data record",
			time: "2024-05-02T10:00:00Z",
			want: map[string]any{"battery_pct": 70.0, "storage_free_gb": 12.5},
		},
	}
	for _, c := range dataByTime {
		var data map[string]any
		if err := pool.QueryRow(
			ctx,
			"SELECT data FROM device_telemetry WHERE device_id = $1 AND time = $2::timestamptz",
			deviceID, c.time,
		).Scan(&data); err != nil {
			t.Fatalf("%s: query data: %v", c.name, err)
		}
		if !reflect.DeepEqual(data, c.want) {
			t.Errorf("%s: data = %#v, want %#v", c.name, data, c.want)
		}
	}
}

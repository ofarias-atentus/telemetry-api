package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestHandleTelemetry(t *testing.T) {
	t.Parallel()

	const twoRows = `[
		{"time":"2024-05-01T10:00:00Z","device_id":"d1","battery_pct":80.5,"battery_health_pct":95,
		 "temp_celsius":30.2,"voltage_v":4.1,"cycle_count":120,"is_charging":true,
		 "cpu_usage":12.5,"ram_usage":40.5},
		{"device_id":"d2","battery_pct":50,"battery_health_pct":90,
		 "temp_celsius":28,"voltage_v":3.9,"cycle_count":10,"is_charging":false,
		 "cpu_usage":5,"ram_usage":20}
	]`

	tests := []struct {
		name       string
		method     string
		body       string
		copyN      int64
		copyErr    error
		wantCode   int
		wantBody   string
		skipBody   bool
		wantCopies int
	}{
		{
			name:     "get is not allowed",
			method:   http.MethodGet,
			wantCode: http.StatusMethodNotAllowed,
			wantBody: "Método no permitido\n",
		},
		{
			name:     "empty body",
			method:   http.MethodPost,
			body:     "",
			wantCode: http.StatusBadRequest,
			wantBody: "EOF\n",
		},
		{
			name:     "malformed json",
			method:   http.MethodPost,
			body:     "{not json",
			wantCode: http.StatusBadRequest,
			skipBody: true,
		},
		{
			name:     "empty array",
			method:   http.MethodPost,
			body:     "[]",
			wantCode: http.StatusOK,
			wantBody: "",
		},
		{
			name:     "null",
			method:   http.MethodPost,
			body:     "null",
			wantCode: http.StatusOK,
			wantBody: "",
		},
		{
			name:       "success",
			method:     http.MethodPost,
			body:       twoRows,
			copyN:      2,
			wantCode:   http.StatusCreated,
			wantBody:   `{"status":"created", "records": 2}`,
			wantCopies: 1,
		},
		{
			name:       "copy error",
			method:     http.MethodPost,
			body:       twoRows,
			copyErr:    errors.New("boom"),
			wantCode:   http.StatusInternalServerError,
			wantBody:   "Error guardando datos\n",
			wantCopies: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDB{copyN: tt.copyN, copyErr: tt.copyErr}

			req := httptest.NewRequest(tt.method, "/api/v1/telemetry", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			NewServer(db, nil).Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if !tt.skipBody && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if len(db.copyCalls) != tt.wantCopies {
				t.Errorf("CopyFrom calls = %d, want %d", len(db.copyCalls), tt.wantCopies)
			}
			if len(db.execCalls) != 0 {
				t.Errorf("Exec calls = %d, want 0", len(db.execCalls))
			}
		})
	}
}

func TestHandleTelemetryCopyArgs(t *testing.T) {
	t.Parallel()

	const body = `[
		{"time":"2024-05-01T10:00:00Z","device_id":"d1","battery_pct":80.5,"battery_health_pct":95,
		 "temp_celsius":30.2,"voltage_v":4.1,"cycle_count":120,"is_charging":true,
		 "cpu_usage":12.5,"ram_usage":40.5},
		{"device_id":"d2","battery_pct":50,"battery_health_pct":90,
		 "temp_celsius":28,"voltage_v":3.9,"cycle_count":10,"is_charging":false,
		 "cpu_usage":5,"ram_usage":20}
	]`

	wantColumns := []string{"time", "device_id", "data"}
	setTime := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

	db := &fakeDB{copyN: 2}
	before := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(body))
	NewServer(db, nil).Routes().ServeHTTP(httptest.NewRecorder(), req)

	if len(db.copyCalls) != 1 {
		t.Fatalf("CopyFrom calls = %d, want 1", len(db.copyCalls))
	}
	call := db.copyCalls[0]

	if !reflect.DeepEqual(call.table, pgx.Identifier{"device_telemetry"}) {
		t.Errorf("table = %v, want device_telemetry", call.table)
	}
	if !reflect.DeepEqual(call.columns, wantColumns) {
		t.Errorf("columns = %v, want %v", call.columns, wantColumns)
	}
	if len(call.rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(call.rows))
	}

	first := call.rows[0]
	if got := first[0].(time.Time); !got.Equal(setTime) {
		t.Errorf("row 0 time = %v, want %v", got, setTime)
	}
	wantFirst := []any{"d1", map[string]any{
		"battery_pct":        80.5,
		"battery_health_pct": 95.0,
		"temp_celsius":       30.2,
		"voltage_v":          4.1,
		"cycle_count":        120,
		"is_charging":        true,
		"cpu_usage":          12.5,
		"ram_usage":          40.5,
	}}
	if !reflect.DeepEqual(first[1:], wantFirst) {
		t.Errorf("row 0 values = %#v, want %#v", first[1:], wantFirst)
	}

	second := call.rows[1]
	got := second[0].(time.Time)
	if got.IsZero() || got.Before(before) {
		t.Errorf("row 1 time = %v, want a time at or after %v", got, before)
	}
	wantSecond := []any{"d2", map[string]any{
		"battery_pct":        50.0,
		"battery_health_pct": 90.0,
		"temp_celsius":       28.0,
		"voltage_v":          3.9,
		"cycle_count":        10,
		"is_charging":        false,
		"cpu_usage":          5.0,
		"ram_usage":          20.0,
	}}
	if !reflect.DeepEqual(second[1:], wantSecond) {
		t.Errorf("row 1 values = %#v, want %#v", second[1:], wantSecond)
	}
}

func TestHandleTelemetryDataFormat(t *testing.T) {
	t.Parallel()

	const body = `[
		{"time":"2024-05-01T10:00:00Z","device_id":"d1",
		 "data":{"battery_pct":80,"storage_free_gb":12.5},"cpu_usage":12}
	]`

	db := &fakeDB{copyN: 1}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(body))
	NewServer(db, nil).Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if len(db.copyCalls) != 1 || len(db.copyCalls[0].rows) != 1 {
		t.Fatalf("copy calls = %#v, want one call with one row", db.copyCalls)
	}

	want := []any{"d1", map[string]any{
		"battery_pct":     80.0,
		"storage_free_gb": 12.5,
		"cpu_usage":       12.0,
	}}
	if got := db.copyCalls[0].rows[0][1:]; !reflect.DeepEqual(got, want) {
		t.Errorf("row values = %#v, want %#v", got, want)
	}
}

func TestHandleTelemetryPayloads(t *testing.T) {
	t.Parallel()

	setTime := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
	allZero := map[string]any{
		"battery_pct":        0.0,
		"battery_health_pct": 0.0,
		"temp_celsius":       0.0,
		"voltage_v":          0.0,
		"cycle_count":        0,
		"is_charging":        false,
		"cpu_usage":          0.0,
		"ram_usage":          0.0,
	}

	tests := []struct {
		name         string
		body         string
		wantCode     int
		wantDeviceID string
		wantData     map[string]any
	}{
		{
			name:         "null data behaves like legacy",
			body:         `[{"time":"2024-05-01T10:00:00Z","device_id":"d1","data":null}]`,
			wantCode:     http.StatusCreated,
			wantDeviceID: "d1",
			wantData:     allZero,
		},
		{
			name:         "empty data plus legacy field",
			body:         `[{"time":"2024-05-01T10:00:00Z","device_id":"d1","data":{},"cpu_usage":12}]`,
			wantCode:     http.StatusCreated,
			wantDeviceID: "d1",
			wantData:     map[string]any{"cpu_usage": 12.0},
		},
		{
			name: "time and device_id inside data stay in data",
			body: `[{"time":"2024-05-01T10:00:00Z","device_id":"d1",` +
				`"data":{"time":"2000-01-01T00:00:00Z","device_id":"other"}}]`,
			wantCode:     http.StatusCreated,
			wantDeviceID: "d1",
			wantData:     map[string]any{"time": "2000-01-01T00:00:00Z", "device_id": "other"},
		},
		{
			name: "nested values pass through",
			body: `[{"time":"2024-05-01T10:00:00Z","device_id":"d1",` +
				`"data":{"disk":{"free_gb":12.5,"parts":["a","b"]},"tags":[1,"x",null]}}]`,
			wantCode:     http.StatusCreated,
			wantDeviceID: "d1",
			wantData: map[string]any{
				"disk": map[string]any{"free_gb": 12.5, "parts": []any{"a", "b"}},
				"tags": []any{1.0, "x", nil},
			},
		},
		{
			name:         "null legacy field is zero-filled in legacy mode",
			body:         `[{"time":"2024-05-01T10:00:00Z","device_id":"d1","battery_pct":null}]`,
			wantCode:     http.StatusCreated,
			wantDeviceID: "d1",
			wantData:     allZero,
		},
		{
			name:         "null legacy field is absent in data mode",
			body:         `[{"time":"2024-05-01T10:00:00Z","device_id":"d1","data":{"x":1},"battery_pct":null}]`,
			wantCode:     http.StatusCreated,
			wantDeviceID: "d1",
			wantData:     map[string]any{"x": 1.0},
		},
		{
			name:     "fractional cycle_count is rejected",
			body:     `[{"device_id":"d1","cycle_count":1.5}]`,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDB{copyN: 1}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(tt.body))
			NewServer(db, nil).Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode != http.StatusCreated {
				if len(db.copyCalls) != 0 {
					t.Errorf("CopyFrom calls = %d, want 0", len(db.copyCalls))
				}
				return
			}
			if len(db.copyCalls) != 1 || len(db.copyCalls[0].rows) != 1 {
				t.Fatalf("copy calls = %#v, want one call with one row", db.copyCalls)
			}

			row := db.copyCalls[0].rows[0]
			if got := row[0].(time.Time); !got.Equal(setTime) {
				t.Errorf("time = %v, want %v", got, setTime)
			}
			if row[1] != tt.wantDeviceID {
				t.Errorf("device_id = %v, want %v", row[1], tt.wantDeviceID)
			}
			if !reflect.DeepEqual(row[2], tt.wantData) {
				t.Errorf("data = %#v, want %#v", row[2], tt.wantData)
			}
		})
	}
}

func TestHandleTelemetryDataNotObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "number", body: `[{"device_id":"d1","data":5}]`},
		{name: "array", body: `[{"device_id":"d1","data":[1]}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDB{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(tt.body))
			NewServer(db, nil).Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if len(db.copyCalls) != 0 {
				t.Errorf("CopyFrom calls = %d, want 0", len(db.copyCalls))
			}
		})
	}
}

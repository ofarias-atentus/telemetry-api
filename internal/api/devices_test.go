package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const twoDevicesBody = `[
	{"device_id":"d1","alias_name":"a1","platform":"ios","model":"m1","os_version":"17",
	 "host_name":"pc1","host_ip":"10.0.0.1","host_location":"loc1","country":"CL",
	 "battery_design_cap":4000,"status":"active"},
	{"device_id":"d2","alias_name":"a2","platform":"android","model":"m2","os_version":"14",
	 "host_name":"pc2","host_ip":"10.0.0.2","host_location":"loc2","country":"AR",
	 "battery_design_cap":5000,"status":"inactive"}
]`

func TestHandleDevices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		method    string
		body      string
		execErr   map[string]error
		wantCode  int
		wantBody  string
		skipBody  bool
		wantExecs int
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
			wantBody: `{"status":"success"}`,
		},
		{
			name:     "null",
			method:   http.MethodPost,
			body:     "null",
			wantCode: http.StatusOK,
			wantBody: `{"status":"success"}`,
		},
		{
			name:      "two devices",
			method:    http.MethodPost,
			body:      twoDevicesBody,
			wantCode:  http.StatusOK,
			wantBody:  `{"status":"success"}`,
			wantExecs: 2,
		},
		{
			name:      "exec error on one row still succeeds",
			method:    http.MethodPost,
			body:      twoDevicesBody,
			execErr:   map[string]error{"d1": errors.New("boom")},
			wantCode:  http.StatusOK,
			wantBody:  `{"status":"success"}`,
			wantExecs: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDB{execErr: tt.execErr}

			req := httptest.NewRequest(tt.method, "/api/v1/devices", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			NewServer(db).Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if !tt.skipBody && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if len(db.execCalls) != tt.wantExecs {
				t.Errorf("Exec calls = %d, want %d", len(db.execCalls), tt.wantExecs)
			}
			if len(db.copyCalls) != 0 {
				t.Errorf("CopyFrom calls = %d, want 0", len(db.copyCalls))
			}
		})
	}
}

func TestHandleDevicesExecArgs(t *testing.T) {
	t.Parallel()

	want := [][]any{
		{"d1", "ios", "pc1", "10.0.0.1", ptr("loc1"), "CL", "active", map[string]any{
			"alias_name": "a1", "model": "m1", "os_version": "17", "battery_design_cap": 4000,
		}},
		{"d2", "android", "pc2", "10.0.0.2", ptr("loc2"), "AR", "inactive", map[string]any{
			"alias_name": "a2", "model": "m2", "os_version": "14", "battery_design_cap": 5000,
		}},
	}

	db := &fakeDB{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", strings.NewReader(twoDevicesBody))
	NewServer(db).Routes().ServeHTTP(httptest.NewRecorder(), req)

	if len(db.execCalls) != len(want) {
		t.Fatalf("Exec calls = %d, want %d", len(db.execCalls), len(want))
	}
	for i, call := range db.execCalls {
		if call.sql != upsertDeviceQuery {
			t.Errorf("call %d: sql = %q, want upsertDeviceQuery", i, call.sql)
		}
		if !reflect.DeepEqual(call.args, want[i]) {
			t.Errorf("call %d: args = %#v, want %#v", i, call.args, want[i])
		}
	}
}

func TestHandleDevicesHostLocationArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
		want     *string
	}{
		{name: "absent is NULL", location: ``, want: nil},
		{name: "null is NULL", location: `,"host_location":null`, want: nil},
		{name: "empty is NULL", location: `,"host_location":""`, want: nil},
		{name: "set value is passed through", location: `,"host_location":"lab"`, want: ptr("lab")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := `[{"device_id":"d1","platform":"ios","host_name":"pc","host_ip":"10.0.0.1",` +
				`"country":"CL","status":"online"` + tt.location + `}]`
			db := &fakeDB{}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", strings.NewReader(body))
			NewServer(db).Routes().ServeHTTP(httptest.NewRecorder(), req)

			if len(db.execCalls) != 1 {
				t.Fatalf("Exec calls = %d, want 1", len(db.execCalls))
			}
			got, ok := db.execCalls[0].args[4].(*string)
			if !ok {
				t.Fatalf("location arg = %#v, want *string", db.execCalls[0].args[4])
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("location arg = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpsertDeviceQueryText(t *testing.T) {
	t.Parallel()

	const setMarker = "DO UPDATE SET"

	hostPart, devicePart, found := strings.Cut(upsertDeviceQuery, "INSERT INTO devices")
	if !found {
		t.Fatal("query does not contain INSERT INTO devices")
	}
	devicePart = "INSERT INTO devices" + devicePart
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	listAfter := func(sql, marker string) []string {
		_, rest, _ := strings.Cut(sql, marker)
		return strings.FieldsFunc(
			rest[strings.Index(rest, "(")+1:strings.Index(rest, ")")],
			func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' },
		)
	}

	t.Run("hosts insert columns in order", func(t *testing.T) {
		t.Parallel()

		want := []string{"host_pc", "host_ip", "host_location", "updated_at"}
		if got := listAfter(hostPart, "INSERT INTO hosts"); !reflect.DeepEqual(got, want) {
			t.Errorf("hosts columns = %v, want %v", got, want)
		}
	})

	t.Run("devices insert columns in order", func(t *testing.T) {
		t.Parallel()

		want := []string{"device_id", "platform", "host_pc", "country", "status", "attributes", "updated_at"}
		if got := listAfter(devicePart, "INSERT INTO devices"); !reflect.DeepEqual(got, want) {
			t.Errorf("devices columns = %v, want %v", got, want)
		}
	})

	hostContains := []string{
		"WITH host AS (",
		"VALUES ($3, $4, $5, NOW())",
		"ON CONFLICT (host_pc) DO UPDATE SET",
		"host_ip = EXCLUDED.host_ip",
		"host_location = COALESCE(EXCLUDED.host_location, hosts.host_location)",
		"updated_at = NOW()",
	}
	for _, s := range hostContains {
		t.Run("hosts contains "+s, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(normalize(hostPart), s) {
				t.Errorf("hosts CTE does not contain %q", s)
			}
		})
	}

	deviceContains := []string{
		"VALUES ($1, $2, $3, $6, $7, $8, NOW())",
		"ON CONFLICT (device_id) DO UPDATE SET",
		"host_pc = EXCLUDED.host_pc",
		"country = EXCLUDED.country",
		"status = EXCLUDED.status",
		"attributes = devices.attributes || EXCLUDED.attributes " +
			"|| jsonb_strip_nulls(jsonb_build_object( " +
			"'model', devices.attributes->'model', " +
			"'battery_design_cap', devices.attributes->'battery_design_cap')),",
		"updated_at = NOW()",
	}
	for _, s := range deviceContains {
		t.Run("devices contains "+s, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(normalize(devicePart), s) {
				t.Errorf("devices upsert does not contain %q", s)
			}
		})
	}

	t.Run("devices set list in order", func(t *testing.T) {
		t.Parallel()

		order := []string{"host_pc = ", "country = ", "status = ", "attributes = ", "updated_at = "}
		_, set, _ := strings.Cut(normalize(devicePart), setMarker)
		pos := 0
		for _, o := range order {
			i := strings.Index(set[pos:], o)
			if i < 0 {
				t.Fatalf("SET list does not contain %q in order", o)
			}
			pos += i + len(o)
		}
	})

	t.Run("devices set clause excludes platform", func(t *testing.T) {
		t.Parallel()

		_, set, _ := strings.Cut(devicePart, setMarker)
		if strings.Contains(set, "platform") {
			t.Error("SET clause contains \"platform\"")
		}
	})
}

func TestHandleDevicesAttributesFormats(t *testing.T) {
	t.Parallel()

	allZero := map[string]any{
		"alias_name": "", "model": "", "os_version": "", "battery_design_cap": 0,
	}
	noLocation := (*string)(nil)

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantArgs []any
	}{
		{
			name: "new format reaches exec with the map",
			body: `[{"device_id":"d1","platform":"ios","host_name":"pc","host_ip":"10.0.0.1",` +
				`"country":"CL","status":"online","attributes":{"alias_name":"a","extra":{"k":[1]}}}]`,
			wantCode: http.StatusOK,
			wantArgs: []any{"d1", "ios", "pc", "10.0.0.1", noLocation, "CL", "online", map[string]any{
				"alias_name": "a",
				"extra":      map[string]any{"k": []any{1.0}},
			}},
		},
		{
			name: "null attributes behaves like legacy",
			body: `[{"device_id":"d1","platform":"ios","host_name":"pc","host_ip":"10.0.0.1",` +
				`"country":"CL","status":"online","attributes":null}]`,
			wantCode: http.StatusOK,
			wantArgs: []any{"d1", "ios", "pc", "10.0.0.1", noLocation, "CL", "online", allZero},
		},
		{
			name:     "attributes number is rejected",
			body:     `[{"device_id":"d1","attributes":5}]`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "attributes array is rejected",
			body:     `[{"device_id":"d1","attributes":[1]}]`,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDB{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", strings.NewReader(tt.body))
			NewServer(db).Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantArgs == nil {
				if len(db.execCalls) != 0 {
					t.Errorf("Exec calls = %d, want 0", len(db.execCalls))
				}
				return
			}
			if len(db.execCalls) != 1 {
				t.Fatalf("Exec calls = %d, want 1", len(db.execCalls))
			}
			if got := db.execCalls[0].args; !reflect.DeepEqual(got, tt.wantArgs) {
				t.Errorf("args = %#v, want %#v", got, tt.wantArgs)
			}
		})
	}
}

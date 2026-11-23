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
		{"d1", "a1", "ios", "m1", "17", "pc1", "10.0.0.1", "loc1", "CL", 4000, "active"},
		{"d2", "a2", "android", "m2", "14", "pc2", "10.0.0.2", "loc2", "AR", 5000, "inactive"},
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

func TestUpsertDeviceQueryText(t *testing.T) {
	t.Parallel()

	const setMarker = "DO UPDATE SET"

	wantContains := []string{
		"INSERT INTO devices (",
		"ON CONFLICT (device_id) DO UPDATE SET",
		"alias_name = EXCLUDED.alias_name",
		"os_version = EXCLUDED.os_version",
		"host_pc = EXCLUDED.host_pc",
		"host_ip = EXCLUDED.host_ip",
		"host_location = EXCLUDED.host_location",
		"country = EXCLUDED.country",
		"status = EXCLUDED.status",
		"updated_at = NOW()",
	}
	for _, s := range wantContains {
		t.Run("contains "+strings.Fields(s)[0], func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(upsertDeviceQuery, s) {
				t.Errorf("query does not contain %q", s)
			}
		})
	}

	t.Run("insert columns in order", func(t *testing.T) {
		t.Parallel()

		insertList, _, _ := strings.Cut(upsertDeviceQuery, "VALUES")
		columns := []string{
			"device_id",
			"alias_name",
			"platform",
			"model",
			"os_version",
			"host_pc",
			"host_ip",
			"host_location",
			"country",
			"battery_design_cap",
			"status",
			"updated_at",
		}
		got := strings.FieldsFunc(
			insertList[strings.Index(insertList, "(")+1:strings.LastIndex(insertList, ")")],
			func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' },
		)
		if !reflect.DeepEqual(got, columns) {
			t.Errorf("insert columns = %v, want %v", got, columns)
		}
	})

	_, setClause, found := strings.Cut(upsertDeviceQuery, setMarker)
	if !found {
		t.Fatalf("query does not contain %q", setMarker)
	}
	for _, col := range []string{"platform", "model", "battery_design_cap"} {
		t.Run("set clause excludes "+col, func(t *testing.T) {
			t.Parallel()

			if strings.Contains(setClause, col) {
				t.Errorf("SET clause contains %q", col)
			}
		})
	}
}

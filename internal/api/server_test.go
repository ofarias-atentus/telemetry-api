package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		wantCode int
	}{
		{name: "unknown path", path: "/api/v1/unknown", wantCode: http.StatusNotFound},
		{name: "root path", path: "/", wantCode: http.StatusNotFound},
		{name: "devices registered", path: "/api/v1/devices", wantCode: http.StatusMethodNotAllowed},
		{name: "telemetry registered", path: "/api/v1/telemetry", wantCode: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			NewServer(&fakeDB{}, nil).Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("GET %s status = %d, want %d", tt.path, rec.Code, tt.wantCode)
			}
		})
	}
}

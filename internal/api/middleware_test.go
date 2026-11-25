package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var hexID16 = regexp.MustCompile(`^[0-9a-f]{16}$`)

func newJSONLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("línea de log inválida %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func findRecord(recs []map[string]any, msg string) map[string]any {
	for _, r := range recs {
		if r["msg"] == msg {
			return r
		}
	}
	return nil
}

func TestAccessLogStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		db        *fakeDB
		wantCode  int
		wantLevel string
	}{
		{"default 200", http.MethodPost, "/api/v1/telemetry", `[]`, &fakeDB{}, http.StatusOK, "INFO"},
		{"201", http.MethodPost, "/api/v1/telemetry", `[{"device_id":"d1"}]`, &fakeDB{copyN: 1}, http.StatusCreated, "INFO"},
		{"405", http.MethodGet, "/api/v1/devices", ``, &fakeDB{}, http.StatusMethodNotAllowed, "WARN"},
		{"500", http.MethodPost, "/api/v1/telemetry", `[{"device_id":"d1"}]`, &fakeDB{copyErr: errors.New("boom")}, http.StatusInternalServerError, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			h := NewServer(tt.db, newJSONLogger(&buf)).Handler()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			entry := findRecord(logRecords(t, &buf), "solicitud")
			if entry == nil {
				t.Fatalf("no hay línea de acceso en %q", buf.String())
			}
			if got := int(entry["status"].(float64)); got != tt.wantCode {
				t.Errorf("status logueado = %d, want %d", got, tt.wantCode)
			}
			if entry["level"] != tt.wantLevel {
				t.Errorf("level = %v, want %s", entry["level"], tt.wantLevel)
			}
			if entry["method"] != tt.method || entry["path"] != tt.path {
				t.Errorf("method/path = %v %v", entry["method"], entry["path"])
			}
			if int(entry["bytes"].(float64)) != rec.Body.Len() {
				t.Errorf("bytes = %v, want %d", entry["bytes"], rec.Body.Len())
			}
			for _, k := range []string{"duration_ms", "remote_addr", "request_id"} {
				if _, ok := entry[k]; !ok {
					t.Errorf("falta el atributo %s", k)
				}
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		incoming string
		keep     bool
	}{
		{"none", "", false},
		{"valid kept", "abc-123_X.y", true},
		{"max length kept", strings.Repeat("a", 64), true},
		{"too long replaced", strings.Repeat("a", 65), false},
		{"bad chars replaced", "abc def", false},
		{"newline-like chars replaced", "abc/\"x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			h := NewServer(&fakeDB{}, newJSONLogger(&buf)).Handler()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(`[]`))
			if tt.incoming != "" {
				req.Header.Set("X-Request-ID", tt.incoming)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			got := rec.Header().Get("X-Request-ID")
			if tt.keep {
				if got != tt.incoming {
					t.Errorf("X-Request-ID = %q, want %q", got, tt.incoming)
				}
			} else if !hexID16.MatchString(got) {
				t.Errorf("X-Request-ID = %q, want 16 hex chars", got)
			}

			entry := findRecord(logRecords(t, &buf), "solicitud")
			if entry == nil || entry["request_id"] != got {
				t.Errorf("request_id logueado = %v, want %q", entry["request_id"], got)
			}
		})
	}
}

func TestRequestIDInHandlerErrorLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		body    string
		db      *fakeDB
		wantMsg string
	}{
		{
			name:    "device upsert",
			path:    "/api/v1/devices",
			body:    `[{"device_id":"d1","host_name":"h","country":"cl"}]`,
			db:      &fakeDB{execErr: map[string]error{"d1": errors.New("boom")}},
			wantMsg: "upsert falló",
		},
		{
			name:    "telemetry copy",
			path:    "/api/v1/telemetry",
			body:    `[{"device_id":"d1"}]`,
			db:      &fakeDB{copyErr: errors.New("boom")},
			wantMsg: "inserción masiva CopyFrom fallida",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			h := NewServer(tt.db, newJSONLogger(&buf)).Handler()
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("X-Request-ID", "req-42")
			h.ServeHTTP(httptest.NewRecorder(), req)

			entry := findRecord(logRecords(t, &buf), tt.wantMsg)
			if entry == nil {
				t.Fatalf("no hay registro %q en %q", tt.wantMsg, buf.String())
			}
			if entry["request_id"] != "req-42" {
				t.Errorf("request_id = %v, want req-42", entry["request_id"])
			}
			if entry["level"] != "ERROR" {
				t.Errorf("level = %v, want ERROR", entry["level"])
			}
		})
	}
}

func TestStatusWriterUnwrap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}
	if sw.Unwrap() != http.ResponseWriter(rec) {
		t.Error("Unwrap no devuelve el ResponseWriter original")
	}
}

func TestStatusWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		do          func(w http.ResponseWriter)
		wantStatus  int
		wantBytes   int
		wantFlushed bool
	}{
		{"write without header", func(w http.ResponseWriter) { _, _ = w.Write([]byte("ab")) }, 200, 2, false},
		{"double WriteHeader keeps first", func(w http.ResponseWriter) {
			w.WriteHeader(201)
			w.WriteHeader(500)
		}, 201, 0, false},
		{"WriteHeader after Write ignored", func(w http.ResponseWriter) {
			_, _ = w.Write([]byte("abc"))
			w.WriteHeader(500)
		}, 200, 3, false},
		{"informational then final", func(w http.ResponseWriter) {
			w.WriteHeader(103)
			w.WriteHeader(201)
		}, 201, 0, false},
		{"bytes across writes", func(w http.ResponseWriter) {
			_, _ = w.Write([]byte("ab"))
			_, _ = w.Write([]byte("cde"))
		}, 200, 5, false},
		{"flush via ResponseController", func(w http.ResponseWriter) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				panic(err)
			}
		}, 200, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}
			tt.do(sw)

			if sw.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", sw.status, tt.wantStatus)
			}
			if sw.bytes != tt.wantBytes {
				t.Errorf("bytes = %d, want %d", sw.bytes, tt.wantBytes)
			}
			if rec.Flushed != tt.wantFlushed {
				t.Errorf("Flushed = %v, want %v", rec.Flushed, tt.wantFlushed)
			}
		})
	}
}

func TestAccessLogPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     any
		wantPanic string
	}{
		{"panic value logged", "kaboom", "kaboom"},
		{"abort handler not logged", http.ErrAbortHandler, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			s := NewServer(&fakeDB{}, newJSONLogger(&buf))
			h := s.accessLog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic(tt.value)
			}))
			req := httptest.NewRequest(http.MethodGet, "/boom", nil)
			req.Header.Set("X-Request-ID", "req-panic")

			var got any
			func() {
				defer func() { got = recover() }()
				h.ServeHTTP(httptest.NewRecorder(), req)
			}()
			if got != tt.value {
				t.Fatalf("panic propagado = %v, want %v", got, tt.value)
			}

			entry := findRecord(logRecords(t, &buf), "solicitud")
			if entry == nil {
				t.Fatalf("no hay línea de acceso en %q", buf.String())
			}
			if entry["level"] != "ERROR" || int(entry["status"].(float64)) != 500 {
				t.Errorf("level/status = %v/%v, want ERROR/500", entry["level"], entry["status"])
			}
			if entry["request_id"] != "req-panic" {
				t.Errorf("request_id = %v", entry["request_id"])
			}
			p, ok := entry["panic"]
			if tt.wantPanic == "" {
				if ok {
					t.Errorf("panic attr = %v, want ausente", p)
				}
			} else if p != tt.wantPanic {
				t.Errorf("panic attr = %v, want %q", p, tt.wantPanic)
			}
		})
	}
}

func TestNewServerNilLoggerUsesDefault(t *testing.T) {
	t.Parallel()

	if NewServer(&fakeDB{}, nil).logger == nil {
		t.Error("logger nil, want slog.Default()")
	}
}

package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"telemetry-api/internal/config"
)

func TestNewJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&buf, config.LogConfig{Level: slog.LevelWarn, Format: "json"})
	logger.Info("omitido")
	logger.Warn("visible", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("salida no es JSON válido (%q): %v", buf.String(), err)
	}
	if rec["msg"] != "visible" || rec["level"] != "WARN" || rec["k"] != "v" {
		t.Errorf("registro inesperado: %v", rec)
	}
}

func TestNewText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&buf, config.LogConfig{Level: slog.LevelDebug, Format: "text"})
	logger.Debug("hola", "k", "v")

	out := buf.String()
	for _, part := range []string{"level=DEBUG", "msg=hola", "k=v"} {
		if !strings.Contains(out, part) {
			t.Errorf("salida %q no contiene %q", out, part)
		}
	}
}

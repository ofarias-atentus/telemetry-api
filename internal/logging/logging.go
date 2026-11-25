// Package logging construye el logger estructurado de la API.
package logging

import (
	"io"
	"log/slog"

	"telemetry-api/internal/config"
)

// New devuelve un logger que escribe en w con el nivel y formato de cfg.
// Un formato distinto de "json" produce salida de texto.
func New(w io.Writer, cfg config.LogConfig) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.Format == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

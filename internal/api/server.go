// Package api implementa los endpoints HTTP de la API ingestora.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB es el subconjunto de operaciones de base de datos que usan los handlers.
// *pgxpool.Pool lo satisface.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	CopyFrom(
		ctx context.Context,
		tableName pgx.Identifier,
		columnNames []string,
		rowSrc pgx.CopyFromSource,
	) (int64, error)
}

// Server atiende las peticiones HTTP usando db para persistir los datos.
type Server struct {
	db     DB
	logger *slog.Logger
}

// NewServer crea un Server que persiste en db y registra en logger.
// Si logger es nil usa slog.Default().
func NewServer(db DB, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{db: db, logger: logger}
}

// Routes devuelve un mux con los endpoints de la API registrados.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/telemetry", s.handleTelemetry)
	return mux
}

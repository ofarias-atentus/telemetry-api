package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"telemetry-api/internal/api"
	"telemetry-api/internal/config"
	"telemetry-api/internal/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error(strings.ReplaceAll(err.Error(), "\n", "; "))
		os.Exit(1)
	}
}

func run() error {
	// 1. Cargar configuración
	cfg, err := config.Load(".env")
	if err != nil {
		return fmt.Errorf("Error crítico al leer la configuración de la API: %w", err)
	}

	logger := logging.New(os.Stderr, cfg.Log)
	slog.SetDefault(logger)

	// 2. Construir DSN e inicializar Pool de conexiones
	pool, err := pgxpool.New(context.Background(), cfg.Database.BuildConnString())
	if err != nil {
		return fmt.Errorf("Error al conectar a PostgreSQL con los datos del config: %w", err)
	}
	defer pool.Close()

	// Probar conexión inicial
	if err := pool.Ping(context.Background()); err != nil {
		return fmt.Errorf("No se pudo hacer Ping a la base de datos: %w", err)
	}

	// 3. Registrar Endpoints
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:     addr,
		Handler:  api.NewServer(pool, logger).Handler(),
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	logger.Info("API Ingestora central escuchando", "addr", addr)
	return srv.ListenAndServe()
}

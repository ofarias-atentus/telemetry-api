package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"telemetry-api/internal/api"
	"telemetry-api/internal/config"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 1. Cargar archivo de configuración
	cfg, err := config.Load("config.json")
	if err != nil {
		return fmt.Errorf("Error crítico al leer config.json de la API: %w", err)
	}

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
	mux := api.NewServer(pool).Routes()

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("API Ingestora central escuchando en el puerto %s...", addr)
	return http.ListenAndServe(addr, mux)
}

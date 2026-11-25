package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"telemetry-api/internal/api"
	"telemetry-api/internal/config"
	"telemetry-api/internal/logging"
	"telemetry-api/internal/migrate"
)

const (
	serveReadHeaderTimeout  = 10 * time.Second
	serveReadTimeout        = 30 * time.Second
	serveWriteTimeout       = 30 * time.Second
	serveIdleTimeout        = 120 * time.Second
	serveMaxHeaderBytes     = 64 << 10
	serveShutdownTimeout    = 15 * time.Second
	serveStartupPingTimeout = 10 * time.Second
)

// validatePort acepta 0 (usar SERVER_PORT) o un puerto entre 1 y 65535.
func validatePort(port int) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("--port %d inválido: debe estar entre 1 y 65535", port)
	}
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	host := fs.String("host", "", "interfaz donde escuchar (por defecto todas)")
	port := fs.Int("port", 0, "puerto donde escuchar, 1-65535 (por defecto SERVER_PORT)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &exitError{code: 2, err: err}
	}
	if fs.NArg() > 0 {
		return &exitError{code: 2, err: fmt.Errorf("serve no acepta argumentos: %v", fs.Args())}
	}
	if err := validatePort(*port); err != nil {
		return &exitError{code: 2, err: err}
	}

	cfg, err := config.Load(".env")
	if err != nil {
		return fmt.Errorf("Error crítico al leer la configuración de la API: %w", err)
	}

	logger := logging.New(os.Stderr, cfg.Log)
	slog.SetDefault(logger)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.Database.BuildConnString())
	if err != nil {
		return fmt.Errorf("Error al conectar a PostgreSQL con los datos del config: %w", err)
	}
	if err := startupChecks(ctx, pool); err != nil {
		pool.Close()
		return err
	}

	listenPort := cfg.Server.Port
	if *port != 0 {
		listenPort = *port
	}
	addr := net.JoinHostPort(*host, strconv.Itoa(listenPort))
	srv := newHTTPServer(addr, api.NewServer(pool, logger).Handler(), slog.NewLogLogger(logger.Handler(), slog.LevelError))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	logger.Info("API Ingestora central escuchando", "addr", addr)
	return serveLifetime(logger, srv, sigCh, pool.Close, serveShutdownTimeout)
}

// startupChecks prueba la conexión y exige que el esquema esté en la versión
// que espera este binario. serve nunca migra.
func startupChecks(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, serveStartupPingTimeout)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("No se pudo hacer Ping a la base de datos: %w", err)
	}
	return migrate.CheckVersion(ctx, pool)
}

func newHTTPServer(addr string, handler http.Handler, errorLog *log.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ErrorLog:          errorLog,
		ReadHeaderTimeout: serveReadHeaderTimeout,
		ReadTimeout:       serveReadTimeout,
		WriteTimeout:      serveWriteTimeout,
		IdleTimeout:       serveIdleTimeout,
		MaxHeaderBytes:    serveMaxHeaderBytes,
	}
}

type httpServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
	Close() error
}

// serveLifetime escucha hasta recibir una señal o hasta que el servidor
// falle, hace el apagado ordenado dentro de budget y cierra el pool al final.
func serveLifetime(logger *slog.Logger, srv httpServer, signals <-chan os.Signal, closePool func(), budget time.Duration) error {
	defer closePool()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-signals:
		if sig != nil {
			logger.Info("señal de apagado recibida", "signal", sig.String())
		}
	}

	var result error
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		result = errors.Join(result, fmt.Errorf("apagado ordenado fallido: %w", err))
		if err := srv.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("cierre del servidor fallido: %w", err))
		}
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		result = errors.Join(result, err)
	}
	return result
}

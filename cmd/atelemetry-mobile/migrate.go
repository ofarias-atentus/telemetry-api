package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"telemetry-api/internal/config"
	"telemetry-api/internal/migrate"
)

// openMigrationDB carga la configuración y abre la conexión de migración.
// Nunca imprime el DSN.
func openMigrationDB(ctx context.Context) (*sql.DB, error) {
	cfg, err := config.Load(".env")
	if err != nil {
		return nil, &exitError{code: 2, err: fmt.Errorf("Error crítico al leer la configuración de la API: %w", err)}
	}
	db, err := migrate.Open(ctx, cfg.Database.ConnString())
	if err != nil {
		return nil, &exitError{code: 2, err: err}
	}
	return db, nil
}

func migrateStatus(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()
	db, err := openMigrationDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	applied, err := migrate.Status(ctx, db)
	return printStatus(os.Stdout, applied, err)
}

// printStatus escribe el estado y devuelve el error con el código de salida:
// 0 al día, 1 pendiente, adelantado o sin historial, 2 error operativo.
func printStatus(w io.Writer, applied int64, err error) error {
	fmt.Fprintf(w, "versión esperada: %d\n", migrate.LatestVersion)
	if err != nil {
		if errors.Is(err, migrate.ErrNotVersioned) {
			fmt.Fprintln(w, "versión aplicada: ninguna (sin historial)")
			fmt.Fprintln(w, "estado: sin historial; en una base vacía ejecuta `atelemetry-mobile migrate up`, "+
				"en una creada con los scripts SQL antiguos ejecuta `atelemetry-mobile migrate baseline`")
			return &exitError{code: 1, err: err}
		}
		return &exitError{code: 2, err: err}
	}
	fmt.Fprintf(w, "versión aplicada: %d\n", applied)
	switch {
	case applied == migrate.LatestVersion:
		fmt.Fprintln(w, "estado: al día")
		return nil
	case applied < migrate.LatestVersion:
		fmt.Fprintln(w, "estado: pendiente; ejecuta `atelemetry-mobile migrate up`")
		return &exitError{code: 1, err: fmt.Errorf("%w: aplicada %d, esperada %d",
			migrate.ErrVersionMismatch, applied, migrate.LatestVersion)}
	default:
		fmt.Fprintln(w, "estado: la base está en una versión posterior a la de este binario")
		return &exitError{code: 1, err: fmt.Errorf("%w: aplicada %d, esperada %d",
			migrate.ErrVersionMismatch, applied, migrate.LatestVersion)}
	}
}

func migrateUp(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()
	db, err := openMigrationDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	applied, err := migrate.Up(ctx, db)
	if len(applied) > 0 {
		printUp(os.Stdout, applied)
	}
	if err != nil {
		return &exitError{code: exitCodeFor(err), err: err}
	}
	if len(applied) == 0 {
		printUp(os.Stdout, applied)
	}
	return nil
}

func printUp(w io.Writer, applied []int64) {
	if len(applied) == 0 {
		fmt.Fprintf(w, "ya está en la versión %d\n", migrate.LatestVersion)
		return
	}
	parts := make([]string, 0, len(applied))
	for _, v := range applied {
		parts = append(parts, fmt.Sprint(v))
	}
	fmt.Fprintf(w, "migraciones aplicadas: %s (versión actual %d)\n", strings.Join(parts, ", "), migrate.LatestVersion)
}

func migrateBaseline(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()
	db, err := openMigrationDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	version, err := migrate.Baseline(ctx, db)
	if err != nil {
		return &exitError{code: exitCodeFor(err), err: err}
	}
	fmt.Fprintf(os.Stdout, "baseline registrado en la versión %d\n", version)
	if version < migrate.LatestVersion {
		fmt.Fprintln(os.Stdout, "faltan migraciones: ejecuta `atelemetry-mobile migrate up`")
	}
	return nil
}

// Package migrate gestiona el historial de migraciones del esquema con goose
// (github.com/pressly/goose/v3). El historial vive en public.goose_db_version.
//
// serve nunca migra: solo lee la versión aplicada (AppliedVersion) y exige que
// sea LatestVersion. Up y Baseline son los únicos que escriben.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// LatestVersion es la versión más alta de las migraciones embebidas.
const LatestVersion int64 = 4

// VersionsTable es la tabla de historial de goose.
const VersionsTable = "goose_db_version"

// ErrNotVersioned indica que la base no tiene historial de migraciones.
var ErrNotVersioned = errors.New("la base de datos no tiene historial de migraciones")

// ErrVersionMismatch indica que la versión aplicada no es la esperada, o que
// la base existe sin historial. La CLI lo mapea al código de salida 1.
var ErrVersionMismatch = errors.New("la versión del esquema no coincide")

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrationsFS devuelve las migraciones embebidas con la raíz en migrations/.
func MigrationsFS() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(fmt.Sprintf("migrate: directorio embebido inválido: %v", err))
	}
	return sub
}

// Querier es la superficie mínima para leer una fila. *pgxpool.Pool la cumple.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type sqlQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// sqlQuerier adapta *sql.DB o *sql.Tx a Querier.
type sqlQuerier struct{ q sqlQueryer }

func (s sqlQuerier) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return s.q.QueryRowContext(ctx, query, args...)
}

// AppliedVersion devuelve la versión más alta aplicada según
// public.goose_db_version. Es de solo lectura. Si la tabla no existe devuelve
// un error que envuelve ErrNotVersioned; una tabla sin filas aplicadas da 0.
func AppliedVersion(ctx context.Context, q Querier) (int64, error) {
	var v int64
	err := q.QueryRow(ctx,
		`SELECT coalesce(max(version_id), 0) FROM public.goose_db_version WHERE is_applied`).Scan(&v)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return 0, fmt.Errorf("%w: no existe la tabla %s", ErrNotVersioned, VersionsTable)
		}
		return 0, fmt.Errorf("no se pudo leer la versión del esquema: %w", err)
	}
	return v, nil
}

// Status devuelve la versión aplicada. Envuelve ErrNotVersioned si no hay historial.
func Status(ctx context.Context, db *sql.DB) (int64, error) {
	return AppliedVersion(ctx, sqlQuerier{db})
}

// CheckVersion exige que la versión aplicada sea LatestVersion. Es la puerta
// de arranque de serve.
func CheckVersion(ctx context.Context, q Querier) error {
	applied, err := AppliedVersion(ctx, q)
	if err != nil {
		if errors.Is(err, ErrNotVersioned) {
			return fmt.Errorf("%w: la base de datos no tiene historial de migraciones; "+
				"ejecuta `atdevice-api migrate status` para diagnosticar, `atdevice-api migrate up` si la base está vacía "+
				"o `atdevice-api migrate baseline` si fue creada con los scripts SQL antiguos", ErrVersionMismatch)
		}
		return err
	}
	return versionError(applied)
}

func versionError(applied int64) error {
	switch {
	case applied == LatestVersion:
		return nil
	case applied < LatestVersion:
		return fmt.Errorf("%w: el esquema está en la versión %d y la API requiere la %d; "+
			"ejecuta `atdevice-api migrate up`", ErrVersionMismatch, applied, LatestVersion)
	default:
		return fmt.Errorf("%w: el esquema está en la versión %d, posterior a la %d que conoce este binario; "+
			"despliega un binario más reciente (`atdevice-api migrate status` muestra el detalle)",
			ErrVersionMismatch, applied, LatestVersion)
	}
}

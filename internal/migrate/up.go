package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// gooseLockKey serializa migraciones concurrentes con un advisory lock de
// PostgreSQL. 0x4154444556415049 es "ATDEVAPI" en ASCII.
const gooseLockKey int64 = 0x4154444556415049

// legacyTables son las tablas que crearon los scripts SQL antiguos. Si existe
// alguna sin historial de goose, la base se debe adoptar con Baseline.
var legacyTables = []string{"countries", "device_telemetry", "devices", "hosts"}

// Open abre un *sql.DB con el driver pgx stdlib sobre dsn y comprueba la
// conexión. dsn no debe llevar parámetros pool_*. Fija search_path=public para
// que las migraciones y su historial no dependan del search_path del rol.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", pinSearchPath(dsn))
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir la conexión de migración: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("no se pudo hacer Ping a la base de datos: %w", err)
	}
	return db, nil
}

func pinSearchPath(dsn string) string {
	clean := strings.TrimSpace(dsn)
	if u, err := url.Parse(clean); err == nil && u.Scheme != "" && u.Host != "" {
		q := u.Query()
		q.Set("search_path", "public")
		u.RawQuery = q.Encode()
		return u.String()
	}
	if strings.Contains(strings.ToLower(clean), "search_path=") {
		return clean
	}
	sep := "?"
	if strings.Contains(clean, "?") {
		sep = "&"
	}
	return clean + sep + "search_path=public"
}

func newProvider(db *sql.DB) (*goose.Provider, error) {
	// Sondea cada 1 s hasta 300 veces (5 min) para que una instancia en espera
	// continúe apenas la otra termina.
	locker, err := lock.NewPostgresSessionLocker(
		lock.WithLockID(gooseLockKey),
		lock.WithLockTimeout(1, 300),
	)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el bloqueo de migraciones: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, MigrationsFS(), goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el proveedor de migraciones: %w", err)
	}
	return provider, nil
}

// Up aplica las migraciones pendientes y devuelve las versiones aplicadas en
// orden. Una base al día no cambia y devuelve una lista vacía. Una base con
// tablas pero sin historial se rechaza sin tocarla. No cierra db.
func Up(ctx context.Context, db *sql.DB) ([]int64, error) {
	// Esta comprobación corre fuera del advisory lock de goose: dos `up`
	// concurrentes sobre una base antigua no quedan totalmente protegidos.
	// Ejecutar `up` desde un solo lugar.
	if err := refuseUnversioned(ctx, db); err != nil {
		return nil, err
	}
	provider, err := newProvider(db)
	if err != nil {
		return nil, err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("no se pudieron aplicar las migraciones: %w", err)
	}
	applied := make([]int64, 0, len(results))
	for _, r := range results {
		if r != nil && r.Source != nil {
			applied = append(applied, r.Source.Version)
		}
	}
	sort.Slice(applied, func(i, j int) bool { return applied[i] < applied[j] })
	if err := alignVersionsOwner(ctx, db); err != nil {
		return applied, fmt.Errorf("migraciones aplicadas, pero %w; ejecuta migrate up como superusuario o como dueño de devices", err)
	}
	return applied, nil
}

// unversionedError decide si Up debe negarse: sin historial y con alguna tabla
// del esquema antiguo. Es pura para poder probarla sin base de datos.
func unversionedError(versioned bool, existing []string) error {
	if versioned || len(existing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: la base ya tiene las tablas %s pero no tiene historial de migraciones "+
		"(base existente sin historial); ejecuta `atdevice-api migrate baseline`",
		ErrVersionMismatch, strings.Join(existing, ", "))
}

// refuseUnversioned consulta en una sola sentencia si existe el historial y
// qué tablas antiguas hay, para ver un único snapshot.
func refuseUnversioned(ctx context.Context, db *sql.DB) error {
	var versioned bool
	var tables sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT to_regclass('public.goose_db_version') IS NOT NULL, `+
			`(SELECT string_agg(c.relname, ',' ORDER BY c.relname) FROM pg_catalog.pg_class c `+
			`JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace `+
			`WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND c.relname = ANY($1::text[]))`,
		"{"+strings.Join(legacyTables, ",")+"}").Scan(&versioned, &tables)
	if err != nil {
		return fmt.Errorf("no se pudo comprobar el historial de migraciones: %w", err)
	}
	var existing []string
	if tables.Valid && tables.String != "" {
		existing = strings.Split(tables.String, ",")
	}
	return unversionedError(versioned, existing)
}

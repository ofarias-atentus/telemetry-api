package migrate

import (
	"context"
	"database/sql"
	"fmt"
)

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// alignVersionsOwnerSQL deja goose_db_version a nombre del dueño de devices, para
// que el resultado no dependa de quién ejecutó la migración (por ejemplo un
// superusuario). La secuencia de identidad de la tabla cambia de dueño junto con
// ella. Si devices o el historial no existen no hace nada.
const alignVersionsOwnerSQL = `DO $$
DECLARE
    dev_owner name;
    ver_owner name;
BEGIN
    IF to_regclass('public.devices') IS NULL OR to_regclass('public.goose_db_version') IS NULL THEN
        RETURN;
    END IF;
    SELECT pg_get_userbyid(relowner) INTO dev_owner FROM pg_class WHERE oid = to_regclass('public.devices');
    SELECT pg_get_userbyid(relowner) INTO ver_owner FROM pg_class WHERE oid = to_regclass('public.goose_db_version');
    IF dev_owner <> ver_owner THEN
        EXECUTE format('ALTER TABLE public.goose_db_version OWNER TO %I', dev_owner);
    END IF;
END $$`

func alignVersionsOwner(ctx context.Context, e execer) error {
	if _, err := e.ExecContext(ctx, alignVersionsOwnerSQL); err != nil {
		return fmt.Errorf("no se pudo asignar el dueño de %s: %w", VersionsTable, err)
	}
	return nil
}

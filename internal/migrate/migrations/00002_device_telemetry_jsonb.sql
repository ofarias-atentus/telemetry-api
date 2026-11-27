-- +goose Up
-- Mueve las métricas de device_telemetry a la columna JSONB data y elimina las columnas antiguas.
-- Irreversible: no tiene sección Down. Hacer antes un respaldo con pg_dump -t device_telemetry.
-- Se aplica con `atdevice-api migrate up`, con la API detenida: el binario
-- antiguo no es compatible con el esquema nuevo ni al revés.
-- El UPDATE reescribe todas las filas (más disco y WAL); en una hypertable grande puede tardar.
--
-- Antes de cambiar nada comprueba, y aborta con un mensaje que explica qué hacer si:
--   * la tabla no existe, ya tiene la columna data o le falta alguna de las 8 métricas;
--   * no somos dueños de la tabla;
--   * en TimescaleDB, la hypertable tiene compresión habilitada o chunks comprimidos
--     (hay que descomprimirlos antes), o tiene agregados continuos;
--   * hay vistas, vistas materializadas o reglas que usan las columnas que se eliminan.
-- Antes de eliminar las columnas antiguas verifica que el número de filas no cambió, que
-- ninguna fila tiene data NULL y que, por cada métrica, las filas con valor coinciden con
-- las filas que tienen esa clave en data. Si algo no cuadra, aborta y se revierte todo.
-- Los NUMERIC especiales (NaN) quedan en data como texto; los NULL se omiten.
-- No revisa los jobs o políticas de TimescaleDB (p. ej. reorder) que usen un índice de una columna eliminada: DROP COLUMN borra el índice y ese job fallaría después; revisa timescaledb_information.jobs antes de migrar.

SET LOCAL lock_timeout = '5s';

-- +goose StatementBegin
DO $$
DECLARE
    metrics CONSTANT text[] := ARRAY['battery_pct', 'battery_health_pct', 'temp_celsius',
        'voltage_v', 'cycle_count', 'is_charging', 'cpu_usage', 'ram_usage'];
    missing text;
    deps text;
    n bigint;
BEGIN
    IF to_regclass('device_telemetry') IS NULL THEN
        RAISE EXCEPTION 'La tabla device_telemetry no existe: la base no tiene el esquema original. Revisa con `migrate status` o usa `migrate baseline`.';
    END IF;

    IF NOT pg_has_role(current_user, (SELECT relowner FROM pg_class WHERE oid = 'device_telemetry'::regclass), 'USAGE') THEN
        RAISE EXCEPTION 'El rol % no es dueño de device_telemetry y no puede alterarla. Ejecuta la migración con el rol dueño de la tabla.', current_user;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_attribute
               WHERE attrelid = 'device_telemetry'::regclass AND attname = 'data' AND NOT attisdropped) THEN
        RAISE EXCEPTION 'device_telemetry ya tiene una columna data: la tabla está a medio migrar o fue modificada a mano. Revisa el esquema antes de continuar.';
    END IF;

    SELECT string_agg(m, ', ') INTO missing
    FROM unnest(metrics) AS m
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_attribute a
        WHERE a.attrelid = 'device_telemetry'::regclass AND a.attname = m
            AND a.attnum > 0 AND NOT a.attisdropped
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'A device_telemetry le faltan columnas de métricas: %. El esquema no coincide con el original; revisa con `migrate baseline`.', missing;
    END IF;

    SELECT string_agg(format('%I (%s)', c.relname, r.rulename), ', ') INTO deps
    FROM pg_depend d
    JOIN pg_rewrite r ON d.classid = 'pg_rewrite'::regclass AND d.objid = r.oid
    JOIN pg_class c ON c.oid = r.ev_class
    WHERE d.refclassid = 'pg_class'::regclass
        AND d.refobjid = 'device_telemetry'::regclass
        AND d.refobjsubid IN (
            SELECT a.attnum FROM pg_attribute a
            WHERE a.attrelid = 'device_telemetry'::regclass AND a.attname = ANY (metrics)
        );
    IF deps IS NOT NULL THEN
        RAISE EXCEPTION 'Hay vistas, vistas materializadas o reglas que usan columnas de device_telemetry que esta migración elimina: %. Elimínalas (o recréalas sobre device_telemetry.data) y vuelve a ejecutar.', deps;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
        EXECUTE format('SELECT count(*) FROM timescaledb_information.hypertables '
            'WHERE hypertable_schema = %L AND hypertable_name = ''device_telemetry'' AND compression_enabled',
            current_schema()) INTO n;
        IF n > 0 THEN
            RAISE EXCEPTION 'device_telemetry es una hypertable con compresión de TimescaleDB habilitada: descomprime los chunks y desactiva la compresión antes de migrar (SELECT remove_compression_policy(''device_telemetry'', if_exists => true); SELECT decompress_chunk(c, true) FROM show_chunks(''device_telemetry'') c; ALTER TABLE device_telemetry SET (timescaledb.compress = false);). Se puede volver a activar con la nueva columna data.';
        END IF;

        EXECUTE format('SELECT count(*) FROM timescaledb_information.chunks '
            'WHERE hypertable_schema = %L AND hypertable_name = ''device_telemetry'' AND is_compressed',
            current_schema()) INTO n;
        IF n > 0 THEN
            RAISE EXCEPTION 'device_telemetry tiene % chunks comprimidos: descomprime los chunks (SELECT decompress_chunk(c, true) FROM show_chunks(''device_telemetry'') c;) antes de migrar.', n;
        END IF;

        EXECUTE format('SELECT string_agg(view_name::text, '', '') FROM timescaledb_information.continuous_aggregates '
            'WHERE hypertable_schema = %L AND hypertable_name = ''device_telemetry''',
            current_schema()) INTO deps;
        IF deps IS NOT NULL THEN
            RAISE EXCEPTION 'Hay agregados continuos de TimescaleDB sobre device_telemetry: %. Elimínalos (DROP MATERIALIZED VIEW) y recréalos sobre data después de migrar.', deps;
        END IF;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE device_telemetry ADD COLUMN data JSONB NOT NULL DEFAULT '{}'::jsonb;

-- El ADD COLUMN ya tiene el bloqueo exclusivo: el conteo es estable hasta el COMMIT.
SELECT set_config('migration.telemetry_rows', count(*)::text, true) FROM device_telemetry;

UPDATE device_telemetry SET data = jsonb_strip_nulls(jsonb_build_object(
    'battery_pct',        battery_pct,
    'battery_health_pct', battery_health_pct,
    'temp_celsius',       temp_celsius,
    'voltage_v',          voltage_v,
    'cycle_count',        cycle_count,
    'is_charging',        is_charging,
    'cpu_usage',          cpu_usage,
    'ram_usage',          ram_usage
));

-- +goose StatementBegin
DO $$
DECLARE
    metrics CONSTANT text[] := ARRAY['battery_pct', 'battery_health_pct', 'temp_celsius',
        'voltage_v', 'cycle_count', 'is_charging', 'cpu_usage', 'ram_usage'];
    rows_before bigint := current_setting('migration.telemetry_rows')::bigint;
    r record;
    k text;
    mismatches text := '';
BEGIN
    SELECT
        count(*) AS total,
        count(*) FILTER (WHERE data IS NULL) AS null_data,
        count(battery_pct) AS battery_pct_src,
        count(*) FILTER (WHERE data ? 'battery_pct') AS battery_pct_json,
        count(battery_health_pct) AS battery_health_pct_src,
        count(*) FILTER (WHERE data ? 'battery_health_pct') AS battery_health_pct_json,
        count(temp_celsius) AS temp_celsius_src,
        count(*) FILTER (WHERE data ? 'temp_celsius') AS temp_celsius_json,
        count(voltage_v) AS voltage_v_src,
        count(*) FILTER (WHERE data ? 'voltage_v') AS voltage_v_json,
        count(cycle_count) AS cycle_count_src,
        count(*) FILTER (WHERE data ? 'cycle_count') AS cycle_count_json,
        count(is_charging) AS is_charging_src,
        count(*) FILTER (WHERE data ? 'is_charging') AS is_charging_json,
        count(cpu_usage) AS cpu_usage_src,
        count(*) FILTER (WHERE data ? 'cpu_usage') AS cpu_usage_json,
        count(ram_usage) AS ram_usage_src,
        count(*) FILTER (WHERE data ? 'ram_usage') AS ram_usage_json
    INTO r
    FROM device_telemetry;

    IF r.total <> rows_before THEN
        RAISE EXCEPTION 'device_telemetry cambió de % a % filas durante la migración; se revierte. Detén la ingesta y reintenta.', rows_before, r.total;
    END IF;
    IF r.null_data > 0 THEN
        RAISE EXCEPTION 'device_telemetry tiene % filas con data NULL tras el UPDATE; se revierte.', r.null_data;
    END IF;

    FOREACH k IN ARRAY metrics LOOP
        IF (to_jsonb(r) ->> (k || '_src')) <> (to_jsonb(r) ->> (k || '_json')) THEN
            mismatches := mismatches || format('%s (columna: %s, data: %s) ', k,
                to_jsonb(r) ->> (k || '_src'), to_jsonb(r) ->> (k || '_json'));
        END IF;
    END LOOP;
    IF mismatches <> '' THEN
        RAISE EXCEPTION 'Las métricas copiadas a data no coinciden con las columnas originales: %. No se eliminan las columnas; se revierte.', mismatches;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE device_telemetry
    DROP COLUMN battery_pct,
    DROP COLUMN battery_health_pct,
    DROP COLUMN temp_celsius,
    DROP COLUMN voltage_v,
    DROP COLUMN cycle_count,
    DROP COLUMN is_charging,
    DROP COLUMN cpu_usage,
    DROP COLUMN ram_usage;

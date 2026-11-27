-- +goose Up
-- Mueve host_pc, host_ip y host_location de devices a la nueva tabla hosts (una fila por host_pc)
-- y alias_name, model, os_version y battery_design_cap a la columna JSONB devices.attributes.
-- Si los dispositivos de un host discrepan, hosts toma los valores del dispositivo actualizado más
-- recientemente (updated_at; a igualdad o sin fecha, el de menor device_id). host_location vacío pasa a NULL
-- y un host_ip NULL pasa a cadena vacía, porque hosts.host_ip es NOT NULL.
-- Irreversible: no tiene sección Down. Hacer antes un respaldo con pg_dump -t devices.
-- Se aplica con `atdevice-api migrate up`, con la API detenida: el binario
-- antiguo no es compatible con el esquema nuevo ni al revés.
-- La ingesta de telemetría también se detiene hasta el COMMIT: la FK de device_telemetry toma bloqueos sobre devices.
--
-- Antes de cambiar nada comprueba, y aborta con un mensaje que explica qué hacer si:
--   * devices no existe, ya existe hosts o devices.attributes, o falta alguna columna esperada;
--   * no somos dueños de devices;
--   * hay vistas, vistas materializadas o reglas que usan las columnas que se eliminan;
--   * hay dispositivos con host_pc NULL, o con host_pc, host_ip o host_location más largos que
--     las columnas de hosts (VARCHAR(100), VARCHAR(15) y VARCHAR(50)); se listan los device_id.
-- Antes de eliminar las columnas antiguas verifica que devices conserva sus filas, que todo
-- dispositivo tiene su fila en hosts, que hosts tiene una fila por host_pc distinto y que, por
-- cada atributo, las filas con valor coinciden con las filas que tienen esa clave en attributes.
-- Si algo no cuadra, aborta y se revierte todo.
-- No revisa los jobs o políticas de TimescaleDB (p. ej. reorder) que usen un índice de una columna eliminada: DROP COLUMN borra el índice y ese job fallaría después; revisa timescaledb_information.jobs antes de migrar.

SET LOCAL lock_timeout = '5s';

-- +goose StatementBegin
DO $$
DECLARE
    old_cols CONSTANT text[] := ARRAY['alias_name', 'model', 'os_version', 'battery_design_cap',
        'host_pc', 'host_ip', 'host_location', 'device_id', 'country', 'updated_at'];
    dropped_cols CONSTANT text[] := ARRAY['alias_name', 'model', 'os_version', 'battery_design_cap',
        'host_ip', 'host_location'];
    missing text;
    deps text;
    sample text;
    n bigint;
BEGIN
    IF to_regclass('devices') IS NULL THEN
        RAISE EXCEPTION 'La tabla devices no existe: la base no tiene el esquema original. Revisa con `migrate status` o usa `migrate baseline`.';
    END IF;

    IF NOT pg_has_role(current_user, (SELECT relowner FROM pg_class WHERE oid = 'devices'::regclass), 'USAGE') THEN
        RAISE EXCEPTION 'El rol % no es dueño de devices y no puede alterarla. Ejecuta la migración con el rol dueño de la tabla.', current_user;
    END IF;

    IF to_regclass('hosts') IS NOT NULL THEN
        RAISE EXCEPTION 'Ya existe una relación llamada hosts: la base está a medio migrar o fue modificada a mano. Revisa el esquema antes de continuar.';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_attribute
               WHERE attrelid = 'devices'::regclass AND attname = 'attributes' AND NOT attisdropped) THEN
        RAISE EXCEPTION 'devices ya tiene una columna attributes: la tabla está a medio migrar o fue modificada a mano. Revisa el esquema antes de continuar.';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'devices_host_pc_fkey' AND conrelid = 'devices'::regclass) THEN
        RAISE EXCEPTION 'devices ya tiene la restricción devices_host_pc_fkey: la base está a medio migrar o fue modificada a mano.';
    END IF;

    SELECT string_agg(m, ', ') INTO missing
    FROM unnest(old_cols) AS m
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_attribute a
        WHERE a.attrelid = 'devices'::regclass AND a.attname = m
            AND a.attnum > 0 AND NOT a.attisdropped
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'A devices le faltan columnas: %. El esquema no coincide con el original; revisa con `migrate baseline`.', missing;
    END IF;

    SELECT string_agg(format('%I (%s)', c.relname, r.rulename), ', ') INTO deps
    FROM pg_depend d
    JOIN pg_rewrite r ON d.classid = 'pg_rewrite'::regclass AND d.objid = r.oid
    JOIN pg_class c ON c.oid = r.ev_class
    WHERE d.refclassid = 'pg_class'::regclass
        AND d.refobjid = 'devices'::regclass
        AND d.refobjsubid IN (
            SELECT a.attnum FROM pg_attribute a
            WHERE a.attrelid = 'devices'::regclass AND a.attname = ANY (dropped_cols)
        );
    IF deps IS NOT NULL THEN
        RAISE EXCEPTION 'Hay vistas, vistas materializadas o reglas que usan columnas de devices que esta migración elimina (%): %. Elimínalas (o recréalas sobre hosts y devices.attributes) y vuelve a ejecutar.', array_to_string(dropped_cols, ', '), deps;
    END IF;

    SELECT count(*), array_to_string((array_agg(device_id ORDER BY device_id))[1:10], ', ')
    INTO n, sample
    FROM devices WHERE host_pc IS NULL;
    IF n > 0 THEN
        RAISE EXCEPTION 'Hay % dispositivos con host_pc NULL (device_id, hasta 10): %. Asígnales un host_pc o elimínalos antes de migrar.', n, sample;
    END IF;

    SELECT count(*), array_to_string((array_agg(device_id ORDER BY device_id))[1:10], ', ')
    INTO n, sample
    FROM (
        SELECT DISTINCT ON (host_pc) device_id, host_pc, host_ip, host_location
        FROM devices
        ORDER BY host_pc, updated_at DESC NULLS LAST, device_id
    ) AS latest
    WHERE length(host_pc) > 100 OR length(host_ip) > 15 OR length(host_location) > 50;
    IF n > 0 THEN
        RAISE EXCEPTION 'Hay % hosts cuyo host_pc (máx. 100), host_ip (máx. 15) o host_location (máx. 50) no cabe en la tabla hosts (device_id, hasta 10): %. Acorta esos valores antes de migrar.', n, sample;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE devices ADD COLUMN attributes JSONB NOT NULL DEFAULT '{}'::jsonb;

-- El ADD COLUMN ya tiene el bloqueo exclusivo: el conteo es estable hasta el COMMIT.
SELECT set_config('migration.devices_rows', count(*)::text, true) FROM devices;

CREATE TABLE hosts (
    host_pc              VARCHAR(100) PRIMARY KEY,
    host_ip              VARCHAR(15) NOT NULL,
    host_location        VARCHAR(50),
    updated_at           TIMESTAMPTZ DEFAULT NOW()
);

-- Si migra un superusuario, hosts queda a nombre del dueño de devices, como si migrara el dueño.
-- +goose StatementBegin
DO $$
DECLARE
    dev_owner name;
BEGIN
    SELECT pg_get_userbyid(relowner) INTO dev_owner FROM pg_class WHERE oid = 'devices'::regclass;
    IF dev_owner <> current_user THEN
        EXECUTE format('ALTER TABLE %I OWNER TO %I', 'hosts', dev_owner);
    END IF;
END $$;
-- +goose StatementEnd

INSERT INTO hosts (host_pc, host_ip, host_location, updated_at)
SELECT DISTINCT ON (host_pc) host_pc, COALESCE(host_ip, ''), NULLIF(host_location, ''), NOW()
FROM devices
ORDER BY host_pc, updated_at DESC NULLS LAST, device_id;

UPDATE devices SET attributes = jsonb_strip_nulls(jsonb_build_object(
    'alias_name',         alias_name,
    'model',              model,
    'os_version',         os_version,
    'battery_design_cap', battery_design_cap
));

-- +goose StatementBegin
DO $$
DECLARE
    attrs CONSTANT text[] := ARRAY['alias_name', 'model', 'os_version', 'battery_design_cap'];
    rows_before bigint := current_setting('migration.devices_rows')::bigint;
    r record;
    k text;
    orphans bigint;
    hosts_rows bigint;
    mismatches text := '';
BEGIN
    SELECT
        count(*) AS total,
        count(DISTINCT host_pc) AS distinct_hosts,
        count(alias_name) AS alias_name_src,
        count(*) FILTER (WHERE attributes ? 'alias_name') AS alias_name_json,
        count(model) AS model_src,
        count(*) FILTER (WHERE attributes ? 'model') AS model_json,
        count(os_version) AS os_version_src,
        count(*) FILTER (WHERE attributes ? 'os_version') AS os_version_json,
        count(battery_design_cap) AS battery_design_cap_src,
        count(*) FILTER (WHERE attributes ? 'battery_design_cap') AS battery_design_cap_json
    INTO r
    FROM devices;

    SELECT count(*) INTO orphans
    FROM devices d
    WHERE NOT EXISTS (SELECT 1 FROM hosts h WHERE h.host_pc = d.host_pc);

    SELECT count(*) INTO hosts_rows FROM hosts;

    IF r.total <> rows_before THEN
        RAISE EXCEPTION 'devices cambió de % a % filas durante la migración; se revierte. Detén la ingesta y reintenta.', rows_before, r.total;
    END IF;
    IF orphans > 0 THEN
        RAISE EXCEPTION '% dispositivos no tienen fila en hosts; se revierte.', orphans;
    END IF;
    IF hosts_rows <> r.distinct_hosts THEN
        RAISE EXCEPTION 'hosts tiene % filas pero devices tiene % host_pc distintos; se revierte.', hosts_rows, r.distinct_hosts;
    END IF;

    FOREACH k IN ARRAY attrs LOOP
        IF (to_jsonb(r) ->> (k || '_src')) <> (to_jsonb(r) ->> (k || '_json')) THEN
            mismatches := mismatches || format('%s (columna: %s, attributes: %s) ', k,
                to_jsonb(r) ->> (k || '_src'), to_jsonb(r) ->> (k || '_json'));
        END IF;
    END LOOP;
    IF mismatches <> '' THEN
        RAISE EXCEPTION 'Los atributos copiados a attributes no coinciden con las columnas originales: %. No se eliminan las columnas; se revierte.', mismatches;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE devices
    DROP COLUMN alias_name,
    DROP COLUMN model,
    DROP COLUMN os_version,
    DROP COLUMN battery_design_cap,
    DROP COLUMN host_ip,
    DROP COLUMN host_location;

ALTER TABLE devices ADD CONSTRAINT devices_host_pc_fkey FOREIGN KEY (host_pc) REFERENCES hosts(host_pc);

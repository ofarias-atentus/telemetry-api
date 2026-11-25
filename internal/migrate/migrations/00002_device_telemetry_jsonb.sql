-- +goose Up
-- Mueve las métricas de device_telemetry a la columna JSONB data y elimina las columnas antiguas.
-- Irreversible: no tiene sección Down. Hacer antes un respaldo con pg_dump -t device_telemetry.
-- Se aplica con `atdevice-api migrate up`, con la API detenida: el binario
-- antiguo no es compatible con el esquema nuevo ni al revés.
-- El UPDATE reescribe todas las filas (más disco y WAL); en una hypertable grande puede tardar.
-- En TimescaleDB, descomprimir antes los chunks comprimidos; los agregados continuos o
-- políticas que usen las columnas antiguas bloquean el DROP COLUMN.

SET LOCAL lock_timeout = '5s';

ALTER TABLE device_telemetry ADD COLUMN data JSONB NOT NULL DEFAULT '{}'::jsonb;

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

ALTER TABLE device_telemetry
    DROP COLUMN battery_pct,
    DROP COLUMN battery_health_pct,
    DROP COLUMN temp_celsius,
    DROP COLUMN voltage_v,
    DROP COLUMN cycle_count,
    DROP COLUMN is_charging,
    DROP COLUMN cpu_usage,
    DROP COLUMN ram_usage;

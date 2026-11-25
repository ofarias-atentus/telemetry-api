-- +goose Up
-- Mueve host_pc, host_ip y host_location de devices a la nueva tabla hosts (una fila por host_pc)
-- y alias_name, model, os_version y battery_design_cap a la columna JSONB devices.attributes.
-- Si los dispositivos de un host discrepan, hosts toma los valores del dispositivo actualizado más recientemente.
-- Irreversible: no tiene sección Down. Hacer antes un respaldo con pg_dump -t devices.
-- Se aplica con `atdevice-api migrate up`, con la API detenida: el binario
-- antiguo no es compatible con el esquema nuevo ni al revés.
-- La ingesta de telemetría también se detiene hasta el COMMIT: la FK de device_telemetry toma bloqueos sobre devices.
-- Vistas u otros objetos que dependan de las columnas eliminadas hacen fallar DROP COLUMN: revisarlos antes.

SET LOCAL lock_timeout = '5s';

CREATE TABLE hosts (
    host_pc              VARCHAR(100) PRIMARY KEY,
    host_ip              VARCHAR(15) NOT NULL,
    host_location        VARCHAR(50),
    updated_at           TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO hosts (host_pc, host_ip, host_location, updated_at)
SELECT DISTINCT ON (host_pc) host_pc, host_ip, NULLIF(host_location, ''), NOW()
FROM devices
ORDER BY host_pc, updated_at DESC NULLS LAST;

ALTER TABLE devices ADD COLUMN attributes JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE devices SET attributes = jsonb_strip_nulls(jsonb_build_object(
    'alias_name',         alias_name,
    'model',              model,
    'os_version',         os_version,
    'battery_design_cap', battery_design_cap
));

ALTER TABLE devices
    DROP COLUMN alias_name,
    DROP COLUMN model,
    DROP COLUMN os_version,
    DROP COLUMN battery_design_cap,
    DROP COLUMN host_ip,
    DROP COLUMN host_location;

ALTER TABLE devices ADD CONSTRAINT devices_host_pc_fkey FOREIGN KEY (host_pc) REFERENCES hosts(host_pc);

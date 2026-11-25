-- +goose Up
-- Esquema original de la API (devices y device_telemetry con columnas de métricas).
-- Las migraciones 00002 a 00004 lo llevan al esquema actual.
-- Irreversible: no tiene sección Down.
-- TimescaleDB es opcional: la extensión y la hypertable solo se crean si timescaledb
-- está disponible en el servidor; en PostgreSQL común se omiten.

-- Tabla de Inventario de Dispositivos (Control estático y estado del host)
CREATE TABLE IF NOT EXISTS devices (
    device_id            VARCHAR(64) PRIMARY KEY,
    alias_name           VARCHAR(100),
    platform             VARCHAR(20) NOT NULL,    -- "ios" o "android"
    model                VARCHAR(50),             -- Ej: "iPhone 12,1"
    os_version           VARCHAR(20),             -- Ej: "17.4"
    host_pc              VARCHAR(100) NOT NULL,   -- Nombre del Host
    host_ip              VARCHAR(15) NOT NULL,    -- IP del Host
    host_location        VARCHAR(50),             -- Ej: "Rack-Santiago-Fila-1"
    country              VARCHAR(50) NOT NULL,    -- Ej: "Chile", "Argentina", "Peru"
    battery_design_cap   INT,                     -- Capacidad de fábrica en mAh
    status               VARCHAR(20) DEFAULT 'active', -- "active", "maintenance", "retired"
    updated_at           TIMESTAMPTZ DEFAULT NOW()
);

-- Tabla de Telemetría (Series Temporales de Hardware)
CREATE TABLE IF NOT EXISTS device_telemetry (
    time                 TIMESTAMPTZ NOT NULL,
    device_id            VARCHAR(64) REFERENCES devices(device_id),
    battery_pct          NUMERIC(5,2),
    battery_health_pct   NUMERIC(5,2),
    temp_celsius         NUMERIC(5,2),
    voltage_v            NUMERIC(5,3),
    cycle_count          INT,
    is_charging          BOOLEAN,
    cpu_usage            NUMERIC(5,2),
    ram_usage            NUMERIC(5,2)
);

-- Activar la extensión de series temporales y convertir la telemetría en hypertable
-- particionada por tiempo, solo si TimescaleDB está disponible.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb') THEN
        CREATE EXTENSION IF NOT EXISTS timescaledb;
        PERFORM create_hypertable('device_telemetry', 'time', if_not_exists => TRUE);
    END IF;
END $$;
-- +goose StatementEnd

-- Índice secundario para consultas rápidas por dispositivo y tiempo
CREATE INDEX IF NOT EXISTS idx_device_time ON device_telemetry (device_id, time DESC);

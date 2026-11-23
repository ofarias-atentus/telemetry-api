-- 1. Activar la extensión de series temporales
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- 2. Tabla de Inventario de Dispositivos (Control estático y estado del host)
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

-- 3. Tabla de Telemetría (Series Temporales; las métricas van en el JSONB data)
CREATE TABLE IF NOT EXISTS device_telemetry (
    time                 TIMESTAMPTZ NOT NULL,
    device_id            VARCHAR(64) REFERENCES devices(device_id),
    data                 JSONB NOT NULL DEFAULT '{}'::jsonb  -- Métricas, ej: {"battery_pct": 80, "cpu_usage": 12.5}
);

-- 4. Convertir la tabla de telemetría en Hypertable particionada por tiempo
SELECT create_hypertable('device_telemetry', 'time', if_not_exists => TRUE);

-- 5. Crear índice secundario para consultas rápidas por dispositivo y tiempo
CREATE INDEX IF NOT EXISTS idx_device_time ON device_telemetry (device_id, time DESC);
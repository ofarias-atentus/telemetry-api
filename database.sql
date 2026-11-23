-- 1. Activar la extensión de series temporales
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- 2. Tabla de Hosts (PC que tiene conectados los dispositivos)
CREATE TABLE IF NOT EXISTS hosts (
    host_pc              VARCHAR(100) PRIMARY KEY, -- Nombre del Host
    host_ip              VARCHAR(15) NOT NULL,     -- IP del Host
    host_location        VARCHAR(50),              -- Ej: "Rack-Santiago-Fila-1"
    updated_at           TIMESTAMPTZ DEFAULT NOW()
);

-- 3. Tabla de Inventario de Dispositivos (Campos núcleo en columnas; el resto en el JSONB attributes)
CREATE TABLE IF NOT EXISTS devices (
    device_id            VARCHAR(64) PRIMARY KEY,
    platform             VARCHAR(20) NOT NULL,    -- "ios" o "android"
    host_pc              VARCHAR(100) NOT NULL CONSTRAINT devices_host_pc_fkey REFERENCES hosts(host_pc),
    country              VARCHAR(50) NOT NULL,    -- Ej: "Chile", "Argentina", "Peru"
    status               VARCHAR(20) DEFAULT 'active', -- "active", "maintenance", "retired"
    attributes           JSONB NOT NULL DEFAULT '{}'::jsonb, -- Ej: {"alias_name": "a", "model": "iPhone 12,1", "os_version": "17.4", "battery_design_cap": 4000}
    updated_at           TIMESTAMPTZ DEFAULT NOW()
);

-- 4. Tabla de Telemetría (Series Temporales; las métricas van en el JSONB data)
CREATE TABLE IF NOT EXISTS device_telemetry (
    time                 TIMESTAMPTZ NOT NULL,
    device_id            VARCHAR(64) REFERENCES devices(device_id),
    data                 JSONB NOT NULL DEFAULT '{}'::jsonb  -- Métricas, ej: {"battery_pct": 80, "cpu_usage": 12.5}
);

-- 5. Convertir la tabla de telemetría en Hypertable particionada por tiempo
SELECT create_hypertable('device_telemetry', 'time', if_not_exists => TRUE);

-- 6. Crear índice secundario para consultas rápidas por dispositivo y tiempo
CREATE INDEX IF NOT EXISTS idx_device_time ON device_telemetry (device_id, time DESC);
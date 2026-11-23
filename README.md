# Telemetry API (`atdevice-api`)

API ingestora central escrita en **Go** para el registro de inventario y recolección masiva de telemetría de hardware (batería, temperatura, voltaje, CPU y RAM) de dispositivos móviles (**iOS** y **Android**) distribuidos en hosts de monitoreo. Utiliza **PostgreSQL** con **TimescaleDB** para el almacenamiento optimizado de series temporales.

---

## Características Principales

- **Inventario de Dispositivos (`Upsert`)**: Registro y actualización continua del estado, versión de sistema operativo y ubicación física/lógica (host, IP, rack, país) de cada dispositivo.
- **Ingesta Masiva de Telemetría (`CopyFrom`)**: Inserción por lotes de alta velocidad utilizando el protocolo binario `COPY` de PostgreSQL a través de `pgx/v5`.
- **Series Temporales con TimescaleDB**: Particionamiento automático por tiempo (`hypertable`) e indexación compuesta por dispositivo y marca temporal descendente.

---

## Requisitos Previos

- **Go**: `1.26+` (según definición en `go.mod`)
- **PostgreSQL**: `14+` con la extensión **TimescaleDB** instalada y activa.

---

## Estructura del Proyecto

```text
telemetry-api/
├── config.json      # Configuración del servidor HTTP y credenciales de base de datos
├── database.sql     # Script DDL para inicializar tablas, hypertable e índices
├── go.mod           # Definición del módulo y dependencias Go
├── go.sum           # Checksums de dependencias
├── main.go          # Punto de entrada: carga la configuración, conecta a PostgreSQL y arranca el servidor
├── internal/
│   ├── api/                     # Servidor HTTP: handlers, modelos y pruebas
│   │   ├── server.go            # Interfaz DB, Server, NewServer y Routes
│   │   ├── devices.go           # Handler de /api/v1/devices y consulta de upsert
│   │   ├── telemetry.go         # Handler de /api/v1/telemetry
│   │   ├── models.go            # Structs DeviceInventory y TelemetryData, y payload() para la columna data
│   │   ├── models_test.go       # Pruebas de payload() y de su JSON
│   │   ├── fake_db_test.go      # Base de datos falsa para pruebas unitarias
│   │   ├── devices_test.go      # Pruebas unitarias de /api/v1/devices
│   │   ├── telemetry_test.go    # Pruebas unitarias de /api/v1/telemetry
│   │   ├── server_test.go       # Pruebas de enrutamiento
│   │   └── integration_test.go  # Prueba de integración (tag integration)
│   └── config/                  # Carga y decodificación de config.json
│       ├── config.go
│       └── config_test.go
├── migrations/
│   └── 001_device_telemetry_jsonb.sql  # Mueve las métricas antiguas a la columna data
└── README.md        # Documentación del proyecto
```

---

## Configuración e Instalación

### 1. Preparar la Base de Datos

Crea la base de datos en PostgreSQL (por ejemplo, `attelemetry`) y ejecuta el script `database.sql` para habilitar `timescaledb` y crear las tablas `devices` y `device_telemetry`:

```bash
psql -U <usuario> -h <host> -d attelemetry -f database.sql
```

#### Modelo de Datos

1. **Tabla `devices`**: Almacena el inventario estático y estado actual del dispositivo.
   - `device_id` (`VARCHAR(64)`, PK): Identificador único del dispositivo (UDID / Serial).
   - `alias_name` (`VARCHAR(100)`): Nombre descriptivo o alias del equipo.
   - `platform` (`VARCHAR(20)`): Plataforma (`ios` o `android`).
   - `model` (`VARCHAR(50)`): Modelo del dispositivo (ej. `iPhone 12,1`).
   - `os_version` (`VARCHAR(20)`): Versión del sistema operativo (ej. `17.4`).
   - `host_pc` (`VARCHAR(100)`): Nombre del equipo host al que está conectado.
   - `host_ip` (`VARCHAR(15)`): Dirección IP del host.
   - `host_location` (`VARCHAR(50)`): Ubicación física (ej. `Rack-Santiago-Fila-1`).
   - `country` (`VARCHAR(50)`): País de ubicación (ej. `Chile`, `Argentina`, `Peru`).
   - `battery_design_cap` (`INT`): Capacidad de fábrica de la batería en mAh.
   - `status` (`VARCHAR(20)`): Estado operativo (`active`, `maintenance`, `retired`).
   - `updated_at` (`TIMESTAMPTZ`): Fecha y hora de la última actualización.

2. **Hypertable `device_telemetry`**: Almacena las mediciones en series temporales.
   - `time` (`TIMESTAMPTZ`, NOT NULL): Marca de tiempo de la muestra.
   - `device_id` (`VARCHAR(64)`, FK -> `devices.device_id`): Dispositivo asociado.
   - `data` (`JSONB`, NOT NULL, por defecto `{}`): Métricas como objeto JSON libre, por ejemplo `battery_pct`, `temp_celsius` o `cpu_usage`. El esquema no limita sus claves.

#### Migraciones

Si la base de datos ya tiene `device_telemetry` con las columnas antiguas (`battery_pct`, `battery_health_pct`, `temp_celsius`, `voltage_v`, `cycle_count`, `is_charging`, `cpu_usage` y `ram_usage`), aplica `migrations/001_device_telemetry_jsonb.sql`. Mueve las métricas a la columna `data` y elimina las columnas antiguas. Todo se ejecuta en una sola transacción:

```bash
psql -U <usuario> -h <host> -d attelemetry -f migrations/001_device_telemetry_jsonb.sql
```

> **Advertencias**
> - Haz un respaldo antes de aplicar la migración, por ejemplo con `pg_dump -U <usuario> -h <host> -t device_telemetry attelemetry > device_telemetry_backup.sql`. No hay migración inversa.
> - Detén la API y despliega el binario nuevo junto con la migración. El esquema antiguo y el nuevo no son compatibles: cada binario falla con el otro.
> - La migración reescribe todas las filas; en una hypertable grande puede tardar y usa más disco y WAL. Si la compresión de TimescaleDB está activa, descomprime antes los chunks comprimidos. Los continuous aggregates o políticas que usen las columnas antiguas bloquean el `DROP COLUMN`, así que revísalos antes.
> - La migración fija `lock_timeout` en 5 segundos: si no obtiene los bloqueos a tiempo, falla en lugar de esperar.
> - Ejecutarla por segunda vez falla y hace rollback, sin cambios.
> - Las métricas con valor `NULL` no se copian a `data`.
> - Las instalaciones nuevas no necesitan la migración: `database.sql` ya crea la tabla en el formato nuevo.

---

### 2. Configurar `config.json`

Edita el archivo `config.json` en la raíz del proyecto con los parámetros del servidor y de conexión a PostgreSQL:

```json
{
  "server": {
    "port": 8080
  },
  "database": {
    "host": "127.0.0.1",
    "port": 5432,
    "user": "postgres",
    "password": "tu_password",
    "dbname": "attelemetry",
    "sslmode": "disable",
    "max_conns": 25,
    "min_conns": 5
  }
}
```

---

## Ejecución del Proyecto

### Opción A: Ejecutar directamente desde el código fuente

1. Descargar las dependencias del módulo:
   ```bash
   go mod download
   ```
2. Iniciar el servidor:
   ```bash
   go run main.go
   ```

### Opción B: Compilar y ejecutar el binario

1. Compilar el binario `atdevice-api`:
   ```bash
   go build -o atdevice-api main.go
   ```
2. Ejecutar el binario (asegúrate de que `config.json` esté en el directorio de trabajo actual):
   ```bash
   ./atdevice-api
   ```

Al iniciar correctamente, verás en consola:
```text
API Ingestora central escuchando en el puerto :8080...
```

---

## Pruebas

### Pruebas unitarias

No requieren base de datos. Usan `httptest` y una base de datos falsa:

```bash
go test ./...
go test -race ./...
go test -run TestHandleDevices ./internal/api/
```

### Prueba de integración

Requiere PostgreSQL con `database.sql` ya aplicado y la variable `TEST_DATABASE_URL` con una URL `postgres://`. Si la variable no está definida, la prueba se omite. La prueba crea y elimina sus propias filas `itest-*`.

```bash
TEST_DATABASE_URL='postgres://usuario:password@127.0.0.1:5432/attelemetry?sslmode=disable' \
  go test -tags integration -run Integration ./internal/api/
```

---

## Especificación de Endpoints (API v1)

### 1. Registrar / Actualizar Inventario de Dispositivos

Inserta nuevos dispositivos o actualiza los existentes si el `device_id` ya se encuentra registrado (`ON CONFLICT DO UPDATE`).

- **URL**: `/api/v1/devices`
- **Método**: `POST`
- **Content-Type**: `application/json`

#### Cuerpo de la Petición (JSON Array)

```json
[
  {
    "device_id": "00008101-00123456789ABCDE",
    "alias_name": "CL-IPH12-01",
    "platform": "ios",
    "model": "iPhone 12,1",
    "os_version": "17.4",
    "host_name": "macmini-santiago-01",
    "host_ip": "192.168.10.25",
    "host_location": "Rack-Santiago-Fila-1",
    "country": "Chile",
    "battery_design_cap": 3110,
    "status": "active"
  }
]
```

#### Respuestas

- **`200 OK`**:
  ```json
  {"status":"success"}
  ```
- **`400 Bad Request`**: JSON inválido.
- **`405 Method Not Allowed`**: Método HTTP distinto de `POST`.

#### Ejemplo con `curl`

```bash
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '[
    {
      "device_id": "00008101-00123456789ABCDE",
      "alias_name": "CL-IPH12-01",
      "platform": "ios",
      "model": "iPhone 12,1",
      "os_version": "17.4",
      "host_name": "macmini-santiago-01",
      "host_ip": "192.168.10.25",
      "host_location": "Rack-Santiago-Fila-1",
      "country": "Chile",
      "battery_design_cap": 3110,
      "status": "active"
    }
  ]'
```

---

### 2. Ingestar Lote de Telemetría

Realiza una inserción masiva en la tabla `device_telemetry`. Si el campo `time` se omite o es el valor cero, el servidor asigna automáticamente la hora actual (`time.Now()`). Las métricas se guardan en la columna `data` (JSONB).

> **Importante**: Cada `device_id` incluido en el lote debe existir previamente en la tabla `devices` debido a la restricción de llave foránea (`FOREIGN KEY`).

- **URL**: `/api/v1/telemetry`
- **Método**: `POST`
- **Content-Type**: `application/json`

El endpoint acepta dos formatos, que pueden mezclarse en el mismo lote.

#### Formato nuevo (recomendado)

Cada elemento tiene `device_id`, `time` (opcional) y `data`, un objeto con las claves que se necesiten. Se admiten valores anidados.

```json
[
  {
    "time": "2026-11-06T14:30:00Z",
    "device_id": "00008101-00123456789ABCDE",
    "data": {
      "battery_pct": 85.5,
      "battery_health_pct": 92.0,
      "temp_celsius": 31.4,
      "voltage_v": 4.125,
      "cycle_count": 340,
      "is_charging": true,
      "cpu_usage": 24.5,
      "ram_usage": 61.2,
      "sensor": {"gps": {"lat": -33.45, "lon": -70.66}}
    }
  }
]
```

#### Formato legado

Las métricas van en el nivel superior del elemento y no se envía `data` (o se envía `"data": null`). Se guardan las 8 claves (`battery_pct`, `battery_health_pct`, `temp_celsius`, `voltage_v`, `cycle_count`, `is_charging`, `cpu_usage`, `ram_usage`) dentro de `data`. Las que no se envían quedan en `0` (o `false` en `is_charging`), igual que con las columnas antiguas.

```json
[
  {
    "time": "2026-11-06T14:30:00Z",
    "device_id": "00008101-00123456789ABCDE",
    "battery_pct": 85.5,
    "battery_health_pct": 92.0,
    "temp_celsius": 31.4,
    "voltage_v": 4.125,
    "cycle_count": 340,
    "is_charging": true,
    "cpu_usage": 24.5,
    "ram_usage": 61.2
  }
]
```

#### Reglas de combinación

- Si `data` es un objeto (también `{}`), se guarda tal cual. Los campos legados enviados en el mismo elemento se agregan solo si su clave no existe en `data`, así que `data` gana en caso de conflicto.
- En el formato nuevo no se agregan con cero las métricas que no se envían.
- Si `data` no es un objeto (por ejemplo, un número o un arreglo), la petición se rechaza con `400`.
- Los valores numéricos ya no se validan contra la precisión de las columnas `NUMERIC(5,2)`: JSONB acepta cualquier número.

#### Respuestas

- **`201 Created`**:
  ```json
  {"status":"created", "records": 1}
  ```
- **`200 OK`**: Si se envía un arreglo vacío `[]`.
- **`400 Bad Request`**: JSON mal formado o `data` que no es un objeto.
- **`405 Method Not Allowed`**: Método HTTP distinto de `POST`.
- **`500 Internal Server Error`**: Error en la inserción masiva (por ejemplo, violación de llave foránea o pérdida de conexión con la base de datos).

#### Ejemplo con `curl`

```bash
curl -X POST http://localhost:8080/api/v1/telemetry \
  -H "Content-Type: application/json" \
  -d '[
    {
      "time": "2026-11-06T14:30:00Z",
      "device_id": "00008101-00123456789ABCDE",
      "data": {
        "battery_pct": 85.5,
        "battery_health_pct": 92.0,
        "temp_celsius": 31.4,
        "voltage_v": 4.125,
        "cycle_count": 340,
        "is_charging": true,
        "cpu_usage": 24.5,
        "ram_usage": 61.2
      }
    }
  ]'
```

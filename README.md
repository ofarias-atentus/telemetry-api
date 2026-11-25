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
├── .env.example     # Plantilla de variables de entorno (copiar a .env)
├── database.sql     # Script DDL para inicializar tablas, hypertable e índices
├── go.mod           # Definición del módulo y dependencias Go
├── go.sum           # Checksums de dependencias
├── main.go          # Punto de entrada: carga la configuración (.env y entorno), conecta a PostgreSQL y arranca el servidor
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
│   │   ├── schema_test.go       # Pruebas del seed de countries y de database.sql / migraciones
│   │   └── integration_test.go  # Prueba de integración (tag integration)
│   └── config/                  # Carga y validación de la configuración desde .env y variables de entorno
│       ├── config.go
│       └── config_test.go
├── migrations/
│   ├── 001_device_telemetry_jsonb.sql  # Mueve las métricas antiguas a la columna data
│   ├── 002_devices_attributes_jsonb.sql  # Crea hosts y mueve los campos de devices a attributes
│   └── 003_countries.sql  # Crea countries y convierte devices.country a código ISO
└── README.md        # Documentación del proyecto
```

---

## Configuración e Instalación

### 1. Preparar la Base de Datos

Crea la base de datos en PostgreSQL (por ejemplo, `attelemetry`) y ejecuta el script `database.sql` para habilitar `timescaledb` y crear las tablas `countries`, `hosts`, `devices` y `device_telemetry`:

```bash
psql -U <usuario> -h <host> -d attelemetry -f database.sql
```

#### Modelo de Datos

1. **Tabla `countries`**: Catálogo de países ISO 3166-1 alfa-2 (249 códigos). `devices.country` referencia su código.
   - `code` (`CHAR(2)`, PK): Código ISO en mayúsculas (ej. `CL`).
   - `name` (`VARCHAR(60)`, NOT NULL, UNIQUE): Nombre en español (ej. `Chile`, `Perú`).
   - `aliases` (`TEXT[]`): Otras grafías en minúsculas, como nombres en inglés o formas sin tildes (ej. `peru`, `usa`).
   - El `INSERT` de esta tabla está duplicado en `database.sql` y en `migrations/003_countries.sql`; un test verifica que ambos bloques sean idénticos.

2. **Tabla `hosts`**: Un registro por PC de monitoreo, compartido por todos sus dispositivos.
   - `host_pc` (`VARCHAR(100)`, PK): Nombre del equipo host.
   - `host_ip` (`VARCHAR(15)`, NOT NULL): Dirección IP del host.
   - `host_location` (`VARCHAR(50)`, NULL permitido): Ubicación física (ej. `Rack-Santiago-Fila-1`).
   - `updated_at` (`TIMESTAMPTZ`): Fecha y hora de la última actualización.

3. **Tabla `devices`**: Almacena el inventario del dispositivo. Los campos núcleo van en columnas y el resto en `attributes`.
   - `device_id` (`VARCHAR(64)`, PK): Identificador único del dispositivo (UDID / Serial).
   - `platform` (`VARCHAR(20)`, NOT NULL): Plataforma (`ios` o `android`).
   - `host_pc` (`VARCHAR(100)`, NOT NULL, FK -> `hosts.host_pc`): Equipo host al que está conectado.
   - `country` (`CHAR(2)`, NOT NULL, FK -> `countries.code`): País de ubicación como código ISO (ej. `CL`, `AR`, `PE`).
   - `status` (`VARCHAR(20)`): Estado operativo (`active`, `maintenance`, `retired`).
   - `attributes` (`JSONB`, NOT NULL, por defecto `{}`): Atributos libres, por ejemplo `alias_name` (alias del equipo), `model` (ej. `iPhone 12,1`), `os_version` (ej. `17.4`) y `battery_design_cap` (capacidad de fábrica en mAh).
   - `updated_at` (`TIMESTAMPTZ`): Fecha y hora de la última actualización.

4. **Hypertable `device_telemetry`**: Almacena las mediciones en series temporales.
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

Si la base de datos ya tiene `devices` con las columnas antiguas (`host_ip`, `host_location`, `alias_name`, `model`, `os_version` o `battery_design_cap`), aplica `migrations/002_devices_attributes_jsonb.sql`. Crea la tabla `hosts` con un registro por `host_pc`, mueve los campos del host a `hosts` y los atributos a `devices.attributes`, y luego añade la llave foránea. Es independiente de la 001, así que el orden entre ambas no importa. Todo se ejecuta en una sola transacción, con `lock_timeout` de 5 segundos:

```bash
psql -U <usuario> -h <host> -d attelemetry -f migrations/002_devices_attributes_jsonb.sql
```

> **Advertencias**
> - Haz un respaldo antes de aplicarla, por ejemplo con `pg_dump -U <usuario> -h <host> -t devices attelemetry > devices_backup.sql`. No hay migración inversa.
> - Detén la API y despliega el binario nuevo junto con la migración. El esquema antiguo y el nuevo no son compatibles.
> - Mientras corre la migración se detiene la ingesta de telemetría.
> - Las vistas que dependan de las columnas antiguas bloquean el `DROP COLUMN`; revísalas antes.
> - Si los dispositivos de un host discrepan en `host_ip` o `host_location`, el host toma los valores del dispositivo actualizado más recientemente. Una ubicación vacía pasa a `NULL`.
> - No es idempotente: una segunda ejecución falla en `CREATE TABLE` y revierte todo.

Si `devices.country` todavía guarda nombres en texto libre, aplica `migrations/003_countries.sql`. Crea la tabla `countries` con los 249 códigos ISO, convierte `devices.country` a `CHAR(2)` mapeando cada valor a su código y añade la llave foránea. Antes de cambiar nada verifica que todos los valores existentes se puedan mapear; si alguno no se puede, la migración falla y lista los valores (`Valores de country sin código ISO: ...`). Corrige o elimina esas filas y vuelve a ejecutarla. Es independiente de las migraciones 001 y 002, así que el orden no importa. Todo se ejecuta en una sola transacción, con `lock_timeout` de 5 segundos y `client_encoding` en UTF8:

```bash
psql -U <usuario> -h <host> -d attelemetry -f migrations/003_countries.sql
```

> **Advertencias**
> - Haz un respaldo antes de aplicarla, por ejemplo con `pg_dump -U <usuario> -h <host> -t devices attelemetry > devices_backup.sql`. No hay migración inversa.
> - Detén la API y despliega el binario nuevo junto con la migración. El binario antiguo no resuelve el país y el esquema nuevo rechaza nombres largos.
> - `ALTER TYPE` reescribe `devices` con bloqueo exclusivo: la ingesta de telemetría se detiene hasta el `COMMIT`.
> - Las vistas u otros objetos que dependan de `devices.country` hacen fallar `ALTER TYPE`; revísalos antes.
> - En bases con locale `C`, `lower()` solo pliega ASCII: un valor antiguo con tildes y en mayúsculas (por ejemplo `PERÚ`) se listaría como sin mapear. Corrígelo antes de migrar.
> - No es idempotente: una segunda ejecución falla en `CREATE TABLE` y revierte todo.

---

### 2. Configurar variables de entorno

La API lee su configuración de variables de entorno. Si existe un archivo `.env` en el directorio de trabajo actual, también se carga. Crea el archivo a partir de la plantilla y ajústalo:

```bash
cp .env.example .env
chmod 600 .env
```

```dotenv
SERVER_PORT=8080
DB_HOST=127.0.0.1
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=tu_password
DB_NAME=attelemetry
DB_SSLMODE=disable
DB_MAX_CONNS=25
DB_MIN_CONNS=5
```

| Variable | Obligatoria | Por defecto | Descripción |
|---|---|---|---|
| `SERVER_PORT` | No | `8080` | Puerto HTTP, de 1 a 65535. |
| `DB_HOST` | Sí | — | Host de PostgreSQL. Se admiten IPv6 entre corchetes, como `[::1]`. |
| `DB_PORT` | No | `5432` | Puerto de PostgreSQL, de 1 a 65535. |
| `DB_USER` | Sí | — | Usuario de PostgreSQL. |
| `DB_PASSWORD` | Sí | — | Contraseña del usuario. No se recortan sus espacios. |
| `DB_NAME` | Sí | — | Nombre de la base de datos. |
| `DB_SSLMODE` | No | `disable` | Modo SSL de la conexión a PostgreSQL. |
| `DB_MAX_CONNS` | No | `25` | Máximo de conexiones del pool. Debe ser al menos 1. |
| `DB_MIN_CONNS` | No | `5` | Mínimo de conexiones del pool. Debe estar entre 0 y `DB_MAX_CONNS`. |

#### Reglas de lectura

- Los valores se recortan de espacios al inicio y al final, excepto `DB_PASSWORD`.
- Las variables ya definidas en el entorno del proceso tienen prioridad sobre `.env`. Una variable exportada con valor vacío cuenta como no definida: no se usa su valor del `.env` y puede provocar el error de variables faltantes.
- Si un valor contiene `$`, `#` o espacios, ponlo entre comillas simples. godotenv expande `$VAR` en valores sin comillas o con comillas dobles, y `#` inicia un comentario en valores sin comillas:
  ```dotenv
  DB_PASSWORD='mi $ecreto #1'
  ```
- El usuario y la contraseña se escapan al construir la cadena de conexión, así que admiten caracteres especiales.
- Si falta una variable obligatoria o un valor no es válido, la API muestra todos los errores juntos al arrancar y termina. Por ejemplo:
  ```text
  Error crítico al leer la configuración de la API: faltan variables de entorno: DB_HOST, DB_USER, DB_PASSWORD, DB_NAME
  ```
- Si no existe `.env`, no es un error siempre que las variables estén definidas en el entorno.

> **Advertencias**
> - `.env` contiene credenciales reales. Está ignorado por git, no se versiona y no debe copiarse ni compartirse. Se recomienda permiso `600`.
> - El antiguo `config.json` sigue en el historial de git con credenciales reales. Cambia la contraseña de la base de datos.

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
2. Ejecutar el binario (asegúrate de que `.env` esté en el directorio de trabajo actual, o de que las variables estén definidas en el entorno):
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

Inserta nuevos dispositivos o actualiza los existentes si el `device_id` ya se encuentra registrado (`ON CONFLICT DO UPDATE`). Cada dispositivo también registra o actualiza su host en la tabla `hosts`.

- **URL**: `/api/v1/devices`
- **Método**: `POST`
- **Content-Type**: `application/json`

El endpoint acepta dos formatos, que pueden mezclarse en el mismo lote. `host_name`, `host_ip` y `host_location` van en el nivel superior en ambos.

#### Formato nuevo (recomendado)

Los atributos del dispositivo van en el objeto `attributes`, con las claves que se necesiten.

```json
[
  {
    "device_id": "00008101-00123456789ABCDE",
    "platform": "ios",
    "host_name": "macmini-santiago-01",
    "host_ip": "192.168.10.25",
    "host_location": "Rack-Santiago-Fila-1",
    "country": "Chile",
    "status": "active",
    "attributes": {
      "alias_name": "CL-IPH12-01",
      "model": "iPhone 12,1",
      "os_version": "17.4",
      "battery_design_cap": 3110,
      "lab": "Santiago"
    }
  }
]
```

#### Formato legado

Los atributos van en el nivel superior del elemento y no se envía `attributes` (o se envía `"attributes": null`). Los que no se envían se guardan como `""` o `0`.

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

#### Reglas de combinación y del host

- `host_name` se guarda como `hosts.host_pc` y como `devices.host_pc`. Cada dispositivo se guarda con una sola sentencia SQL que hace el upsert del host y luego del dispositivo, de forma atómica.
- **Host**: `host_ip` toma siempre el valor del último dispositivo enviado. `host_location` se conserva si el dispositivo no la envía o la envía vacía, porque la ubicación pertenece al host. Antes se sobrescribía con `""`.
- **Dispositivo**: `host_pc`, `country` y `status` se actualizan. `platform` nunca se actualiza.
- **`attributes`**: en el formato nuevo, cualquier clave es válida y se admiten valores anidados. Los campos legados enviados en el mismo elemento se agregan solo si su clave no existe en `attributes`, así que `attributes` gana en caso de conflicto.
- `model` y `battery_design_cap` ya guardados no cambian con un nuevo envío; solo se escriben si todavía no existen. `alias_name` y `os_version` sí se actualizan, así que un envío legado sin ellos los deja en `""`.
- Si `attributes` no es un objeto (por ejemplo, un número o un arreglo), la petición se rechaza con `400`.

#### Resolución de `country`

`country` acepta el código ISO o un nombre o alias de la tabla `countries`. La comparación no distingue mayúsculas ni minúsculas y recorta los espacios al inicio y al final. El servidor guarda siempre el código:

| Valor enviado | Se guarda |
|---|---|
| `Chile`, ` cl `, `CL` | `CL` |
| `Perú`, `peru`, `PERÚ` | `PE` |
| `usa` | `US` |

- Un valor desconocido (por ejemplo `Atlantis`) hace fallar la sentencia de ese dispositivo con un error de `NOT NULL`. Como es una sola sentencia, también se revierte el upsert de su host. El error se registra como `[ERROR] Upsert falló para dispositivo ...` y la respuesta sigue siendo `200`.
- Un dispositivo ya registrado que se envía con un país desconocido no se actualiza.
- Go convierte el valor a minúsculas antes de enviarlo a la base de datos, porque `lower()` de PostgreSQL solo pliega ASCII en bases con locale `C`.

#### Respuestas

- **`200 OK`**:
  ```json
  {"status":"success"}
  ```
- **`400 Bad Request`**: JSON inválido o `attributes` que no es un objeto.
- **`405 Method Not Allowed`**: Método HTTP distinto de `POST`.

#### Ejemplo con `curl`

```bash
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '[
    {
      "device_id": "00008101-00123456789ABCDE",
      "platform": "ios",
      "host_name": "macmini-santiago-01",
      "host_ip": "192.168.10.25",
      "host_location": "Rack-Santiago-Fila-1",
      "country": "Chile",
      "status": "active",
      "attributes": {
        "alias_name": "CL-IPH12-01",
        "model": "iPhone 12,1",
        "os_version": "17.4",
        "battery_design_cap": 3110
      }
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

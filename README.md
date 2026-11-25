# Telemetry API (`atdevice-api`)

API ingestora central escrita en **Go** para el registro de inventario y recolección masiva de telemetría de hardware (batería, temperatura, voltaje, CPU y RAM) de dispositivos móviles (**iOS** y **Android**) distribuidos en hosts de monitoreo. Utiliza **PostgreSQL** para el almacenamiento, con **TimescaleDB** opcional para las series temporales.

---

## Características Principales

- **Inventario de Dispositivos (`Upsert`)**: Registro y actualización continua del estado, versión de sistema operativo y ubicación física/lógica (host, IP, rack, país) de cada dispositivo.
- **Ingesta Masiva de Telemetría (`CopyFrom`)**: Inserción por lotes de alta velocidad utilizando el protocolo binario `COPY` de PostgreSQL a través de `pgx/v5`.
- **Series Temporales con TimescaleDB**: Si TimescaleDB está disponible, la telemetría se particiona automáticamente por tiempo (`hypertable`) e indexa por dispositivo y marca temporal descendente.
- **Migraciones con goose**: El esquema se versiona con migraciones embebidas en el binario, y `serve` se niega a arrancar si la base no está en la versión esperada.
- **Logs Estructurados (`log/slog`)**: Una línea por solicitud con `request_id`, método, ruta, estado y duración, y un `X-Request-ID` en cada respuesta para seguir una petición en los logs.

---

## Requisitos Previos

- **Go**: `1.26.8+` (según `go.mod`). No hace falta si solo se usa la imagen Docker.
- **PostgreSQL**: `14+`. **TimescaleDB** es opcional: si el servidor la tiene, la migración `00001` convierte `device_telemetry` en hypertable; si no, la tabla es una tabla normal. La API no usa funciones de TimescaleDB.
- **Docker** (opcional): para construir la imagen y para el despliegue con `docker compose`. Ver [Docker](#docker).

---

## Estructura del Proyecto

```text
telemetry-api/
├── .env.example          # Plantilla de variables de entorno (copiar a .env)
├── .dockerignore         # Lista blanca del contexto de build: go.mod, go.sum, cmd/ e internal/
├── Dockerfile            # Imagen en dos etapas (golang para compilar, distroless nonroot para ejecutar)
├── go.mod                # Definición del módulo y dependencias Go
├── go.sum                # Checksums de dependencias
├── cmd/
│   └── atdevice-api/            # Punto de entrada: solo la CLI
│       ├── main.go              # Despacho de subcomandos (serve, migrate, help) y códigos de salida
│       ├── serve.go             # serve: comprueba la conexión y el esquema, sirve la API y apaga ordenadamente
│       ├── migrate.go           # migrate status, up y baseline
│       └── main_test.go         # Pruebas de la CLI
├── internal/
│   ├── api/                     # Servidor HTTP: handlers, modelos, middleware de logs y pruebas
│   │   ├── server.go            # Interfaz DB, Server, NewServer, Routes y Handler
│   │   ├── middleware.go        # Registro de solicitudes, X-Request-ID y recuperación de panics
│   │   ├── middleware_test.go   # Pruebas del middleware
│   │   ├── devices.go           # Handler de /api/v1/devices y consulta de upsert
│   │   ├── telemetry.go         # Handler de /api/v1/telemetry
│   │   ├── models.go            # Structs DeviceInventory y TelemetryData, y payload() para la columna data
│   │   ├── models_test.go       # Pruebas de payload() y de su JSON
│   │   ├── fake_db_test.go      # Base de datos falsa para pruebas unitarias
│   │   ├── devices_test.go      # Pruebas unitarias de /api/v1/devices
│   │   ├── telemetry_test.go    # Pruebas unitarias de /api/v1/telemetry
│   │   ├── server_test.go       # Pruebas de enrutamiento
│   │   ├── schema_test.go       # Pruebas del seed de countries (00004) y del predicado de país
│   │   └── integration_test.go  # Prueba de integración de la API (tag integration)
│   ├── config/                  # Carga y validación de la configuración desde .env y variables de entorno
│   │   ├── config.go
│   │   ├── config_test.go
│   │   └── connstring_test.go   # Pruebas de las cadenas de conexión
│   ├── logging/                 # Construcción del logger slog (texto o JSON, nivel)
│   │   ├── logging.go
│   │   └── logging_test.go
│   └── migrate/                 # Migraciones con goose, embebidas en el binario
│       ├── migrate.go           # LatestVersion, Status, CheckVersion y lectura de la versión aplicada
│       ├── up.go                # Up: bloqueo advisory, rechazo de bases sin historial y search_path
│       ├── baseline.go          # Baseline: registra el historial de bases creadas con los scripts antiguos
│       ├── migrate_test.go      # Pruebas sin base de datos
│       ├── baseline_test.go     # Pruebas de la detección de versión
│       ├── integration_test.go  # Prueba de integración de migraciones (tag integration)
│       └── migrations/          # Migraciones SQL de goose (embebidas con go:embed)
│           ├── 00001_initial.sql               # Esquema original (TimescaleDB solo si está disponible)
│           ├── 00002_device_telemetry_jsonb.sql # Mueve las métricas antiguas a la columna data
│           ├── 00003_devices_attributes_jsonb.sql # Crea hosts y mueve los campos de devices a attributes
│           └── 00004_countries.sql             # Crea countries y convierte devices.country a código ISO
├── deploy/
│   ├── docker-compose.yml       # Compose de referencia para ejecutar la imagen
│   └── .env.docker.example      # Plantilla de .env para docker compose
├── installer/                   # Paquete .deb para Ubuntu 20.04 (ver installer/ubuntu/README.md)
│   ├── build-installer.py       # Genera el paquete
│   └── ubuntu/                  # Metadatos del paquete y unidad systemd
└── README.md            # Documentación del proyecto
```

---

## Configuración e Instalación

### 1. Preparar la Base de Datos

Crea una base de datos vacía en PostgreSQL (por ejemplo, `attelemetry`). El esquema lo crea el propio binario con sus migraciones, así que ya no hace falta ejecutar `psql -f database.sql`. Configura antes el archivo `.env` (paso 2) y luego:

```bash
./atdevice-api migrate status   # muestra la versión esperada y la aplicada
./atdevice-api migrate up       # crea el esquema completo en una base vacía
```

#### Modelo de Datos

1. **Tabla `countries`**: Catálogo de países ISO 3166-1 alfa-2 (249 códigos). `devices.country` referencia su código.
   - `code` (`CHAR(2)`, PK): Código ISO en mayúsculas (ej. `CL`).
   - `name` (`VARCHAR(60)`, NOT NULL, UNIQUE): Nombre en español (ej. `Chile`, `Perú`).
   - `aliases` (`TEXT[]`): Otras grafías en minúsculas, como nombres en inglés o formas sin tildes (ej. `peru`, `usa`).
   - El `INSERT` de esta tabla está solo en la migración `00004_countries.sql`. Un test verifica los 249 códigos, que los nombres sean únicos, que ningún código, nombre o alias apunte a dos países, y que el predicado de búsqueda coincida con el de la consulta de upsert.

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

4. **Tabla `device_telemetry`**: Almacena las mediciones en series temporales. Es una hypertable cuando TimescaleDB está disponible.
   - `time` (`TIMESTAMPTZ`, NOT NULL): Marca de tiempo de la muestra.
   - `device_id` (`VARCHAR(64)`, FK -> `devices.device_id`): Dispositivo asociado.
   - `data` (`JSONB`, NOT NULL, por defecto `{}`): Métricas como objeto JSON libre, por ejemplo `battery_pct`, `temp_celsius` o `cpu_usage`. El esquema no limita sus claves.

#### Migraciones

Las migraciones usan [goose](https://github.com/pressly/goose) (`github.com/pressly/goose/v3`) y van embebidas en el binario desde `internal/migrate/migrations/`, así que no hay archivos SQL que copiar. El historial se guarda en la tabla `goose_db_version`.

| Versión | Archivo | Qué hace |
|---|---|---|
| 1 | `00001_initial.sql` | Esquema original: `devices` y `device_telemetry` con columnas de métricas. Activa TimescaleDB y la hypertable solo si la extensión está disponible. |
| 2 | `00002_device_telemetry_jsonb.sql` | Mueve las 8 métricas antiguas a la columna `data` (JSONB) y elimina esas columnas. |
| 3 | `00003_devices_attributes_jsonb.sql` | Crea `hosts`, mueve `host_ip` y `host_location` ahí, mueve `alias_name`, `model`, `os_version` y `battery_design_cap` a `devices.attributes` y añade la FK de `devices.host_pc`. |
| 4 | `00004_countries.sql` | Crea `countries` (249 códigos ISO), convierte `devices.country` a `CHAR(2)` con FK y falla si algún valor existente no se puede mapear. |

La versión esperada por el binario es `LatestVersion` (`internal/migrate/migrate.go`), que hoy vale `4`. Cada cambio de esquema nuevo es un archivo numerado de forma correlativa y una subida de `LatestVersion`.

Comandos:

```bash
./atdevice-api migrate status     # sale con 0 solo si la base está al día
./atdevice-api migrate up         # aplica las pendientes (o crea el esquema en una base vacía)
./atdevice-api migrate baseline   # una sola vez, para bases creadas con los scripts SQL antiguos
```

- `migrate status` imprime la versión esperada y la aplicada. Sale con `0` si coinciden, y con `1` si la base está pendiente, adelantada o sin historial.
- `migrate up` toma un bloqueo advisory de PostgreSQL. Si otra ejecución ya está migrando, espera a que termine. La comprobación de historial se hace antes de ese bloqueo, así que conviene ejecutarlo desde un solo lugar a la vez.
- Si la base tiene tablas del esquema antiguo (`countries`, `device_telemetry`, `devices` o `hosts`) pero no tiene `goose_db_version`, `migrate up` se niega sin tocarla y pide ejecutar `migrate baseline`.
- `migrate baseline` detecta la versión (1 a 4) a partir del catálogo y crea `goose_db_version` con las filas 0 a N, todo en una transacción. No cambia nada más. Se niega si ya existe el historial, si la base está vacía o si el esquema no coincide con ninguna versión conocida. Después ejecuta `migrate up` para aplicar lo que falte.
- Las migraciones fijan `search_path` a `public`. Cada una corre en su propia transacción y son irreversibles: no tienen sección `Down`.
- Las migraciones `00002` a `00004` fijan `lock_timeout` en 5 segundos, así que si no obtienen los bloqueos a tiempo fallan en lugar de esperar. `00001` no lo fija.

#### Actualizar una base existente

1. Detén la API.
2. Haz un respaldo de la base: `pg_dump -U <usuario> -h <host> -d attelemetry > attelemetry_backup.sql`.
3. Despliega el binario nuevo junto con la migración.
4. Solo para bases creadas con los scripts `database.sql` y `migrations/` antiguos (sin `goose_db_version`), registra el historial una vez: `./atdevice-api migrate baseline`.
5. Aplica lo pendiente: `./atdevice-api migrate up`.
6. Inicia la API: `./atdevice-api serve`.

> **Advertencias**
> - Haz un respaldo antes de `migrate up`. No hay migración inversa.
> - Detén la API y despliega el binario nuevo junto con la migración. El binario antiguo inserta en columnas que ya no existen, y el esquema nuevo necesita columnas que el binario antiguo no conoce: ninguno funciona con el esquema del otro.
> - `serve` nunca migra. Si la versión del esquema no coincide, se niega a arrancar con código 1.
> - `00002` reescribe todas las filas de `device_telemetry`: usa más disco y WAL, y en una hypertable grande puede tardar. Las métricas con valor `NULL` no se copian a `data`.
> - `00003` y `00004` detienen la ingesta de telemetría hasta el `COMMIT`. `00004` cambia el tipo de `devices.country` con `ALTER TYPE`, que reescribe la tabla con bloqueo exclusivo.
> - Revisa antes la compresión de TimescaleDB (descomprime los chunks comprimidos que toque la migración) y los continuous aggregates o vistas que dependan de las columnas o de `devices.country`: bloquean `DROP COLUMN` y `ALTER TYPE`.
> - Si los dispositivos de un host discrepan en `host_ip` o `host_location`, `hosts` toma los valores del dispositivo actualizado más recientemente. Una ubicación vacía pasa a `NULL`.
> - `00004` falla si algún `devices.country` existente no se puede mapear a un código por código, nombre o alias, y lista los valores (`Valores de country sin código ISO: ...`). Corrige o elimina esas filas y vuelve a ejecutar. En bases con locale `C`, `lower()` solo pliega ASCII: un valor con tildes y en mayúsculas (por ejemplo `PERÚ`) se listaría como sin mapear, así que corrígelo antes.
> - Cada migración que falla hace rollback de sí misma; las anteriores ya aplicadas se quedan. Ejecutar de nuevo una migración ya aplicada no es posible porque el historial lo impide.
> - Las instalaciones nuevas no necesitan `baseline`: `migrate up` sobre una base vacía crea el esquema completo.

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
LOG_LEVEL=info
LOG_FORMAT=text
```

| Variable | Obligatoria | Por defecto | Descripción |
|---|---|---|---|
| `SERVER_PORT` | No | `8080` | Puerto HTTP, de 1 a 65535. `serve --port` lo sustituye. |
| `DB_HOST` | Sí | — | Host de PostgreSQL. Se admiten IPv6 entre corchetes, como `[::1]`. |
| `DB_PORT` | No | `5432` | Puerto de PostgreSQL, de 1 a 65535. |
| `DB_USER` | Sí | — | Usuario de PostgreSQL. |
| `DB_PASSWORD` | Sí | — | Contraseña del usuario. No se recortan sus espacios. |
| `DB_NAME` | Sí | — | Nombre de la base de datos. |
| `DB_SSLMODE` | No | `disable` | Modo SSL de la conexión a PostgreSQL. |
| `DB_MAX_CONNS` | No | `25` | Máximo de conexiones del pool. Debe ser al menos 1. |
| `DB_MIN_CONNS` | No | `5` | Mínimo de conexiones del pool. Debe estar entre 0 y `DB_MAX_CONNS`. |
| `LOG_LEVEL` | No | `info` | Nivel mínimo de log: `debug`, `info`, `warn` o `error`. No distingue mayúsculas. |
| `LOG_FORMAT` | No | `text` | Formato de los logs: `text` (líneas `clave=valor`) o `json`. No distingue mayúsculas. |

#### Reglas de lectura

- Los valores se recortan de espacios al inicio y al final, excepto `DB_PASSWORD`.
- Un valor no válido en `LOG_LEVEL` o `LOG_FORMAT` es un error de configuración, igual que los demás: se muestra junto con el resto al arrancar.
- Las variables ya definidas en el entorno del proceso tienen prioridad sobre `.env`. Una variable exportada con valor vacío cuenta como no definida: no se usa su valor del `.env` y puede provocar el error de variables faltantes.
- Si un valor contiene `$`, `#` o espacios, ponlo entre comillas simples. godotenv expande `$VAR` en valores sin comillas o con comillas dobles, y `#` inicia un comentario en valores sin comillas:
  ```dotenv
  DB_PASSWORD='mi $ecreto #1'
  ```
- El usuario y la contraseña se escapan al construir la cadena de conexión, así que admiten caracteres especiales.
- Si falta una variable obligatoria o un valor no es válido, la API registra todos los errores juntos en una línea `ERROR` al arrancar y termina con código 2. Esa línea no usa `LOG_FORMAT` ni `LOG_LEVEL` (ver [Errores de arranque](#errores-de-arranque)).
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
   go run ./cmd/atdevice-api serve
   ```

### Opción B: Compilar y ejecutar el binario

1. Compilar el binario `atdevice-api`:
   ```bash
   go build -o atdevice-api ./cmd/atdevice-api
   ```
2. Revisar o aplicar las migraciones (asegúrate de que `.env` esté en el directorio de trabajo actual, o de que las variables estén definidas en el entorno):
   ```bash
   ./atdevice-api migrate status
   ./atdevice-api migrate up
   ```
3. Iniciar la API:
   ```bash
   ./atdevice-api serve
   ```

### Subcomandos

| Comando | Descripción |
|---|---|
| `serve [--host HOST] [--port PUERTO]` | Comprueba la conexión y que el esquema esté en la versión esperada, y sirve la API. Nunca migra. Por defecto escucha en todas las interfaces y en `SERVER_PORT`. `--port` acepta de 1 a 65535 (0 usa `SERVER_PORT`). |
| `migrate status` | Compara la versión esperada con la aplicada (sale con `1` si no coinciden). |
| `migrate up` | Aplica las migraciones pendientes. |
| `migrate baseline` | Registra el historial en una base creada con los scripts SQL antiguos. |
| `help` | Muestra el uso. `-h` y `--help` hacen lo mismo. |

Ejecutar el binario sin argumentos muestra el uso y sale con código `2`.

Al iniciar correctamente, verás en consola (stderr):
```text
time=2026-11-25T15:06:20.102-03:00 level=INFO msg="API Ingestora central escuchando" addr=:8080
```

#### Códigos de salida

| Código | Significado |
|---|---|
| `0` | Correcto, ayuda, apagado normal o `migrate status` al día. |
| `1` | Versión del esquema distinta de la esperada: pendiente, adelantada o sin historial. También `migrate baseline` rechazado porque ya existe el historial. |
| `2` | Uso incorrecto, configuración, conectividad, permisos u otro error operativo. |

#### Servidor HTTP y apagado

- Límites del servidor: cabeceras (`ReadHeaderTimeout`) 10 s, lectura 30 s, escritura 30 s, inactividad 120 s y cabeceras máximas de 64 KiB.
- Al recibir `SIGINT` o `SIGTERM`, `serve` registra `señal de apagado recibida`, deja de aceptar conexiones y espera hasta 15 s a que terminen las peticiones en curso. Si no terminan a tiempo, cierra el servidor a la fuerza. Al final cierra el pool de PostgreSQL.

---

## Docker

El `Dockerfile` de la raíz construye la imagen en dos etapas: compila el binario con `golang:1.27.1-bookworm` (`CGO_ENABLED=0`, `GOTOOLCHAIN=local`) y lo copia a `gcr.io/distroless/static-debian12:nonroot`, que no tiene shell. El contenedor ejecuta `atdevice-api serve` por defecto y corre como usuario `nonroot`.

La configuración no va en la imagen. `.dockerignore` es una lista blanca (solo `go.mod`, `go.sum`, `cmd/` e `internal/`), así que el `.env` del repositorio nunca entra en el contexto de build. La configuración se pasa en tiempo de ejecución con `--env-file`; la plantilla está en `deploy/.env.docker.example`. Las comillas simples que la plantilla usa para valores con `$`, `#` o espacios solo aplican a Compose (`env_file`) y al `.env` de godotenv. `docker run --env-file` no quita comillas, así que ahí esos valores se escriben sin comillas.

```bash
docker build -t atdevice-api .

# Revisar o aplicar migraciones (serve nunca migra)
docker run --rm --env-file .env.docker atdevice-api migrate status
docker run --rm --env-file .env.docker atdevice-api migrate up

# Servir la API
docker run -d --name atdevice-api --env-file .env.docker -p 8080:8080 --stop-timeout 30 atdevice-api
```

Notas:
- Para una base creada con los scripts antiguos, el registro inicial es `docker run --rm --env-file .env.docker atdevice-api migrate baseline`, antes de `migrate up`.
- Si PostgreSQL está en el host, usa `DB_HOST=host.docker.internal`. En Linux añade `--add-host=host.docker.internal:host-gateway` al `docker run` (el compose ya lo incluye en `extra_hosts`). PostgreSQL debe aceptar conexiones desde la red de Docker (ver [installer/ubuntu/README.md](installer/ubuntu/README.md)).
- `--stop-timeout 30`: `serve` tarda hasta 15 s en apagarse, y el valor por defecto de Docker (10 s) es menor.
- No hay `HEALTHCHECK`: la imagen no tiene shell ni `curl`, y la API no tiene endpoint de salud.
- Los logs salen por stderr: `docker logs atdevice-api`.
- La API no tiene autenticación ni TLS. Publica el puerto solo en redes confiables o detrás de un proxy.

---

## Despliegue en Ubuntu

El repositorio incluye `deploy/docker-compose.yml` (referencia) y un paquete `.deb` para Ubuntu 20.04 en `installer/`, que se genera con `installer/build-installer.py`. El paquete instala en `/srv/atdevice-api`:

- `docker-compose.yml` y `.env` (copias de `deploy/`; nunca sobrescribe archivos existentes; `.env` con permisos `600`).
- La unidad systemd `atdevice-api.service`, que ejecuta `docker compose up`. Se reinicia si falla, salvo que salga con código 1: eso indica un esquema de base distinto al esperado, pero también errores de Compose como una imagen que no se descarga o un `.env` que falta (revisa `journalctl`). Permite 5 arranques por cada 60 s, y los códigos 130 y 143 cuentan como parada normal. En la primera instalación queda habilitada pero no iniciada.

La imagen que usa el compose es `atentuschile/atdevice-api` (`ATDEVICE_API_TAG`, `1.0.0` por defecto). Ese nombre de registro es un supuesto que hay que confirmar.

Primer despliegue, en resumen (el detalle está en [installer/ubuntu/README.md](installer/ubuntu/README.md)):

1. Instala antes `docker-ce` y `docker-compose-plugin`. El `.deb` solo depende de `debconf`.
2. Instala el paquete: `sudo dpkg -i atdevice-api_1.0.0_all.deb`.
3. Edita `/srv/atdevice-api/.env` con los datos de la base de datos.
4. Respalda la base con `pg_dump`, revisa el estado con `docker compose -f /srv/atdevice-api/docker-compose.yml run --rm api migrate status` y aplica `migrate up` (o `migrate baseline` seguido de `migrate up` en bases antiguas).
5. Inicia el servicio: `sudo systemctl start atdevice-api`.

Los logs están en el journal: `journalctl -u atdevice-api -f`. Al actualizar el paquete el servicio no se reinicia: queda detenido, y `postinst` actualiza `ATDEVICE_API_TAG` en `/srv/atdevice-api/.env` a la versión de imagen del paquete. El procedimiento (descargar la imagen, aplicar migraciones con respaldo previo si hacen falta y reiniciar el servicio) está en la sección «Actualización» de [installer/ubuntu/README.md](installer/ubuntu/README.md).

---

## Logs

Los logs usan `log/slog` de la biblioteca estándar y salen por stderr. El formato y el nivel se eligen con `LOG_FORMAT` y `LOG_LEVEL`.

Cada solicitud HTTP genera una línea `msg=solicitud` con estos atributos:

| Atributo | Descripción |
|---|---|
| `request_id` | Identificador de la solicitud (ver más abajo). |
| `method` | Método HTTP. |
| `path` | Ruta de la solicitud. |
| `status` | Código de respuesta. |
| `bytes` | Bytes enviados en el cuerpo de la respuesta. |
| `duration_ms` | Duración de la solicitud en milisegundos. |
| `remote_addr` | Dirección remota del cliente. |

El nivel depende del código de respuesta: `INFO` para `status` menor que 400, `WARN` para 4xx y `ERROR` para 5xx.

Ejemplo en formato `text` (`LOG_FORMAT=text`):
```text
time=2026-11-25T15:06:21.617-03:00 level=INFO msg=solicitud request_id=bcc29bd74cd484ca method=POST path=/api/v1/telemetry status=200 bytes=0 duration_ms=0.187 remote_addr=[::1]:56341
```

Ejemplo en formato `json` (`LOG_FORMAT=json`):
```json
{"time":"2026-11-25T15:06:21.617-03:00","level":"INFO","msg":"solicitud","request_id":"bcc29bd74cd484ca","method":"POST","path":"/api/v1/telemetry","status":200,"bytes":0,"duration_ms":0.187,"remote_addr":"[::1]:56341"}
```

#### Identificador de solicitud

- Si la petición trae la cabecera `X-Request-ID` con 1 a 64 caracteres del conjunto `[A-Za-z0-9._-]`, se conserva. En otro caso, el servidor genera un identificador de 16 caracteres hexadecimales.
- El identificador se devuelve en la cabecera `X-Request-ID` de la respuesta y aparece como `request_id` en todas las líneas de esa solicitud, también en los errores del handler.

#### Errores del handler

- Un dispositivo que falla en `POST /api/v1/devices` se registra como `upsert falló`, con los atributos `device_id` y `err`.
- Una inserción masiva fallida en `POST /api/v1/telemetry` se registra como `inserción masiva CopyFrom fallida`, con los atributos `records` y `err`.

Ejemplo de un lote de telemetría rechazado por una llave foránea. La última línea es el registro de la solicitud con el mismo `request_id`:
```text
time=2026-11-25T15:06:21.640-03:00 level=ERROR msg="inserción masiva CopyFrom fallida" request_id=3e0ab5a22f7c3f69 records=1 err="ERROR: insert or update on table \"device_telemetry\" violates foreign key constraint ..."
time=2026-11-25T15:06:21.641-03:00 level=ERROR msg=solicitud request_id=3e0ab5a22f7c3f69 method=POST path=/api/v1/telemetry status=500 bytes=0 duration_ms=12.904 remote_addr=[::1]:56355
```

#### Panics

Si un handler entra en pánico, se registra la línea `solicitud` en nivel `ERROR` con `status=500` y un atributo `panic`, aunque el handler ya hubiera enviado cabeceras. Después, `net/http` maneja el pánico como antes. Un `http.ErrAbortHandler` también se registra en nivel `ERROR` con `status=500`, pero sin el atributo `panic`.

#### Errores del servidor HTTP

Los errores internos de `http.Server` también pasan por `slog`, en nivel `ERROR`.

#### Errores de arranque

Los errores fatales de `serve` y de los subcomandos `migrate` se registran en una sola línea `ERROR` con el mensaje en español, y el proceso termina con código 1 o 2 (ver [Códigos de salida](#códigos-de-salida)). Si hay varios errores de configuración, se unen con `; ` en esa línea.

Los errores de configuración se registran antes de crear el logger, así que usan el manejador por defecto de `slog`: formato estándar de `log`, sin `LOG_FORMAT` ni `LOG_LEVEL`. Esto también aplica a `migrate`, que no configura el logger. Los errores de `serve` posteriores a la configuración (crear el pool, ping, versión del esquema, escuchar) sí siguen `LOG_FORMAT`.
```text
2026/11/25 15:09:14 ERROR Error crítico al leer la configuración de la API: faltan variables de entorno: DB_HOST, DB_USER, DB_PASSWORD, DB_NAME
```

> **Advertencias**
> - La línea `solicitud` solo tiene los atributos de la tabla; el cuerpo de la petición no se registra.
> - Las llamadas a la base de datos no usan el contexto de la solicitud, así que un cliente que se desconecta no cancela la inserción. El servidor sí tiene timeouts de lectura y escritura y apagado ordenado (ver [Servidor HTTP y apagado](#servidor-http-y-apagado)).

---

## Pruebas

### Pruebas unitarias

No requieren base de datos. Usan `httptest`, una base de datos falsa y pruebas de las migraciones sin conexión:

```bash
go vet ./...
gofmt -l .
go test ./...
go test -race ./...
go test -run TestHandleDevices ./internal/api/
```

### Pruebas de integración

#### API

Requiere PostgreSQL con la base ya migrada a la versión `4` (`migrate up`) y la variable `TEST_DATABASE_URL` con una URL `postgres://`. Si la variable no está definida, la prueba se omite. La prueba crea y elimina sus propias filas `itest-*`.

```bash
TEST_DATABASE_URL='postgres://usuario:password@127.0.0.1:5432/attelemetry?sslmode=disable' \
  go test -tags integration -run Integration ./internal/api/
```

#### Migraciones

Requiere una base de datos **vacía y desechable** en `TEST_MIGRATE_DATABASE_URL`, sin parámetros `pool_*` en la URL. La prueba aplica todas las migraciones, comprueba el estado y el rechazo de `baseline`, y no limpia la base al terminar.

```bash
TEST_MIGRATE_DATABASE_URL='postgres://usuario:password@127.0.0.1:5432/atdevice_scratch?sslmode=disable' \
  go test -tags integration -run Integration ./internal/migrate/
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

- Un valor desconocido (por ejemplo `Atlantis`) hace fallar la sentencia de ese dispositivo con un error de `NOT NULL`. Como es una sola sentencia, también se revierte el upsert de su host. El error se registra en nivel `ERROR` como `upsert falló`, con `device_id` y `err`, y la respuesta sigue siendo `200`.
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
- **`500 Internal Server Error`**: Error en la inserción masiva (por ejemplo, violación de llave foránea o pérdida de conexión con la base de datos). Se registra como `inserción masiva CopyFrom fallida`.

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

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Central ingestion API in Go (module `telemetry-api`) that stores device inventory and hardware telemetry (battery, temperature, voltage, CPU, RAM) from iOS and Android devices in PostgreSQL with TimescaleDB. The README calls the built binary `atdevice-api`.

`main.go` (repo root, `package main`) is a thin entry point: `run()` loads the configuration (`.env` plus environment variables), creates the pgx pool, pings it, and serves `api.NewServer(pool).Routes()` with `http.ListenAndServe`. The rest is in `internal/`:

- `internal/config`: `Config` types, `Load(envFile string)` (reads the optional `.env` file with godotenv, then the environment, and reports all validation errors together), and `DatabaseConfig.BuildConnString()`. Tests use an unexported `fromEnv(lookup)` and `t.Setenv`.
- `internal/api`: the `DB` interface (`Exec` and `CopyFrom`, which `*pgxpool.Pool` satisfies), `Server`/`NewServer`, and `Routes()`, which returns a dedicated `ServeMux`. Handlers are in `devices.go` and `telemetry.go`, models in `models.go`.

It uses stdlib `net/http` with no router. The direct dependencies are `github.com/jackc/pgx/v5` (`pgxpool`, `CopyFrom`) and `github.com/joho/godotenv` (`.env` loading).

`main.go` must stay at the repo root as `package main` and compile on its own, because the build is `go build -o atdevice-api main.go`. Put new code in `internal/` packages, not in sibling files at the root, or that build breaks. The `DB` interface is the test seam: handler tests inject a fake instead of a real pool.

Schema is in `database.sql`: `countries` (PK `code CHAR(2)`, ISO 3166-1 alpha-2, with Spanish `name` and lowercase `aliases`), `hosts` (PK `host_pc`, one row per monitoring PC), `devices` (PK `device_id`, `country CHAR(2)` FK to `countries`, `host_pc` FK to `hosts`, core columns plus `attributes JSONB`), and `device_telemetry` (columns `time`, `device_id`, and `data JSONB` for metrics; FK to `devices`, turned into a hypertable on `time`).

Go style and practice guides are in `.claude/skills/golang-*` (from samber/cc-skills-golang) and apply to `.go` files.

## Language

The README, log lines, and error messages are in Spanish. Keep new messages and docs consistent with that. This file is in English.

The startup error strings in `run()` (`main.go`) are capitalized Spanish on purpose. They are user-facing log output kept byte-identical, so linters' error-string rules (lowercase, no capital) do not apply. Do not lowercase them.

## Runtime behavior

- Configuration comes from environment variables, optionally loaded from `.env` in the current working directory, so run the binary from the repo root. `.env` holds real database credentials and is not tracked by git. Don't echo, read out, or copy its contents. Variables already set in the process environment win over `.env`; an exported empty value counts as unset. Required: `DB_HOST`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`. Defaults: `SERVER_PORT` 8080, `DB_PORT` 5432, `DB_SSLMODE` disable, `DB_MAX_CONNS` 25, `DB_MIN_CONNS` 5. Validation: ports 1–65535, `DB_MAX_CONNS` >= 1, and 0 <= `DB_MIN_CONNS` <= `DB_MAX_CONNS`. `.env.example` is the committed template.
- The old `config.json` is still in git history with real database credentials, so the DB password should be rotated. Don't put the password in docs or output.
- Startup loads the config, builds a pgx pool, pings the database, and exits if the config is invalid (all errors are reported together) or the ping fails. It then listens on `:<SERVER_PORT>` (all interfaces, default 8080). There is no auth and no TLS.
- `POST /api/v1/devices` takes a JSON array of `DeviceInventory`. `host_name`, `host_ip`, and `host_location` stay top-level in both formats. Each device is one SQL statement (a CTE upserts `hosts`, then `devices`, `ON CONFLICT ... DO UPDATE`), with no transaction across devices. Row errors are only logged; the handler still returns `200 {"status":"success"}`. `platform` is never updated. `host_ip` is overwritten by the latest device, and `host_location` is kept when a device sends nil or empty. Stored `model` and `battery_design_cap` are preserved when already present.
- Two device formats, which can be mixed. Legacy: `alias_name`, `model`, `os_version`, `battery_design_cap` at top level; unsent ones are stored as `""` or 0. New: `attributes` object with any keys; legacy fields that were sent are merged in only when their key is absent, so `attributes` wins. A non-object `attributes` gets 400. The logic is `DeviceInventory.attributes()` and `hostLocation()` in `internal/api/models.go`.
- `country` accepts an ISO code or a name/alias from `countries`, trimmed and case-insensitive (`'Chile'`, `' cl '`, `'Perú'`, `'peru'` all resolve). `handleDevices` lowercases it in Go before the SQL, because Postgres `lower()` only folds ASCII under a C locale. The SQL resolves it with a scalar subquery. An unknown value makes that device's statement fail with a NOT NULL error: the device and its host upsert are skipped and logged as `[ERROR] Upsert falló...`, and the response is still 200. An existing device sent with an unknown country is not updated.
- Known follow-ups, not fixed: a device that moves to another host leaves the old `hosts` row orphaned; a device sending an empty `host_ip` blanks the IP of its host for all its devices.
- `POST /api/v1/telemetry` takes a JSON array of `TelemetryData`, written with one `CopyFrom` call into `device_telemetry` (columns `time`, `device_id`, `data`), so the batch is all-or-nothing. Each row's `data` comes from `TelemetryData.payload()` in `internal/api/models.go`. An unknown `device_id` violates the FK and fails the whole batch with 500. An empty array returns 200 with no body. Success returns 201 `{"status":"created","records":N}`. A zero `time` is replaced with `time.Now()`.
- Two input formats, which can be mixed in one batch. Legacy: metric fields at the top level, no `data` (or `"data": null`); all 8 legacy keys are stored in `data`, unsent ones as 0/false. New: `data` is an object with any keys, nested values allowed. Legacy fields that were sent are merged in only when their key is absent from `data`, so `data` wins. A `data` that is not an object gets 400 with the decoder's message. Numbers are no longer range-checked by `NUMERIC` columns.
- Non-POST requests get 405. Malformed JSON gets 400, including a single object sent instead of an array.
- The JSON field `host_name` is stored in the `host_pc` column.
- `README.md` documents the endpoints; keep it in sync when handler behavior changes.

## Database

- Requires PostgreSQL 14+ with TimescaleDB. The app uses no TimescaleDB functions, so on plain PostgreSQL the `CREATE EXTENSION` and `create_hypertable` statements in `database.sql` can be skipped. This is how it has been run locally (PostgreSQL 16 without TimescaleDB).
- The upsert SQL (`upsertDeviceQuery` in `internal/api/devices.go`) is pinned by `TestUpsertDeviceQueryText`. Changing its columns, conflict clause, or SET list fails that test, so update both together.
- `migrations/` holds schema changes for existing databases, independent of each other. `001_device_telemetry_jsonb.sql` moves the 8 old metric columns into `data`. `002_devices_attributes_jsonb.sql` creates `hosts`, moves `host_ip` and `host_location` there, moves the 4 attribute fields into `devices.attributes`, and adds the FK. `003_countries.sql` creates `countries`, maps `devices.country` to ISO codes, and changes it to `CHAR(2)` with the FK; it refuses to run if any existing value cannot be mapped. All three run in one transaction, set `lock_timeout` to 5s, are not idempotent (a second run fails and rolls back), and have no reverse migration; back up with `pg_dump -t <table>` first. Every schema change goes in a new file there and in `database.sql`, which must keep describing the fresh-install shape.
- The `countries` seed is duplicated in `database.sql` and `migrations/003_countries.sql`. `schema_test.go` requires the two `INSERT` blocks to be identical, checks the exact 249 ISO codes and unique names, and fails if any code, name, or alias resolves to two countries. It also checks that the country matching predicate is the same in the upsert and in migration 003. Edit the seed in both files. `database.sql` starts with `SET client_encoding = 'UTF8'` for the accented names.
- Deploy ordering: stop the API and deploy the new binary together with the migration. The old binary inserts into dropped columns and the new one needs the new columns, so neither works against the other schema. Migrations 002 and 003 also stall telemetry ingestion while they run (003 until COMMIT, since `ALTER TYPE` rewrites `devices`), and dependent views block their `DROP COLUMN` or `ALTER TYPE`. Check TimescaleDB compression and continuous aggregates before running any migration.

## Commands

```bash
go mod download
cp .env.example .env               # then edit; .env is gitignored
go build -o atdevice-api main.go   # or: go run main.go
./atdevice-api                     # from the repo root, reads .env
psql -U <user> -h <host> -d attelemetry -f database.sql
psql -U <user> -h <host> -d attelemetry -f migrations/001_device_telemetry_jsonb.sql   # existing DBs with the old columns only
psql -U <user> -h <host> -d attelemetry -f migrations/002_devices_attributes_jsonb.sql  # existing DBs with the old devices columns only
psql -U <user> -h <host> -d attelemetry -f migrations/003_countries.sql  # existing DBs with free-text devices.country only
psql -U <user> -h <host> -d attelemetry -f migrations/003_countries.sql  # existing DBs with free-text devices.country only
go vet ./...
gofmt -l .
go test ./...
go test -race ./...
go test -run TestHandleDevices ./internal/api/   # single test
TEST_DATABASE_URL='postgres://user:pass@host:5432/attelemetry?sslmode=disable' \
  go test -tags integration -run Integration ./internal/api/
```

Unit tests use stdlib `testing` and `httptest` with a fake DB (`internal/api/fake_db_test.go`) and need no database. `schema_test.go` reads `database.sql` and the migration files, so it also needs no database. The integration test (`internal/api/integration_test.go`, build tag `integration`) skips when `TEST_DATABASE_URL` is unset, assumes `database.sql` is already applied, and creates and deletes its own `itest-*` rows.

There is no Makefile, linter config, or Dockerfile.

Quick check against a running server:

```bash
curl -i -X POST http://localhost:8080/api/v1/telemetry -H "Content-Type: application/json" -d '[]'   # expect 200
curl -i http://localhost:8080/api/v1/devices                                                       # expect 405
```

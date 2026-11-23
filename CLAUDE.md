# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Central ingestion API in Go (module `telemetry-api`) that stores device inventory and hardware telemetry (battery, temperature, voltage, CPU, RAM) from iOS and Android devices in PostgreSQL with TimescaleDB. The README calls the built binary `atdevice-api`.

`main.go` (repo root, `package main`) is a thin entry point: `run()` loads `config.json`, creates the pgx pool, pings it, and serves `api.NewServer(pool).Routes()` with `http.ListenAndServe`. The rest is in `internal/`:

- `internal/config`: `Config` types, `Load(path)`, and `DatabaseConfig.BuildConnString()`.
- `internal/api`: the `DB` interface (`Exec` and `CopyFrom`, which `*pgxpool.Pool` satisfies), `Server`/`NewServer`, and `Routes()`, which returns a dedicated `ServeMux`. Handlers are in `devices.go` and `telemetry.go`, models in `models.go`.

It uses stdlib `net/http` with no router. The only direct dependency is `github.com/jackc/pgx/v5` (`pgxpool`, `CopyFrom`).

`main.go` must stay at the repo root as `package main` and compile on its own, because the build is `go build -o atdevice-api main.go`. Put new code in `internal/` packages, not in sibling files at the root, or that build breaks. The `DB` interface is the test seam: handler tests inject a fake instead of a real pool.

Schema is in `database.sql`: `hosts` (PK `host_pc`, one row per monitoring PC), `devices` (PK `device_id`, FK `host_pc` to `hosts`, core columns plus `attributes JSONB`), and `device_telemetry` (columns `time`, `device_id`, and `data JSONB` for metrics; FK to `devices`, turned into a hypertable on `time`).

Go style and practice guides are in `.claude/skills/golang-*` (from samber/cc-skills-golang) and apply to `.go` files.

## Language

The README, log lines, and error messages are in Spanish. Keep new messages and docs consistent with that. This file is in English.

The startup error strings in `run()` (`main.go`) are capitalized Spanish on purpose. They are user-facing log output kept byte-identical, so linters' error-string rules (lowercase, no capital) do not apply. Do not lowercase them.

## Runtime behavior

- `config.json` is read from the current working directory, so run the binary from the repo root. It is tracked in git and holds real database credentials, so do not echo them in output or copy them elsewhere. Its `database.host` is `0.0.0.0`.
- Startup builds a pgx pool from `config.json`, pings the database, and exits if the ping fails. It then listens on `:<server.port>` (all interfaces, default 8080). There is no auth and no TLS.
- `POST /api/v1/devices` takes a JSON array of `DeviceInventory`. `host_name`, `host_ip`, and `host_location` stay top-level in both formats. Each device is one SQL statement (a CTE upserts `hosts`, then `devices`, `ON CONFLICT ... DO UPDATE`), with no transaction across devices. Row errors are only logged; the handler still returns `200 {"status":"success"}`. `platform` is never updated. `host_ip` is overwritten by the latest device, and `host_location` is kept when a device sends nil or empty. Stored `model` and `battery_design_cap` are preserved when already present.
- Two device formats, which can be mixed. Legacy: `alias_name`, `model`, `os_version`, `battery_design_cap` at top level; unsent ones are stored as `""` or 0. New: `attributes` object with any keys; legacy fields that were sent are merged in only when their key is absent, so `attributes` wins. A non-object `attributes` gets 400. The logic is `DeviceInventory.attributes()` and `hostLocation()` in `internal/api/models.go`.
- Known follow-ups, not fixed: a device that moves to another host leaves the old `hosts` row orphaned; a device sending an empty `host_ip` blanks the IP of its host for all its devices.
- `POST /api/v1/telemetry` takes a JSON array of `TelemetryData`, written with one `CopyFrom` call into `device_telemetry` (columns `time`, `device_id`, `data`), so the batch is all-or-nothing. Each row's `data` comes from `TelemetryData.payload()` in `internal/api/models.go`. An unknown `device_id` violates the FK and fails the whole batch with 500. An empty array returns 200 with no body. Success returns 201 `{"status":"created","records":N}`. A zero `time` is replaced with `time.Now()`.
- Two input formats, which can be mixed in one batch. Legacy: metric fields at the top level, no `data` (or `"data": null`); all 8 legacy keys are stored in `data`, unsent ones as 0/false. New: `data` is an object with any keys, nested values allowed. Legacy fields that were sent are merged in only when their key is absent from `data`, so `data` wins. A `data` that is not an object gets 400 with the decoder's message. Numbers are no longer range-checked by `NUMERIC` columns.
- Non-POST requests get 405. Malformed JSON gets 400, including a single object sent instead of an array.
- The JSON field `host_name` is stored in the `host_pc` column.
- `README.md` documents the endpoints; keep it in sync when handler behavior changes.

## Database

- Requires PostgreSQL 14+ with TimescaleDB. The app uses no TimescaleDB functions, so on plain PostgreSQL the `CREATE EXTENSION` and `create_hypertable` statements in `database.sql` can be skipped. This is how it has been run locally (PostgreSQL 16 without TimescaleDB).
- The upsert SQL (`upsertDeviceQuery` in `internal/api/devices.go`) is pinned by `TestUpsertDeviceQueryText`. Changing its columns, conflict clause, or SET list fails that test, so update both together.
- `migrations/` holds schema changes for existing databases. They are independent of each other. `001_device_telemetry_jsonb.sql` moves the 8 old metric columns into `data`. `002_devices_attributes_jsonb.sql` creates `hosts`, moves `host_ip` and `host_location` there, moves the 4 attribute fields into `devices.attributes`, and adds the FK. Both run in one transaction, set `lock_timeout` to 5s, are not idempotent (a second run fails and rolls back), and have no reverse migration; back up with `pg_dump -t <table>` first. Every schema change goes in a new file there and in `database.sql`, which must keep describing the fresh-install shape.
- Deploy ordering: stop the API and deploy the new binary together with the migration. The old binary inserts into dropped columns and the new one needs the new columns, so neither works against the other schema. Migration 002 also stalls telemetry ingestion while it runs, and dependent views block its `DROP COLUMN`. Check TimescaleDB compression and continuous aggregates before running either migration.

## Commands

```bash
go mod download
go build -o atdevice-api main.go   # or: go run main.go
./atdevice-api                     # from the repo root
psql -U <user> -h <host> -d attelemetry -f database.sql
psql -U <user> -h <host> -d attelemetry -f migrations/001_device_telemetry_jsonb.sql   # existing DBs with the old columns only
psql -U <user> -h <host> -d attelemetry -f migrations/002_devices_attributes_jsonb.sql  # existing DBs with the old devices columns only
go vet ./...
gofmt -l .
go test ./...
go test -race ./...
go test -run TestHandleDevices ./internal/api/   # single test
TEST_DATABASE_URL='postgres://user:pass@host:5432/attelemetry?sslmode=disable' \
  go test -tags integration -run Integration ./internal/api/
```

Unit tests use stdlib `testing` and `httptest` with a fake DB (`internal/api/fake_db_test.go`) and need no database. The integration test (`internal/api/integration_test.go`, build tag `integration`) skips when `TEST_DATABASE_URL` is unset, assumes `database.sql` is already applied, and creates and deletes its own `itest-*` rows.

There is no Makefile, linter config, or Dockerfile.

Quick check against a running server:

```bash
curl -i -X POST http://localhost:8080/api/v1/telemetry -H "Content-Type: application/json" -d '[]'   # expect 200
curl -i http://localhost:8080/api/v1/devices                                                       # expect 405
```

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Central ingestion API in Go (module `telemetry-api`) that stores device inventory and hardware telemetry (battery, temperature, voltage, CPU, RAM) from iOS and Android devices in PostgreSQL with TimescaleDB. The README calls the built binary `atdevice-api`.

All code is in `main.go` (`package main`): config loading, pgx pool setup, and both HTTP handlers. It uses stdlib `net/http` with no router. The only direct dependency is `github.com/jackc/pgx/v5` (`pgxpool`, `CopyFrom`).

Schema is in `database.sql`: `devices` (PK `device_id`) and `device_telemetry` (FK to `devices`, turned into a hypertable on `time`).

## Language

The README, log lines, and error messages are in Spanish. Keep new messages and docs consistent with that. This file is in English.

## Runtime behavior

- `config.json` is read from the current working directory, so run the binary from the repo root. It is tracked in git and holds real database credentials, so do not echo them in output or copy them elsewhere. Its `database.host` is `0.0.0.0`.
- Startup builds a pgx pool from `config.json`, pings the database, and exits if the ping fails. It then listens on `:<server.port>` (all interfaces, default 8080). There is no auth and no TLS.
- `POST /api/v1/devices` takes a JSON array of `DeviceInventory`. Each row is a separate upsert (`ON CONFLICT (device_id) DO UPDATE`) with no transaction. Row errors are only logged; the handler still returns `200 {"status":"success"}`. The upsert does not update `platform`, `model`, or `battery_design_cap`.
- `POST /api/v1/telemetry` takes a JSON array of `TelemetryData`, written with one `CopyFrom` call, so the batch is all-or-nothing. An unknown `device_id` violates the FK and fails the whole batch with 500. An empty array returns 200 with no body. Success returns 201 `{"status":"created","records":N}`. A zero `time` is replaced with `time.Now()`.
- Non-POST requests get 405. Malformed JSON gets 400, including a single object sent instead of an array.
- The JSON field `host_name` is stored in the `host_pc` column.
- `README.md` documents the endpoints; keep it in sync when handler behavior changes.

## Database

- Requires PostgreSQL 14+ with TimescaleDB. The app uses no TimescaleDB functions, so on plain PostgreSQL the `CREATE EXTENSION` and `create_hypertable` statements in `database.sql` can be skipped. This is how it has been run locally (PostgreSQL 16 without TimescaleDB).

## Commands

```bash
go mod download
go build -o atdevice-api main.go   # or: go run main.go
./atdevice-api                     # from the repo root
psql -U <user> -h <host> -d attelemetry -f database.sql
go vet ./...
gofmt -l .
```

There are no tests (no `*_test.go`), and no Makefile, linter config, or Dockerfile. When tests are added, use `go test ./...` or `go test -run <Name> ./...`.

Quick check against a running server:

```bash
curl -i -X POST http://localhost:8080/api/v1/telemetry -H "Content-Type: application/json" -d '[]'   # expect 200
curl -i http://localhost:8080/api/v1/devices                                                       # expect 405
```

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

const upsertDeviceQuery = `
		WITH host AS (
			INSERT INTO hosts (host_pc, host_ip, host_location, updated_at)
			VALUES ($3, $4, $5, NOW())
			ON CONFLICT (host_pc) DO UPDATE SET
				host_ip = EXCLUDED.host_ip,
				-- La ubicación es del host: un dispositivo que no la envía no debe borrarla.
				host_location = COALESCE(EXCLUDED.host_location, hosts.host_location),
				updated_at = NOW()
		)
		INSERT INTO devices (
			device_id, platform, host_pc, country, status, attributes, updated_at
		) VALUES ($1, $2, $3, (
			SELECT c.code FROM countries c
			WHERE c.code = upper(btrim($6::text))
				OR lower(c.name) = lower(btrim($6))
				OR lower(btrim($6)) = ANY(c.aliases)
		), $7, $8, NOW())
		ON CONFLICT (device_id) DO UPDATE SET
			host_pc = EXCLUDED.host_pc,
			country = EXCLUDED.country,
			status = EXCLUDED.status,
			attributes = devices.attributes || EXCLUDED.attributes
				|| jsonb_strip_nulls(jsonb_build_object(
					'model', devices.attributes->'model',
					'battery_design_cap', devices.attributes->'battery_design_cap')),
			updated_at = NOW();
	`

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var devices []DeviceInventory
	if err := json.NewDecoder(r.Body).Decode(&devices); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	log := s.loggerFrom(r.Context())

	var (
		stored      int
		failed      []failedDevice
		dataFailure = true
	)
	for _, d := range devices {
		// lower() de Postgres depende de la locale y en C solo pliega ASCII; Go pliega Unicode.
		country := strings.ToLower(strings.TrimSpace(d.Country))
		_, err := s.db.Exec(
			ctx,
			upsertDeviceQuery,
			d.DeviceID,
			d.Platform,
			d.HostName,
			d.HostIP,
			d.hostLocation(),
			country,
			d.Status,
			d.attributes(),
		)
		if err != nil {
			reason, isData := deviceFailureReason(err)
			level := slog.LevelError
			if isData {
				level = slog.LevelWarn
			}
			log.Log(ctx, level, "upsert falló", "device_id", d.DeviceID, "err", err)
			failed = append(failed, failedDevice{DeviceID: d.DeviceID, Error: reason})
			dataFailure = dataFailure && isData
			continue
		}
		stored++
	}

	if len(failed) == 0 {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"status":"success"}`)
		return
	}

	status, level := http.StatusUnprocessableEntity, slog.LevelWarn
	if !dataFailure {
		status, level = http.StatusInternalServerError, slog.LevelError
	}
	log.Log(ctx, level, "dispositivos rechazados", "stored", stored, "failed", len(failed))

	body, _ := json.Marshal(deviceErrorResponse{Status: "error", Stored: stored, Failed: failed})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

type failedDevice struct {
	DeviceID string `json:"device_id"`
	Error    string `json:"error"`
}

type deviceErrorResponse struct {
	Status string         `json:"status"`
	Stored int            `json:"stored"`
	Failed []failedDevice `json:"failed"`
}

// deviceFailureReason devuelve el motivo corto del rechazo y si la causa son los datos del dispositivo.
func deviceFailureReason(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || len(pgErr.Code) < 2 {
		return "error interno", false
	}
	switch {
	// Con ON CONFLICT solo puede venir de una carrera entre inserciones concurrentes: conviene reintentar.
	case pgErr.Code == "23505":
		return "error interno", false
	case pgErr.Code == "23502" && pgErr.ColumnName == "country":
		return "país desconocido", true
	case pgErr.Code == "22001":
		return "valor demasiado largo", true
	case pgErr.Code[:2] == "22" || pgErr.Code[:2] == "23":
		return "datos inválidos", true
	}
	return "error interno", false
}

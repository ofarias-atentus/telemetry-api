package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
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
		) VALUES ($1, $2, $3, $6, $7, $8, NOW())
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

	for _, d := range devices {
		_, err := s.db.Exec(
			ctx,
			upsertDeviceQuery,
			d.DeviceID,
			d.Platform,
			d.HostName,
			d.HostIP,
			d.hostLocation(),
			d.Country,
			d.Status,
			d.attributes(),
		)
		if err != nil {
			log.Printf("[ERROR] Upsert falló para dispositivo %s: %v", d.DeviceID, err)
		}
	}

	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"success"}`)
}

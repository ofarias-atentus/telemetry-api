package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
)

const upsertDeviceQuery = `
		INSERT INTO devices (
			device_id, alias_name, platform, model, os_version,
			host_pc, host_ip, host_location, country, battery_design_cap, status, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (device_id) DO UPDATE SET
			alias_name = EXCLUDED.alias_name,
			os_version = EXCLUDED.os_version,
			host_pc = EXCLUDED.host_pc,
			host_ip = EXCLUDED.host_ip,
			host_location = EXCLUDED.host_location,
			country = EXCLUDED.country,
			status = EXCLUDED.status,
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
			d.AliasName,
			d.Platform,
			d.Model,
			d.OSVersion,
			d.HostName,
			d.HostIP,
			d.HostLocation,
			d.Country,
			d.BatteryDesignCap,
			d.Status,
		)
		if err != nil {
			log.Printf("[ERROR] Upsert falló para dispositivo %s: %v", d.DeviceID, err)
		}
	}

	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"success"}`)
}

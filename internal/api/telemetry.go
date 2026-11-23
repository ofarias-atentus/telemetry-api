package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var batch []TelemetryData
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(batch) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctx := context.Background()
	copyCount, err := s.db.CopyFrom(
		ctx,
		pgx.Identifier{"device_telemetry"},
		[]string{
			"time",
			"device_id",
			"battery_pct",
			"battery_health_pct",
			"temp_celsius",
			"voltage_v",
			"cycle_count",
			"is_charging",
			"cpu_usage",
			"ram_usage",
		},
		pgx.CopyFromSlice(len(batch), func(i int) ([]any, error) {
			m := batch[i]
			if m.Time.IsZero() {
				m.Time = time.Now()
			}
			return []any{
				m.Time,
				m.DeviceID,
				m.BatteryPct,
				m.BatteryHealthPct,
				m.TempCelsius,
				m.VoltageV,
				m.CycleCount,
				m.IsCharging,
				m.CPUUsage,
				m.RAMUsage,
			}, nil
		}),
	)
	if err != nil {
		log.Printf("[ERROR] Inserción Masiva CopyFrom fallida: %v", err)
		http.Error(w, "Error guardando datos", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"status":"created", "records": %d}`, copyCount)
}

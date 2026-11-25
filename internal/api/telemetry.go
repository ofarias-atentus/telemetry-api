package api

import (
	"context"
	"encoding/json"
	"fmt"
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
			"data",
		},
		pgx.CopyFromSlice(len(batch), func(i int) ([]any, error) {
			m := batch[i]
			if m.Time.IsZero() {
				m.Time = time.Now()
			}
			return []any{m.Time, m.DeviceID, m.payload()}, nil
		}),
	)
	if err != nil {
		s.loggerFrom(r.Context()).Error("inserción masiva CopyFrom fallida", "records", len(batch), "err", err)
		http.Error(w, "Error guardando datos", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"status":"created", "records": %d}`, copyCount)
}

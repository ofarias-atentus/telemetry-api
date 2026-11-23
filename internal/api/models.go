package api

import "time"

// DeviceInventory describe un dispositivo registrado en el inventario.
type DeviceInventory struct {
	DeviceID         string `json:"device_id"`
	AliasName        string `json:"alias_name"`
	Platform         string `json:"platform"`
	Model            string `json:"model"`
	OSVersion        string `json:"os_version"`
	HostName         string `json:"host_name"`
	HostIP           string `json:"host_ip"`
	HostLocation     string `json:"host_location"`
	Country          string `json:"country"`
	BatteryDesignCap int    `json:"battery_design_cap"`
	Status           string `json:"status"`
}

// TelemetryData es una medición reportada por un dispositivo.
// Las métricas viajan en Data (formato nuevo) o en los campos sueltos
// (formato legado); son punteros para distinguir ausente de cero.
type TelemetryData struct {
	Time             time.Time      `json:"time"`
	DeviceID         string         `json:"device_id"`
	Data             map[string]any `json:"data"`
	BatteryPct       *float64       `json:"battery_pct"`
	BatteryHealthPct *float64       `json:"battery_health_pct"`
	TempCelsius      *float64       `json:"temp_celsius"`
	VoltageV         *float64       `json:"voltage_v"`
	CycleCount       *int           `json:"cycle_count"`
	IsCharging       *bool          `json:"is_charging"`
	CPUUsage         *float64       `json:"cpu_usage"`
	RAMUsage         *float64       `json:"ram_usage"`
}

// payload devuelve el documento que se guarda en la columna data.
// Sin data (formato legado) incluye las 8 métricas, con cero para las ausentes.
// Con data parte de una copia de ella y suma solo los campos legados enviados
// cuyas claves no existan; ante un conflicto gana data.
func (m TelemetryData) payload() map[string]any {
	legacy := []struct {
		key  string
		sent bool
		val  any
		zero any
	}{
		{"battery_pct", m.BatteryPct != nil, deref(m.BatteryPct), float64(0)},
		{"battery_health_pct", m.BatteryHealthPct != nil, deref(m.BatteryHealthPct), float64(0)},
		{"temp_celsius", m.TempCelsius != nil, deref(m.TempCelsius), float64(0)},
		{"voltage_v", m.VoltageV != nil, deref(m.VoltageV), float64(0)},
		{"cycle_count", m.CycleCount != nil, deref(m.CycleCount), 0},
		{"is_charging", m.IsCharging != nil, deref(m.IsCharging), false},
		{"cpu_usage", m.CPUUsage != nil, deref(m.CPUUsage), float64(0)},
		{"ram_usage", m.RAMUsage != nil, deref(m.RAMUsage), float64(0)},
	}

	out := make(map[string]any, len(m.Data)+len(legacy))
	if m.Data == nil {
		for _, l := range legacy {
			if l.sent {
				out[l.key] = l.val
			} else {
				out[l.key] = l.zero
			}
		}
		return out
	}

	for k, v := range m.Data {
		out[k] = v
	}
	for _, l := range legacy {
		if _, exists := out[l.key]; l.sent && !exists {
			out[l.key] = l.val
		}
	}
	return out
}

// deref devuelve *p, o el valor cero de T si p es nil.
func deref[T any](p *T) any {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

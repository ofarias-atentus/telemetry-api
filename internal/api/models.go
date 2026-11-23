package api

import "time"

// DeviceInventory describe un dispositivo registrado en el inventario.
// El host (host_name, host_ip, host_location) va a la tabla hosts y los campos
// núcleo a columnas de devices; el resto viaja en Attributes (formato nuevo) o
// en los campos sueltos (formato legado), que son punteros para distinguir
// ausente de cero.
type DeviceInventory struct {
	DeviceID         string         `json:"device_id"`
	Platform         string         `json:"platform"`
	HostName         string         `json:"host_name"`
	HostIP           string         `json:"host_ip"`
	Country          string         `json:"country"`
	Status           string         `json:"status"`
	Attributes       map[string]any `json:"attributes"`
	AliasName        *string        `json:"alias_name"`
	Model            *string        `json:"model"`
	OSVersion        *string        `json:"os_version"`
	HostLocation     *string        `json:"host_location"`
	BatteryDesignCap *int           `json:"battery_design_cap"`
}

// attributes devuelve el documento que se guarda en la columna attributes.
// Sin Attributes (formato legado) incluye las 4 claves, con "" o 0 para las ausentes.
// Con Attributes parte de una copia de ellos y suma solo los campos legados
// enviados cuyas claves no existan; ante un conflicto ganan Attributes.
func (d DeviceInventory) attributes() map[string]any {
	legacy := []struct {
		key  string
		sent bool
		val  any
		zero any
	}{
		{"alias_name", d.AliasName != nil, deref(d.AliasName), ""},
		{"model", d.Model != nil, deref(d.Model), ""},
		{"os_version", d.OSVersion != nil, deref(d.OSVersion), ""},
		{"battery_design_cap", d.BatteryDesignCap != nil, deref(d.BatteryDesignCap), 0},
	}

	out := make(map[string]any, len(d.Attributes)+len(legacy))
	if d.Attributes == nil {
		for _, l := range legacy {
			if l.sent {
				out[l.key] = l.val
			} else {
				out[l.key] = l.zero
			}
		}
		return out
	}

	for k, v := range d.Attributes {
		out[k] = v
	}
	for _, l := range legacy {
		if _, exists := out[l.key]; l.sent && !exists {
			out[l.key] = l.val
		}
	}
	return out
}

// hostLocation devuelve la ubicación del host, o nil (NULL) si no se envió o viene vacía.
func (d DeviceInventory) hostLocation() *string {
	if d.HostLocation == nil || *d.HostLocation == "" {
		return nil
	}
	return d.HostLocation
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

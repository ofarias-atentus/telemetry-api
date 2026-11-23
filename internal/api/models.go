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

// TelemetryData es una medición de hardware reportada por un dispositivo.
type TelemetryData struct {
	Time             time.Time `json:"time"`
	DeviceID         string    `json:"device_id"`
	BatteryPct       float64   `json:"battery_pct"`
	BatteryHealthPct float64   `json:"battery_health_pct"`
	TempCelsius      float64   `json:"temp_celsius"`
	VoltageV         float64   `json:"voltage_v"`
	CycleCount       int       `json:"cycle_count"`
	IsCharging       bool      `json:"is_charging"`
	CPUUsage         float64   `json:"cpu_usage"`
	RAMUsage         float64   `json:"ram_usage"`
}

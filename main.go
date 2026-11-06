package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Estructuras de Configuración
type Config struct {
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
}

type ServerConfig struct {
	Port int `json:"port"`
}

type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
	MaxConns int    `json:"max_conns"`
	MinConns int    `json:"min_conns"`
}

// Estructuras de Datos de Dominio
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

type Server struct {
	db *pgxpool.Pool
}

func loadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (dbCfg *DatabaseConfig) BuildConnString() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&pool_max_conns=%d&pool_min_conns=%d",
		dbCfg.User, dbCfg.Password, dbCfg.Host, dbCfg.Port, dbCfg.DBName,
		dbCfg.SSLMode, dbCfg.MaxConns, dbCfg.MinConns,
	)
}

func main() {
	// 1. Cargar archivo de configuración
	cfg, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Error crítico al leer config.json de la API: %v", err)
	}

	// 2. Construir DSN e inicializar Pool de conexiones
	dbURL := cfg.Database.BuildConnString()
	dbpool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Error al conectar a PostgreSQL con los datos del config: %v\n", err)
	}
	defer dbpool.Close()

	// Probar conexión inicial
	if err := dbpool.Ping(context.Background()); err != nil {
		log.Fatalf("No se pudo hacer Ping a la base de datos: %v\n", err)
	}

	srv := &Server{db: dbpool}

	// 3. Registrar Endpoints
	http.HandleFunc("/api/v1/devices", srv.handleDevices)
	http.HandleFunc("/api/v1/telemetry", srv.handleTelemetry)

	serverAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("API Ingestora central escuchando en el puerto %s...", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, nil))
}

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
	query := `
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

	for _, d := range devices {
		_, err := s.db.Exec(ctx, query,
			d.DeviceID, d.AliasName, d.Platform, d.Model, d.OSVersion,
			d.HostName, d.HostIP, d.HostLocation, d.Country, d.BatteryDesignCap, d.Status,
		)
		if err != nil {
			log.Printf("[ERROR] Upsert falló para dispositivo %s: %v", d.DeviceID, err)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

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
		[]string{"time", "device_id", "battery_pct", "battery_health_pct", "temp_celsius", "voltage_v", "cycle_count", "is_charging", "cpu_usage", "ram_usage"},
		pgx.CopyFromSlice(len(batch), func(i int) ([]any, error) {
			m := batch[i]
			if m.Time.IsZero() {
				m.Time = time.Now()
			}
			return []any{
				m.Time, m.DeviceID, m.BatteryPct, m.BatteryHealthPct,
				m.TempCelsius, m.VoltageV, m.CycleCount, m.IsCharging,
				m.CPUUsage, m.RAMUsage,
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

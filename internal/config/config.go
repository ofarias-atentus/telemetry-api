// Package config carga la configuración de la API desde un archivo JSON.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config agrupa la configuración del servidor y de la base de datos.
type Config struct {
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
}

// ServerConfig contiene los parámetros del servidor HTTP.
type ServerConfig struct {
	Port int `json:"port"`
}

// DatabaseConfig contiene los parámetros de conexión a PostgreSQL.
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

// Load lee y decodifica el archivo de configuración en filePath.
func Load(filePath string) (*Config, error) {
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

// BuildConnString devuelve el DSN de PostgreSQL para el pool de conexiones.
func (c DatabaseConfig) BuildConnString() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&pool_max_conns=%d&pool_min_conns=%d",
		c.User,
		c.Password,
		c.Host,
		c.Port,
		c.DBName,
		c.SSLMode,
		c.MaxConns,
		c.MinConns,
	)
}

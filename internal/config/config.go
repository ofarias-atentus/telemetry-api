// Package config carga la configuración de la API desde variables de entorno.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const maxPort = 65535

// Config agrupa la configuración del servidor y de la base de datos.
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
}

// ServerConfig contiene los parámetros del servidor HTTP.
type ServerConfig struct {
	Port int
}

// DatabaseConfig contiene los parámetros de conexión a PostgreSQL.
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
	MaxConns int
	MinConns int
}

// Load carga envFile (si existe) y lee la configuración del entorno.
// Las variables ya definidas en el entorno tienen prioridad sobre el archivo.
func Load(envFile string) (*Config, error) {
	if err := godotenv.Load(envFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no se pudo leer %s: %w", envFile, err)
	}
	return fromEnv(os.LookupEnv)
}

// fromEnv construye la configuración y reporta juntos todos los errores.
// Los valores se recortan con TrimSpace, salvo DB_PASSWORD; un valor vacío
// cuenta como no definido.
func fromEnv(lookup func(string) (string, bool)) (*Config, error) {
	var errs []error
	var missing []string

	get := func(key string) string {
		v, _ := lookup(key)
		if key == "DB_PASSWORD" {
			return v
		}
		return strings.TrimSpace(v)
	}
	required := func(key string) string {
		v := get(key)
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	intVar := func(key string, def, lo, hi int) (int, bool) {
		v := get(key)
		if v == "" {
			return def, true
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s inválido: %w", key, err))
			return def, false
		}
		if n < lo || n > hi {
			rango := fmt.Sprintf("%d-%d", lo, hi)
			if hi == math.MaxInt {
				rango = fmt.Sprintf("mínimo %d", lo)
			}
			errs = append(errs, fmt.Errorf("%s fuera de rango (%s): %d", key, rango, n))
			return def, false
		}
		return n, true
	}

	serverPort, _ := intVar("SERVER_PORT", 8080, 1, maxPort)
	dbPort, _ := intVar("DB_PORT", 5432, 1, maxPort)
	maxConns, maxOK := intVar("DB_MAX_CONNS", 25, 1, math.MaxInt)
	minConns, minOK := intVar("DB_MIN_CONNS", 5, 0, math.MaxInt)
	if maxOK && minOK && minConns > maxConns {
		errs = append(errs, fmt.Errorf("DB_MIN_CONNS (%d) no puede ser mayor que DB_MAX_CONNS (%d)", minConns, maxConns))
	}

	cfg := &Config{
		Server: ServerConfig{Port: serverPort},
		Database: DatabaseConfig{
			Host:     required("DB_HOST"),
			Port:     dbPort,
			User:     required("DB_USER"),
			Password: required("DB_PASSWORD"),
			DBName:   required("DB_NAME"),
			SSLMode:  get("DB_SSLMODE"),
			MaxConns: maxConns,
			MinConns: minConns,
		},
	}
	if cfg.Database.SSLMode == "" {
		cfg.Database.SSLMode = "disable"
	}

	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("faltan variables de entorno: %s", strings.Join(missing, ", ")))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cfg, nil
}

// BuildConnString devuelve el DSN de PostgreSQL para el pool de conexiones.
func (c DatabaseConfig) BuildConnString() string {
	q := url.Values{}
	q.Set("sslmode", c.SSLMode)
	q.Set("pool_max_conns", strconv.Itoa(c.MaxConns))
	q.Set("pool_min_conns", strconv.Itoa(c.MinConns))

	host := c.Host
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(host, strconv.Itoa(c.Port)),
		Path:     "/" + c.DBName,
		RawQuery: q.Encode(),
	}
	return u.String()
}

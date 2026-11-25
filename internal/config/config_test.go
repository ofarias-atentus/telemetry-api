package config

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mapLookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestFromEnv(t *testing.T) {
	t.Parallel()

	required := map[string]string{
		"DB_HOST":     "db.local",
		"DB_USER":     "u",
		"DB_PASSWORD": "p",
		"DB_NAME":     "d",
	}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range required {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	tests := []struct {
		name        string
		env         map[string]string
		want        Config
		wantErrPart []string
	}{
		{
			name: "all set",
			env: with(map[string]string{
				"SERVER_PORT":  "9090",
				"DB_PORT":      "6543",
				"DB_SSLMODE":   "require",
				"DB_MAX_CONNS": "10",
				"DB_MIN_CONNS": "2",
				"LOG_LEVEL":    "debug",
				"LOG_FORMAT":   "json",
			}),
			want: Config{
				Server: ServerConfig{Port: 9090},
				Database: DatabaseConfig{
					Host: "db.local", Port: 6543, User: "u", Password: "p", DBName: "d",
					SSLMode: "require", MaxConns: 10, MinConns: 2,
				},
				Log: LogConfig{Level: slog.LevelDebug, Format: "json"},
			},
		},
		{
			name: "defaults applied",
			env:  with(nil),
			want: Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "db.local", Port: 5432, User: "u", Password: "p", DBName: "d",
					SSLMode: "disable", MaxConns: 25, MinConns: 5,
				},
				Log: LogConfig{Level: slog.LevelInfo, Format: "text"},
			},
		},
		{
			name: "log level and format are case-insensitive and trimmed",
			env:  with(map[string]string{"LOG_LEVEL": " WARN ", "LOG_FORMAT": " Json\t"}),
			want: Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "db.local", Port: 5432, User: "u", Password: "p", DBName: "d",
					SSLMode: "disable", MaxConns: 25, MinConns: 5,
				},
				Log: LogConfig{Level: slog.LevelWarn, Format: "json"},
			},
		},
		{
			name: "log error level",
			env:  with(map[string]string{"LOG_LEVEL": "error"}),
			want: Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "db.local", Port: 5432, User: "u", Password: "p", DBName: "d",
					SSLMode: "disable", MaxConns: 25, MinConns: 5,
				},
				Log: LogConfig{Level: slog.LevelError, Format: "text"},
			},
		},
		{
			name:        "invalid log level",
			env:         with(map[string]string{"LOG_LEVEL": "verbose"}),
			wantErrPart: []string{`LOG_LEVEL inválido: "verbose" (debug, info, warn, error)`},
		},
		{
			name:        "invalid log format",
			env:         with(map[string]string{"LOG_FORMAT": "xml"}),
			wantErrPart: []string{`LOG_FORMAT inválido: "xml" (text, json)`},
		},
		{
			name: "invalid log values reported together",
			env:  with(map[string]string{"LOG_LEVEL": "x", "LOG_FORMAT": "y", "DB_PORT": "0"}),
			wantErrPart: []string{
				"LOG_LEVEL inválido",
				"LOG_FORMAT inválido",
				"DB_PORT fuera de rango",
			},
		},
		{
			name:        "missing required lists all names",
			env:         map[string]string{"DB_PASSWORD": "p"},
			wantErrPart: []string{"faltan variables de entorno: DB_HOST, DB_USER, DB_NAME"},
		},
		{
			name:        "invalid int",
			env:         with(map[string]string{"DB_PORT": "abc"}),
			wantErrPart: []string{"DB_PORT inválido"},
		},
		{
			name:        "invalid server port",
			env:         with(map[string]string{"SERVER_PORT": "80x"}),
			wantErrPart: []string{"SERVER_PORT inválido"},
		},
		{
			name: "empty string treated as unset",
			env: with(map[string]string{
				"SERVER_PORT": "",
				"DB_PORT":     "",
				"DB_SSLMODE":  "",
				"LOG_LEVEL":   "",
				"LOG_FORMAT":  "",
			}),
			want: Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "db.local", Port: 5432, User: "u", Password: "p", DBName: "d",
					SSLMode: "disable", MaxConns: 25, MinConns: 5,
				},
				Log: LogConfig{Level: slog.LevelInfo, Format: "text"},
			},
		},
		{
			name:        "empty required treated as missing",
			env:         with(map[string]string{"DB_HOST": ""}),
			wantErrPart: []string{"faltan variables de entorno: DB_HOST"},
		},
		{
			name: "values are trimmed",
			env: map[string]string{
				"DB_HOST":      "  db.local\t",
				"DB_USER":      " u ",
				"DB_PASSWORD":  " p ",
				"DB_NAME":      " d\n",
				"SERVER_PORT":  " 9090 ",
				"DB_PORT":      " 6543 ",
				"DB_SSLMODE":   " require ",
				"DB_MAX_CONNS": " 10 ",
				"DB_MIN_CONNS": " 2 ",
			},
			want: Config{
				Server: ServerConfig{Port: 9090},
				Database: DatabaseConfig{
					Host: "db.local", Port: 6543, User: "u", Password: " p ", DBName: "d",
					SSLMode: "require", MaxConns: 10, MinConns: 2,
				},
				Log: LogConfig{Level: slog.LevelInfo, Format: "text"},
			},
		},
		{
			name:        "blank value treated as unset",
			env:         with(map[string]string{"DB_HOST": "   "}),
			wantErrPart: []string{"faltan variables de entorno: DB_HOST"},
		},
		{
			name:        "server port too high",
			env:         with(map[string]string{"SERVER_PORT": "65536"}),
			wantErrPart: []string{"SERVER_PORT fuera de rango (1-65535): 65536"},
		},
		{
			name:        "db port zero",
			env:         with(map[string]string{"DB_PORT": "0"}),
			wantErrPart: []string{"DB_PORT fuera de rango (1-65535): 0"},
		},
		{
			name:        "db port too high",
			env:         with(map[string]string{"DB_PORT": "99999"}),
			wantErrPart: []string{"DB_PORT fuera de rango (1-65535): 99999"},
		},
		{
			name:        "max conns zero",
			env:         with(map[string]string{"DB_MAX_CONNS": "0"}),
			wantErrPart: []string{"DB_MAX_CONNS fuera de rango (mínimo 1): 0"},
		},
		{
			name:        "min conns negative",
			env:         with(map[string]string{"DB_MIN_CONNS": "-1"}),
			wantErrPart: []string{"DB_MIN_CONNS fuera de rango (mínimo 0): -1"},
		},
		{
			name:        "min greater than max",
			env:         with(map[string]string{"DB_MIN_CONNS": "30"}),
			wantErrPart: []string{"DB_MIN_CONNS (30) no puede ser mayor que DB_MAX_CONNS (25)"},
		},
		{
			name: "multiple errors reported together",
			env: map[string]string{
				"DB_USER":      "u",
				"DB_PASSWORD":  "p",
				"DB_NAME":      "d",
				"DB_PORT":      "99999",
				"DB_MAX_CONNS": "abc",
			},
			wantErrPart: []string{
				"faltan variables de entorno: DB_HOST",
				"DB_PORT fuera de rango (1-65535): 99999",
				"DB_MAX_CONNS inválido",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := fromEnv(mapLookup(tt.env))
			if len(tt.wantErrPart) > 0 {
				if err == nil {
					t.Fatalf("fromEnv() error = nil, want error")
				}
				for _, part := range tt.wantErrPart {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("error %q does not contain %q", err, part)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("fromEnv() unexpected error: %v", err)
			}
			if *got != tt.want {
				t.Errorf("fromEnv() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"DB_HOST", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_PORT",
		"DB_SSLMODE", "SERVER_PORT", "DB_MAX_CONNS", "DB_MIN_CONNS",
		"LOG_LEVEL", "LOG_FORMAT",
	}
	for _, k := range keys {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("Unsetenv(%s): %v", k, err)
		}
	}
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp env file: %v", err)
	}
	return path
}

func TestLoadQuotedPassword(t *testing.T) {
	base := "DB_HOST=h\nDB_USER=u\nDB_NAME=d\n"
	tests := []struct {
		name string
		line string
	}{
		{"single quoted", `DB_PASSWORD='p$a#ss'`},
		{"double quoted escaped dollar", `DB_PASSWORD="p\$a#ss"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			path := writeEnvFile(t, base+tt.line+"\n")

			got, err := Load(path)
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.Database.Password != "p$a#ss" {
				t.Errorf("Password = %q, want %q", got.Database.Password, "p$a#ss")
			}
		})
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	clearConfigEnv(t)

	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v no debería ser ErrNotExist", err)
	}
}

func TestLoadEnvFile(t *testing.T) {
	content := "DB_HOST=file.host\nDB_USER=fileuser\nDB_PASSWORD=filepass\nDB_NAME=filedb\nDB_PORT=6000\n"
	path := writeEnvFile(t, content)

	clearConfigEnv(t)
	t.Setenv("DB_HOST", "env.host")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.Database.Host != "env.host" {
		t.Errorf("Host = %q, want env.host (el entorno debe ganar al archivo)", got.Database.Host)
	}
	if got.Database.User != "fileuser" || got.Database.Port != 6000 {
		t.Errorf("valores del archivo no aplicados: %+v", got.Database)
	}
}

func TestLoadMissingFile(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DB_HOST", "h")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")
	t.Setenv("DB_NAME", "d")

	got, err := Load(filepath.Join(t.TempDir(), "no-existe.env"))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.Database.Host != "h" || got.Database.DBName != "d" {
		t.Errorf("Load() = %+v", got.Database)
	}
}

func TestBuildConnStringIPv6Host(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"::1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			cfg := DatabaseConfig{Host: host, Port: 5432, User: "u", Password: "p", DBName: "d", SSLMode: "disable", MaxConns: 10, MinConns: 2}
			u, err := url.Parse(cfg.BuildConnString())
			if err != nil {
				t.Fatalf("url.Parse() error: %v", err)
			}
			if u.Host != "[::1]:5432" {
				t.Errorf("Host = %q, want [::1]:5432", u.Host)
			}
		})
	}
}

func TestDatabaseConfigBuildConnString(t *testing.T) {
	t.Parallel()

	cfg := DatabaseConfig{
		Host:     "db.local",
		Port:     5432,
		User:     "us@er",
		Password: "p@ss:w/rd#1",
		DBName:   "telemetry",
		SSLMode:  "disable",
		MaxConns: 10,
		MinConns: 2,
	}

	u, err := url.Parse(cfg.BuildConnString())
	if err != nil {
		t.Fatalf("url.Parse() error: %v", err)
	}
	if u.Scheme != "postgres" {
		t.Errorf("Scheme = %q", u.Scheme)
	}
	if u.User.Username() != cfg.User {
		t.Errorf("User = %q, want %q", u.User.Username(), cfg.User)
	}
	if pw, _ := u.User.Password(); pw != cfg.Password {
		t.Errorf("Password = %q, want %q", pw, cfg.Password)
	}
	if u.Host != "db.local:5432" {
		t.Errorf("Host = %q", u.Host)
	}
	if u.Path != "/telemetry" {
		t.Errorf("Path = %q", u.Path)
	}
	q := u.Query()
	for k, want := range map[string]string{"sslmode": "disable", "pool_max_conns": "10", "pool_min_conns": "2"} {
		if got := q.Get(k); got != want {
			t.Errorf("query %s = %q, want %q", k, got, want)
		}
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	validJSON := `{
		"server": {"port": 8080},
		"database": {
			"host": "db.local",
			"port": 5432,
			"user": "u",
			"password": "p",
			"dbname": "d",
			"sslmode": "disable",
			"max_conns": 10,
			"min_conns": 2
		}
	}`

	tests := []struct {
		name    string
		content *string
		want    Config
		wantErr bool
	}{
		{
			name:    "valid json",
			content: &validJSON,
			want: Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host:     "db.local",
					Port:     5432,
					User:     "u",
					Password: "p",
					DBName:   "d",
					SSLMode:  "disable",
					MaxConns: 10,
					MinConns: 2,
				},
			},
		},
		{
			name:    "missing file",
			content: nil,
			wantErr: true,
		},
		{
			name:    "bad json",
			content: ptr("{not json"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.json")
			if tt.content != nil {
				if err := os.WriteFile(path, []byte(*tt.content), 0o600); err != nil {
					t.Fatalf("writing temp config: %v", err)
				}
			}

			got, err := Load(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if *got != tt.want {
				t.Errorf("Load() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestDatabaseConfigBuildConnString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  DatabaseConfig
		want string
	}{
		{
			name: "all fields",
			cfg: DatabaseConfig{
				Host:     "db.local",
				Port:     5432,
				User:     "user",
				Password: "secret",
				DBName:   "telemetry",
				SSLMode:  "disable",
				MaxConns: 10,
				MinConns: 2,
			},
			want: "postgres://user:secret@db.local:5432/telemetry?sslmode=disable&pool_max_conns=10&pool_min_conns=2",
		},
		{
			name: "zero value",
			cfg:  DatabaseConfig{},
			want: "postgres://:@:0/?sslmode=&pool_max_conns=0&pool_min_conns=0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.cfg.BuildConnString(); got != tt.want {
				t.Errorf("BuildConnString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }

package config

import (
	"net/url"
	"testing"
)

func TestDatabaseConfigConnString(t *testing.T) {
	t.Parallel()

	cfg := DatabaseConfig{
		Host:     "db.local",
		Port:     5433,
		User:     "us@er",
		Password: "p@ss:w/rd#1",
		DBName:   "telemetry",
		SSLMode:  "require",
		MaxConns: 10,
		MinConns: 2,
	}

	u, err := url.Parse(cfg.ConnString())
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
	if u.Host != "db.local:5433" {
		t.Errorf("Host = %q", u.Host)
	}
	if u.Path != "/telemetry" {
		t.Errorf("Path = %q", u.Path)
	}
	q := u.Query()
	if len(q) != 1 || q.Get("sslmode") != "require" {
		t.Errorf("query = %v, want only sslmode=require", q)
	}
}

func TestBuildConnStringBuildsOnConnString(t *testing.T) {
	t.Parallel()

	cfg := DatabaseConfig{Host: "::1", Port: 5432, User: "u", Password: "p", DBName: "d", SSLMode: "disable", MaxConns: 7, MinConns: 1}

	base, err := url.Parse(cfg.ConnString())
	if err != nil {
		t.Fatalf("url.Parse(ConnString) error: %v", err)
	}
	pool, err := url.Parse(cfg.BuildConnString())
	if err != nil {
		t.Fatalf("url.Parse(BuildConnString) error: %v", err)
	}
	if base.Host != pool.Host || base.Path != pool.Path || base.User.String() != pool.User.String() {
		t.Errorf("ConnString %q and BuildConnString %q differ outside the query", base, pool)
	}
	if got := pool.Query().Get("sslmode"); got != "disable" {
		t.Errorf("sslmode = %q", got)
	}
	if pool.Query().Get("pool_max_conns") != "7" || pool.Query().Get("pool_min_conns") != "1" {
		t.Errorf("pool params missing: %v", pool.Query())
	}
	if base.Query().Has("pool_max_conns") || base.Query().Has("pool_min_conns") {
		t.Errorf("ConnString must not carry pool params: %v", base.Query())
	}
}

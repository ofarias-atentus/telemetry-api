package migrate

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestEmbeddedMigrations(t *testing.T) {
	t.Parallel()

	fsys := MigrationsFS()
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	re := regexp.MustCompile(`^(\d{5})_[a-z0-9_]+\.sql$`)
	var versions []int64
	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			t.Fatalf("unexpected file %q", e.Name())
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		versions = append(versions, v)

		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", e.Name(), err)
		}
		body := string(raw)
		if !strings.Contains(body, "-- +goose Up") {
			t.Errorf("%s has no -- +goose Up", e.Name())
		}
		if strings.Contains(body, "-- +goose Down") {
			t.Errorf("%s must not have a Down section", e.Name())
		}
		for _, banned := range []string{`\set`, "BEGIN;", "COMMIT;"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s contains %q", e.Name(), banned)
			}
		}
	}

	if len(versions) == 0 {
		t.Fatal("no embedded migrations")
	}
	for i, v := range versions {
		if want := int64(i + 1); v != want {
			t.Errorf("versions[%d] = %d, want %d (contiguous from 1)", i, v, want)
		}
	}
	if got := versions[len(versions)-1]; got != LatestVersion {
		t.Errorf("highest embedded version = %d, LatestVersion = %d", got, LatestVersion)
	}
}

func TestStatementBeginEndBalanced(t *testing.T) {
	t.Parallel()

	fsys := MigrationsFS()
	entries, _ := fs.ReadDir(fsys, ".")
	for _, e := range entries {
		raw, _ := fs.ReadFile(fsys, e.Name())
		body := string(raw)
		if b, en := strings.Count(body, "-- +goose StatementBegin"), strings.Count(body, "-- +goose StatementEnd"); b != en {
			t.Errorf("%s: StatementBegin=%d StatementEnd=%d", e.Name(), b, en)
		}
		if got, want := strings.Count(body, "DO $$"), strings.Count(body, "-- +goose StatementBegin"); got != want {
			t.Errorf("%s: %d DO blocks but %d StatementBegin", e.Name(), got, want)
		}
	}
}

type fakeRow struct {
	val int64
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int64)) = r.val
	return nil
}

type fakeQuerier struct{ row fakeRow }

func (f fakeQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }

func TestAppliedVersion(t *testing.T) {
	t.Parallel()

	t.Run("value", func(t *testing.T) {
		t.Parallel()
		got, err := AppliedVersion(context.Background(), fakeQuerier{fakeRow{val: 3}})
		if err != nil || got != 3 {
			t.Errorf("got (%d, %v), want (3, nil)", got, err)
		}
	})

	t.Run("missing table maps to ErrNotVersioned", func(t *testing.T) {
		t.Parallel()
		_, err := AppliedVersion(context.Background(), fakeQuerier{fakeRow{err: &pgconn.PgError{Code: "42P01"}}})
		if !errors.Is(err, ErrNotVersioned) {
			t.Errorf("err = %v, want ErrNotVersioned", err)
		}
	})

	t.Run("other errors are not ErrNotVersioned", func(t *testing.T) {
		t.Parallel()
		_, err := AppliedVersion(context.Background(), fakeQuerier{fakeRow{err: &pgconn.PgError{Code: "42501"}}})
		if err == nil || errors.Is(err, ErrNotVersioned) {
			t.Errorf("err = %v, want a non-ErrNotVersioned error", err)
		}
	})
}

func TestCheckVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		row      fakeRow
		mismatch bool
		hint     string
	}{
		{"current", fakeRow{val: LatestVersion}, false, ""},
		{"behind", fakeRow{val: LatestVersion - 1}, true, "migrate up"},
		{"ahead", fakeRow{val: LatestVersion + 1}, true, "binario"},
		{"not versioned", fakeRow{err: &pgconn.PgError{Code: "42P01"}}, true, "migrate baseline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := CheckVersion(context.Background(), fakeQuerier{tc.row})
			if !tc.mismatch {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrVersionMismatch) {
				t.Fatalf("err = %v, want ErrVersionMismatch", err)
			}
			if !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("err = %q, want it to mention %q", err, tc.hint)
			}
		})
	}

	t.Run("connectivity error is not a mismatch", func(t *testing.T) {
		t.Parallel()
		err := CheckVersion(context.Background(), fakeQuerier{fakeRow{err: errors.New("connection refused")}})
		if err == nil || errors.Is(err, ErrVersionMismatch) {
			t.Errorf("err = %v, want a non-mismatch error", err)
		}
	})
}

func TestUnversionedError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		versioned bool
		existing  []string
		refuse    bool
	}{
		{"empty database", false, nil, false},
		{"versioned with tables", true, []string{"devices"}, false},
		{"versioned empty", true, nil, false},
		{"tables without history", false, []string{"devices", "hosts"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := unversionedError(tc.versioned, tc.existing)
			if tc.refuse {
				if !errors.Is(err, ErrVersionMismatch) || !strings.Contains(err.Error(), "migrate baseline") {
					t.Errorf("err = %v, want ErrVersionMismatch mentioning migrate baseline", err)
				}
			} else if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
}

func TestPinSearchPath(t *testing.T) {
	t.Parallel()

	got := pinSearchPath("postgres://u:p@h:5432/d?sslmode=disable")
	if !strings.Contains(got, "search_path=public") || !strings.Contains(got, "sslmode=disable") {
		t.Errorf("pinSearchPath = %q", got)
	}
}

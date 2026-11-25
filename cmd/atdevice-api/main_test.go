package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"telemetry-api/internal/migrate"
)

func exitCode(t *testing.T, err error) int {
	t.Helper()

	var xe *exitError
	if !errors.As(err, &xe) {
		t.Fatalf("err = %v, want *exitError", err)
	}
	return xe.code
}

func TestRunDispatch(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no args", nil, 2},
		{"unknown subcommand", []string{"bogus"}, 2},
		{"migrate without action", []string{"migrate"}, 2},
		{"unknown migrate action", []string{"migrate", "down"}, 2},
		{"migrate action with extra args", []string{"migrate", "status", "x"}, 2},
		{"serve unknown flag", []string{"serve", "--nope"}, 2},
		{"serve positional argument", []string{"serve", "extra"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.args)
			if err == nil {
				t.Fatal("err = nil, want exit error")
			}
			if got := exitCode(t, err); got != tc.want {
				t.Errorf("code = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			old := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			runErr := run([]string{arg})
			os.Stdout = old
			_ = w.Close()
			out, _ := io.ReadAll(r)

			if runErr != nil {
				t.Fatalf("err = %v, want nil", runErr)
			}
			if !strings.Contains(string(out), "atdevice-api serve") {
				t.Errorf("stdout = %q, want usage", out)
			}
		})
	}
}

func TestExitCodeFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"mismatch", migrate.ErrVersionMismatch, 1},
		{"wrapped mismatch", fmt.Errorf("x: %w", migrate.ErrVersionMismatch), 1},
		{"not versioned", fmt.Errorf("x: %w", migrate.ErrNotVersioned), 1},
		{"other", errors.New("connection refused"), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAsExitErrorKeepsCode(t *testing.T) {
	t.Parallel()

	inner := &exitError{code: 1, err: errors.New("x")}
	if got := exitCode(t, asExitError(fmt.Errorf("wrap: %w", inner))); got != 1 {
		t.Errorf("code = %d, want 1", got)
	}
	if asExitError(nil) != nil {
		t.Error("asExitError(nil) != nil")
	}
}

func TestServePortValidation(t *testing.T) {
	for _, port := range []string{"70000", "-1", "65536"} {
		t.Run(port, func(t *testing.T) {
			err := runServe([]string{"--port", port})
			if err == nil {
				t.Fatal("err = nil, want error")
			}
			if got := exitCode(t, err); got != 2 {
				t.Errorf("code = %d, want 2", got)
			}
		})
	}

	for _, port := range []int{0, 1, 8080, 65535} {
		if err := validatePort(port); err != nil {
			t.Errorf("validatePort(%d) = %v, want nil", port, err)
		}
	}
}

func TestPrintStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		applied int64
		err     error
		code    int
		want    string
	}{
		{"up to date", migrate.LatestVersion, nil, 0, "estado: al día"},
		{"pending", migrate.LatestVersion - 1, nil, 1, "estado: pendiente"},
		{"ahead", migrate.LatestVersion + 1, nil, 1, "posterior"},
		{"unversioned", 0, fmt.Errorf("x: %w", migrate.ErrNotVersioned), 1, "ninguna (sin historial)"},
		{"operational", 0, errors.New("permission denied"), 2, "versión esperada"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			err := printStatus(&buf, tc.applied, tc.err)
			if tc.code == 0 {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			} else if got := exitCode(t, err); got != tc.code {
				t.Errorf("code = %d, want %d", got, tc.code)
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("output = %q, want it to contain %q", buf.String(), tc.want)
			}
		})
	}
}

func TestPrintUp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printUp(&buf, nil)
	if want := fmt.Sprintf("ya está en la versión %d", migrate.LatestVersion); !strings.Contains(buf.String(), want) {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}

	buf.Reset()
	printUp(&buf, []int64{1, 2})
	if !strings.Contains(buf.String(), "1, 2") {
		t.Errorf("output = %q, want applied versions", buf.String())
	}
}

func TestNewHTTPServerLimits(t *testing.T) {
	t.Parallel()

	srv := newHTTPServer(":9999", http.NewServeMux(), nil)
	if srv.Addr != ":9999" {
		t.Errorf("Addr = %q", srv.Addr)
	}
	checks := []struct {
		name      string
		got, want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 10 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 30 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 30 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 120 * time.Second},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if srv.MaxHeaderBytes != 64<<10 {
		t.Errorf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, 64<<10)
	}
}

type fakeHTTPServer struct {
	mu          sync.Mutex
	stop        chan struct{}
	listenErr   error
	shutdownErr error
	shutdowns   int
	closes      int
	listening   chan struct{}
}

func newFakeHTTPServer() *fakeHTTPServer {
	return &fakeHTTPServer{stop: make(chan struct{}), listening: make(chan struct{})}
}

func (f *fakeHTTPServer) ListenAndServe() error {
	if f.listenErr != nil {
		return f.listenErr
	}
	close(f.listening)
	<-f.stop
	return http.ErrServerClosed
}

func (f *fakeHTTPServer) Shutdown(context.Context) error {
	f.mu.Lock()
	f.shutdowns++
	f.mu.Unlock()
	if f.shutdownErr != nil {
		return f.shutdownErr
	}
	f.release()
	return nil
}

func (f *fakeHTTPServer) Close() error {
	f.mu.Lock()
	f.closes++
	f.mu.Unlock()
	f.release()
	return nil
}

func (f *fakeHTTPServer) release() {
	select {
	case <-f.stop:
	default:
		close(f.stop)
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestServeLifetimeSignalTriggersShutdown(t *testing.T) {
	t.Parallel()

	srv := newFakeHTTPServer()
	sigs := make(chan os.Signal, 1)
	poolClosed := 0

	done := make(chan error, 1)
	go func() {
		done <- serveLifetime(quietLogger(), srv, sigs, func() { poolClosed++ }, time.Second)
	}()

	<-srv.listening
	sigs <- syscall.SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveLifetime did not return")
	}
	if srv.shutdowns != 1 {
		t.Errorf("Shutdown calls = %d, want 1", srv.shutdowns)
	}
	if srv.closes != 0 {
		t.Errorf("Close calls = %d, want 0", srv.closes)
	}
	if poolClosed != 1 {
		t.Errorf("pool closes = %d, want 1", poolClosed)
	}
}

func TestServeLifetimeShutdownFailureClosesServer(t *testing.T) {
	t.Parallel()

	srv := newFakeHTTPServer()
	srv.shutdownErr = context.DeadlineExceeded
	sigs := make(chan os.Signal, 1)
	poolClosed := 0

	done := make(chan error, 1)
	go func() {
		done <- serveLifetime(quietLogger(), srv, sigs, func() { poolClosed++ }, time.Second)
	}()

	<-srv.listening
	sigs <- os.Interrupt

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want it to wrap DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveLifetime did not return")
	}
	if srv.closes != 1 {
		t.Errorf("Close calls = %d, want 1", srv.closes)
	}
	if poolClosed != 1 {
		t.Errorf("pool closes = %d, want 1", poolClosed)
	}
}

func TestServeLifetimeListenErrorIsReturned(t *testing.T) {
	t.Parallel()

	srv := newFakeHTTPServer()
	srv.listenErr = errors.New("address already in use")
	poolClosed := 0

	err := serveLifetime(quietLogger(), srv, make(chan os.Signal), func() { poolClosed++ }, time.Second)
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("err = %v, want the listen error", err)
	}
	if srv.shutdowns != 0 {
		t.Errorf("Shutdown calls = %d, want 0", srv.shutdowns)
	}
	if poolClosed != 1 {
		t.Errorf("pool closes = %d, want 1", poolClosed)
	}
}

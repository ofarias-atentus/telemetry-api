package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	requestIDHeader = "X-Request-ID"
	maxRequestIDLen = 64
)

type loggerKey struct{}

// Handler devuelve Routes() envuelto con el log de acceso y el ID de petición.
func (s *Server) Handler() http.Handler {
	return s.accessLog(s.Routes())
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)

		logger := s.logger.With("request_id", id)
		ctx := context.WithValue(r.Context(), loggerKey{}, logger)
		r = r.WithContext(ctx)

		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			rec := recover()
			status := rw.status
			if rec != nil {
				status = http.StatusInternalServerError
			}

			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", rw.bytes),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("remote_addr", r.RemoteAddr),
			}
			if rec != nil && rec != http.ErrAbortHandler {
				attrs = append(attrs, slog.String("panic", fmt.Sprint(rec)))
			}
			logger.LogAttrs(ctx, level, "solicitud", attrs...)

			if rec != nil {
				panic(rec)
			}
		}()

		next.ServeHTTP(rw, r)
	})
}

// loggerFrom devuelve el logger con request_id del contexto, o el del servidor.
func (s *Server) loggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

func validRequestID(id string) bool {
	if len(id) < 1 || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	w.wroteHeader = true
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

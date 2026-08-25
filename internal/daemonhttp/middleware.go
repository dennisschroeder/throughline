// Package daemonhttp implements the security and diagnostics boundary in front of the
// Throughline daemon's loopback MCP endpoint: Host/Origin validation, bearer
// authentication, body/timeout limits, and a redacted access log. It authenticates and
// validates transport before any MCP parsing or workspace routing happens, per the accepted
// decision, and never exposes CORS.
package daemonhttp

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/credential"
)

type requestIDKey struct{}

// RequestIDFromContext returns the correlation id Protect attached to this request's
// context, or "" if the request did not pass through Protect (e.g. an in-process test using
// the MCP server directly).
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// DefaultMaxRequestBytes bounds request bodies; MCP tool payloads are small structured
// JSON, not file transfers.
const DefaultMaxRequestBytes = 4 << 20 // 4 MiB

// Config configures the security middleware. Token is the expected bearer credential;
// AllowedHosts lists the loopback host:port values (and bare hostnames) a request's Host
// and Origin headers must match. Logger receives one redacted access-log line per request;
// a nil Logger disables logging.
type Config struct {
	Token           string
	AllowedHosts    []string
	MaxRequestBytes int64
	Logger          *slog.Logger
}

// Protect wraps next with the daemon's transport security boundary. Order matters: Host and
// Origin are checked first (rejecting a request before it can even present a credential to
// try), then authentication, then the body-size limit; a request that fails any check never
// reaches next.
func Protect(cfg Config, next http.Handler) http.Handler {
	maxBytes := cfg.MaxRequestBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRequestBytes
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := newRequestID()
		w.Header().Set("X-Request-Id", requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID))

		status := http.StatusOK
		defer func() {
			logAccess(cfg.Logger, requestID, r, status, time.Since(start))
		}()

		if !hostAllowed(r.Host, cfg.AllowedHosts) {
			status = http.StatusForbidden
			http.Error(w, "forbidden", status)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, cfg.AllowedHosts) {
			status = http.StatusForbidden
			http.Error(w, "forbidden", status)
			return
		}
		if !authorized(r, cfg.Token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			status = http.StatusUnauthorized
			http.Error(w, "unauthorized", status)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		status = recorder.status
	})
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	return credential.Equal(token, strings.TrimPrefix(header, prefix))
}

// hostAllowed reports whether host (the request's Host header, host[:port]) matches one of
// allowed. An empty allowed list denies everything, closed by default.
func hostAllowed(host string, allowed []string) bool {
	return matchesAny(host, allowed)
}

// originAllowed reports whether a non-empty Origin header names a host in allowed. An
// absent Origin is handled by the caller (native MCP clients do not send one); a present,
// non-matching Origin is always rejected, with no CORS headers ever added.
func originAllowed(origin string, allowed []string) bool {
	host := origin
	if index := strings.Index(origin, "://"); index >= 0 {
		host = origin[index+3:]
	}
	return matchesAny(host, allowed)
}

func matchesAny(hostPort string, allowed []string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	for _, candidate := range allowed {
		candidateHost := candidate
		if h, _, err := net.SplitHostPort(candidate); err == nil {
			candidateHost = h
		}
		if strings.EqualFold(hostPort, candidate) || strings.EqualFold(host, candidateHost) {
			return true
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// logAccess writes one redacted line: request_id, method, path, status, and timing only.
// It never logs headers (the Authorization token above all), query strings, request or
// response bodies, or any filesystem path or provider locator a handler resolved.
func logAccess(logger *slog.Logger, requestID string, r *http.Request, status int, elapsed time.Duration) {
	if logger == nil {
		return
	}
	logger.Info("daemon_request",
		"request_id", requestID,
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", elapsed.Milliseconds(),
	)
}

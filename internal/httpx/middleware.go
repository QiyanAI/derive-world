// SPDX-License-Identifier: Apache-2.0
package httpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Middleware wraps an HTTP handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware in the order given.
func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestID accepts a valid inbound X-Request-Id or generates a UUIDv7.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if !requestIDPattern.MatchString(requestID) {
			id, err := uuid.NewV7()
			if err != nil {
				id = uuid.New()
			}
			requestID = id.String()
		}
		w.Header().Set("X-Request-Id", requestID)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), requestID)))
	})
}

// SecurityHeaders sets the frozen security header set.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// BodyLimit limits the request body to maxBytes.
func BodyLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxBytes > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TagRoute tags a handler with its ServeMux pattern for access logging.
func TagRoute(pattern string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithRoute(r.Context(), pattern)))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	errorCode   string
	actor       Actor
}

func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) SetErrorCode(code string) {
	w.errorCode = code
}

func (w *responseWriter) SetActor(actor Actor) {
	w.actor = actor
}

func (w *responseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responseWriter) WroteHeader() bool {
	return w.wroteHeader
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// AccessLog emits one structured line per request. It never logs the raw path,
// query, cookies, authorization headers, request bodies, or object content.
func AccessLog(logger *slog.Logger, ipPepper string, trustProxyHeaders bool) Middleware {
	if logger == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &responseWriter{ResponseWriter: w}
			next.ServeHTTP(recorder, r)

			route := r.Pattern
			if space := strings.IndexByte(route, ' '); space >= 0 {
				route = route[space+1:]
			}
			if route == "" {
				route = RouteFromContext(r.Context())
			}
			if route == "" {
				route = "unmatched"
			}
			actor := recorder.actor
			if actor.UserID == "" {
				actor = ActorFromContext(r.Context())
			}
			fields := []any{
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"route", route,
				"status", recorder.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"actor_user_id", actor.UserID,
				"actor_type", actor.Type,
				"ip_hash", HashIP(ipPepper, ClientIP(r, trustProxyHeaders)),
				"error_code", recorder.errorCode,
			}
			logger.Info("http_request", fields...)
		})
	}
}

// Recover converts a panic into a safe internal error response.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if logger != nil {
						logger.Error("panic_recovered",
							"request_id", RequestIDFromContext(r.Context()),
							"panic_type", fmt.Sprintf("%T", recovered),
						)
					}
					if recorder, ok := w.(*responseWriter); ok && recorder.WroteHeader() {
						return
					}
					WriteError(w, r, NewError(CodeInternal, "internal server error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS applies an exact-origin allow list. It never enables credentials and
// never emits a wildcard.
func CORS(allowedOrigins []string) Middleware {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := allowed[origin]; !ok {
				if r.Method == http.MethodOptions {
					WriteError(w, r, NewError(CodeForbidden, "origin is not allowed"))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Request-Id, Idempotency-Key, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the remote IP, optionally honoring trusted proxy headers.
func ClientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// HashIP returns a peppered SHA-256 prefix; raw IPs never enter logs.
func HashIP(pepper, ip string) string {
	sum := sha256.Sum256([]byte(pepper + "|" + ip))
	return hex.EncodeToString(sum[:8])
}

// HashIPBytes returns the full peppered SHA-256 digest used by audit_events.
func HashIPBytes(pepper, ip string) []byte {
	sum := sha256.Sum256([]byte(pepper + "|" + ip))
	return sum[:]
}

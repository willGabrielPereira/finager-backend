package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/response"
)

// statusRecorder captura o status code final para fins de log, sem alterar o comportamento do ResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// RequestLogger loga cada requisição (method, path, status, latência, request_id) via slog,
// e reporta panics e respostas 5xx ao Sentry (se configurado — sentry.CurrentHub() é no-op sem DSN).
//
// Aplicado globalmente em main.go, como camada mais externa (antes de CORS/SecurityHeaders),
// para capturar também falhas nesses middlewares.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			w.Header().Set("X-Request-ID", requestID)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			defer func() {
				if p := recover(); p != nil {
					sentry.CurrentHub().Recover(p)
					logger.Error("panic recuperado", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "panic", p)
					response.Error(rec, http.StatusInternalServerError, "E_INTERNAL", "Erro interno do servidor")
				}
			}()

			next.ServeHTTP(rec, r)

			attrs := []any{
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_ip", realIP(r),
			}

			if rec.status >= http.StatusInternalServerError {
				sentry.CaptureMessage(r.Method + " " + r.URL.Path + " respondeu " + http.StatusText(rec.status))
				logger.Error("request falhou", attrs...)
			} else {
				logger.Info("request", attrs...)
			}
		})
	}
}

// Package server provides HTTP middleware for the llm-bridge proxy.
package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// generateRequestID returns a UUID v4 string generated from crypto/rand.
func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---------------------------------------------------------------------------
// loggingResponseWriter
// ---------------------------------------------------------------------------

// loggingResponseWriter wraps http.ResponseWriter to capture status code,
// response byte count, and the backend server URL.
type loggingResponseWriter struct {
	http.ResponseWriter
	status       int
	wroteHeader  bool
	bytesWritten int
	serverURL    string
}

// WriteHeader delegates to the underlying writer and records the status.
func (rw *loggingResponseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

// Write delegates to the underlying writer, counting bytes.
// Implicitly writes 200 OK if WriteHeader was not called.
func (rw *loggingResponseWriter) Write(p []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(p)
	rw.bytesWritten += n
	return n, err
}

// BackendServerURL returns the backend server URL for this request, or "-"
// if not set.
func (rw *loggingResponseWriter) BackendServerURL() string {
	if rw.serverURL != "" {
		return rw.serverURL
	}
	if v := rw.Header().Get("X-Backend-Server"); v != "" {
		rw.serverURL = v
		return v
	}
	return "-"
}

// SetBackendServerURL sets the backend server URL for logging.
// Call from the handler before writing the response.
func (rw *loggingResponseWriter) SetBackendServerURL(url string) {
	rw.serverURL = url
	rw.Header().Set("X-Backend-Server", url)
}

// ---------------------------------------------------------------------------
// Logging middleware
// ---------------------------------------------------------------------------

// simpleRequestBody captures only the model field from a JSON request body.
type simpleRequestBody struct {
	Model string `json:"model"`
}

const modelExtractLimit = 4096

// extractModelWithBody peeks at the first modelExtractLimit bytes of body,
// decodes the model field, and returns a new io.ReadCloser that replays the
// consumed prefix plus the unconsumed remainder.
func extractModelWithBody(body io.ReadCloser) (string, io.ReadCloser, error) {
	limited := &io.LimitedReader{R: body, N: modelExtractLimit}
	var buf bytes.Buffer
	tee := io.TeeReader(limited, &buf)

	var sr simpleRequestBody
	err := json.NewDecoder(tee).Decode(&sr)

	// Always reconstruct the body from consumed + unconsumed, so the
	// original data is available to downstream handlers regardless of
	// whether model extraction succeeded.
	consumed := buf.Bytes()
	reconstructed := io.NopCloser(io.MultiReader(bytes.NewReader(consumed), body))

	if err != nil {
		return "-", reconstructed, fmt.Errorf("decode model from body: %w", err)
	}

	model := sr.Model
	if model == "" {
		model = "-"
	}

	return model, reconstructed, nil
}

// LoggingMiddleware returns chi-compatible middleware that logs each request
// as a single JSON line to stdout via slog.
func LoggingMiddleware() func(http.Handler) http.Handler {
	logger := slog.Default().With("component", "http")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := generateRequestID()
			start := time.Now()

			rw := &loggingResponseWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			// Request body size from Content-Length.
			requestBytes := 0
			if cl := r.Header.Get("Content-Length"); cl != "" {
				if n, err := strconv.Atoi(cl); err == nil {
					requestBytes = n
				}
			}

			// Try to extract model from POST body (limited reader, no full buffer).
			model := "-"
			if r.Method == http.MethodPost && r.Body != nil {
				var err error
				model, r.Body, err = extractModelWithBody(r.Body)
				if err != nil {
					logger.Warn("failed to extract model from request body",
						"error", err, "request_id", requestID)
				}
			}

			// Recover from handler panics.
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("handler panic recovered",
						"request_id", requestID, "panic", rec)
					http.Error(rw, "internal server error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(rw, r)

			// Default status 200 if handler never wrote headers.
			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}

			serverURL := rw.BackendServerURL()
			durationMs := time.Since(start).Seconds() * 1000

			logger.Info("request completed",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", durationMs,
				"request_bytes", requestBytes,
				"response_bytes", rw.bytesWritten,
				"server", serverURL,
				"model", model,
			)
		})
	}
}

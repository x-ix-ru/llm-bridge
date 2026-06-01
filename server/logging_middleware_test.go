package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	id2 := generateRequestID()

	// Each ID should match UUID v4 pattern: 8-4-4-4-12 hex groups.
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, id1)
	assert.NotEqual(t, id1, id2)
}

func TestMiddleware_LogsBasicFields(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/test/endpoint", "application/json",
		bytes.NewReader([]byte(`{"model":"gpt-4"}`)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestMiddleware_RequestResponseBytes(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Read and echo the body.
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append(body, []byte("echo")...))
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	requestBody := `{"model":"gpt-4","messages":[]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader([]byte(requestBody)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	assert.Equal(t, requestBody+"echo", string(respBody))
}

func TestMiddleware_ModelExtraction(t *testing.T) {
	var extractedModel string
	handler := func(w http.ResponseWriter, r *http.Request) {
		// The handler should still be able to read the body.
		body, _ := io.ReadAll(r.Body)
		var m map[string]interface{}
		json.Unmarshal(body, &m)
		extractedModel = m["model"].(string)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	// Send a POST request with model in body.
	resp, err := http.Post(ts.URL, "application/json",
		bytes.NewReader([]byte(`{"model":"llama-3.1","temperature":0.7}`)))
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "llama-3.1", extractedModel)
}

func TestMiddleware_ModelExtraction_NonPOST(t *testing.T) {
	var bodyRead bool
	handler := func(w http.ResponseWriter, r *http.Request) {
		bodyRead = r.Body != nil
		w.WriteHeader(http.StatusOK)
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/models")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, bodyRead)
}

func TestMiddleware_ModelExtraction_InvalidJSON(t *testing.T) {
	var bodyReceived string
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyReceived = string(body)
		w.WriteHeader(http.StatusOK)
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	nonJSON := `this is not json at all`
	resp, err := http.Post(ts.URL, "text/plain", bytes.NewReader([]byte(nonJSON)))
	require.NoError(t, err)
	resp.Body.Close()

	// Handler should still receive the body.
	assert.Equal(t, nonJSON, bodyReceived)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMiddleware_ModelExtraction_EmptyModel(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json",
		bytes.NewReader([]byte(`{"temperature":0.7}`)))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMiddleware_ResponseWriter_SetBackendServerURL(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Set backend server URL via the logging response writer.
		if lrw, ok := w.(*loggingResponseWriter); ok {
			lrw.SetBackendServerURL("http://backend:8000")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMiddleware_PanicRecovery(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		panic("intentional test panic")
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Panic should be recovered, response should be 500.
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "internal server error")
}

func TestMiddleware_ImplicitStatus200(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Write body without calling WriteHeader — implicit 200.
		_, _ = w.Write([]byte(`{"data":"hello"}`))
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMiddleware_CustomHeadersPreserved(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "test-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}

	ts := httptest.NewServer(
		LoggingMiddleware()(http.HandlerFunc(handler)),
	)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "test-value", resp.Header.Get("X-Custom"))
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

func TestExtractModelWithBody(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		expectModel string
		expectError bool
	}{
		{
			name:        "valid model",
			body:        `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`,
			expectModel: "gpt-4",
		},
		{
			name:        "empty model",
			body:        `{"messages":[]}`,
			expectModel: "-",
		},
		{
			name:        "invalid json",
			body:        `not json`,
			expectModel: "-",
			expectError: true,
		},
		{
			name:        "empty body",
			body:        ``,
			expectModel: "-",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := io.NopCloser(bytes.NewReader([]byte(tt.body)))
			model, reconstructed, err := extractModelWithBody(body)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectModel, model)

			// Reconstructed body should contain original content.
			if !tt.expectError {
				full, _ := io.ReadAll(reconstructed)
				assert.Equal(t, tt.body, string(full))
			}
			reconstructed.Close()
		})
	}
}

func TestExtractModelWithBody_LargeBody(t *testing.T) {
	// Create a body larger than the 4KB extraction limit.
	payload := make([]byte, 8192)
	for i := range payload {
		payload[i] = 'x'
	}
	// Prepend the model field so it's within the first 4KB.
	fullBody := `{"model":"big-model"}` + string(payload)

	body := io.NopCloser(bytes.NewReader([]byte(fullBody)))
	model, reconstructed, err := extractModelWithBody(body)
	require.NoError(t, err)
	assert.Equal(t, "big-model", model)

	// Reconstructed body should still contain all original data.
	full, _ := io.ReadAll(reconstructed)
	assert.Equal(t, len(fullBody), len(full))
	assert.Equal(t, fullBody, string(full))
	reconstructed.Close()
}

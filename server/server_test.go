package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"llm-bridge/backend"
	"llm-bridge/config"
	"llm-bridge/discovery"
	"llm-bridge/metrics"
	"llm-bridge/router"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// mockBackend creates an httptest.Server that behaves like a simple OpenAI
// API backend for the given models.  It responds to /v1/models for discovery
// and echoes /v1/chat/completions, /v1/completions and /v1/embeddings.
func mockBackend(t *testing.T, models []string, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/models":
				data := make([]map[string]string, len(models))
				for i, m := range models {
					data[i] = map[string]string{"id": m, "object": "model"}
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"object": "list",
					"data":   data,
				})
			case "/v1/chat/completions", "/v1/completions", "/v1/embeddings":
				var req map[string]interface{}
				json.NewDecoder(r.Body).Decode(&req)
				resp := map[string]interface{}{
					"id":      "mock-id",
					"object":  "chat.completion",
					"model":   req["model"],
					"choices": []map[string]interface{}{{"text": "mock response"}},
				}
				json.NewEncoder(w).Encode(resp)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}
	return httptest.NewServer(http.HandlerFunc(handler))
}

type testFixture struct {
	ts       *httptest.Server
	srv      *Server
	backends []*httptest.Server
	store    *config.Store
	pool     *backend.Pool
	disc     *discovery.Discovery
	cleanup  func()
}

func setupTest(t *testing.T) *testFixture {
	t.Helper()

	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "config.yaml"))
	require.NoError(t, store.Load())

	pool := backend.NewPool()
	disc := discovery.New(time.Hour)

	return &testFixture{
		store:   store,
		pool:    pool,
		disc:    disc,
		cleanup: func() {},
	}
}

func (f *testFixture) addBackend(t *testing.T, models []string, handler func(w http.ResponseWriter, r *http.Request)) string {
	t.Helper()
	srv := mockBackend(t, models, handler)
	f.backends = append(f.backends, srv)

	// Update config with backend URL.
	cfg := f.store.Get()
	cfg.Servers = append(cfg.Servers, config.ServerConfig{
		URL:                   srv.URL,
		Distance:              1,
		MaxConcurrentRequests: 5,
	})
	require.NoError(t, f.store.Set(cfg))

	f.pool.AddServer(cfg.Servers[len(cfg.Servers)-1])

	urls := make([]string, len(cfg.Servers))
	for i, sc := range cfg.Servers {
		urls[i] = sc.URL
	}
	f.disc.SetServers(urls)
	require.NoError(t, f.disc.Discover(context.Background()))

	return srv.URL
}

func (f *testFixture) start(t *testing.T) {
	t.Helper()

	rtr := router.New(f.store, f.disc, f.pool)
	f.srv = New(f.store, f.disc, f.pool, rtr, nil, ":0")
	f.ts = httptest.NewServer(f.srv.mux)

	f.cleanup = func() {
		f.ts.Close()
		for _, b := range f.backends {
			b.Close()
		}
	}
}

func (f *testFixture) do(method, path string, body []byte) (*http.Response, []byte) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, f.ts.URL+path, reqBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGetModels(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4", "gpt-3.5-turbo"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/v1/models", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &result))
	assert.Equal(t, "list", result["object"])

	data, ok := result["data"].([]interface{})
	require.True(t, ok)

	modelNames := make(map[string]bool)
	for _, item := range data {
		entry, ok := item.(map[string]interface{})
		require.True(t, ok)
		modelNames[entry["id"].(string)] = true
	}
	assert.True(t, modelNames["gpt-4"])
	assert.True(t, modelNames["gpt-3.5-turbo"])
}

func TestChatCompletions(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	body := map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	resp, respBody := f.do("POST", "/v1/chat/completions", bodyBytes)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(respBody, &result))
	assert.Equal(t, "mock-id", result["id"])
}

func TestChatCompletionsStreaming(t *testing.T) {
	f := setupTest(t)

	// Backend that returns SSE.
	_ = f.addBackend(t, []string{"gpt-4"}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data":   []map[string]string{{"id": "gpt-4", "object": "model"}},
			})
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			require.True(t, ok)
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
			flusher.Flush()
		}
	})
	f.start(t)
	defer f.cleanup()

	body := map[string]interface{}{
		"model":    "gpt-4",
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}
	bodyBytes, _ := json.Marshal(body)

	resp, respBody := f.do("POST", "/v1/chat/completions", bodyBytes)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Contains(t, string(respBody), "data:")
}

func TestChatCompletionsUnknownModel(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	body := map[string]interface{}{
		"model": "unknown-model",
	}
	bodyBytes, _ := json.Marshal(body)

	resp, respBody := f.do("POST", "/v1/chat/completions", bodyBytes)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var errResp map[string]interface{}
	require.NoError(t, json.Unmarshal(respBody, &errResp))
	errObj, ok := errResp["error"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, errObj["message"], "no servers available")
}

func TestChatCompletionsMissingModel(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	body := map[string]interface{}{"messages": []map[string]string{{"role": "user", "content": "hi"}}}
	bodyBytes, _ := json.Marshal(body)

	resp, _ := f.do("POST", "/v1/chat/completions", bodyBytes)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCompletions(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-3.5-turbo"}, nil)
	f.start(t)
	defer f.cleanup()

	body := map[string]interface{}{"model": "gpt-3.5-turbo", "prompt": "hello"}
	bodyBytes, _ := json.Marshal(body)

	resp, _ := f.do("POST", "/v1/completions", bodyBytes)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestEmbeddings(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"text-embedding-ada-002"}, nil)
	f.start(t)
	defer f.cleanup()

	body := map[string]interface{}{"model": "text-embedding-ada-002", "input": "hello"}
	bodyBytes, _ := json.Marshal(body)

	resp, _ := f.do("POST", "/v1/embeddings", bodyBytes)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Admin: server CRUD
// ---------------------------------------------------------------------------

func TestAdminListServers(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/servers", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var servers []map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &servers))
	require.Len(t, servers, 1)

	srv := servers[0]
	assert.Equal(t, f.backends[0].URL, srv["url"])
	assert.Equal(t, float64(1), srv["distance"])
	assert.Equal(t, float64(5), srv["max_concurrent_requests"])
	assert.Equal(t, true, srv["healthy"])
	assert.Equal(t, float64(0), srv["inflight"])
}

func TestAdminAddServer(t *testing.T) {
	f := setupTest(t)
	f.start(t)
	defer f.cleanup()

	// Create a real backend that will be added.
	backendSrv := mockBackend(t, []string{"gpt-4"}, nil)
	defer backendSrv.Close()

	addBody := map[string]interface{}{
		"url":                     backendSrv.URL,
		"distance":                2,
		"max_concurrent_requests": 10,
	}
	bodyBytes, _ := json.Marshal(addBody)

	resp, respBody := f.do("POST", "/admin/servers", bodyBytes)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created config.ServerConfig
	require.NoError(t, json.Unmarshal(respBody, &created))
	assert.Equal(t, backendSrv.URL, created.URL)
	assert.Equal(t, 2, created.Distance)
	assert.Equal(t, 10, created.MaxConcurrentRequests)

	// Verify it appears in the list.
	resp, body := f.do("GET", "/admin/servers", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var servers []map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &servers))
	require.Len(t, servers, 1)
	assert.Equal(t, backendSrv.URL, servers[0]["url"])

	// Verify it's in the pool.
	_, ok := f.pool.GetServer(backendSrv.URL)
	assert.True(t, ok)

	// Verify duplicate is rejected.
	resp, _ = f.do("POST", "/admin/servers", bodyBytes)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestAdminAddServerInvalid(t *testing.T) {
	f := setupTest(t)
	f.start(t)
	defer f.cleanup()

	tests := []struct {
		name string
		body map[string]interface{}
	}{
		{"missing url", map[string]interface{}{"distance": 1, "max_concurrent_requests": 5}},
		{"invalid distance", map[string]interface{}{"url": "http://x:8000", "distance": 0, "max_concurrent_requests": 5}},
		{"invalid max", map[string]interface{}{"url": "http://x:8000", "distance": 1, "max_concurrent_requests": 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			resp, _ := f.do("POST", "/admin/servers", bodyBytes)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestAdminGetServer(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/servers/"+f.backends[0].URL, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var srv serverResponse
	require.NoError(t, json.Unmarshal(body, &srv))
	assert.Equal(t, f.backends[0].URL, srv.URL)
	assert.Equal(t, 1, srv.Distance)
	assert.Equal(t, true, srv.Healthy)

	// Non-existent server.
	resp, _ = f.do("GET", "/admin/servers/http://nonexistent:9999", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAdminUpdateServer(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	update := map[string]interface{}{
		"distance":                5,
		"max_concurrent_requests": 20,
	}
	bodyBytes, _ := json.Marshal(update)

	resp, respBody := f.do("PUT", "/admin/servers/"+f.backends[0].URL, bodyBytes)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var updated config.ServerConfig
	require.NoError(t, json.Unmarshal(respBody, &updated))
	assert.Equal(t, f.backends[0].URL, updated.URL)
	assert.Equal(t, 5, updated.Distance)
	assert.Equal(t, 20, updated.MaxConcurrentRequests)

	// Non-existent server.
	resp, _ = f.do("PUT", "/admin/servers/http://nonexistent:9999", bodyBytes)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAdminDeleteServer(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	// Delete the server.
	resp, _ := f.do("DELETE", "/admin/servers/"+f.backends[0].URL, nil)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify it's gone from the list.
	resp, body := f.do("GET", "/admin/servers", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var servers []map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &servers))
	assert.Empty(t, servers)

	// Verify it's removed from the pool.
	_, ok := f.pool.GetServer(f.backends[0].URL)
	assert.False(t, ok)

	// Delete non-existent server.
	resp, _ = f.do("DELETE", "/admin/servers/http://nonexistent:9999", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Admin: config
// ---------------------------------------------------------------------------

func TestAdminGetConfig(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/config", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(body, &cfg))
	require.Len(t, cfg.Servers, 1)
	assert.Equal(t, f.backends[0].URL, cfg.Servers[0].URL)
}

func TestAdminPutConfigJSON(t *testing.T) {
	f := setupTest(t)
	f.start(t)
	defer f.cleanup()

	// Create a backend for the new config.
	backendSrv := mockBackend(t, []string{"gpt-4"}, nil)
	defer backendSrv.Close()

	newCfg := config.Config{
		Global: config.GlobalConfig{
			FallbackStrategy:     config.FallbackBestEffort,
			DiscoveryIntervalSec: 30,
			RequestTimeoutSec:    120,
			QueueTimeoutSec:      60,
		},
		Servers: []config.ServerConfig{
			{URL: backendSrv.URL, Distance: 1, MaxConcurrentRequests: 5},
		},
	}
	bodyBytes, _ := json.Marshal(newCfg)

	req, _ := http.NewRequest("PUT", f.ts.URL+"/admin/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, respBody := f.httpDo(req)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var returned config.Config
	require.NoError(t, json.Unmarshal(respBody, &returned))
	assert.Equal(t, config.FallbackBestEffort, returned.Global.FallbackStrategy)
	assert.Len(t, returned.Servers, 1)
}

func TestAdminPutConfigYAML(t *testing.T) {
	f := setupTest(t)
	f.start(t)
	defer f.cleanup()

	backendSrv := mockBackend(t, []string{"gpt-4"}, nil)
	defer backendSrv.Close()

	yamlBody := `
global:
  fallback_strategy: queue
  discovery_interval_sec: 10
  request_timeout_sec: 30
servers:
  - url: ` + "`" + backendSrv.URL + "`" + `
    distance: 2
    max_concurrent_requests: 8
`
	// Use raw string for the URL.
	yamlBody = "global:\n  fallback_strategy: queue\n  discovery_interval_sec: 10\n  request_timeout_sec: 30\nservers:\n  - url: " + backendSrv.URL + "\n    distance: 2\n    max_concurrent_requests: 8\n"

	req, _ := http.NewRequest("PUT", f.ts.URL+"/admin/config", bytes.NewReader([]byte(yamlBody)))
	req.Header.Set("Content-Type", "application/yaml")
	resp, _ := f.httpDo(req)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAdminPutConfigInvalid(t *testing.T) {
	f := setupTest(t)
	f.start(t)
	defer f.cleanup()

	req, _ := http.NewRequest("PUT", f.ts.URL+"/admin/config", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := f.httpDo(req)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Admin: status
// ---------------------------------------------------------------------------

func TestAdminStatus(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/status", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var status map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &status))

	servers, ok := status["servers"].([]interface{})
	require.True(t, ok)
	require.Len(t, servers, 1)

	models, ok := status["models"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, models, "gpt-4")

	healthy, ok := status["healthy"].(map[string]interface{})
	require.True(t, ok)
	assert.True(t, healthy[f.backends[0].URL].(bool))
}

func TestAdminStatusInflight(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	// Acquire a connection to increase inflight count.
	conn, err := f.pool.Acquire(context.Background(), f.backends[0].URL)
	require.NoError(t, err)
	defer f.pool.Release(conn)

	resp, body := f.do("GET", "/admin/status", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var status map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &status))

	servers, _ := status["servers"].([]interface{})
	srv := servers[0].(map[string]interface{})
	assert.Equal(t, float64(1), srv["inflight"])
}

func TestAdminStatusUnhealthy(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	// Close the backend to make it unhealthy.
	f.backends[0].Close()

	// The discovery won't know it's unhealthy until the next poll.
	// Manually set it unhealthy.
	f.disc.SetServers([]string{f.backends[0].URL})
	require.NoError(t, f.disc.Discover(context.Background()))

	resp, body := f.do("GET", "/admin/status", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var status map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &status))

	servers, _ := status["servers"].([]interface{})
	srv := servers[0].(map[string]interface{})
	assert.False(t, srv["healthy"].(bool))
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestChatCompletionsAllServersBusy(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	// Fill all capacity on the only server.
	conn, err := f.pool.Acquire(context.Background(), f.backends[0].URL)
	require.NoError(t, err)
	defer f.pool.Release(conn)

	// Also acquire 4 more to fill the 5-capacity server.
	conns := make([]*backend.Conn, 4)
	for i := range conns {
		c, err := f.pool.Acquire(context.Background(), f.backends[0].URL)
		require.NoError(t, err)
		conns[i] = c
	}
	for _, c := range conns {
		defer f.pool.Release(c)
	}

	body := map[string]interface{}{"model": "gpt-4"}
	bodyBytes, _ := json.Marshal(body)

	resp, _ := f.do("POST", "/v1/chat/completions", bodyBytes)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestInvalidJSONBody(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, _ := f.do("POST", "/v1/chat/completions", []byte("not json"))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestEmptyBody(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, _ := f.do("POST", "/v1/chat/completions", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestProxyRequestError(t *testing.T) {
	f := setupTest(t)

	// Create a backend that responds to /v1/models (for discovery) but
	// hijacks and drops the connection on any other endpoint so that
	// the proxy call fails with a transport-level error.
	testBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data":   []map[string]string{{"id": "gpt-4", "object": "model"}},
			})
			return
		}
		h, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := h.Hijack()
			conn.Close()
		}
	}))
	defer testBackend.Close()

	cfg := f.store.Get()
	cfg.Servers = append(cfg.Servers, config.ServerConfig{
		URL:                   testBackend.URL,
		Distance:              1,
		MaxConcurrentRequests: 5,
	})
	require.NoError(t, f.store.Set(cfg))
	f.pool.AddServer(cfg.Servers[len(cfg.Servers)-1])
	f.disc.SetServers([]string{testBackend.URL})
	require.NoError(t, f.disc.Discover(context.Background()))

	f.start(t)
	defer f.cleanup()

	body := map[string]interface{}{"model": "gpt-4"}
	bodyBytes, _ := json.Marshal(body)

	resp, _ := f.do("POST", "/v1/chat/completions", bodyBytes)
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

func TestMultipleBackends(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.addBackend(t, []string{"gpt-4", "claude-3"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/v1/models", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &result))
	data, _ := result["data"].([]interface{})
	modelNames := make(map[string]bool)
	for _, item := range data {
		entry := item.(map[string]interface{})
		modelNames[entry["id"].(string)] = true
	}
	assert.True(t, modelNames["gpt-4"])
	assert.True(t, modelNames["claude-3"])
}

// ---------------------------------------------------------------------------
// OpenCode Config tests
// ---------------------------------------------------------------------------

func TestOpenCodeConfig_Basic(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4", "gpt-3.5-turbo"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/jsonc", resp.Header.Get("Content-Type"))

	assert.Contains(t, string(body), "// Auto-generated by llm-bridge")
	assert.Contains(t, string(body), `"gpt-4"`)
	assert.Contains(t, string(body), `"gpt-3.5-turbo"`)
	assert.Contains(t, string(body), `"baseURL"`)
	assert.Contains(t, string(body), `"output": 4192`)
	assert.Contains(t, string(body), `"input": 4000`)
	assert.Contains(t, string(body), `"context"`)
	assert.Contains(t, string(body), `"sk-llm-bridge`)
	assert.Contains(t, string(body), `"enabled_providers"`)
	assert.Contains(t, string(body), `"llm-bridge"`)
	assert.Contains(t, string(body), `"$schema": "https://opencode.ai/config.json"`)
}

func TestOpenCodeConfig_WithMaxModelLen(t *testing.T) {
	f := setupTest(t)
	// Backend that includes max_model_len in metadata.
	f.addBackend(t, []string{"smart"}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{
						"id":            "smart",
						"object":        "model",
						"created":       1700000000,
						"owned_by":      "test",
						"max_model_len": 32768,
					},
				},
			})
		case "/v1/chat/completions", "/v1/completions", "/v1/embeddings":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      "mock-id",
				"object":  "chat.completion",
				"model":   "smart",
				"choices": []map[string]interface{}{{"text": "mock"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// max_model_len=32768 → context=32768, buffer=4000, auto mode: inLimit=buffer=4000, outLimit=32768-4000=28768
	assert.Contains(t, string(body), `"context": 32768`)
	assert.Contains(t, string(body), `"input": 4000`)
	assert.Contains(t, string(body), `"output": 28768`)
}

func TestOpenCodeConfig_CustomBaseURL(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	// Pass custom base_url via query param.
	resp, body := f.do("GET", "/admin/opencode-config?base_url=http://my-bridge:8080", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Contains(t, string(body), `"baseURL": "http://my-bridge:8080/v1"`)
}

func TestOpenCodeConfig_NoModels(t *testing.T) {
	f := setupTest(t)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Should have empty models under provider and enabled_providers with llm-bridge.
	assert.Contains(t, string(body), `"models": {}`)
	assert.Contains(t, string(body), `"enabled_providers"`)
	assert.Contains(t, string(body), `"llm-bridge"`)
}

func TestOpenCodeConfig_ContextLimitFromVLLM(t *testing.T) {
	f := setupTest(t)
	// Use realistic vLLM-style metadata.
	f.addBackend(t, []string{"qwen2.5-32b"}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{
						"id":                     "qwen2.5-32b",
						"object":                 "model",
						"created":                1700000000,
						"owned_by":               "vllm",
						"root":                   "qwen2.5-32b",
						"max_model_len":          65536,
						"max_num_seqs":           256,
						"max_num_batched_tokens": 65536,
					},
				},
			})
		case "/v1/chat/completions", "/v1/completions", "/v1/embeddings":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      "mock-id",
				"object":  "chat.completion",
				"model":   "qwen2.5-32b",
				"choices": []map[string]interface{}{{"text": "mock"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// vLLM-style: max_model_len=65536 → context=65536, buffer=4000, auto mode: inLimit=buffer=4000, outLimit=65536-4000=61536
	assert.Contains(t, string(body), `"context": 65536`)
	assert.Contains(t, string(body), `"input": 4000`)
	assert.Contains(t, string(body), `"output": 61536`)
}

func TestOpenCodeConfig_NoMaxModelLen(t *testing.T) {
	f := setupTest(t)
	// Backend with metadata but no max_model_len.
	f.addBackend(t, []string{"basic-model"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Should fall back to default 8192 → context=8192, buffer=4000, auto mode: inLimit=buffer=4000, outLimit=8192-4000=4192
	assert.Contains(t, string(body), `"context": 8192`)
	assert.Contains(t, string(body), `"input": 4000`)
	assert.Contains(t, string(body), `"output": 4192`)
}

// ---------------------------------------------------------------------------
// OpenCode Config: formula edge-case tests
// ---------------------------------------------------------------------------

func TestOpenCodeConfig_AutoMode_Defaults(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"smart"}, nil)

	cfg := f.store.Get()
	cfg.Global.OpenCodeContextBuffer = 4000
	cfg.Global.OpenCodeContextInput = 0 // auto mode
	require.NoError(t, f.store.Set(cfg))

	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// buffer=4000, input=0 (auto) → inLimit=buffer=4000, outLimit=8192-4000=4192
	assert.Contains(t, string(body), `"input": 4000`)
	assert.Contains(t, string(body), `"output": 4192`)
}

func TestOpenCodeConfig_AutoMode_LargeBuffer(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"smart"}, nil)

	cfg := f.store.Get()
	cfg.Global.OpenCodeContextBuffer = 10000
	cfg.Global.OpenCodeContextInput = 0 // auto mode
	require.NoError(t, f.store.Set(cfg))

	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// buffer=10000, input=0 (auto) → inLimit=10000>ctx, so inLimit=ctx/4=2048, outLimit=8192-2048=6144
	assert.Contains(t, string(body), `"input": 2048`)
	assert.Contains(t, string(body), `"output": 6144`)
}

func TestOpenCodeConfig_ExplicitMode(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"smart"}, nil)

	cfg := f.store.Get()
	cfg.Global.OpenCodeContextBuffer = 8000
	cfg.Global.OpenCodeContextInput = 2000 // explicit mode
	require.NoError(t, f.store.Set(cfg))

	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// buffer=8000, input=2000 (explicit) → inLimit=2000, outLimit=8192-2000=6192
	assert.Contains(t, string(body), `"input": 2000`)
	assert.Contains(t, string(body), `"output": 6192`)
}

func TestOpenCodeConfig_ExplicitMode_Large(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"smart"}, nil)

	cfg := f.store.Get()
	cfg.Global.OpenCodeContextBuffer = 20000
	cfg.Global.OpenCodeContextInput = 5000 // explicit mode
	require.NoError(t, f.store.Set(cfg))

	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// buffer=20000, input=5000 (explicit) → inLimit=5000, outLimit=8192-5000=3192
	assert.Contains(t, string(body), `"input": 5000`)
	assert.Contains(t, string(body), `"output": 3192`)
}

func TestOpenCodeConfig_ExplicitMode_GuardInput(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"smart"}, nil)

	cfg := f.store.Get()
	cfg.Global.OpenCodeContextBuffer = 1500
	cfg.Global.OpenCodeContextInput = 0 // auto mode, but buffer < 3000
	require.NoError(t, f.store.Set(cfg))

	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Auto mode: inLimit=buffer=1500, outLimit=8192-1500=6692
	assert.Contains(t, string(body), `"input": 1500`)
	assert.Contains(t, string(body), `"output": 6692`)
}

func TestOpenCodeConfig_ExplicitMode_GuardOutput(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"smart"}, nil)

	cfg := f.store.Get()
	cfg.Global.OpenCodeContextBuffer = 1500
	cfg.Global.OpenCodeContextInput = 100 // explicit mode
	require.NoError(t, f.store.Set(cfg))

	f.start(t)
	defer f.cleanup()

	resp, body := f.do("GET", "/admin/opencode-config", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Explicit: inLimit=100, outLimit=8192-100=8092
	assert.Contains(t, string(body), `"input": 100`)
	assert.Contains(t, string(body), `"output": 8092`)
}

// httpDo executes an *http.Request built externally and returns the response.
func (f *testFixture) httpDo(req *http.Request) (*http.Response, []byte) {
	// Rewrite the URL to point at the test server.
	req.URL.Scheme = "http"
	req.URL.Host = f.ts.Listener.Addr().String()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}

// ---------------------------------------------------------------------------
// Metrics test helpers
// ---------------------------------------------------------------------------

// mockMetricsBackend creates an httptest.Server that handles standard OpenAI
// API paths plus a /metrics endpoint returning vLLM-style Prometheus data.
func mockMetricsBackend(t *testing.T, models []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			data := make([]map[string]string, len(models))
			for i, m := range models {
				data[i] = map[string]string{"id": m, "object": "model"}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data":   data,
			})
		case "/v1/chat/completions", "/v1/completions", "/v1/embeddings":
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			resp := map[string]interface{}{
				"id":      "mock-id",
				"object":  "chat.completion",
				"model":   reqBody["model"],
				"choices": []map[string]interface{}{{"text": "mock response"}},
			}
			json.NewEncoder(w).Encode(resp)
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`# HELP vllm:num_requests_running Number of running requests
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{engine="0",model_name="gpt-4"} 3
# HELP vllm:num_requests_waiting Number of waiting requests
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting{engine="0",model_name="gpt-4"} 2
# HELP vllm:kv_cache_usage_perc KV cache usage percentage
# TYPE vllm:kv_cache_usage_perc gauge
vllm:kv_cache_usage_perc{engine="0",model_name="gpt-4"} 0.425
# HELP vllm:prompt_tokens_total Total prompt tokens
# TYPE vllm:prompt_tokens_total counter
vllm:prompt_tokens_total{engine="0",model_name="gpt-4"} 1500
# HELP vllm:generation_tokens_total Total generation tokens
# TYPE vllm:generation_tokens_total counter
vllm:generation_tokens_total{engine="0",model_name="gpt-4"} 750
`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// testFixtureWithMetrics wraps testFixture and adds metrics-specific resources.
type testFixtureWithMetrics struct {
	*testFixture
	metricsSrv *httptest.Server
	collector  *metrics.Collector
}

// setupMetricsTest creates a test fixture with a real metrics collector that
// has fetched data from a mock backend.  The returned fixture's server is
// already running.  Caller should use t.Cleanup(fmc.cleanup).
func setupMetricsTest(t *testing.T) *testFixtureWithMetrics {
	t.Helper()

	f := setupTest(t)

	// Create mock backend with /metrics endpoint.
	backendSrv := mockMetricsBackend(t, []string{"gpt-4", "gpt-3.5-turbo"})

	f.backends = append(f.backends, backendSrv)

	// Update config.
	cfg := f.store.Get()
	cfg.Servers = append(cfg.Servers, config.ServerConfig{
		URL:                   backendSrv.URL,
		Distance:              1,
		MaxConcurrentRequests: 5,
	})
	require.NoError(t, f.store.Set(cfg))
	f.pool.AddServer(cfg.Servers[len(cfg.Servers)-1])

	// Discovery.
	urls := []string{backendSrv.URL}
	f.disc.SetServers(urls)
	require.NoError(t, f.disc.Discover(context.Background()))

	// Metrics collector — Start blocks until initial fetch completes.
	ctx, cancel := context.WithCancel(context.Background())
	mc := metrics.New(time.Hour)
	mc.Start(ctx, urls)

	// Create server WITH metrics collector.
	rtr := router.New(f.store, f.disc, f.pool)
	f.srv = New(f.store, f.disc, f.pool, rtr, mc, ":0")
	f.ts = httptest.NewServer(f.srv.mux)

	fmc := &testFixtureWithMetrics{
		testFixture: f,
		metricsSrv:  backendSrv,
		collector:   mc,
	}

	fmc.cleanup = func() {
		cancel()
		mc.Stop()
		f.ts.Close()
		for _, b := range f.backends {
			b.Close()
		}
	}

	return fmc
}

// ---------------------------------------------------------------------------
// Admin: metrics endpoint tests
// ---------------------------------------------------------------------------

// TestMetricsEndpoint_Basic verifies the /admin/metrics endpoint works
// with a nil metrics collector (no vLLM backends).
func TestMetricsEndpoint_Basic(t *testing.T) {
	t.Parallel()
	f := setupTest(t)
	f.start(t)
	t.Cleanup(f.cleanup)

	resp, body := f.do("GET", "/admin/metrics", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/plain; version=0.0.4; charset=utf-8",
		resp.Header.Get("Content-Type"))

	metricsStr := string(body)
	assert.Contains(t, metricsStr, "llm_bridge_requests_total")
	assert.NotContains(t, metricsStr, "vllm_")
}

// TestMetricsEndpoint_WithVLLMMetrics verifies the endpoint includes
// vLLM metrics when a collector is present and has fetched data.
func TestMetricsEndpoint_WithVLLMMetrics(t *testing.T) {
	t.Parallel()
	fmc := setupMetricsTest(t)
	t.Cleanup(fmc.cleanup)

	resp, body := fmc.do("GET", "/admin/metrics", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	metricsStr := string(body)
	// Labels are the raw server URL, e.g. vllm_requests_running{http://...}
	assert.Contains(t, metricsStr, "vllm_requests_running{")
	assert.Contains(t, metricsStr, "vllm_kv_cache_usage_perc{")
	assert.Contains(t, metricsStr, "llm_bridge_requests_total{status=\"success\"}")
	// The label value contains the backend URL.
	assert.Contains(t, metricsStr, fmc.metricsSrv.URL)
}

// TestMetricsEndpoint_BridgeCounters verifies that bridge request counters
// are incremented correctly and reflected in the metrics output.
func TestMetricsEndpoint_BridgeCounters(t *testing.T) {
	t.Parallel()
	fmc := setupMetricsTest(t)
	t.Cleanup(fmc.cleanup)

	// Send 3 successful chat completion requests.
	const numReqs = 3
	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	})
	for i := 0; i < numReqs; i++ {
		resp, _ := fmc.do("POST", "/v1/chat/completions", reqBody)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Check metrics.
	resp, body := fmc.do("GET", "/admin/metrics", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	metricsStr := string(body)
	// Extract the success counter value.
	re := regexp.MustCompile(`llm_bridge_requests_total\{status="success"\}\s+(\d+)`)
	matches := re.FindStringSubmatch(metricsStr)
	require.Len(t, matches, 2)
	val, err := strconv.Atoi(matches[1])
	require.NoError(t, err)
	assert.Equal(t, numReqs, val)
}

// TestMetricsEndpoint_PrometheusFormat validates that every metric data line
// has a corresponding HELP and TYPE declaration preceding it.
func TestMetricsEndpoint_PrometheusFormat(t *testing.T) {
	t.Parallel()
	fmc := setupMetricsTest(t)
	t.Cleanup(fmc.cleanup)

	resp, body := fmc.do("GET", "/admin/metrics", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := strings.Split(string(body), "\n")

	// Track which metric names have seen HELP and TYPE declarations.
	var seenHelp, seenType map[string]bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse HELP line.
		if strings.HasPrefix(line, "# HELP ") {
			parts := strings.Fields(line[7:]) // skip "# HELP "
			if len(parts) >= 1 {
				if seenHelp == nil {
					seenHelp = make(map[string]bool)
				}
				seenHelp[parts[0]] = true
			}
			continue
		}

		// Parse TYPE line.
		if strings.HasPrefix(line, "# TYPE ") {
			parts := strings.Fields(line[7:]) // skip "# TYPE "
			if len(parts) >= 1 {
				if seenType == nil {
					seenType = make(map[string]bool)
				}
				seenType[parts[0]] = true
			}
			continue
		}

		// Data line — extract metric name (strip labels).
		parts := strings.Fields(line)
		assert.GreaterOrEqual(t, len(parts), 2, "malformed metric line: %s", line)
		rawName := parts[0]
		name := rawName
		if idx := strings.IndexByte(name, '{'); idx >= 0 {
			name = name[:idx]
		}

		assert.True(t, seenHelp[name], "metric %s missing HELP declaration", name)
		assert.True(t, seenType[name], "metric %s missing TYPE declaration", name)
	}
}

// TestMetricsEndpoint_RouteConflict ensures /admin/metrics does not serve
// HTML (which would indicate the catch-all admin UI route is intercepting).
func TestMetricsEndpoint_RouteConflict(t *testing.T) {
	t.Parallel()
	f := setupTest(t)
	f.start(t)
	t.Cleanup(f.cleanup)

	resp, body := f.do("GET", "/admin/metrics", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	ct := resp.Header.Get("Content-Type")
	assert.Contains(t, ct, "text/plain")
	assert.NotContains(t, ct, "text/html")
	assert.NotContains(t, string(body), "<!DOCTYPE html>")
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

// TestIntegration_MetricsEndpoint verifies the /admin/metrics endpoint end-to-end
// with a real metrics collector fetching vLLM data from a mock backend.
func TestIntegration_MetricsEndpoint(t *testing.T) {
	t.Parallel()
	fmc := setupMetricsTest(t)
	t.Cleanup(fmc.cleanup)

	resp, body := fmc.do("GET", "/admin/metrics", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Content-Type must be Prometheus text format, not HTML.
	ct := resp.Header.Get("Content-Type")
	assert.Equal(t, "text/plain; version=0.0.4; charset=utf-8", ct)

	metricsStr := string(body)

	// Must contain HELP and TYPE declarations.
	assert.Contains(t, metricsStr, "# HELP")
	assert.Contains(t, metricsStr, "# TYPE")

	// Must contain vLLM metrics (the collector fetched from mock backend).
	assert.Contains(t, metricsStr, "vllm_requests_running{")
	assert.Contains(t, metricsStr, "vllm_kv_cache_usage_perc{")
	assert.Contains(t, metricsStr, "vllm_prompt_tokens_total{")

	// Must contain bridge counters.
	assert.Contains(t, metricsStr, "llm_bridge_requests_total{status=\"success\"}")
	assert.Contains(t, metricsStr, "llm_bridge_requests_total{status=\"error\"}")

	// Must contain bridge inflight per server.
	assert.Contains(t, metricsStr, "llm_bridge_inflight_requests{")

	// Must NOT contain HTML (route conflict guard).
	assert.NotContains(t, metricsStr, "<!DOCTYPE html>")
	assert.NotContains(t, ct, "text/html")

	// The label values must contain the mock backend URL.
	assert.Contains(t, metricsStr, fmc.metricsSrv.URL)
}

// TestIntegration_ChatEndpoint verifies the chat admin page and streaming
// chat completion end-to-end.
func TestIntegration_ChatEndpoint(t *testing.T) {
	t.Parallel()
	f := setupTest(t)

	// Backend that supports SSE streaming on /v1/chat/completions.
	_ = f.addBackend(t, []string{"gpt-4"}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data":   []map[string]string{{"id": "gpt-4", "object": "model"}},
			})
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			require.True(t, ok)
			_, _ = w.Write([]byte("data: {\"id\":\"sse-1\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
			flusher.Flush()
			_, _ = w.Write([]byte("data: {\"id\":\"sse-2\",\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n"))
			flusher.Flush()
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}
	})
	f.start(t)
	t.Cleanup(f.cleanup)

	// 1. GET /admin/chat → 200, text/html, contains expected elements.
	resp, body := f.do("GET", "/admin/chat", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	ct := resp.Header.Get("Content-Type")
	assert.Contains(t, ct, "text/html")

	chatHTML := string(body)
	assert.Contains(t, chatHTML, "chat-model-select")
	assert.Contains(t, chatHTML, "chat-message-input")
	assert.Contains(t, chatHTML, "chat-messages")
	assert.Contains(t, chatHTML, "chat-metrics")
	assert.Contains(t, chatHTML, "btn-chat-send")
	assert.Contains(t, chatHTML, `data-section="chat"`)
	assert.Contains(t, chatHTML, "Chat")

	// 2. Backend is reachable — GET /v1/models returns models.
	resp, modelsBody := f.do("GET", "/v1/models", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var modelsResp map[string]interface{}
	require.NoError(t, json.Unmarshal(modelsBody, &modelsResp))
	data, ok := modelsResp["data"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data)

	// 3. POST /v1/chat/completions with stream:true → SSE response.
	streamReq := map[string]interface{}{
		"model":  "gpt-4",
		"stream": true,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	streamBody, _ := json.Marshal(streamReq)
	resp, sseBody := f.do("POST", "/v1/chat/completions", streamBody)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Contains(t, string(sseBody), "data:")
	assert.Contains(t, string(sseBody), "choices")
}

// TestIntegration_NoRegression is a smoke test verifying that all primary
// endpoints still respond after any change to the codebase.
func TestIntegration_NoRegression(t *testing.T) {
	t.Parallel()
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4", "gpt-3.5-turbo"}, nil)
	f.start(t)
	t.Cleanup(f.cleanup)

	tests := []struct {
		method string
		path   string
		body   map[string]interface{}
	}{
		{"GET", "/v1/models", nil},
		{"POST", "/v1/chat/completions", map[string]interface{}{
			"model":    "gpt-4",
			"messages": []map[string]string{{"role": "user", "content": "hello"}},
		}},
		{"GET", "/admin/servers", nil},
		{"GET", "/admin/status", nil},
		{"GET", "/admin/config", nil},
	}

	for _, tt := range tests {
		name := tt.method + " " + tt.path
		var bodyBytes []byte
		if tt.body != nil {
			bodyBytes, _ = json.Marshal(tt.body)
		}
		resp, _ := f.do(tt.method, tt.path, bodyBytes)
		assert.Equal(t, http.StatusOK, resp.StatusCode, name)
	}
}

// ---------------------------------------------------------------------------
// POST /opencode/config tests
// ---------------------------------------------------------------------------

func TestPostOpenCodeConfig_ValidJSON(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4", "gpt-3.5-turbo"}, nil)
	f.start(t)
	defer f.cleanup()

	// Send a valid user config (JSON with // comments).
	userConfig := `{
		// User's old opencode config
		"$schema": "https://opencode.ai/config.json",
		"enabled_providers": ["anthropic"],
		"provider": {
			"anthropic": {
				"api": "anthropic",
				"name": "anthropic",
				"options": {
					"baseURL": "https://api.anthropic.com",
					"apiKey": "sk-ant-..."
				},
				"models": {
					"claude-3-opus": {
						"id": "claude-3-opus",
						"limit": {"context": 200000, "input": 190000, "output": 10000}
					}
				}
			}
		}
	}`

	resp, body := f.do("POST", "/opencode/config", []byte(userConfig))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/jsonc", resp.Header.Get("Content-Type"))

	// Body should start with a // comment.
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "// Auto-generated by llm-bridge")
	assert.Contains(t, bodyStr, `"llm-bridge"`)

	// Should contain our discovered models.
	assert.Contains(t, bodyStr, `"gpt-4"`)
	assert.Contains(t, bodyStr, `"gpt-3.5-turbo"`)
}

func TestPostOpenCodeConfig_ValidJSONNoComments(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"small"}, nil)
	f.start(t)
	defer f.cleanup()

	userConfig := `{"enabled_providers": ["test"]}`
	resp, body := f.do("POST", "/opencode/config", []byte(userConfig))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/jsonc", resp.Header.Get("Content-Type"))

	bodyStr := string(body)
	assert.Contains(t, bodyStr, `// Auto-generated by llm-bridge`)
	assert.Contains(t, bodyStr, `"small"`)
}

func TestPostOpenCodeConfig_EmptyBody(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"test"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("POST", "/opencode/config", []byte(""))
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var errResp map[string]interface{}
	json.Unmarshal(body, &errResp)
	assert.Contains(t, errResp, "error")
}

func TestPostOpenCodeConfig_InvalidJSON(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"test"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("POST", "/opencode/config", []byte("this is not json {"))
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var errResp map[string]interface{}
	json.Unmarshal(body, &errResp)
	errObj, ok := errResp["error"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "invalid JSON body", errObj["message"])
}

func TestPostOpenCodeConfig_OnlyComments(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	// Config that is only comments — should fail to parse as JSON.
	resp, _ := f.do("POST", "/opencode/config", []byte("// just a comment\n// another comment"))
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPostOpenCodeConfig_BaseURLOverride(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("POST", "/opencode/config?base_url=http://custom-bridge:9090", []byte("{}"))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Contains(t, string(body), `"baseURL": "http://custom-bridge:9090/v1"`)
}

func TestPostOpenCodeConfig_NoModels(t *testing.T) {
	f := setupTest(t)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("POST", "/opencode/config", []byte("{}"))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"models": {}`)
	assert.Contains(t, bodyStr, `"enabled_providers"`)
	assert.Contains(t, bodyStr, `"llm-bridge"`)
}

func TestPostOpenCodeConfig_UsesLLMBridgeProviderName(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"test-model"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("POST", "/opencode/config", []byte(`{"provider": {"anything": {}}}`))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	bodyStr := string(body)
	// Should have $schema, enabled_providers with llm-bridge, and provider with llm-bridge key.
	assert.Contains(t, bodyStr, `"https://opencode.ai/config.json"`)
	assert.Contains(t, bodyStr, `"llm-bridge"`)

	// Parse and verify the JSON structure after the comment.
	commentIdx := strings.Index(bodyStr, "\n\n")
	require.Greater(t, commentIdx, 0)
	jsonPart := bodyStr[commentIdx+2:]
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(jsonPart), &parsed)
	require.NoError(t, err)

	// Check $schema.
	assert.Equal(t, "https://opencode.ai/config.json", parsed["$schema"])

	// Check enabled_providers.
	enabled, ok := parsed["enabled_providers"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, 1, len(enabled))
	assert.Equal(t, "llm-bridge", enabled[0])

	// Check provider.
	provider, ok := parsed["provider"].(map[string]interface{})
	require.True(t, ok)
	_, hasLLMBridge := provider["llm-bridge"]
	assert.True(t, hasLLMBridge, "provider should contain 'llm-bridge' key")
}

func TestPostOpenCodeConfig_APIKeyIsSkLlmBridge(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"test-model"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("POST", "/opencode/config", []byte("{}"))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	bodyStr := string(body)
	assert.Contains(t, bodyStr, `sk-llm-bridge`)
}

func TestPostOpenCodeConfig_ModelsHaveLimits(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	resp, body := f.do("POST", "/opencode/config", []byte("{}"))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"gpt-4"`)
	// Each model should have context, input, output limits.
	assert.Contains(t, bodyStr, `"limit"`)
	assert.Contains(t, bodyStr, `"context"`)
	assert.Contains(t, bodyStr, `"input"`)
	assert.Contains(t, bodyStr, `"output"`)
}

func TestPostOpenCodeConfig_CorrectsUserProvider(t *testing.T) {
	f := setupTest(t)
	f.addBackend(t, []string{"gpt-4"}, nil)
	f.start(t)
	defer f.cleanup()

	// Send a config with a completely different provider.
	userConfig := `{
		"provider": {
			"custom-openai": {
				"api": "openai",
				"name": "custom",
				"options": {"baseURL": "https://my-custom-api.com", "apiKey": "my-key"},
				"models": {"my-model": {"id": "my-model", "limit": {"context": 1000}}}
			}
		}
	}`
	resp, body := f.do("POST", "/opencode/config", []byte(userConfig))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	bodyStr := string(body)
	// Should NOT contain the user's provider.
	assert.NotContains(t, bodyStr, "custom-openai")
	assert.NotContains(t, bodyStr, "my-custom-api.com")
	assert.NotContains(t, bodyStr, "my-key")
	// Should contain our provider with discovered models.
	assert.Contains(t, bodyStr, `"llm-bridge"`)
	assert.Contains(t, bodyStr, `"gpt-4"`)
}

// ---------------------------------------------------------------------------
// stripJSONC unit tests
// ---------------------------------------------------------------------------

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "no comments",
			input:  `{"key": "value"}`,
			expect: `{"key": "value"}`,
		},
		{
			name:   "single comment",
			input:  `{"key": "value"} // comment`,
			expect: `{"key": "value"} `,
		},
		{
			name:   "comment at end of line",
			input:  `{// start comment
	"key": "value" // inline
}`,
			expect: "{\n\t\"key\": \"value\" \n}",
		},
		{
			name:   "comment with // inside string preserved",
			input:  `{"url": "https://example.com"} // comment`,
			expect: `{"url": "https://example.com"} `,
		},
		{
			name:   "multiple lines with comments",
			input:  `{
	// Header comment
	"key1": "value1", // trailing
	"key2": "value2"
}`,
			expect: "{\n\t\n\t\"key1\": \"value1\", \n\t\"key2\": \"value2\"\n}",
		},
		{
			name:   "empty input",
			input:  ``,
			expect: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripJSONC(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}


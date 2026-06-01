package backend

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"llm-bridge/config"

	"github.com/stretchr/testify/assert"
)

func TestAcquireRelease(t *testing.T) {
	pool := NewPool()
	cfg := config.ServerConfig{URL: "http://test:8080", Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg)

	conn, err := pool.Acquire(context.Background(), cfg.URL)
	assert.NoError(t, err)
	assert.Equal(t, cfg.URL, conn.ServerID)
	assert.Equal(t, int64(1), pool.Inflight(cfg.URL))

	pool.Release(conn)
	assert.Equal(t, int64(0), pool.Inflight(cfg.URL))
}

func TestAcquireUnknownServer(t *testing.T) {
	pool := NewPool()
	_, err := pool.Acquire(context.Background(), "http://unknown:8080")
	assert.ErrorIs(t, err, ErrServerUnhealthy)
}

func TestAddRemoveServer(t *testing.T) {
	pool := NewPool()
	assert.Equal(t, 0, pool.ServerCount())

	cfg := config.ServerConfig{URL: "http://test:8080", Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg)
	assert.Equal(t, 1, pool.ServerCount())

	got, ok := pool.GetServer(cfg.URL)
	assert.True(t, ok)
	assert.Equal(t, cfg, got)

	pool.RemoveServer(cfg.URL)
	assert.Equal(t, 0, pool.ServerCount())

	_, ok = pool.GetServer(cfg.URL)
	assert.False(t, ok)
}

func TestInflightCounting(t *testing.T) {
	pool := NewPool()
	cfg1 := config.ServerConfig{URL: "http://srv1:8080", Distance: 1, MaxConcurrentRequests: 5}
	cfg2 := config.ServerConfig{URL: "http://srv2:8080", Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg1)
	pool.AddServer(cfg2)

	conn1, _ := pool.Acquire(context.Background(), cfg1.URL)
	conn2, _ := pool.Acquire(context.Background(), cfg2.URL)
	conn3, _ := pool.Acquire(context.Background(), cfg1.URL)

	assert.Equal(t, int64(2), pool.Inflight(cfg1.URL))
	assert.Equal(t, int64(1), pool.Inflight(cfg2.URL))

	pool.Release(conn1)
	assert.Equal(t, int64(1), pool.Inflight(cfg1.URL))

	pool.Release(conn3)
	assert.Equal(t, int64(0), pool.Inflight(cfg1.URL))

	pool.Release(conn2)
	assert.Equal(t, int64(0), pool.Inflight(cfg2.URL))
}

func TestAcquireContextCancellation(t *testing.T) {
	pool := NewPool()
	cfg := config.ServerConfig{URL: "http://test:8080", Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pool.Acquire(ctx, cfg.URL)
	assert.Error(t, err)
	assert.Equal(t, int64(0), pool.Inflight(cfg.URL))
}

func TestProxyRequest(t *testing.T) {
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/test-path", r.URL.Path)
		assert.Equal(t, "test-value", r.Header.Get("X-Test"))

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "request body", string(body))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backendSrv.Close()

	pool := NewPool()
	cfg := config.ServerConfig{URL: backendSrv.URL, Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg)

	conn, err := pool.Acquire(context.Background(), cfg.URL)
	assert.NoError(t, err)
	defer pool.Release(conn)

	headers := http.Header{}
	headers.Set("X-Test", "test-value")

	resp, err := pool.ProxyRequest(context.Background(), conn, "POST", "/test-path",
		strings.NewReader("request body"), headers)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	respBody, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, `{"status":"ok"}`, string(respBody))
}

func TestProxyRequestStreaming(t *testing.T) {
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		assert.True(t, ok)

		for i := 0; i < 3; i++ {
			_, _ = w.Write([]byte("data: chunk\n\n"))
			flusher.Flush()
		}
	}))
	defer backendSrv.Close()

	pool := NewPool()
	cfg := config.ServerConfig{URL: backendSrv.URL, Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg)

	conn, err := pool.Acquire(context.Background(), cfg.URL)
	assert.NoError(t, err)
	defer pool.Release(conn)

	resp, err := pool.ProxyRequest(context.Background(), conn, "GET", "/stream", nil, http.Header{})
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, "data: chunk\n\ndata: chunk\n\ndata: chunk\n\n", string(body))
}

func TestAddServerUsesTransport(t *testing.T) {
	pool := NewPool()
	cfg := config.ServerConfig{URL: "http://test:8080", Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg)

	pool.mu.Lock()
	bc, ok := pool.clients[cfg.URL]
	pool.mu.Unlock()
	assert.True(t, ok)
	assert.NotNil(t, bc.client.Transport)

	transport, ok := bc.client.Transport.(*http.Transport)
	assert.True(t, ok, "Transport should be *http.Transport")
	assert.Equal(t, 100, transport.MaxIdleConns)
	assert.Equal(t, 10, transport.MaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, transport.IdleConnTimeout)
}

func TestRemoveServerDecrementsInflight(t *testing.T) {
	pool := NewPool()
	cfg := config.ServerConfig{URL: "http://test:8080", Distance: 1, MaxConcurrentRequests: 5}
	pool.AddServer(cfg)

	conn, _ := pool.Acquire(context.Background(), cfg.URL)
	assert.Equal(t, int64(1), pool.Inflight(cfg.URL))

	pool.RemoveServer(cfg.URL)
	assert.Equal(t, 0, pool.ServerCount())
	assert.Equal(t, int64(0), pool.Inflight(cfg.URL))

	pool.Release(conn)
	assert.Equal(t, int64(0), pool.Inflight(cfg.URL))
}

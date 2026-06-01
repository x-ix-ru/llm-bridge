package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"llm-bridge/backend"
	"llm-bridge/config"
	"llm-bridge/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T, models []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]map[string]string, len(models))
		for i, m := range models {
			data[i] = map[string]string{"id": m, "object": "model"}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data":   data,
		})
	}))
}

func setupTest(t *testing.T, models []string, srvConfigs []config.ServerConfig, fallback config.FallbackStrategy) (*Router, *backend.Pool, []*httptest.Server) {
	t.Helper()

	pool := backend.NewPool()
	var servers []*httptest.Server
	var srvURLs []string

	for _, sc := range srvConfigs {
		srv := testServer(t, models)
		servers = append(servers, srv)
		sc.URL = srv.URL
		pool.AddServer(sc)
		srvURLs = append(srvURLs, srv.URL)
	}

	disc := discovery.New(time.Hour)
	disc.SetServers(srvURLs)
	require.NoError(t, disc.Discover(context.Background()))

	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "config.yaml"))
	require.NoError(t, store.Load())
	cfg := store.Get()
	cfg.Global.FallbackStrategy = fallback
	if fallback == config.FallbackQueue {
		cfg.Global.QueueTimeoutSec = 5
	}
	require.NoError(t, store.Set(cfg))

	rt := New(store, disc, pool)
	return rt, pool, servers
}

func TestRouteSingleServer(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 5},
	}, config.FallbackError)
	defer servers[0].Close()

	conn, err := rt.Route(context.Background(), "gpt-4")
	require.NoError(t, err)
	assert.Equal(t, servers[0].URL, conn.ServerID)
	pool.Release(conn)
}

func TestRoutePrefersLowerDistance(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 5},
		{Distance: 10, MaxConcurrentRequests: 5},
	}, config.FallbackError)
	defer servers[0].Close()
	defer servers[1].Close()

	conn, err := rt.Route(context.Background(), "gpt-4")
	require.NoError(t, err)
	assert.Equal(t, servers[0].URL, conn.ServerID,
		"should pick server with distance 1 over distance 10")
	pool.Release(conn)
}

func TestRouteRoundRobin(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 5},
		{Distance: 1, MaxConcurrentRequests: 5},
	}, config.FallbackError)
	defer servers[0].Close()
	defer servers[1].Close()

	conn1, err := rt.Route(context.Background(), "gpt-4")
	require.NoError(t, err)

	conn2, err := rt.Route(context.Background(), "gpt-4")
	require.NoError(t, err)

	assert.NotEqual(t, conn1.ServerID, conn2.ServerID,
		"round-robin should alternate between equal-distance servers")

	pool.Release(conn1)
	pool.Release(conn2)
}

func TestRouteSkipsServerAtCapacity(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 1},
		{Distance: 1, MaxConcurrentRequests: 5},
	}, config.FallbackError)
	defer servers[0].Close()
	defer servers[1].Close()

	fillConn, err := pool.Acquire(context.Background(), servers[0].URL)
	require.NoError(t, err)

	conn, err := rt.Route(context.Background(), "gpt-4")
	require.NoError(t, err)
	assert.Equal(t, servers[1].URL, conn.ServerID,
		"should skip full server and pick available one")

	pool.Release(fillConn)
	pool.Release(conn)
}

func TestRouteErrNoServers(t *testing.T) {
	rt, _, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 5},
	}, config.FallbackError)
	defer servers[0].Close()

	_, err := rt.Route(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrNoServers)
}

func TestRouteErrAllBusy(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 1},
	}, config.FallbackError)
	defer servers[0].Close()

	fillConn, err := pool.Acquire(context.Background(), servers[0].URL)
	require.NoError(t, err)

	_, err = rt.Route(context.Background(), "gpt-4")
	assert.ErrorIs(t, err, ErrAllBusy)

	pool.Release(fillConn)
}

func TestFallbackBestEffort(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 5},
		{Distance: 10, MaxConcurrentRequests: 5},
	}, config.FallbackBestEffort)
	defer servers[0].Close()
	defer servers[1].Close()

	var conns []*backend.Conn
	for i := 0; i < 6; i++ {
		c, err := pool.Acquire(context.Background(), servers[0].URL)
		require.NoError(t, err)
		conns = append(conns, c)
	}
	for i := 0; i < 5; i++ {
		c, err := pool.Acquire(context.Background(), servers[1].URL)
		require.NoError(t, err)
		conns = append(conns, c)
	}

	conn, err := rt.Route(context.Background(), "gpt-4")
	require.NoError(t, err)
	assert.Equal(t, servers[1].URL, conn.ServerID,
		"best_effort should pick srv2 with lower ratio (5/5=1.0 vs 6/5=1.2)")

	pool.Release(conn)
	for _, c := range conns {
		pool.Release(c)
	}
}

func TestFallbackQueue(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 1},
	}, config.FallbackQueue)
	defer servers[0].Close()

	fillConn, err := pool.Acquire(context.Background(), servers[0].URL)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		pool.Release(fillConn)
	}()

	conn, err := rt.Route(context.Background(), "gpt-4")
	require.NoError(t, err)
	assert.Equal(t, servers[0].URL, conn.ServerID)

	pool.Release(conn)
	wg.Wait()
}

func TestQueueTimeout(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 1},
	}, config.FallbackQueue)
	defer servers[0].Close()

	store := rt.cfg
	cfg := store.Get()
	cfg.Global.QueueTimeoutSec = 0
	require.NoError(t, store.Set(cfg))

	fillConn, err := pool.Acquire(context.Background(), servers[0].URL)
	require.NoError(t, err)
	defer pool.Release(fillConn)

	_, err = rt.Route(context.Background(), "gpt-4")
	assert.ErrorIs(t, err, ErrQueueTimeout)
}

func TestQueueDrainOnCancel(t *testing.T) {
	rt, pool, servers := setupTest(t, []string{"gpt-4"}, []config.ServerConfig{
		{Distance: 1, MaxConcurrentRequests: 1},
	}, config.FallbackQueue)
	defer servers[0].Close()

	fillConn, err := pool.Acquire(context.Background(), servers[0].URL)
	require.NoError(t, err)
	defer pool.Release(fillConn)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := rt.Route(ctx, "gpt-4")
		errCh <- err
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for queue drain")
	}
}

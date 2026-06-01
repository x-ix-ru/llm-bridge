package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, FallbackError, cfg.Global.FallbackStrategy)
	assert.Equal(t, 15, cfg.Global.DiscoveryIntervalSec)
	assert.Equal(t, 60, cfg.Global.RequestTimeoutSec)
	assert.Empty(t, cfg.Servers)
}

func TestStoreLoadCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	store := NewStore(path)
	err := store.Load()
	require.NoError(t, err)
	cfg := store.Get()
	assert.Equal(t, FallbackError, cfg.Global.FallbackStrategy)
	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestStoreLoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `
global:
  fallback_strategy: best_effort
  discovery_interval_sec: 30
  request_timeout_sec: 120
servers:
  - url: http://test:8000
    distance: 1
    max_concurrent_requests: 4
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)
	cfg := store.Get()
	assert.Equal(t, FallbackBestEffort, cfg.Global.FallbackStrategy)
	assert.Equal(t, 30, cfg.Global.DiscoveryIntervalSec)
	require.Len(t, cfg.Servers, 1)
	assert.Equal(t, "http://test:8000", cfg.Servers[0].URL)
	assert.Equal(t, 1, cfg.Servers[0].Distance)
	assert.Equal(t, 4, cfg.Servers[0].MaxConcurrentRequests)
}

func TestStoreSetInvalidStrategy(t *testing.T) {
	store := NewStore("test.yaml")
	cfg := DefaultConfig()
	cfg.Global.FallbackStrategy = FallbackStrategy("invalid")
	err := store.Set(cfg)
	assert.Error(t, err)
}

func TestStoreSetInvalidServer(t *testing.T) {
	store := NewStore("test.yaml")
	cfg := DefaultConfig()
	cfg.Servers = append(cfg.Servers, ServerConfig{URL: "", Distance: 0, MaxConcurrentRequests: 0})
	err := store.Set(cfg)
	assert.Error(t, err)
}

func TestStoreSetValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	store := NewStore(path)
	err := store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	cfg.Servers = append(cfg.Servers, ServerConfig{
		URL: "http://srv:8000", Distance: 2, MaxConcurrentRequests: 8,
	})
	err = store.Set(cfg)
	require.NoError(t, err)

	loaded := store.Get()
	require.Len(t, loaded.Servers, 1)
	assert.Equal(t, "http://srv:8000", loaded.Servers[0].URL)
}

func TestStoreSetPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	store := NewStore(path)
	err := store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	cfg.Global.FallbackStrategy = FallbackBestEffort
	cfg.Servers = append(cfg.Servers, ServerConfig{URL: "http://x:8000", Distance: 1, MaxConcurrentRequests: 4})
	err = store.Set(cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "best_effort")
	assert.Contains(t, string(data), "http://x:8000")
}

func TestStoreGetCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	store := NewStore(path)
	err := store.Load()
	require.NoError(t, err)

	cfg := store.GetCopy()
	assert.Equal(t, FallbackError, cfg.Global.FallbackStrategy)
}

func TestFallbackStrategyValid(t *testing.T) {
	assert.True(t, FallbackError.Valid())
	assert.True(t, FallbackBestEffort.Valid())
	assert.True(t, FallbackQueue.Valid())
	assert.False(t, FallbackStrategy("unknown").Valid())
}

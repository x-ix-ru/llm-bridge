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

func TestOpenCodeContextDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 4000, cfg.Global.OpenCodeContextBuffer)
	assert.Equal(t, 0, cfg.Global.OpenCodeContextInput)
}

func TestOpenCodeContextLoad_Override(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `
global:
  fallback_strategy: queue
  opencode_context_buffer: 5000
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	assert.Equal(t, 5000, cfg.Global.OpenCodeContextBuffer)
	assert.Equal(t, 0, cfg.Global.OpenCodeContextInput)
}

func TestOpenCodeContextSet_InvalidBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	store := NewStore(path)

	err := store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	cfg.Global.OpenCodeContextBuffer = -1

	err = store.Set(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "opencode_context_buffer must be > 0")
}

// ---------------------------------------------------------------------------
// Task 004 — ENV override helpers
// ---------------------------------------------------------------------------

func TestEnvInt_Helper(t *testing.T) {
	tests := []struct {
		name       string
		envKey     string
		envVal     string
		setEnv     bool
		defaultVal int
		expected   int
	}{
		{
			name:       "not_set_returns_default",
			envKey:     "TEST_ENVINT_NOTSET",
			setEnv:     false,
			defaultVal: 42,
			expected:   42,
		},
		{
			name:       "empty_string_returns_default",
			envKey:     "TEST_ENVINT_EMPTY",
			envVal:     "",
			setEnv:     true,
			defaultVal: 42,
			expected:   42,
		},
		{
			name:       "valid_int_overrides_default",
			envKey:     "TEST_ENVINT_VALID",
			envVal:     "100",
			setEnv:     true,
			defaultVal: 42,
			expected:   100,
		},
		{
			name:       "invalid_string_returns_default",
			envKey:     "TEST_ENVINT_INVALID",
			envVal:     "abc",
			setEnv:     true,
			defaultVal: 42,
			expected:   42,
		},
		{
			name:       "negative_int_overrides_default",
			envKey:     "TEST_ENVINT_NEG",
			envVal:     "-5",
			setEnv:     true,
			defaultVal: 42,
			expected:   -5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.envKey, tt.envVal)
			}
			got := envInt(tt.envKey, tt.defaultVal)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestEnvString_Helper(t *testing.T) {
	tests := []struct {
		name       string
		envKey     string
		envVal     string
		setEnv     bool
		defaultVal string
		expected   string
	}{
		{
			name:       "not_set_returns_default",
			envKey:     "TEST_ENVSTRING_NOTSET",
			setEnv:     false,
			defaultVal: "default",
			expected:   "default",
		},
		{
			name:       "empty_string_returns_default",
			envKey:     "TEST_ENVSTRING_EMPTY",
			envVal:     "",
			setEnv:     true,
			defaultVal: "default",
			expected:   "default",
		},
		{
			name:       "custom_value_overrides_default",
			envKey:     "TEST_ENVSTRING_CUSTOM",
			envVal:     "custom",
			setEnv:     true,
			defaultVal: "default",
			expected:   "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.envKey, tt.envVal)
			}
			got := envString(tt.envKey, tt.defaultVal)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Task 004 — Store.Load() ENV override tests
// ---------------------------------------------------------------------------

func TestStoreLoad_ENVOverrideIntFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  discovery_interval_sec: 15
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Setenv("DISCOVERY_INTERVAL_SEC", "99")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	assert.Equal(t, 99, cfg.Global.DiscoveryIntervalSec)
}

func TestStoreLoad_ENVOverrideStringField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  fallback_strategy: best_effort
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Setenv("FALLBACK_STRATEGY", "queue")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	assert.Equal(t, FallbackQueue, cfg.Global.FallbackStrategy)
}

func TestStoreLoad_ENVOverrideInvalidInt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  discovery_interval_sec: 15
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Setenv("DISCOVERY_INTERVAL_SEC", "abc")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	// Невалидный ENV → значение из YAML сохранено
	assert.Equal(t, 15, cfg.Global.DiscoveryIntervalSec)
}

func TestStoreLoad_ENVOverrideInvalidFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  fallback_strategy: best_effort
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Setenv("FALLBACK_STRATEGY", "invalid")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	// Невалидная стратегия → default FallbackError
	assert.Equal(t, FallbackError, cfg.Global.FallbackStrategy)
}

func TestStoreLoad_ENVOverrideNegativeInt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  request_timeout_sec: 60
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Setenv("REQUEST_TIMEOUT_SEC", "-10")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	// ENV парсится как -10, но applyEnvOverrides проверяет <= 0 → default 60
	assert.Equal(t, 60, cfg.Global.RequestTimeoutSec)
}

func TestStoreLoad_ENVNotSet_UsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  request_timeout_sec: 120
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// REQUEST_TIMEOUT_SEC не установлена

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	assert.Equal(t, 120, cfg.Global.RequestTimeoutSec)
}

func TestStoreLoad_ENVOverrideOpenCodeFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  opencode_context_buffer: 4000
  opencode_context_input: 0
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Setenv("OPENCODE_CONTEXT_BUFFER", "8000")
	t.Setenv("OPENCODE_CONTEXT_INPUT", "5000")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	assert.Equal(t, 8000, cfg.Global.OpenCodeContextBuffer)
	assert.Equal(t, 5000, cfg.Global.OpenCodeContextInput)
}

func TestStoreLoad_ENVOverrideOpenCodeBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
global:
  fallback_strategy: best_effort
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Setenv("OPENCODE_BASE_URL", "http://mybridge:9090")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	assert.Equal(t, "http://mybridge:9090", cfg.Global.OpenCodeBaseURL)
}

func TestStoreLoad_AllENVOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// YAML с дефолтными значениями (не совпадают с ENV)
	yamlContent := `
global:
  fallback_strategy: best_effort
  discovery_interval_sec: 15
  request_timeout_sec: 60
  queue_timeout_sec: 30
  drain_timeout_sec: 30
  shutdown_timeout_sec: 10
  opencode_base_url: http://yaml-url:8080
  opencode_context_buffer: 4000
  opencode_context_input: 0
`
	err := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Все 9 ENV-переменных одновременно
	t.Setenv("FALLBACK_STRATEGY", "queue")
	t.Setenv("DISCOVERY_INTERVAL_SEC", "100")
	t.Setenv("REQUEST_TIMEOUT_SEC", "200")
	t.Setenv("QUEUE_TIMEOUT_SEC", "50")
	t.Setenv("DRAIN_TIMEOUT_SEC", "45")
	t.Setenv("SHUTDOWN_TIMEOUT_SEC", "25")
	t.Setenv("OPENCODE_BASE_URL", "http://mybridge:9090")
	t.Setenv("OPENCODE_CONTEXT_BUFFER", "8000")
	t.Setenv("OPENCODE_CONTEXT_INPUT", "5000")

	store := NewStore(path)
	err = store.Load()
	require.NoError(t, err)

	cfg := store.Get()
	assert.Equal(t, FallbackQueue, cfg.Global.FallbackStrategy)
	assert.Equal(t, 100, cfg.Global.DiscoveryIntervalSec)
	assert.Equal(t, 200, cfg.Global.RequestTimeoutSec)
	assert.Equal(t, 50, cfg.Global.QueueTimeoutSec)
	assert.Equal(t, 45, cfg.Global.DrainTimeoutSec)
	assert.Equal(t, 25, cfg.Global.ShutdownTimeoutSec)
	assert.Equal(t, "http://mybridge:9090", cfg.Global.OpenCodeBaseURL)
	assert.Equal(t, 8000, cfg.Global.OpenCodeContextBuffer)
	assert.Equal(t, 5000, cfg.Global.OpenCodeContextInput)
}

// Package config handles YAML configuration persistence and validation
// for the llm-bridge proxy.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"sync"

	"gopkg.in/yaml.v3"
)

type FallbackStrategy string

const (
	FallbackError      FallbackStrategy = "error"
	FallbackBestEffort FallbackStrategy = "best_effort"
	FallbackQueue      FallbackStrategy = "queue"
)

func (s FallbackStrategy) Valid() bool {
	switch s {
	case FallbackError, FallbackBestEffort, FallbackQueue:
		return true
	}
	return false
}

type ServerConfig struct {
	URL                   string `yaml:"url"`
	Distance              int    `yaml:"distance"`
	MaxConcurrentRequests int    `yaml:"max_concurrent_requests"`
}

type GlobalConfig struct {
	FallbackStrategy      FallbackStrategy `yaml:"fallback_strategy"`
	DiscoveryIntervalSec  int              `yaml:"discovery_interval_sec"`
	RequestTimeoutSec     int              `yaml:"request_timeout_sec"`
	QueueTimeoutSec       int              `yaml:"queue_timeout_sec"`
	DrainTimeoutSec       int              `yaml:"drain_timeout_sec"`
	ShutdownTimeoutSec    int              `yaml:"shutdown_timeout_sec"`
	OpenCodeBaseURL       string           `yaml:"opencode_base_url,omitempty"`
	OpenCodeContextBuffer int              `yaml:"opencode_context_buffer,omitempty"`
	OpenCodeContextInput  int              `yaml:"opencode_context_input,omitempty"`
}

type Config struct {
	Global  GlobalConfig   `yaml:"global"`
	Servers []ServerConfig `yaml:"servers"`
}

func envInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return val
}

func envString(key string, defaultVal string) string {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	return v
}

func applyEnvOverrides(cfg *GlobalConfig) {
	cfg.FallbackStrategy = FallbackStrategy(
		envString("FALLBACK_STRATEGY", string(cfg.FallbackStrategy)),
	)
	if !cfg.FallbackStrategy.Valid() {
		cfg.FallbackStrategy = FallbackError
	}

	cfg.DiscoveryIntervalSec = envInt("DISCOVERY_INTERVAL_SEC", cfg.DiscoveryIntervalSec)
	if cfg.DiscoveryIntervalSec <= 0 {
		cfg.DiscoveryIntervalSec = 15
	}

	cfg.RequestTimeoutSec = envInt("REQUEST_TIMEOUT_SEC", cfg.RequestTimeoutSec)
	if cfg.RequestTimeoutSec <= 0 {
		cfg.RequestTimeoutSec = 60
	}

	cfg.QueueTimeoutSec = envInt("QUEUE_TIMEOUT_SEC", cfg.QueueTimeoutSec)
	if cfg.QueueTimeoutSec <= 0 {
		cfg.QueueTimeoutSec = 30
	}

	cfg.DrainTimeoutSec = envInt("DRAIN_TIMEOUT_SEC", cfg.DrainTimeoutSec)
	if cfg.DrainTimeoutSec <= 0 {
		cfg.DrainTimeoutSec = 30
	}

	cfg.ShutdownTimeoutSec = envInt("SHUTDOWN_TIMEOUT_SEC", cfg.ShutdownTimeoutSec)
	if cfg.ShutdownTimeoutSec <= 0 {
		cfg.ShutdownTimeoutSec = 10
	}

	if v := os.Getenv("OPENCODE_BASE_URL"); v != "" {
		cfg.OpenCodeBaseURL = v
	}

	cfg.OpenCodeContextBuffer = envInt("OPENCODE_CONTEXT_BUFFER", cfg.OpenCodeContextBuffer)
	if cfg.OpenCodeContextBuffer <= 0 {
		cfg.OpenCodeContextBuffer = 4000
	}

	cfg.OpenCodeContextInput = envInt("OPENCODE_CONTEXT_INPUT", cfg.OpenCodeContextInput)
	if cfg.OpenCodeContextInput < 0 {
		cfg.OpenCodeContextInput = 0
	}
}

func DefaultConfig() Config {
	return Config{
		Global: GlobalConfig{
			FallbackStrategy:      FallbackError,
			DiscoveryIntervalSec:  15,
			RequestTimeoutSec:     60,
			QueueTimeoutSec:       30,
			DrainTimeoutSec:       30,
			ShutdownTimeoutSec:    10,
			OpenCodeContextBuffer: 4000,
			OpenCodeContextInput:  0,
		},
		Servers: []ServerConfig{},
	}
}

type Store struct {
	mu       sync.RWMutex
	config   Config
	filePath string
}

func NewStore(filePath string) *Store {
	return &Store{
		config:   DefaultConfig(),
		filePath: filePath,
	}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.config = DefaultConfig()
			return s.saveLocked()
		}
		return fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if !cfg.Global.FallbackStrategy.Valid() {
		cfg.Global.FallbackStrategy = FallbackError
	}
	if cfg.Global.DiscoveryIntervalSec <= 0 {
		cfg.Global.DiscoveryIntervalSec = 15
	}
	if cfg.Global.RequestTimeoutSec <= 0 {
		cfg.Global.RequestTimeoutSec = 60
	}
	if cfg.Global.QueueTimeoutSec <= 0 {
		cfg.Global.QueueTimeoutSec = 30
	}
	if cfg.Global.DrainTimeoutSec <= 0 {
		cfg.Global.DrainTimeoutSec = 30
	}
	if cfg.Global.ShutdownTimeoutSec <= 0 {
		cfg.Global.ShutdownTimeoutSec = 10
	}
	if cfg.Global.OpenCodeContextBuffer <= 0 {
		cfg.Global.OpenCodeContextBuffer = 4000
	}

	applyEnvOverrides(&cfg.Global)

	s.config = cfg
	return nil
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *Store) Set(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !cfg.Global.FallbackStrategy.Valid() {
		return fmt.Errorf("invalid fallback strategy: %s", cfg.Global.FallbackStrategy)
	}
	for i, sv := range cfg.Servers {
		if sv.URL == "" {
			return fmt.Errorf("server %d: url is required", i)
		}
		if _, err := url.Parse(sv.URL); err != nil {
			return fmt.Errorf("server %d: invalid url: %w", i, err)
		}
		if sv.Distance < 1 || sv.Distance > 10 {
			return fmt.Errorf("server %d: distance must be 1-10", i)
		}
		if sv.MaxConcurrentRequests <= 0 {
			return fmt.Errorf("server %d: max_concurrent_requests must be > 0", i)
		}
	}

	if cfg.Global.OpenCodeContextBuffer == 0 {
		cfg.Global.OpenCodeContextBuffer = 4000
	}
	if cfg.Global.OpenCodeContextBuffer < 0 {
		return fmt.Errorf("opencode_context_buffer must be > 0")
	}

	s.config = cfg
	return s.saveLocked()
}

func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	data, err := yaml.Marshal(&s.config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// GetCopy returns a copy of the current configuration.
func (s *Store) GetCopy() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.config
	return cp
}

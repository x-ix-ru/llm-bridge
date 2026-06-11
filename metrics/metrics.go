// Package metrics provides a collector that fetches and parses
// vLLM Prometheus metrics from backend servers.
package metrics

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ServerMetrics holds the parsed vLLM metrics for a single server.
// Zero values are meaningful (e.g. no running requests) so omitempty is
// intentionally omitted on numeric fields.
type ServerMetrics struct {
	RequestsRunning   int       `json:"requests_running"`
	RequestsWaiting   int       `json:"requests_waiting"`
	KVCacheUsagePerc  float64   `json:"kv_cache_usage_perc"`
	PromptTokensTotal int64     `json:"prompt_tokens_total"`
	GenTokensTotal    int64     `json:"gen_tokens_total"`
	PrefillThroughput float64   `json:"prefill_throughput"`
	DecodeThroughput  float64   `json:"decode_throughput"`
	AvgPrefillTimeMS  float64   `json:"avg_prefill_time_ms"`
	AvgDecodeTimeMS   float64   `json:"avg_decode_time_ms"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// IsZero returns true if no metrics have been collected yet.
func (m *ServerMetrics) IsZero() bool {
	return m.UpdatedAt.IsZero()
}

// prevCounters stores previous cumulative values to compute throughput rates.
type prevCounters struct {
	promptTokens int64
	genTokens    int64
}

// Collector periodically fetches /metrics from a set of servers
// and caches the parsed results.
type Collector struct {
	mu          sync.RWMutex
	data        map[string]*ServerMetrics
	prev        map[string]*prevCounters
	currentURLs []string
	interval    time.Duration
	client      *http.Client
	logger      *slog.Logger
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// New creates a new Collector. The interval controls how often
// metrics are fetched from each server.
func New(interval time.Duration) *Collector {
	return &Collector{
		data:     make(map[string]*ServerMetrics),
		prev:     make(map[string]*prevCounters),
		interval: interval,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: slog.Default().With("component", "metrics"),
		stopCh: make(chan struct{}),
	}
}

// Start begins periodic metrics collection for the given server URLs.
// It blocks until the first fetch completes for all servers, then
// returns. The collector runs until the context is cancelled.
func (c *Collector) Start(ctx context.Context, urls []string) {
	c.mu.Lock()
	c.currentURLs = urls
	for _, u := range urls {
		if _, ok := c.data[u]; !ok {
			c.data[u] = &ServerMetrics{}
		}
	}
	c.mu.Unlock()

	// Initial fetch synchronously.
	c.fetchAll(ctx, urls)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.mu.RLock()
				current := c.currentURLs
				c.mu.RUnlock()
				c.fetchAll(ctx, current)
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			}
		}
	}()
}

// SetServers updates the set of server URLs to collect metrics from.
func (c *Collector) SetServers(urls []string) {
	c.mu.Lock()
	oldURLs := c.currentURLs
	c.currentURLs = urls

	// Add new entries.
	for _, u := range urls {
		if _, ok := c.data[u]; !ok {
			c.data[u] = &ServerMetrics{}
		}
	}
	// Remove stale entries.
	for u := range c.data {
		found := false
		for _, nu := range urls {
			if nu == u {
				found = true
				break
			}
		}
		if !found {
			delete(c.data, u)
		}
	}
	c.mu.Unlock()

	// Fetch new servers immediately so they appear on the dashboard faster.
	// Run in a goroutine to avoid blocking the admin API call.
	if len(urls) > len(oldURLs) {
		newURLs := make([]string, 0, len(urls))
		urlSet := make(map[string]bool, len(oldURLs))
		for _, u := range oldURLs {
			urlSet[u] = true
		}
		for _, u := range urls {
			if !urlSet[u] {
				newURLs = append(newURLs, u)
			}
		}
		if len(newURLs) > 0 {
			go c.fetchAll(context.Background(), newURLs)
		}
	}
}

// Stop stops the periodic collection and waits for any in-flight fetch to finish.
func (c *Collector) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

// fetchAll fetches metrics from all given URLs and computes throughput rates.
func (c *Collector) fetchAll(ctx context.Context, urls []string) {
	for _, u := range urls {
		if ctx.Err() != nil {
			return
		}
		m, err := c.fetchOne(ctx, u)
		if err != nil {
			c.logger.Debug("metrics fetch failed", "server", u, "error", err)
			continue
		}

		// Compute throughput rates from previous counters.
		// Only after the first successful poll (c.data[u].UpdatedAt non-zero).
		if prev, ok := c.prev[u]; ok {
			dt := m.UpdatedAt.Sub(c.data[u].UpdatedAt).Seconds()
			if dt > 0 && prev.promptTokens > 0 {
				m.PrefillThroughput = float64(m.PromptTokensTotal-prev.promptTokens) / dt
			}
			if dt > 0 && prev.genTokens > 0 {
				m.DecodeThroughput = float64(m.GenTokensTotal-prev.genTokens) / dt
			}
		}
		// Store current as previous for next cycle.
		c.prev[u] = &prevCounters{
			promptTokens: m.PromptTokensTotal,
			genTokens:    m.GenTokensTotal,
		}

		c.mu.Lock()
		c.data[u] = m
		c.mu.Unlock()
	}
}

// fetchOne fetches and parses /metrics from a single server.
func (c *Collector) fetchOne(ctx context.Context, serverURL string) (*ServerMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/metrics", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Server doesn't support /metrics – that's okay, leave zero metrics.
		return &ServerMetrics{}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}

	m := parsePrometheus(string(body))
	m.UpdatedAt = time.Now()
	return m, nil
}

// Get returns the latest cached metrics for a server URL, or nil if unknown.
func (c *Collector) Get(serverURL string) *ServerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data[serverURL]
}

// GetAll returns a copy of all cached metrics indexed by server URL.
func (c *Collector) GetAll() map[string]*ServerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[string]*ServerMetrics, len(c.data))
	for k, v := range c.data {
		cp := *v
		out[k] = &cp
	}
	return out
}

// Remove removes a server from the collector.
func (c *Collector) Remove(serverURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, serverURL)
}

// ---------------------------------------------------------------------------
// Prometheus parser
// ---------------------------------------------------------------------------

// stripLabels removes Prometheus labels from a metric name.
// "vllm:num_requests_running{engine=\"0\",model_name=\"smart\"}" → "vllm:num_requests_running"
func stripLabels(name string) string {
	if idx := strings.IndexByte(name, '{'); idx >= 0 {
		return name[:idx]
	}
	return name
}

// parsePrometheus extracts known vLLM metrics from a Prometheus text body.
// It handles labels (e.g. vllm:num_requests_running{...}) by stripping them
// before matching, and ignores HELP/TYPE lines.
func parsePrometheus(body string) *ServerMetrics {
	m := &ServerMetrics{}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on space or tab – the last token is the value.
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		name := stripLabels(parts[0])
		raw := parts[len(parts)-1]

		switch {
		case name == "vllm:num_requests_running":
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				m.RequestsRunning = int(math.Round(v))
			}
		case name == "vllm:num_requests_waiting":
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				m.RequestsWaiting = int(math.Round(v))
			}
		case name == "vllm:kv_cache_usage_perc":
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				m.KVCacheUsagePerc = v * 100 // convert 0-1 ratio → percentage
			}
		case name == "vllm:prompt_tokens_total":
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				m.PromptTokensTotal = int64(math.Round(v))
			}
		case name == "vllm:generation_tokens_total":
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				m.GenTokensTotal = int64(math.Round(v))
			}
		}
	}
	return m
}

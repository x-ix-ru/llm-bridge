// Package discovery provides periodic polling of backend servers to
// discover available LLM models and track server health status.
package discovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Discovery periodically polls backend servers to discover available models.
type Discovery struct {
	mu           sync.RWMutex
	servers      []string
	models       map[string][]string
	modelDetails map[string]json.RawMessage
	healthy      map[string]bool
	interval     time.Duration
	client       *http.Client
	cancel       context.CancelFunc
	started      bool
	wg           sync.WaitGroup
	logger       *slog.Logger
}

// modelsResponse is the top-level /v1/models response from OpenAI-compatible backends.
type modelsResponse struct {
	Object string            `json:"object"`
	Data   []json.RawMessage `json:"data"`
}

// New creates a new Discovery service with the given poll interval.
func New(interval time.Duration) *Discovery {
	return &Discovery{
		models:       make(map[string][]string),
		modelDetails: make(map[string]json.RawMessage),
		healthy:      make(map[string]bool),
		interval:     interval,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: slog.Default(),
	}
}

// SetServers updates the list of servers to poll.
// Newly added servers will be logged as "unknown → healthy/unhealthy"
// on the next Discover() call.
func (d *Discovery) SetServers(urls []string) {
	d.mu.Lock()
	d.servers = make([]string, len(urls))
	copy(d.servers, urls)
	d.mu.Unlock()
}

// Start begins the periodic polling in a background goroutine.
// It respects context cancellation for graceful shutdown.
func (d *Discovery) Start(ctx context.Context) {
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return
	}
	ctx, d.cancel = context.WithCancel(ctx)
	d.started = true
	d.mu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(d.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = d.Discover(ctx)
			}
		}
	}()
}

// Stop cancels the polling goroutine and waits for it to exit.
func (d *Discovery) Stop() {
	d.mu.Lock()
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.started = false
	d.mu.Unlock()
	d.wg.Wait()
}

// ModelDetails returns a copy of the current model metadata map.
// Each value is the full raw JSON returned by the upstream server.
func (d *Discovery) ModelDetails() map[string]json.RawMessage {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]json.RawMessage, len(d.modelDetails))
	for k, v := range d.modelDetails {
		cp := make(json.RawMessage, len(v))
		copy(cp, v)
		result[k] = cp
	}
	return result
}

// Models returns a copy of the current model->servers map.
func (d *Discovery) Models() map[string][]string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string][]string, len(d.models))
	for k, v := range d.models {
		servers := make([]string, len(v))
		copy(servers, v)
		result[k] = servers
	}
	return result
}

// Healthy returns the health status of all servers.
func (d *Discovery) Healthy() map[string]bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]bool, len(d.healthy))
	for k, v := range d.healthy {
		result[k] = v
	}
	return result
}

// Discover performs a single poll of all servers and updates state.
// This can be called manually or happens periodically via Start().
func (d *Discovery) Discover(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	d.mu.RLock()
	servers := make([]string, len(d.servers))
	copy(servers, d.servers)
	d.mu.RUnlock()

	for _, url := range servers {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/v1/models", nil)
		if err != nil {
			d.setHealthy(url, false, err)
			d.logger.Debug("discovery: request creation failed", "server", url, "error", err)
			continue
		}

		resp, err := d.client.Do(req)
		if err != nil {
			d.setHealthy(url, false, err)
			d.logger.Debug("discovery: request failed", "server", url, "error", err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		var body modelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			d.setHealthy(url, false, err)
			d.logger.Debug("discovery: decode failed", "server", url, "error", err)
			continue
		}
		resp.Body.Close()

		modelIDs := make([]string, 0, len(body.Data))
		for _, raw := range body.Data {
			var meta struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &meta); err != nil || meta.ID == "" {
				continue
			}
			modelIDs = append(modelIDs, meta.ID)
			// Store full raw metadata on first encounter (first server wins).
			d.mu.Lock()
			if _, exists := d.modelDetails[meta.ID]; !exists {
				d.modelDetails[meta.ID] = raw
			}
			d.mu.Unlock()
		}

		d.updateModels(url, modelIDs)
		d.setHealthy(url, true, nil)
	}

	return nil
}

// IsHealthy returns true if the server is currently healthy.
func (d *Discovery) IsHealthy(url string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.healthy[url]
}

// ServersForModel returns the list of healthy servers that have the given model.
func (d *Discovery) ServersForModel(model string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	servers, ok := d.models[model]
	if !ok {
		return nil
	}

	result := make([]string, 0, len(servers))
	for _, url := range servers {
		if d.healthy[url] {
			result = append(result, url)
		}
	}
	return result
}

// setHealthy updates the health status for a server and logs the
// transition if it changed.
func (d *Discovery) setHealthy(url string, healthy bool, err error) {
	d.mu.Lock()
	prev, existed := d.healthy[url]
	d.healthy[url] = healthy
	d.mu.Unlock()

	var fromStatus string
	switch {
	case !existed:
		fromStatus = "unknown"
	case prev:
		fromStatus = "healthy"
	default:
		fromStatus = "unhealthy"
	}

	var toStatus string
	if healthy {
		toStatus = "healthy"
	} else {
		toStatus = "unhealthy"
	}

	if fromStatus == toStatus {
		return
	}

	attrs := []any{
		"component", "discovery",
		"server_url", url,
		"from_status", fromStatus,
		"to_status", toStatus,
	}
	if err != nil && !healthy {
		attrs = append(attrs, "error", err.Error())
	}
	d.logger.Info("server status changed", attrs...)
}

func (d *Discovery) updateModels(url string, modelIDs []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	advertised := make(map[string]bool, len(modelIDs))
	for _, id := range modelIDs {
		advertised[id] = true
	}

	for model, servers := range d.models {
		filtered := make([]string, 0, len(servers))
		for _, s := range servers {
			if s != url {
				filtered = append(filtered, s)
			}
		}
		if advertised[model] {
			filtered = append(filtered, url)
		}
		if len(filtered) == 0 {
			delete(d.models, model)
			delete(d.modelDetails, model)
		} else {
			d.models[model] = filtered
		}
		delete(advertised, model)
	}

	for id := range advertised {
		d.models[id] = []string{url}
	}
}

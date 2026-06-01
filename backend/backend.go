// Package backend manages a pool of backend servers with HTTP clients
// and atomic inflight request tracking.
package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"llm-bridge/config"
)

// ErrServerUnhealthy is returned when a server is not found or marked unhealthy.
var ErrServerUnhealthy = errors.New("server unhealthy")

// Pool manages a set of backend servers with inflight request tracking.
type Pool struct {
	mu      sync.RWMutex
	clients map[string]*backendClient
}

type backendClient struct {
	server   config.ServerConfig
	client   *http.Client
	inflight atomic.Int64
}

// Conn represents an acquired connection slot to a backend server.
// Caller must call Release() when done.
type Conn struct {
	ServerID string
	Pool     *Pool
}

// NewPool creates a new Pool.
func NewPool() *Pool {
	return &Pool{
		clients: make(map[string]*backendClient),
	}
}

// Acquire acquires a connection slot to the given server.
// It increments the inflight counter. If the server is not found,
// ErrServerUnhealthy is returned. If the context is cancelled,
// the counter is decremented back.
func (p *Pool) Acquire(ctx context.Context, serverID string) (*Conn, error) {
	p.mu.RLock()
	bc, ok := p.clients[serverID]
	p.mu.RUnlock()
	if !ok {
		return nil, ErrServerUnhealthy
	}

	bc.inflight.Add(1)

	select {
	case <-ctx.Done():
		bc.inflight.Add(-1)
		return nil, ctx.Err()
	default:
	}

	return &Conn{
		ServerID: serverID,
		Pool:     p,
	}, nil
}

// Release decrements the inflight counter for the connection's server.
func (p *Pool) Release(conn *Conn) {
	p.mu.RLock()
	bc, ok := p.clients[conn.ServerID]
	p.mu.RUnlock()
	if !ok {
		return
	}
	bc.inflight.Add(-1)
}

// Inflight returns the current inflight count for the given server.
func (p *Pool) Inflight(serverID string) int64 {
	p.mu.RLock()
	bc, ok := p.clients[serverID]
	p.mu.RUnlock()
	if !ok {
		return 0
	}
	return bc.inflight.Load()
}

// AddServer adds or replaces a backend server in the pool.
func (p *Pool) AddServer(cfg config.ServerConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	p.clients[cfg.URL] = &backendClient{
		server: cfg,
		client: &http.Client{
			Transport: transport,
		},
	}
}

// RemoveServer removes a backend server from the pool by its URL.
func (p *Pool) RemoveServer(serverID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.clients, serverID)
}

// ServerCount returns the number of servers in the pool.
func (p *Pool) ServerCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}

// ProxyRequest proxies an HTTP request to the backend server associated with
// the given connection. It copies response headers and status code from the
// upstream response. The response body is streamed directly without buffering.
func (p *Pool) ProxyRequest(ctx context.Context, conn *Conn, method, path string, body io.Reader, headers http.Header) (*http.Response, error) {
	p.mu.RLock()
	bc, ok := p.clients[conn.ServerID]
	p.mu.RUnlock()
	if !ok {
		return nil, ErrServerUnhealthy
	}

	base, err := url.Parse(conn.ServerID)
	if err != nil {
		return nil, fmt.Errorf("parse server url: %w", err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse request path: %w", err)
	}
	fullURL := base.ResolveReference(ref)

	req, err := http.NewRequestWithContext(ctx, method, fullURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for key, vals := range headers {
		for _, v := range vals {
			req.Header.Add(key, v)
		}
	}

	resp, err := bc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy request: %w", err)
	}

	return resp, nil
}

// GetServer returns the server config for the given server ID (URL).
func (p *Pool) GetServer(serverID string) (config.ServerConfig, bool) {
	p.mu.RLock()
	bc, ok := p.clients[serverID]
	p.mu.RUnlock()
	if !ok {
		return config.ServerConfig{}, false
	}
	return bc.server, true
}

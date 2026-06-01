// Package server provides the HTTP server for the llm-bridge proxy,
// implementing OpenAI-compatible API endpoints and admin API.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"llm-bridge/backend"
	"llm-bridge/config"
	"llm-bridge/discovery"
	"llm-bridge/metrics"
	"llm-bridge/router"
	"llm-bridge/web"
)

// Server is the HTTP server for the llm-bridge proxy.
// It serves the OpenAI-compatible API and the admin API.
type Server struct {
	cfg     *config.Store
	disc    *discovery.Discovery
	pool    *backend.Pool
	rtr     *router.Router
	metrics *metrics.Collector
	mux     *chi.Mux
	httpSrv *http.Server
	logger  slog.Logger
	addr    string

	bridgeReqsSuccess atomic.Int64
	bridgeReqsError   atomic.Int64
}

// New creates a new Server, sets up all routes, and synchronises
// the pool and discovery with the config.
func New(cfg *config.Store, disc *discovery.Discovery, pool *backend.Pool, rtr *router.Router, mc *metrics.Collector, addr string) *Server {
	s := &Server{
		cfg:     cfg,
		disc:    disc,
		pool:    pool,
		rtr:     rtr,
		metrics: mc,
		logger:  *slog.Default(),
		addr:    addr,
	}
	s.setupRoutes()
	s.syncServers()
	return s
}

// Handler returns the HTTP handler for use with httptest or other servers.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// setupRoutes configures all HTTP routes on the chi mux.
func (s *Server) setupRoutes() {
	s.mux = chi.NewMux()

	// OpenAI API
	s.mux.Post("/v1/chat/completions", s.handleChatCompletions)
	s.mux.Post("/v1/completions", s.handleCompletions)
	s.mux.Post("/v1/embeddings", s.handleEmbeddings)
	s.mux.Get("/v1/models", s.handleModels)

	// Admin API
	s.mux.Get("/admin/servers", s.handleListServers)
	s.mux.Post("/admin/servers", s.handleAddServer)
	s.mux.Get("/admin/servers/*", s.handleGetServer)
	s.mux.Put("/admin/servers/*", s.handleUpdateServer)
	s.mux.Delete("/admin/servers/*", s.handleDeleteServer)
	s.mux.Get("/admin/config", s.handleGetConfig)
	s.mux.Put("/admin/config", s.handlePutConfig)
	s.mux.Get("/admin/status", s.handleStatus)
	s.mux.Get("/admin/opencode-config", s.handleOpenCodeConfig)
	s.mux.Get("/admin/metrics", s.handleMetrics)

	// Admin UI static files (must be registered after API routes to avoid conflicts)
	webFS := http.FS(web.Static)
	fileServer := http.FileServer(webFS)
	s.mux.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	s.mux.Handle("/admin/static/*", http.StripPrefix("/admin/", fileServer))
	s.mux.Get("/admin/*", s.handleAdminUI)
}

// handleAdminUI serves the admin SPA entry point for all /admin/ paths not handled by API routes.
func (s *Server) handleAdminUI(w http.ResponseWriter, r *http.Request) {
	data, err := web.Static.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "admin UI not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// Start starts the HTTP server. It blocks until the server exits.
func (s *Server) Start(ctx context.Context) error {
	s.httpSrv = &http.Server{
		Addr:    s.addr,
		Handler: s.mux,
	}

	s.logger.Info("starting server", "addr", s.addr)
	if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

// syncServers synchronises the pool, discovery, and metrics collector with the current config.
func (s *Server) syncServers() {
	cfg := s.cfg.Get()
	for _, sc := range cfg.Servers {
		s.pool.AddServer(sc)
	}
	urls := make([]string, len(cfg.Servers))
	for i, sc := range cfg.Servers {
		urls[i] = sc.URL
	}
	s.disc.SetServers(urls)
	if s.metrics != nil {
		s.metrics.SetServers(urls)
	}
}

// ---------------------------------------------------------------------------
// OpenAI API handlers
// ---------------------------------------------------------------------------

type openAIRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// handleChatCompletions proxies a chat completions request to a backend server.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleOpenAIProxy(w, r, true)
}

// handleCompletions proxies a text completions request to a backend server.
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleOpenAIProxy(w, r, false)
}

// handleEmbeddings proxies an embeddings request to a backend server.
func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	s.handleOpenAIProxy(w, r, false)
}

// handleOpenAIProxy is the shared implementation for all OpenAI-style proxy
// endpoints. It reads the request body, extracts the model, routes the request
// to a backend server, and proxies the response back to the client.
func (s *Server) handleOpenAIProxy(w http.ResponseWriter, r *http.Request, checkStream bool) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req openAIRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Model == "" {
		s.respondError(w, http.StatusBadRequest, "model field is required")
		return
	}

	ctx := r.Context()
	conn, err := s.rtr.Route(ctx, req.Model)
	if err != nil {
		switch {
		case errors.Is(err, router.ErrNoServers):
			s.respondError(w, http.StatusServiceUnavailable, "no servers available for model: "+req.Model)
		case errors.Is(err, router.ErrAllBusy):
			s.respondError(w, http.StatusServiceUnavailable, "all servers at capacity")
		case errors.Is(err, router.ErrQueueTimeout):
			s.respondError(w, http.StatusServiceUnavailable, "queue wait timed out")
		case errors.Is(err, context.Canceled):
			s.respondError(w, http.StatusServiceUnavailable, "request canceled")
		default:
			s.respondError(w, http.StatusInternalServerError, "routing error: "+err.Error())
		}
		return
	}
	defer s.pool.Release(conn)

	headers := r.Header.Clone()
	if checkStream && req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
	}

	resp, err := s.pool.ProxyRequest(ctx, conn, r.Method, r.URL.Path, bytes.NewReader(bodyBytes), headers)
	if err != nil {
		s.bridgeReqsError.Add(1)
		s.respondError(w, http.StatusBadGateway, "proxy error: "+err.Error())
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		if k == "Content-Type" && checkStream && req.Stream {
			w.Header().Set(k, vals[0])
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		s.logger.Warn("stream copy failed", "error", err)
	}
	s.bridgeReqsSuccess.Add(1)
}

// handleModels returns the list of models aggregated from discovery.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := s.disc.Models()
	details := s.disc.ModelDetails()

	// Use json.RawMessage so we preserve all upstream metadata verbatim.
	data := make([]json.RawMessage, 0, len(models))
	for name := range models {
		if raw, ok := details[name]; ok {
			data = append(data, raw)
		} else {
			entry, _ := json.Marshal(map[string]string{"id": name, "object": "model"})
			data = append(data, json.RawMessage(entry))
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}

// ---------------------------------------------------------------------------
// Admin API handlers
// ---------------------------------------------------------------------------

type serverResponse struct {
	URL                   string `json:"url"`
	Distance              int    `json:"distance"`
	MaxConcurrentRequests int    `json:"max_concurrent_requests"`
	Healthy               bool   `json:"healthy"`
	Inflight              int64  `json:"inflight"`
}

// handleListServers returns all configured servers with their runtime state.
func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	healthy := s.disc.Healthy()

	resp := make([]serverResponse, 0, len(cfg.Servers))
	for _, sc := range cfg.Servers {
		resp = append(resp, serverResponse{
			URL:                   sc.URL,
			Distance:              sc.Distance,
			MaxConcurrentRequests: sc.MaxConcurrentRequests,
			Healthy:               healthy[sc.URL],
			Inflight:              s.pool.Inflight(sc.URL),
		})
	}

	s.respondJSON(w, http.StatusOK, resp)
}

// handleAddServer adds a new server to the config, pool, and discovery.
func (s *Server) handleAddServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL                   string `json:"url"`
		Distance              int    `json:"distance"`
		MaxConcurrentRequests int    `json:"max_concurrent_requests"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	sc := config.ServerConfig{
		URL:                   req.URL,
		Distance:              req.Distance,
		MaxConcurrentRequests: req.MaxConcurrentRequests,
	}

	cfg := s.cfg.Get()
	for _, existing := range cfg.Servers {
		if existing.URL == sc.URL {
			s.respondError(w, http.StatusConflict, "server already exists: "+sc.URL)
			return
		}
	}

	cfg.Servers = append(cfg.Servers, sc)
	if err := s.cfg.Set(cfg); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.syncServers()

	s.respondJSON(w, http.StatusCreated, sc)
}

// handleGetServer returns a single server by its URL.
func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "*")
	if rawID == "" {
		s.respondError(w, http.StatusBadRequest, "missing server id")
		return
	}
	id, err := url.PathUnescape(rawID)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid server id encoding")
		return
	}

	cfg := s.cfg.Get()
	for _, sc := range cfg.Servers {
		if sc.URL == id {
			healthy := s.disc.IsHealthy(id)
			s.respondJSON(w, http.StatusOK, serverResponse{
				URL:                   sc.URL,
				Distance:              sc.Distance,
				MaxConcurrentRequests: sc.MaxConcurrentRequests,
				Healthy:               healthy,
				Inflight:              s.pool.Inflight(sc.URL),
			})
			return
		}
	}

	s.respondError(w, http.StatusNotFound, "server not found: "+id)
}

// handleUpdateServer updates an existing server's configuration.
func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "*")
	if rawID == "" {
		s.respondError(w, http.StatusBadRequest, "missing server id")
		return
	}
	id, err := url.PathUnescape(rawID)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid server id encoding")
		return
	}

	var req struct {
		URL                   *string `json:"url"`
		Distance              *int    `json:"distance"`
		MaxConcurrentRequests *int    `json:"max_concurrent_requests"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	cfg := s.cfg.Get()
	var found bool
	var updated config.ServerConfig
	for i, sc := range cfg.Servers {
		if sc.URL == id {
			updated = sc
			if req.URL != nil {
				updated.URL = *req.URL
			}
			if req.Distance != nil {
				updated.Distance = *req.Distance
			}
			if req.MaxConcurrentRequests != nil {
				updated.MaxConcurrentRequests = *req.MaxConcurrentRequests
			}
			cfg.Servers[i] = updated
			found = true
			break
		}
	}

	if !found {
		s.respondError(w, http.StatusNotFound, "server not found: "+id)
		return
	}

	if err := s.cfg.Set(cfg); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// If the URL changed, remove the old one from the pool.
	if updated.URL != id {
		s.pool.RemoveServer(id)
	}

	s.syncServers()

	s.respondJSON(w, http.StatusOK, updated)
}

// handleDeleteServer removes a server from the config, pool, and discovery.
func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "*")
	if rawID == "" {
		s.respondError(w, http.StatusBadRequest, "missing server id")
		return
	}
	id, err := url.PathUnescape(rawID)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid server id encoding")
		return
	}

	cfg := s.cfg.Get()
	var found bool
	for i, sc := range cfg.Servers {
		if sc.URL == id {
			cfg.Servers = append(cfg.Servers[:i], cfg.Servers[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		s.respondError(w, http.StatusNotFound, "server not found: "+id)
		return
	}

	if err := s.cfg.Set(cfg); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.pool.RemoveServer(id)
	s.syncServers()

	w.WriteHeader(http.StatusNoContent)
}

// handleGetConfig returns the current configuration as JSON.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	s.respondJSON(w, http.StatusOK, cfg)
}

// handlePutConfig updates the configuration from a JSON or YAML body.
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	contentType := r.Header.Get("Content-Type")
	var newCfg config.Config

	switch {
	case strings.Contains(contentType, "json"):
		if err := json.Unmarshal(bodyBytes, &newCfg); err != nil {
			s.respondError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	case strings.Contains(contentType, "yaml"), strings.Contains(contentType, "yml"):
		if err := yaml.Unmarshal(bodyBytes, &newCfg); err != nil {
			s.respondError(w, http.StatusBadRequest, "invalid YAML: "+err.Error())
			return
		}
	default:
		// Try JSON first, then YAML.
		if err := json.Unmarshal(bodyBytes, &newCfg); err != nil {
			if err2 := yaml.Unmarshal(bodyBytes, &newCfg); err2 != nil {
				s.respondError(w, http.StatusBadRequest, "unable to parse config as JSON or YAML")
				return
			}
		}
	}

	if err := s.cfg.Set(newCfg); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.syncServers()

	s.respondJSON(w, http.StatusOK, s.cfg.Get())
}

// handleStatus returns the server health, inflight counts, model-to-server
// mappings, and vLLM metrics.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	healthy := s.disc.Healthy()
	models := s.disc.Models()

	type serverInfo struct {
		URL                   string                 `json:"url"`
		Distance              int                    `json:"distance"`
		MaxConcurrentRequests int                    `json:"max_concurrent_requests"`
		Healthy               bool                   `json:"healthy"`
		Inflight              int64                  `json:"inflight"`
		Metrics               *metrics.ServerMetrics `json:"metrics"`
	}

	servers := make([]serverInfo, 0, len(cfg.Servers))
	for _, sc := range cfg.Servers {
		var sm *metrics.ServerMetrics
		if s.metrics != nil {
			sm = s.metrics.Get(sc.URL)
			if sm != nil && sm.IsZero() {
				sm = nil
			}
		}
		servers = append(servers, serverInfo{
			URL:                   sc.URL,
			Distance:              sc.Distance,
			MaxConcurrentRequests: sc.MaxConcurrentRequests,
			Healthy:               healthy[sc.URL],
			Inflight:              s.pool.Inflight(sc.URL),
			Metrics:               sm,
		})
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"servers": servers,
		"models":  models,
		"healthy": healthy,
	})
}

// handleMetrics exposes combined vLLM + bridge metrics in Prometheus text format.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	allMetrics := make(map[string]*metrics.ServerMetrics)
	if s.metrics != nil {
		allMetrics = s.metrics.GetAll()
	}
	out := exportMetrics(allMetrics, s.pool,
		s.bridgeReqsSuccess.Load(),
		s.bridgeReqsError.Load(),
	)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// respondError writes a JSON error response in OpenAI-compatible format.
func (s *Server) respondError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]interface{}{
		"error": map[string]string{
			"message": msg,
			"type":    "error",
		},
	})
	_, _ = w.Write(body)
}

// respondJSON writes a JSON response with the given status code.
func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := json.Marshal(data)
	_, _ = w.Write(body)
}

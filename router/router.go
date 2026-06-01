// Package router implements the server selection algorithm for the
// llm-bridge proxy. It selects the optimal backend server based on
// distance, load, and fallback strategy.
package router

import (
	"context"
	"errors"
	"sort"
	"sync"

	"llm-bridge/backend"
	"llm-bridge/config"
	"llm-bridge/discovery"
)

var (
	ErrNoServers    = errors.New("no servers available for model")
	ErrAllBusy      = errors.New("all servers at capacity")
	ErrQueueTimeout = errors.New("queue wait timed out")
)

type serverInfo struct {
	url      string
	distance int
	maxConn  int
	inflight int64
}

// Router selects the optimal backend server for a given model request.
type Router struct {
	cfg      *config.Store
	disc     *discovery.Discovery
	pool     *backend.Pool
	rrIndex  map[string]int
	rrMu     sync.Mutex
	queueMgr *queueManager
}

// New creates a new Router.
func New(cfg *config.Store, disc *discovery.Discovery, pool *backend.Pool) *Router {
	r := &Router{
		cfg:     cfg,
		disc:    disc,
		pool:    pool,
		rrIndex: make(map[string]int),
	}
	r.queueMgr = newQueueManager(r)
	return r
}

// Route selects the best backend server for the given model.
func (r *Router) Route(ctx context.Context, model string) (*backend.Conn, error) {
	return r.route(ctx, model, true)
}

func (r *Router) route(ctx context.Context, model string, allowQueue bool) (*backend.Conn, error) {
	cfg := r.cfg.Get()
	servers := r.disc.ServersForModel(model)
	if len(servers) == 0 {
		return nil, ErrNoServers
	}

	var infos []serverInfo
	for _, url := range servers {
		sc, ok := r.pool.GetServer(url)
		if !ok {
			continue
		}
		infos = append(infos, serverInfo{
			url:      url,
			distance: sc.Distance,
			maxConn:  sc.MaxConcurrentRequests,
			inflight: r.pool.Inflight(url),
		})
	}
	if len(infos) == 0 {
		return nil, ErrNoServers
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].distance < infos[j].distance
	})

	var groups [][]serverInfo
	for i := 0; i < len(infos); {
		j := i
		for j < len(infos) && infos[j].distance == infos[i].distance {
			j++
		}
		groups = append(groups, infos[i:j])
		i = j
	}

	for _, group := range groups {
		r.rrMu.Lock()
		idx := r.rrIndex[model] % len(group)
		r.rrIndex[model] = idx + 1
		r.rrMu.Unlock()

		for k := 0; k < len(group); k++ {
			si := group[(idx+k)%len(group)]
			if si.inflight < int64(si.maxConn) {
				conn, err := r.pool.Acquire(ctx, si.url)
				if err == nil {
					return conn, nil
				}
			}
		}
	}

	strategy := cfg.Global.FallbackStrategy

	if strategy == config.FallbackBestEffort {
		var best serverInfo
		var bestRatio float64
		var found bool

		for _, si := range infos {
			ratio := float64(si.inflight) / float64(si.maxConn)
			if !found || ratio < bestRatio {
				best = si
				bestRatio = ratio
				found = true
			}
		}

		if found {
			conn, err := r.pool.Acquire(ctx, best.url)
			if err == nil {
				return conn, nil
			}
		}
	}

	if strategy == config.FallbackQueue && allowQueue {
		return r.queueMgr.enqueue(ctx, model)
	}

	return nil, ErrAllBusy
}

// Drain rejects all queued requests and stops the queue manager goroutine.
func (r *Router) Drain() {
	r.queueMgr.drain()
}

// Stop gracefully stops the queue manager goroutine.
// Must be called after Drain() to release the goroutine.
func (r *Router) Stop() {
	r.queueMgr.stop()
}

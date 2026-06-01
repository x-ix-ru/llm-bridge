package router

import (
	"context"
	"sync"
	"time"

	"llm-bridge/backend"
)

type queueItem struct {
	model    string
	deadline time.Time
	ctx      context.Context
	result   chan *queueResult
}

type queueResult struct {
	conn *backend.Conn
	err  error
}

type queueManager struct {
	mu       sync.Mutex
	queue    []*queueItem
	router   *Router
	signalCh chan struct{}
	stopCh   chan struct{}
	started  bool
}

func newQueueManager(r *Router) *queueManager {
	return &queueManager{
		router:   r,
		signalCh: make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
	}
}

func (qm *queueManager) enqueue(ctx context.Context, model string) (*backend.Conn, error) {
	qm.ensureStarted()

	cfg := qm.router.cfg.Get()
	maxWait := time.Duration(cfg.Global.QueueTimeoutSec) * time.Second
	deadline := time.Now().Add(maxWait)
	if d, ok := ctx.Deadline(); ok {
		if d.Before(deadline) {
			deadline = d
		}
	}

	result := make(chan *queueResult, 1)

	item := &queueItem{
		model:    model,
		deadline: deadline,
		ctx:      ctx,
		result:   result,
	}

	qm.mu.Lock()
	qm.queue = append(qm.queue, item)
	qm.mu.Unlock()

	qm.notify()

	select {
	case res := <-result:
		return res.conn, res.err
	case <-ctx.Done():
		select {
		case res := <-result:
			if res.conn != nil {
				qm.router.pool.Release(res.conn)
			}
			return nil, ctx.Err()
		default:
			qm.removeItem(item)
			return nil, ctx.Err()
		}
	}
}

func (qm *queueManager) removeItem(item *queueItem) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	for i, it := range qm.queue {
		if it == item {
			qm.queue = append(qm.queue[:i], qm.queue[i+1:]...)
			return
		}
	}
}

func (qm *queueManager) ensureStarted() {
	qm.mu.Lock()
	if !qm.started {
		qm.started = true
		go qm.run()
	}
	qm.mu.Unlock()
}

func (qm *queueManager) run() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-qm.signalCh:
			qm.processQueue()
		case <-ticker.C:
			qm.processQueue()
		case <-qm.stopCh:
			return
		}
	}
}

func (qm *queueManager) processQueue() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	now := time.Now()
	var remaining []*queueItem

	for _, item := range qm.queue {
		if now.After(item.deadline) {
			item.result <- &queueResult{err: ErrQueueTimeout}
			continue
		}

		conn, err := qm.router.route(item.ctx, item.model, false)
		if err == nil {
			item.result <- &queueResult{conn: conn}
			continue
		}

		remaining = append(remaining, item)
	}

	qm.queue = remaining
}

func (qm *queueManager) notify() {
	select {
	case qm.signalCh <- struct{}{}:
	default:
	}
}

func (qm *queueManager) drain() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	for _, item := range qm.queue {
		item.result <- &queueResult{err: context.Canceled}
	}
	qm.queue = nil
}

func (qm *queueManager) stop() {
	select {
	case qm.stopCh <- struct{}{}:
	default:
	}
}

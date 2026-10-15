package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/netpulse/netpulse-backend/internal/domain"
	"github.com/netpulse/netpulse-backend/internal/probes"
)

type Store interface {
	ListEnabledNodes(context.Context) ([]domain.Node, error)
	RecordProbe(context.Context, uuid.UUID, domain.ProbeResult) error
	CleanupOldSamples(context.Context, int) error
	MarkStaleAgentsOffline(context.Context, time.Duration) error
}
type Worker struct {
	store         Store
	runner        *probes.Runner
	interval      time.Duration
	timeout       time.Duration
	retentionDays int
	logger        *slog.Logger
}

func New(store Store, runner *probes.Runner, interval, timeout time.Duration, retentionDays int, logger *slog.Logger) *Worker {
	return &Worker{store: store, runner: runner, interval: interval, timeout: timeout, retentionDays: retentionDays, logger: logger}
}

func (w *Worker) Run(ctx context.Context) {
	lastChecked := make(map[uuid.UUID]time.Time)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cleanupTicker := time.NewTicker(6 * time.Hour)
	defer cleanupTicker.Stop()
	agentStaleTicker := time.NewTicker(15 * time.Second)
	defer agentStaleTicker.Stop()
	// Run immediately so a newly created node does not wait for the first tick.
	w.runDue(ctx, lastChecked)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runDue(ctx, lastChecked)
		case <-cleanupTicker.C:
			if err := w.store.CleanupOldSamples(ctx, w.retentionDays); err != nil {
				w.logger.Error("metric cleanup failed", "error", err)
			}
		case <-agentStaleTicker.C:
			if err := w.store.MarkStaleAgentsOffline(ctx, 45*time.Second); err != nil {
				w.logger.Error("mark stale agents offline", "error", err)
			}
		}
	}
}

func (w *Worker) runDue(ctx context.Context, lastChecked map[uuid.UUID]time.Time) {
	nodes, err := w.store.ListEnabledNodes(ctx)
	if err != nil {
		w.logger.Error("load nodes for monitoring", "error", err)
		return
	}
	now := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, node := range nodes {
		interval := time.Duration(node.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = w.interval
		}
		if last, ok := lastChecked[node.ID]; ok && now.Sub(last) < interval {
			continue
		}
		lastChecked[node.ID] = now
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(n domain.Node) {
			defer wg.Done()
			defer func() { <-sem }()
			result := w.runner.Check(ctx, n)
			if err := w.recordWithTimeout(ctx, n.ID, result); err != nil {
				w.logger.Error("record probe result", "node_id", n.ID, "error", err)
			}
		}(node)
	}
	wg.Wait()
}

func (w *Worker) recordWithTimeout(parent context.Context, id uuid.UUID, result domain.ProbeResult) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	return w.store.RecordProbe(ctx, id, result)
}

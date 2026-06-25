// Package scheduler runs each configured service check on its own interval and
// persists the latest status to storage.
package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/timanthonyalexander/heartd/internal/checks"
	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// Scheduler evaluates a node's checks on schedule and writes their status to the
// database so the dashboard can read current state.
type Scheduler struct {
	db     *storage.DB
	node   string
	checks []config.Check
}

// New builds a Scheduler for the given node and check set.
func New(db *storage.DB, node string, checkList []config.Check) *Scheduler {
	return &Scheduler{db: db, node: node, checks: checkList}
}

// Run launches one goroutine per check, each running on its configured interval,
// and blocks until ctx is cancelled. Each check runs once immediately on start.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, c := range s.checks {
		wg.Add(1)
		go func(c config.Check) {
			defer wg.Done()
			s.runLoop(ctx, c)
		}(c)
	}
	wg.Wait()
}

func (s *Scheduler) runLoop(ctx context.Context, c config.Check) {
	s.evaluate(ctx, c)

	ticker := time.NewTicker(c.Interval.Std())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evaluate(ctx, c)
		}
	}
}

// evaluate runs one check and persists its result.
func (s *Scheduler) evaluate(ctx context.Context, c config.Check) {
	res := checks.Run(ctx, c)
	status := storage.CheckStatus{
		Node:      s.node,
		Name:      c.Name,
		Type:      c.Type,
		Status:    string(res.Status),
		Detail:    res.Detail,
		LatencyMS: res.LatencyMS,
		At:        res.At,
	}
	if err := s.db.UpsertCheckStatus(status); err != nil {
		log.Printf("scheduler: persist check %q failed: %v", c.Name, err)
	}
}

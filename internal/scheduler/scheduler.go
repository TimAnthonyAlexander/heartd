// Package scheduler runs configured service checks on their schedules and
// persists their status. The check list is read live from settings, so checks
// added, edited, or removed in the dashboard take effect without a restart.
package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/timanthonyalexander/heartd/internal/alert"
	"github.com/timanthonyalexander/heartd/internal/checks"
	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// tickInterval is how often the scheduler reconciles the check list against
// what is due to run. It bounds scheduling granularity, not check frequency.
const tickInterval = time.Second

// Scheduler evaluates a node's checks on their individual intervals.
type Scheduler struct {
	db       *storage.DB
	node     string
	settings *settings.Service
	engine   *alert.Engine // optional; nil when alerting is disabled

	mu       sync.Mutex
	lastRun  map[string]time.Time
	inflight map[string]bool
}

// New builds a Scheduler. engine may be nil.
func New(db *storage.DB, node string, set *settings.Service, engine *alert.Engine) *Scheduler {
	return &Scheduler{
		db:       db,
		node:     node,
		settings: set,
		engine:   engine,
		lastRun:  make(map[string]time.Time),
		inflight: make(map[string]bool),
	}
}

// Run reconciles and dispatches due checks each tick until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.seedAlertState()

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

// reconcile launches any enabled check that is due and not already running.
func (s *Scheduler) reconcile(ctx context.Context) {
	now := time.Now()
	for _, c := range s.settings.Checks() {
		if !c.Enabled || c.Interval <= 0 {
			continue
		}
		s.mu.Lock()
		last, seen := s.lastRun[c.Name]
		due := !seen || now.Sub(last) >= c.Interval
		if due && !s.inflight[c.Name] {
			s.lastRun[c.Name] = now
			s.inflight[c.Name] = true
			s.mu.Unlock()
			go s.run(ctx, c)
		} else {
			s.mu.Unlock()
		}
	}
}

func (s *Scheduler) run(ctx context.Context, c settings.Check) {
	defer func() {
		s.mu.Lock()
		s.inflight[c.Name] = false
		s.mu.Unlock()
	}()
	s.evaluate(ctx, c)
}

// seedAlertState primes the alert engine with each check's last persisted status
// so a restart does not re-alert an already-failing check.
func (s *Scheduler) seedAlertState() {
	if s.engine == nil {
		return
	}
	stored, err := s.db.CheckStatuses(s.node)
	if err != nil {
		log.Printf("scheduler: seed alert state failed: %v", err)
		return
	}
	for _, st := range stored {
		s.engine.SeedCheck(s.node, st.Name, st.Status)
	}
}

// evaluate runs one check, persists its result, and reports it to the alert
// engine (which fires only on transitions).
func (s *Scheduler) evaluate(ctx context.Context, c settings.Check) {
	res := checks.Run(ctx, toConfigCheck(c))
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
	if s.engine != nil {
		s.engine.ObserveCheck(s.node, c.Name, c.Type, string(res.Status), res.Detail)
	}
}

func toConfigCheck(c settings.Check) config.Check {
	return config.Check{
		Name:     c.Name,
		Type:     c.Type,
		Interval: config.Duration(c.Interval),
		Timeout:  config.Duration(c.Timeout),
		URL:      c.URL,
		Method:   c.Method,
		Host:     c.Host,
		PortNum:  c.Port,
		Process:  c.Process,
		Command:  c.Command,
	}
}

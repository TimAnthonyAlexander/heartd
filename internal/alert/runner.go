package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// evalInterval is how often the runner re-evaluates all alert rules.
const evalInterval = 5 * time.Second

// Runner periodically evaluates the configured alert rules against current
// storage and feeds the results to the Engine. It reads the rule list fresh each
// tick (the live-reload pattern), so rules edited in the dashboard take effect
// without a restart. Self-metric rules (cpu/mem/disk/check/net) evaluate the
// local node; peer/nodata rules evaluate this node's peers.
type Runner struct {
	db       *storage.DB
	selfName string
	settings *settings.Service
	engine   *Engine
}

// NewRunner builds a Runner.
func NewRunner(db *storage.DB, selfName string, set *settings.Service, engine *Engine) *Runner {
	return &Runner{db: db, selfName: selfName, settings: set, engine: engine}
}

// Run primes baseline state once (no alerts), then evaluates every interval until
// ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	r.evaluate(true, time.Now().UTC()) // seed pass — primes state, dispatches nothing
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(evalInterval):
			r.evaluate(false, time.Now().UTC())
		}
	}
}

func (r *Runner) evaluate(seed bool, now time.Time) {
	for _, rule := range r.settings.AlertRules() {
		if !rule.Enabled {
			continue
		}
		r.evalRule(rule, seed, now)
	}
}

func view(rule storage.AlertRule) RuleView {
	return RuleView{ID: rule.ID, Name: rule.Name, Source: rule.Source, Severity: rule.Severity, ForSec: rule.ForSec, RecoverGrace: rule.RecoverGrace}
}

func (r *Runner) evalRule(rule storage.AlertRule, seed bool, now time.Time) {
	v := view(rule)
	switch rule.Source {
	case settings.SourceCPU:
		if m, ok, _ := r.db.LatestMetric(r.selfName); ok {
			r.numeric(v, r.selfName, "", m.CPUPercent, "CPU", "%", rule, seed, now)
		}
	case settings.SourceMem:
		if m, ok, _ := r.db.LatestMetric(r.selfName); ok {
			r.numeric(v, r.selfName, "", m.MemPercent, "Memory", "%", rule, seed, now)
		}
	case settings.SourceNetRecv:
		if n, ok, _ := r.db.LatestNetSample(r.selfName); ok {
			r.numericRate(v, r.selfName, n.RecvRate, "Recv", rule, seed, now)
		}
	case settings.SourceNetSent:
		if n, ok, _ := r.db.LatestNetSample(r.selfName); ok {
			r.numericRate(v, r.selfName, n.SentRate, "Sent", rule, seed, now)
		}
	case settings.SourceDisk:
		disks, _ := r.db.DiskStatuses(r.selfName)
		for _, d := range disks {
			if rule.Entity != "*" && rule.Entity != d.Mount {
				continue
			}
			r.numeric(v, r.selfName, d.Mount, d.Percent, "Disk", "%", rule, seed, now)
		}
	case settings.SourceCheckStatus:
		checks, _ := r.db.CheckStatuses(r.selfName)
		for _, c := range checks {
			if rule.Entity != "*" && rule.Entity != c.Name {
				continue
			}
			met := c.Status == "failing"
			detail := c.Detail
			if detail == "" {
				detail = "check is failing"
			}
			r.engine.Observe(v, r.selfName, c.Name, detail, met, seed, now)
		}
	case settings.SourceCheckLatency:
		checks, _ := r.db.CheckStatuses(r.selfName)
		for _, c := range checks {
			if rule.Entity != "*" && rule.Entity != c.Name {
				continue
			}
			met := compareNum(float64(c.LatencyMS), rule.Comparator, rule.Threshold)
			detail := fmt.Sprintf("latency %dms %s %.0fms", c.LatencyMS, rule.Comparator, rule.Threshold)
			r.engine.Observe(v, r.selfName, c.Name, detail, met, seed, now)
		}
	case settings.SourcePeer:
		peers, _ := r.db.ListPeers()
		for _, p := range peers {
			if !p.Enabled {
				continue // muted peers are not alerted on
			}
			if rule.Entity != "*" && rule.Entity != p.Name {
				continue
			}
			met := p.Status == "down"
			detail := p.LastError
			if detail == "" {
				detail = "node is unreachable"
			}
			r.engine.Observe(v, p.Name, "", detail, met, seed, now)
		}
	case settings.SourceNoData:
		peers, _ := r.db.ListPeers()
		for _, p := range peers {
			if !p.Enabled {
				continue // muted peers are not alerted on
			}
			if rule.Entity != "*" && rule.Entity != p.Name {
				continue
			}
			age := staleSeconds(r.db, p.Name, now)
			met := compareNum(age, rule.Comparator, rule.Threshold)
			detail := fmt.Sprintf("no fresh data for %.0fs (threshold %.0fs)", age, rule.Threshold)
			r.engine.Observe(v, p.Name, "", detail, met, seed, now)
		}
	}
}

// numeric evaluates a percent/ms numeric source.
func (r *Runner) numeric(v RuleView, node, entity string, value float64, label, unit string, rule storage.AlertRule, seed bool, now time.Time) {
	met := compareNum(value, rule.Comparator, rule.Threshold)
	detail := fmt.Sprintf("%s %.1f%s %s %.0f%s", label, value, unit, rule.Comparator, rule.Threshold, unit)
	r.engine.Observe(v, node, entity, detail, met, seed, now)
}

// numericRate evaluates a bytes/sec network-rate source (compared in bytes/s,
// displayed in MB/s).
func (r *Runner) numericRate(v RuleView, node string, value float64, label string, rule storage.AlertRule, seed bool, now time.Time) {
	met := compareNum(value, rule.Comparator, rule.Threshold)
	detail := fmt.Sprintf("%s %.2f MB/s %s %.2f MB/s", label, value/1e6, rule.Comparator, rule.Threshold/1e6)
	r.engine.Observe(v, node, "", detail, met, seed, now)
}

// staleSeconds returns how many seconds old the newest metric sample for node is,
// or a very large number if there is no data at all.
func staleSeconds(db *storage.DB, node string, now time.Time) float64 {
	m, ok, err := db.LatestMetric(node)
	if err != nil || !ok {
		return 1e9
	}
	return now.Sub(m.At).Seconds()
}

func compareNum(value float64, comparator string, threshold float64) bool {
	switch comparator {
	case ">":
		return value > threshold
	case "<=":
		return value <= threshold
	case "<":
		return value < threshold
	default: // ">=" and anything unexpected
		return value >= threshold
	}
}

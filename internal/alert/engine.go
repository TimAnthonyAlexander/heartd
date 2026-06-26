package alert

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
)

// Status values used for checks and peers.
const (
	statusUnknown = "unknown"
	statusOK      = "ok"
	statusFailing = "failing"
	statusDown    = "down"
)

// Engine is the deduplication brain. It records the last known state of every
// observed entity and dispatches an alert ONLY when an observation differs from
// the prior state (edge-triggered). It is safe for concurrent use.
type Engine struct {
	dispatcher dispatcher
	thresholds func() config.Thresholds

	mu         sync.Mutex
	checkState map[string]string // node|name  -> ok|failing|unknown
	peerState  map[string]string // name       -> ok|down|unknown
	metricOver map[string]bool   // node|cpu, node|mem -> over threshold?
}

// NewEngine builds an Engine that dispatches via d, reading thresholds fresh from
// the provider on each metric evaluation (so runtime-edited thresholds apply
// immediately).
func NewEngine(d *Dispatcher, thresholds func() config.Thresholds) *Engine {
	return &Engine{
		dispatcher: d,
		thresholds: thresholds,
		checkState: make(map[string]string),
		peerState:  make(map[string]string),
		metricOver: make(map[string]bool),
	}
}

func checkKey(node, name string) string { return node + "|" + name }

// SeedCheck sets the baseline state for a check WITHOUT firing an alert. Called
// at startup from the DB so a restart does not re-alert an already-failing
// check.
func (e *Engine) SeedCheck(node, name, status string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.checkState[checkKey(node, name)] = status
}

// SeedPeer sets the baseline state for a peer WITHOUT firing an alert.
func (e *Engine) SeedPeer(name, status string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.peerState[name] = status
}

// ForgetNode discards all remembered state for a node — its peer status, every
// check keyed to it, and its metric-threshold flags. Called when a node is
// removed so a later re-add starts from a clean baseline rather than inheriting
// stale state (which could suppress or spuriously fire an alert).
func (e *Engine) ForgetNode(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.peerState, name)
	prefix := name + "|"
	for k := range e.checkState {
		if strings.HasPrefix(k, prefix) {
			delete(e.checkState, k)
		}
	}
	for k := range e.metricOver {
		if strings.HasPrefix(k, prefix) {
			delete(e.metricOver, k)
		}
	}
}

// ObserveCheck compares a check's new status to its last known status and
// dispatches an alert only on a transition. Safe to call concurrently.
func (e *Engine) ObserveCheck(node, name, checkType, status, detail string) {
	key := checkKey(node, name)

	e.mu.Lock()
	prior, ok := e.checkState[key]
	if !ok {
		prior = statusUnknown
	}
	e.checkState[key] = status

	if status == prior {
		e.mu.Unlock()
		return
	}

	var alert *Alert
	switch {
	case status == statusFailing:
		alert = &Alert{
			Firing: true,
			Title:  fmt.Sprintf("Check %q is failing on %s", name, node),
			Detail: detail,
		}
	case status == statusOK && prior == statusFailing:
		alert = &Alert{
			Firing: false,
			Title:  fmt.Sprintf("Check %q recovered on %s", name, node),
			Detail: detail,
		}
	}
	e.mu.Unlock()

	if alert == nil {
		return
	}
	alert.Kind = KindCheck
	alert.Node = node
	alert.Subject = name
	e.fire(*alert)
}

// ObservePeer compares a peer's new status to its last known status and
// dispatches an alert only on a transition. Safe to call concurrently.
func (e *Engine) ObservePeer(name, status string) {
	e.mu.Lock()
	prior, ok := e.peerState[name]
	if !ok {
		prior = statusUnknown
	}
	e.peerState[name] = status

	if status == prior {
		e.mu.Unlock()
		return
	}

	var alert *Alert
	switch {
	case status == statusDown:
		alert = &Alert{
			Firing: true,
			Title:  fmt.Sprintf("Node %s is unreachable", name),
		}
	case status == statusOK && prior == statusDown:
		alert = &Alert{
			Firing: false,
			Title:  fmt.Sprintf("Node %s recovered", name),
		}
	}
	e.mu.Unlock()

	if alert == nil {
		return
	}
	alert.Kind = KindPeer
	alert.Node = name
	alert.Subject = name
	e.fire(*alert)
}

// ObserveMetric evaluates CPU, Memory, and Disk percentages independently
// against their thresholds and dispatches alerts only on a crossing. A
// threshold <= 0 disables that metric entirely. diskPercent should be the
// highest usage across the node's mounts. Safe to call concurrently.
func (e *Engine) ObserveMetric(node string, cpuPercent, memPercent, diskPercent float64) {
	th := e.thresholds()
	e.evaluateMetric(node, "cpu", "CPU", cpuPercent, th.CPUPercent)
	e.evaluateMetric(node, "mem", "Memory", memPercent, th.MemPercent)
	e.evaluateMetric(node, "disk", "Disk", diskPercent, th.DiskPercent)
}

// evaluateMetric handles a single metric's threshold crossing.
func (e *Engine) evaluateMetric(node, suffix, label string, value, threshold float64) {
	if threshold <= 0 {
		// Metric disabled — never fire, and don't track state.
		return
	}
	key := node + "|" + suffix
	over := value >= threshold

	e.mu.Lock()
	prior := e.metricOver[key] // absent => false
	e.metricOver[key] = over

	if over == prior {
		e.mu.Unlock()
		return
	}

	var alert Alert
	if over {
		alert = Alert{
			Firing: true,
			Title:  fmt.Sprintf("%s on %s is high: %.1f%% (threshold %.1f%%)", label, node, value, threshold),
			Detail: fmt.Sprintf("%s usage %.1f%% exceeded threshold %.1f%%", label, value, threshold),
		}
	} else {
		alert = Alert{
			Firing: false,
			Title:  fmt.Sprintf("%s on %s recovered: %.1f%%", label, node, value),
			Detail: fmt.Sprintf("%s usage %.1f%% back below threshold %.1f%%", label, value, threshold),
		}
	}
	e.mu.Unlock()

	alert.Kind = KindMetric
	alert.Node = node
	alert.Subject = label
	e.fire(alert)
}

// fire stamps the alert time and dispatches it. Never called while holding the
// mutex.
func (e *Engine) fire(a Alert) {
	a.Time = time.Now().UTC()
	e.dispatcher.Dispatch(a)
}

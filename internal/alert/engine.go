package alert

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Alert levels. A rule is either ok or firing at its configured severity.
const (
	levelOK = "ok"
)

// RuleView is the slice of an alert rule the Engine needs to run its state
// machine. The runner builds it from the persisted rule so the Engine stays
// decoupled from storage.
type RuleView struct {
	ID           int64
	Name         string
	Source       string // rule source (cpu|peer|nodata|…); carried into the dedup key
	Severity     string // warning | critical
	ForSec       int64  // sustained duration before firing
	RecoverGrace int64  // keep-firing-for after recovery (anti-flap)
}

// ruleState is the remembered state for one (rule, node, entity) triple.
type ruleState struct {
	level       string    // "ok" | severity
	breachSince time.Time // when the condition first became true; zero if not breaching
	recoveredAt time.Time // when it first cleared while firing; drives RecoverGrace
	firingSince time.Time // when this entity started firing; zero when ok (survives recovery grace)

	// Context captured on the last observation, so ActiveAlerts can describe a
	// currently-firing entity without re-deriving it from the key.
	node    string
	entity  string
	source  string
	subject string
	detail  string
}

// ActiveAlert is a snapshot of one entity the Engine currently considers firing.
// It is a value copy; the Engine never hands out pointers to its internal state.
type ActiveAlert struct {
	Node        string
	Entity      string
	Source      string
	Subject     string
	Severity    string
	Detail      string
	BreachSince time.Time
}

// Engine is the deduplication brain. It records the last known state of every
// observed (rule, node, entity) and dispatches an alert only on a transition
// (edge-triggered), honouring each rule's sustained-duration and recovery-grace
// timers. It is safe for concurrent use.
type Engine struct {
	dispatcher dispatcher
	coord      *Coordinator // optional cross-node dedup; nil = send everything locally
	recorder   func(Alert)  // optional incident-history sink; nil = record nothing

	mu    sync.Mutex
	state map[string]ruleState
}

// NewEngine builds an Engine that dispatches via d.
func NewEngine(d *Dispatcher) *Engine {
	return &Engine{dispatcher: d, state: make(map[string]ruleState)}
}

// SetCoordinator enables cross-node alert deduplication. When set, alerts about
// a peer are gated through the Coordinator so only one node delivers them.
func (e *Engine) SetCoordinator(c *Coordinator) { e.coord = c }

// SetRecorder installs a sink that persists every confirmed transition (one
// firing, one recovered) to the incident history. The recorder is invoked OFF
// the Engine's lock and only for real edges — never during a Seed pass — so the
// restart-safety priming records no synthetic history.
func (e *Engine) SetRecorder(r func(Alert)) { e.recorder = r }

// gatedDispatch sends a through the coordinator (off the caller's goroutine, so
// the runner loop never blocks on peer queries) when one is configured; without
// a coordinator it dispatches directly.
func (e *Engine) gatedDispatch(a Alert) {
	if e.coord == nil {
		e.dispatcher.Dispatch(a)
		return
	}
	go func() {
		if e.coord.ShouldSend(a) {
			e.dispatcher.Dispatch(a)
		} else {
			log.Printf("alert: suppressed %q — another node is notifying about this incident", a.Title)
		}
	}()
}

func ruleKey(ruleID int64, node, entity string) string {
	return fmt.Sprintf("%d|%s|%s", ruleID, node, entity)
}

// Observe records the current truth of a rule for one (node, entity) and
// dispatches an alert only when the firing/recovered state actually changes,
// gated by the rule's ForSec (must hold continuously before firing) and
// RecoverGrace (must stay clear before recovering).
//
// When seed is true the Engine only primes baseline state and never dispatches —
// used at startup so an already-breaching rule does not re-alert on restart.
func (e *Engine) Observe(rule RuleView, node, entity, detail string, conditionMet, seed bool, now time.Time) {
	key := ruleKey(rule.ID, node, entity)
	forDur := time.Duration(rule.ForSec) * time.Second
	graceDur := time.Duration(rule.RecoverGrace) * time.Second
	severity := rule.Severity
	if severity == "" {
		severity = "warning"
	}

	e.mu.Lock()
	st := e.state[key]
	if st.level == "" {
		st.level = levelOK // an unseen entity starts from the ok baseline
	}

	var toFire *Alert

	if seed {
		// Prime steady-state without dispatching (and without recording history).
		if conditionMet {
			st.level = severity
			st.breachSince = now.Add(-forDur)
			st.firingSince = st.breachSince
			st.recoveredAt = time.Time{}
		} else {
			st = ruleState{level: levelOK}
		}
		st.node, st.entity, st.source, st.subject, st.detail = node, entity, rule.Source, rule.Name, detail
		e.state[key] = st
		e.mu.Unlock()
		return
	}

	if conditionMet {
		st.recoveredAt = time.Time{}
		if st.breachSince.IsZero() {
			st.breachSince = now
		}
		if st.level == levelOK && now.Sub(st.breachSince) >= forDur {
			st.level = severity
			st.firingSince = st.breachSince
			toFire = &Alert{
				Firing: true,
				Title:  fmt.Sprintf("%s — %s", rule.Name, scope(node, entity)),
				Detail: detail,
			}
		}
	} else {
		st.breachSince = time.Time{}
		if st.level != levelOK {
			if st.recoveredAt.IsZero() {
				st.recoveredAt = now
			}
			if now.Sub(st.recoveredAt) >= graceDur {
				st.level = levelOK
				st.recoveredAt = time.Time{}
				st.firingSince = time.Time{}
				toFire = &Alert{
					Firing: false,
					Title:  fmt.Sprintf("%s recovered — %s", rule.Name, scope(node, entity)),
					Detail: detail,
				}
			}
		}
	}

	st.node, st.entity, st.source, st.subject, st.detail = node, entity, rule.Source, rule.Name, detail
	e.state[key] = st
	e.mu.Unlock()

	if toFire == nil {
		return
	}
	toFire.Kind = KindRule
	toFire.RuleID = rule.ID
	toFire.Source = rule.Source
	toFire.Node = node
	toFire.Entity = entity
	toFire.Subject = rule.Name
	toFire.Severity = severity
	toFire.Time = now.UTC()
	// Persist the transition (firing or recovered) to the incident history before
	// dispatch. Done outside the lock so the recorder can't deadlock the Engine,
	// and unconditionally — history is per-node-local and not subject to the
	// cross-node send election that gatedDispatch applies.
	if e.recorder != nil {
		e.recorder(*toFire)
	}
	e.gatedDispatch(*toFire)
}

// ActiveAlerts returns a snapshot of every entity the Engine currently considers
// firing (a breached rule that has not yet recovered, including those still
// within their recovery grace). The result is a copy; no internal state is
// shared with the caller.
func (e *Engine) ActiveAlerts() []ActiveAlert {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ActiveAlert, 0)
	for _, st := range e.state {
		if st.level == "" || st.level == levelOK {
			continue
		}
		out = append(out, ActiveAlert{
			Node:        st.node,
			Entity:      st.entity,
			Source:      st.source,
			Subject:     st.subject,
			Severity:    st.level,
			Detail:      st.detail,
			BreachSince: st.firingSince,
		})
	}
	return out
}

// ActiveAlertsForNode is ActiveAlerts filtered to a single node.
func (e *Engine) ActiveAlertsForNode(node string) []ActiveAlert {
	all := e.ActiveAlerts()
	out := make([]ActiveAlert, 0, len(all))
	for _, a := range all {
		if a.Node == node {
			out = append(out, a)
		}
	}
	return out
}

// Forget drops all remembered state for a rule (e.g. when it is deleted), so a
// later rule reusing those keys starts clean.
func (e *Engine) Forget(ruleID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prefix := fmt.Sprintf("%d|", ruleID)
	for k := range e.state {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(e.state, k)
		}
	}
}

// ForgetNode drops all remembered state concerning a node (e.g. when a peer is
// removed), across every rule, so its alerts don't linger.
func (e *Engine) ForgetNode(node string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k := range e.state {
		parts := strings.SplitN(k, "|", 3)
		if len(parts) == 3 && parts[1] == node {
			delete(e.state, k)
		}
	}
}

// scope renders the node (and entity, if any) for an alert title.
func scope(node, entity string) string {
	if entity != "" && entity != "*" {
		return node + " [" + entity + "]"
	}
	return node
}

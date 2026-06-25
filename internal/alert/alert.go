// Package alert implements heartd's alerting subsystem: edge-triggered state
// transition detection (the dedup "brain"), plus delivery over optional Email
// (SMTP) and Webhook (HTTP POST JSON) channels.
//
// The central guarantee is deduplication: exactly ONE alert fires when a
// problem begins and exactly ONE when it recovers. An ongoing failure never
// produces repeated alerts. The Engine enforces this by comparing each new
// observation to the last known state and dispatching only on a transition.
package alert

import (
	"context"
	"log"
	"time"
)

// Kind classifies what an Alert concerns.
type Kind string

// Alert kinds.
const (
	KindCheck  Kind = "check"
	KindPeer   Kind = "peer"
	KindMetric Kind = "metric"
)

// Alert is a single notification about a state transition.
type Alert struct {
	Kind    Kind      // check | peer | metric
	Node    string    // node the alert concerns
	Subject string    // check name, peer name, or metric name ("CPU"/"Memory")
	Firing  bool      // true = problem began; false = recovered
	Title   string    // short human line, e.g. `Check "Google" is failing on web-01`
	Detail  string    // extra context (the check detail, the metric value, etc.)
	Time    time.Time // when the alert was generated (UTC)
}

// Status returns "firing" when the alert represents a problem beginning, else
// "recovered". It is used in the webhook JSON payload.
func (a Alert) Status() string {
	if a.Firing {
		return "firing"
	}
	return "recovered"
}

// Notifier delivers an Alert over one channel.
type Notifier interface {
	Send(ctx context.Context, a Alert) error
	Name() string // for logging, e.g. "email", "webhook"
}

// sendTimeout bounds a single notifier delivery attempt.
const sendTimeout = 8 * time.Second

// Dispatcher fans an Alert out to all configured notifiers without blocking the
// caller on network I/O.
type Dispatcher struct {
	notifiers []Notifier
}

// NewDispatcher builds a Dispatcher over the given notifiers. Nil notifiers are
// skipped so callers can pass optional channels directly.
func NewDispatcher(notifiers ...Notifier) *Dispatcher {
	filtered := make([]Notifier, 0, len(notifiers))
	for _, n := range notifiers {
		if n != nil {
			filtered = append(filtered, n)
		}
	}
	return &Dispatcher{notifiers: filtered}
}

// Empty reports whether no notifiers are configured.
func (d *Dispatcher) Empty() bool { return len(d.notifiers) == 0 }

// Dispatch delivers a to all notifiers. It does NOT block the caller on network
// I/O: the sends run in a background goroutine, each with its own timeout
// context. A failed send is logged (via the standard log package), not retried.
func (d *Dispatcher) Dispatch(a Alert) {
	if d == nil || len(d.notifiers) == 0 {
		return
	}
	notifiers := d.notifiers
	go func() {
		for _, n := range notifiers {
			ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
			if err := n.Send(ctx, a); err != nil {
				log.Printf("alert: %s delivery failed for %q: %v", n.Name(), a.Title, err)
			}
			cancel()
		}
	}()
}

// dispatcher is the minimal interface the Engine relies on, allowing tests to
// substitute a synchronous or recording implementation.
type dispatcher interface {
	Dispatch(a Alert)
}

// ensure *Dispatcher satisfies the internal interface.
var _ dispatcher = (*Dispatcher)(nil)

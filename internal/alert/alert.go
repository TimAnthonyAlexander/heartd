// Package alert implements heartd's alerting subsystem: edge-triggered state
// transition detection (the dedup "brain"), plus delivery over optional Email
// (SMTP), Webhook (HTTP POST JSON), Slack, Discord, and Telegram channels.
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
	KindRule   Kind = "rule"
)

// Alert is a single notification about a state transition.
type Alert struct {
	Kind     Kind      // rule (or legacy check|peer|metric)
	RuleID   int64     // the alert rule that produced this (0 if none)
	Source   string    // rule source (cpu|mem|disk|peer|nodata|...); part of the dedup key
	Node     string    // node the alert concerns
	Entity   string    // mount / check / peer the rule targets ("" if n/a)
	Subject  string    // rule name (or legacy check/peer/metric name)
	Severity string    // warning | critical ("" for legacy)
	Firing   bool      // true = problem began; false = recovered
	Title    string    // short human line
	Detail   string    // extra context (the value vs threshold, the check detail, etc.)
	Time     time.Time // when the alert was generated (UTC)
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

// Dispatcher fans an Alert out to its current notifiers without blocking the
// caller on network I/O. The notifier set is resolved via a provider on each
// dispatch, so notification settings can change at runtime.
type Dispatcher struct {
	provider func() []Notifier
}

// NewDispatcher builds a Dispatcher over a fixed set of notifiers. Nil notifiers
// are skipped so callers can pass optional channels directly.
func NewDispatcher(notifiers ...Notifier) *Dispatcher {
	filtered := make([]Notifier, 0, len(notifiers))
	for _, n := range notifiers {
		if n != nil {
			filtered = append(filtered, n)
		}
	}
	return &Dispatcher{provider: func() []Notifier { return filtered }}
}

// NewDynamicDispatcher builds a Dispatcher whose notifiers are resolved fresh on
// every dispatch via provider — used so runtime-edited notify settings take
// effect immediately.
func NewDynamicDispatcher(provider func() []Notifier) *Dispatcher {
	return &Dispatcher{provider: provider}
}

// Empty reports whether no notifiers are currently configured.
func (d *Dispatcher) Empty() bool { return len(d.provider()) == 0 }

// Dispatch delivers a to the current notifiers. It does NOT block the caller on
// network I/O: the sends run in a background goroutine, each with its own
// timeout context. A failed send is logged, not retried.
func (d *Dispatcher) Dispatch(a Alert) {
	if d == nil {
		return
	}
	notifiers := d.provider()
	if len(notifiers) == 0 {
		return
	}
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

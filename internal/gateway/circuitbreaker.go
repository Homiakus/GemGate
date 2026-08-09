package gateway

import (
	"sync"
	"time"
)

const (
	defaultCircuitFailureThreshold = 5
	defaultCircuitOpenFor          = 30 * time.Second
)

type circuitState string

const (
	circuitClosed   circuitState = "closed"
	circuitOpen     circuitState = "open"
	circuitHalfOpen circuitState = "half_open"
)

type circuitPolicy struct {
	enabled          bool
	failureThreshold int
	openFor          time.Duration
}

type circuitPermit struct {
	probe bool
}

type circuitBreaker struct {
	mu               sync.Mutex
	enabled          bool
	state            circuitState
	failures         int
	failureThreshold int
	openFor          time.Duration
	openUntil        time.Time
}

type CircuitSnapshot struct {
	Provider   string
	State      string
	Failures   int
	RetryAfter time.Duration
}

func defaultCircuitPolicy() circuitPolicy {
	return circuitPolicy{enabled: true, failureThreshold: defaultCircuitFailureThreshold, openFor: defaultCircuitOpenFor}
}

func newCircuitBreaker(threshold int, openFor time.Duration) *circuitBreaker {
	policy := defaultCircuitPolicy()
	if threshold > 0 {
		policy.failureThreshold = threshold
	}
	if openFor > 0 {
		policy.openFor = openFor
	}
	return newCircuitBreakerWithPolicy(policy)
}

func newCircuitBreakerWithPolicy(policy circuitPolicy) *circuitBreaker {
	if policy.failureThreshold <= 0 {
		policy.failureThreshold = defaultCircuitFailureThreshold
	}
	if policy.openFor <= 0 {
		policy.openFor = defaultCircuitOpenFor
	}
	return &circuitBreaker{
		enabled:          policy.enabled,
		state:            circuitClosed,
		failureThreshold: policy.failureThreshold,
		openFor:          policy.openFor,
	}
}

// cloneWithPolicy creates a breaker for a new immutable runtime snapshot.
// The current circuit state is copied, while the old breaker remains owned by
// in-flight requests that started before the reload.
func (b *circuitBreaker) cloneWithPolicy(policy circuitPolicy, now time.Time) *circuitBreaker {
	if b == nil {
		return newCircuitBreakerWithPolicy(policy)
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	next := newCircuitBreakerWithPolicy(policy)
	if !next.enabled || !b.enabled {
		return next
	}
	next.state = b.state
	next.failures = b.failures
	next.openUntil = b.openUntil

	if next.state == circuitOpen && !now.Before(next.openUntil) {
		next.state = circuitHalfOpen
		next.openUntil = time.Time{}
	}
	if next.state == circuitClosed && next.failures >= next.failureThreshold {
		next.state = circuitOpen
		next.openUntil = now.Add(next.openFor)
	}
	return next
}

func (b *circuitBreaker) allow(now time.Time) (circuitPermit, bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.enabled {
		return circuitPermit{}, true, 0
	}
	switch b.state {
	case circuitOpen:
		if now.Before(b.openUntil) {
			return circuitPermit{}, false, b.openUntil.Sub(now)
		}
		b.state = circuitHalfOpen
		return circuitPermit{probe: true}, true, 0
	case circuitHalfOpen:
		return circuitPermit{}, false, time.Second
	default:
		return circuitPermit{}, true, 0
	}
}

func (b *circuitBreaker) finish(permit circuitPermit, failed bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.enabled {
		return
	}
	if permit.probe {
		if failed {
			b.state = circuitOpen
			b.failures = b.failureThreshold
			b.openUntil = now.Add(b.openFor)
			return
		}
		b.state = circuitClosed
		b.failures = 0
		b.openUntil = time.Time{}
		return
	}

	// A request admitted while closed can complete after a concurrent request
	// opens the breaker. Such a late completion must not alter the opened state.
	if b.state != circuitClosed {
		return
	}
	if !failed {
		b.failures = 0
		return
	}
	b.failures++
	if b.failures >= b.failureThreshold {
		b.state = circuitOpen
		b.openUntil = now.Add(b.openFor)
	}
}

func (b *circuitBreaker) snapshot(now time.Time) CircuitSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.enabled {
		return CircuitSnapshot{State: "disabled"}
	}
	state := b.state
	retry := time.Duration(0)
	if state == circuitOpen {
		if now.Before(b.openUntil) {
			retry = b.openUntil.Sub(now)
		} else {
			state = circuitHalfOpen
		}
	}
	return CircuitSnapshot{State: string(state), Failures: b.failures, RetryAfter: retry}
}

func circuitSnapshots(state runtimeSnapshot, now time.Time) []CircuitSnapshot {
	out := make([]CircuitSnapshot, 0, len(state.providers))
	for name, p := range state.providers {
		snapshot := p.breaker.snapshot(now)
		snapshot.Provider = name
		out = append(out, snapshot)
	}
	return out
}

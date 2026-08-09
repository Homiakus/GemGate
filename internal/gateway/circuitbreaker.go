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

type circuitPermit struct {
	probe bool
}

type circuitBreaker struct {
	mu               sync.Mutex
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

func newCircuitBreaker(threshold int, openFor time.Duration) *circuitBreaker {
	if threshold <= 0 {
		threshold = defaultCircuitFailureThreshold
	}
	if openFor <= 0 {
		openFor = defaultCircuitOpenFor
	}
	return &circuitBreaker{
		state:            circuitClosed,
		failureThreshold: threshold,
		openFor:          openFor,
	}
}

func (b *circuitBreaker) allow(now time.Time) (circuitPermit, bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case circuitOpen:
		if now.Before(b.openUntil) {
			return circuitPermit{}, false, b.openUntil.Sub(now)
		}
		b.state = circuitHalfOpen
		return circuitPermit{probe: true}, true, 0
	case circuitHalfOpen:
		// Only one request is allowed to test recovery.
		return circuitPermit{}, false, time.Second
	default:
		return circuitPermit{}, true, 0
	}
}

func (b *circuitBreaker) finish(permit circuitPermit, failed bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

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

	// A request admitted while the breaker was closed may finish after another
	// concurrent request has already opened it. Such late completions must not
	// accidentally close or extend the newly opened circuit.
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
	state := b.state
	retry := time.Duration(0)
	if state == circuitOpen {
		if now.Before(b.openUntil) {
			retry = b.openUntil.Sub(now)
		} else {
			// The next admitted request will become the half-open probe.
			state = circuitHalfOpen
		}
	}
	return CircuitSnapshot{State: string(state), Failures: b.failures, RetryAfter: retry}
}

var circuitRegistry sync.Map // *providerMetrics -> *circuitBreaker

func (m *Metrics) circuit(provider string) *circuitBreaker {
	metrics := m.provider(provider)
	if breaker, ok := circuitRegistry.Load(metrics); ok {
		return breaker.(*circuitBreaker)
	}
	breaker := newCircuitBreaker(defaultCircuitFailureThreshold, defaultCircuitOpenFor)
	actual, _ := circuitRegistry.LoadOrStore(metrics, breaker)
	return actual.(*circuitBreaker)
}

func (m *Metrics) CircuitSnapshots() []CircuitSnapshot {
	providers := m.providerSnapshots()
	out := make([]CircuitSnapshot, 0, len(providers))
	now := time.Now()
	for _, provider := range providers {
		snapshot := m.circuit(provider.Name).snapshot(now)
		snapshot.Provider = provider.Name
		out = append(out, snapshot)
	}
	return out
}

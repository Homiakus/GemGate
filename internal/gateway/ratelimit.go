package gateway

import (
	"sync"
	"time"
)

// rateWindow implements an exact one-minute sliding window.
//
// Compared with a fixed window it prevents a client from sending one full
// minute's quota immediately before a boundary and another full quota
// immediately after it. State remains intentionally in-memory and per process.
type rateWindow struct {
	mu       sync.Mutex
	start    time.Time // oldest request retained; kept for Retry-After calculation
	count    int
	requests []time.Time
}

func (w *rateWindow) allow(limit int, now time.Time) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.prune(now)
	if len(w.requests) >= limit {
		reset := time.Minute - now.Sub(w.requests[0])
		if reset < 0 {
			reset = 0
		}
		w.syncCompatFields()
		return false, reset
	}

	w.requests = append(w.requests, now)
	w.syncCompatFields()
	return true, 0
}

func (w *rateWindow) prune(now time.Time) {
	if len(w.requests) == 0 {
		return
	}
	cutoff := now.Add(-time.Minute)
	first := 0
	for first < len(w.requests) && !w.requests[first].After(cutoff) {
		first++
	}
	if first == 0 {
		return
	}
	copy(w.requests, w.requests[first:])
	w.requests = w.requests[:len(w.requests)-first]
}

func (w *rateWindow) syncCompatFields() {
	w.count = len(w.requests)
	if len(w.requests) == 0 {
		w.start = time.Time{}
		return
	}
	w.start = w.requests[0]
}

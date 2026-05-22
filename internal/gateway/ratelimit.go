package gateway

import (
	"sync"
	"time"
)

type rateWindow struct {
	mu    sync.Mutex
	start time.Time
	count int
}

func (w *rateWindow) allow(limit int, now time.Time) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.start.IsZero() || now.Sub(w.start) >= time.Minute {
		w.start = now
		w.count = 0
	}

	if w.count >= limit {
		return false, time.Minute - now.Sub(w.start)
	}

	w.count++
	return true, 0
}

package gateway

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Time      time.Time
	Level     string
	Client    string
	Method    string
	Path      string
	Status    int
	Bytes     int64
	Duration  time.Duration
	RequestID string
	Message   string
}

type LogRing struct {
	mu      sync.RWMutex
	entries []LogEntry
	limit   int
}

func NewLogRing(limit int) *LogRing {
	if limit <= 0 {
		limit = 300
	}
	return &LogRing{limit: limit, entries: make([]LogEntry, 0, limit)}
}

func (r *LogRing) Add(e LogEntry) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if e.Level == "" {
		e.Level = "info"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) >= r.limit {
		copy(r.entries, r.entries[1:])
		r.entries[len(r.entries)-1] = e
		return
	}
	r.entries = append(r.entries, e)
}

func (r *LogRing) Snapshot() []LogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]LogEntry, len(r.entries))
	copy(out, r.entries)
	return out
}

func (e LogEntry) Line() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = fmt.Sprintf("%s %s", e.Method, e.Path)
	}
	return fmt.Sprintf("%s %-5s %-10s %3d %7s %s %s",
		e.Time.Format("15:04:05"),
		strings.ToUpper(e.Level),
		e.Client,
		e.Status,
		e.Duration.Round(time.Millisecond),
		e.RequestID,
		msg,
	)
}

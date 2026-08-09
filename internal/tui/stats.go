package tui

import (
	"sort"
	"time"

	"gemgate/internal/gateway"
)

type statsSnapshot struct {
	Requests    int
	LastMinute  int
	TwoXX       int
	FourXX      int
	FiveXX      int
	SuccessRate float64
	AvgLatency  time.Duration
	P95Latency  time.Duration
	Trend       string
	Last        gateway.LogEntry
	Clients     map[string]clientStats
	Routes      map[string]routeStats
}

type clientStats struct {
	Requests   int
	Errors     int
	BytesOut   int64
	AvgLatency time.Duration
	durations  []time.Duration
}

type routeStats struct {
	Requests   int
	Errors     int
	AvgLatency time.Duration
	durations  []time.Duration
}

func summarize(logs []gateway.LogEntry) statsSnapshot {
	out := statsSnapshot{Clients: map[string]clientStats{}, Routes: map[string]routeStats{}, Trend: mutedStyle.Render("no recent traffic")}
	var durations []time.Duration
	var totalLatency time.Duration
	now := time.Now()
	buckets := make([]int, 20)

	for _, e := range logs {
		if e.Status == 0 && e.Message == "" {
			continue
		}
		out.Last = e
		if e.Status == 0 {
			continue
		}
		out.Requests++
		if now.Sub(e.Time) <= time.Minute {
			out.LastMinute++
		}
		switch {
		case e.Status >= 200 && e.Status < 300:
			out.TwoXX++
		case e.Status >= 400 && e.Status < 500:
			out.FourXX++
		case e.Status >= 500:
			out.FiveXX++
		}
		if e.Duration > 0 {
			durations = append(durations, e.Duration)
			totalLatency += e.Duration
		}
		minutesAgo := int(now.Sub(e.Time).Minutes())
		if minutesAgo >= 0 && minutesAgo < len(buckets) {
			buckets[len(buckets)-1-minutesAgo]++
		}
		name := safeText(e.Client, "unknown")
		cs := out.Clients[name]
		cs.Requests++
		cs.BytesOut += e.Bytes
		if e.Status >= 400 {
			cs.Errors++
		}
		if e.Duration > 0 {
			cs.durations = append(cs.durations, e.Duration)
		}
		out.Clients[name] = cs
		route := routeKey(e.Path)
		rs := out.Routes[route]
		rs.Requests++
		if e.Status >= 400 {
			rs.Errors++
		}
		if e.Duration > 0 {
			rs.durations = append(rs.durations, e.Duration)
		}
		out.Routes[route] = rs
	}
	if out.Requests > 0 {
		out.SuccessRate = float64(out.TwoXX) / float64(out.Requests) * 100
	}
	if len(durations) > 0 {
		out.AvgLatency = totalLatency / time.Duration(len(durations))
		out.P95Latency = percentile(durations, 95)
	}
	for name, cs := range out.Clients {
		if len(cs.durations) > 0 {
			cs.AvgLatency = averageDuration(cs.durations)
			out.Clients[name] = cs
		}
	}
	for name, rs := range out.Routes {
		if len(rs.durations) > 0 {
			rs.AvgLatency = averageDuration(rs.durations)
			out.Routes[name] = rs
		}
	}
	out.Trend = sparkline(buckets)
	return out
}

func percentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := (len(cp)*p + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	return cp[idx-1]
}

func averageDuration(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	var total time.Duration
	for _, v := range values {
		total += v
	}
	return total / time.Duration(len(values))
}

package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

func (m Model) dashboardView() string {
	stats := summarize(m.logs)
	w := m.contentWidth()
	cols := responsiveColumns(w)
	cardW := cardWidth(w, cols)
	errorValue := fmt.Sprintf("%d", stats.FourXX+stats.FiveXX)
	if stats.FiveXX > 0 {
		errorValue = badStyle.Render(errorValue)
	}

	providerNote := fmt.Sprintf("%d configured", len(m.cfg.Providers))
	if attention := providerAttentionCount(m.metrics.Providers, m.metrics.Circuits); attention > 0 {
		providerNote = warnStyle.Render(fmt.Sprintf("%d need attention", attention))
	}

	cards := []string{
		metricCard("Requests", fmt.Sprintf("%d", m.metrics.Requests), fmt.Sprintf("%d/min recent", stats.LastMinute), cardW),
		metricCard("Success", fmt.Sprintf("%.1f%%", stats.SuccessRate), fmt.Sprintf("%d ok responses", stats.TwoXX), cardW),
		metricCard("Errors", errorValue, fmt.Sprintf("%d 4xx / %d 5xx", stats.FourXX, stats.FiveXX), cardW),
		metricCard("Latency", formatDuration(stats.P95Latency), "p95 recent", cardW),
		metricCard("In-flight", fmt.Sprintf("%d", m.metrics.InFlight), "active gateway calls", cardW),
		metricCard("Providers", fmt.Sprintf("%d", len(m.metrics.Providers)), providerNote, cardW),
		metricCard("Rate limited", fmt.Sprintf("%d", m.metrics.RateLimited), "sliding-window 429s", cardW),
		metricCard("Uptime", m.metrics.Uptime.Round(time.Second).String(), "since start", cardW),
	}

	trafficBlock := lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Traffic"),
		stats.Trend,
		mutedStyle.Render("last 20 minutes, each block is one minute"),
	)

	latest := textStyle.Render("No requests yet.")
	if stats.Last.Status != 0 || stats.Last.Message != "" {
		latest = strings.Join([]string{
			textStyle.Render(fmt.Sprintf("%s %s %s", stats.Last.Time.Format("15:04:05"), coloredStatus(stats.Last.Status), logPath(stats.Last))),
			mutedStyle.Render(fmt.Sprintf("client=%s  provider=%s  latency=%s  request_id=%s",
				safeText(stats.Last.Client, "-"), safeText(stats.Last.Provider, "-"),
				formatDuration(stats.Last.Duration), safeText(stats.Last.RequestID, "-"))),
		}, "\n")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left, subtitleStyle.Render("Traffic overview"), "", cardGrid(cards, cols))),
		boxStyle.Width(w).Render(trafficBlock),
		boxStyle.Width(w).Render(subtitleStyle.Render("Provider health")+"\n"+providerHealthSummary(m.metrics.Providers)),
		boxStyle.Width(w).Render(subtitleStyle.Render("Latest event")+"\n"+latest),
	)
}

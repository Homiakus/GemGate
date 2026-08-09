package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

func (m Model) clientsView() string {
	stats := summarize(m.logs)
	w := m.contentWidth()
	header := labelStyle.Render(fmt.Sprintf("%-19s %-11s %-12s %-10s %-10s %-14s", "Client", "State", "RPM limit", "Requests", "Error", "Avg"))
	lines := []string{header}
	seen := map[string]bool{}
	for _, c := range m.cfg.Clients {
		cs := stats.Clients[c.Name]
		seen[c.Name] = true
		lines = append(lines, clientRow(c.Name, enabledText(c.Enabled), limitText(c.RateLimitRPM), cs, w))
	}
	extra := make([]string, 0)
	for name := range stats.Clients {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		lines = append(lines, clientRow(name, "seen", "n/a", stats.Clients[name], w))
	}
	if len(lines) == 1 {
		lines = append(lines, mutedStyle.Render("No configured clients."))
	}
	hint := hintBoxStyle.Width(min(w-8, 84)).Render(strings.Join([]string{
		"Recommended: issue one token per app/user.",
		"rate_limit_rpm uses an exact rolling one-minute window; limits remain process-local.",
	}, "\n"))
	return boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Clients and usage"), "", strings.Join(lines, "\n"), "", hint,
	))
}

func clientRow(name, state, limit string, stats clientStats, width int) string {
	errorRate := "0.0%"
	if stats.Requests > 0 {
		errorRate = fmt.Sprintf("%.1f%%", float64(stats.Errors)/float64(stats.Requests)*100)
	}
	stateText := textStyle.Render(state)
	if state == "enabled" {
		stateText = okStyle.Render(state)
	}
	errText := textStyle.Render(errorRate)
	if stats.Errors > 0 {
		errText = warnStyle.Render(errorRate)
	}
	return fmt.Sprintf("%-19s %s%-*s %-12s %-10s %s%-*s %-14s",
		textStyle.Render(truncate(name, 18)), stateText, max(0, 11-len(state)), "",
		textStyle.Render(limit), textStyle.Render(fmt.Sprintf("%d", stats.Requests)),
		errText, max(0, 10-len(errorRate)), "", textStyle.Render(formatDuration(stats.AvgLatency)))
}

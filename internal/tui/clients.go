package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
)

func (m *Model) updateClientRows() {
	stats := summarize(m.logs)
	rows := make([]table.Row, 0, len(m.cfg.Clients))
	width := m.contentWidth()

	for _, c := range m.cfg.Clients {
		cs := stats.Clients[c.Name]
		rows = append(rows, clientTableRow(c.Name, c.Enabled, c.RateLimitRPM, cs, width))
	}
	m.clientTable.SetRows(rows)
}

func clientTableRow(name string, enabled bool, limit int, stats clientStats, width int) table.Row {
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	errRate := percent(stats.Errors, stats.Requests)

	switch {
	case width >= 102:
		return table.Row{
			name,
			state,
			limitText(limit),
			fmt.Sprintf("%d", stats.Requests),
			errRate,
			formatDuration(stats.AvgLatency),
			humanBytes(uint64(max64(stats.BytesOut, 0))),
		}
	case width >= 76:
		return table.Row{
			name,
			state,
			limitText(limit),
			fmt.Sprintf("%d", stats.Requests),
			errRate,
		}
	default:
		return table.Row{name, state, fmt.Sprintf("%d", stats.Requests)}
	}
}

func (m Model) clientsView() string {
	w := m.contentWidth()
	parts := []string{
		sectionRule(fmt.Sprintf("Clients  %d enabled / %d configured", enabledClientCount(m.cfg), len(m.cfg.Clients)), w),
		mutedStyle.Render("One token per consumer keeps usage, revocation and rate limits attributable."),
		"",
		m.clientTable.View(),
		"",
		sectionRule("Selected client", w),
		m.clientDetailView(),
	}
	return strings.Join(parts, "\n")
}

func (m Model) clientDetailView() string {
	if len(m.cfg.Clients) == 0 {
		return mutedStyle.Render("No configured clients.")
	}
	idx := m.clientTable.Cursor()
	if idx < 0 || idx >= len(m.cfg.Clients) {
		idx = 0
	}
	c := m.cfg.Clients[idx]
	stats := summarize(m.logs).Clients[c.Name]

	state := "disabled"
	stateText := mutedStyle.Render(state)
	if c.Enabled {
		state = "enabled"
		stateText = okStyle.Render("OK enabled")
	}

	line1 := fmt.Sprintf("%s  %s  limit=%s",
		valueStyle.Render(c.Name),
		stateText,
		limitText(c.RateLimitRPM),
	)
	line2 := fmt.Sprintf("requests=%d  errors=%s  avg=%s  bytes=%s",
		stats.Requests,
		clientErrorText(stats),
		formatDuration(stats.AvgLatency),
		humanBytes(uint64(max64(stats.BytesOut, 0))),
	)
	line3 := mutedStyle.Render("Token material is intentionally omitted from the operator UI.")
	return detailBgStyle.Render(strings.Join([]string{line1, line2, line3}, "\n"))
}

func clientErrorText(stats clientStats) string {
	rate := percent(stats.Errors, stats.Requests)
	if stats.Errors > 0 {
		return warnStyle.Render(rate)
	}
	return textStyle.Render(rate)
}

func clientColumns(width int) []table.Column {
	switch {
	case width >= 102:
		return []table.Column{
			{Title: "CLIENT", Width: 22},
			{Title: "STATE", Width: 10},
			{Title: "LIMIT", Width: 14},
			{Title: "REQUESTS", Width: 10},
			{Title: "ERROR", Width: 9},
			{Title: "AVG", Width: 12},
			{Title: "BYTES", Width: max(10, width-87)},
		}
	case width >= 76:
		return []table.Column{
			{Title: "CLIENT", Width: max(18, width-49)},
			{Title: "STATE", Width: 10},
			{Title: "LIMIT", Width: 14},
			{Title: "REQUESTS", Width: 10},
			{Title: "ERROR", Width: 9},
		}
	default:
		return []table.Column{
			{Title: "CLIENT", Width: max(18, width-26)},
			{Title: "STATE", Width: 10},
			{Title: "REQUESTS", Width: 10},
		}
	}
}

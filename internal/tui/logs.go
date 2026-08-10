package tui

import (
	"fmt"
	"strings"

	"gemgate/internal/gateway"

	"charm.land/bubbles/v2/table"
)

func (m *Model) updateLogRows() {
	m.visibleLogs = make([]gateway.LogEntry, 0, len(m.logs))
	rows := make([]table.Row, 0, len(m.logs))
	width := m.contentWidth()

	for i := len(m.logs) - 1; i >= 0; i-- {
		e := m.logs[i]
		if !m.filterLog(e) {
			continue
		}
		m.visibleLogs = append(m.visibleLogs, e)
		rows = append(rows, logRow(e, width))
	}
	m.logTable.SetRows(rows)
}

func logRow(e gateway.LogEntry, width int) table.Row {
	switch {
	case width >= 118:
		return table.Row{
			e.Time.Format("15:04:05"),
			strings.ToUpper(e.Level),
			safeText(e.Client, "-"),
			safeText(e.Provider, "-"),
			safeText(e.Method, "-"),
			statusText(e.Status),
			formatDuration(e.Duration),
			humanBytes(uint64(max64(e.Bytes, 0))),
			logPath(e),
		}
	case width >= 88:
		return table.Row{
			e.Time.Format("15:04:05"),
			safeText(e.Client, "-"),
			safeText(e.Provider, "-"),
			statusText(e.Status),
			formatDuration(e.Duration),
			logPath(e),
		}
	case width >= 68:
		return table.Row{
			e.Time.Format("15:04:05"),
			safeText(e.Provider, "-"),
			statusText(e.Status),
			formatDuration(e.Duration),
			logPath(e),
		}
	default:
		return table.Row{
			e.Time.Format("15:04"),
			statusText(e.Status),
			logPath(e),
		}
	}
}

func (m Model) logsView() string {
	w := m.contentWidth()
	filter := strings.Join([]string{
		filterPill("a", "all", m.filter == filterAll),
		filterPill("w", "warn", m.filter == filterWarnings),
		filterPill("e", "errors", m.filter == filterErrors),
		filterPill("u", "auth", m.filter == filterAuth),
	}, "   ")

	title := fmt.Sprintf("Requests  %d shown / %d retained", len(m.visibleLogs), len(m.logs))
	parts := []string{
		sectionRule(title, w),
		filter,
		"",
		m.logTable.View(),
		"",
		sectionRule("Selected request", w),
		m.logDetailView(),
	}
	return strings.Join(parts, "\n")
}

func (m Model) logDetailView() string {
	if len(m.visibleLogs) == 0 {
		return mutedStyle.Render("No matching request entries.")
	}
	idx := m.logTable.Cursor()
	if idx < 0 || idx >= len(m.visibleLogs) {
		idx = 0
	}
	e := m.visibleLogs[idx]

	if m.contentWidth() < 82 {
		lines := []string{
			fmt.Sprintf("%s %s %s", safeText(e.Method, "-"), coloredStatus(e.Status), logPath(e)),
			mutedStyle.Render(fmt.Sprintf("provider=%s  client=%s", safeText(e.Provider, "-"), safeText(e.Client, "-"))),
			mutedStyle.Render(fmt.Sprintf("duration=%s  bytes=%s", formatDuration(e.Duration), humanBytes(uint64(max64(e.Bytes, 0))))),
		}
		if e.Message != "" && e.Message != e.Path {
			lines = append(lines, warnStyle.Render(truncate(e.Message, max(24, m.contentWidth()-2))))
		}
		return strings.Join(lines, "\n")
	}

	fields := []string{
		fmt.Sprintf("request_id=%s", safeText(e.RequestID, "-")),
		fmt.Sprintf("client=%s", safeText(e.Client, "-")),
		fmt.Sprintf("provider=%s", safeText(e.Provider, "-")),
		fmt.Sprintf("status=%s", statusText(e.Status)),
		fmt.Sprintf("duration=%s", formatDuration(e.Duration)),
		fmt.Sprintf("bytes=%s", humanBytes(uint64(max64(e.Bytes, 0)))),
	}
	line := strings.Join(fields, "  ")
	if e.Message != "" && e.Message != e.Path {
		line += "\n" + warnStyle.Render(e.Message)
	}
	return detailBgStyle.Render(line)
}

func defaultLogColumns(width int) []table.Column {
	switch {
	case width >= 118:
		pathWidth := max(22, width-96)
		return []table.Column{
			{Title: "TIME", Width: 8},
			{Title: "LVL", Width: 5},
			{Title: "CLIENT", Width: 12},
			{Title: "PROVIDER", Width: 13},
			{Title: "METHOD", Width: 7},
			{Title: "STATUS", Width: 7},
			{Title: "DURATION", Width: 9},
			{Title: "BYTES", Width: 9},
			{Title: "PATH", Width: pathWidth},
		}
	case width >= 88:
		return []table.Column{
			{Title: "TIME", Width: 8},
			{Title: "CLIENT", Width: 13},
			{Title: "PROVIDER", Width: 13},
			{Title: "STATUS", Width: 7},
			{Title: "DURATION", Width: 9},
			{Title: "PATH", Width: max(22, width-62)},
		}
	case width >= 68:
		return []table.Column{
			{Title: "TIME", Width: 8},
			{Title: "PROVIDER", Width: 13},
			{Title: "STATUS", Width: 7},
			{Title: "DURATION", Width: 9},
			{Title: "PATH", Width: max(20, width-45)},
		}
	default:
		return []table.Column{
			{Title: "TIME", Width: 5},
			{Title: "STATUS", Width: 7},
			{Title: "PATH", Width: max(18, width-18)},
		}
	}
}

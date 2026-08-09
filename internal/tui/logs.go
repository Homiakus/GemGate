package tui

import (
	"fmt"
	"strings"

	"gemgate/internal/gateway"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

func (m *Model) updateLogRows() {
	m.visibleLogs = make([]gateway.LogEntry, 0, len(m.logs))
	rows := make([]table.Row, 0, len(m.logs))
	for i := len(m.logs) - 1; i >= 0; i-- {
		e := m.logs[i]
		if !m.filterLog(e) {
			continue
		}
		m.visibleLogs = append(m.visibleLogs, e)
		rows = append(rows, table.Row{
			e.Time.Format("15:04:05"),
			strings.ToUpper(e.Level),
			safeText(e.Client, "-"),
			safeText(e.Provider, "-"),
			safeText(e.Method, "-"),
			statusText(e.Status),
			formatDuration(e.Duration),
			humanBytes(uint64(max64(e.Bytes, 0))),
			logPath(e),
		})
	}
	m.logTable.SetRows(rows)
}

func (m Model) logsView() string {
	w := m.contentWidth()
	filter := lipgloss.JoinHorizontal(lipgloss.Left,
		filterPill("a", "all", m.filter == filterAll), " ",
		filterPill("w", "warn", m.filter == filterWarnings), " ",
		filterPill("e", "errors", m.filter == filterErrors), " ",
		filterPill("u", "auth", m.filter == filterAuth),
	)
	title := fmt.Sprintf("Request logs: %d shown / %d retained", len(m.visibleLogs), len(m.logs))
	return boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render(title), filter, "", m.logTable.View(), "", m.logDetailView(),
	))
}

func (m Model) logDetailView() string {
	if len(m.visibleLogs) == 0 {
		return mutedStyle.Render("No matching log entries.")
	}
	idx := m.logTable.Cursor()
	if idx < 0 || idx >= len(m.visibleLogs) {
		idx = 0
	}
	e := m.visibleLogs[idx]
	fields := []string{
		fmt.Sprintf("request_id=%s", safeText(e.RequestID, "-")),
		fmt.Sprintf("client=%s", safeText(e.Client, "-")),
		fmt.Sprintf("provider=%s", safeText(e.Provider, "-")),
		fmt.Sprintf("status=%s", statusText(e.Status)),
		fmt.Sprintf("duration=%s", formatDuration(e.Duration)),
		fmt.Sprintf("bytes=%s", humanBytes(uint64(max64(e.Bytes, 0)))),
	}
	if e.Message != "" {
		fields = append(fields, "message="+e.Message)
	}
	return detailBgStyle.Render(strings.Join(fields, "  "))
}

func defaultLogColumns(width int) []table.Column {
	pathWidth := width - 91
	if pathWidth < 20 {
		pathWidth = 20
	}
	return []table.Column{
		{Title: "Time", Width: 8},
		{Title: "Lv", Width: 5},
		{Title: "Client", Width: 12},
		{Title: "Provider", Width: 12},
		{Title: "Method", Width: 7},
		{Title: "Status", Width: 7},
		{Title: "Dur", Width: 9},
		{Title: "Bytes", Width: 9},
		{Title: "Path", Width: pathWidth},
	}
}

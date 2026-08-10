package tui

import (
	"fmt"
	"strings"
	"time"

	"gemgate/internal/gateway"

	"charm.land/lipgloss/v2"
)

func filterPill(k, label string, selected bool) string {
	text := k + " " + label
	if selected {
		return selectedPillStyle.Render("[" + text + "]")
	}
	return mutedStyle.Render(text)
}

func enabledClientCount(cfg gateway.ConfigSnapshot) int {
	var n int
	for _, c := range cfg.Clients {
		if c.Enabled {
			n++
		}
	}
	return n
}

func enabledText(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

func limitText(v int) string {
	if v <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d rpm", v)
}

func statusText(status int) string {
	if status == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", status)
}

func coloredStatus(status int) string {
	s := statusText(status)
	switch {
	case status >= 500:
		return badStyle.Render(s)
	case status >= 400:
		return warnStyle.Render(s)
	case status >= 200 && status < 300:
		return okStyle.Render(s)
	default:
		return textStyle.Render(s)
	}
}

func logPath(e gateway.LogEntry) string {
	if e.Path != "" {
		return e.Path
	}
	if e.Message != "" {
		return e.Message
	}
	return "-"
}

func safeText(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

func humanBytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := uint64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(v)/float64(div), "KMGTPE"[exp])
}

func sparkline(values []int) string {
	maxValue := 0
	for _, v := range values {
		if v > maxValue {
			maxValue = v
		}
	}
	if maxValue == 0 {
		return mutedStyle.Render("no recent traffic")
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range values {
		if v == 0 {
			b.WriteRune('·')
			continue
		}
		idx := (v*len(blocks) - 1) / maxValue
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		b.WriteRune(blocks[idx])
	}
	return valueStyle.Render(b.String())
}

func routeKey(path string) string {
	path = strings.Split(path, "?")[0]
	switch {
	case path == "/_metrics":
		return "metrics"
	case path == "/_config":
		return "config"
	case path == "/_healthz":
		return "health"
	case strings.HasPrefix(path, "/providers/"):
		return "provider"
	case strings.HasPrefix(path, "/v1beta/openai/") || strings.HasPrefix(path, "/v1/openai/"):
		return "openai"
	case strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/v1/"):
		return "native"
	default:
		return "other"
	}
}

func localBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "http://localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	if strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") {
		return "http://" + addr
	}
	return "http://" + addr
}

func menuIndex(v string) int {
	switch v {
	case "1":
		return 0
	case "2":
		return 1
	case "3":
		return 2
	case "4":
		return 3
	case "5":
		return 4
	default:
		return -1
	}
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	target := width - 1
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > target {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

func padBetween(left, right string, width int) string {
	if width <= 0 {
		return left + " " + right
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncatePlainStyled(left, max(1, width-lipgloss.Width(right)-1)) + " " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncatePlainStyled(s string, width int) string {
	// Header inputs contain small ANSI-styled fragments. When space is tight,
	// returning the left context unchanged is preferable to cutting escape
	// sequences. Compact callers already keep the left side short.
	if lipgloss.Width(s) <= width {
		return s
	}
	return s
}

func sectionRule(title string, width int) string {
	label := " " + title + " "
	remaining := width - lipgloss.Width(label)
	if remaining < 2 {
		return subtitleStyle.Render(title)
	}
	return subtitleStyle.Render(label) + sectionRuleStyle.Render(strings.Repeat("─", remaining))
}

func kvRow(label, value string, labelWidth int) string {
	if labelWidth < 8 {
		labelWidth = 8
	}
	return labelStyle.Render(fmt.Sprintf("%-*s", labelWidth, label)) + " " + textStyle.Render(value)
}

func percent(numerator, denominator int) string {
	if denominator <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(numerator)/float64(denominator)*100)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

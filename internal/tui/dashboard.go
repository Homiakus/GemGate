package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (m Model) dashboardView() string {
	stats := summarize(m.logs)
	w := m.contentWidth()

	summary := m.overviewSummary(stats, w)
	traffic := lipJoin(
		sectionRule("Traffic", w),
		fmt.Sprintf("%s  %s", valueStyle.Render(fmt.Sprintf("%d/min", stats.LastMinute)), stats.Trend),
		mutedStyle.Render("rolling last 20 minutes; each cell is one minute"),
	)

	providers := lipJoin(
		sectionRule("Provider state", w),
		m.providerAttentionView(),
	)

	latest := lipJoin(
		sectionRule("Latest request", w),
		m.latestRequestView(stats),
	)

	if m.layout.IsWide() && w >= 96 {
		leftW := (w * 58) / 100
		rightW := max(32, w-leftW-3)
		left := lipglossWidth(leftW, lipJoin(summary, "", traffic))
		right := lipglossWidth(rightW, lipJoin(providers, "", latest))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right)
	}

	return lipJoin(summary, "", traffic, "", providers, "", latest)
}

func (m Model) overviewSummary(stats statsSnapshot, width int) string {
	errors := stats.FourXX + stats.FiveXX
	success := fmt.Sprintf("%.1f%%", stats.SuccessRate)
	errorText := fmt.Sprintf("%d", errors)
	if errors > 0 {
		errorText = warnStyle.Render(errorText)
	}
	if stats.FiveXX > 0 || m.metrics.UpstreamErrors > 0 {
		errorText = badStyle.Render(fmt.Sprintf("%d", errors))
	}

	items := []string{
		metricInline("requests", valueStyle.Render(fmt.Sprintf("%d", m.metrics.Requests))),
		metricInline("success", valueStyle.Render(success)),
		metricInline("errors", errorText),
		metricInline("p95", valueStyle.Render(formatDuration(stats.P95Latency))),
		metricInline("in-flight", valueStyle.Render(fmt.Sprintf("%d", m.metrics.InFlight))),
		metricInline("limited", valueStyle.Render(fmt.Sprintf("%d", m.metrics.RateLimited))),
	}

	if width < 72 {
		return lipJoin(
			sectionRule("Now", width),
			strings.Join(items[:3], "   "),
			strings.Join(items[3:], "   "),
		)
	}
	return lipJoin(sectionRule("Now", width), strings.Join(items, "   "))
}

func metricInline(label, value string) string {
	return mutedStyle.Render(label+" ") + value
}

func (m Model) providerAttentionView() string {
	if len(m.cfg.Providers) == 0 {
		return mutedStyle.Render("No providers configured.")
	}

	byName := providerMetricsByName(m.metrics.Providers)
	circuits := providerCircuitsByName(m.metrics.Circuits)
	lines := make([]string, 0, min(6, len(m.cfg.Providers)))

	for _, p := range m.cfg.Providers {
		pm := byName[p.Name]
		circuit := circuits[p.Name]
		if pm.Health != "warning" && pm.Health != "degraded" && circuit.State != "open" && circuit.State != "half_open" {
			continue
		}
		lines = append(lines, fmt.Sprintf("! %-18s %-10s circuit=%s",
			truncate(p.Name, 18),
			providerHealthText(pm.Health),
			safeText(circuit.State, "closed"),
		))
		if len(lines) == 5 {
			break
		}
	}

	if len(lines) == 0 {
		return okStyle.Render("OK  all configured providers are passive-healthy")
	}
	return strings.Join(lines, "\n")
}

func (m Model) latestRequestView(stats statsSnapshot) string {
	e := stats.Last
	if e.Status == 0 && e.Message == "" {
		return mutedStyle.Render("No requests yet.")
	}

	line1 := fmt.Sprintf("%s  %s  %s",
		e.Time.Format("15:04:05"),
		coloredStatus(e.Status),
		truncate(logPath(e), max(20, m.contentWidth()-24)),
	)
	line2 := fmt.Sprintf("client=%s  provider=%s  duration=%s",
		safeText(e.Client, "-"),
		safeText(e.Provider, "-"),
		formatDuration(e.Duration),
	)
	if e.RequestID != "" && m.contentWidth() >= 90 {
		line2 += "  request_id=" + e.RequestID
	}
	return textStyle.Render(line1) + "\n" + mutedStyle.Render(line2)
}

func lipJoin(parts ...string) string {
	return strings.Join(parts, "\n")
}

func lipglossWidth(width int, content string) string {
	return lipgloss.NewStyle().Width(max(1, width)).Render(content)
}

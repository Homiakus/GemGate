package tui

import (
	"fmt"
	"strings"

	"gemgate/internal/gateway"

	"charm.land/bubbles/v2/table"
)

func (m *Model) updateProviderRows() {
	rows := make([]table.Row, 0, len(m.cfg.Providers))
	byName := providerMetricsByName(m.metrics.Providers)
	circuits := providerCircuitsByName(m.metrics.Circuits)
	width := m.contentWidth()

	for _, p := range m.cfg.Providers {
		pm := byName[p.Name]
		circuit := circuits[p.Name]
		role := "named"
		if p.Name == m.cfg.DefaultProvider {
			role = "default"
		}
		rows = append(rows, providerRow(p.Name, p.Type, role, pm, circuit, width))
	}
	m.providerTable.SetRows(rows)
}

func providerRow(name, providerType, role string, pm gateway.ProviderMetricsSnapshot, circuit gateway.CircuitSnapshot, width int) table.Row {
	health := providerHealthCell(pm.Health)
	circuitState := safeText(circuit.State, "closed")
	errRate := providerErrorRate(pm)

	switch {
	case width >= 112:
		return table.Row{
			name, role, health, circuitState,
			fmt.Sprintf("%d", pm.Requests),
			errRate,
			fmt.Sprintf("%d", pm.InFlight),
			formatDuration(pm.AverageDuration),
			providerType,
		}
	case width >= 82:
		return table.Row{
			name, role, health, circuitState,
			fmt.Sprintf("%d", pm.Requests),
			errRate,
			formatDuration(pm.AverageDuration),
		}
	default:
		return table.Row{name, health, circuitState, fmt.Sprintf("%d", pm.Requests)}
	}
}

func (m Model) providersView() string {
	w := m.contentWidth()
	parts := []string{
		sectionRule(fmt.Sprintf("Providers  %d configured", len(m.cfg.Providers)), w),
		mutedStyle.Render("Passive health + full-stream circuit accounting. No automatic generation retries."),
		"",
		m.providerTable.View(),
		"",
		sectionRule("Selected provider", w),
		m.providerDetailView(),
	}
	return strings.Join(parts, "\n")
}

func (m Model) providerDetailView() string {
	if len(m.cfg.Providers) == 0 {
		return mutedStyle.Render("No configured providers.")
	}
	idx := m.providerTable.Cursor()
	if idx < 0 || idx >= len(m.cfg.Providers) {
		idx = 0
	}
	p := m.cfg.Providers[idx]
	pm := providerMetricsByName(m.metrics.Providers)[p.Name]
	circuit := providerCircuitsByName(m.metrics.Circuits)[p.Name]

	role := "named"
	if p.Name == m.cfg.DefaultProvider {
		role = "default"
	}
	circuitPolicy := "disabled"
	if p.CircuitEnabled {
		circuitPolicy = fmt.Sprintf("threshold=%d, open=%s", p.CircuitFailureThreshold, p.CircuitOpenFor)
	}

	line1 := fmt.Sprintf("%s  %s  role=%s",
		valueStyle.Render(p.Name),
		mutedStyle.Render(p.Type),
		role,
	)
	line2 := fmt.Sprintf("health=%s  circuit=%s  requests=%d  error=%s  in-flight=%d  avg=%s",
		providerHealthText(pm.Health),
		providerCircuitText(circuit.State),
		pm.Requests,
		providerErrorText(providerErrorRate(pm), pm),
		pm.InFlight,
		formatDuration(pm.AverageDuration),
	)
	line3 := mutedStyle.Render("base: " + truncate(p.BaseURL, max(24, m.contentWidth()-8)))
	line4 := mutedStyle.Render("circuit policy: " + circuitPolicy)

	if m.contentWidth() >= 82 {
		baseURL := localBaseURL(m.cfg.Listen)
		line4 += "\n" + mutedStyle.Render("route: "+baseURL+"/providers/"+p.Name+"/...")
	}
	return detailBgStyle.Render(strings.Join([]string{line1, line2, line3, line4}, "\n"))
}

func providerColumns(width int) []table.Column {
	switch {
	case width >= 112:
		return []table.Column{
			{Title: "PROVIDER", Width: 18},
			{Title: "ROLE", Width: 9},
			{Title: "HEALTH", Width: 9},
			{Title: "CIRCUIT", Width: 11},
			{Title: "REQ", Width: 7},
			{Title: "ERROR", Width: 8},
			{Title: "FLIGHT", Width: 8},
			{Title: "AVG", Width: 10},
			{Title: "TYPE", Width: max(14, width-98)},
		}
	case width >= 82:
		return []table.Column{
			{Title: "PROVIDER", Width: 18},
			{Title: "ROLE", Width: 9},
			{Title: "HEALTH", Width: 9},
			{Title: "CIRCUIT", Width: 11},
			{Title: "REQ", Width: 7},
			{Title: "ERROR", Width: 8},
			{Title: "AVG", Width: max(10, width-72)},
		}
	default:
		return []table.Column{
			{Title: "PROVIDER", Width: max(16, width-31)},
			{Title: "HEALTH", Width: 9},
			{Title: "CIRCUIT", Width: 11},
			{Title: "REQ", Width: 7},
		}
	}
}

func providerMetricsByName(items []gateway.ProviderMetricsSnapshot) map[string]gateway.ProviderMetricsSnapshot {
	out := make(map[string]gateway.ProviderMetricsSnapshot, len(items))
	for _, p := range items {
		out[p.Name] = p
	}
	return out
}

func providerCircuitsByName(items []gateway.CircuitSnapshot) map[string]gateway.CircuitSnapshot {
	out := make(map[string]gateway.CircuitSnapshot, len(items))
	for _, c := range items {
		out[c.Provider] = c
	}
	return out
}

func providerAttentionCount(metrics []gateway.ProviderMetricsSnapshot, circuits []gateway.CircuitSnapshot) int {
	attention := make(map[string]struct{})
	for _, p := range metrics {
		if p.Health == "warning" || p.Health == "degraded" {
			attention[p.Name] = struct{}{}
		}
	}
	for _, c := range circuits {
		if c.State == "open" || c.State == "half_open" {
			attention[c.Provider] = struct{}{}
		}
	}
	return len(attention)
}

func providerHealthSummary(items []gateway.ProviderMetricsSnapshot) string {
	if len(items) == 0 {
		return mutedStyle.Render("no providers configured")
	}
	parts := make([]string, 0, len(items))
	for _, p := range items {
		parts = append(parts, fmt.Sprintf("%s %s", textStyle.Render(p.Name), providerHealthText(p.Health)))
	}
	return strings.Join(parts, "   ")
}

func providerHealthCell(health string) string {
	switch health {
	case "healthy":
		return "OK"
	case "warning":
		return "WARN"
	case "degraded":
		return "FAIL"
	default:
		return "?"
	}
}

func providerHealthText(health string) string {
	switch health {
	case "healthy":
		return okStyle.Render("OK healthy")
	case "warning":
		return warnStyle.Render("! warning")
	case "degraded":
		return badStyle.Render("! degraded")
	default:
		return mutedStyle.Render("? unknown")
	}
}

func providerCircuitText(state string) string {
	switch state {
	case "closed":
		return okStyle.Render("closed")
	case "half_open":
		return warnStyle.Render("half-open")
	case "open":
		return badStyle.Render("open")
	case "disabled":
		return mutedStyle.Render("disabled")
	default:
		return mutedStyle.Render("closed")
	}
}

func providerErrorRate(p gateway.ProviderMetricsSnapshot) string {
	if p.Requests == 0 {
		return "0.0%"
	}
	errors := p.Requests4xx + p.Requests5xx + p.TransportErrors
	return fmt.Sprintf("%.1f%%", float64(errors)/float64(p.Requests)*100)
}

func providerErrorText(rate string, p gateway.ProviderMetricsSnapshot) string {
	if p.Requests5xx+p.TransportErrors > 0 {
		return badStyle.Render(rate)
	}
	if p.Requests4xx > 0 {
		return warnStyle.Render(rate)
	}
	return textStyle.Render(rate)
}

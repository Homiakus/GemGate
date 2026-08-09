package tui

import (
	"fmt"
	"strings"

	"gemgate/internal/gateway"

	"charm.land/lipgloss/v2"
)

func (m Model) providersView() string {
	w := m.contentWidth()
	byName := providerMetricsByName(m.metrics.Providers)
	circuits := providerCircuitsByName(m.metrics.Circuits)
	header := labelStyle.Render(fmt.Sprintf("%-17s %-15s %-9s %-10s %-10s %-7s %-8s %-8s %-10s",
		"Provider", "Type", "Role", "Health", "Circuit", "Req", "Error", "Flight", "Avg"))
	lines := []string{header}

	for _, p := range m.cfg.Providers {
		pm := byName[p.Name]
		circuit := circuits[p.Name]
		role := "named"
		if p.Name == m.cfg.DefaultProvider {
			role = "default"
		}
		errorRate := providerErrorRate(pm)
		lines = append(lines, fmt.Sprintf("%-17s %-15s %-9s %-10s %-10s %-7d %-8s %-8d %-10s",
			textStyle.Render(truncate(p.Name, 16)),
			mutedStyle.Render(truncate(p.Type, 14)),
			providerRoleText(role),
			providerHealthText(pm.Health),
			providerCircuitText(circuit.State),
			pm.Requests,
			providerErrorText(errorRate, pm),
			pm.InFlight,
			formatDuration(pm.AverageDuration),
		))
	}
	if len(m.cfg.Providers) == 0 {
		lines = append(lines, mutedStyle.Render("No configured providers."))
	}

	baseURL := localBaseURL(m.cfg.Listen)
	quick := hintBoxStyle.Width(min(w-8, 104)).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Routing & resilience"),
		codeStyle.Render("Default: "+baseURL+"/<provider-path>"),
		codeStyle.Render("Named:   "+baseURL+"/providers/{name}/<provider-path>"),
		mutedStyle.Render("Circuit policy is per provider and hot-reloadable; defaults are threshold=5, open=30s. No automatic retries."),
	))

	return boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Providers"),
		mutedStyle.Render("Health and circuit state are passive and measured around the full streamed response lifecycle."),
		"", strings.Join(lines, "\n"), "", quick,
	))
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

func providerRoleText(role string) string {
	if role == "default" {
		return valueStyle.Render(role)
	}
	return mutedStyle.Render(role)
}

func providerHealthText(health string) string {
	switch health {
	case "healthy":
		return okStyle.Render(health)
	case "warning":
		return warnStyle.Render(health)
	case "degraded":
		return badStyle.Render(health)
	default:
		return mutedStyle.Render("unknown")
	}
}

func providerCircuitText(state string) string {
	switch state {
	case "closed":
		return okStyle.Render(state)
	case "half_open":
		return warnStyle.Render("half-open")
	case "open":
		return badStyle.Render(state)
	case "disabled":
		return mutedStyle.Render(state)
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
	if p.Requests4xx+p.Requests5xx+p.TransportErrors > 0 {
		return warnStyle.Render(rate)
	}
	return textStyle.Render(rate)
}

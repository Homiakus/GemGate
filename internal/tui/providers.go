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
	header := labelStyle.Render(fmt.Sprintf("%-18s %-17s %-10s %-11s %-8s %-9s %-9s %-11s",
		"Provider", "Type", "Role", "Health", "Req", "Error", "In-flight", "Avg"))
	lines := []string{header}

	for _, p := range m.cfg.Providers {
		pm := byName[p.Name]
		role := "named"
		if p.Name == m.cfg.DefaultProvider {
			role = "default"
		}
		errorRate := providerErrorRate(pm)
		lines = append(lines, fmt.Sprintf("%-18s %-17s %-10s %-11s %-8d %-9s %-9d %-11s",
			textStyle.Render(truncate(p.Name, 17)),
			mutedStyle.Render(truncate(p.Type, 16)),
			providerRoleText(role),
			providerHealthText(pm.Health),
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
	quick := hintBoxStyle.Width(min(w-8, 100)).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Routing"),
		codeStyle.Render("Default: "+baseURL+"/<provider-path>"),
		codeStyle.Render("Named:   "+baseURL+"/providers/{name}/<provider-path>"),
		mutedStyle.Render("Health is passive: transport/5xx streaks only; it is not an active readiness probe."),
	))

	return boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Providers"),
		mutedStyle.Render("Provider-level status is measured around the full streamed response lifecycle."),
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

func providerAttentionCount(items []gateway.ProviderMetricsSnapshot) int {
	count := 0
	for _, p := range items {
		if p.Health == "warning" || p.Health == "degraded" {
			count++
		}
	}
	return count
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

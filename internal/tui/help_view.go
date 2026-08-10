package tui

import (
	"fmt"
	"strings"
)

func (m *Model) updateHelpViewport() {
	if m.layout.IsTiny() {
		return
	}
	m.helpViewport.SetContent(m.helpContent())
}

func (m Model) helpView() string {
	w := m.contentWidth()
	return strings.Join([]string{
		sectionRule("Keyboard help / "+sections[m.active].Name, w),
		mutedStyle.Render("Context-sensitive commands only. Press Esc or ? to return."),
		"",
		m.helpViewport.View(),
	}, "\n")
}

func (m Model) helpContent() string {
	lines := []string{
		subtitleStyle.Render("Navigation"),
		helpRow("1-5", "jump directly to a section"),
		helpRow("Tab / ]", "next section"),
		helpRow("Shift+Tab / [", "previous section"),
		helpRow("Esc", "return to Overview; close help when open"),
		"",
		subtitleStyle.Render("Global"),
		helpRow("r", "refresh the local gateway snapshot immediately"),
		helpRow("Space / p", "pause or resume the one-second live refresh"),
		helpRow("?", "open/close this help"),
		helpRow("q / Ctrl+C", "quit and restore the terminal"),
	}

	switch m.active {
	case tabRequests:
		lines = append(lines,
			"",
			subtitleStyle.Render("Requests"),
			helpRow("Up / Down, j / k", "move through retained request entries"),
			helpRow("PgUp / PgDn", "page through requests"),
			helpRow("a", "show all requests"),
			helpRow("w", "warnings and 4xx"),
			helpRow("e", "errors and 5xx"),
			helpRow("u", "authentication-related requests"),
		)
	case tabProviders:
		lines = append(lines,
			"",
			subtitleStyle.Render("Providers"),
			helpRow("Up / Down, j / k", "select a provider"),
			helpRow("PgUp / PgDn", "page through providers"),
		)
	case tabClients:
		lines = append(lines,
			"",
			subtitleStyle.Render("Clients"),
			helpRow("Up / Down, j / k", "select a client"),
			helpRow("PgUp / PgDn", "page through clients"),
		)
	case tabConfig:
		lines = append(lines,
			"",
			subtitleStyle.Render("Config"),
			helpRow("Up / Down, j / k", "scroll the redacted runtime view"),
			helpRow("PgUp / PgDn", "page through runtime configuration"),
		)
	default:
		lines = append(lines,
			"",
			subtitleStyle.Render("Overview"),
			textStyle.Render("No extra mode is required: the screen refreshes automatically and keeps only actionable state visible."),
		)
	}

	lines = append(lines,
		"",
		subtitleStyle.Render("Operator notes"),
		textStyle.Render("- TUI navigation is fully keyboard-first; mouse support is not required."),
		textStyle.Render("- Colors reinforce state but status text remains readable in monochrome terminals."),
		textStyle.Render("- Provider health/readiness is passive; the TUI never spends model quota for probes."),
		textStyle.Render("- Prometheus/config/health surfaces can be moved to a separate operations listener."),
	)

	return strings.Join(lines, "\n")
}

func helpRow(binding, description string) string {
	return keyStyle.Render(fmt.Sprintf("%-18s", binding)) + " " + textStyle.Render(description)
}

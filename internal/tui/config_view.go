package tui

import (
	"fmt"
	"strings"
)

func (m Model) configView() string {
	w := m.contentWidth()
	origins := "disabled"
	if m.cfg.CORSEnabled {
		origins = strings.Join(m.cfg.CORSOrigins, ", ")
	}
	lines := []string{
		subtitleStyle.Render("Runtime config"), "",
		labelStyle.Render("listen:") + "              " + textStyle.Render(m.cfg.Listen),
		labelStyle.Render("default_provider:") + "    " + valueStyle.Render(m.cfg.DefaultProvider),
		labelStyle.Render("providers:") + "           " + textStyle.Render(fmt.Sprintf("%d", len(m.cfg.Providers))),
		labelStyle.Render("public_health:") + "       " + textStyle.Render(fmt.Sprintf("%t", m.cfg.PublicHealth)),
		labelStyle.Render("request_body_limit:") + "  " + textStyle.Render(m.cfg.RequestBodyLimit),
		labelStyle.Render("cors:") + "                " + textStyle.Render(origins),
		labelStyle.Render("recent_logs:") + "         " + textStyle.Render(fmt.Sprintf("%d", m.cfg.LogRecent)),
		"", subtitleStyle.Render("Configured providers"), "",
	}
	for _, p := range m.cfg.Providers {
		role := ""
		if p.Name == m.cfg.DefaultProvider {
			role = " [default]"
		}
		circuit := "disabled"
		if p.CircuitEnabled {
			circuit = fmt.Sprintf("threshold=%d open=%s", p.CircuitFailureThreshold, p.CircuitOpenFor)
		}
		lines = append(lines,
			textStyle.Render(fmt.Sprintf("• %s (%s)%s", p.Name, p.Type, role))+
				mutedStyle.Render("  "+p.BaseURL+"  key="+p.APIKey+"  circuit="+circuit),
		)
	}
	lines = append(lines,
		"", subtitleStyle.Render("Security posture"), "",
		textStyle.Render("• clients authenticate only with GemGate bearer tokens"),
		textStyle.Render("• provider credentials are sanitized from inbound requests and injected server-side"),
		textStyle.Render("• config, secrets and provider circuit policies are swapped atomically on hot reload"),
		textStyle.Render("• browser CORS policy is explicit; server-to-server clients are unaffected"),
		textStyle.Render("• provider quota, billing and safety responses pass through unchanged"),
	)
	return boxStyle.Width(w).Render(strings.Join(lines, "\n"))
}

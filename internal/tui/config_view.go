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
	rateLimit := m.cfg.RateLimitBackend
	if rateLimit == "" {
		rateLimit = "memory"
	}
	if rateLimit == "redis" {
		mode := m.cfg.RateLimitMode
		if mode == "" {
			mode = "standalone"
		}
		rateLimit = fmt.Sprintf("redis/%s (fail_open=%t)", mode, m.cfg.RateLimitFailOpen)
	}
	operationsAuth := "legacy client-token fallback"
	if m.cfg.DedicatedOperationsAuth {
		operationsAuth = "dedicated token"
	}
	tracing := "off"
	if m.cfg.TelemetryEnabled {
		collector := "environment/default endpoint"
		if m.cfg.TelemetryEndpointConfigured {
			collector = "configured endpoint"
		}
		tracing = fmt.Sprintf("OTLP %s sample=%.2f propagate=%t (%s)",
			m.cfg.TelemetryServiceName,
			m.cfg.TelemetrySampleRatio,
			m.cfg.TelemetryPropagateUpstream,
			collector,
		)
		if m.cfg.TelemetryEnvironment != "" {
			tracing += " env=" + m.cfg.TelemetryEnvironment
		}
	}
	lines := []string{
		subtitleStyle.Render("Runtime config"), "",
		labelStyle.Render("listen:") + "              " + textStyle.Render(m.cfg.Listen),
		labelStyle.Render("default_provider:") + "    " + valueStyle.Render(m.cfg.DefaultProvider),
		labelStyle.Render("providers:") + "           " + textStyle.Render(fmt.Sprintf("%d", len(m.cfg.Providers))),
		labelStyle.Render("public_health:") + "       " + textStyle.Render(fmt.Sprintf("%t", m.cfg.PublicHealth)),
		labelStyle.Render("operations_auth:") + "     " + textStyle.Render(operationsAuth),
		labelStyle.Render("request_body_limit:") + "  " + textStyle.Render(m.cfg.RequestBodyLimit),
		labelStyle.Render("rate_limit:") + "          " + textStyle.Render(rateLimit),
		labelStyle.Render("tracing:") + "             " + textStyle.Render(tracing),
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
		textStyle.Render("• application and operations bearer tokens can be separated into independent trust domains"),
		textStyle.Render("• provider credentials are sanitized from inbound requests and injected server-side"),
		textStyle.Render("• Redis rate-limit keys use token hashes; Redis credentials are never shown here"),
		textStyle.Render("• tracing captures bounded metadata only; collector endpoint and auth are never shown here"),
		textStyle.Render("• trace context propagation to AI providers is explicit opt-in; baggage is never forwarded"),
		textStyle.Render("• config, secrets and provider circuit policies are swapped atomically on hot reload"),
		textStyle.Render("• browser CORS policy is explicit; server-to-server clients are unaffected"),
		textStyle.Render("• provider quota, billing and safety responses pass through unchanged"),
	)
	return boxStyle.Width(w).Render(strings.Join(lines, "\n"))
}

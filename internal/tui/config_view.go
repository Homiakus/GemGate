package tui

import (
	"fmt"
	"strings"
)

func (m *Model) updateConfigViewport() {
	if m.layout.IsTiny() {
		return
	}
	m.configViewport.SetContent(m.configContent())
}

func (m Model) configView() string {
	w := m.contentWidth()
	return strings.Join([]string{
		sectionRule("Effective runtime configuration", w),
		mutedStyle.Render("Read-only, redacted operator view. Secrets and collector/Redis endpoints are never rendered."),
		"",
		m.configViewport.View(),
	}, "\n")
}

func (m Model) configContent() string {
	origins := "disabled"
	if m.cfg.CORSEnabled {
		origins = strings.Join(m.cfg.CORSOrigins, ", ")
		if origins == "" {
			origins = "enabled"
		}
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
		rateLimit = fmt.Sprintf("redis/%s, fail_open=%t", mode, m.cfg.RateLimitFailOpen)
	}

	operationsAuth := "legacy client-token fallback"
	if m.cfg.DedicatedOperationsAuth {
		operationsAuth = "dedicated operations token"
	}

	tracing := "off"
	if m.cfg.TelemetryEnabled {
		endpoint := "default/environment endpoint"
		if m.cfg.TelemetryEndpointConfigured {
			endpoint = "configured endpoint"
		}
		tracing = fmt.Sprintf("OTLP service=%s sample=%.2f propagate_upstream=%t, %s",
			m.cfg.TelemetryServiceName,
			m.cfg.TelemetrySampleRatio,
			m.cfg.TelemetryPropagateUpstream,
			endpoint,
		)
		if m.cfg.TelemetryEnvironment != "" {
			tracing += ", env=" + m.cfg.TelemetryEnvironment
		}
	}

	lines := []string{
		subtitleStyle.Render("Server"),
		kvRow("listen", m.cfg.Listen, 20),
		kvRow("public health", fmt.Sprintf("%t", m.cfg.PublicHealth), 20),
		kvRow("request body", m.cfg.RequestBodyLimit, 20),
		kvRow("trusted proxies", fmt.Sprintf("%d configured", len(m.cfg.TrustedProxies)), 20),
		"",
		subtitleStyle.Render("Control plane"),
		kvRow("operations auth", operationsAuth, 20),
		kvRow("rate limit", rateLimit, 20),
		kvRow("tracing", tracing, 20),
		kvRow("CORS", origins, 20),
		kvRow("recent logs", fmt.Sprintf("%d", m.cfg.LogRecent), 20),
		"",
		subtitleStyle.Render("Providers"),
	}

	if len(m.cfg.Providers) == 0 {
		lines = append(lines, mutedStyle.Render("No configured providers."))
	}
	for _, p := range m.cfg.Providers {
		role := "named"
		if p.Name == m.cfg.DefaultProvider {
			role = "default"
		}
		circuit := "disabled"
		if p.CircuitEnabled {
			circuit = fmt.Sprintf("threshold=%d, open=%s", p.CircuitFailureThreshold, p.CircuitOpenFor)
		}
		lines = append(lines,
			fmt.Sprintf("%s  %s  %s",
				valueStyle.Render(p.Name),
				mutedStyle.Render(p.Type),
				mutedStyle.Render("role="+role),
			),
			"  "+mutedStyle.Render(truncate(p.BaseURL, max(24, m.contentWidth()-8))),
			"  "+mutedStyle.Render("key="+p.APIKey+"  circuit="+circuit),
		)
	}

	lines = append(lines,
		"",
		subtitleStyle.Render("Security posture"),
		securityLine(m.cfg.DedicatedOperationsAuth, "application and operations credentials are separated", "operations endpoints still accept legacy client-token fallback"),
		textStyle.Render("OK provider credentials are stripped inbound and injected server-side"),
		textStyle.Render("OK Redis URLs/credentials and OTLP collector credentials are hidden"),
		textStyle.Render("OK tracing is metadata-only; body/query/arbitrary headers are excluded"),
		textStyle.Render("OK provider trace propagation is opt-in and baggage is never forwarded"),
		textStyle.Render("OK reload candidates validate fully before the runtime snapshot is swapped"),
	)

	return strings.Join(lines, "\n")
}

func securityLine(ok bool, success, warning string) string {
	if ok {
		return okStyle.Render("OK ") + textStyle.Render(success)
	}
	return warnStyle.Render("! ") + textStyle.Render(warning)
}

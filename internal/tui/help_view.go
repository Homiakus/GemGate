package tui

import "strings"

func (m Model) helpView() string {
	w := m.contentWidth()
	text := strings.Join([]string{
		subtitleStyle.Render("Controls"), "",
		labelStyle.Render("1-6") + "            " + textStyle.Render("switch sections"),
		labelStyle.Render("tab/right") + "      " + textStyle.Render("next section"),
		labelStyle.Render("shift+tab/left") + " " + textStyle.Render("previous section"),
		labelStyle.Render("r") + "              " + textStyle.Render("refresh immediately"),
		labelStyle.Render("space / p") + "      " + textStyle.Render("pause live refresh"),
		labelStyle.Render("j/k  ↑/↓") + "      " + textStyle.Render("scroll logs"),
		labelStyle.Render("pgup/pgdn") + "      " + textStyle.Render("page through logs"),
		labelStyle.Render("a/w/e/u") + "        " + textStyle.Render("filter logs: all/warn/errors/auth"),
		labelStyle.Render("?") + "              " + textStyle.Render("toggle help footer"),
		"", subtitleStyle.Render("Operational notes"), "",
		textStyle.Render("• terminate TLS at a trusted reverse proxy/load balancer"),
		textStyle.Render("• use one GemGate token per app/user and set rate_limit_rpm where useful"),
		textStyle.Render("• provider health is passive; use /_metrics for detailed provider series"),
		textStyle.Render("• configure an explicit CORS allow-list, or disable CORS for server-only deployments"),
		textStyle.Render("• /_metrics requires a GemGate bearer token"),
	}, "\n")
	return boxStyle.Width(w).Render(text)
}

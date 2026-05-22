package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gemgate/internal/gateway"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ──────────────────────────────────── tabs ──────────────────────────────────

type tab int

const (
	tabDashboard tab = iota
	tabLogs
	tabClients
	tabRoutes
	tabConfig
	tabHelp
)

var tabNames = []string{"Overview", "Logs", "Clients", "Routes", "Config", "Help"}

// ──────────────────────────────────── log filter ────────────────────────────

type logFilter int

const (
	filterAll logFilter = iota
	filterWarnings
	filterErrors
	filterAuth
)

// ──────────────────────────────────── tick ──────────────────────────────────

type tickMsg time.Time

// ──────────────────────────────────── model ─────────────────────────────────

type Model struct {
	gw      *gateway.Gateway
	cfg     gateway.ConfigSnapshot
	metrics gateway.MetricsSnapshot
	logs    []gateway.LogEntry

	active      tab
	filter      logFilter
	paused      bool
	width       int
	height      int
	lastRefresh time.Time

	visibleLogs []gateway.LogEntry
	logTable    table.Model
	help        help.Model
	keys        keyMap
}

// ──────────────────────────────────── keys ──────────────────────────────────

type keyMap struct {
	Quit       key.Binding
	Refresh    key.Binding
	Pause      key.Binding
	NextTab    key.Binding
	PrevTab    key.Binding
	Jump       key.Binding
	Help       key.Binding
	FilterAll  key.Binding
	FilterWarn key.Binding
	FilterErr  key.Binding
	FilterAuth key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextTab, k.Refresh, k.Pause, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.NextTab, k.PrevTab, k.Jump, k.Refresh},
		{k.Pause, k.FilterAll, k.FilterWarn, k.FilterErr, k.FilterAuth},
		{k.Help, k.Quit},
	}
}

// ──────────────────────────────────── New ───────────────────────────────────

func New(gw *gateway.Gateway) Model {
	keys := keyMap{
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Pause:      key.NewBinding(key.WithKeys(" ", "p"), key.WithHelp("space/p", "pause")),
		NextTab:    key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab/right", "next")),
		PrevTab:    key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("shift+tab/left", "prev")),
		Jump:       key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6"), key.WithHelp("1-6", "menu")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		FilterAll:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
		FilterWarn: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "warn")),
		FilterErr:  key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "errors")),
		FilterAuth: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "auth")),
	}

	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		Bold(true).
		Foreground(lipgloss.Color("#c4b5fd")).
		Background(lipgloss.Color("#0f172a")).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color("#334155"))
	styles.Selected = styles.Selected.
		Bold(true).
		Foreground(lipgloss.Color("#f8fafc")).
		Background(lipgloss.Color("#4c1d95"))
	styles.Cell = styles.Cell.
		Foreground(lipgloss.Color("#dbeafe"))

	logTable := table.New(
		table.WithColumns(defaultLogColumns(96)),
		table.WithRows(nil),
		table.WithFocused(true),
		table.WithHeight(16),
		table.WithStyles(styles),
	)

	m := Model{gw: gw, active: tabDashboard, logTable: logTable, help: help.New(), keys: keys}
	m.refresh()
	return m
}

// ──────────────────────────────────── tea.Model ─────────────────────────────

func (m Model) Init() tea.Cmd {
	return tick()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Refresh):
			m.refresh()
			return m, nil
		case key.Matches(msg, m.keys.Pause):
			m.paused = !m.paused
			return m, nil
		case key.Matches(msg, m.keys.NextTab):
			m.active = (m.active + 1) % tab(len(tabNames))
			return m, nil
		case key.Matches(msg, m.keys.PrevTab):
			m.active = (m.active - 1 + tab(len(tabNames))) % tab(len(tabNames))
			return m, nil
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		case key.Matches(msg, m.keys.FilterAll):
			m.setFilter(filterAll)
			return m, nil
		case key.Matches(msg, m.keys.FilterWarn):
			m.setFilter(filterWarnings)
			return m, nil
		case key.Matches(msg, m.keys.FilterErr):
			m.setFilter(filterErrors)
			return m, nil
		case key.Matches(msg, m.keys.FilterAuth):
			m.setFilter(filterAuth)
			return m, nil
		}

		if idx := menuIndex(msg.String()); idx >= 0 {
			m.active = tab(idx)
			return m, nil
		}
	case tickMsg:
		if !m.paused {
			m.refresh()
		}
		return m, tick()
	}

	if m.active == tabLogs {
		var cmd tea.Cmd
		m.logTable, cmd = m.logTable.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) View() tea.View {
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		m.tabsView(),
		m.bodyView(),
		m.footerView(),
	)
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// ──────────────────────────────────── refresh ──────────────────────────────

func (m *Model) refresh() {
	m.cfg = m.gw.ConfigSnapshot()
	m.metrics = m.gw.Metrics()
	m.logs = m.gw.Logs()
	m.lastRefresh = time.Now()
	m.updateLogRows()
}

func (m *Model) setFilter(f logFilter) {
	m.filter = f
	m.updateLogRows()
}

func (m *Model) updateLogRows() {
	m.visibleLogs = make([]gateway.LogEntry, 0, len(m.logs))
	rows := make([]table.Row, 0, len(m.logs))
	for i := len(m.logs) - 1; i >= 0; i-- {
		e := m.logs[i]
		if !m.filterLog(e) {
			continue
		}
		m.visibleLogs = append(m.visibleLogs, e)
		rows = append(rows, table.Row{
			e.Time.Format("15:04:05"),
			strings.ToUpper(e.Level),
			safeText(e.Client, "-"),
			safeText(e.Method, "-"),
			statusText(e.Status),
			formatDuration(e.Duration),
			humanBytes(uint64(max64(e.Bytes, 0))),
			logPath(e),
		})
	}
	m.logTable.SetRows(rows)
}

func (m Model) filterLog(e gateway.LogEntry) bool {
	switch m.filter {
	case filterWarnings:
		return e.Level == "warn" || (e.Status >= 400 && e.Status < 500)
	case filterErrors:
		return e.Level == "error" || e.Status >= 500
	case filterAuth:
		return strings.Contains(strings.ToLower(e.Message), "auth") || e.Status == 401 || e.Client == "anonymous"
	default:
		return true
	}
}

// ──────────────────────────────────── resize ───────────────────────────────

func (m *Model) resize() {
	w := m.contentWidth()
	h := m.height - 13
	if h < 8 {
		h = 8
	}
	m.logTable.SetColumns(defaultLogColumns(w))
	m.logTable.SetWidth(w)
	m.logTable.SetHeight(h)
}

func (m Model) contentWidth() int {
	if m.width <= 0 {
		return 100
	}
	w := m.width - 4
	if w < 32 {
		return 32
	}
	return w
}

// ──────────────────────────────────── header ───────────────────────────────
// Matches SVG: [GemGate] [LIVE] [listen :8080] [1 clients]  updated 15:04:05

func (m Model) headerView() string {
	status := statusOKStyle.Render(" LIVE ")
	if m.paused {
		status = statusPausedStyle.Render(" PAUSED ")
	} else if m.metrics.Requests5xx > 0 || m.metrics.UpstreamErrors > 0 {
		status = statusWarnStyle.Render(" DEGRADED ")
	}

	last := "not refreshed"
	if !m.lastRefresh.IsZero() {
		last = m.lastRefresh.Format("15:04:05")
	}

	parts := []string{
		titleStyle.Render(" GemGate "),
		" ",
		status,
		" ",
		pillStyle.Render("listen " + m.gw.Addr()),
		" ",
		pillStyle.Render(fmt.Sprintf("%d clients", enabledClientCount(m.cfg))),
		"  ",
		mutedStyle.Render("updated " + last),
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

// ──────────────────────────────────── tabs ─────────────────────────────────
// Matches SVG: [1 Overview] [2 Logs] [3 Clients] [4 Routes] [5 Config] [6 Help]

func (m Model) tabsView() string {
	parts := make([]string, 0, len(tabNames))
	for i, name := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		style := inactiveTabStyle
		if tab(i) == m.active {
			style = activeTabStyle
		}
		parts = append(parts, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// ──────────────────────────────────── body ──────────────────────────────────

func (m Model) bodyView() string {
	switch m.active {
	case tabDashboard:
		return m.dashboardView()
	case tabLogs:
		return m.logsView()
	case tabClients:
		return m.clientsView()
	case tabRoutes:
		return m.routesView()
	case tabConfig:
		return m.configView()
	case tabHelp:
		return m.helpView()
	default:
		return ""
	}
}

// ──────────────────────────── 1. Overview / Dashboard ──────────────────────
// SVG: "Traffic overview" title, 2 rows × 4 cards, Traffic sparkline, footer

func (m Model) dashboardView() string {
	stats := summarize(m.logs)
	w := m.contentWidth()
	cols := responsiveColumns(w)
	cardW := cardWidth(w, cols)
	errorValue := fmt.Sprintf("%d", stats.FourXX+stats.FiveXX)
	if stats.FiveXX > 0 {
		errorValue = badStyle.Render(errorValue)
	}

	cards := []string{
		metricCard("Requests", fmt.Sprintf("%d", m.metrics.Requests), fmt.Sprintf("%d/min recent", stats.LastMinute), cardW),
		metricCard("Success", fmt.Sprintf("%.1f%%", stats.SuccessRate), fmt.Sprintf("%d ok responses", stats.TwoXX), cardW),
		metricCard("Errors", errorValue, fmt.Sprintf("%d 4xx / %d 5xx", stats.FourXX, stats.FiveXX), cardW),
		metricCard("Latency", formatDuration(stats.P95Latency), "p95 recent", cardW),
		metricCard("In-flight", fmt.Sprintf("%d", m.metrics.InFlight), "active upstream calls", cardW),
		metricCard("Rate limited", fmt.Sprintf("%d", m.metrics.RateLimited), "429 responses", cardW),
		metricCard("Bytes out", humanBytes(m.metrics.BytesOut), "proxied downstream", cardW),
		metricCard("Uptime", m.metrics.Uptime.Round(time.Second).String(), "since start", cardW),
	}

	trafficBlock := lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Traffic"),
		stats.Trend,
		mutedStyle.Render("last 20 minutes, each block is one minute"),
	)

	latest := textStyle.Render("No requests yet.")
	if stats.Last.Status != 0 || stats.Last.Message != "" {
		latest = strings.Join([]string{
			textStyle.Render(fmt.Sprintf("%s %s %s",
				stats.Last.Time.Format("15:04:05"),
				coloredStatus(stats.Last.Status),
				logPath(stats.Last))),
			mutedStyle.Render(fmt.Sprintf("client=%s  latency=%s  request_id=%s",
				safeText(stats.Last.Client, "-"),
				formatDuration(stats.Last.Duration),
				safeText(stats.Last.RequestID, "-"))),
		}, "\n")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		boxStyle.Width(w).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				subtitleStyle.Render("Traffic overview"),
				"",
				cardGrid(cards, cols),
			)),
		boxStyle.Width(w).Render(trafficBlock),
		boxStyle.Width(w).Render(subtitleStyle.Render("Latest event")+"\n"+latest),
	)
}

// ──────────────────────────── 2. Logs ──────────────────────────────────────
// SVG: filter pills, table with colored Lv/Status, detail row at bottom

func (m Model) logsView() string {
	w := m.contentWidth()
	filter := lipgloss.JoinHorizontal(lipgloss.Left,
		filterPill("a", "all", m.filter == filterAll), " ",
		filterPill("w", "warn", m.filter == filterWarnings), " ",
		filterPill("e", "errors", m.filter == filterErrors), " ",
		filterPill("u", "auth", m.filter == filterAuth),
	)
	title := fmt.Sprintf("Request logs: %d shown / %d retained", len(m.visibleLogs), len(m.logs))
	return boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render(title),
		filter,
		"",
		m.logTable.View(),
		"",
		m.logDetailView(),
	))
}

func (m Model) logDetailView() string {
	if len(m.visibleLogs) == 0 {
		return mutedStyle.Render("No matching log entries.")
	}
	idx := m.logTable.Cursor()
	if idx < 0 || idx >= len(m.visibleLogs) {
		idx = 0
	}
	e := m.visibleLogs[idx]
	fields := []string{
		fmt.Sprintf("request_id=%s", safeText(e.RequestID, "-")),
		fmt.Sprintf("client=%s", safeText(e.Client, "-")),
		fmt.Sprintf("status=%s", statusText(e.Status)),
		fmt.Sprintf("duration=%s", formatDuration(e.Duration)),
		fmt.Sprintf("bytes=%s", humanBytes(uint64(max64(e.Bytes, 0)))),
	}
	if e.Message != "" {
		fields = append(fields, "message="+e.Message)
	}
	return detailBgStyle.Render(strings.Join(fields, "  "))
}

// ──────────────────────────── 3. Clients ───────────────────────────────────
// SVG: header row with label style, rows, hint box at the bottom

func (m Model) clientsView() string {
	stats := summarize(m.logs)
	w := m.contentWidth()

	// Header row
	header := labelStyle.Render(
		fmt.Sprintf("%-19s %-11s %-12s %-10s %-10s %-14s",
			"Client", "State", "RPM limit", "Requests", "Error", "Avg"))

	lines := []string{header}

	seen := map[string]bool{}
	for _, c := range m.cfg.Clients {
		cs := stats.Clients[c.Name]
		seen[c.Name] = true
		lines = append(lines, clientRow(c.Name, enabledText(c.Enabled), limitText(c.RateLimitRPM), cs, w))
	}

	extra := make([]string, 0)
	for name := range stats.Clients {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		lines = append(lines, clientRow(name, "seen", "n/a", stats.Clients[name], w))
	}

	if len(lines) == 1 {
		lines = append(lines, mutedStyle.Render("No configured clients."))
	}

	hint := hintBoxStyle.Width(min(w-8, 80)).Render(strings.Join([]string{
		"Recommended: issue one token per app/user.",
		"Use rate_limit_rpm to protect billing and quotas.",
	}, "\n"))

	return boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Clients and usage"),
		mutedStyle.Render("Per-client rate limits are enforced in-memory per running process."),
		"",
		strings.Join(lines, "\n"),
		"",
		hint,
	))
}

// ──────────────────────────── 4. Routes ────────────────────────────────────
// SVG: route table, Quick starts code block

func (m Model) routesView() string {
	stats := summarize(m.logs)
	w := m.contentWidth()
	baseURL := localBaseURL(m.cfg.Listen)

	// Header row
	header := labelStyle.Render(
		fmt.Sprintf("%-19s %-11s %-12s %-10s %-18s",
			"Route", "Req", "Error", "Avg", "Purpose"))

	routes := []string{
		header,
		routeRow("OpenAI compat", stats.Routes["openai"], "OpenAI-style SDK base", w),
		routeRow("Gemini native", stats.Routes["gemini"], "Gemini REST passthrough", w),
		routeRow("Metrics", stats.Routes["metrics"], "Prometheus scrape", w),
		routeRow("Config", stats.Routes["config"], "Redacted runtime config", w),
		routeRow("Health", stats.Routes["health"], "Public health probe", w),
	}

	quickStarts := hintBoxStyle.Width(min(w-8, 96)).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Quick starts"),
		codeStyle.Render("OpenAI base URL: "+baseURL+"/v1beta/openai/"),
		codeStyle.Render("Native Gemini:   "+baseURL+"/v1beta/models/gemini-3.5-flash:generateContent"),
		codeStyle.Render("Metrics: curl "+baseURL+"/_metrics -H \"Authorization: Bearer $GEMGATE_TOKEN\""),
	))

	return boxStyle.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left,
		subtitleStyle.Render("Routes"),
		"",
		strings.Join(routes, "\n"),
		"",
		quickStarts,
	))
}

// ──────────────────────────── 5. Config ────────────────────────────────────

func (m Model) configView() string {
	w := m.contentWidth()
	lines := []string{
		subtitleStyle.Render("Runtime config"),
		"",
		labelStyle.Render("listen:") + "             " + textStyle.Render(m.cfg.Listen),
		labelStyle.Render("upstream_base_url:") + "  " + textStyle.Render(m.cfg.UpstreamBaseURL),
		labelStyle.Render("upstream_api_key:") + "   " + textStyle.Render(m.cfg.UpstreamAPIKey),
		labelStyle.Render("public_health:") + "      " + textStyle.Render(fmt.Sprintf("%t", m.cfg.PublicHealth)),
		labelStyle.Render("request_body_limit:") + " " + textStyle.Render(m.cfg.RequestBodyLimit),
		labelStyle.Render("recent_logs:") + "        " + textStyle.Render(fmt.Sprintf("%d", m.cfg.LogRecent)),
		"",
		subtitleStyle.Render("Security posture"),
		"",
		textStyle.Render("• external clients must use Authorization: Bearer <GEMGATE_TOKEN>"),
		textStyle.Render("• Gemini API key is injected only upstream and redacted in /_config"),
		textStyle.Render("• quota and safety responses from Gemini are passed through unchanged"),
	}
	return boxStyle.Width(w).Render(strings.Join(lines, "\n"))
}

// ──────────────────────────── 6. Help ──────────────────────────────────────

func (m Model) helpView() string {
	w := m.contentWidth()
	text := strings.Join([]string{
		subtitleStyle.Render("Controls"),
		"",
		labelStyle.Render("1-6") + "            " + textStyle.Render("switch menu sections"),
		labelStyle.Render("tab/right") + "      " + textStyle.Render("next section"),
		labelStyle.Render("shift+tab/left") + " " + textStyle.Render("previous section"),
		labelStyle.Render("r") + "              " + textStyle.Render("refresh immediately"),
		labelStyle.Render("space / p") + "      " + textStyle.Render("pause live refresh"),
		labelStyle.Render("j/k  ↑/↓") + "      " + textStyle.Render("scroll logs"),
		labelStyle.Render("pgup/pgdn") + "      " + textStyle.Render("page through logs"),
		labelStyle.Render("a/w/e/u") + "        " + textStyle.Render("filter logs: all/warn/errors/auth"),
		labelStyle.Render("?") + "              " + textStyle.Render("toggle help footer"),
		"",
		subtitleStyle.Render("Operational notes"),
		"",
		textStyle.Render("• Use a real TLS edge: Caddy, Nginx, Traefik, Cloudflare Tunnel, or a load balancer."),
		textStyle.Render("• Use per-client tokens and optional rate_limit_rpm for consumers."),
		textStyle.Render("• Prometheus endpoint: /_metrics (requires client bearer token)."),
	}, "\n")
	return boxStyle.Width(w).Render(text)
}

// ──────────────────────────── footer ───────────────────────────────────────
// SVG: single muted line at the bottom with key hints

func (m Model) footerView() string {
	return mutedStyle.Render(m.help.View(m.keys))
}

// ──────────────────────────── tick ──────────────────────────────────────────

func tick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return tickMsg(time.Now())
	}
}

// ──────────────────────────── stats ────────────────────────────────────────

type statsSnapshot struct {
	Requests    int
	LastMinute  int
	TwoXX       int
	FourXX      int
	FiveXX      int
	SuccessRate float64
	AvgLatency  time.Duration
	P95Latency  time.Duration
	Trend       string
	Last        gateway.LogEntry
	Clients     map[string]clientStats
	Routes      map[string]routeStats
}

type clientStats struct {
	Requests   int
	Errors     int
	BytesOut   int64
	AvgLatency time.Duration
	durations  []time.Duration
}

type routeStats struct {
	Requests   int
	Errors     int
	AvgLatency time.Duration
	durations  []time.Duration
}

func summarize(logs []gateway.LogEntry) statsSnapshot {
	out := statsSnapshot{
		Clients: map[string]clientStats{},
		Routes:  map[string]routeStats{},
		Trend:   mutedStyle.Render("no recent traffic"),
	}
	var durations []time.Duration
	var totalLatency time.Duration
	now := time.Now()
	buckets := make([]int, 20)

	for _, e := range logs {
		if e.Status == 0 && e.Message == "" {
			continue
		}
		out.Last = e
		if e.Status == 0 {
			continue
		}

		out.Requests++
		if now.Sub(e.Time) <= time.Minute {
			out.LastMinute++
		}
		switch {
		case e.Status >= 200 && e.Status < 300:
			out.TwoXX++
		case e.Status >= 400 && e.Status < 500:
			out.FourXX++
		case e.Status >= 500:
			out.FiveXX++
		}

		if e.Duration > 0 {
			durations = append(durations, e.Duration)
			totalLatency += e.Duration
		}

		minutesAgo := int(now.Sub(e.Time).Minutes())
		if minutesAgo >= 0 && minutesAgo < len(buckets) {
			buckets[len(buckets)-1-minutesAgo]++
		}

		name := safeText(e.Client, "unknown")
		cs := out.Clients[name]
		cs.Requests++
		cs.BytesOut += e.Bytes
		if e.Status >= 400 {
			cs.Errors++
		}
		if e.Duration > 0 {
			cs.durations = append(cs.durations, e.Duration)
		}
		out.Clients[name] = cs

		route := routeKey(e.Path)
		rs := out.Routes[route]
		rs.Requests++
		if e.Status >= 400 {
			rs.Errors++
		}
		if e.Duration > 0 {
			rs.durations = append(rs.durations, e.Duration)
		}
		out.Routes[route] = rs
	}

	if out.Requests > 0 {
		out.SuccessRate = float64(out.TwoXX) / float64(out.Requests) * 100
	}
	if len(durations) > 0 {
		out.AvgLatency = totalLatency / time.Duration(len(durations))
		out.P95Latency = percentile(durations, 95)
	}
	for name, cs := range out.Clients {
		if len(cs.durations) > 0 {
			cs.AvgLatency = averageDuration(cs.durations)
			out.Clients[name] = cs
		}
	}
	for name, rs := range out.Routes {
		if len(rs.durations) > 0 {
			rs.AvgLatency = averageDuration(rs.durations)
			out.Routes[name] = rs
		}
	}
	out.Trend = sparkline(buckets)
	return out
}

// ──────────────────────────── log table columns ────────────────────────────

func defaultLogColumns(width int) []table.Column {
	pathWidth := width - 77
	if pathWidth < 24 {
		pathWidth = 24
	}
	return []table.Column{
		{Title: "Time", Width: 8},
		{Title: "Lv", Width: 5},
		{Title: "Client", Width: 12},
		{Title: "Method", Width: 7},
		{Title: "Status", Width: 7},
		{Title: "Dur", Width: 9},
		{Title: "Bytes", Width: 9},
		{Title: "Path", Width: pathWidth},
	}
}

// ──────────────────────────── metric cards ─────────────────────────────────

func metricCard(label, value, note string, width int) string {
	return cardStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left,
		mutedStyle.Render(label),
		valueStyle.Render(value),
		dimStyle.Render(note),
	))
}

func cardGrid(cards []string, cols int) string {
	rows := make([]string, 0, (len(cards)+cols-1)/cols)
	for i := 0; i < len(cards); i += cols {
		end := i + cols
		if end > len(cards) {
			end = len(cards)
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, appendSpacing(cards[i:end])...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func appendSpacing(items []string) []string {
	if len(items) <= 1 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, " ")
		}
		out = append(out, item)
	}
	return out
}

// ──────────────────────────── filter pills ─────────────────────────────────

func filterPill(k, label string, selected bool) string {
	text := k + " " + label
	if selected {
		return selectedPillStyle.Render(text)
	}
	return pillStyle.Render(text)
}

// ──────────────────────────── row formatters ───────────────────────────────

func clientRow(name, state, limit string, stats clientStats, width int) string {
	errorRate := "0.0%"
	if stats.Requests > 0 {
		errorRate = fmt.Sprintf("%.1f%%", float64(stats.Errors)/float64(stats.Requests)*100)
	}

	// Color the state text
	stateText := textStyle.Render(state)
	if state == "enabled" {
		stateText = okStyle.Render(state)
	}

	// Color error rate
	errText := textStyle.Render(errorRate)
	if stats.Errors > 0 {
		errText = warnStyle.Render(errorRate)
	}

	return fmt.Sprintf("%-19s %s%-*s %-12s %-10s %s%-*s %-14s",
		textStyle.Render(truncate(name, 18)),
		stateText, max(0, 11-len(state)), "",
		textStyle.Render(limit),
		textStyle.Render(fmt.Sprintf("%d", stats.Requests)),
		errText, max(0, 10-len(errorRate)), "",
		textStyle.Render(formatDuration(stats.AvgLatency)),
	)
}

func routeRow(name string, stats routeStats, purpose string, width int) string {
	errorRate := "0.0%"
	if stats.Requests > 0 {
		errorRate = fmt.Sprintf("%.1f%%", float64(stats.Errors)/float64(stats.Requests)*100)
	}

	errText := textStyle.Render(errorRate)
	if stats.Errors > 0 {
		errText = warnStyle.Render(errorRate)
	}

	return fmt.Sprintf("%-19s %-11s %s%-*s %-10s %-18s",
		textStyle.Render(truncate(name, 18)),
		textStyle.Render(fmt.Sprintf("%d", stats.Requests)),
		errText, max(0, 12-len(errorRate)), "",
		textStyle.Render(formatDuration(stats.AvgLatency)),
		mutedStyle.Render(truncate(purpose, 30)),
	)
}

// ──────────────────────────── responsive layout ────────────────────────────

func responsiveColumns(width int) int {
	switch {
	case width >= 112:
		return 4
	case width >= 82:
		return 3
	case width >= 58:
		return 2
	default:
		return 1
	}
}

func cardWidth(width, cols int) int {
	extraPerCard := 6 // border plus horizontal padding.
	gaps := cols - 1
	w := (width - gaps - extraPerCard*cols) / cols
	if w < 16 {
		return 16
	}
	if w > 30 {
		return 30
	}
	return w
}

// ──────────────────────────── helpers ───────────────────────────────────────

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

// coloredStatus renders a status code with the appropriate SVG color.
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

func percentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := (len(cp)*p + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	return cp[idx-1]
}

func averageDuration(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	var total time.Duration
	for _, v := range values {
		total += v
	}
	return total / time.Duration(len(values))
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
	case strings.HasPrefix(path, "/v1beta/openai/") || strings.HasPrefix(path, "/v1/openai/"):
		return "openai"
	case strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/v1/"):
		return "gemini"
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
	case "6":
		return 5
	default:
		return -1
	}
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
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

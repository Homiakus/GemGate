package tui

import (
	"fmt"
	"strings"
	"time"

	"gemgate/internal/gateway"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type tab int

const (
	tabOverview tab = iota
	tabRequests
	tabProviders
	tabClients
	tabConfig
)

type sectionDef struct {
	Name        string
	Description string
}

var sections = []sectionDef{
	{Name: "Overview", Description: "health and traffic"},
	{Name: "Requests", Description: "request stream and diagnostics"},
	{Name: "Providers", Description: "upstreams and circuit state"},
	{Name: "Clients", Description: "consumer usage and limits"},
	{Name: "Config", Description: "effective runtime posture"},
}

type logFilter int

const (
	filterAll logFilter = iota
	filterWarnings
	filterErrors
	filterAuth
)

type tickMsg time.Time

type Model struct {
	gw      *gateway.Gateway
	cfg     gateway.ConfigSnapshot
	metrics gateway.MetricsSnapshot
	logs    []gateway.LogEntry

	active      tab
	filter      logFilter
	paused      bool
	showHelp    bool
	width       int
	height      int
	layout      Layout
	lastRefresh time.Time

	visibleLogs   []gateway.LogEntry
	logTable      table.Model
	providerTable table.Model
	clientTable   table.Model

	configViewport viewport.Model
	helpViewport   viewport.Model
	keys           keyMap
}

func New(gw *gateway.Gateway) Model {
	m := Model{
		gw:             gw,
		active:         tabOverview,
		width:          100,
		height:         30,
		logTable:       newTable(),
		providerTable:  newTable(),
		clientTable:    newTable(),
		configViewport: viewport.New(),
		helpViewport:   viewport.New(),
		keys:           newKeyMap(),
	}
	m.configViewport.SoftWrap = true
	m.helpViewport.SoftWrap = true
	m.refresh()
	m.resize()
	return m
}

func newTable() table.Model {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		Bold(true).
		Foreground(mutedColor).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(borderColor)
	styles.Selected = styles.Selected.
		Bold(true).
		Foreground(foregroundColor).
		Background(surfaceColor)
	styles.Cell = styles.Cell.Foreground(foregroundColor)

	return table.New(
		table.WithColumns(nil),
		table.WithRows(nil),
		table.WithFocused(true),
		table.WithHeight(10),
		table.WithStyles(styles),
	)
}

func (m Model) Init() tea.Cmd { return tick() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil

	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}

		if m.showHelp {
			switch {
			case key.Matches(msg, m.keys.Help), key.Matches(msg, m.keys.Back):
				m.showHelp = false
				return m, nil
			}
			var cmd tea.Cmd
			m.helpViewport, cmd = m.helpViewport.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keys.Help):
			m.showHelp = true
			m.updateHelpViewport()
			return m, nil
		case key.Matches(msg, m.keys.Refresh):
			m.refresh()
			return m, nil
		case key.Matches(msg, m.keys.Pause):
			m.paused = !m.paused
			return m, nil
		case key.Matches(msg, m.keys.NextTab):
			m.setActive((m.active + 1) % tab(len(sections)))
			return m, nil
		case key.Matches(msg, m.keys.PrevTab):
			m.setActive((m.active - 1 + tab(len(sections))) % tab(len(sections)))
			return m, nil
		case key.Matches(msg, m.keys.Back):
			if m.active != tabOverview {
				m.setActive(tabOverview)
			}
			return m, nil
		}

		if idx := menuIndex(msg.String()); idx >= 0 {
			m.setActive(tab(idx))
			return m, nil
		}

		if m.active == tabRequests {
			switch {
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
		}

	case tickMsg:
		if !m.paused {
			m.refresh()
		}
		return m, tick()
	}

	var cmd tea.Cmd
	switch m.active {
	case tabRequests:
		m.logTable, cmd = m.logTable.Update(msg)
	case tabProviders:
		m.providerTable, cmd = m.providerTable.Update(msg)
	case tabClients:
		m.clientTable, cmd = m.clientTable.Update(msg)
	case tabConfig:
		m.configViewport, cmd = m.configViewport.Update(msg)
	}
	return m, cmd
}

func (m Model) View() tea.View {
	var content string
	if m.layout.IsTiny() {
		content = m.tinyView()
	} else {
		header := m.headerView()
		footer := m.footerView()
		body := m.workspaceView()
		if m.layout.IsWide() {
			body = lipgloss.JoinHorizontal(lipgloss.Top, m.navigationView(), strings.Repeat(" ", m.layout.Gap), body)
			content = lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
		} else {
			content = lipgloss.JoinVertical(lipgloss.Left, header, m.sectionBarView(), body, footer)
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m *Model) refresh() {
	m.cfg = m.gw.ConfigSnapshot()
	m.metrics = m.gw.Metrics()
	m.logs = m.gw.Logs()
	m.lastRefresh = time.Now()
	m.updateLogRows()
	m.updateProviderRows()
	m.updateClientRows()
	m.updateConfigViewport()
	if m.showHelp {
		m.updateHelpViewport()
	}
}

func (m *Model) setActive(next tab) {
	if next < 0 || int(next) >= len(sections) {
		return
	}
	m.active = next
	m.showHelp = false
	if next == tabConfig {
		m.updateConfigViewport()
	}
}

func (m *Model) setFilter(f logFilter) {
	m.filter = f
	m.updateLogRows()
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

func (m *Model) resize() {
	m.layout = calculateLayout(m.width, m.height)
	if m.layout.IsTiny() {
		return
	}

	workspaceWidth := m.layout.WorkspaceWidth
	contentWidth := max(24, workspaceWidth-4)
	bodyHeight := max(8, m.layout.BodyHeight)

	m.logTable.SetColumns(defaultLogColumns(contentWidth))
	m.logTable.SetWidth(contentWidth)
	m.logTable.SetHeight(max(5, bodyHeight-10))

	m.providerTable.SetColumns(providerColumns(contentWidth))
	m.providerTable.SetWidth(contentWidth)
	m.providerTable.SetHeight(max(5, bodyHeight-11))

	m.clientTable.SetColumns(clientColumns(contentWidth))
	m.clientTable.SetWidth(contentWidth)
	m.clientTable.SetHeight(max(5, bodyHeight-11))

	m.configViewport.SetWidth(contentWidth)
	m.configViewport.SetHeight(max(6, bodyHeight-3))
	m.helpViewport.SetWidth(contentWidth)
	m.helpViewport.SetHeight(max(6, bodyHeight-3))

	m.updateLogRows()
	m.updateProviderRows()
	m.updateClientRows()
	m.updateConfigViewport()
	if m.showHelp {
		m.updateHelpViewport()
	}
}

func (m Model) contentWidth() int {
	if m.layout.WorkspaceWidth > 0 {
		return max(24, m.layout.WorkspaceWidth-4)
	}
	return 96
}

func (m Model) overallStatus() string {
	if m.paused {
		return statusPausedStyle.Render("PAUSED")
	}
	if m.metrics.Requests5xx > 0 || m.metrics.UpstreamErrors > 0 || providerAttentionCount(m.metrics.Providers, m.metrics.Circuits) > 0 {
		return statusWarnStyle.Render("! DEGRADED")
	}
	return statusOKStyle.Render("OK LIVE")
}

func (m Model) headerView() string {
	section := sections[m.active]
	left := titleStyle.Render("GemGate") + mutedStyle.Render(" / ") + textStyle.Render(section.Name)

	updated := "--:--:--"
	if !m.lastRefresh.IsZero() {
		updated = m.lastRefresh.Format("15:04:05")
	}
	right := m.overallStatus()
	if m.layout.Width >= 72 {
		right = fmt.Sprintf("%s  req %d  in %d  %s", right, m.metrics.Requests, m.metrics.InFlight, mutedStyle.Render(updated))
	}
	if m.layout.Width >= 110 {
		right = fmt.Sprintf("%s  %s", mutedStyle.Render(m.cfg.Listen), right)
	}
	return headerStyle.Width(max(1, m.layout.Width-2)).Render(padBetween(left, right, max(1, m.layout.Width-4)))
}

func (m Model) navigationView() string {
	lines := []string{subtitleStyle.Render("Sections"), ""}
	for i, section := range sections {
		prefix := "  "
		style := navItemStyle
		if tab(i) == m.active {
			prefix = "> "
			style = navActiveStyle
		}
		lines = append(lines, style.Render(fmt.Sprintf("%s%d  %s", prefix, i+1, section.Name)))
	}
	lines = append(lines,
		"",
		sectionRuleStyle.Render(strings.Repeat("─", max(1, m.layout.NavWidth-4))),
		"",
		mutedStyle.Render("listen"),
		textStyle.Render(truncate(m.cfg.Listen, max(10, m.layout.NavWidth-4))),
		"",
		mutedStyle.Render(fmt.Sprintf("%d providers", len(m.cfg.Providers))),
		mutedStyle.Render(fmt.Sprintf("%d clients", enabledClientCount(m.cfg))),
	)
	return navigationStyle.Width(max(12, m.layout.NavWidth-3)).Render(strings.Join(lines, "\n"))
}

func (m Model) sectionBarView() string {
	if m.layout.IsCompact() {
		current := fmt.Sprintf("%d/%d %s", int(m.active)+1, len(sections), sections[m.active].Name)
		hint := mutedStyle.Render("[ ] / tab sections")
		return headerStyle.Width(max(1, m.layout.Width-2)).Render(padBetween(navActiveStyle.Render(current), hint, max(1, m.layout.Width-4)))
	}

	parts := make([]string, 0, len(sections))
	for i, section := range sections {
		label := fmt.Sprintf("%d %s", i+1, section.Name)
		if tab(i) == m.active {
			parts = append(parts, activeTabStyle.Render(label))
		} else {
			parts = append(parts, inactiveTabStyle.Render(label))
		}
	}
	return headerStyle.Render(strings.Join(parts, "   "))
}

func (m Model) workspaceView() string {
	var body string
	if m.showHelp {
		body = m.helpView()
	} else {
		body = m.bodyView()
	}
	return workspaceStyle.Width(max(1, m.layout.WorkspaceWidth-2)).Render(body)
}

func (m Model) bodyView() string {
	switch m.active {
	case tabOverview:
		return m.dashboardView()
	case tabRequests:
		return m.logsView()
	case tabProviders:
		return m.providersView()
	case tabClients:
		return m.clientsView()
	case tabConfig:
		return m.configView()
	default:
		return ""
	}
}

func (m Model) footerView() string {
	width := max(1, m.layout.Width-2)
	if m.showHelp {
		return footerStyle.Width(width).Render("↑↓ scroll   esc close   q quit")
	}

	context := ""
	switch m.active {
	case tabRequests:
		context = "↑↓ select   a/w/e/u filter"
	case tabProviders, tabClients:
		context = "↑↓ select"
	case tabConfig:
		context = "↑↓/pgup/pgdn scroll"
	default:
		context = "r refresh"
	}

	global := "? help   q quit"
	if m.layout.Width >= 76 {
		global = "tab/[ ] section   r refresh   ? help   q quit"
	}
	return footerStyle.Width(width).Render(padBetween(context, global, max(1, width-2)))
}

func (m Model) tinyView() string {
	lines := []string{
		titleStyle.Render("GemGate"),
		"",
		warnStyle.Render("Terminal too small for the operator UI."),
		fmt.Sprintf("Minimum: %dx%d", minTerminalWidth, minTerminalHeight),
		fmt.Sprintf("Current: %dx%d", m.width, m.height),
		"",
		mutedStyle.Render("Resize the terminal or press q to quit."),
	}
	return strings.Join(lines, "\n")
}

func tick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return tickMsg(time.Now())
	}
}

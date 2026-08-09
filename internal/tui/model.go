package tui

import (
	"fmt"
	"strings"
	"time"

	"gemgate/internal/gateway"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type tab int

const (
	tabDashboard tab = iota
	tabLogs
	tabClients
	tabProviders
	tabConfig
	tabHelp
)

var tabNames = []string{"Overview", "Logs", "Clients", "Providers", "Config", "Help"}

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
	width       int
	height      int
	lastRefresh time.Time

	visibleLogs []gateway.LogEntry
	logTable    table.Model
	help        help.Model
	keys        keyMap
}

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
	styles.Header = styles.Header.Bold(true).Foreground(lipgloss.Color("#c4b5fd")).Background(lipgloss.Color("#0f172a")).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(lipgloss.Color("#334155"))
	styles.Selected = styles.Selected.Bold(true).Foreground(lipgloss.Color("#f8fafc")).Background(lipgloss.Color("#4c1d95"))
	styles.Cell = styles.Cell.Foreground(lipgloss.Color("#dbeafe"))

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

func (m Model) Init() tea.Cmd { return tick() }

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
	content := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), m.tabsView(), m.bodyView(), m.footerView())
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

func (m Model) headerView() string {
	status := statusOKStyle.Render(" LIVE ")
	if m.paused {
		status = statusPausedStyle.Render(" PAUSED ")
	} else if m.metrics.Requests5xx > 0 || m.metrics.UpstreamErrors > 0 || providerAttentionCount(m.metrics.Providers) > 0 {
		status = statusWarnStyle.Render(" DEGRADED ")
	}

	last := "not refreshed"
	if !m.lastRefresh.IsZero() {
		last = m.lastRefresh.Format("15:04:05")
	}

	parts := []string{
		titleStyle.Render(" GemGate "), " ", status, " ",
		pillStyle.Render("listen " + m.gw.Addr()), " ",
		pillStyle.Render(fmt.Sprintf("%d clients", enabledClientCount(m.cfg))), " ",
		pillStyle.Render(fmt.Sprintf("%d providers", len(m.cfg.Providers))), "  ",
		mutedStyle.Render("updated " + last),
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

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

func (m Model) bodyView() string {
	switch m.active {
	case tabDashboard:
		return m.dashboardView()
	case tabLogs:
		return m.logsView()
	case tabClients:
		return m.clientsView()
	case tabProviders:
		return m.providersView()
	case tabConfig:
		return m.configView()
	case tabHelp:
		return m.helpView()
	default:
		return ""
	}
}

func (m Model) footerView() string { return mutedStyle.Render(m.help.View(m.keys)) }

func tick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return tickMsg(time.Now())
	}
}

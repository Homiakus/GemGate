package tui

import "charm.land/lipgloss/v2"

// ──────────────────────────────────────────────────────────────────
// Palette — matches the reference SVG mockups exactly.
// bg:         #0b1020 / #060b16
// panel:      #101827  border #344055
// card:       #121c2d  border #334155
// tab-active: #6d28d9  tab-inactive: #1f2937
// pill/muted: #263244  green-pill: #22c55e  pill-active: #5eead4
// text:       #dbeafe  small/muted: #94a3b8  dim: #475569
// accent:     #5eead4  value: #5eead4 (bold)
// ok:         #86efac  warn: #fbbf24  error: #f87171  bad: #f87171
// code:       #fde68a  label: #c4b5fd
// head:       #0f172a  row-alt: #111c2f  row-selected: #4c1d95
// ──────────────────────────────────────────────────────────────────

var (
	// Title — bold white-on-purple, consistent with the "GemGate" badge
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f8fafc")).
			Background(lipgloss.Color("#6d28d9")).
			Padding(0, 1)

	// Section subtitle — teal accent
	subtitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#5eead4"))

	// Number prefix in tab labels ("1 ", "2 ", …)
	menuIndexStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8"))

	// Active tab — purple background, white text
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f8fafc")).
			Background(lipgloss.Color("#6d28d9")).
			Padding(0, 2)

	// Inactive tab — dark gray background, muted text
	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#dbeafe")).
				Background(lipgloss.Color("#1f2937")).
				Padding(0, 2)

	// Outer panel box — rounded border, dark fill
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#344055")).
			Padding(1, 2).
			MarginTop(1)

	// Metric cards inside overview
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#334155")).
			Padding(0, 2)

	// Status pills — LIVE / DEGRADED / PAUSED
	statusOKStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#052e16")).
			Background(lipgloss.Color("#22c55e")).
			Padding(0, 1)

	statusWarnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#422006")).
			Background(lipgloss.Color("#fbbf24")).
			Padding(0, 1)

	statusPausedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#dbeafe")).
				Background(lipgloss.Color("#263244")).
				Padding(0, 1)

	// Muted pill (listen addr, client count)
	pillStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dbeafe")).
			Background(lipgloss.Color("#263244")).
			Padding(0, 1)

	// Selected filter pill — teal bg, dark text
	selectedPillStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#052e16")).
				Background(lipgloss.Color("#5eead4")).
				Padding(0, 1)

	// Code snippets — yellowish on dark bg
	codeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#fde68a")).
			Background(lipgloss.Color("#0f172a")).
			Padding(0, 1)

	// Text styles for log/table content
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
	textStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#dbeafe"))
	valueStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5eead4"))
	badStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f87171"))
	warnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#fbbf24"))
	okStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#86efac"))
	labelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#c4b5fd"))

	// Detail row background in log view
	detailBgStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#0f172a")).
			Foreground(lipgloss.Color("#94a3b8")).
			Padding(0, 1)

	// Table header row background
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#5eead4")).
				Background(lipgloss.Color("#0f172a"))

	// Hint box for clients/routes tips
	hintBoxStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#0f172a")).
			Foreground(lipgloss.Color("#94a3b8")).
			Padding(1, 2)
)

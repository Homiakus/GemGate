package tui

import "charm.land/lipgloss/v2"

var (
	foregroundColor = lipgloss.Color("#d8dee9")
	mutedColor      = lipgloss.Color("#8792a2")
	dimColor        = lipgloss.Color("#566273")
	accentColor     = lipgloss.Color("#7dd3fc")
	borderColor     = lipgloss.Color("#344054")
	surfaceColor    = lipgloss.Color("#111827")
	successColor    = lipgloss.Color("#86efac")
	warningColor    = lipgloss.Color("#facc15")
	errorColor      = lipgloss.Color("#f87171")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(foregroundColor)

	subtitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(foregroundColor)

	mutedStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	dimStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	textStyle = lipgloss.NewStyle().
			Foreground(foregroundColor)

	valueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)

	labelStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	okStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(successColor)

	warnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(warningColor)

	badStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(errorColor)

	statusOKStyle     = okStyle
	statusWarnStyle   = warnStyle
	statusPausedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(mutedColor)

	// Full-width surfaces reserve their outer width in the layout calculation.
	// Keep only a left inset here; symmetric horizontal padding adds one extra
	// rendered cell at medium/compact widths in Lip Gloss v2.
	headerStyle = lipgloss.NewStyle().
			Foreground(foregroundColor).
			PaddingLeft(1)

	footerStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			PaddingLeft(1)

	navigationStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).
			BorderForeground(borderColor).
			Padding(0, 1)

	navItemStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	navActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor).
			Underline(true)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(mutedColor)

	workspaceStyle = lipgloss.NewStyle().
			PaddingLeft(1)

	sectionRuleStyle = lipgloss.NewStyle().
				Foreground(borderColor)

	selectedPillStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor)

	pillStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	codeStyle = lipgloss.NewStyle().
			Foreground(accentColor)

	detailBgStyle = lipgloss.NewStyle().
			Foreground(foregroundColor).
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(borderColor).
			PaddingLeft(1)

	hintBoxStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(borderColor).
			PaddingLeft(1)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(mutedColor)

	keyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)

	// Setup and legacy helper views still expect symmetric padding. Keep that
	// compatibility independent from the new full-width workspace styles.
	boxStyle  = lipgloss.NewStyle().Padding(0, 1)
	cardStyle = lipgloss.NewStyle()
)

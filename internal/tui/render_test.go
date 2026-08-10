package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
)

func TestRenderedViewsStayWithinTerminalWidth(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{name: "wide", width: 140, height: 40},
		{name: "medium", width: 100, height: 30},
		{name: "compact", width: 70, height: 24},
		{name: "minimum", width: 54, height: 14},
		{name: "tiny", width: 50, height: 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := renderTestModel(tt.width, tt.height)
			for section := range sections {
				m.active = tab(section)
				m.resize()
				assertViewWidth(t, m.View().Content, tt.width)
			}
			m.showHelp = true
			m.updateHelpViewport()
			assertViewWidth(t, m.View().Content, tt.width)
		})
	}
}

func renderTestModel(width, height int) Model {
	m := Model{
		active:         tabOverview,
		width:          width,
		height:         height,
		logTable:       newTable(),
		providerTable:  newTable(),
		clientTable:    newTable(),
		configViewport: viewport.New(),
		helpViewport:   viewport.New(),
		keys:           newKeyMap(),
	}
	m.configViewport.SoftWrap = true
	m.helpViewport.SoftWrap = true
	m.resize()
	return m
}

func assertViewWidth(t *testing.T, content string, maxWidth int) {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		if width := lipgloss.Width(line); width > maxWidth {
			t.Fatalf("line %d display width=%d exceeds terminal width=%d: %q", i+1, width, maxWidth, line)
		}
	}
}

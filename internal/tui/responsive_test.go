package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestResponsiveTableColumns(t *testing.T) {
	logCases := []struct {
		width int
		want  int
	}{{130, 9}, {100, 6}, {75, 5}, {55, 3}}
	for _, tc := range logCases {
		if got := len(defaultLogColumns(tc.width)); got != tc.want {
			t.Fatalf("log columns at %d=%d want=%d", tc.width, got, tc.want)
		}
	}

	providerCases := []struct {
		width int
		want  int
	}{{130, 9}, {90, 7}, {70, 4}}
	for _, tc := range providerCases {
		if got := len(providerColumns(tc.width)); got != tc.want {
			t.Fatalf("provider columns at %d=%d want=%d", tc.width, got, tc.want)
		}
	}

	clientCases := []struct {
		width int
		want  int
	}{{130, 7}, {90, 5}, {70, 3}}
	for _, tc := range clientCases {
		if got := len(clientColumns(tc.width)); got != tc.want {
			t.Fatalf("client columns at %d=%d want=%d", tc.width, got, tc.want)
		}
	}
}

func TestTruncateUsesTerminalDisplayWidth(t *testing.T) {
	cases := []struct {
		name  string
		input string
		width int
	}{
		{name: "cyrillic", input: "провайдер-длинное-имя", width: 12},
		{name: "cjk", input: "東京モデルゲートウェイ", width: 10},
		{name: "ascii", input: "very-long-provider-name", width: 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.input, tc.width)
			if width := lipgloss.Width(got); width > tc.width {
				t.Fatalf("display width=%d > %d for %q", width, tc.width, got)
			}
			if lipgloss.Width(tc.input) > tc.width && !strings.HasSuffix(got, "…") {
				t.Fatalf("truncated string %q has no ellipsis", got)
			}
		})
	}
}

func TestSectionJumpMatchesFiveSectionInformationArchitecture(t *testing.T) {
	if len(sections) != 5 {
		t.Fatalf("sections=%d want=5", len(sections))
	}
	if menuIndex("5") != 4 {
		t.Fatalf("5 should select Config")
	}
	if menuIndex("6") != -1 {
		t.Fatalf("legacy Help tab shortcut must be removed")
	}
	for _, section := range sections {
		if section.Name == "Help" {
			t.Fatalf("Help must use contextual overlay, not a navigation section")
		}
	}
}

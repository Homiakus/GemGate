package tui

import "testing"

func TestCalculateLayoutBreakpoints(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		height   int
		wantMode layoutMode
		wantNav  bool
		wantTiny bool
	}{
		{name: "wide", width: 140, height: 40, wantMode: layoutWide, wantNav: true},
		{name: "medium", width: 100, height: 30, wantMode: layoutMedium},
		{name: "compact", width: 70, height: 24, wantMode: layoutCompact},
		{name: "tiny width", width: 50, height: 20, wantMode: layoutTiny, wantTiny: true},
		{name: "tiny height", width: 100, height: 12, wantMode: layoutTiny, wantTiny: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateLayout(tt.width, tt.height)
			if got.Mode != tt.wantMode {
				t.Fatalf("mode=%v want=%v", got.Mode, tt.wantMode)
			}
			if got.WorkspaceWidth <= 0 || got.BodyHeight <= 0 {
				t.Fatalf("non-positive layout: %#v", got)
			}
			if tt.wantNav && got.NavWidth <= 0 {
				t.Fatalf("wide layout missing navigation width: %#v", got)
			}
			if tt.wantTiny != got.IsTiny() {
				t.Fatalf("tiny=%t want=%t", got.IsTiny(), tt.wantTiny)
			}
		})
	}
}

func TestCalculateLayoutNeverProducesNegativeDimensions(t *testing.T) {
	for width := 1; width <= 160; width++ {
		for height := 1; height <= 50; height++ {
			got := calculateLayout(width, height)
			if got.WorkspaceWidth < 1 || got.BodyHeight < 1 {
				t.Fatalf("%dx%d produced invalid layout %#v", width, height, got)
			}
		}
	}
}

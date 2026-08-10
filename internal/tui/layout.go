package tui

type layoutMode int

const (
	layoutWide layoutMode = iota
	layoutMedium
	layoutCompact
	layoutTiny
)

const (
	minTerminalWidth  = 54
	minTerminalHeight = 14
)

type Layout struct {
	Width          int
	Height         int
	Mode           layoutMode
	HeaderHeight   int
	SectionHeight  int
	FooterHeight   int
	NavWidth       int
	Gap            int
	WorkspaceWidth int
	BodyHeight     int
}

func calculateLayout(width, height int) Layout {
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}

	l := Layout{
		Width:        width,
		Height:       height,
		HeaderHeight: 1,
		FooterHeight: 1,
		Gap:          1,
	}

	if width < minTerminalWidth || height < minTerminalHeight {
		l.Mode = layoutTiny
		l.WorkspaceWidth = max(1, width)
		l.BodyHeight = max(1, height-2)
		return l
	}

	switch {
	case width >= 118:
		l.Mode = layoutWide
		l.NavWidth = 20
		l.SectionHeight = 0
		l.WorkspaceWidth = max(48, width-l.NavWidth-l.Gap)
	case width >= 80:
		l.Mode = layoutMedium
		l.SectionHeight = 1
		l.WorkspaceWidth = width
	default:
		l.Mode = layoutCompact
		l.SectionHeight = 1
		l.WorkspaceWidth = width
	}

	l.BodyHeight = height - l.HeaderHeight - l.SectionHeight - l.FooterHeight - 2
	if l.BodyHeight < 8 {
		l.BodyHeight = 8
	}
	return l
}

func (l Layout) IsWide() bool    { return l.Mode == layoutWide }
func (l Layout) IsCompact() bool { return l.Mode == layoutCompact }
func (l Layout) IsTiny() bool    { return l.Mode == layoutTiny }

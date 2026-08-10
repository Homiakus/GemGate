package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Quit       key.Binding
	Refresh    key.Binding
	Pause      key.Binding
	NextTab    key.Binding
	PrevTab    key.Binding
	Jump       key.Binding
	Help       key.Binding
	Back       key.Binding
	FilterAll  key.Binding
	FilterWarn key.Binding
	FilterErr  key.Binding
	FilterAuth key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Pause:      key.NewBinding(key.WithKeys(" ", "p"), key.WithHelp("space", "pause")),
		NextTab:    key.NewBinding(key.WithKeys("tab", "]"), key.WithHelp("tab/]", "next section")),
		PrevTab:    key.NewBinding(key.WithKeys("shift+tab", "["), key.WithHelp("shift+tab/[", "previous section")),
		Jump:       key.NewBinding(key.WithKeys("1", "2", "3", "4", "5"), key.WithHelp("1-5", "sections")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		FilterAll:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
		FilterWarn: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "warnings")),
		FilterErr:  key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "errors")),
		FilterAuth: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "auth")),
	}
}

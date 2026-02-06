package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keybindings for the TUI.
type KeyMap struct {
	// Navigation
	Up     key.Binding
	Down   key.Binding
	Top    key.Binding
	Bottom key.Binding

	// Actions
	Open   key.Binding
	Add    key.Binding
	Delete key.Binding
	Search key.Binding

	// General
	Quit   key.Binding
	Cancel key.Binding
	Submit key.Binding
	Help   key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "bottom"),
		),
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open in neovim"),
		),
		Add: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add URL"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
	}
}

// ShortHelp returns keybindings to show in the short help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Open, k.Delete, k.Quit}
}

// FullHelp returns keybindings to show in the full help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Open, k.Add, k.Delete, k.Search},
		{k.Quit, k.Cancel, k.Help},
	}
}

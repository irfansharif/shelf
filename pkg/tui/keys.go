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
	Open         key.Binding
	Add          key.Binding
	Import       key.Binding
	Delete       key.Binding
	Archive      key.Binding
	ShowArchive  key.Binding
	Search       key.Binding
	Reload       key.Binding
	SafariReload key.Binding

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
		Import: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "import safari"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		Archive: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "archive"),
		),
		ShowArchive: key.NewBinding(
			key.WithKeys("X"),
			key.WithHelp("X", "show archived"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Reload: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "reload"),
		),
		SafariReload: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "refetch (safari)"),
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
	return []key.Binding{k.Add, k.Import, k.Open, k.Delete, k.Archive, k.ShowArchive, k.Reload, k.SafariReload, k.Quit}
}

// FullHelp returns keybindings to show in the full help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Open, k.Add, k.Import, k.Delete, k.Archive, k.ShowArchive, k.Search, k.Reload, k.SafariReload},
		{k.Quit, k.Cancel, k.Help},
	}
}

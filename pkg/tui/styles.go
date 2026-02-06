package tui

import "github.com/charmbracelet/lipgloss"

// Styles holds all the lipgloss styles for the TUI.
type Styles struct {
	// App-level styles
	App    lipgloss.Style
	Header lipgloss.Style
	Footer lipgloss.Style

	// List styles
	ListTitle       lipgloss.Style
	ListItem        lipgloss.Style
	ListItemTitle   lipgloss.Style
	ListItemDesc    lipgloss.Style
	SelectedItem    lipgloss.Style
	SelectedTitle   lipgloss.Style
	SelectedDesc    lipgloss.Style
	SelectionMarker lipgloss.Style

	// Input styles
	InputBox    lipgloss.Style
	InputLabel  lipgloss.Style
	InputField  lipgloss.Style
	InputPrompt lipgloss.Style

	// Status styles
	Spinner lipgloss.Style
	Error   lipgloss.Style
	Success lipgloss.Style
	Muted   lipgloss.Style

	// Search styles
	SearchBox     lipgloss.Style
	SearchPrompt  lipgloss.Style
	SearchPlaceholder lipgloss.Style
}

// DefaultStyles returns the default style configuration.
func DefaultStyles() Styles {
	subtle := lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"}
	highlight := lipgloss.AdaptiveColor{Light: "#7D56F4", Dark: "#AD8CFF"}
	special := lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	errorColor := lipgloss.AdaptiveColor{Light: "#FF5F5F", Dark: "#FF8888"}

	return Styles{
		App: lipgloss.NewStyle().
			Padding(1, 2),

		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(highlight).
			MarginBottom(1),

		Footer: lipgloss.NewStyle().
			Foreground(subtle).
			MarginTop(1),

		ListTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(highlight),

		ListItem: lipgloss.NewStyle().
			PaddingLeft(2),

		ListItemTitle: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#fafafa"}),

		ListItemDesc: lipgloss.NewStyle().
			Foreground(subtle),

		SelectedItem: lipgloss.NewStyle().
			PaddingLeft(0),

		SelectedTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(highlight),

		SelectedDesc: lipgloss.NewStyle().
			Foreground(highlight),

		SelectionMarker: lipgloss.NewStyle().
			Foreground(highlight).
			SetString("› "),

		InputBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(highlight).
			Padding(1, 2).
			Width(60),

		InputLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(highlight).
			MarginBottom(1),

		InputField: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#fafafa"}),

		InputPrompt: lipgloss.NewStyle().
			Foreground(subtle),

		Spinner: lipgloss.NewStyle().
			Foreground(special),

		Error: lipgloss.NewStyle().
			Foreground(errorColor),

		Success: lipgloss.NewStyle().
			Foreground(special),

		Muted: lipgloss.NewStyle().
			Foreground(subtle),

		SearchBox: lipgloss.NewStyle().
			MarginBottom(1),

		SearchPrompt: lipgloss.NewStyle().
			Foreground(subtle).
			SetString("⊘ "),

		SearchPlaceholder: lipgloss.NewStyle().
			Foreground(subtle).
			Italic(true),
	}
}

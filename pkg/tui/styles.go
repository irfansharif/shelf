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

	// Tag styles
	Tag lipgloss.Style

	// Search styles
	SearchBox     lipgloss.Style
	SearchPrompt  lipgloss.Style
	SearchPlaceholder lipgloss.Style
}

// DefaultStyles returns the default style configuration using Solarized colors.
func DefaultStyles() Styles {
	// Solarized base tones
	base01 := lipgloss.Color("#586e75") // comments/secondary
	base00 := lipgloss.Color("#657b83") // body text (light bg)
	base0 := lipgloss.Color("#839496")  // body text (dark bg)
	base1 := lipgloss.Color("#93a1a1")  // emphasized content

	subtle := lipgloss.AdaptiveColor{Light: string(base01), Dark: string(base01)}
	body := lipgloss.AdaptiveColor{Light: string(base00), Dark: string(base0)}
	emphasis := lipgloss.AdaptiveColor{Light: string(base00), Dark: string(base1)}

	// Solarized accents
	yellow := lipgloss.Color("#b58900")
	orange := lipgloss.Color("#cb4b16")
	green := lipgloss.Color("#859900")

	return Styles{
		App: lipgloss.NewStyle().
			Padding(1, 2),

		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(orange),

		Footer: lipgloss.NewStyle().
			Foreground(subtle),

		ListTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(emphasis),

		ListItem: lipgloss.NewStyle().
			PaddingLeft(2),

		ListItemTitle: lipgloss.NewStyle().
			Foreground(body),

		ListItemDesc: lipgloss.NewStyle().
			Foreground(subtle),

		SelectedItem: lipgloss.NewStyle().
			PaddingLeft(0),

		SelectedTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(yellow),

		SelectedDesc: lipgloss.NewStyle().
			Foreground(body),

		SelectionMarker: lipgloss.NewStyle().
			Foreground(yellow).
			SetString("› "),

		InputBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(yellow).
			Padding(1, 2),

		InputLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(yellow).
			MarginBottom(1),

		InputField: lipgloss.NewStyle().
			Foreground(body),

		InputPrompt: lipgloss.NewStyle().
			Foreground(subtle),

		Spinner: lipgloss.NewStyle().
			Foreground(yellow),

		Error: lipgloss.NewStyle().
			Foreground(orange),

		Success: lipgloss.NewStyle().
			Foreground(green),

		Muted: lipgloss.NewStyle().
			Foreground(subtle),

		Tag: lipgloss.NewStyle().
			Foreground(green),

		SearchBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(yellow).
			Padding(0, 1),

		SearchPrompt: lipgloss.NewStyle().
			Foreground(subtle).
			SetString("⌕ "),

		SearchPlaceholder: lipgloss.NewStyle().
			Foreground(subtle).
			Italic(true),
	}
}

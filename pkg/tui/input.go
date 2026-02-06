package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// URLInputModel handles URL input state.
type URLInputModel struct {
	textInput textinput.Model
	styles    Styles
}

// NewURLInput creates a new URL input model.
func NewURLInput(styles Styles) URLInputModel {
	ti := textinput.New()
	ti.Placeholder = "https://example.com/article"
	ti.Focus()
	ti.CharLimit = 2048
	ti.Width = 54 // Fit within the input box

	return URLInputModel{
		textInput: ti,
		styles:    styles,
	}
}

// Init initializes the URL input model.
func (m URLInputModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages for the URL input.
func (m URLInputModel) Update(msg tea.Msg) (URLInputModel, tea.Cmd) {
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// View renders the URL input.
func (m URLInputModel) View() string {
	return m.styles.InputBox.Render(
		m.styles.InputLabel.Render("Add URL") + "\n\n" +
			m.textInput.View() + "\n\n" +
			m.styles.Muted.Render("Press Enter to fetch, Esc to cancel"),
	)
}

// Value returns the current input value.
func (m URLInputModel) Value() string {
	return m.textInput.Value()
}

// Reset clears the input.
func (m URLInputModel) Reset() URLInputModel {
	m.textInput.Reset()
	return m
}

// Focus focuses the input.
func (m URLInputModel) Focus() tea.Cmd {
	return m.textInput.Focus()
}

// SearchInputModel handles search input state.
type SearchInputModel struct {
	textInput textinput.Model
	styles    Styles
	active    bool
}

// NewSearchInput creates a new search input model.
func NewSearchInput(styles Styles) SearchInputModel {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 100
	ti.Width = 40

	return SearchInputModel{
		textInput: ti,
		styles:    styles,
		active:    false,
	}
}

// Init initializes the search input model.
func (m SearchInputModel) Init() tea.Cmd {
	return nil
}

// Update handles messages for the search input.
func (m SearchInputModel) Update(msg tea.Msg) (SearchInputModel, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// View renders the search input.
func (m SearchInputModel) View() string {
	if m.active {
		return m.styles.SearchPrompt.Render("") + m.textInput.View()
	}

	if m.textInput.Value() != "" {
		return m.styles.SearchPrompt.Render("") + m.styles.ListItemTitle.Render(m.textInput.Value())
	}

	return m.styles.SearchPrompt.Render("") + m.styles.SearchPlaceholder.Render("Search...")
}

// Value returns the current search query.
func (m SearchInputModel) Value() string {
	return m.textInput.Value()
}

// Activate enables search input mode.
func (m SearchInputModel) Activate() (SearchInputModel, tea.Cmd) {
	m.active = true
	return m, m.textInput.Focus()
}

// Deactivate disables search input mode.
func (m SearchInputModel) Deactivate() SearchInputModel {
	m.active = false
	m.textInput.Blur()
	return m
}

// Clear clears the search query.
func (m SearchInputModel) Clear() SearchInputModel {
	m.textInput.Reset()
	return m
}

// IsActive returns whether search is active.
func (m SearchInputModel) IsActive() bool {
	return m.active
}

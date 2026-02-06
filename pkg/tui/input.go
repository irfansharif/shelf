package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// URLInputModel handles URL input state.
type URLInputModel struct {
	textInput textinput.Model
	styles    Styles
	width     int
}

// NewURLInput creates a new URL input model.
func NewURLInput(styles Styles) URLInputModel {
	ti := textinput.New()
	ti.Placeholder = "https://example.com/article"
	ti.Focus()
	ti.CharLimit = 2048
	ti.Width = 54

	return URLInputModel{
		textInput: ti,
		styles:    styles,
		width:     60,
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
	boxWidth := m.width - 6
	box := m.styles.InputBox.Width(boxWidth)
	return box.Render(
		m.styles.InputLabel.Render("Add URL") + "\n\n" +
			m.textInput.View() + "\n\n" +
			m.styles.Muted.Render("Press Enter to fetch, Esc to cancel"),
	)
}

// Value returns the current input value.
func (m URLInputModel) Value() string {
	return m.textInput.Value()
}

// SetWidth sets the available width for the URL input.
func (m URLInputModel) SetWidth(w int) URLInputModel {
	m.width = w
	// InputBox: Width (content+padding) = w-6, inner content = w-6-4, minus prompt (2)
	m.textInput.Width = w - 6 - 4 - 2
	return m
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
	width     int
}

// NewSearchInput creates a new search input model.
func NewSearchInput(styles Styles) SearchInputModel {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.Prompt = ""
	ti.CharLimit = 100
	ti.Width = 40

	return SearchInputModel{
		textInput: ti,
		styles:    styles,
		active:    false,
		width:     60,
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

// View renders the search input as a bordered bar with a search icon.
func (m SearchInputModel) View() string {
	boxWidth := m.width - 6

	var content string
	icon := m.styles.SearchPrompt.Render("")
	if m.active {
		content = icon + m.textInput.View()
	} else if m.textInput.Value() != "" {
		content = icon + m.styles.ListItemTitle.Render(m.textInput.Value())
	} else {
		content = icon + m.styles.SearchPlaceholder.Render("Search...")
	}

	return m.styles.SearchBox.Width(boxWidth).Render(content)
}

// Value returns the current search query.
func (m SearchInputModel) Value() string {
	return m.textInput.Value()
}

// SetWidth sets the available width for the search input.
func (m SearchInputModel) SetWidth(w int) SearchInputModel {
	m.width = w
	// SearchBox: Width (content+padding) = w-6, inner content = w-6-2, minus icon (2)
	m.textInput.Width = w - 6 - 2 - 2
	return m
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

package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/irfansharif/browser/pkg/extractor"
	"github.com/irfansharif/browser/pkg/storage"
)

// State represents the current UI state.
type State int

const (
	stateList State = iota
	stateAddURL
	stateLoading
	stateSearch
)

// Model is the main TUI model.
type Model struct {
	state    State
	store    *storage.Store
	extract  *extractor.Extractor
	keys     KeyMap
	styles   Styles
	width    int
	height   int

	// List state
	articles  []storage.ArticleMeta
	cursor    int
	scrollPos int

	// Components
	urlInput    URLInputModel
	searchInput SearchInputModel
	spinner     spinner.Model

	// Status
	err        error
	statusMsg  string
}

// Messages
type (
	articlesFetchedMsg struct{ articles []storage.ArticleMeta }
	articleSavedMsg    struct{ meta storage.ArticleMeta }
	articleDeletedMsg  struct{ id string }
	extractionErrMsg   struct{ err error }
	editorFinishedMsg  struct{ err error }
	clearStatusMsg     struct{}
)

// New creates a new TUI model. endpointURL is the Modal ReaderLM-v2 endpoint
// used for HTML-to-Markdown conversion.
func New(store *storage.Store, endpointURL string) Model {
	styles := DefaultStyles()
	keys := DefaultKeyMap()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styles.Spinner

	return Model{
		state:       stateList,
		store:       store,
		extract:     extractor.New(endpointURL),
		keys:        keys,
		styles:      styles,
		urlInput:    NewURLInput(styles),
		searchInput: NewSearchInput(styles),
		spinner:     s,
		articles:    store.List(),
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.urlInput = m.urlInput.SetWidth(msg.Width)
		m.searchInput = m.searchInput.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		if m.state == stateLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case articleSavedMsg:
		m.state = stateList
		m.articles = m.store.List()
		m.statusMsg = fmt.Sprintf("Saved: %s", msg.meta.Title)
		m.err = nil
		return m, nil

	case extractionErrMsg:
		m.state = stateList
		m.err = msg.err
		m.statusMsg = ""
		return m, nil

	case articleDeletedMsg:
		m.articles = m.store.List()
		if m.cursor >= len(m.articles) && m.cursor > 0 {
			m.cursor--
		}
		m.statusMsg = "Article deleted"
		return m, nil

	case editorFinishedMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		// Reload index to pick up any manual edits to markdown metadata.
		if err := m.store.Reload(); err != nil {
			m.err = err
		}
		m.articles = m.store.List()
		if m.cursor >= len(m.articles) {
			m.cursor = max(0, len(m.articles)-1)
		}
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		m.err = nil
		return m, nil
	}

	// Update sub-components
	var cmd tea.Cmd
	switch m.state {
	case stateAddURL:
		m.urlInput, cmd = m.urlInput.Update(msg)
	case stateSearch:
		m.searchInput, cmd = m.searchInput.Update(msg)
		// Update filtered articles
		m.articles = m.store.Search(m.searchInput.Value())
		if m.cursor >= len(m.articles) {
			m.cursor = max(0, len(m.articles)-1)
		}
	}

	return m, cmd
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle state-specific keys first
	switch m.state {
	case stateAddURL:
		return m.handleAddURLKeys(msg)
	case stateSearch:
		return m.handleSearchKeys(msg)
	case stateLoading:
		// Only allow quit during loading
		if key.Matches(msg, m.keys.Quit) || key.Matches(msg, m.keys.Cancel) {
			m.state = stateList
			return m, nil
		}
		return m, nil
	}

	// List state keys
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.articles)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, m.keys.Top):
		m.cursor = 0
		return m, nil

	case key.Matches(msg, m.keys.Bottom):
		if len(m.articles) > 0 {
			m.cursor = len(m.articles) - 1
		}
		return m, nil

	case key.Matches(msg, m.keys.Open):
		return m.openSelectedArticle()

	case key.Matches(msg, m.keys.Add):
		m.state = stateAddURL
		m.urlInput = m.urlInput.Reset()
		m.err = nil
		return m, m.urlInput.Focus()

	case key.Matches(msg, m.keys.Delete):
		return m.deleteSelectedArticle()

	case key.Matches(msg, m.keys.Search):
		m.state = stateSearch
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Activate()
		return m, cmd

	case key.Matches(msg, m.keys.Reload):
		if err := m.store.Reload(); err != nil {
			m.err = err
			return m, nil
		}
		m.articles = m.store.List()
		if m.cursor >= len(m.articles) {
			m.cursor = max(0, len(m.articles)-1)
		}
		m.statusMsg = fmt.Sprintf("Reloaded %d articles", len(m.articles))
		return m, nil
	}

	return m, nil
}

func (m Model) handleAddURLKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.state = stateList
		return m, nil

	case key.Matches(msg, m.keys.Submit):
		url := strings.TrimSpace(m.urlInput.Value())
		if url == "" {
			m.state = stateList
			return m, nil
		}
		m.state = stateLoading
		return m, tea.Batch(
			m.spinner.Tick,
			m.extractAndSave(url),
		)
	}

	// Pass to text input
	var cmd tea.Cmd
	m.urlInput, cmd = m.urlInput.Update(msg)
	return m, cmd
}

func (m Model) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.state = stateList
		m.searchInput = m.searchInput.Deactivate()
		m.searchInput = m.searchInput.Clear()
		m.articles = m.store.List()
		m.cursor = 0
		return m, nil

	case key.Matches(msg, m.keys.Submit):
		m.state = stateList
		m.searchInput = m.searchInput.Deactivate()
		return m, nil
	}

	// Pass to search input
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	// Update filtered results
	m.articles = m.store.Search(m.searchInput.Value())
	if m.cursor >= len(m.articles) {
		m.cursor = max(0, len(m.articles)-1)
	}
	return m, cmd
}

func (m Model) extractAndSave(url string) tea.Cmd {
	return func() tea.Msg {
		article, err := m.extract.Extract(url)
		if err != nil {
			return extractionErrMsg{err: err}
		}

		if err := m.store.Save(article); err != nil {
			return extractionErrMsg{err: err}
		}

		return articleSavedMsg{meta: article.Meta}
	}
}

func (m Model) openSelectedArticle() (tea.Model, tea.Cmd) {
	if len(m.articles) == 0 || m.cursor >= len(m.articles) {
		return m, nil
	}

	article := m.articles[m.cursor]
	fpath := m.store.GetFilePath(article.FilePath)

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}

	// Run editor through a login shell to ensure config files are loaded
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	c := exec.Command(shell, "-l", "-c", fmt.Sprintf("%s %q", editor, fpath))
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m Model) deleteSelectedArticle() (tea.Model, tea.Cmd) {
	if len(m.articles) == 0 || m.cursor >= len(m.articles) {
		return m, nil
	}

	article := m.articles[m.cursor]
	if err := m.store.Delete(article.FilePath); err != nil {
		m.err = err
		return m, nil
	}

	return m, func() tea.Msg {
		return articleDeletedMsg{id: article.FilePath}
	}
}

// View renders the TUI.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var sb strings.Builder

	// Header
	total := m.store.Count()
	filtered := len(m.articles)
	if m.searchInput.Value() != "" {
		sb.WriteString(m.styles.Header.Render(fmt.Sprintf("Articles (%d of %d)", filtered, total)))
	} else {
		sb.WriteString(m.styles.Header.Render(fmt.Sprintf("Articles (%d)", total)))
	}
	sb.WriteString("\n")

	// Search bar
	sb.WriteString(m.searchInput.View())
	sb.WriteString("\n\n")

	// Main content area
	switch m.state {
	case stateAddURL:
		sb.WriteString(m.urlInput.View())
	case stateLoading:
		sb.WriteString(m.spinner.View())
		sb.WriteString(" Fetching article...")
	default:
		sb.WriteString(m.renderList())
	}

	// Status/error message — placed just above the footer help text.
	var statusLine string
	if m.err != nil {
		statusLine = m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err))
	} else if m.statusMsg != "" {
		statusLine = m.styles.Muted.Render(m.statusMsg)
	}

	// Footer — push to bottom by filling remaining vertical space.
	content := sb.String()
	contentHeight := strings.Count(content, "\n") + 1
	appPaddingV := 2 // Top + bottom padding from App style
	footerLines := 1 // Help text
	if statusLine != "" {
		footerLines += 2 // Status line + blank line separating it from help
	}
	remaining := m.height - contentHeight - appPaddingV - footerLines
	if remaining > 0 {
		sb.WriteString(strings.Repeat("\n", remaining))
	}

	if statusLine != "" {
		sb.WriteString(statusLine)
		sb.WriteString("\n")
	}
	sb.WriteString(m.styles.Footer.Render(m.renderHelp()))

	return m.styles.App.Render(sb.String())
}

func (m Model) renderList() string {
	if len(m.articles) == 0 {
		if m.searchInput.Value() != "" {
			return renderNoResults(m.searchInput.Value(), m.styles)
		}
		return renderEmptyState(m.styles)
	}

	var sb strings.Builder

	// Calculate visible items based on height
	listHeight := m.height - 12 // Account for header, footer, etc.
	itemHeight := 3            // Each item is 2 lines + 1 blank line
	visibleItems := listHeight / itemHeight
	if visibleItems < 1 {
		visibleItems = 5
	}

	// Calculate scroll position
	start := 0
	if m.cursor >= visibleItems {
		start = m.cursor - visibleItems + 1
	}
	end := start + visibleItems
	if end > len(m.articles) {
		end = len(m.articles)
	}

	for i := start; i < end; i++ {
		if i > start {
			sb.WriteString("\n\n")
		}
		selected := i == m.cursor
		sb.WriteString(renderArticleItem(m.articles[i], selected, m.width-4, m.styles))
	}

	return sb.String()
}

func (m Model) renderHelp() string {
	var parts []string

	switch m.state {
	case stateAddURL:
		parts = append(parts, "[enter] fetch", "[esc] cancel")
	case stateSearch:
		parts = append(parts, "[enter] done", "[esc] clear")
	default:
		parts = append(parts,
			"[a]dd URL",
			"[enter] open in neovim",
			"[d]elete",
			"[/] search",
			"[r]eload",
			"[q]uit",
		)
	}

	return strings.Join(parts, "  ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

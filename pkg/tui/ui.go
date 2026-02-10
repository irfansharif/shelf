package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/irfansharif/shelf/pkg/extractor"
	"github.com/irfansharif/shelf/pkg/storage"
)

// State represents the current UI state.
type State int

const (
	stateList State = iota
	stateAddURL
	stateLoading
	stateSearch
	stateConfirmOverwrite
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
	articles     []storage.ArticleMeta
	cursor       int
	scrollPos    int
	showArchived bool

	// Components
	urlInput    URLInputModel
	searchInput SearchInputModel
	spinner     spinner.Model

	// Overwrite confirmation
	pendingResult  *extractor.ExtractResult // post-fetch slug collision
	overwritePath  string                   // pre-fetch URL match: file path to delete
	overwriteTitle string                   // pre-fetch URL match: title for display

	// Status
	err        error
	statusMsg  string
}

// Messages
type (
	articlesFetchedMsg struct{ articles []storage.ArticleMeta }
	articleExtractedMsg struct{ result *extractor.ExtractResult }
	articleDeletedMsg   struct{ id string }
	extractionErrMsg    struct{ err error }
	editorFinishedMsg  struct{ err error }
	clearStatusMsg     struct{}
)

// New creates a new TUI model. endpointURL is the Modal endpoint used for
// HTML-to-Markdown conversion.
func New(store *storage.Store, endpointURL string) Model {
	styles := DefaultStyles()
	keys := DefaultKeyMap()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styles.Spinner

	m := Model{
		state:       stateList,
		store:       store,
		extract:     extractor.New(endpointURL),
		keys:        keys,
		styles:      styles,
		urlInput:    NewURLInput(styles),
		searchInput: NewSearchInput(styles),
		spinner:     s,
	}
	m.refreshArticles()
	return m
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

	case articleExtractedMsg:
		images := make([]storage.ImageFile, len(msg.result.Images))
		for i, img := range msg.result.Images {
			images[i] = storage.ImageFile{Path: img.Path, Data: img.Data}
		}
		// If overwriting a URL-matched article, delete old first.
		if m.overwritePath != "" {
			_ = m.store.Delete(m.overwritePath)
			m.overwritePath = ""
			m.overwriteTitle = ""
		}
		if err := m.store.SaveContent(msg.result.Title, msg.result.Content, images); err != nil {
			var existsErr *storage.ErrArticleExists
			if errors.As(err, &existsErr) {
				m.state = stateConfirmOverwrite
				m.pendingResult = msg.result
				return m, nil
			}
			m.state = stateList
			m.err = err
			return m, nil
		}
		m.state = stateList
		m.pendingResult = nil
		m.refreshArticles()
		m.err = nil
		for i, a := range m.articles {
			if a.Title == msg.result.Title {
				m.cursor = i
				break
			}
		}
		return m.openSelectedArticle()

	case extractionErrMsg:
		m.state = stateList
		m.err = msg.err
		m.statusMsg = ""
		return m, nil

	case articleDeletedMsg:
		m.refreshArticles()
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
		m.refreshArticles()
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
	case stateConfirmOverwrite:
		return m.handleConfirmOverwriteKeys(msg)
	}

	// Any keypress in the list clears a previous status/error toast.
	m.statusMsg = ""
	m.err = nil

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

	case key.Matches(msg, m.keys.Archive):
		return m.archiveSelectedArticle()

	case key.Matches(msg, m.keys.ShowArchive):
		m.showArchived = !m.showArchived
		m.refreshArticles()
		return m, nil

	case key.Matches(msg, m.keys.Search):
		m.state = stateSearch
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Activate()
		return m, cmd

	case key.Matches(msg, m.keys.Reload):
		if len(m.articles) == 0 || m.cursor >= len(m.articles) {
			return m, nil
		}
		article := m.articles[m.cursor]
		if article.SourceURL == "" {
			m.err = fmt.Errorf("no source URL for %q", article.Title)
			return m, nil
		}
		// Pre-fill the URL bar and go straight to fetching.
		m.urlInput = m.urlInput.SetValue(article.SourceURL)
		m.overwritePath = article.FilePath
		m.overwriteTitle = article.Title
		m.state = stateLoading
		return m, tea.Batch(
			m.spinner.Tick,
			m.extractArticle(article.SourceURL),
		)
	}

	return m, nil
}

func (m Model) handleAddURLKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c":
		if m.urlInput.Value() != "" {
			m.urlInput = m.urlInput.Reset()
			return m, nil
		}
		m.state = stateList
		return m, nil

	case key.Matches(msg, m.keys.Cancel):
		m.state = stateList
		return m, nil

	case key.Matches(msg, m.keys.Submit):
		url := strings.TrimSpace(m.urlInput.Value())
		if url == "" {
			m.state = stateList
			return m, nil
		}
		// Check if an article from this URL already exists.
		for _, a := range m.store.List() {
			if a.SourceURL == url {
				if a.IsArchived() {
					// Unarchive instead of re-fetching.
					var newTags []string
					for _, t := range a.Tags {
						if strings.ToLower(t) != "archived" {
							newTags = append(newTags, t)
						}
					}
					if err := m.store.UpdateTags(a.FilePath, newTags); err != nil {
						m.err = err
						m.state = stateList
						return m, nil
					}
					m.state = stateList
					m.statusMsg = fmt.Sprintf("Unarchived %q", a.Title)
					m.refreshArticles()
					for i, ar := range m.articles {
						if ar.FilePath == a.FilePath {
							m.cursor = i
							break
						}
					}
					return m, nil
				}
				m.state = stateConfirmOverwrite
				m.overwritePath = a.FilePath
				m.overwriteTitle = a.Title
				return m, nil
			}
		}
		m.state = stateLoading
		return m, tea.Batch(
			m.spinner.Tick,
			m.extractArticle(url),
		)
	}

	// Pass to text input
	var cmd tea.Cmd
	m.urlInput, cmd = m.urlInput.Update(msg)
	return m, cmd
}

func (m Model) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c":
		if m.searchInput.Value() != "" {
			m.searchInput = m.searchInput.Clear()
			m.refreshArticles()
			m.cursor = 0
			return m, nil
		}
		m.state = stateList
		m.searchInput = m.searchInput.Deactivate()
		return m, nil

	case key.Matches(msg, m.keys.Cancel):
		m.state = stateList
		m.searchInput = m.searchInput.Deactivate()
		m.searchInput = m.searchInput.Clear()
		m.refreshArticles()
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

func (m Model) extractArticle(url string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.extract.Extract(url)
		if err != nil {
			return extractionErrMsg{err: err}
		}
		return articleExtractedMsg{result: result}
	}
}

func (m Model) handleConfirmOverwriteKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.pendingResult != nil {
			// Post-fetch slug collision: force save.
			images := make([]storage.ImageFile, len(m.pendingResult.Images))
			for i, img := range m.pendingResult.Images {
				images[i] = storage.ImageFile{Path: img.Path, Data: img.Data}
			}
			if err := m.store.SaveContentForce(m.pendingResult.Title, m.pendingResult.Content, images); err != nil {
				m.state = stateList
				m.err = err
				m.pendingResult = nil
				return m, nil
			}
			m.state = stateList
			m.refreshArticles()
			m.err = nil
			for i, a := range m.articles {
				if a.Title == m.pendingResult.Title {
					m.cursor = i
					break
				}
			}
			m.pendingResult = nil
			return m.openSelectedArticle()
		}
		// Pre-fetch URL match: proceed to fetch (overwritePath stays set).
		url := strings.TrimSpace(m.urlInput.Value())
		m.state = stateLoading
		return m, tea.Batch(
			m.spinner.Tick,
			m.extractArticle(url),
		)
	case "n", "N", "esc":
		m.state = stateList
		m.pendingResult = nil
		m.overwritePath = ""
		m.overwriteTitle = ""
		return m, nil
	}
	return m, nil
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

func (m *Model) refreshArticles() {
	if m.searchInput.Value() != "" {
		m.articles = m.store.Search(m.searchInput.Value())
	} else {
		m.articles = m.applyArchiveFilter(m.store.List())
	}
	if m.cursor >= len(m.articles) {
		m.cursor = max(0, len(m.articles)-1)
	}
}

func (m Model) applyArchiveFilter(articles []storage.ArticleMeta) []storage.ArticleMeta {
	if m.showArchived {
		return articles
	}
	var filtered []storage.ArticleMeta
	for _, a := range articles {
		if !a.IsArchived() {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func (m Model) archiveSelectedArticle() (tea.Model, tea.Cmd) {
	if len(m.articles) == 0 || m.cursor >= len(m.articles) {
		return m, nil
	}

	article := m.articles[m.cursor]
	tags := article.Tags

	if article.IsArchived() {
		// Remove "archived" tag
		var newTags []string
		for _, t := range tags {
			if strings.ToLower(t) != "archived" {
				newTags = append(newTags, t)
			}
		}
		tags = newTags
		if err := m.store.UpdateTags(article.FilePath, tags); err != nil {
			m.err = err
			return m, nil
		}
		m.statusMsg = fmt.Sprintf("Unarchived %q", article.Title)
	} else {
		tags = append(tags, "archived")
		if err := m.store.UpdateTags(article.FilePath, tags); err != nil {
			m.err = err
			return m, nil
		}
		m.statusMsg = fmt.Sprintf("Archived %q", article.Title)
	}

	m.refreshArticles()
	return m, nil
}

// View renders the TUI.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var sb strings.Builder

	// Header
	filtered := len(m.articles)
	sb.WriteString(m.styles.Header.Render("Articles"))
	if m.showArchived {
		sb.WriteString(m.styles.Muted.Render(" (+archived)"))
	}
	if m.state != stateAddURL && m.state != stateLoading && m.state != stateConfirmOverwrite {
		if m.searchInput.Value() != "" {
			sb.WriteString(m.styles.Muted.Render(fmt.Sprintf(" (%d of %d of %d)", m.cursor+1, filtered, m.store.Count())))
		} else {
			sb.WriteString(m.styles.Muted.Render(fmt.Sprintf(" (%d of %d)", m.cursor+1, filtered)))
		}
	}
	sb.WriteString("\n\n")

	// Search bar (replaced by URL input when adding/loading)
	if m.state == stateAddURL || m.state == stateLoading || m.state == stateConfirmOverwrite {
		sb.WriteString(m.urlInput.View())
	} else {
		sb.WriteString(m.searchInput.View())
	}
	sb.WriteString("\n\n")

	// Main content area
	switch m.state {
	case stateAddURL:
		// Nothing below the URL input bar
	case stateLoading:
		sb.WriteString(m.spinner.View())
		sb.WriteString(" Fetching article...")
	case stateConfirmOverwrite:
		if m.pendingResult != nil {
			sb.WriteString(fmt.Sprintf("Article %q already exists. Overwrite? [y/n]", m.pendingResult.Title))
		} else {
			sb.WriteString(fmt.Sprintf("Already saved as %q. Re-fetch? [y/n]", m.overwriteTitle))
		}
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
	footerLines := 2 // Status/blank line + help text
	remaining := m.height - contentHeight - appPaddingV - footerLines
	if remaining > 0 {
		sb.WriteString(strings.Repeat("\n", remaining))
	}

	if statusLine != "" {
		sb.WriteString(statusLine)
		sb.WriteString("\n")
	} else {
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

	// Find the archive boundary (first archived article index) for separator.
	archiveBoundary := -1
	if m.showArchived {
		for i, a := range m.articles {
			if a.IsArchived() {
				archiveBoundary = i
				break
			}
		}
	}

	for i := start; i < end; i++ {
		if i > start {
			if i == archiveBoundary {
				// Draw a separator line between non-archived and archived groups.
				sep := strings.Repeat("─", m.width-6)
				sb.WriteString("\n\n")
				sb.WriteString(m.styles.Muted.Render(sep))
				sb.WriteString("\n\n")
			} else {
				sb.WriteString("\n\n")
			}
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
		parts = append(parts, "[enter] fetch", "[ctrl+c] clear", "[esc] cancel")
	case stateSearch:
		parts = append(parts, "[enter] done", "[ctrl+c] clear", "[esc] clear")
	case stateConfirmOverwrite:
		parts = append(parts, "[y] overwrite", "[n] cancel")
	default:
		archiveLabel := "[x] archive"
		if len(m.articles) > 0 && m.cursor < len(m.articles) && m.articles[m.cursor].IsArchived() {
			archiveLabel = "[x] unarchive"
		}
		showArchiveLabel := "[X] show archived"
		if m.showArchived {
			showArchiveLabel = "[X] hide archived"
		}
		parts = append(parts,
			"[a]dd URL",
			"[enter] open in neovim",
			"[d]elete",
			archiveLabel,
			showArchiveLabel,
			"[/] search",
			"[r]efetch",
			"[q]uit",
		)
	}

	return strings.Join(parts, "  ")
}

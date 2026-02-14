package tui

import (
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/irfansharif/shelf/pkg/extractor"
	"github.com/irfansharif/shelf/pkg/safari"
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
	stateConfirmDelete
	stateGatheringTabs
	stateImporting
	stateSafariWaiting
)

// Model is the main TUI model.
type Model struct {
	state          State
	store          *storage.Store
	extract        *extractor.Extractor
	keys           KeyMap
	styles         Styles
	width          int
	height         int
	safariURL    string         // URL being fetched via Safari (for process endpoint)
	safariWindow *safari.Window // tracked Safari window for the current fetch

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

	// Delete confirmation
	pendingDeletePath  string // file path of article pending deletion
	pendingDeleteTitle string // title for display in confirmation prompt

	// Import state
	importQueue   []string
	importTotal   int
	importDone    int
	importSkipped int
	importErrors  []string

	// Status
	err        error
	statusMsg  string

	// Fetch generation counter — incremented when a fetch starts, checked
	// when results arrive. Stale results (from cancelled fetches) are
	// discarded.
	fetchGen uint64

	// Tmux split
	tmuxPaneID   string // tmux pane ID for the editor split (e.g. "%42")
	positionFile string // temp file where vim writes cursor position on exit

	// suppressQuit is set when ctrl+c cancels a non-list state. This
	// prevents the SIGINT-generated QuitMsg (which arrives after the
	// KeyMsg transitions state to stateList) from killing the app.
	suppressQuit bool
}

// Messages
type (
	articlesFetchedMsg  struct{ articles []storage.ArticleMeta }
	articleExtractedMsg struct {
		result *extractor.ExtractResult
		gen    uint64
	}
	articleDeletedMsg struct{ id string }
	extractionErrMsg struct {
		err error
		gen uint64
	}
	editorFinishedMsg   struct{ err error }
	clearStatusMsg          struct{}
	safariOpenedMsg         struct {
		window *safari.Window
		err    error
	}
	safariHTMLExtractedMsg  struct {
		url  string
		html string
		err  error
	}
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
		state:        stateList,
		store:        store,
		extract:      extractor.New(endpointURL),
		keys:         keys,
		styles:       styles,
		urlInput:     NewURLInput(styles),
		searchInput:  NewSearchInput(styles),
		spinner:      s,
		positionFile: filepath.Join(os.TempDir(), fmt.Sprintf("shelf-pos-%d", os.Getpid())),
	}
	m.refreshArticles()
	return m
}

// InListState reports whether the model is in the default list browsing state
// and not suppressing a quit from a recent ctrl+c cancel.
func (m Model) InListState() bool {
	return m.state == stateList && !m.suppressQuit
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.suppressQuit = false

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
		if m.state == stateLoading || m.state == stateGatheringTabs || m.state == stateImporting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case articleExtractedMsg:
		// Discard results from cancelled fetches.
		if msg.gen != m.fetchGen {
			return m, nil
		}
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

	case safariOpenedMsg:
		if msg.err != nil {
			m.state = stateList
			m.err = fmt.Errorf("opening Safari: %w", msg.err)
			m.safariURL = ""
			m.safariWindow = nil
			return m, nil
		}
		m.safariWindow = msg.window
		// Stay in stateSafariWaiting — user will press Enter when ready.
		return m, nil

	case safariHTMLExtractedMsg:
		if msg.err != nil {
			m.state = stateList
			m.err = msg.err
			return m, nil
		}
		m.state = stateLoading
		m.fetchGen++
		return m, tea.Batch(
			m.spinner.Tick,
			m.extractArticleFromHTML(msg.url, msg.html),
		)

	case extractionErrMsg:
		if msg.gen != m.fetchGen {
			return m, nil
		}
		m.state = stateList
		m.err = msg.err
		m.statusMsg = ""
		return m, nil

	case articleDeletedMsg:
		m.refreshArticles()
		m.statusMsg = "Article deleted"
		return m, nil

	case editorFinishedMsg:
		m.tmuxPaneID = ""
		if msg.err != nil {
			m.err = msg.err
		}
		m.savePositionFromFile()
		// Reload index to pick up any manual edits to markdown metadata.
		if err := m.store.Reload(); err != nil {
			m.err = err
		}
		m.refreshArticles()
		return m, nil

	case safariTabsGatheredMsg:
		return m.handleSafariTabsGathered(msg)

	case importEditorFinishedMsg:
		return m.handleImportEditorFinished(msg)

	case importArticleResultMsg:
		return m.handleImportArticleResult(msg)

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
		m.articles = m.applyArchiveFilter(m.store.Search(m.searchInput.Value()))
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
	case stateLoading, stateGatheringTabs:
		// Allow quit or cancel during loading
		if key.Matches(msg, m.keys.Quit) || key.Matches(msg, m.keys.Cancel) || msg.String() == "ctrl+c" {
			m.state = stateList
			m.suppressQuit = true
			m.fetchGen++ // invalidate in-flight results
			return m, nil
		}
		return m, nil
	case stateSafariWaiting:
		switch {
		case key.Matches(msg, m.keys.Submit): // Enter
			m.state = stateLoading
			return m, tea.Batch(m.spinner.Tick, m.extractSafariHTML())
		case key.Matches(msg, m.keys.Cancel), key.Matches(msg, m.keys.Quit), msg.String() == "ctrl+c":
			if m.safariWindow != nil {
				_ = m.safariWindow.Close()
			}
			m.state = stateList
			m.suppressQuit = true
			m.overwritePath = ""
			m.overwriteTitle = ""
			m.safariURL = ""
			m.safariWindow = nil
			return m, nil
		}
		return m, nil
	case stateImporting:
		// Cancel stops remaining imports but keeps already-saved articles.
		if key.Matches(msg, m.keys.Cancel) || key.Matches(msg, m.keys.Quit) || msg.String() == "ctrl+c" {
			m.importQueue = nil
			m.state = stateList
			m.suppressQuit = true
			m.refreshArticles()
			m.statusMsg = m.importSummary() + " (cancelled)"
			return m, nil
		}
		return m, nil
	case stateConfirmOverwrite:
		return m.handleConfirmOverwriteKeys(msg)
	case stateConfirmDelete:
		return m.handleConfirmDeleteKeys(msg)
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
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Focus()
		return m, cmd

	case key.Matches(msg, m.keys.Import):
		m.state = stateGatheringTabs
		m.err = nil
		return m, tea.Batch(m.spinner.Tick, gatherSafariTabs())

	case key.Matches(msg, m.keys.Delete):
		if len(m.articles) == 0 || m.cursor >= len(m.articles) {
			return m, nil
		}
		article := m.articles[m.cursor]
		m.pendingDeletePath = article.FilePath
		m.pendingDeleteTitle = article.Title
		m.state = stateConfirmDelete
		return m, nil

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
		m.urlInput = m.urlInput.SetValue(article.SourceURL).Blur()
		m.overwritePath = article.FilePath
		m.overwriteTitle = article.Title
		m.state = stateLoading
		m.fetchGen++
		return m, tea.Batch(
			m.spinner.Tick,
			m.extractArticle(article.SourceURL),
		)

	case key.Matches(msg, m.keys.SafariReload):
		if len(m.articles) == 0 || m.cursor >= len(m.articles) {
			return m, nil
		}
		article := m.articles[m.cursor]
		if article.SourceURL == "" {
			m.err = fmt.Errorf("no source URL for %q", article.Title)
			return m, nil
		}
		m.overwritePath = article.FilePath
		m.overwriteTitle = article.Title
		m.safariURL = article.SourceURL
		m.urlInput = m.urlInput.SetValue(article.SourceURL).Blur()
		m.state = stateSafariWaiting
		return m, m.openInSafari(article.SourceURL)
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
		rawURL := strings.TrimSpace(m.urlInput.Value())
		if rawURL == "" {
			m.state = stateList
			return m, nil
		}
		// Validate URL format before sending to the server.
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			rawURL = "https://" + rawURL
			m.urlInput = m.urlInput.SetValue(rawURL)
		}
		if u, err := neturl.Parse(rawURL); err != nil || u.Host == "" || !strings.Contains(u.Host, ".") {
			m.err = fmt.Errorf("invalid URL: %s", rawURL)
			m.state = stateList
			return m, nil
		}
		url := rawURL
		m.urlInput = m.urlInput.Blur()
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
		m.fetchGen++
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
	m.articles = m.applyArchiveFilter(m.store.Search(m.searchInput.Value()))
	if m.cursor >= len(m.articles) {
		m.cursor = max(0, len(m.articles)-1)
	}
	return m, cmd
}

func (m Model) extractArticle(url string) tea.Cmd {
	gen := m.fetchGen
	return func() tea.Msg {
		result, err := m.extract.Extract(url)
		if err != nil {
			return extractionErrMsg{err: err, gen: gen}
		}
		return articleExtractedMsg{result: result, gen: gen}
	}
}

func (m Model) extractArticleFromHTML(url, html string) tea.Cmd {
	gen := m.fetchGen
	return func() tea.Msg {
		result, err := m.extract.ExtractFromHTML(url, html)
		if err != nil {
			return extractionErrMsg{err: err, gen: gen}
		}
		return articleExtractedMsg{result: result, gen: gen}
	}
}

func (m Model) openInSafari(url string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(750 * time.Millisecond) // Let TUI render before Safari steals focus.
		w, err := safari.OpenURL(url)
		return safariOpenedMsg{window: w, err: err}
	}
}

func (m Model) extractSafariHTML() tea.Cmd {
	url := m.safariURL
	w := m.safariWindow
	return func() tea.Msg {
		html, err := w.TabSource()
		if err != nil {
			return safariHTMLExtractedMsg{url: url, err: fmt.Errorf("extracting HTML from Safari: %w", err)}
		}
		if strings.TrimSpace(html) == "" {
			return safariHTMLExtractedMsg{url: url, err: fmt.Errorf("Safari returned empty HTML")}
		}
		_ = w.Close()
		return safariHTMLExtractedMsg{url: url, html: html}
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
		m.fetchGen++
		return m, tea.Batch(
			m.spinner.Tick,
			m.extractArticle(url),
		)
	case "n", "N", "esc", "ctrl+c":
		m.state = stateList
		m.suppressQuit = true
		m.pendingResult = nil
		m.overwritePath = ""
		m.overwriteTitle = ""
		return m, nil
	}
	return m, nil
}


func (m Model) handleConfirmDeleteKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		path := m.pendingDeletePath
		m.pendingDeletePath = ""
		m.pendingDeleteTitle = ""
		m.state = stateList
		if err := m.store.Delete(path); err != nil {
			m.err = err
			return m, nil
		}
		return m, func() tea.Msg {
			return articleDeletedMsg{id: path}
		}
	case "n", "N", "esc", "ctrl+c":
		m.state = stateList
		m.suppressQuit = true
		m.pendingDeletePath = ""
		m.pendingDeleteTitle = ""
		return m, nil
	}
	return m, nil
}

func inTmux() bool {
	return os.Getenv("TMUX") != ""
}

func tmuxPaneAlive(paneID string) bool {
	return exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{pane_id}").Run() == nil
}

func isVimEditor(editor string) bool {
	base := filepath.Base(editor)
	return base == "vim" || base == "nvim"
}

// vimEditorCommand builds a shell command string for vim/nvim that:
// - Opens the file at the saved progress line (if any)
// - Sets a VimLeave autocmd to write the final cursor position to posFile
func vimEditorCommand(editor, fpath, posFile string, progress int) string {
	startArg := ""
	if progress > 0 {
		startArg = fmt.Sprintf("+%d ", progress)
	}
	// The autocmd writes "absolutePath:lineNum" to posFile on VimLeave.
	autocmd := fmt.Sprintf(
		`au VimLeave * call writefile([expand('%%:p') . ':' . line('.')], '%s')`,
		posFile,
	)
	return fmt.Sprintf(`%s %s-c "%s" %q`, editor, startArg, autocmd, fpath)
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

	if !inTmux() {
		return m.openArticleExecProcess(editor, fpath, article.Progress)
	}

	// Clean up stale pane ID if pane is dead.
	if m.tmuxPaneID != "" && !tmuxPaneAlive(m.tmuxPaneID) {
		m.tmuxPaneID = ""
	}

	// Tmux: reuse existing pane if alive and editor is vim/nvim.
	if m.tmuxPaneID != "" {
		if isVimEditor(editor) {
			// Save the current file's cursor position before switching.
			saveCmd := fmt.Sprintf(
				`:call writefile([expand('%%:p') . ':' . line('.')], '%s')`,
				m.positionFile,
			)
			_ = exec.Command("tmux", "send-keys", "-t", m.tmuxPaneID, saveCmd, "Enter").Run()
			time.Sleep(50 * time.Millisecond)
			m.savePositionFromFile()

			// Send :e command to switch files in the existing editor.
			// Use +LINE to restore saved position.
			eCmd := fmt.Sprintf(":e %s", fpath)
			if article.Progress > 0 {
				eCmd = fmt.Sprintf(":e +%d %s", article.Progress, fpath)
			}
			cmd := exec.Command("tmux", "send-keys", "-t", m.tmuxPaneID,
				eCmd, "Enter")
			if err := cmd.Run(); err != nil {
				// send-keys failed (pane might have just died), clear ID and fall through.
				m.tmuxPaneID = ""
			} else {
				return m, nil
			}
		}
	}

	// Tmux: open a new split pane.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	channel := fmt.Sprintf("shelf-editor-done-%d", os.Getpid())

	editorCmd := fmt.Sprintf("%s %q", editor, fpath)
	if isVimEditor(editor) {
		editorCmd = vimEditorCommand(editor, fpath, m.positionFile, article.Progress)
	}
	splitCmd := exec.Command("tmux", "split-window", "-h", "-l", "63%",
		"-P", "-F", "#{pane_id}",
		shell, "-l", "-c",
		fmt.Sprintf("%s; tmux wait-for -S %s", editorCmd, channel))
	out, err := splitCmd.Output()
	if err != nil {
		m.err = fmt.Errorf("tmux split-window: %w", err)
		return m, nil
	}
	m.tmuxPaneID = strings.TrimSpace(string(out))

	// Block in background until the editor exits.
	return m, func() tea.Msg {
		err := exec.Command("tmux", "wait-for", channel).Run()
		return editorFinishedMsg{err: err}
	}
}

func (m Model) openArticleExecProcess(editor, fpath string, progress int) (tea.Model, tea.Cmd) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	editorCmd := fmt.Sprintf("%s %q", editor, fpath)
	if isVimEditor(editor) {
		editorCmd = vimEditorCommand(editor, fpath, m.positionFile, progress)
	}
	c := exec.Command(shell, "-l", "-c", editorCmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

// savePositionFromFile reads the vim cursor position file, updates the
// article's progress in the store, and refreshes the article list.
func (m *Model) savePositionFromFile() {
	data, err := os.ReadFile(m.positionFile)
	if err != nil {
		return
	}
	os.Remove(m.positionFile)
	// Format: absolutePath:lineNum
	parts := strings.SplitN(strings.TrimSpace(string(data)), ":", 2)
	if len(parts) != 2 {
		return
	}
	lineNum, err := strconv.Atoi(parts[1])
	if err != nil || lineNum <= 0 {
		return
	}
	absPath := parts[0]
	for _, a := range m.store.List() {
		if m.store.GetFilePath(a.FilePath) == absPath {
			_ = m.store.UpdateProgress(a.FilePath, lineNum)
			break
		}
	}
	m.refreshArticles()
}

func (m *Model) refreshArticles() {
	if m.searchInput.Value() != "" {
		m.articles = m.applyArchiveFilter(m.store.Search(m.searchInput.Value()))
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
	// Move cursor to the article's new position in the list.
	for i, a := range m.articles {
		if a.FilePath == article.FilePath {
			m.cursor = i
			break
		}
	}
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
	if m.state != stateAddURL && m.state != stateLoading && m.state != stateConfirmOverwrite && m.state != stateConfirmDelete && m.state != stateGatheringTabs && m.state != stateImporting && m.state != stateSafariWaiting {
		if m.searchInput.Value() != "" {
			total := len(m.applyArchiveFilter(m.store.List()))
			if filtered == 0 {
				sb.WriteString(m.styles.Muted.Render(fmt.Sprintf(" (0 of %d)", total)))
			} else {
				sb.WriteString(m.styles.Muted.Render(fmt.Sprintf(" (%d of %d of %d)", m.cursor+1, filtered, total)))
			}
		} else {
			sb.WriteString(m.styles.Muted.Render(fmt.Sprintf(" (%d of %d)", m.cursor+1, filtered)))
		}
	}
	sb.WriteString("\n\n")

	// Search bar (replaced by URL input when adding/loading)
	switch m.state {
	case stateAddURL, stateLoading, stateConfirmOverwrite, stateSafariWaiting:
		sb.WriteString(m.urlInput.View())
	case stateGatheringTabs, stateImporting:
		// No input bar during import workflow.
	default:
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
	case stateConfirmDelete:
		sb.WriteString(fmt.Sprintf("Delete %q? This cannot be undone. [y/n]", m.pendingDeleteTitle))
	case stateConfirmOverwrite:
		if m.pendingResult != nil {
			sb.WriteString(fmt.Sprintf("Article %q already exists. Overwrite? [y/n]", m.pendingResult.Title))
		} else {
			sb.WriteString(fmt.Sprintf("Already saved as %q. Re-fetch? [y/n]", m.overwriteTitle))
		}
	case stateSafariWaiting:
		sb.WriteString("Safari opened — complete any verification, then press Enter...")
	case stateGatheringTabs:
		sb.WriteString(m.spinner.View())
		sb.WriteString(" Gathering Safari tabs...")
	case stateImporting:
		sb.WriteString(m.spinner.View())
		saved := m.importDone - m.importSkipped - len(m.importErrors)
		sb.WriteString(fmt.Sprintf(" Importing %d/%d...", m.importDone+1, m.importTotal))
		if saved > 0 || m.importSkipped > 0 {
			details := []string{}
			if saved > 0 {
				details = append(details, fmt.Sprintf("%d saved", saved))
			}
			if m.importSkipped > 0 {
				details = append(details, fmt.Sprintf("%d skipped", m.importSkipped))
			}
			if len(m.importErrors) > 0 {
				details = append(details, fmt.Sprintf("%d failed", len(m.importErrors)))
			}
			sb.WriteString(" " + strings.Join(details, ", "))
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
				sep := strings.Repeat("─", m.width-4)
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
	case stateLoading:
		parts = append(parts, "[esc] cancel")
	case stateConfirmDelete:
		parts = append(parts, "[y] delete", "[n] cancel")
	case stateConfirmOverwrite:
		parts = append(parts, "[y] overwrite", "[n] cancel")
	case stateSafariWaiting:
		parts = append(parts, "[enter] extract", "[esc] cancel")
	case stateGatheringTabs:
		parts = append(parts, "[esc] cancel")
	case stateImporting:
		parts = append(parts, "[esc] cancel")
	default:
		archiveLabel := "[x/X] archive/show"
		if len(m.articles) > 0 && m.cursor < len(m.articles) && m.articles[m.cursor].IsArchived() {
			archiveLabel = "[x/X] unarchive/hide"
		}
		if m.showArchived {
			archiveLabel = "[x/X] archive/hide"
			if len(m.articles) > 0 && m.cursor < len(m.articles) && m.articles[m.cursor].IsArchived() {
				archiveLabel = "[x/X] unarchive/hide"
			}
		}
		parts = append(parts,
			"[a]dd URL",
			"[i]mport",
			"[enter] open",
			"[d]elete",
			archiveLabel,
			"[/] search",
			"[r/R]efetch",
			"[q]uit",
		)
	}

	usable := m.width - 4 // account for App padding
	sep := "  "
	result := strings.Join(parts, sep)
	if len(result) > usable && usable > 0 {
		// Try single-space separator first.
		sep = " "
		result = strings.Join(parts, sep)
	}
	if len(result) > usable && usable > 0 {
		// Drop items from the end (least important) until it fits,
		// but always keep the last item (quit/cancel).
		for len(parts) > 2 {
			parts = append(parts[:len(parts)-2], parts[len(parts)-1])
			result = strings.Join(parts, sep)
			if len(result) <= usable {
				break
			}
		}
	}
	return result
}

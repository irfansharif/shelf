package tui

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/irfansharif/shelf/pkg/safari"
	"github.com/irfansharif/shelf/pkg/storage"
)

// Messages for the import workflow.
type (
	safariTabsGatheredMsg struct {
		tabs     map[string][]safari.Tab
		warnings []error
	}
	importEditorFinishedMsg struct {
		tmpPath string
		err     error
	}
	importArticleResultMsg struct {
		url     string
		title   string
		skipped bool
		err     error
	}
)

// gatherSafariTabs returns a command that collects tabs from Safari.
func gatherSafariTabs() tea.Cmd {
	return func() tea.Msg {
		tabs, warnings := safari.GatherTabs()
		return safariTabsGatheredMsg{tabs: tabs, warnings: warnings}
	}
}

// sourceLabel maps source keys to display names for the import file headers.
var sourceLabel = map[string]string{
	"icloud":      "iCloud Tabs",
	"local":       "Local Tabs",
	"readinglist": "Reading List",
}

// sourceOrder defines the iteration order for sources in the import file.
var sourceOrder = []string{"local", "icloud", "readinglist"}

// formatImportFile generates the temp file content for the editor buffer.
// All URLs are commented out by default; the user uncomments the ones they
// want to import. Tabs are grouped first by source (iCloud, Local, Reading
// List) with level-1 fold markers, then by domain with level-2 fold markers.
// Within each domain, tabs are sorted by LastViewed descending; domain groups
// are sorted by their most recent tab's LastViewed (descending), with an
// alphabetical tiebreaker.
func formatImportFile(tabsBySource map[string][]safari.Tab, savedURLs map[string]bool, warnings []error) string {
	var sb strings.Builder
	sb.WriteString("# Safari Import — uncomment URLs to import, then :wq\n")
	sb.WriteString("# Use zo/zc to unfold/fold groups, zR to open all.\n")
	sb.WriteString("#\n")

	for _, w := range warnings {
		sb.WriteString(fmt.Sprintf("# Warning: %s\n", w.Error()))
	}
	if len(warnings) > 0 {
		sb.WriteString("#\n")
	}

	for _, source := range sourceOrder {
		tabs := tabsBySource[source]
		if len(tabs) == 0 {
			continue
		}

		label := sourceLabel[source]
		// Level-1 fold: source group.
		sb.WriteString(fmt.Sprintf("\n# === %s (%d) === %s\n", label, len(tabs), "{"+"{"+"{1"))

		// Group tabs by domain.
		domainTabs := make(map[string][]safari.Tab)
		for _, t := range tabs {
			domain := extractDomain(t.URL)
			domainTabs[domain] = append(domainTabs[domain], t)
		}

		// Sort tabs within each domain by LastViewed descending.
		for d := range domainTabs {
			sort.Slice(domainTabs[d], func(i, j int) bool {
				return domainTabs[d][i].LastViewed.After(domainTabs[d][j].LastViewed)
			})
		}

		// Sort domains by most recent tab's LastViewed (descending),
		// alphabetical tiebreaker.
		var domains []string
		for d := range domainTabs {
			domains = append(domains, d)
		}
		domainMaxTime := make(map[string]time.Time)
		for d, dt := range domainTabs {
			var maxT time.Time
			for _, t := range dt {
				if t.LastViewed.After(maxT) {
					maxT = t.LastViewed
				}
			}
			domainMaxTime[d] = maxT
		}
		sort.Slice(domains, func(i, j int) bool {
			ti, tj := domainMaxTime[domains[i]], domainMaxTime[domains[j]]
			if !ti.Equal(tj) {
				return ti.After(tj)
			}
			return domains[i] < domains[j]
		})

		for _, domain := range domains {
			dt := domainTabs[domain]
			// Level-2 fold: domain group.
			sb.WriteString(fmt.Sprintf("\n# --- %s (%d) --- %s\n", domain, len(dt), "{"+"{"+"{2"))
			for i, t := range dt {
				if i > 0 {
					sb.WriteString("\n")
				}
				title := t.Title
				if title == "" {
					title = t.URL
				}
				if savedURLs[t.URL] {
					sb.WriteString(fmt.Sprintf("\t# %s [already saved]\n", title))
				} else {
					sb.WriteString(fmt.Sprintf("\t# %s\n", title))
				}
				sb.WriteString(fmt.Sprintf("\t# %s\n", t.URL))
			}
			// Close level-2 fold.
			sb.WriteString("# " + "}" + "}" + "}2\n")
		}

		// Close level-1 fold.
		sb.WriteString("# " + "}" + "}" + "}1\n")
	}

	// Vim modeline: conf filetype for # comment highlighting,
	// marker folding for explicit fold regions, start fully folded.
	sb.WriteString("\n# vim: ft=conf foldmethod=marker foldlevel=0\n")

	return sb.String()
}

// extractDomain returns the hostname from a URL, stripping "www." prefix.
func extractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "other"
	}
	host := parsed.Hostname()
	host = strings.TrimPrefix(host, "www.")
	return host
}

// parseImportFile reads the edited temp file and returns URLs to import.
func parseImportFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading import file: %w", err)
	}

	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, nil
}

// handleSafariTabsGathered processes gathered Safari tabs: writes the temp
// file and opens it in the user's editor.
func (m Model) handleSafariTabsGathered(msg safariTabsGatheredMsg) (tea.Model, tea.Cmd) {
	totalTabs := 0
	for _, tabs := range msg.tabs {
		totalTabs += len(tabs)
	}

	if totalTabs == 0 && len(msg.warnings) > 0 {
		m.state = stateList
		m.err = fmt.Errorf("no Safari tabs found: %s", msg.warnings[0].Error())
		return m, nil
	}
	if totalTabs == 0 {
		m.state = stateList
		m.statusMsg = "No Safari tabs found"
		return m, nil
	}

	// Build set of already-saved URLs.
	savedURLs := make(map[string]bool)
	for _, a := range m.store.List() {
		if a.SourceURL != "" {
			savedURLs[a.SourceURL] = true
		}
	}

	content := formatImportFile(msg.tabs, savedURLs, msg.warnings)

	// Write temp file.
	tmpFile, err := os.CreateTemp("", "shelf-import-*.txt")
	if err != nil {
		m.state = stateList
		m.err = fmt.Errorf("creating temp file: %w", err)
		return m, nil
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		m.state = stateList
		m.err = fmt.Errorf("writing temp file: %w", err)
		return m, nil
	}
	tmpFile.Close()

	// Open editor.
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	c := exec.Command(shell, "-l", "-c", fmt.Sprintf("%s %q", editor, tmpPath))
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return importEditorFinishedMsg{tmpPath: tmpPath, err: err}
	})
}

// handleImportEditorFinished parses the edited file and starts batch import.
func (m Model) handleImportEditorFinished(msg importEditorFinishedMsg) (tea.Model, tea.Cmd) {
	defer os.Remove(msg.tmpPath)

	if msg.err != nil {
		m.state = stateList
		m.err = fmt.Errorf("editor: %w", msg.err)
		return m, nil
	}

	urls, err := parseImportFile(msg.tmpPath)
	if err != nil {
		m.state = stateList
		m.err = err
		return m, nil
	}

	if len(urls) == 0 {
		m.state = stateList
		m.statusMsg = "No URLs to import"
		return m, nil
	}

	m.importQueue = urls
	m.importTotal = len(urls)
	m.importDone = 0
	m.importSkipped = 0
	m.importErrors = nil
	m.state = stateImporting
	return m, tea.Batch(m.spinner.Tick, m.importExtractAndSave(urls[0]))
}

// importExtractAndSave extracts an article and saves it in a single command.
// Duplicates (slug collisions) are silently skipped.
func (m Model) importExtractAndSave(url string) tea.Cmd {
	ext := m.extract
	store := m.store
	return func() tea.Msg {
		result, err := ext.Extract(url)
		if err != nil {
			return importArticleResultMsg{url: url, err: err}
		}

		images := make([]storage.ImageFile, len(result.Images))
		for i, img := range result.Images {
			images[i] = storage.ImageFile{Path: img.Path, Data: img.Data}
		}

		err = store.SaveContent(result.Title, result.Content, images)
		if err != nil {
			var existsErr *storage.ErrArticleExists
			if errors.As(err, &existsErr) {
				return importArticleResultMsg{url: url, title: result.Title, skipped: true}
			}
			return importArticleResultMsg{url: url, title: result.Title, err: err}
		}

		return importArticleResultMsg{url: url, title: result.Title}
	}
}

// handleImportArticleResult processes the result of a single import and
// advances the queue or finishes.
func (m Model) handleImportArticleResult(msg importArticleResultMsg) (tea.Model, tea.Cmd) {
	// Advance the queue.
	if len(m.importQueue) > 0 {
		m.importQueue = m.importQueue[1:]
	}

	if msg.err != nil {
		m.importErrors = append(m.importErrors, fmt.Sprintf("%s: %s", msg.url, msg.err.Error()))
	} else if msg.skipped {
		m.importSkipped++
	}

	m.importDone++

	if len(m.importQueue) == 0 {
		m.state = stateList
		m.refreshArticles()
		m.statusMsg = m.importSummary()
		return m, nil
	}

	return m, m.importExtractAndSave(m.importQueue[0])
}

// importSummary returns a human-readable summary of the batch import.
func (m Model) importSummary() string {
	saved := m.importDone - m.importSkipped - len(m.importErrors)
	parts := []string{fmt.Sprintf("Import complete: %d saved", saved)}
	if m.importSkipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", m.importSkipped))
	}
	if len(m.importErrors) > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", len(m.importErrors)))
	}
	return strings.Join(parts, ", ")
}

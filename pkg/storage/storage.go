package storage

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/irfansharif/browser/pkg/images"
)

// Article represents a saved article with its content.
type Article struct {
	Meta    ArticleMeta
	Content string
}

// ArticleMeta represents article metadata parsed from markdown front matter.
type ArticleMeta struct {
	Title        string
	Author       string
	SourceURL    string
	SourceDomain string    // derived from SourceURL
	SavedAt      time.Time
	FilePath     string // relative path, derived from disk
	FileSize     int64  // derived from os.Stat
}

// Store manages article storage.
type Store struct {
	basePath string
	articles []ArticleMeta // cached from scanning articles/ dir
}

// New creates a new Store at the given base path.
func New(basePath string) (*Store, error) {
	s := &Store{basePath: basePath}

	// Ensure directories exist
	articlesDir := filepath.Join(basePath, "articles")
	if err := os.MkdirAll(articlesDir, 0755); err != nil {
		return nil, fmt.Errorf("creating articles directory: %w", err)
	}

	// Scan existing articles
	if err := s.scan(); err != nil {
		return nil, fmt.Errorf("scanning articles: %w", err)
	}

	return s, nil
}

func (s *Store) scan() error {
	articlesDir := filepath.Join(s.basePath, "articles")
	entries, err := os.ReadDir(articlesDir)
	if err != nil {
		return err
	}

	s.articles = nil
	for _, entry := range entries {
		if entry.IsDir() {
			// Directory format: look for index.md inside.
			indexPath := filepath.Join(articlesDir, entry.Name(), "index.md")
			content, err := os.ReadFile(indexPath)
			if err != nil {
				continue
			}

			title, author, source, saved, _, err := parseFrontMatter(string(content))
			if err != nil {
				continue
			}

			relPath := filepath.Join("articles", entry.Name(), "index.md")
			dirPath := filepath.Join(articlesDir, entry.Name())

			meta := ArticleMeta{
				Title:     title,
				Author:    author,
				SourceURL: source,
				SavedAt:   saved,
				FilePath:  relPath,
				FileSize:  calcDirSize(dirPath),
			}
			if source != "" {
				if parsed, err := url.Parse(source); err == nil {
					meta.SourceDomain = parsed.Host
				}
			}
			s.articles = append(s.articles, meta)
		} else if strings.HasSuffix(entry.Name(), ".md") {
			// Flat file format (backward compat).
			relPath := filepath.Join("articles", entry.Name())
			fullPath := filepath.Join(s.basePath, relPath)

			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			title, author, source, saved, _, err := parseFrontMatter(string(content))
			if err != nil {
				continue
			}

			meta := ArticleMeta{
				Title:     title,
				Author:    author,
				SourceURL: source,
				SavedAt:   saved,
				FilePath:  relPath,
				FileSize:  info.Size(),
			}
			if source != "" {
				if parsed, err := url.Parse(source); err == nil {
					meta.SourceDomain = parsed.Host
				}
			}
			s.articles = append(s.articles, meta)
		}
	}

	sort.Slice(s.articles, func(i, j int) bool {
		return s.articles[i].SavedAt.After(s.articles[j].SavedAt)
	})

	return nil
}

// Save stores an article and updates the cache.
func (s *Store) Save(article *Article) error {
	if article.Meta.SavedAt.IsZero() {
		article.Meta.SavedAt = time.Now()
	}

	slug := generateDirName(article.Meta.SavedAt, article.Meta.Title)
	dirPath := filepath.Join(s.basePath, "articles", slug)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("creating article directory: %w", err)
	}

	// Download images and rewrite markdown references.
	imagesDir := filepath.Join(dirPath, "images")
	article.Content = images.DownloadAndRewrite(article.Content, imagesDir)

	article.Meta.FilePath = filepath.Join("articles", slug, "index.md")
	content := formatMarkdown(article)

	indexPath := filepath.Join(dirPath, "index.md")
	if err := os.WriteFile(indexPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing article file: %w", err)
	}

	return s.scan()
}

// List returns all article metadata, sorted by saved date (newest first).
func (s *Store) List() []ArticleMeta {
	result := make([]ArticleMeta, len(s.articles))
	copy(result, s.articles)
	return result
}

// Get retrieves an article by its relative file path.
func (s *Store) Get(filePath string) (*Article, error) {
	fullPath := filepath.Join(s.basePath, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading article file: %w", err)
	}

	title, author, source, saved, body, err := parseFrontMatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}

	meta := ArticleMeta{
		Title:     title,
		Author:    author,
		SourceURL: source,
		SavedAt:   saved,
		FilePath:  filePath,
	}
	if source != "" {
		if parsed, err := url.Parse(source); err == nil {
			meta.SourceDomain = parsed.Host
		}
	}
	if info, err := os.Stat(fullPath); err == nil {
		meta.FileSize = info.Size()
	}

	return &Article{
		Meta:    meta,
		Content: body,
	}, nil
}

// GetFilePath returns the full file path for an article given its relative path.
func (s *Store) GetFilePath(relPath string) string {
	return filepath.Join(s.basePath, relPath)
}

// Delete removes an article by its relative file path.
func (s *Store) Delete(filePath string) error {
	fullPath := filepath.Join(s.basePath, filePath)

	// Directory format: remove the entire article directory.
	if strings.HasSuffix(filePath, "/index.md") || strings.HasSuffix(filePath, string(filepath.Separator)+"index.md") {
		dirPath := filepath.Dir(fullPath)
		if err := os.RemoveAll(dirPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing article directory: %w", err)
		}
	} else {
		// Flat file format.
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing article file: %w", err)
		}
	}

	return s.scan()
}

// Search filters articles by query (matches title, author, or domain).
func (s *Store) Search(query string) []ArticleMeta {
	if query == "" {
		return s.List()
	}

	query = strings.ToLower(query)
	var results []ArticleMeta

	for _, meta := range s.articles {
		if strings.Contains(strings.ToLower(meta.Title), query) ||
			strings.Contains(strings.ToLower(meta.Author), query) ||
			strings.Contains(strings.ToLower(meta.SourceDomain), query) {
			results = append(results, meta)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].SavedAt.After(results[j].SavedAt)
	})

	return results
}

// Count returns the total number of articles.
func (s *Store) Count() int {
	return len(s.articles)
}

func generateDirName(savedAt time.Time, title string) string {
	date := savedAt.Format("2006-01-02")
	slug := slugify(title)
	return fmt.Sprintf("%s-%s", date, slug)
}

func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces and special chars with hyphens
	var result strings.Builder
	lastWasHyphen := false

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			lastWasHyphen = false
		} else if !lastWasHyphen {
			result.WriteRune('-')
			lastWasHyphen = true
		}
	}

	slug := strings.Trim(result.String(), "-")

	// Limit length
	if len(slug) > 60 {
		slug = slug[:60]
		// Don't end on a hyphen
		slug = strings.TrimRight(slug, "-")
	}

	// Remove multiple consecutive hyphens
	re := regexp.MustCompile(`-+`)
	slug = re.ReplaceAllString(slug, "-")

	if slug == "" {
		slug = "untitled"
	}

	return slug
}

func formatMarkdown(article *Article) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("title: %s\n", escapeYAML(article.Meta.Title)))
	if article.Meta.Author != "" {
		sb.WriteString(fmt.Sprintf("author: %s\n", escapeYAML(article.Meta.Author)))
	}
	sb.WriteString(fmt.Sprintf("source: %s\n", article.Meta.SourceURL))
	sb.WriteString(fmt.Sprintf("saved: %s\n", article.Meta.SavedAt.Format(time.RFC3339)))
	sb.WriteString("---\n\n")
	sb.WriteString(article.Content)

	return sb.String()
}

func escapeYAML(s string) string {
	// If the string contains special characters, quote it
	if strings.ContainsAny(s, ":#{}[]&*!|>'\"%@`") || strings.HasPrefix(s, "-") {
		s = strings.ReplaceAll(s, `"`, `\"`)
		return `"` + s + `"`
	}
	return s
}

func parseFrontMatter(content string) (title, author, source string, saved time.Time, body string, err error) {
	// Front matter is delimited by "---\n" at start and "---\n" to close.
	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) < 3 || parts[0] != "" {
		return "", "", "", time.Time{}, content, nil
	}

	header := parts[1]
	body = strings.TrimPrefix(parts[2], "\n")

	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ": ")
		if idx == -1 {
			continue
		}
		key := line[:idx]
		value := line[idx+2:]
		value = unescapeYAML(value)

		switch key {
		case "title":
			title = value
		case "author":
			author = value
		case "source":
			source = value
		case "saved":
			saved, err = time.Parse(time.RFC3339, value)
			if err != nil {
				return "", "", "", time.Time{}, "", fmt.Errorf("parsing saved time: %w", err)
			}
		}
	}

	return
}

func unescapeYAML(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, `\"`, `"`)
	}
	return s
}

func calcDirSize(dir string) int64 {
	var size int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}


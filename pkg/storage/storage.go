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
)

var multiHyphenRe = regexp.MustCompile(`-+`)

// ErrArticleExists is returned when saving an article whose slug already exists.
type ErrArticleExists struct {
	Slug  string
	Title string // title of the existing article
}

func (e *ErrArticleExists) Error() string {
	return fmt.Sprintf("article already exists: %s", e.Slug)
}

// Article represents a saved article with its content (used for the read path).
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
	Tags         []string  // optional comma-separated tags
	FilePath     string    // relative path, derived from disk
	FileSize     int64     // derived from os.Stat
}

// IsArchived returns true if the article has the "archived" tag.
func (m ArticleMeta) IsArchived() bool {
	return hasTag(m.Tags, "archived")
}

// ImageFile holds image data to be written to disk.
type ImageFile struct {
	Path string // relative path, e.g. "images/photo.jpg"
	Data []byte
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

			title, author, source, saved, tags, _, err := parseFrontMatter(string(content))
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
				Tags:      tags,
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

			title, author, source, saved, tags, _, err := parseFrontMatter(string(content))
			if err != nil {
				continue
			}

			meta := ArticleMeta{
				Title:     title,
				Author:    author,
				SourceURL: source,
				SavedAt:   saved,
				Tags:      tags,
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
		ai, aj := s.articles[i].IsArchived(), s.articles[j].IsArchived()
		if ai != aj {
			return !ai // non-archived first
		}
		return s.articles[i].SavedAt.After(s.articles[j].SavedAt)
	})

	return nil
}

// SaveContent stores article content and images. Content is the complete
// index.md file (front matter + markdown). If an article with the same slug
// already exists, it returns *ErrArticleExists. Use SaveContentForce to
// overwrite.
func (s *Store) SaveContent(title, content string, images []ImageFile) error {
	slug := generateDirName(title)
	dirPath := filepath.Join(s.basePath, "articles", slug)

	if _, err := os.Stat(dirPath); err == nil {
		// Directory already exists — find the title of the existing article.
		existingTitle := slug
		if data, err := os.ReadFile(filepath.Join(dirPath, "index.md")); err == nil {
			if t, _, _, _, _, _, err := parseFrontMatter(string(data)); err == nil && t != "" {
				existingTitle = t
			}
		}
		return &ErrArticleExists{Slug: slug, Title: existingTitle}
	}

	return s.saveContent(slug, dirPath, content, images)
}

// SaveContentForce stores article content and images, overwriting any existing
// article with the same slug.
func (s *Store) SaveContentForce(title, content string, images []ImageFile) error {
	slug := generateDirName(title)
	dirPath := filepath.Join(s.basePath, "articles", slug)
	return s.saveContent(slug, dirPath, content, images)
}

func (s *Store) saveContent(slug, dirPath, content string, images []ImageFile) error {
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("creating article directory: %w", err)
	}

	// Write images.
	for _, img := range images {
		imgPath := filepath.Join(dirPath, img.Path)
		if err := os.MkdirAll(filepath.Dir(imgPath), 0755); err != nil {
			return fmt.Errorf("creating image directory: %w", err)
		}
		if err := os.WriteFile(imgPath, img.Data, 0644); err != nil {
			return fmt.Errorf("writing image %s: %w", img.Path, err)
		}
	}

	// Write index.md.
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

	title, author, source, saved, tags, body, err := parseFrontMatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}

	meta := ArticleMeta{
		Title:     title,
		Author:    author,
		SourceURL: source,
		SavedAt:   saved,
		Tags:      tags,
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
			strings.Contains(strings.ToLower(meta.SourceDomain), query) ||
			strings.Contains(strings.ToLower(strings.Join(meta.Tags, ",")), query) {
			results = append(results, meta)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		ai, aj := results[i].IsArchived(), results[j].IsArchived()
		if ai != aj {
			return !ai // non-archived first
		}
		return results[i].SavedAt.After(results[j].SavedAt)
	})

	return results
}

// Reload rescans the articles directory and refreshes the cache.
func (s *Store) Reload() error {
	return s.scan()
}

// Count returns the total number of articles.
func (s *Store) Count() int {
	return len(s.articles)
}

func generateDirName(title string) string {
	return slugify(title)
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
	slug = multiHyphenRe.ReplaceAllString(slug, "-")

	if slug == "" {
		slug = "untitled"
	}

	return slug
}

func parseFrontMatter(content string) (title, author, source string, saved time.Time, tags []string, body string, err error) {
	// Front matter is delimited by "---\n" at start and "---\n" to close.
	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) < 3 || parts[0] != "" {
		return "", "", "", time.Time{}, nil, content, nil
	}

	header := parts[1]
	body = strings.TrimPrefix(parts[2], "\n")

	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		key := line[:idx]
		value := strings.TrimSpace(line[idx+1:])
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
				return "", "", "", time.Time{}, nil, "", fmt.Errorf("parsing saved time: %w", err)
			}
		case "tags":
			for _, t := range strings.Split(value, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
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

func hasTag(tags []string, tag string) bool {
	tag = strings.ToLower(tag)
	for _, t := range tags {
		if strings.ToLower(t) == tag {
			return true
		}
	}
	return false
}

// UpdateTags rewrites the tags line in an article's front matter on disk.
func (s *Store) UpdateTags(filePath string, tags []string) error {
	fullPath := filepath.Join(s.basePath, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("reading article: %w", err)
	}

	updated, err := replaceTags(string(content), tags)
	if err != nil {
		return err
	}

	tmpPath := fullPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("writing tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return fmt.Errorf("renaming tmp file: %w", err)
	}

	return s.scan()
}

// replaceTags splices the tags: line in front matter text.
func replaceTags(content string, tags []string) (string, error) {
	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) < 3 || parts[0] != "" {
		return "", fmt.Errorf("invalid front matter")
	}

	header := parts[1]
	body := parts[2]

	tagValue := strings.Join(tags, ", ")
	newLine := "tags: " + tagValue + "\n"

	var newHeader strings.Builder
	found := false
	for _, line := range strings.Split(header, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "tags:") {
			newHeader.WriteString(newLine)
			found = true
		} else if trimmed != "" {
			newHeader.WriteString(line + "\n")
		}
	}
	if !found {
		newHeader.WriteString(newLine)
	}

	return "---\n" + newHeader.String() + "---\n" + body, nil
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

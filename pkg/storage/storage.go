package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Article represents a saved article with its content.
type Article struct {
	Meta    ArticleMeta
	Content string
}

// ArticleMeta represents article metadata stored in the index.
type ArticleMeta struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	SourceURL    string    `json:"source_url"`
	SourceDomain string    `json:"source_domain"`
	SavedAt      time.Time `json:"saved_at"`
	FilePath     string    `json:"file_path"`
	FileSize     int64     `json:"file_size"`
	Tags         []string  `json:"tags"`
}

// Index represents the article index.
type Index struct {
	Articles []ArticleMeta `json:"articles"`
}

// Store manages article storage.
type Store struct {
	basePath  string
	indexPath string
	index     *Index
}

// New creates a new Store at the given base path.
func New(basePath string) (*Store, error) {
	s := &Store{
		basePath:  basePath,
		indexPath: filepath.Join(basePath, "index.json"),
	}

	// Ensure directories exist
	articlesDir := filepath.Join(basePath, "articles")
	if err := os.MkdirAll(articlesDir, 0755); err != nil {
		return nil, fmt.Errorf("creating articles directory: %w", err)
	}

	// Load or create index
	if err := s.loadIndex(); err != nil {
		return nil, fmt.Errorf("loading index: %w", err)
	}

	return s, nil
}

func (s *Store) loadIndex() error {
	data, err := os.ReadFile(s.indexPath)
	if os.IsNotExist(err) {
		s.index = &Index{Articles: []ArticleMeta{}}
		return nil
	}
	if err != nil {
		return err
	}

	s.index = &Index{}
	return json.Unmarshal(data, s.index)
}

func (s *Store) saveIndex() error {
	data, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.indexPath, data, 0644)
}

// Save stores an article and updates the index.
func (s *Store) Save(article *Article) error {
	// Generate ID if not set
	if article.Meta.ID == "" {
		article.Meta.ID = generateID()
	}

	// Set saved time
	if article.Meta.SavedAt.IsZero() {
		article.Meta.SavedAt = time.Now()
	}

	// Generate filename
	filename := generateFilename(article.Meta.SavedAt, article.Meta.Title)
	article.Meta.FilePath = filepath.Join("articles", filename)

	// Create markdown content with frontmatter
	content := formatMarkdown(article)
	article.Meta.FileSize = int64(len(content))

	// Write file
	fullPath := filepath.Join(s.basePath, article.Meta.FilePath)
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing article file: %w", err)
	}

	// Update index
	s.index.Articles = append(s.index.Articles, article.Meta)
	if err := s.saveIndex(); err != nil {
		// Try to clean up the file
		os.Remove(fullPath)
		return fmt.Errorf("saving index: %w", err)
	}

	return nil
}

// List returns all article metadata, sorted by saved date (newest first).
func (s *Store) List() []ArticleMeta {
	result := make([]ArticleMeta, len(s.index.Articles))
	copy(result, s.index.Articles)

	sort.Slice(result, func(i, j int) bool {
		return result[i].SavedAt.After(result[j].SavedAt)
	})

	return result
}

// Get retrieves an article by ID.
func (s *Store) Get(id string) (*Article, error) {
	var meta *ArticleMeta
	for i := range s.index.Articles {
		if s.index.Articles[i].ID == id {
			meta = &s.index.Articles[i]
			break
		}
	}

	if meta == nil {
		return nil, fmt.Errorf("article not found: %s", id)
	}

	fullPath := filepath.Join(s.basePath, meta.FilePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading article file: %w", err)
	}

	return &Article{
		Meta:    *meta,
		Content: string(content),
	}, nil
}

// GetFilePath returns the full file path for an article by ID.
func (s *Store) GetFilePath(id string) (string, error) {
	for i := range s.index.Articles {
		if s.index.Articles[i].ID == id {
			return filepath.Join(s.basePath, s.index.Articles[i].FilePath), nil
		}
	}
	return "", fmt.Errorf("article not found: %s", id)
}

// Delete removes an article by ID.
func (s *Store) Delete(id string) error {
	idx := -1
	for i := range s.index.Articles {
		if s.index.Articles[i].ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return fmt.Errorf("article not found: %s", id)
	}

	// Remove file
	fullPath := filepath.Join(s.basePath, s.index.Articles[idx].FilePath)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing article file: %w", err)
	}

	// Update index
	s.index.Articles = append(s.index.Articles[:idx], s.index.Articles[idx+1:]...)
	return s.saveIndex()
}

// Search filters articles by query (matches title, author, or domain).
func (s *Store) Search(query string) []ArticleMeta {
	if query == "" {
		return s.List()
	}

	query = strings.ToLower(query)
	var results []ArticleMeta

	for _, meta := range s.index.Articles {
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
	return len(s.index.Articles)
}

func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateFilename(savedAt time.Time, title string) string {
	date := savedAt.Format("2006-01-02")
	slug := slugify(title)
	return fmt.Sprintf("%s-%s.md", date, slug)
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

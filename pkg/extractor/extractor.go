package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/irfansharif/shelf/pkg/storage"
)

// Extractor handles content extraction from URLs.
type Extractor struct {
	client      *http.Client
	endpointURL string // Modal endpoint for HTML-to-Markdown conversion
}

// New creates a new Extractor that uses the given Modal endpoint for
// HTML-to-Markdown conversion.
func New(endpointURL string) *Extractor {
	return &Extractor{
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		endpointURL: endpointURL,
	}
}

// endpointResponse is the structured response from the Modal endpoint.
type endpointResponse struct {
	Title    string `json:"title"`
	Author   string `json:"author"`
	Markdown string `json:"markdown"`
}

// Extract fetches HTML from a URL and converts it to markdown via the Modal endpoint.
func (e *Extractor) Extract(sourceURL string) (*storage.Article, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme == "" {
		sourceURL = "https://" + sourceURL
	}

	// POST URL to Modal endpoint for conversion.
	reqBody, err := json.Marshal(map[string]string{"url": sourceURL})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}
	resp, err := e.client.Post(e.endpointURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("converting to markdown: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("markdown conversion HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result endpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	title := result.Title
	if title == "" {
		title = extractTitleFromURL(sourceURL)
	}

	article := &storage.Article{
		Meta: storage.ArticleMeta{
			Title:     title,
			Author:    result.Author,
			SourceURL: sourceURL,
		},
		Content: result.Markdown,
	}

	return article, nil
}

// extractTitleFromURL generates a title from the URL path.
func extractTitleFromURL(sourceURL string) string {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return "Untitled"
	}

	path := strings.Trim(parsed.Path, "/")
	segments := strings.Split(path, "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		last = strings.ReplaceAll(last, "-", " ")
		last = strings.ReplaceAll(last, "_", " ")
		if idx := strings.LastIndex(last, "."); idx > 0 {
			last = last[:idx]
		}
		if last != "" {
			return titleCase(last)
		}
	}

	return parsed.Host
}

var wordStartRe = regexp.MustCompile(`\b\w`)

// titleCase capitalizes the first letter of each word.
func titleCase(s string) string {
	return wordStartRe.ReplaceAllStringFunc(s, func(match string) string {
		runes := []rune(match)
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	})
}

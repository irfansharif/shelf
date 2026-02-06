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

	"github.com/irfansharif/browser/pkg/markdown"
	"github.com/irfansharif/browser/pkg/storage"
)

// Extractor handles content extraction from URLs.
type Extractor struct {
	client      *http.Client
	endpointURL string // Modal ReaderLM-v2 endpoint
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

// Extract fetches HTML from a URL and converts it to markdown via the Modal endpoint.
func (e *Extractor) Extract(sourceURL string) (*storage.Article, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme == "" {
		sourceURL = "https://" + sourceURL
		parsed, _ = url.Parse(sourceURL)
	}

	// Fetch raw HTML.
	req, err := http.NewRequest("GET", sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; browser-tui/1.0)")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	htmlBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Extract title and author from raw HTML before sending to endpoint.
	html := string(htmlBody)
	title := extractHTMLTitle(html)
	if title == "" {
		title = extractTitleFromURL(sourceURL)
	}
	author := extractHTMLAuthor(html)

	// POST HTML to Modal endpoint as JSON for markdown conversion.
	reqBody, err := json.Marshal(map[string]string{"html": string(htmlBody)})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}
	mdResp, err := e.client.Post(e.endpointURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("converting to markdown: %w", err)
	}
	defer mdResp.Body.Close()

	if mdResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(mdResp.Body)
		return nil, fmt.Errorf("markdown conversion HTTP %d: %s", mdResp.StatusCode, string(respBody))
	}

	var mdContent string
	if err := json.NewDecoder(mdResp.Body).Decode(&mdContent); err != nil {
		return nil, fmt.Errorf("reading markdown response: %w", err)
	}

	body := markdown.Process(mdContent, title)

	article := &storage.Article{
		Meta: storage.ArticleMeta{
			Title:        title,
			Author:       author,
			SourceURL:    sourceURL,
			SourceDomain: parsed.Host,
			Tags:         []string{},
		},
		Content: body,
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

var (
	htmlTitleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	htmlAuthorRe = regexp.MustCompile(`(?i)<meta[^>]+name=["']author["'][^>]+content=["']([^"']+)["']`)
)

// extractHTMLTitle pulls the <title> text from raw HTML.
func extractHTMLTitle(html string) string {
	m := htmlTitleRe.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// extractHTMLAuthor pulls <meta name="author" content="..."> from raw HTML.
func extractHTMLAuthor(html string) string {
	m := htmlAuthorRe.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// titleCase capitalizes the first letter of each word.
func titleCase(s string) string {
	re := regexp.MustCompile(`\b\w`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		runes := []rune(match)
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	})
}

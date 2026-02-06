package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmlutil "html"
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

	// POST URL to Modal endpoint for markdown conversion.
	reqBody, err := json.Marshal(map[string]string{"url": sourceURL})
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

	// Re-inject image references that the LLM may have dropped.
	images := extractHTMLImages(html, sourceURL)
	body = injectMissingImages(body, images)

	article := &storage.Article{
		Meta: storage.ArticleMeta{
			Title:     title,
			Author:    author,
			SourceURL: sourceURL,
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

// htmlImage represents an image found in HTML source.
type htmlImage struct {
	src string
	alt string
}

var (
	imgTagRe = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	imgSrcRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*["']([^"']+)["']`)
	imgAltRe = regexp.MustCompile(`(?i)\balt\s*=\s*["']([^"']*?)["']`)
	wsRe     = regexp.MustCompile(`\s+`)
)

// extractHTMLImages parses <img> tags from raw HTML, returning images with
// non-empty src and alt attributes. Relative URLs are resolved against baseURL.
func extractHTMLImages(rawHTML string, baseURL string) []htmlImage {
	base, _ := url.Parse(baseURL)
	var images []htmlImage
	for _, tag := range imgTagRe.FindAllString(rawHTML, -1) {
		srcMatch := imgSrcRe.FindStringSubmatch(tag)
		if srcMatch == nil {
			continue
		}
		src := htmlutil.UnescapeString(srcMatch[1])

		// Skip data URIs and placeholder srcs.
		if src == "" || src == "#" || strings.HasPrefix(src, "data:") {
			continue
		}

		altMatch := imgAltRe.FindStringSubmatch(tag)
		if altMatch == nil {
			continue
		}
		alt := htmlutil.UnescapeString(altMatch[1])
		if alt == "" {
			continue
		}

		// Resolve relative URLs.
		if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
			if base != nil {
				if ref, err := url.Parse(src); err == nil {
					src = base.ResolveReference(ref).String()
				}
			}
		}

		images = append(images, htmlImage{src: src, alt: alt})
	}
	return images
}

// injectMissingImages finds image alt text that appears as plain paragraphs in
// the markdown (where the LLM dropped the ![...](url) syntax) and restores the
// proper markdown image references.
func injectMissingImages(mdContent string, images []htmlImage) string {
	if len(images) == 0 {
		return mdContent
	}

	// Filter to images not already referenced in the markdown.
	var missing []htmlImage
	for _, img := range images {
		if strings.Contains(mdContent, "]("+img.src+")") {
			continue
		}
		missing = append(missing, img)
	}
	if len(missing) == 0 {
		return mdContent
	}

	// Split into paragraphs (blocks separated by blank lines).
	blocks := strings.Split(mdContent, "\n\n")
	used := make([]bool, len(missing))

	for i, block := range blocks {
		normBlock := collapseWhitespace(block)
		if len(normBlock) < 20 {
			continue
		}
		for j, img := range missing {
			if used[j] {
				continue
			}
			normAlt := collapseWhitespace(img.alt)
			if isAltTextMatch(normBlock, normAlt) {
				blocks[i] = fmt.Sprintf("![%s](%s)", img.alt, img.src)
				used[j] = true
				break
			}
		}
	}

	return strings.Join(blocks, "\n\n")
}

// collapseWhitespace normalizes a string by trimming and collapsing all
// whitespace runs (including newlines) into single spaces.
func collapseWhitespace(s string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// isAltTextMatch returns true if the markdown block appears to be the orphaned
// alt text from an image. The LLM sometimes drops prefixes like "Figure 1." so
// we check for containment with a minimum length threshold.
func isAltTextMatch(normBlock, normAlt string) bool {
	blockLower := strings.ToLower(normBlock)
	altLower := strings.ToLower(normAlt)

	if blockLower == altLower {
		return true
	}

	// Block text is contained in the alt text (LLM dropped a prefix).
	if len(blockLower) >= 30 && strings.Contains(altLower, blockLower) {
		return true
	}

	// Alt text is contained in the block (block has minor extra text).
	if len(altLower) >= 30 && strings.Contains(blockLower, altLower) {
		return float64(len(altLower))/float64(len(blockLower)) > 0.7
	}

	return false
}

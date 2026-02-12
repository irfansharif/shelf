package extractor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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

// ImageData holds a downloaded image with its relative path.
type ImageData struct {
	Path string // e.g. "images/photo.jpg"
	Data []byte // decoded image bytes
}

// ExtractResult is the result of extracting an article from a URL.
type ExtractResult struct {
	Title   string      // article title (for slug generation)
	Content string      // complete index.md content (front matter + markdown)
	Images  []ImageData // downloaded images with relative paths
}

// endpointResponse is the structured response from the Modal endpoint.
type endpointResponse struct {
	Title   string              `json:"title"`
	Content string              `json:"content"`
	Images  []endpointImageData `json:"images"`
}

type endpointImageData struct {
	Path string `json:"path"`
	Data string `json:"data"` // base64-encoded
}

// Extract fetches HTML from a URL and converts it to markdown via the Modal endpoint.
func (e *Extractor) Extract(sourceURL string) (*ExtractResult, error) {
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

	// Decode base64 image data.
	var images []ImageData
	for _, img := range result.Images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			return nil, fmt.Errorf("decoding image %s: %w", img.Path, err)
		}
		images = append(images, ImageData{Path: img.Path, Data: data})
	}

	return &ExtractResult{
		Title:   result.Title,
		Content: result.Content,
		Images:  images,
	}, nil
}

// ExtractFromHTML processes pre-fetched HTML via the Modal process endpoint,
// skipping the HTTP fetch step.
func (e *Extractor) ExtractFromHTML(sourceURL, rawHTML string) (*ExtractResult, error) {
	// Derive process endpoint URL from convert endpoint URL.
	processURL := strings.Replace(e.endpointURL, "-convert.", "-process.", 1)

	reqBody, err := json.Marshal(map[string]string{"url": sourceURL, "html": rawHTML})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}
	resp, err := e.client.Post(processURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("processing HTML: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("process endpoint HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result endpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var images []ImageData
	for _, img := range result.Images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			return nil, fmt.Errorf("decoding image %s: %w", img.Path, err)
		}
		images = append(images, ImageData{Path: img.Path, Data: data})
	}

	return &ExtractResult{
		Title:   result.Title,
		Content: result.Content,
		Images:  images,
	}, nil
}

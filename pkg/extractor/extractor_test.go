package extractor

import (
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	const endpoint = "https://irfansharif--browser-readerlm-convert.modal.run"
	const sourceURL = "http://vizier.report/p/corridor-2026"

	ext := New(endpoint)
	article, err := ext.Extract(sourceURL)
	if err != nil {
		t.Fatalf("Extract(%q): %v", sourceURL, err)
	}

	if article.Meta.Title == "" {
		t.Error("expected non-empty title")
	}
	if article.Meta.SourceURL != "http://vizier.report/p/corridor-2026" {
		t.Errorf("unexpected source URL: %s", article.Meta.SourceURL)
	}
	if article.Meta.SourceDomain != "vizier.report" {
		t.Errorf("unexpected domain: %s", article.Meta.SourceDomain)
	}
	if article.Content == "" {
		t.Error("expected non-empty content")
	}

	t.Logf("Title:  %s", article.Meta.Title)
	t.Logf("Author: %s", article.Meta.Author)
	t.Logf("Content (first 500 chars):\n%s", truncate(article.Content, 500))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func TestInjectMissingImages(t *testing.T) {
	// Simulates the LLM output where image alt text appears as plain paragraphs.
	md := strings.Join([]string{
		"# Rubbing Control Theory",
		"",
		"Some intro text about the article.",
		"",
		"3-node 8vCPU cluster running TPC-C with 1000 warehouses and an aggressive backup schedule",
		"(incremental backups every 10m, full backups every 1h at the 45m mark), with the p99 latency impact",
		"shown.",
		"",
		"This problem is more general: any background operation.",
		"",
		"Low-and-highly-varying runnable goroutines per-processor despite high-but-stable foreground",
		"latencies.",
		"",
		"Some more text after the second image.",
	}, "\n")

	images := []htmlImage{
		{
			src: "https://irfansharif.io/img/incr-and-full-backups.png",
			alt: "3-node 8vCPU cluster running TPC-C with 1000 warehouses and an aggressive backup schedule (incremental backups every 10m, full backups every 1h at the 45m mark), with the p99 latency impact shown.",
		},
		{
			src: "https://irfansharif.io/img/runnable-gs-per-p.png",
			alt: "Low-and-highly-varying runnable goroutines per-processor despite high-but-stable foreground latencies.",
		},
	}

	result := injectMissingImages(md, images)

	if !strings.Contains(result, "![3-node 8vCPU cluster") {
		t.Errorf("expected first image to be injected, got:\n%s", result)
	}
	if !strings.Contains(result, "](https://irfansharif.io/img/incr-and-full-backups.png)") {
		t.Errorf("expected first image URL, got:\n%s", result)
	}
	if !strings.Contains(result, "![Low-and-highly-varying") {
		t.Errorf("expected second image to be injected, got:\n%s", result)
	}
	if !strings.Contains(result, "](https://irfansharif.io/img/runnable-gs-per-p.png)") {
		t.Errorf("expected second image URL, got:\n%s", result)
	}

	// Surrounding text preserved.
	if !strings.Contains(result, "Some intro text") {
		t.Error("intro text lost")
	}
	if !strings.Contains(result, "This problem is more general") {
		t.Error("following paragraph lost")
	}
}

func TestInjectMissingImages_AlreadyPresent(t *testing.T) {
	md := "![alt text](https://example.com/img.png)\n\nSome paragraph."
	images := []htmlImage{
		{src: "https://example.com/img.png", alt: "alt text"},
	}
	result := injectMissingImages(md, images)
	if result != md {
		t.Errorf("should not modify markdown when images already present, got:\n%s", result)
	}
}

func TestExtractHTMLImages(t *testing.T) {
	html := `<html>
		<img src="/img/foo.png" alt="A foo image">
		<img src="https://example.com/bar.jpg" alt="A bar image">
		<img src="data:image/png;base64,abc" alt="should skip">
		<img src="#" alt="placeholder">
		<img src="/img/noalt.png">
	</html>`

	images := extractHTMLImages(html, "https://mysite.com/page")

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d: %+v", len(images), images)
	}

	if images[0].src != "https://mysite.com/img/foo.png" {
		t.Errorf("expected resolved URL, got %s", images[0].src)
	}
	if images[0].alt != "A foo image" {
		t.Errorf("expected 'A foo image', got %s", images[0].alt)
	}

	if images[1].src != "https://example.com/bar.jpg" {
		t.Errorf("expected absolute URL, got %s", images[1].src)
	}
}

func TestIsAltTextMatch(t *testing.T) {
	tests := []struct {
		name     string
		block    string
		alt      string
		expected bool
	}{
		{
			name:     "exact match",
			block:    "some long alt text that describes the image in detail",
			alt:      "some long alt text that describes the image in detail",
			expected: true,
		},
		{
			name:     "block is substring of alt (LLM dropped prefix)",
			block:    "cluster running tpc-c with 1000 warehouses and an aggressive backup schedule shown",
			alt:      "figure 1. cluster running tpc-c with 1000 warehouses and an aggressive backup schedule shown",
			expected: true,
		},
		{
			name:     "short block should not match",
			block:    "short text",
			alt:      "short text within a much longer alt",
			expected: false,
		},
		{
			name:     "unrelated text should not match",
			block:    "this is a completely different paragraph about something else entirely",
			alt:      "an image showing the performance graph of the system under load",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAltTextMatch(tt.block, tt.alt)
			if got != tt.expected {
				t.Errorf("isAltTextMatch(%q, %q) = %v, want %v", tt.block, tt.alt, got, tt.expected)
			}
		})
	}
}

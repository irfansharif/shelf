package extractor

import (
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

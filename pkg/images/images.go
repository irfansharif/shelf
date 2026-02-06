package images

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var markdownImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// DownloadAndRewrite scans markdown content for remote image references,
// downloads them into imagesDir, and rewrites the references to local paths.
// If an image download fails, the original remote URL is kept.
func DownloadAndRewrite(content string, imagesDir string) string {
	matches := markdownImageRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content
	}

	// Collect unique remote URLs and their desired local filenames.
	type imageRef struct {
		url      string
		filename string
	}

	seen := make(map[string]string)      // url -> local filename
	usedNames := make(map[string]bool)   // track used filenames for dedup
	downloaded := make(map[string]bool)   // url -> success
	var refs []imageRef

	for _, loc := range matches {
		urlStr := content[loc[4]:loc[5]]
		if !isRemoteURL(urlStr) {
			continue
		}
		if _, ok := seen[urlStr]; ok {
			continue
		}
		filename := localFilename(urlStr, usedNames)
		usedNames[filename] = true
		seen[urlStr] = filename
		refs = append(refs, imageRef{url: urlStr, filename: filename})
	}

	if len(refs) == 0 {
		return content
	}

	// Create the images directory.
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return content
	}

	// Download each image.
	client := &http.Client{Timeout: 30 * time.Second}
	for _, ref := range refs {
		destPath := filepath.Join(imagesDir, ref.filename)
		if err := downloadFile(client, ref.url, destPath); err == nil {
			downloaded[ref.url] = true
		}
	}

	// Rewrite markdown references (process in reverse to preserve indices).
	result := content
	for i := len(matches) - 1; i >= 0; i-- {
		loc := matches[i]
		urlStr := result[loc[4]:loc[5]]
		if !isRemoteURL(urlStr) {
			continue
		}
		filename, ok := seen[urlStr]
		if !ok || !downloaded[urlStr] {
			continue
		}
		alt := result[loc[2]:loc[3]]
		replacement := fmt.Sprintf("![%s](images/%s)", alt, filename)
		result = result[:loc[0]] + replacement + result[loc[1]:]
	}

	return result
}

func isRemoteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func localFilename(rawURL string, used map[string]bool) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "image.png"
	}

	base := path.Base(parsed.Path)
	if base == "" || base == "." || base == "/" {
		base = "image.png"
	}

	// Sanitize: keep only alphanumeric, hyphens, underscores, dots.
	var sb strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			sb.WriteRune(r)
		}
	}
	name := sb.String()
	if name == "" {
		name = "image.png"
	}

	// Ensure it has an extension.
	if filepath.Ext(name) == "" {
		name += ".png"
	}

	// Deduplicate.
	if !used[name] {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !used[candidate] {
			return candidate
		}
	}
}

func downloadFile(client *http.Client, url, destPath string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

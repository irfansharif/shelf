package extractor_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/datadriven"
)

func TestBrowser(t *testing.T) {
	if os.Getenv("FIXTURE") == "" {
		t.Skip("skipping fixture tests (set FIXTURE=1 to run)")
	}
	datadriven.Walk(t, "testdata/fixture", func(t *testing.T, path string) {
		datadriven.RunTest(t, path, func(t *testing.T, d *datadriven.TestData) string {
			switch d.Cmd {
			case "extract":
				return fixtureExtract(t, d)
			default:
				d.Fatalf(t, "unknown command %q", d.Cmd)
				return ""
			}
		})
	})
}

// safariCurrentTabURL returns the URL of Safari's frontmost tab.
func safariCurrentTabURL() (string, error) {
	script := `tell application "Safari" to return URL of current tab of front window`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// safariCurrentTabSource returns the page source of Safari's frontmost tab.
func safariCurrentTabSource() (string, error) {
	script := `tell application "Safari" to return source of current tab of front window`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func fixtureExtract(t *testing.T, d *datadriven.TestData) string {
	t.Helper()
	if len(d.CmdArgs) < 2 {
		d.Fatalf(t, "extract requires <url> <slug>.html arguments")
	}
	url := d.CmdArgs[0].Key
	slug := d.CmdArgs[1].Key

	// Open URL in Safari.
	escaped := strings.ReplaceAll(url, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	openScript := fmt.Sprintf(`tell application "Safari"
	activate
	if (count of windows) = 0 then
		make new document with properties {URL:"%s"}
	else
		tell front window
			set current tab to (make new tab with properties {URL:"%s"})
		end tell
	end if
end tell`, escaped, escaped)
	if _, err := exec.Command("osascript", "-e", openScript).Output(); err != nil {
		d.Fatalf(t, "opening Safari: %v", err)
	}

	// Wait for Safari's current tab to navigate to our URL. Substack URLs
	// redirect (e.g. /home/post/p-NNN → actual article URL), so we check
	// that the tab URL starts with the requested URL or the tab has moved
	// away from a blank/previous page.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		tabURL, err := safariCurrentTabURL()
		if err != nil {
			continue
		}
		// Accept if tab URL starts with our URL (exact or redirected).
		if strings.HasPrefix(tabURL, url) {
			break
		}
	}

	// Now wait for the page source to stabilize (two consecutive reads match).
	var html string
	var prev string
	deadline = time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		h, err := safariCurrentTabSource()
		if err != nil || strings.TrimSpace(h) == "" {
			continue
		}
		if h == prev {
			html = h
			break
		}
		prev = h
	}
	if html == "" {
		if prev != "" {
			html = prev
		} else {
			d.Fatalf(t, "timed out waiting for Safari to load %s", url)
		}
	}

	fixturePath := filepath.Join("fixtures", slug)
	if err := os.WriteFile(fixturePath, []byte(html), 0644); err != nil {
		d.Fatalf(t, "writing fixture %s: %v", fixturePath, err)
	}
	t.Logf("wrote fixture to %s (%d bytes)", fixturePath, len(html))
	return ""
}

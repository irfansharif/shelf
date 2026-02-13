package extractor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/datadriven"

	"github.com/irfansharif/shelf/pkg/safari"
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

func fixtureExtract(t *testing.T, d *datadriven.TestData) string {
	t.Helper()
	if len(d.CmdArgs) < 2 {
		d.Fatalf(t, "extract requires <url> <slug>.html arguments")
	}
	url := d.CmdArgs[0].Key
	slug := d.CmdArgs[1].Key

	// Open URL in a dedicated Safari window.
	w, err := safari.OpenURL(url)
	if err != nil {
		d.Fatalf(t, "opening Safari: %v", err)
	}
	defer w.Close()

	// Wait for Safari's tab to navigate to our URL. Substack URLs redirect
	// (e.g. /home/post/p-NNN → actual article URL), so we check that the
	// tab URL starts with the requested URL.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		tabURL, err := w.TabURL()
		if err != nil {
			continue
		}
		if strings.HasPrefix(tabURL, url) {
			break
		}
	}

	// Wait for the page source to stabilize (two consecutive reads match).
	var html string
	var prev string
	deadline = time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		h, err := w.TabSource()
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

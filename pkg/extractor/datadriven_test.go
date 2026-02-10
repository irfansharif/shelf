package extractor_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/datadriven"
)

var defaultEndpoints = map[string]string{
	"model": "https://irfansharif--shelf-model-converter-convert.modal.run",
	"api":   "https://irfansharif--shelf-api-converter-convert.modal.run",
}

func endpointURL(app string) string {
	switch app {
	case "model":
		if v := os.Getenv("SHELF_MODEL_ENDPOINT"); v != "" {
			return v
		}
	case "api":
		if v := os.Getenv("SHELF_API_ENDPOINT"); v != "" {
			return v
		}
	}
	return defaultEndpoints[app]
}

func TestConvert(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("skipping modal tests (set INTEGRATION=1 to run)")
	}
	datadriven.Walk(t, "testdata/convert", func(t *testing.T, path string) {
		var state string
		datadriven.RunTest(t, path, func(t *testing.T, d *datadriven.TestData) string {
			return runCmd(t, d, &state)
		})
	})
}

func TestProcess(t *testing.T) {
	datadriven.Walk(t, "testdata/process", func(t *testing.T, path string) {
		var state string
		datadriven.RunTest(t, path, func(t *testing.T, d *datadriven.TestData) string {
			return runCmd(t, d, &state)
		})
	})
}

func runCmd(t *testing.T, d *datadriven.TestData, state *string) string {
	t.Helper()
	switch d.Cmd {
	case "convert":
		return cmdConvert(t, d, state)
	case "persist":
		return cmdPersist(t, d, state)
	case "load":
		return cmdLoad(t, d, state)
	case "process":
		return cmdProcess(t, d, state)
	case "lines":
		return cmdLines(t, d, state)
	default:
		d.Fatalf(t, "unknown command %q", d.Cmd)
		return ""
	}
}

func cmdConvert(t *testing.T, d *datadriven.TestData, state *string) string {
	t.Helper()
	if len(d.CmdArgs) == 0 {
		d.Fatalf(t, "convert requires a URL argument")
	}
	url := d.CmdArgs[0].Key

	app := "model"
	if d.HasArg("app") {
		d.ScanArgs(t, "app", &app)
	}

	endpoint := endpointURL(app)
	if endpoint == "" {
		d.Fatalf(t, "unknown app %q", app)
	}

	reqBody, err := json.Marshal(map[string]string{"url": url})
	if err != nil {
		d.Fatalf(t, "encoding request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		d.Fatalf(t, "POST %s: %v", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		d.Fatalf(t, "HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Title    string `json:"title"`
		Author   string `json:"author"`
		Markdown string `json:"markdown"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		d.Fatalf(t, "decoding response: %v", err)
	}

	*state = result.Markdown
	return ""
}

func cmdPersist(t *testing.T, d *datadriven.TestData, state *string) string {
	t.Helper()
	if len(d.CmdArgs) == 0 {
		d.Fatalf(t, "persist requires a filename stem")
	}
	stem := d.CmdArgs[0].Key
	path := filepath.Join("testdata", "fixtures", stem+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		d.Fatalf(t, "creating fixtures directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(*state), 0644); err != nil {
		d.Fatalf(t, "writing %s: %v", path, err)
	}
	return fmt.Sprintf("wrote %d bytes\n", len(*state))
}

func cmdLoad(t *testing.T, d *datadriven.TestData, state *string) string {
	t.Helper()
	if len(d.CmdArgs) == 0 {
		d.Fatalf(t, "load requires a filename stem")
	}
	stem := d.CmdArgs[0].Key
	path := filepath.Join("testdata", "fixtures", stem+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		d.Fatalf(t, "reading %s: %v", path, err)
	}
	*state = string(data)
	return ""
}

func cmdProcess(t *testing.T, d *datadriven.TestData, state *string) string {
	t.Helper()
	return ""
}

func cmdLines(t *testing.T, d *datadriven.TestData, state *string) string {
	t.Helper()
	if len(d.CmdArgs) == 0 {
		d.Fatalf(t, "lines requires a range argument (e.g., 1-5)")
	}
	rangeStr := d.CmdArgs[0].Key

	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) != 2 {
		d.Fatalf(t, "invalid range %q (expected N-M)", rangeStr)
	}

	start, err := strconv.Atoi(parts[0])
	if err != nil {
		d.Fatalf(t, "invalid start line %q: %v", parts[0], err)
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		d.Fatalf(t, "invalid end line %q: %v", parts[1], err)
	}

	lines := strings.Split(*state, "\n")
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return ""
	}

	selected := lines[start-1 : end]
	for i, line := range selected {
		if line == "----" {
			selected[i] = "---- "
		}
	}
	return strings.Join(selected, "\n") + "\n"
}

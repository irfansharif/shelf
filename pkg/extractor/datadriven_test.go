package extractor_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/cockroachdb/datadriven"
	"github.com/irfansharif/shelf/pkg/extractor"
)

const endpoint = "https://irfansharif--shelf-api-converter-convert.modal.run"

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

func runCmd(t *testing.T, d *datadriven.TestData, state *string) string {
	t.Helper()
	switch d.Cmd {
	case "convert":
		return cmdConvert(t, d, state)
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

	ext := extractor.New(endpoint)
	result, err := ext.Extract(url)
	if err != nil {
		d.Fatalf(t, "extracting %s: %v", url, err)
	}

	*state = result.Content
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

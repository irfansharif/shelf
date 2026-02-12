package safari

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Tab represents a single browser tab from Safari.
type Tab struct {
	URL        string
	Title      string
	Source     string // "local", "icloud", "readinglist"
	LastViewed time.Time
}

// appleEpochOffset is the number of seconds between the Unix epoch
// (1970-01-01) and the Apple Core Data epoch (2001-01-01).
const appleEpochOffset = 978307200

// appleTimeToGoTime converts an Apple Core Data timestamp (seconds since
// 2001-01-01) to a Go time.Time.
func appleTimeToGoTime(appleTS float64) time.Time {
	return time.Unix(int64(appleTS)+appleEpochOffset, 0)
}

// GatherTabs collects tabs from all available Safari sources (local tabs,
// iCloud tabs, Reading List). Each source is best-effort: failures are
// returned as warnings rather than fatal errors. Tabs are deduplicated
// within each source independently (keeping the most recently viewed on
// URL collision).
func GatherTabs() (map[string][]Tab, []error) {
	result := make(map[string][]Tab)
	var warnings []error

	local, err := localTabs()
	if err != nil {
		warnings = append(warnings, fmt.Errorf("local tabs: %w", err))
	}
	if len(local) > 0 {
		result["local"] = deduplicateByURL(local)
	}

	icloud, err := icloudTabs()
	if err != nil {
		warnings = append(warnings, fmt.Errorf("iCloud tabs: %w", err))
	}
	if len(icloud) > 0 {
		result["icloud"] = deduplicateByURL(icloud)
	}

	reading, err := readingListTabs()
	if err != nil {
		warnings = append(warnings, fmt.Errorf("Reading List: %w", err))
	}
	if len(reading) > 0 {
		result["readinglist"] = deduplicateByURL(reading)
	}

	return result, warnings
}

// localTabs uses JXA (JavaScript for Automation) via osascript to get open
// Safari tabs. This works without any special permissions.
func localTabs() ([]Tab, error) {
	script := `
var safari = Application("Safari");
var tabs = [];
for (var w = 0; w < safari.windows.length; w++) {
    var win = safari.windows[w];
    for (var t = 0; t < win.tabs.length; t++) {
        var tab = win.tabs[t];
        tabs.push({url: tab.url(), title: tab.name()});
    }
}
JSON.stringify(tabs);
`
	out, err := exec.Command("osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "-1743") {
				return nil, fmt.Errorf("Automation permission required — allow your terminal to control Safari in System Settings > Privacy & Security > Automation")
			}
			return nil, fmt.Errorf("osascript: %s", stderr)
		}
		return nil, fmt.Errorf("osascript: %w", err)
	}

	var raw []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing JXA output: %w", err)
	}

	historyTimes := localTabHistoryTimes()

	var tabs []Tab
	for _, r := range raw {
		if r.URL == "" {
			continue
		}
		t := Tab{URL: r.URL, Title: r.Title, Source: "local"}
		if ts, ok := historyTimes[r.URL]; ok && ts > 0 {
			t.LastViewed = appleTimeToGoTime(ts)
		}
		tabs = append(tabs, t)
	}
	return tabs, nil
}

// localTabHistoryTimes queries Safari's History.db for the most recent visit
// time of each URL. Returns nil on any error (Full Disk Access required).
func localTabHistoryTimes() map[string]float64 {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	dbPath := filepath.Join(home, "Library", "Safari", "History.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}

	query := "SELECT hi.url, MAX(hv.visit_time) AS last_visit FROM history_items hi JOIN history_visits hv ON hv.history_item = hi.id GROUP BY hi.url;"
	out, err := exec.Command("sqlite3", "-json", dbPath, query).Output()
	if err != nil {
		return nil
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}

	var rows []struct {
		URL       string  `json:"url"`
		LastVisit float64 `json:"last_visit"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil
	}

	m := make(map[string]float64, len(rows))
	for _, r := range rows {
		m[r.URL] = r.LastVisit
	}
	return m
}

// icloudTabs attempts to read iCloud tabs from CloudTabs.db using sqlite3.
// Requires Full Disk Access for the containerized path; degrades gracefully.
func icloudTabs() ([]Tab, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	paths := []string{
		filepath.Join(home, "Library", "Safari", "CloudTabs.db"),
		filepath.Join(home, "Library", "Containers", "com.apple.Safari", "Data", "Library", "Safari", "CloudTabs.db"),
	}

	var dbPath string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			dbPath = p
			break
		}
	}
	if dbPath == "" {
		return nil, fmt.Errorf("CloudTabs.db not found (iCloud tabs unavailable)")
	}

	query := "SELECT title, url, last_viewed_time FROM cloud_tabs;"
	cmd := exec.Command("sqlite3", "-json", dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		// Extract stderr for a useful error message.
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "authorization denied") {
				return nil, fmt.Errorf("Full Disk Access required to read iCloud tabs")
			}
			return nil, fmt.Errorf("sqlite3: %s", stderr)
		}
		return nil, fmt.Errorf("sqlite3: %w", err)
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}

	var rows []struct {
		Title          string  `json:"title"`
		URL            string  `json:"url"`
		LastViewedTime float64 `json:"last_viewed_time"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parsing sqlite3 output: %w", err)
	}

	var tabs []Tab
	for _, r := range rows {
		if r.URL == "" {
			continue
		}
		t := Tab{URL: r.URL, Title: r.Title, Source: "icloud"}
		if r.LastViewedTime > 0 {
			t.LastViewed = appleTimeToGoTime(r.LastViewedTime)
		}
		tabs = append(tabs, t)
	}
	return tabs, nil
}

// readingListTabs reads Safari's Reading List from Bookmarks.plist.
// Requires Full Disk Access; degrades gracefully if not available.
//
// We use python3's plistlib rather than plutil because Bookmarks.plist
// contains NSDate values that plutil -convert json cannot represent,
// causing "invalid object in plist for destination format" errors.
func readingListTabs() ([]Tab, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	plistPath := filepath.Join(home, "Library", "Safari", "Bookmarks.plist")
	if _, err := os.Stat(plistPath); err != nil {
		return nil, fmt.Errorf("Bookmarks.plist not found (Full Disk Access required)")
	}

	script := `
import plistlib, json, sys
with open(sys.argv[1], 'rb') as f:
    data = plistlib.load(f)
items = []
for child in data.get('Children', []):
    if child.get('Title') == 'com.apple.ReadingList':
        for item in child.get('Children', []):
            url = item.get('URLString', '')
            title = ''
            uri_dict = item.get('URIDictionary', {})
            if uri_dict:
                title = uri_dict.get('title', '')
            unix_ts = 0
            rl = item.get('ReadingList', {})
            dt = rl.get('DateLastViewed') or rl.get('DateAdded')
            if dt is not None:
                unix_ts = dt.timestamp()
            if url:
                items.append({'url': url, 'title': title, 'unix_ts': unix_ts})
print(json.dumps(items))
`
	out, err := exec.Command("python3", "-c", script, plistPath).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "PermissionError") || strings.Contains(stderr, "Operation not permitted") {
				return nil, fmt.Errorf("Full Disk Access required to read Reading List")
			}
			return nil, fmt.Errorf("python3: %s", stderr)
		}
		return nil, fmt.Errorf("python3: %w", err)
	}

	var items []struct {
		URL    string  `json:"url"`
		Title  string  `json:"title"`
		UnixTS float64 `json:"unix_ts"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parsing reading list output: %w", err)
	}

	var tabs []Tab
	for _, item := range items {
		t := Tab{URL: item.URL, Title: item.Title, Source: "readinglist"}
		if item.UnixTS > 0 {
			t.LastViewed = time.Unix(int64(item.UnixTS), 0)
		}
		tabs = append(tabs, t)
	}
	return tabs, nil
}

// deduplicateByURL removes duplicate URLs within a single source, keeping the
// tab with the most recent LastViewed time on collision.
func deduplicateByURL(tabs []Tab) []Tab {
	seen := make(map[string]int) // URL -> index in result
	var result []Tab

	for _, t := range tabs {
		if idx, exists := seen[t.URL]; exists {
			if t.LastViewed.After(result[idx].LastViewed) {
				result[idx] = t
			}
		} else {
			seen[t.URL] = len(result)
			result = append(result, t)
		}
	}
	return result
}

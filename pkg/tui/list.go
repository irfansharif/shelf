package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/irfansharif/browser/pkg/storage"
)

// formatRelativeTime returns a human-readable relative time string.
func formatRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	case diff < 365*24*time.Hour:
		months := int(diff.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		years := int(diff.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}

// formatFileSize returns a human-readable file size.
func formatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%d KB", bytes/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// truncateString truncates a string to the given width, adding ellipsis if needed.
func truncateString(s string, width int) string {
	if width <= 3 {
		return s
	}
	if len(s) <= width {
		return s
	}
	return s[:width-3] + "..."
}

// renderArticleItem renders a single article item for the list.
func renderArticleItem(meta storage.ArticleMeta, selected bool, width int, styles Styles) string {
	var sb strings.Builder

	titleWidth := width - 4 // Account for selection marker and padding

	title := truncateString(meta.Title, titleWidth)
	if title == "" {
		title = "Untitled"
	}

	// Build description line: Author · domain · relative time · size
	var descParts []string
	if meta.Author != "" {
		descParts = append(descParts, meta.Author)
	}
	if meta.SourceDomain != "" {
		descParts = append(descParts, meta.SourceDomain)
	}
	descParts = append(descParts, formatRelativeTime(meta.SavedAt))
	if meta.FileSize > 0 {
		descParts = append(descParts, formatFileSize(meta.FileSize))
	}
	desc := strings.Join(descParts, " · ")

	// Render tags as styled chips
	var tagStr string
	if len(meta.Tags) > 0 {
		var tags []string
		for _, t := range meta.Tags {
			tags = append(tags, styles.Tag.Render("#"+t))
		}
		tagStr = " " + strings.Join(tags, " ")
	}

	if selected {
		sb.WriteString(styles.SelectionMarker.Render(""))
		sb.WriteString(styles.SelectedTitle.Render(title))
		sb.WriteString("\n")
		sb.WriteString("  ")
		sb.WriteString(styles.SelectedDesc.Render(desc))
		sb.WriteString(tagStr)
	} else {
		sb.WriteString("  ")
		sb.WriteString(styles.ListItemTitle.Render(title))
		sb.WriteString("\n")
		sb.WriteString("  ")
		sb.WriteString(styles.ListItemDesc.Render(desc))
		sb.WriteString(tagStr)
	}

	return sb.String()
}

// renderEmptyState renders the empty state message.
func renderEmptyState(styles Styles) string {
	return styles.Muted.Render("No articles saved yet. Press 'a' to add a URL.")
}

// renderNoResults renders the no search results message.
func renderNoResults(query string, styles Styles) string {
	return styles.Muted.Render(fmt.Sprintf("No articles matching '%s'", query))
}

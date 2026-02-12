# AGENTS.md

## Project Overview

Terminal UI app for saving and reading web articles offline. URLs are fetched
and converted to Markdown via a Modal-hosted endpoint, then stored locally
with downloaded images.

## Repository Layout

```
cmd/shelf/         Entry point; loads config from ~/.shelf/shelf.toml, boots TUI
pkg/extractor/     Fetches HTML, extracts metadata, calls Modal endpoint, injects missing images
pkg/storage/       Saves/loads articles as Markdown files with YAML front matter
pkg/images/        Downloads remote images, rewrites Markdown links to local paths
pkg/config/        Reads ~/.shelf/shelf.toml (endpoint URL, data directory)
pkg/tui/           Bubble Tea TUI: list view, URL input, search, keybindings, styles
modal/             Python: Modal serverless app (api.py = readability + markdownify on CPU)
data/articles/     Stored articles (gitignored)
```

## Building and Running
```bash
go build -o shelf ./cmd/shelf
./shelf
```
Requires Go 1.24+. On first run, a default config file is created at
`~/.shelf/shelf.toml`. Set the `endpoint` field to the Modal endpoint URL
before running (e.g. `endpoint = "https://irfansharif--shelf-jina-jinaconverter-convert.modal.run"`).

## Tests
```bash
go test ./...
```
Tests live alongside their packages (e.g. `pkg/extractor/extractor_test.go`).
`TestExtract` hits a live endpoint and requires network access.


## Modal Deployment
```bash
cd modal
modal deploy api.py
```
## Key Conventions

- Go style: standard `gofmt`, no linter config
- Articles stored as `data/articles/{slug}/index.md` with an `images/` subdirectory
- TUI uses Solarized color scheme (see `pkg/tui/styles.go`)
- Modal endpoint accepts `{"url": "..."}` via POST, returns JSON-encoded Markdown string
- Go client must `json.Decode` the response (FastAPI JSON-wraps string returns)
- Client timeout is 5 minutes (Modal can take 2.5+ min for long articles)

## Memory

There is a `MEMORY.md` file at the repository root. Read it at the start of a
session and update it when you learn something worth preserving — gotchas,
constraints, things that didn't work, patterns that did. After any reasonably
sized coding session, review what you encountered and incorporate useful
learnings before wrapping up. Keep it concise and organized by topic.


## Modal Deployment

When deploying Modal apps, work in phases: (1) read all related source files
and verify interface consistency and no deprecated APIs, (2) deploy and capture
errors — if deployment fails, fix and retry, (3) regression-test deployed
endpoints with real inputs and diff outputs before considering it done.

## TUI Changes

For visual/UI changes, work in a build-verify loop: make the edit, build,
capture a terminal snapshot (e.g. via tmux capture-pane), read it back to
verify correctness (colors, alignment, no clipping, ANSI-aware widths), and
iterate until the snapshot matches the spec.

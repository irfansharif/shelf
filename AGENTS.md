# AGENTS.md

## Project Overview

Terminal UI app for saving and reading web articles offline. URLs are fetched
and converted to Markdown via a Modal-hosted LLM (ReaderLM-v2 on H100) or an
external Jina endpoint (proxied through Modal as well), then stored locally
with downloaded images.

## Repository Layout

```
cmd/browser/       Entry point; parses -endpoint flag, boots TUI
pkg/extractor/     Fetches HTML, extracts metadata, calls Modal endpoint, injects missing images
pkg/storage/       Saves/loads articles as Markdown files with YAML front matter
pkg/images/        Downloads remote images, rewrites Markdown links to local paths
pkg/markdown/      Post-processing (currently a pass-through)
pkg/tui/           Bubble Tea TUI: list view, URL input, search, keybindings, styles
modal/             Python: Modal serverless app (vLLM on H100), readability extraction, heading fixes
data/articles/     Stored articles (gitignored)
```

## Building and Running

```bash
go build -o browser ./cmd/browser
./browser -endpoint https://irfansharif--browser-readerlm-convert.modal.run
```

Requires Go 1.24+. The `-endpoint` flag is mandatory.

## Tests

```bash
go test ./...
```

Tests live alongside their packages (e.g. `pkg/extractor/extractor_test.go`).
`TestExtract` hits a live endpoint and requires network access.

## Modal Deployment

```bash
cd modal
modal deploy readerlm.py
modal deploy jina.py
```

If warm containers serve stale code after deploy, run `modal app stop browser`
first. Use `modal run modal/readerlm.py` to test locally.

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

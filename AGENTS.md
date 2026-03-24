## Cursor Cloud specific instructions

### Overview

Shelf is a Go 1.24+ TUI application (Bubble Tea) for saving web articles offline as Markdown.
The Go client talks to a remote Modal serverless endpoint for HTML-to-Markdown conversion.
There is no Docker, database, or local backend to run — just a single Go binary.

### Building and running

See `CLAUDE.md` for full details. Quick reference:

- Build: `go build -o shelf ./cmd/shelf`
- Run: `./shelf` (requires `~/.shelf/shelf.toml` with `endpoint` set)
- Tests: `go test ./...`
- Lint: `go vet ./...` (no external linter configured; `gofmt` for formatting)

### Config

The TUI requires `~/.shelf/shelf.toml` with a valid `endpoint` before it will start.
The default endpoint is `https://irfansharif--shelf-api-converter-convert.modal.run`.
Set `data_dir` to where articles should be stored (default: `~/.shelf/data`).
The config file is auto-created on first run but `endpoint` will be empty — you must populate it.

### Non-obvious caveats

- The TUI is a Bubble Tea alt-screen app. To test it programmatically, run it inside tmux
  (`tmux new-session -d -s shelf ./shelf`) and capture output with `tmux capture-pane -t shelf -p`.
- Article fetching hits the live Modal endpoint and can take 10-60+ seconds depending on cold start.
  The Go client timeout is 5 minutes.
- `go test ./...` skips Modal integration tests by default. Use `MODAL=1` to run them.
- `gofmt -l .` may show formatting diffs in some files — this is pre-existing and not a CI gate.
- Safari-related features (tab import, fixture recording) are macOS-only and non-functional on Linux.

# Shelf

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
before running.

## Configuration

Config lives at `~/.shelf/shelf.toml`:

```toml
endpoint = "https://irfansharif--shelf-api-converter-convert.modal.run"
data_dir = "~/path/to/articles"
```

Articles are stored as `articles/{slug}/index.md` with YAML front matter.

## Key Conventions

- Go style: standard `gofmt`, no linter config
- Articles stored as `data/articles/{slug}/index.md` with an `images/` subdirectory
- TUI uses Solarized color scheme (see `pkg/tui/styles.go`)
- Modal endpoint accepts `{"url": "..."}` via POST, returns JSON-encoded Markdown string
- Go client must `json.Decode` the response (FastAPI JSON-wraps string returns)
- Client timeout is 5 minutes (Modal can take 2.5+ min for long articles)

## Tests

Tests use [datadriven](https://github.com/cockroachdb/datadriven) test files
under `pkg/extractor/testdata/`.

```bash
go test ./...
```

**Convert tests** (`MODAL=1`) hit the live Modal endpoint:

```
MODAL=1 go test ./pkg/extractor/ -run TestConvert -v
```

Test directives:

- `convert <url>` — fetches URL via the Modal convert endpoint
- `process <slug>.html` — reads a fixture from `pkg/extractor/fixtures/` and
  sends it to the Modal process endpoint (for sites behind bot protection)
- `lines <N>-<M>` — asserts on a line range of the converted output

**Recording fixtures** (`FIXTURE=1`) uses Safari to capture page source for
sites that block automated HTTP requests:

```
FIXTURE=1 go test ./pkg/extractor/ -run TestBrowser -v
```

This opens the URL in Safari, waits for the page to load and stabilize, then
saves the HTML to `pkg/extractor/fixtures/`. Fixture files are large and
gitignored; regenerate them with the command above.

Test directives:

- `extract <url> <slug>.html` — opens URL in Safari, records page source to
  `fixtures/<slug>.html`

## Modal Backend

The conversion backend runs on [Modal](https://modal.com). Deploy from the
`modal/` directory:

```
cd modal && modal deploy api.py
```

After deploying, warm containers may still run old code. Stop them:

```
modal container list --json | jq -r '.[].id' | xargs -I{} modal container stop {}
```

Stream logs with `modal app logs shelf-api`.

When deploying Modal apps, work in phases: (1) read all related source files
and verify interface consistency and no deprecated APIs, (2) deploy and capture
errors — if deployment fails, fix and retry, (3) regression-test deployed
endpoints with real inputs and diff outputs before considering it done.

## TUI Changes

For visual/UI changes, work in a build-verify loop: make the edit, build,
capture a terminal snapshot (e.g. via tmux capture-pane), read it back to
verify correctness (colors, alignment, no clipping, ANSI-aware widths), and
iterate until the snapshot matches the spec.

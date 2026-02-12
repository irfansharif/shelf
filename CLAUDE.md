# Development

## Modal backend

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

## Tests

Tests use [datadriven](https://github.com/cockroachdb/datadriven) test files
under `pkg/extractor/testdata/`.

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

## Configuration

Config lives at `~/.shelf/shelf.toml`:

```toml
endpoint = "https://irfansharif--shelf-api-converter-convert.modal.run"
data_dir = "~/path/to/articles"
```

Articles are stored as `articles/{slug}/index.md` with YAML front matter.

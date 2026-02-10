"""Shared utilities for HTML-to-Markdown post-processing."""

import base64
import os.path
import re
import textwrap
from datetime import datetime, timezone
from html.parser import HTMLParser
from urllib.parse import urlparse

# ---------------------------------------------------------------------------
# JS rendering fallback (Playwright)
# ---------------------------------------------------------------------------

_JS_REQUIRED_MARKERS = [
    "javascript is not available",
    "you need to enable javascript",
    "please enable javascript",
    "this app requires javascript",
    "requires javascript to run",
    "enable javascript to run this app",
    "javascript is required",
    "javascript must be enabled",
]


def needs_js_rendering(html):
    """Check if HTML suggests the page needs JavaScript to render content."""
    lower = html.lower()
    return any(marker in lower for marker in _JS_REQUIRED_MARKERS)


def fetch_with_js(url, timeout=30000):
    """Re-fetch a URL using a headless browser for JS-rendered pages."""
    import time

    from playwright.sync_api import sync_playwright

    print(f"[js-fallback] fetching with Playwright: {url}")
    t0 = time.perf_counter()
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page()
        page.goto(url, timeout=timeout)
        page.wait_for_timeout(3000)
        html = page.content()
        browser.close()
    elapsed = time.perf_counter() - t0
    print(f"[js-fallback] done in {elapsed:.1f}s ({len(html)} chars)")
    return html


def clean_html(html: str, *, strip_data_uris: bool = False) -> str:
    """Remove scripts, styles, and other non-content elements.

    When strip_data_uris is True, also strip base64-encoded images and
    collapse SVG elements to placeholders (useful for model-based
    conversion where data URIs waste tokens).
    """
    html = re.sub(r"<[ ]*script.*?/[ ]*script[ ]*>", "", html,
                  flags=re.IGNORECASE | re.MULTILINE | re.DOTALL)
    html = re.sub(r"<[ ]*style.*?/[ ]*style[ ]*>", "", html,
                  flags=re.IGNORECASE | re.MULTILINE | re.DOTALL)
    html = re.sub(r"<[ ]*meta.*?>", "", html,
                  flags=re.IGNORECASE | re.MULTILINE | re.DOTALL)
    html = re.sub(r"<[ ]*!--.*?--[ ]*>", "", html,
                  flags=re.IGNORECASE | re.MULTILINE | re.DOTALL)
    html = re.sub(r"<[ ]*link.*?>", "", html,
                  flags=re.IGNORECASE | re.MULTILINE | re.DOTALL)
    if strip_data_uris:
        html = re.sub(r'<img[^>]+src="data:image/[^;]+;base64,[^"]+"[^>]*>',
                      '<img src="#"/>', html)
        html = re.sub(r"(<svg[^>]*>)(.*?)(</svg>)",
                      r"\1\3", html, flags=re.DOTALL)
    return html


def fix_headings(html: str, markdown: str) -> str:
    """Strip hallucinated headings and re-inject real ones from source HTML.

    Readability strips heading tags, so the model never sees them. This
    function:
    1. Extracts real headings (with level and following-paragraph context)
       from the original HTML.
    2. Removes all model-generated markdown headings (since they're invented).
    3. Re-injects the real headings at the right positions by matching the
       following-paragraph context in the markdown.
    """

    # -- Step 1: Extract headings with context from original HTML. -----------
    class _HeadingContextExtractor(HTMLParser):
        """Extracts (level, heading_text, following_text_snippet, preceded_by_hr) tuples."""
        HEADING_TAGS = {"h1", "h2", "h3", "h4", "h5", "h6"}

        def __init__(self):
            super().__init__()
            self.headings = []  # [(level, text, following_snippet, preceded_by_hr)]
            self._in_heading = None
            self._heading_text = ""
            self._capture_after = False
            self._after_text = ""
            self._saw_hr = False

        def _flush_after(self):
            if self._capture_after and self.headings:
                lvl, txt, _, hr = self.headings[-1]
                snippet = " ".join(self._after_text.split())[:120]
                self.headings[-1] = (lvl, txt, snippet, hr)
            self._capture_after = False
            self._after_text = ""

        def handle_starttag(self, tag, attrs):
            if tag == "hr":
                self._saw_hr = True
            elif tag in self.HEADING_TAGS:
                self._flush_after()
                self._in_heading = tag
                self._heading_text = ""
            elif self._in_heading is None and self._capture_after:
                pass  # keep capturing

        def handle_endtag(self, tag):
            if tag == self._in_heading:
                text = self._heading_text.strip()
                if text:
                    level = int(tag[1])
                    self.headings.append((level, text, "", self._saw_hr))
                    self._capture_after = True
                    self._after_text = ""
                self._in_heading = None
                self._saw_hr = False

        def handle_data(self, data):
            if self._in_heading:
                self._heading_text += data
            elif self._capture_after:
                self._after_text += data
                if len(self._after_text) >= 120:
                    self._flush_after()

        def close(self):
            super().close()
            self._flush_after()

    headings = []
    try:
        p = _HeadingContextExtractor()
        p.feed(html)
        p.close()
        headings = p.headings
    except Exception as e:
        print(f"[heading-filter] failed to parse HTML: {e}")

    # Filter out nav/boilerplate headings (very short or generic).
    skip = {"ready for more?", "share", "subscribe", ""}
    headings = [(lvl, txt, ctx, hr) for lvl, txt, ctx, hr in headings
                if txt.lower().strip() not in skip and (lvl == 1 or len(ctx) > 20)]

    print(f"[heading-filter] found {len(headings)} content heading(s) in source HTML:")
    for lvl, txt, ctx, hr in headings:
        ctx_preview = ctx[:60] + "..." if len(ctx) > 60 else ctx
        print(f"  h{lvl}: {txt!r}  (hr={hr}, ctx: {ctx_preview!r})")

    # -- Step 2: Strip all model-generated headings. -------------------------
    lines = markdown.split("\n")
    stripped = []
    removed = []
    for line in lines:
        if re.match(r"^#{1,6}\s+", line):
            removed.append(line)
        else:
            stripped.append(line)

    if removed:
        print(f"[heading-filter] stripped {len(removed)} model-generated heading(s):")
        for h in removed:
            print(f"  - {h}")

    if not headings:
        print("[heading-filter] no source headings to inject")
        return "\n".join(stripped)

    # -- Step 3: Re-inject real headings at matched positions. ---------------
    text = "\n".join(stripped)
    injected = 0

    # h1 is the article title — prepend it directly at the top (context
    # matching won't work because the text following h1 in the full-page HTML
    # is typically nav/byline content that readability strips out).
    h1_headings = [(lvl, txt, ctx, hr) for lvl, txt, ctx, hr in headings if lvl == 1]
    if h1_headings:
        _, h1_text, _, _ = h1_headings[0]
        text = f"# {h1_text}\n\n{text.lstrip()}"
        injected += 1

    for lvl, heading_text, context_snippet, preceded_by_hr in reversed(headings):
        if lvl == 1:
            continue  # already handled above
        # Normalize the context for fuzzy searching in the markdown.
        ctx_words = context_snippet.split()[:12]
        if len(ctx_words) < 3:
            continue
        # Build a regex pattern from the first several words (allowing minor
        # whitespace/punctuation differences).
        pattern_parts = [re.escape(w) for w in ctx_words]
        pattern = r"\s+".join(pattern_parts)
        m = re.search(pattern, text, re.IGNORECASE)
        if m:
            prefix = "#" * lvl
            hr_line = "---\n\n" if preceded_by_hr else ""
            heading_line = f"\n{hr_line}{prefix} {heading_text}\n"
            # Insert heading before the matched context.
            pos = m.start()
            # Back up to the start of the line.
            line_start = text.rfind("\n", 0, pos)
            if line_start == -1:
                line_start = 0
            text = text[:line_start] + "\n" + heading_line + text[line_start:]
            injected += 1
        else:
            print(f"[heading-filter] could not locate context for h{lvl}: {heading_text!r}")

    print(f"[heading-filter] injected {injected}/{len(headings)} heading(s)")
    # Collapse runs of blank lines left by stripped headings.
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text


def postprocess(markdown: str) -> str:
    """Normalize quotes, strip code fences, and wrap lines at 100 chars."""

    # Normalize curly quotes/apostrophes to straight ones.
    markdown = _normalize_quotes(markdown)

    # Strip ```markdown / ``` fences.
    markdown = re.sub(r"^```\s*(?:markdown)?\s*\n?", "", markdown)
    markdown = re.sub(r"\n?```\s*$", "", markdown)

    # Collapse runs of 3+ newlines into 2 (single blank line).
    markdown = re.sub(r"\n{3,}", "\n\n", markdown)

    # Re-join soft-wrapped paragraph lines before re-wrapping.
    # Conversion tools (or HTML source whitespace) may break paragraphs into
    # short lines; joining them lets the wrapping below target the correct
    # display width.  Only consecutive non-indented, non-structural lines are
    # joined — headings, lists, blockquotes, code fences, etc. are preserved.
    _structural_re = re.compile(
        r'^(?:'
        r'#{1,6}\s'        # heading
        r'|---+\s*$'       # horizontal rule
        r'|>'              # blockquote
        r'|\|'             # table
        r'|!\['            # image
        r'|```'            # code fence
        r'|\s*[-*+]\s+'   # unordered list
        r'|\s*\d+[.)]\s+' # ordered list
        r')'
    )
    # Structural patterns for content inside blockquotes (no blockquote marker).
    _bq_inner_structural_re = re.compile(
        r'^(?:'
        r'#{1,6}\s'        # heading
        r'|---+\s*$'       # horizontal rule
        r'|>'              # nested blockquote
        r'|\|'             # table
        r'|!\['            # image
        r'|```'            # code fence
        r'|\s*[-*+]\s+'   # unordered list
        r'|\s*\d+[.)]\s+' # ordered list
        r')'
    )
    rejoined = []
    para_buf = []
    bq_buf = []
    in_code_fence = False
    for line in markdown.split("\n"):
        stripped = line.strip()
        if stripped.startswith('```'):
            if bq_buf:
                rejoined.append("> " + " ".join(bq_buf))
                bq_buf = []
            if para_buf:
                rejoined.append(" ".join(para_buf))
                para_buf = []
            rejoined.append(line)
            in_code_fence = not in_code_fence
        elif in_code_fence:
            rejoined.append(line)
        elif re.match(r'^> ?', line):
            # Blockquote line — join consecutive paragraph lines within.
            if para_buf:
                rejoined.append(" ".join(para_buf))
                para_buf = []
            inner = re.match(r'^> ?(.*)', line).group(1)
            if inner.strip() and not _bq_inner_structural_re.match(inner):
                bq_buf.append(inner)
            else:
                if bq_buf:
                    rejoined.append("> " + " ".join(bq_buf))
                    bq_buf = []
                rejoined.append(line)
        elif stripped and not line[0].isspace() and not _structural_re.match(line):
            if bq_buf:
                rejoined.append("> " + " ".join(bq_buf))
                bq_buf = []
            para_buf.append(line)
        else:
            if bq_buf:
                rejoined.append("> " + " ".join(bq_buf))
                bq_buf = []
            if para_buf:
                rejoined.append(" ".join(para_buf))
                para_buf = []
            rejoined.append(line)
    if bq_buf:
        rejoined.append("> " + " ".join(bq_buf))
    if para_buf:
        rejoined.append(" ".join(para_buf))
    markdown = "\n".join(rejoined)

    # Wrap text body at 100 chars (display width).
    # With vim conceallevel=2, [text](url) displays as just "text" and
    # bold/italic markers (**,*) are hidden. Replace each markdown link with
    # a placeholder whose width equals the displayed link text length, so
    # textwrap targets the visual width rather than the raw character count.
    link_re = re.compile(r"\[([^\]]*)\]\([^)]*\)")
    wrapped_lines = []
    for line in markdown.split("\n"):
        # Don't wrap headings, HRs, blank lines, or tables.
        if (
            not line.strip()
            or re.match(r"^#{1,6}\s+", line)
            or re.match(r"^---+\s*$", line)
            or re.match(r"^\|", line)
            or re.match(r"^!\[", line)
            or len(line) <= 100
        ):
            wrapped_lines.append(line)
        elif re.match(r"^> ?", line):
            # Wrap blockquote content, preserving the > prefix on each line.
            bq_m = re.match(r"^(> ?)", line)
            prefix = "> "
            inner = line[len(bq_m.group(1)):]

            placeholders = {}

            def _replace_bq(m, _ph=placeholders):
                idx = len(_ph)
                link_text = m.group(1)
                full_link = m.group(0)
                display_text = re.sub(r'\*+|_+', '', link_text)
                display_len = max(len(display_text), 1)
                key = f"\x00{idx}\x00".ljust(display_len, "\x01")
                _ph[key] = full_link
                return key

            masked = link_re.sub(_replace_bq, inner)
            marker_chars = sum(len(m) for m in re.findall(r'\*+|_+', masked))
            effective_width = 100 - len(prefix) + marker_chars

            wrapped = textwrap.wrap(
                masked,
                width=effective_width,
                break_long_words=False,
                break_on_hyphens=False,
            )
            for i, wl in enumerate(wrapped):
                for key, original in placeholders.items():
                    wl = wl.replace(key, original)
                wrapped[i] = wl
            wrapped_lines.extend(prefix + wl for wl in wrapped)
        else:
            # Detect list items to compute continuation indent.
            list_m = re.match(r"^(\s*[\-\*\+]\s+|\s*\d+[.)]\s+)", line)
            cont_indent = " " * len(list_m.group(0)) if list_m else ""

            placeholders = {}

            def _replace(m, _ph=placeholders):
                idx = len(_ph)
                link_text = m.group(1)
                full_link = m.group(0)
                # Strip bold/italic markers for true display width.
                display_text = re.sub(r'\*+|_+', '', link_text)
                display_len = max(len(display_text), 1)
                key = f"\x00{idx}\x00".ljust(display_len, "\x01")
                _ph[key] = full_link
                return key

            masked = link_re.sub(_replace, line)

            # Bold/italic markers are also concealed — widen the target
            # so they don't eat into the visible 100-char budget.
            marker_chars = sum(len(m) for m in re.findall(r'\*+|_+', masked))
            effective_width = 100 + marker_chars

            wrapped = textwrap.wrap(
                masked,
                width=effective_width,
                break_long_words=False,
                break_on_hyphens=False,
                subsequent_indent=cont_indent,
            )
            for i, wl in enumerate(wrapped):
                for key, original in placeholders.items():
                    wl = wl.replace(key, original)
                wrapped[i] = wl
            wrapped_lines.extend(wrapped)
    markdown = "\n".join(line.rstrip() for line in wrapped_lines)

    # Final trim.
    markdown = markdown.strip() + "\n"
    return markdown


_MARKDOWN_IMAGE_RE = re.compile(r"!\[([^\]]*)\]\(([^)]+)\)")

IMAGE_HEADERS = {
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Accept": "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
}


def _local_filename(raw_url, used_names):
    """Generate a sanitized, deduplicated local filename from a URL.

    Port of Go's images.go:localFilename().
    """
    try:
        parsed = urlparse(raw_url)
        base = os.path.basename(parsed.path)
    except Exception:
        base = ""

    if not base or base in (".", "/"):
        base = "image.png"

    # Sanitize: keep only alphanumeric, hyphens, underscores, dots.
    name = re.sub(r"[^a-zA-Z0-9._-]", "", base)
    if not name:
        name = "image.png"

    # Ensure it has an extension.
    if not os.path.splitext(name)[1]:
        name += ".png"

    # Deduplicate.
    if name not in used_names:
        return name
    stem, ext = os.path.splitext(name)
    i = 2
    while True:
        candidate = f"{stem}-{i}{ext}"
        if candidate not in used_names:
            return candidate
        i += 1


def download_images(markdown):
    """Download remote images referenced in markdown.

    Returns (rewritten_markdown, [{"path": "images/filename", "data": base64_str}]).
    Failed downloads keep the original remote URL.
    """
    from curl_cffi import requests as cffi_requests

    matches = list(_MARKDOWN_IMAGE_RE.finditer(markdown))
    if not matches:
        return markdown, []

    # Collect unique remote URLs.
    used_names = set()
    seen = {}  # url -> local filename
    remote_urls = []

    for m in matches:
        url = m.group(2)
        if not (url.startswith("http://") or url.startswith("https://")):
            continue
        if url in seen:
            continue
        filename = _local_filename(url, used_names)
        used_names.add(filename)
        seen[url] = filename
        remote_urls.append(url)

    if not remote_urls:
        return markdown, []

    # Download each image.
    downloaded = {}  # url -> base64 data
    for url in remote_urls:
        try:
            resp = cffi_requests.get(
                url,
                headers=IMAGE_HEADERS,
                timeout=30,
                impersonate="chrome",
            )
            if resp.status_code == 200 and resp.content:
                downloaded[url] = base64.b64encode(resp.content).decode("ascii")
                print(f"[images] downloaded {seen[url]} ({len(resp.content)} bytes)")
            else:
                print(f"[images] failed {url}: HTTP {resp.status_code}")
        except Exception as e:
            print(f"[images] failed {url}: {e}")

    if not downloaded:
        return markdown, []

    # Build image list.
    image_list = []
    seen_paths = set()
    for url, b64 in downloaded.items():
        path = f"images/{seen[url]}"
        if path not in seen_paths:
            image_list.append({"path": path, "data": b64})
            seen_paths.add(path)

    # Rewrite markdown references (process in reverse to preserve indices).
    result = markdown
    for m in reversed(matches):
        url = m.group(2)
        if url not in downloaded:
            continue
        alt = m.group(1)
        filename = seen[url]
        if not alt:
            alt = os.path.splitext(filename)[0]
        replacement = f"![{alt}](images/{filename})"
        result = result[:m.start()] + replacement + result[m.end():]

    return result, image_list


_YAML_SPECIAL_RE = re.compile(r"[:#{}[\]&*!|>'\"%@`]")


def _escape_yaml(s):
    """Quote a YAML string value if it contains special characters."""
    if _YAML_SPECIAL_RE.search(s) or s.startswith("-"):
        return '"' + s.replace('"', '\\"') + '"'
    return s


def _normalize_quotes(s):
    """Replace curly quotes/apostrophes with straight ones."""
    return s.replace("\u2018", "'").replace("\u2019", "'").replace("\u201c", '"').replace("\u201d", '"')


def format_article(title, author, source, markdown):
    """Generate complete index.md content with YAML front matter."""
    title = _normalize_quotes(title)
    author = _normalize_quotes(author)
    saved = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    lines = ["---"]
    lines.append(f"title: {_escape_yaml(title)}")
    lines.append(f"author: {_escape_yaml(author)}")
    lines.append(f"source: {source}")
    lines.append(f"saved: {saved}")
    lines.append("tags:")
    lines.append("---")
    lines.append("")
    lines.append(markdown.rstrip("\n"))
    lines.append("")
    return "\n".join(lines)

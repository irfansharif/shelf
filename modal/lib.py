"""Shared utilities for HTML-to-Markdown post-processing."""

import base64
import os.path
import re
import textwrap
from datetime import datetime, timezone
from html import unescape
from urllib.parse import urlparse

# ---------------------------------------------------------------------------
# HTML fetching and metadata extraction
# ---------------------------------------------------------------------------

def fetch_html(url, timeout=30):
    """Fetch HTML using curl_cffi with browser TLS impersonation."""
    from curl_cffi import requests as curl_requests

    resp = curl_requests.get(
        url,
        impersonate="chrome",
        timeout=timeout,
        headers={
            "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
            "Accept-Language": "en-US,en;q=0.9",
        },
        allow_redirects=True,
    )
    resp.raise_for_status()
    return resp.text


def extract_article_html(raw_html):
    """Extract article HTML from archive.is snapshots.

    Archive.is renders pages into deeply nested divs with inline CSS
    grid layout that fragments content across sibling containers.
    Readability's scoring cannot reassemble these fragments, so we
    bypass it entirely: find the <article> element, clean it, and
    return its HTML for direct markdownify conversion.

    Returns the cleaned HTML string, or None if this isn't an
    archive.is page (caller falls back to readability).
    """
    import lxml.html

    # Only activate for archive.is snapshots (identified by their
    # characteristic wrapper div IDs).
    if 'id="SOLID"' not in raw_html or 'id="CONTENT"' not in raw_html:
        return None

    tree = lxml.html.fromstring(raw_html)
    article = tree.find('.//article')
    if article is None:
        return None

    # Remove display:none elements *safely*: preserve .tail text.
    # Archive.is uses display:none spans for drop-caps whose .tail
    # holds the actual paragraph text.
    for el in article.xpath('.//*[contains(@style, "display:none")]'):
        parent = el.getparent()
        if parent is None:
            continue
        tail = el.tail or ""
        prev = el.getprevious()
        if prev is not None:
            prev.tail = (prev.tail or "") + tail
        else:
            parent.text = (parent.text or "") + tail
        parent.remove(el)

    # Strip non-content elements.
    for tag in ("button", "svg", "aside", "nav", "footer", "header"):
        for el in article.findall(f".//{tag}"):
            parent = el.getparent()
            if parent is not None:
                parent.remove(el)

    return lxml.html.tostring(article, encoding="unicode")


def article_fallback_html(raw_html):
    """Extract the largest <article> element as a fallback.

    Used when readability mis-scores a page and captures only a fraction
    of the content (e.g. Chatham House splits article body across sibling
    divs).  Returns ``(html_str, text_length)`` or ``(None, 0)`` if no
    suitable element is found.
    """
    import lxml.html

    tree = lxml.html.fromstring(raw_html)
    articles = tree.findall('.//article')
    if not articles:
        return None, 0

    article = max(articles, key=lambda el: len(el.text_content()))
    text_len = len(article.text_content().strip())
    if text_len < 200:
        return None, 0

    for tag in ("button", "svg", "aside", "nav", "footer", "header"):
        for el in article.findall(f".//{tag}"):
            parent = el.getparent()
            if parent is not None:
                parent.remove(el)

    return lxml.html.tostring(article, encoding="unicode"), text_len


def extract_metadata(raw_html):
    """Extract title and author from HTML meta tags and headings.

    Title priority: <h1> > og:title > <title>.
    """
    title_m = re.search(r"(?is)<title[^>]*>(.*?)</title>", raw_html)
    title = unescape(title_m.group(1).strip()) if title_m else ""
    og_m = re.search(
        r'(?i)<meta[^>]+property=["\']og:title["\'][^>]+content=["\']([^"\']+)["\']',
        raw_html,
    ) or re.search(
        r'(?i)<meta[^>]+content=["\']([^"\']+)["\'][^>]+property=["\']og:title["\']',
        raw_html,
    )
    if og_m:
        og_title = unescape(og_m.group(1).strip())
        if og_title:
            title = og_title
    # Find the first h1 whose content isn't entirely a link (nav/masthead
    # h1 tags are typically <h1><a href="/">Site Name</a></h1>).
    for h1_m in re.finditer(r"(?is)<h1[^>]*>(.*?)</h1>", raw_html):
        inner = h1_m.group(1).strip()
        if re.match(r"(?is)^\s*<a\s[^>]*>.*?</a>\s*$", inner):
            continue
        h1_text = unescape(re.sub(r"<[^>]+>", "", inner).strip())
        if h1_text:
            title = h1_text
            break
    # Discard garbage titles from SPA shells / error pages.
    _garbage_titles = {"javascript is not available", "just a moment", "attention required"}
    if title.lower().rstrip(".") in _garbage_titles:
        title = ""

    author_m = re.search(
        r'(?i)<meta[^>]+name=["\']author["\'][^>]+content=["\']([^"\']+)["\']',
        raw_html,
    )
    author = unescape(author_m.group(1).strip()) if author_m else ""
    return title, author


_LINK_RE = re.compile(r"\[([^\]]*)\]\([^)]*\)")


def _mask_links(text):
    """Replace ``](url)`` with ``\\x01``, leaving ``[`` and link text intact.

    Returns ``(masked_text, urls)`` where *urls* is the list of
    ``](url)`` strings in left-to-right order.  After ``textwrap``
    wraps the masked text, call `_restore_links` to put the URLs back.
    """
    urls = []

    def _sub(m, _urls=urls):
        _urls.append(m.group(0)[1 + len(m.group(1)):])   # ](url)
        return "[" + m.group(1) + "\x01"

    return _LINK_RE.sub(_sub, text), urls


def _restore_links(lines, urls):
    """Replace each ``\\x01`` sentinel with the corresponding ``](url)``.

    Sentinels are consumed left-to-right across all *lines*.
    """
    url_iter = iter(urls)
    out = []
    for line in lines:
        parts = []
        for ch in line:
            if ch == "\x01":
                parts.append(next(url_iter))
            else:
                parts.append(ch)
        out.append("".join(parts))
    return out


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
    list_buf = []        # accumulates list item + continuation lines
    list_cont_indent = ""  # whitespace prefix for continuation lines
    in_code_fence = False

    def _flush_list():
        nonlocal list_buf, list_cont_indent
        if list_buf:
            rejoined.append(" ".join(list_buf))
            list_buf = []
            list_cont_indent = ""

    for line in markdown.split("\n"):
        stripped = line.strip()
        if stripped.startswith('```'):
            if bq_buf:
                rejoined.append("> " + " ".join(bq_buf))
                bq_buf = []
            if para_buf:
                rejoined.append(" ".join(para_buf))
                para_buf = []
            _flush_list()
            rejoined.append(line)
            in_code_fence = not in_code_fence
        elif in_code_fence:
            rejoined.append(line)
        elif re.match(r'^> ?', line):
            # Blockquote line — join consecutive paragraph lines within.
            if para_buf:
                rejoined.append(" ".join(para_buf))
                para_buf = []
            _flush_list()
            inner = re.match(r'^> ?(.*)', line).group(1)
            if inner.strip() and not _bq_inner_structural_re.match(inner):
                bq_buf.append(inner)
            else:
                if bq_buf:
                    rejoined.append("> " + " ".join(bq_buf))
                    bq_buf = []
                rejoined.append(line)
        else:
            list_m = re.match(r'^(\s*[\-\*\+]\s+|\s*\d+[.)]\s+)', line)
            if list_m:
                # New list item — flush previous buffers, start list accumulation.
                if bq_buf:
                    rejoined.append("> " + " ".join(bq_buf))
                    bq_buf = []
                if para_buf:
                    rejoined.append(" ".join(para_buf))
                    para_buf = []
                _flush_list()
                list_buf = [line]
                list_cont_indent = " " * len(list_m.group(0))
            elif (list_buf and stripped
                  and line.startswith(list_cont_indent)
                  and not _structural_re.match(stripped)):
                # Continuation of current list item (indented plain text).
                list_buf.append(stripped)
            elif stripped and not line[0].isspace() and not _structural_re.match(line):
                if bq_buf:
                    rejoined.append("> " + " ".join(bq_buf))
                    bq_buf = []
                _flush_list()
                para_buf.append(line)
            else:
                if bq_buf:
                    rejoined.append("> " + " ".join(bq_buf))
                    bq_buf = []
                if para_buf:
                    rejoined.append(" ".join(para_buf))
                    para_buf = []
                _flush_list()
                rejoined.append(line)
    if bq_buf:
        rejoined.append("> " + " ".join(bq_buf))
    if para_buf:
        rejoined.append(" ".join(para_buf))
    _flush_list()
    markdown = "\n".join(rejoined)

    # Wrap text body at 100 raw chars.
    #
    # We replace ](url) with a 1-char sentinel (\x01), leaving [ and the
    # link text words in place so textwrap can break between words inside
    # link text (valid CommonMark).  After wrapping we restore each \x01
    # to its ](url) in left-to-right order.
    wrapped_lines = []
    for line in markdown.split("\n"):
        # Don't wrap headings, HRs, blank lines, tables, or images.
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

            masked, urls = _mask_links(inner)
            wrapped = textwrap.wrap(
                masked,
                width=100 - len(prefix),
                break_long_words=False,
                break_on_hyphens=False,
            )
            wrapped = _restore_links(wrapped, urls)
            wrapped_lines.extend(prefix + wl for wl in wrapped)
        else:
            # Detect list items to compute continuation indent.
            list_m = re.match(r"^(\s*[\-\*\+]\s+|\s*\d+[.)]\s+)", line)
            cont_indent = " " * len(list_m.group(0)) if list_m else ""

            masked, urls = _mask_links(line)
            wrapped = textwrap.wrap(
                masked,
                width=100,
                break_long_words=False,
                break_on_hyphens=False,
                subsequent_indent=cont_indent,
            )
            wrapped = _restore_links(wrapped, urls)
            wrapped_lines.extend(wrapped)

    markdown = "\n".join(line.rstrip() for line in wrapped_lines)

    # Final trim.
    markdown = markdown.strip() + "\n"
    return markdown


_MARKDOWN_IMAGE_RE = re.compile(r"!\[([^\]]*)\]\(([^)]+)\)")



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
    from curl_cffi import requests as curl_requests

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
            resp = curl_requests.get(url, impersonate="chrome", timeout=30)
            data = resp.content
            if data:
                downloaded[url] = base64.b64encode(data).decode("ascii")
                print(f"[images] downloaded {seen[url]} ({len(data)} bytes)")
            else:
                print(f"[images] failed {url}: empty response")
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
    lines.append("progress:")
    lines.append("---")
    lines.append("")
    lines.append(markdown.rstrip("\n"))
    lines.append("")
    return "\n".join(lines)


def build_result(result, url):
    """Download images and format article from a conversion result dict."""
    markdown, images = download_images(result["markdown"])
    content = format_article(result["title"], result["author"], url, markdown)
    return {"title": result["title"], "content": content, "images": images}

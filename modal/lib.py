"""Shared utilities for HTML-to-Markdown post-processing."""

import re
import textwrap
from html.parser import HTMLParser


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
    markdown = markdown.replace("\u2018", "'").replace("\u2019", "'")
    markdown = markdown.replace("\u201c", '"').replace("\u201d", '"')

    # Strip ```markdown / ``` fences.
    markdown = re.sub(r"^```\s*(?:markdown)?\s*\n?", "", markdown)
    markdown = re.sub(r"\n?```\s*$", "", markdown)

    # Collapse runs of 3+ newlines into 2 (single blank line).
    markdown = re.sub(r"\n{3,}", "\n\n", markdown)

    # Wrap text body at 100 chars (display width).
    # With vim conceallevel=2, [text](url) displays as just "text" and
    # bold/italic markers (**,*) are hidden. Use the link text length as the
    # placeholder width so wrapping targets the displayed width.
    #
    # To prevent very long raw lines when URLs are long, cap the amount of
    # concealment per link to MAX_CONCEAL chars. Any URL syntax beyond that
    # budget still counts toward the line width, causing wrapping to kick in.
    MAX_CONCEAL = 50
    link_re = re.compile(r"\[([^\]]*)\]\([^)]*\)")
    wrapped_lines = []
    for line in markdown.split("\n"):
        # Don't wrap headings, HRs, blank lines, blockquotes, or tables.
        if (
            not line.strip()
            or re.match(r"^#{1,6}\s+", line)
            or re.match(r"^---+\s*$", line)
            or re.match(r"^>", line)
            or re.match(r"^\|", line)
            or re.match(r"^!\[", line)
            or len(line) <= 100
        ):
            wrapped_lines.append(line)
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
                # Cap concealment so very long URLs still trigger wrapping.
                concealed = len(full_link) - display_len
                effective_len = display_len + max(0, concealed - MAX_CONCEAL)
                key = f"\x00{idx}\x00".ljust(effective_len, "\x01")
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

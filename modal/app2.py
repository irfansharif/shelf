"""
Modal app using Jina Reader for URL-to-Markdown conversion (CPU only).
"""

import modal

from lib import fix_headings, postprocess

MINUTES = 60

app = modal.App("browser2")

image = modal.Image.debian_slim(python_version="3.12")


def _clean_html(html: str) -> str:
    """Remove scripts, styles, and other non-content elements."""
    import re
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
    return html


def clean_jina_output(raw: str) -> str:
    """Strip Jina Reader metadata and artifacts."""
    import re

    lines = raw.split("\n")

    # Extract title from metadata header.
    title = ""
    for line in lines:
        if line.startswith("Title: "):
            title = line[7:].strip()
            break

    # Strip metadata header (everything up to and including "Markdown Content:").
    start = 0
    for i, line in enumerate(lines):
        if line.startswith("Markdown Content:"):
            start = i + 1
            break
    markdown = "\n".join(lines[start:])

    # Remove empty anchor links [](url) (standalone or inline).
    markdown = re.sub(r"\[\]\([^)]+\)", "", markdown)

    # Strip image blocks (standalone ![...](url) and linked [![...](url)](url))
    # along with their following caption lines.
    mlines = markdown.split("\n")
    filtered = []
    skip_until = -1
    for i, line in enumerate(mlines):
        if i <= skip_until:
            continue
        stripped_line = line.strip()
        if stripped_line.startswith("![") or stripped_line.startswith("[!["):
            # Skip the image line + optional blank + caption.
            skip_until = i
            if i + 1 < len(mlines) and mlines[i + 1].strip() == "":
                skip_until = i + 1
                if (i + 2 < len(mlines)
                        and mlines[i + 2].strip()
                        and not mlines[i + 2].startswith(
                            ("#", "!", "[", "*", "-", "`", ">", "|"))):
                    skip_until = i + 2
            continue
        filtered.append(line)
    markdown = "\n".join(filtered)

    # Remove indented figure caption lines (" Figure N. ...").
    markdown = re.sub(r"^ +Figure \d+\..*$", "", markdown, flags=re.MULTILINE)

    # Remove plain-text caption lines that duplicate image alt text.
    # Pattern: a text line followed by blank line followed by ![...
    mlines = markdown.split("\n")
    cleaned = []
    for i, line in enumerate(mlines):
        if (
            i + 2 < len(mlines)
            and mlines[i + 1].strip() == ""
            and mlines[i + 2].startswith("![")
            and line.strip()
            and not line.startswith(("#", "!", "[", "*", "-", "`", ">", "|", " "))
        ):
            continue
        cleaned.append(line)
    markdown = "\n".join(cleaned)

    # Strip stray footnote reference numbers (e.g. "1972.3 IC" -> "1972. IC").
    markdown = re.sub(r"(\w\.)(\d{1,2})(\s+[A-Z])", r"\1\3", markdown)

    # Ensure space before markdown links when preceded by a word character.
    markdown = re.sub(r"(\w)\[([^\]]+)\]\(", r"\1 [\2](", markdown)
    # Ensure space after markdown links when followed by a word character.
    markdown = re.sub(r"\]\(([^)]*)\)(\w)", r"](\1) \2", markdown)

    # Strip hero/nav content before article body.
    m = re.search(r"^[_*]This was published", markdown, re.MULTILINE)
    if m:
        markdown = markdown[m.start() :]

    # Prepend title as h1.
    if title:
        markdown = f"# {title}\n\n{markdown}"

    # Fix setext-style h2: [Text](url)\n---+ -> ## Text
    markdown = re.sub(
        r"\[([^\]]+)\]\([^)]+\)\n-{3,}",
        lambda m: f"## {m.group(1)}",
        markdown,
    )

    # Fix ATX headings wrapped in links: ### [Text](url) -> ### Text
    markdown = re.sub(
        r"^(#{1,6})\s+\[([^\]]+)\]\([^)]+\)",
        r"\1 \2",
        markdown,
        flags=re.MULTILINE,
    )

    # * * * -> ---
    markdown = re.sub(r"^\* \* \*$", "---", markdown, flags=re.MULTILINE)

    # * list items -> - list items
    markdown = re.sub(r"^\*\s{1,3}", "- ", markdown, flags=re.MULTILINE)

    # Deduplicate consecutive identical paragraphs.
    paras = re.split(r"\n\n+", markdown)
    deduped = []
    for p in paras:
        if not deduped or p.strip() != deduped[-1].strip():
            deduped.append(p)
    markdown = "\n\n".join(deduped)

    # Collapse runs of blank lines.
    markdown = re.sub(r"\n{3,}", "\n\n", markdown)

    return markdown.strip()


@app.cls(
    image=image,
    scaledown_window=5 * MINUTES,
    timeout=2 * MINUTES,
    max_containers=1,
)
class ReaderLM:
    def _convert(self, url: str) -> str:
        import time
        import urllib.request

        t0 = time.perf_counter()

        # Fetch raw HTML for heading extraction.
        html_req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(html_req, timeout=60) as resp:
            raw_html = resp.read().decode("utf-8")
        t_html = time.perf_counter()

        # Fetch Jina Reader markdown.
        jina_req = urllib.request.Request(
            f"https://r.jina.ai/{url}",
            headers={"Accept": "text/markdown", "User-Agent": "Mozilla/5.0"},
        )
        with urllib.request.urlopen(jina_req, timeout=60) as resp:
            raw = resp.read().decode("utf-8")
        t_fetch = time.perf_counter()

        cleaned = clean_jina_output(raw)
        cleaned = fix_headings(_clean_html(raw_html), cleaned)
        result = postprocess(cleaned)
        t_done = time.perf_counter()

        print(
            f"[instrumentation] html_fetch={t_html - t0:.3f}s, "
            f"jina_fetch={t_fetch - t_html:.3f}s, "
            f"postprocess={t_done - t_fetch:.3f}s, total={t_done - t0:.3f}s"
        )
        return result

    @modal.fastapi_endpoint(method="POST")
    def convert(self, data: dict):
        from fastapi.responses import JSONResponse

        try:
            return self._convert(data["url"])
        except Exception as e:
            import traceback

            traceback.print_exc()
            return JSONResponse(
                status_code=500,
                content={"error": str(e), "type": type(e).__name__},
            )

    @modal.method()
    def url_to_markdown(self, url: str) -> str:
        return self._convert(url)

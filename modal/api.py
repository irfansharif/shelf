"""
API-based URL-to-Markdown conversion (CPU only).

Fetches HTML via curl_cffi (browser TLS fingerprinting) and converts
to markdown using readability + markdownify.
"""

import modal

MINUTES = 60

app = modal.App("shelf-api")

image = (
    modal.Image.debian_slim(python_version="3.12")
    .pip_install(
        "curl_cffi",
        "readability-lxml", "lxml[html_clean]", "markdownify",
    )
    .add_local_file("lib.py", "/root/lib.py")
)

BROWSER_HEADERS = {
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "Accept-Language": "en-US,en;q=0.9",
}


@app.cls(
    image=image,
    scaledown_window=5 * MINUTES,
    timeout=2 * MINUTES,
    max_containers=1,
)
class Converter:
    def _convert(self, url: str) -> dict:
        import re
        import time
        from html import unescape

        from curl_cffi import requests as cffi_requests
        from markdownify import markdownify
        from readability import Document

        from lib import clean_html, fix_headings, postprocess

        t0 = time.perf_counter()

        # Fetch raw HTML.
        resp = cffi_requests.get(
            url,
            headers=BROWSER_HEADERS,
            timeout=60,
            impersonate="chrome",
        )
        resp.raise_for_status()
        raw_html = resp.text
        t_html = time.perf_counter()

        # Extract metadata.
        title_m = re.search(r"(?is)<title[^>]*>(.*?)</title>", raw_html)
        title = unescape(title_m.group(1).strip()) if title_m else ""
        author_m = re.search(
            r'(?i)<meta[^>]+name=["\']author["\'][^>]+content=["\']([^"\']+)["\']',
            raw_html,
        )
        author = unescape(author_m.group(1).strip()) if author_m else ""

        # Extract article and convert to markdown.
        doc = Document(raw_html)
        article_html = doc.summary()
        markdown = markdownify(article_html, heading_style="ATX")
        heading = title or doc.short_title()
        if heading:
            markdown = f"# {heading}\n\n{markdown}"
        t_convert = time.perf_counter()

        # Post-process: inject real headings from source HTML, normalize.
        cleaned_html = clean_html(raw_html)
        markdown = fix_headings(cleaned_html, markdown)
        result = postprocess(markdown)
        t_done = time.perf_counter()

        print(
            f"[instrumentation] html_fetch={t_html - t0:.3f}s, "
            f"convert={t_convert - t_html:.3f}s, "
            f"postprocess={t_done - t_convert:.3f}s, total={t_done - t0:.3f}s"
        )
        return {"title": title, "author": author, "markdown": result}

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
    def url_to_markdown(self, url: str) -> dict:
        return self._convert(url)

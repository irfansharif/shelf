"""
API-based URL-to-Markdown conversion (CPU only).

Fetches HTML via Playwright (headless Chromium) and converts to
markdown using readability + markdownify.
"""

import modal

MINUTES = 60

app = modal.App("shelf-api")

image = (
    modal.Image.debian_slim(python_version="3.12")
    .pip_install(
        "readability-lxml", "lxml[html_clean]", "markdownify",
        "playwright",
    )
    .run_commands("playwright install chromium --with-deps")
    .add_local_file("lib.py", "/root/lib.py")
)


@app.cls(
    image=image,
    scaledown_window=5 * MINUTES,
    timeout=5 * MINUTES,
    max_containers=10,
    max_inputs=1,
)
class Converter:
    def _extract(self, raw_html: str):
        """Run readability + markdownify on raw HTML."""
        from markdownify import markdownify
        from readability import Document

        from lib import extract_metadata

        title, author = extract_metadata(raw_html)
        doc = Document(raw_html)
        article_html = doc.summary()
        markdown = markdownify(article_html, heading_style="ATX")
        heading = title or doc.short_title()
        if heading:
            markdown = f"# {heading}\n\n{markdown}"
        return title, author, markdown

    def _convert(self, url: str) -> dict:
        import time

        from lib import fetch_html, postprocess

        t0 = time.perf_counter()

        # Fetch raw HTML.
        raw_html = fetch_html(url)
        t_html = time.perf_counter()

        title, author, markdown = self._extract(raw_html)
        t_convert = time.perf_counter()

        # If readability extracted very little content, the page is likely a
        # JS-rendered SPA (e.g. X.com). Re-fetch with a longer timeout to
        # let the page fully render.
        content_len = len(markdown.strip())
        if content_len < 500:
            print(f"[convert] only {content_len} chars extracted, retrying with longer timeout")
            raw_html = fetch_html(url, timeout=60000)
            title, author, markdown = self._extract(raw_html)
            t_convert = time.perf_counter()

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

        from lib import build_result

        try:
            url = data["url"]
            result = self._convert(url)
            return build_result(result, url)
        except Exception as e:
            import traceback

            traceback.print_exc()
            return JSONResponse(
                status_code=500,
                content={"error": str(e), "type": type(e).__name__},
            )

    @modal.method()
    def url_to_markdown(self, url: str) -> dict:
        from lib import build_result

        result = self._convert(url)
        return build_result(result, url)

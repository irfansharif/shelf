"""
API-based URL-to-Markdown conversion (CPU only).

Fetches HTML via curl_cffi (browser TLS impersonation) and converts to
markdown using readability + markdownify.
"""

import modal

app = modal.App("shelf-api")

image = (
    modal.Image.debian_slim(python_version="3.12")
    .pip_install(
        "readability-lxml", "lxml[html_clean]", "markdownify",
        "curl_cffi",
    )
    .add_local_file("lib.py", "/root/lib.py")
)


@app.cls(
    image=image,
    scaledown_window=5 * 60,
    timeout=60,
    max_containers=10,
    max_inputs=1,
)
class Converter:
    def _extract(self, raw_html: str):
        """Run readability + markdownify on raw HTML."""
        from markdownify import markdownify
        from readability import Document

        from lib import extract_article_html, extract_metadata

        title, author = extract_metadata(raw_html)

        # Prefer semantic <article> element (avoids readability mis-scoring
        # on archive.is and similar deeply-nested pages).
        article_html = extract_article_html(raw_html)
        if article_html is not None:
            markdown = markdownify(article_html, heading_style="ATX")
        else:
            doc = Document(raw_html)
            article_html = doc.summary()
            markdown = markdownify(article_html, heading_style="ATX")
            heading = title or doc.short_title()
            if heading:
                markdown = f"# {heading}\n\n{markdown}"
        return title, author, markdown

    def _convert(self, url: str) -> dict:
        from lib import fetch_html, postprocess

        raw_html = fetch_html(url)
        title, author, markdown = self._extract(raw_html)
        result = postprocess(markdown)
        return {"title": title, "author": author, "markdown": result}

    @modal.fastapi_endpoint(method="POST")
    def convert(self, data: dict):
        from lib import build_result

        url = data["url"]
        result = self._convert(url)
        return build_result(result, url)

    @modal.fastapi_endpoint(method="POST")
    def process(self, data: dict):
        """Process pre-fetched HTML (skip HTTP fetch)."""
        from lib import build_result, postprocess

        url = data["url"]
        html = data["html"]
        title, author, markdown = self._extract(html)
        result = postprocess(markdown)
        return build_result({"title": title, "author": author, "markdown": result}, url)

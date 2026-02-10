"""
Model-based HTML-to-Markdown conversion (ReaderLM-v2 1.5B on H100).

Uses curl_cffi for browser-like TLS fingerprinting to avoid bot detection.
"""

import modal

MINUTES = 60
MODEL_NAME = "jinaai/ReaderLM-v2"

app = modal.App("shelf-model")

image = (
    modal.Image.from_registry("nvidia/cuda:12.8.0-devel-ubuntu22.04", add_python="3.12")
    .entrypoint([])
    .uv_pip_install(
        "vllm==0.13.0",
        "huggingface-hub==0.36.0",
        "readability-lxml", "lxml[html_clean]",
        "curl_cffi",
        "playwright",
    )
    .run_commands("playwright install chromium --with-deps")
    .add_local_file("lib.py", "/root/lib.py")
)

hf_cache_vol = modal.Volume.from_name("huggingface-cache", create_if_missing=True)
vllm_cache_vol = modal.Volume.from_name("vllm-cache", create_if_missing=True)

BROWSER_HEADERS = {
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "Accept-Language": "en-US,en;q=0.9",
}


@app.cls(
    image=image,
    gpu="H100",
    scaledown_window=15 * MINUTES,
    timeout=5 * MINUTES,
    volumes={
        "/root/.cache/huggingface": hf_cache_vol,
        "/root/.cache/vllm": vllm_cache_vol,
    },
    max_containers=1,
)
class Converter:
    @modal.enter()
    def load(self):
        import vllm

        self.llm = vllm.LLM(model=MODEL_NAME, dtype="float16")
        self.sampling_params = self.llm.get_default_sampling_params()
        self.sampling_params.max_tokens = 16384
        self.sampling_params.temperature = 0

    def _convert(self, raw_html: str) -> dict:
        import re
        import time
        from html import unescape

        from readability import Document

        from lib import clean_html, fix_headings, postprocess

        t0 = time.perf_counter()

        # Extract metadata from raw HTML.
        title_m = re.search(r"(?is)<title[^>]*>(.*?)</title>", raw_html)
        title = unescape(title_m.group(1).strip()) if title_m else ""
        author_m = re.search(
            r'(?i)<meta[^>]+name=["\']author["\'][^>]+content=["\']([^"\']+)["\']',
            raw_html,
        )
        author = unescape(author_m.group(1).strip()) if author_m else ""

        # Clean and extract article.
        cleaned_html = clean_html(raw_html, strip_data_uris=True)
        doc = Document(cleaned_html)
        article_html = doc.summary()
        t_extract = time.perf_counter()

        max_chars = 65000
        if len(article_html) > max_chars:
            article_html = article_html[:max_chars]
        prompt = (
            "Convert the given HTML to Markdown format. "
            "Reproduce the text content faithfully — do not add, remove, rephrase, "
            "or editorialize any text. Do not add headings that are not in the HTML."
            f"\n```html\n{article_html}\n```"
        )
        messages = [{"role": "user", "content": prompt}]
        t_tokenize = time.perf_counter()

        outputs = self.llm.chat(messages, self.sampling_params)
        t_generate = time.perf_counter()

        req_output = outputs[0]
        comp_output = req_output.outputs[0]
        result = comp_output.text
        n_input = len(req_output.prompt_token_ids)
        n_output = len(comp_output.token_ids)
        total = t_generate - t0
        gen_time = t_generate - t_tokenize
        print(f"[instrumentation] raw_html={len(cleaned_html)} chars, article_html={len(article_html)} chars")
        print(f"[instrumentation] input_tokens={n_input}, output_tokens={n_output}")
        print(f"[instrumentation] extract={t_extract - t0:.3f}s, prompt_build={t_tokenize - t_extract:.3f}s, generate={gen_time:.3f}s, total={total:.3f}s")
        print(f"[instrumentation] generate throughput: {n_output / gen_time:.1f} tok/s")

        # Strip ```markdown fences before heading injection so the title
        # lands at the true start of the text (postprocess anchors on ^/$).
        result = re.sub(r"^```\s*(?:markdown)?\s*\n?", "", result)
        result = re.sub(r"\n?```\s*$", "", result)
        result = fix_headings(cleaned_html, result)
        result = postprocess(result)
        return {"title": title, "author": author, "markdown": result}

    @modal.fastapi_endpoint(method="POST")
    def convert(self, data: dict):
        from fastapi.responses import JSONResponse

        from curl_cffi import requests as cffi_requests
        from lib import download_images, fetch_with_js, format_article, needs_js_rendering

        try:
            url = data["url"]
            resp = cffi_requests.get(
                url,
                headers=BROWSER_HEADERS,
                timeout=60,
                impersonate="chrome",
            )
            resp.raise_for_status()
            raw_html = resp.text

            # Fall back to headless browser if the page requires JS rendering.
            if needs_js_rendering(raw_html):
                raw_html = fetch_with_js(url)

            result = self._convert(raw_html)
            markdown, images = download_images(result["markdown"])
            content = format_article(
                result["title"], result["author"], url, markdown,
            )
            return {"title": result["title"], "content": content, "images": images}
        except Exception as e:
            import traceback
            traceback.print_exc()
            return JSONResponse(
                status_code=500,
                content={"error": str(e), "type": type(e).__name__},
            )

    @modal.method()
    def url_to_markdown(self, url: str) -> dict:
        from curl_cffi import requests as cffi_requests
        from lib import download_images, fetch_with_js, format_article, needs_js_rendering

        resp = cffi_requests.get(
            url,
            headers=BROWSER_HEADERS,
            timeout=60,
            impersonate="chrome",
        )
        resp.raise_for_status()
        raw_html = resp.text

        # Fall back to headless browser if the page requires JS rendering.
        if needs_js_rendering(raw_html):
            raw_html = fetch_with_js(url)

        result = self._convert(raw_html)
        markdown, images = download_images(result["markdown"])
        content = format_article(
            result["title"], result["author"], url, markdown,
        )
        return {"title": result["title"], "content": content, "images": images}

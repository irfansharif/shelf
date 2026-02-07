"""
Model-based HTML-to-Markdown conversion (ReaderLM-v2 1.5B on H100).
"""

import modal

MINUTES = 60
MODEL_NAME = "jinaai/ReaderLM-v2"

app = modal.App("shelf")

image = (
    modal.Image.from_registry("nvidia/cuda:12.8.0-devel-ubuntu22.04", add_python="3.12")
    .entrypoint([])
    .uv_pip_install(
        "vllm==0.13.0",
        "huggingface-hub==0.36.0",
        "readability-lxml", "lxml[html_clean]",
    )
    .add_local_file("lib.py", "/root/lib.py")
)

hf_cache_vol = modal.Volume.from_name("huggingface-cache", create_if_missing=True)
vllm_cache_vol = modal.Volume.from_name("vllm-cache", create_if_missing=True)


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
class ReaderLM:
    @modal.enter()
    def load(self):
        import vllm

        self.llm = vllm.LLM(model=MODEL_NAME, dtype="float16")
        self.sampling_params = self.llm.get_default_sampling_params()
        self.sampling_params.max_tokens = 16384
        self.sampling_params.temperature = 0

    def _clean_html(self, html: str) -> str:
        import re
        # Remove scripts, styles, meta/link tags, and comments before readability.
        # Recommended by https://huggingface.co/jinaai/ReaderLM-v2
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
        # Strip base64-encoded images (large data URIs waste tokens).
        html = re.sub(r'<img[^>]+src="data:image/[^;]+;base64,[^"]+"[^>]*>',
                      '<img src="#"/>', html)
        # Collapse SVGs to placeholders.
        html = re.sub(r"(<svg[^>]*>)(.*?)(</svg>)",
                      r"\1\3", html, flags=re.DOTALL)
        return html

    def _extract_article(self, html: str) -> tuple:
        from readability import Document
        doc = Document(html)
        return doc.summary(), doc.short_title()

    def _convert(self, html: str) -> str:
        import time

        t0 = time.perf_counter()
        cleaned_html = self._clean_html(html)
        article_html, _ = self._extract_article(cleaned_html)
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
        # Dump full structure of vLLM output objects (excluding large token/text fields).
        def _dump(obj, name):
            attrs = {}
            for k in sorted(dir(obj)):
                if k.startswith("_"):
                    continue
                v = getattr(obj, k, None)
                if callable(v):
                    continue
                if k in ("text", "prompt", "prompt_token_ids", "token_ids"):
                    attrs[k] = f"<{type(v).__name__}, len={len(v) if v else 0}>"
                else:
                    attrs[k] = repr(v)
            print(f"[instrumentation] {name}:")
            for k, v in attrs.items():
                print(f"  {k} = {v}")
        _dump(req_output, "RequestOutput")
        _dump(comp_output, "CompletionOutput")

        # Strip ```markdown fences before heading injection so the title
        # lands at the true start of the text (postprocess anchors on ^/$).
        import re as _re
        result = _re.sub(r"^```\s*(?:markdown)?\s*\n?", "", result)
        result = _re.sub(r"\n?```\s*$", "", result)
        from lib import fix_headings, postprocess
        result = fix_headings(cleaned_html, result)
        result = postprocess(result)
        return result

    @modal.fastapi_endpoint(method="POST")
    def convert(self, data: dict):
        import urllib.request
        from fastapi.responses import JSONResponse
        try:
            url = data["url"]
            req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
            with urllib.request.urlopen(req) as resp:
                html = resp.read().decode("utf-8")
            return self._convert(html)
        except Exception as e:
            import traceback
            traceback.print_exc()
            return JSONResponse(
                status_code=500,
                content={"error": str(e), "type": type(e).__name__},
            )

    @modal.method()
    def html_to_markdown(self, html: str) -> str:
        return self._convert(html)

    @modal.method()
    def url_to_markdown(self, url: str) -> str:
        import urllib.request

        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req) as resp:
            html = resp.read().decode("utf-8")
        return self._convert(html)

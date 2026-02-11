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

        from lib import clean_html, extract_metadata, postprocess

        t0 = time.perf_counter()

        # Clean HTML and feed the full page to ReaderLM-v2.  The model was
        # trained to extract main content + metadata from complete pages, so
        # we skip the readability pre-extraction (which strips headings and
        # forces a separate regex-based title extraction step).
        cleaned_html = clean_html(raw_html, strip_data_uris=True)
        max_chars = 65000
        if len(cleaned_html) > max_chars:
            cleaned_html = cleaned_html[:max_chars]
        t_extract = time.perf_counter()

        prompt = (
            "Extract the main content from the given HTML and convert "
            "it to Markdown format."
            f"\n```html\n{cleaned_html}\n```"
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
        print(f"[instrumentation] cleaned_html={len(cleaned_html)} chars")
        print(f"[instrumentation] input_tokens={n_input}, output_tokens={n_output}")
        print(f"[instrumentation] extract={t_extract - t0:.3f}s, prompt_build={t_tokenize - t_extract:.3f}s, generate={gen_time:.3f}s, total={total:.3f}s")
        print(f"[instrumentation] generate throughput: {n_output / gen_time:.1f} tok/s")

        # Strip ```markdown fences.
        result = re.sub(r"^```\s*(?:markdown)?\s*\n?", "", result)
        result = re.sub(r"\n?```\s*$", "", result)

        # Parse Title:/Author: metadata headers from model output (the
        # format ReaderLM-v2 produces with the standard instruction).
        title, author = "", ""
        lines = result.split("\n")
        content_start = 0
        for i, line in enumerate(lines):
            if line.startswith("Title: "):
                title = line[7:].strip()
                content_start = i + 1
            elif line.startswith("Author: "):
                author = line[8:].strip()
                content_start = i + 1
            elif line.strip() == "":
                content_start = i + 1
            else:
                break
        result = "\n".join(lines[content_start:])

        # Fall back to regex-based extraction if the model didn't produce
        # metadata headers.
        if not title:
            print("[metadata] model did not produce Title: header, falling back to regex")
            title, regex_author = extract_metadata(raw_html)
            if not author:
                author = regex_author

        result = postprocess(result)
        return {"title": title, "author": author, "markdown": result}

    @modal.fastapi_endpoint(method="POST")
    def convert(self, data: dict):
        from fastapi.responses import JSONResponse

        from lib import build_result, fetch_html

        try:
            url = data["url"]
            raw_html = fetch_html(url)
            result = self._convert(raw_html)
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
        from lib import build_result, fetch_html

        raw_html = fetch_html(url)
        result = self._convert(raw_html)
        return build_result(result, url)

"""
Modal app serving ReaderLM-v2 (1.5B) for HTML-to-Markdown conversion.
"""

import modal

MINUTES = 60
MODEL_NAME = "jinaai/ReaderLM-v2"

app = modal.App("browser")

image = (
    modal.Image.from_registry("nvidia/cuda:12.8.0-devel-ubuntu22.04", add_python="3.12")
    .entrypoint([])
    .uv_pip_install(
        "vllm==0.13.0",
        "huggingface-hub==0.36.0",
        "readability-lxml", "lxml[html_clean]",
    )
)

hf_cache_vol = modal.Volume.from_name("huggingface-cache", create_if_missing=True)
vllm_cache_vol = modal.Volume.from_name("vllm-cache", create_if_missing=True)


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
    import re
    from html.parser import HTMLParser

    # -- Step 1: Extract headings with context from original HTML. -----------
    class _HeadingContextExtractor(HTMLParser):
        """Extracts (level, heading_text, following_text_snippet) tuples."""
        HEADING_TAGS = {"h1", "h2", "h3", "h4", "h5", "h6"}

        def __init__(self):
            super().__init__()
            self.headings = []  # [(level, text, following_snippet)]
            self._in_heading = None
            self._heading_text = ""
            self._capture_after = False
            self._after_text = ""

        def _flush_after(self):
            if self._capture_after and self.headings:
                lvl, txt, _ = self.headings[-1]
                snippet = " ".join(self._after_text.split())[:120]
                self.headings[-1] = (lvl, txt, snippet)
            self._capture_after = False
            self._after_text = ""

        def handle_starttag(self, tag, attrs):
            if tag in self.HEADING_TAGS:
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
                    self.headings.append((level, text, ""))
                    self._capture_after = True
                    self._after_text = ""
                self._in_heading = None

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
    headings = [(lvl, txt, ctx) for lvl, txt, ctx in headings
                if txt.lower().strip() not in skip and (lvl == 1 or len(ctx) > 20)]

    print(f"[heading-filter] found {len(headings)} content heading(s) in source HTML:")
    for lvl, txt, ctx in headings:
        ctx_preview = ctx[:60] + "..." if len(ctx) > 60 else ctx
        print(f"  h{lvl}: {txt!r}  (ctx: {ctx_preview!r})")

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
    h1_headings = [(lvl, txt, ctx) for lvl, txt, ctx in headings if lvl == 1]
    if h1_headings:
        _, h1_text, _ = h1_headings[0]
        text = f"# {h1_text}\n\n{text.lstrip()}"
        injected += 1

    for lvl, heading_text, context_snippet in reversed(headings):
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
            heading_line = f"\n{prefix} {heading_text}\n"
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
    """Clean up model output: strip code fences, wrap lines."""
    import re
    import textwrap

    # -- Normalize curly quotes/apostrophes to straight ones. ---------------
    markdown = markdown.replace("\u2018", "'").replace("\u2019", "'")  # ' '
    markdown = markdown.replace("\u201c", '"').replace("\u201d", '"')  # " "

    # -- Strip ```markdown / ``` fences. ------------------------------------
    markdown = re.sub(r"^```\s*(?:markdown)?\s*\n?", "", markdown)
    markdown = re.sub(r"\n?```\s*$", "", markdown)

    # -- Collapse runs of 3+ newlines into 2 (single blank line). ----------
    markdown = re.sub(r"\n{3,}", "\n\n", markdown)

    # -- Wrap text body at 100 chars (display width). ------------------------
    # With vim conceallevel=2, [text](url) displays as just "text". Use the
    # link text length as the placeholder width so wrapping targets the
    # displayed width, not the raw markdown width.
    link_re = re.compile(r"\[([^\]]*)\]\([^)]*\)")
    wrapped_lines = []
    for line in markdown.split("\n"):
        # Don't wrap headings, HRs, blank lines, blockquotes, or tables.
        if (not line.strip()
                or re.match(r"^#{1,6}\s+", line)
                or re.match(r"^---+\s*$", line)
                or re.match(r"^>", line)
                or re.match(r"^\|", line)
                or len(line) <= 100):
            wrapped_lines.append(line)
        else:
            # Detect list items to compute continuation indent.
            list_m = re.match(r"^(\s*[\-\*\+]\s+|\s*\d+[.)]\s+)", line)
            cont_indent = " " * len(list_m.group(0)) if list_m else ""

            placeholders = {}
            def _replace(m):
                # Placeholder sized to the link *text* (the concealed
                # display width), padded with non-space chars so textwrap
                # treats it as one word.
                idx = len(placeholders)
                display_len = max(len(m.group(1)), 1)
                key = f"\x00{idx}\x00".ljust(display_len, "\x01")
                placeholders[key] = m.group(0)
                return key
            masked = link_re.sub(_replace, line)
            wrapped = textwrap.wrap(
                masked, width=100,
                break_long_words=False, break_on_hyphens=False,
                subsequent_indent=cont_indent,
            )
            for i, wl in enumerate(wrapped):
                for key, original in placeholders.items():
                    wl = wl.replace(key, original)
                wrapped[i] = wl
            wrapped_lines.extend(wrapped)
    markdown = "\n".join(wrapped_lines)

    # -- Final trim. --------------------------------------------------------
    markdown = markdown.strip() + "\n"
    return markdown


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
        result = fix_headings(cleaned_html, result)
        result = postprocess(result)
        return result

    @modal.fastapi_endpoint(method="POST")
    def convert(self, data: dict):
        from fastapi.responses import JSONResponse
        try:
            return self._convert(data["html"])
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

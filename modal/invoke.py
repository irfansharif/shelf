#!/usr/bin/env python3

import argparse
import modal

TEST_HTML = """
<html><body>
  <nav><a href="/">Home</a></nav>
  <article>
    <h1>Small Language Models</h1>
    <p>By Jane Doe | January 2025</p>
    <h2>Advantages</h2>
    <ul>
      <li>Lower latency</li>
      <li>Easier to deploy</li>
      <li>Better privacy</li>
    </ul>
    <table>
      <tr><th>Model</th><th>Params</th></tr>
      <tr><td>ReaderLM-v2</td><td>1.5B</td></tr>
    </table>
  </article>
  <footer>&copy; 2025</footer>
</body></html>
""".strip()


def remote(url: str = ""):
    if url:
        import urllib.request
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req) as resp:
            html = resp.read().decode("utf-8")
    else:
        html = TEST_HTML
    reader = modal.Cls.from_name("browser", "ReaderLM")()
    print(reader.html_to_markdown.remote(html))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="")
    args = parser.parse_args()
    remote(url=args.url)

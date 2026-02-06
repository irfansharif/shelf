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


def remote(url: str = "", app_name: str = "browser"):
    reader = modal.Cls.from_name(app_name, "ReaderLM")()
    if url:
        print(reader.url_to_markdown.remote(url))
    else:
        print(reader.html_to_markdown.remote(TEST_HTML))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="")
    parser.add_argument("--app", default="browser", choices=["browser", "browser2"])
    args = parser.parse_args()
    remote(url=args.url, app_name=args.app)

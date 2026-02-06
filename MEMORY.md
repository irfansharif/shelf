# MEMORY.md

## Modal Workflow

- After editing any file under `modal/`, always redeploy before testing:
  ```bash
  cd modal && modal deploy readerlm.py   # "browser" app — ReaderLM/vLLM on H100
  cd modal && modal deploy jina.py       # "browser2" app — Jina Reader, CPU only
  ```
  Warm containers may serve stale code. If behavior doesn't match your changes,
  run `modal app stop browser` (or `browser2`) before redeploying to force a
  cold start.

- When debugging or testing Modal endpoints, tail the app logs in the
  background:
  ```bash
  modal app logs browser     # or browser2
  ```
  Start this before triggering requests so you can see server-side print
  output, errors, and stack traces. Check these logs whenever something
  unexpected happens — the answer is usually there.

## Test Skill

Deploy the latest code and run tests against it. All commands run from the
project root directory.

### Steps

1. **Deploy first** — run the `/deploy` skill to push the latest Modal backend
   and stop warm containers.

2. **Run browser tests** to record fixtures and test extraction:
   ```
   cd <project-root> && FIXTURE=1 go test ./pkg/extractor/ -run TestBrowser -v
   ```

3. **Run convert tests** against the live Modal endpoint:
   ```
   cd <project-root> && MODAL=1 go test ./pkg/extractor/ -run TestConvert -v
   ```

4. Report results. If any tests fail, show the failing output.

## Deploy Skill

Deploy the Modal backend. All commands run from the project root directory.

### Steps

1. **Deploy the Modal backend:**
   ```
   cd <project-root>/modal && modal deploy api.py
   ```
   If the deploy fails, report the errors and stop.

2. **Stop warm containers** running old code:
   ```
   modal container list --json | jq -r '.[].id' | xargs -I{} modal container stop {}
   ```
   It's fine if there are no containers to stop.

3. Report success with a summary of what was deployed.

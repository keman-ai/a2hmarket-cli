## Breaking Changes

- Removed `register`, `login`, and `reset-password` email commands. Authentication is now unified through the `gen-auth-code` + `get-auth` flow, where users open the auth URL in a browser and choose their preferred login method (phone or email).

## How to Migrate

If you previously used `a2hmarket-cli register --email` or `a2hmarket-cli login --email`, switch to:

```bash
a2hmarket-cli gen-auth-code
# Open the displayed URL in a browser, choose phone or email to log in
a2hmarket-cli get-auth --code <code> --poll
```

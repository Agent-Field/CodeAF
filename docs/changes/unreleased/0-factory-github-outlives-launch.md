---
kind: fixed
title: The app's factory stays connected to GitHub across a relaunch
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: the app's factory held its GitHub connection in memory only, so every relaunch left it not knowing whose GitHub it was and its runner off until setup connected GitHub again. Is true now: a yes to gh is kept in the factory's profile and gh is read again at every launch; a pasted token is kept by the app in its keychain and handed back with `keys.set {provider: \"github\", key}` before the factory resumes, so the runner comes back on by itself."
  - "Was true: in the app, `WriteGitHubViaGH` was refused like a key write. Is true now: the yes to gh is consent, not a key, and the app's profile keeps it alone; no GitHub token reaches the profile either way."
  - "Was true: a finished setup was trusted while any token was held, so a revoked or expired one left setup at `done` with no way to connect again. Is true now: `setup_read` on a finished setup asks GitHub's `/user` once, and only a 401 sends it back to the `github` step; a GitHub that cannot be reached leaves it `done`."
---

A person who set up a factory on their own Mac relaunched the app and found it
saying "Not running jobs" until they walked through setup again. The token was
held for one launch only. Each way of connecting now keeps itself where its
owner keeps credentials: gh holds its own token, and the app's keychain holds a
pasted one beside the model keys. A terminal's `codeaf factory serve` is
unchanged.

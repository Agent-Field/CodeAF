---
kind: added
title: A factory can be told to read GitHub's repository list again, at most every 30 seconds
pr: 3001
surface: [factory, app, docs]
invalidates:
  - "The list of repositories a person could add was read from GitHub at most every five minutes and nothing could ask for it sooner, and an operator turning on a repository the saved list lacked was told `repository not found`. `GET /v1/repos?refresh=1` (over the app protocol, `factory.repos_refresh`, announced in `hello` as the feature of the same name) now starts a discovery at once and waits for it, the factory starts at most one every 30 seconds however often it is asked, and turning on a missing repository reads GitHub once before it says not found."
  - "The desktop app's Add repos picker does not call the refresh yet; it needs a matching app change, so until then a new repository appears at the next five-minute read."
---

The method is optional: an engine that does not implement it answers
`unknown_method` and omits the feature, and an older factory ignores the query
and answers its ordinary list.

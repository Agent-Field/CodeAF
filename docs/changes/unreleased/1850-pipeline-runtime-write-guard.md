---
kind: changed
title: Pipeline previews and pull requests that carry runtime words need the factory's own say-so
pr: 1850
surface: [engine]
invalidates:
  - "A factory's runtime_fields claim was returned to the app and then forgotten, so a preview or pull request could carry kind, when, effort or fanout to a factory that never said it runs them. The sidecar now remembers the claim per factory, origin, sign-in, repository, commit and exact file text for thirty minutes, and refuses a runtime preview without it."
  - "A pull request whose text spells those words was sent as given. It now needs a remembered claim for its expected base commit, and the file is read again first: if the factory no longer attests it, serves another commit or other bytes, nothing is sent and the person is told to read the file again."
---

No edit operation was added and the desktop still exposes only prompt, until and max. Legacy text, with none of the four words, takes the path it always took. A sidecar restart forgets every claim, which fails closed.

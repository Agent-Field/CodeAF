---
kind: fixed
title: A joined runner runs steps on its own home's model provider
pr: 2121
surface: [engine, docs]
invalidates:
  - "`codeaf runner join` opened its steps from an empty conversation config, so the first leased step failed with provider base URL is required. It now opens them from the runner home's own provider setup, the one `codeaf chat` there uses."
  - "A runner was listed as idle or busy whatever its provider. A runner whose home has no model provider is now listed as needs_provider with the reason needs a model provider, and the factory leases it no step."
---

The same holds for the app's own local runner, which also opens its steps from
its home's profile: with no provider there, it is listed as needs a model
provider rather than taking jobs that fail. The provider state is read when the
runner attaches; after adding a key, run `codeaf runner join` again (or restart
the app).

---
kind: changed
title: A fresh Railway install follows the docs, and the model door's key and claim link are told straight
pr: 2911
surface: [docs, build, chat]
invalidates:
  - "Was true: deploy/org's README and compose.yaml said the hosted model door uses OPENROUTER_API_KEY on codeaf, or the key the server already holds. Is true now: the door uses CODEAF_HOSTED_MODEL_KEY, else the organization key an operator saved in Settings, Organizations, and never reads OPENROUTER_API_KEY, which only the runner and the nodes read."
  - "Was true: the manual's hosting page told an operator to set CODEAF_HOSTED_MODEL_KEY or the key the server already holds. Is true now: it says that key is one saved in Settings, Organizations, and that the OPENROUTER_API_KEY variable is not what the door uses."
  - "Was true: the Railway template script, the operator README, docs/organizations.md and the manual's hosting page said the draft Railway template does not carry the sessionhost. Is true now: make-template.sh adds the sessionhost, the hosted variables and a codeaf health check, and says it has still only been dry-run because the images are not published."
  - "Was true: railway up run from a checkout of the repository stopped while indexing, on a tracked symlink to nothing in internal/skills/testdata. Is true now: the test makes its own links, the tree has none that dangle, and a gate fails the build if one comes back."
  - "Was true: the README gave the operator link on Railway only as a line in the codeaf deploy log. Is true now: deploy/org/railway/operator-link.sh prints it, and the README says that a restart of an unclaimed codeaf retires the old link and that anyone who can read the project's logs can read it."
---

A stranger followed the Railway section from an empty project: every service came up, the sign-in page answered over HTTPS, the sessionhost probe said
`uid+fence` with Landlock, and the organization was left unclaimed. The section now says how the source gets onto Railway (connect the public repository,
a fork, or `railway up` from a fresh clone), the order that worked, the health check path for `codeaf` (`/v1/about`), `GH_TOKEN` for real work, that a volume cannot
shrink, how to check it, and what a service shows when it was deployed before the service it names existed (an empty host name
in the stub's address, fixed by a redeploy). `docs/organizations.md` points a Railway reader at that section.

---
kind: changed
title: the README sells the chat flow and says which install road is actually open
pr: 1021
surface: [chat, docs]
invalidates:
  - "The README's install road was four `curl` lines against `raw.githubusercontent.com/Agent-Field/aforge-v2/main/scripts/install.sh`. The repository is private and `scripts/install.sh` is not on `main`, so all four answer 404 anonymously; the road that works today is clone plus `make build`, and the proxied `https://agentfield.ai/get/aforge` is the preferred form once the repository is public."
  - "`internal/manual/chat/running-from-the-terminal.md` told a person to update aforge with those same four dead `curl` lines. Its first section now names the build road first and says the published URL 404s while the repository is private."
  - "The permission question was described as a one-line card, `? needs your ok to run bash [1] allow once · [2] always, this command · [3] deny · [esc] later · 7s`. That spelling survives only in a comment at `internal/tui3/consent.go:20`; the block draws it now — a bordered block whose header counts ten seconds, whose second choice is the bare word `always`, and whose foot is `c change · ? ask back · 1–3 jump`."
  - "`aforge models` was listed among the commands that need no key. It exits 1 with `aforge needs a model to work with.`; it spends nothing of yours but it needs a resolvable key and reaches the network. `aforge --help` still files it under `Look at what happened — read-only, no key, nothing spent`, and that heading is wrong."
  - "`setupKeyWord` — `aforge talks to models on its default service through openrouter, on your key and your card.` — was quoted as the first-run copy. `internal/tui3/firstrun.go:808` picks `setupConnectWord` wherever browser connect is available, so a reader with a browser sees `connect openrouter` and `sign in once in your browser.` instead."
---

The old README said nothing about the product. This one leads with the chat flow and
a real captured session, and it adds the thing no page said out loud: which folder a
conversation works in. `cmd/aforge/chatv3_layout.go:84` takes `--workspace`, else the
git root, else the directory — except in a home directory or under a temporary
directory, where aforge keeps a folder of its own and the welcome screen says
`in a folder aforge keeps for this conversation · /workspace picks another`. A reader
who did not know that asked aforge to read a file and was told the directory was
empty.

Every sentence in it was written against a citation and then run: a 193-row claims
ledger, and an independent pass that re-ran all of it and drove the real binary
through a fresh first run. Three sentences did not survive that pass and are the
first three invalidations above.

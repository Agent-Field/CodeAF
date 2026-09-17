---
kind: fixed
title: The README shows how to pin a release version again
pr: 1103
surface: [docs]
invalidates:
  - "The README said only that version pinning is on the releases page. Its Install section now shows the pin inline: `curl -fsSL https://agentfield.ai/get/codeaf | VERSION=<tag> bash`."
  - "`TestPublishedPinExamplesGiveVersionToBash` failed on a clean `dev` checkout after the README rewrite. It passes; every published install surface gives `VERSION` to bash, not to curl."
---

The pin goes after the pipe because `VERSION=<tag> curl ...` sets the variable
for curl and the install script never sees it; the release test guards every
page that shows an install line against that spelling.

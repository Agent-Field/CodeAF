---
kind: added
title: a dev build installs beside codeaf as devaf and follows the dev channel
pr: 1333
surface: [build, chat, docs]
invalidates:
  - A dev or staging build made no launch request and said nothing about being
    out of date. It now asks its own channel and draws the same one dim line
    when that channel has a newer build.
  - "`/update` and `codeaf update` with no channel word installed stable on
    every build. They now select the channel the running build came from, and
    stable is only the default for a stable or rc build."
  - The launch notice and every update failure ended with one constant curl
    line for stable codeaf. They now end with the road that reinstalls the file
    actually running, so a devaf is never told to reinstall itself as codeaf.
  - The installer always wrote the file `codeaf` and had no way to choose
    another name. It now takes `--name WORD` and `CODEAF_INSTALL_NAME`, so a
    build can be installed beside codeaf instead of over it.
  - There was one install address per channel under `agentfield.ai/get/codeaf`.
    `agentfield.ai/get/devaf` now installs the newest dev build as `devaf`, and
    it is served by a separate website change that fails closed until this one
    is on `dev`.
  - The launch check kept one answer in `update-check.json` for 24 hours. A dev
    or staging build keeps its own `update-check.dev.json` or
    `update-check.staging.json` for one hour instead, so two builds sharing a
    profile cannot thrash one file.
---

Santosh asked for internal dev usage to be separated from the main install. A dev
build could already be installed, but only over `~/.codeaf/bin/codeaf`, and it
then quietly turned back into a stable binary at the next `/update`. `devaf` is a
file name, not a product: every sentence codeaf prints still says codeaf.

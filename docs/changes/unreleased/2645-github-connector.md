---
kind: added
title: GitHub is a connector, and it is the same GitHub connection the Factory uses
pr: 2645
surface: [app, engine, chat, factory, docs]
invalidates:
  - "Was true: GitHub was absent from internal/connect. mcp_catalog.go said it 'comes back only with an application registered by hand, in a later wave', and the only way to connect GitHub was Factory setup or org sign-in. Is true now: GitHub is registered by internal/connect's github.go and featured first, in category developer. It connects with the device flow (the WorkAF OAuth app), the gh login on this computer or a pasted token. Agents get GitHub's hosted tools from https://api.githubcopilot.com/mcp/ with the token as a bearer, and a gh OAuth-app token was proved to work there."
  - "Was true: connect.Manager kept every connection in credentials.json. Is true now: GitHub is the exception. The manager reads and writes it through GitHubDoors (Manager.UseGitHub), which v3Connect hangs over the Factory's own store (factorygithub.TokenAt, KeepToken, KeepGH, Forget). Connecting or disconnecting in either place does both. A manager given no doors does not offer GitHub."
  - "Was true: the plugins wire had no device code, no local login and no connect_local. Is true now: Plugin carries also_key and local_login {account, source}, plugins.connect_browser may answer code, plugins.connect_local {id} adopts gh's login, and hello announces plugins_github."
  - "Was true: the registry census was 129 services, 30 of them browser. Is true now: 130 services, 31 browser (GitHub added)."
  - "Was true: session.connectHub.BeginAuth answered (url, wait, err). Is true now: it answers (url, code, wait, err), Event carries AuthCode, and the terminal's waiting card reads 'enter the code …' for a device sign-in."
---

The owner wanted agents to be able to use GitHub without anyone opening the Factory.
It had to be one token, so the Connectors page and the Factory can never disagree
about whether GitHub is connected. `local_login` comes from `gh auth status`, which
checks gh's token with GitHub, so codeaf still reads gh's token only after the
person chooses to use that login.

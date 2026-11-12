---
kind: changed
title: factory.me answers a public view that says whether a person signed in through GitHub
pr: 0
surface: [engine]
invalidates:
  - "factory.me returned the factory's whole HTTP identity (runners, machines, takes_unassigned, served_by). It now returns appwire.FactoryMe: factory, codeaf_version, contract, handle, name, avatar_url, scopes (always an array), mode, operator, access and github_signed_in."
  - "A handle or operator flag was the only hint of who was signed in. github_signed_in is true only when the factory reported a session for the caller, so a script or host access key is never read as a person's GitHub sign-in."
---

Provisional entry; the pull request number is filled when it opens. The sign-in
flow is unchanged: `factory.sign_in_poll` still carries `api.Me` with its
session id removed.

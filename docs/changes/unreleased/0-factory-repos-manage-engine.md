---
kind: added
title: The engine can turn one watched repository on or off for a factory's operator
pr: 0
surface: [engine]
invalidates:
  - "The app wire could only read a factory's repositories (`factory.repos`); turning one on or off needed the factory's own web or command line. `factory.repo_set` now does it, for the operator, one repository at a time. It still cannot clone, move or delete a folder, change a repository's configuration or write to GitHub."
  - "`PUT /v1/repos/{owner}/{repo}` answered every refusal with no statement of whether the list had been saved. It now carries `X-Codeaf-Phase: pre-mutation` on a refusal made before the list is saved and clears it just before saving, like the invitation and session routes."
---

A distinct optional feature, `factory.repos_manage`, adds one method,
`factory.repo_set {factory, repo, on}`, answered with the same three fields once
the factory said 200 or 204. `FactoryCapabilities.repos_manage` says the engine
serves it; whether a caller may use it is the operator answer from
`GET /v1/me`, read again on every call.

It works for the operator of a team factory, and for the machine's own solo
factory and an attached solo factory, but only while the app holds a live
credential for the connection. Unlike sign-out and invitations it does not
refuse the local row. With no credential held it refuses before any request
(`signed_out`, or `unreachable` for the local row) and never asks the factory
with no bearer, reads the host's key or reuses the terminal's attach record, so
a loopback team factory cannot hand its host's authority to the caller.

The params are exactly `{factory, repo, on}`: `repo` is a bounded `owner/name`,
`on` is required, and a path, verb, body or folder is `bad_params`. The engine
sends one `PUT` and never retries. Only a 4xx or 501 carrying the phase marker is
a definite refusal, in fixed words. A dropped connection, a timeout, a redirect,
a 5xx, an unmarked refusal (the factory saves the list and then records the
event) or an unexpected success such as 202 is `uncertain`, and the answer
says to read `factory.repos`. If the sign-in changes during a wait, the reply
carries no repository or switch.

Turning a repository off only stops watching it. Its jobs, checkouts and folders
are left alone, and no new delete-folder behaviour exists.

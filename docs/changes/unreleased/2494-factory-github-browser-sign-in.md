---
kind: added
title: the app's first run connects GitHub in the browser with a one-time code
pr: 2494
surface: [app, engine, factory, build, docs]
invalidates:
  - "Was true: the app's first run could connect GitHub only through gh's own login (the person told to run gh auth status, then gh auth login, in Terminal) or a pasted token, and nothing on the app wire spoke GitHub's device flow for the factory on this machine. Is true now: factory.setup_github_start/poll/cancel run GitHub's OAuth device flow; the granted token takes factory.setup_connect's token path and the one signed_in poll answer carries it back as github_token for the app's Keychain. gh's login and a pasted token still work."
  - "Was true: a GitHub OAuth client ID existed only as a team host's github_client_id config key, for `codeaf factory serve`. Is true now: the app's own factory also uses one, stamped at build time with `make … CODEAF_GITHUB_CLIENT_ID=…` (release.yml reads the repository variable of that name) or read at run time from CODEAF_GITHUB_CLIENT_ID. A build with neither does not announce factory.setup_github and answers its methods unknown_method."
  - "Was true: the only answer of the setup wire that could carry a secret was none: a pasted token went in and never came back. Is true now: factory.setup_github_poll's signed_in answer carries github_token once; the desk core takes it out before any window sees it."
---

The owner's rule (2026-11-07) is that a WorkAF user never thinks about code, so
the first run's GitHub step cannot start with a Terminal command. The browser
sign-in asks GitHub for `repo` only: the one classic scope that reads issues,
comments, pushes a branch and opens a pull request on a private repository.
CODEAF_GITHUB_LOGIN_URL points the sign-in at a stand-in GitHub for tests; it
must be https, or http on this machine.

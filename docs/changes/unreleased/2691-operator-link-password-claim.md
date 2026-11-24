---
kind: fixed
title: a server that holds a GitHub token can be claimed with a username and password
pr: 2691
surface: [factory, engine, docs]
invalidates:
  - "A team server that already held a GitHub token (GH_TOKEN, or a GitHub grant in its home) named its operator link for that token's GitHub login, so the link answered github_link to a sign-up, its landing offered only GitHub, and a password operator could not claim the server. A server that takes passwords now never names its operator link for a GitHub login: the first person to open it claims it with a password, a passkey or GitHub. Recovery links (--new-operator-link) still name the operator who claimed."
  - "The operator access link of a fresh team server waited for GitHub to name the host's login, and for a spent rate limit's reset, before it was printed. On a server that takes passwords it is printed at once."
---

Film D14. The org-password design (2646) makes the operator link a username and password account by default, with
GitHub the second way; naming the link for the host's GitHub login was left from before passwords, and quietly took
the password way away on any server that happened to hold a GitHub token.

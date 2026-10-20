---
kind: internal
title: the agent guide says CI is real, Spark is private, and no provider key is normal
pr: 1884
surface: [docs]
invalidates:
  - >-
    Agents in cloud sessions concluded the repository had no CI because `gh`
    failed there. CI is GitHub Actions (`PR gate` on every pull request,
    `Full check` nightly); `gh` fails only because the container's GH_TOKEN
    does not authenticate, and the GitHub MCP tools read runs and logs.
  - >-
    "Run it on Spark" read as a step every contributor owed. Spark is the
    owner's private machine; CI's `touched packages` and `Full check` do its
    work for everyone else, and a cloud session need not repeat `make pr-ready`
    before CI runs the same selector.
  - >-
    The pull-request template asked about `.github/known-red.txt`, deleted in
    #1012; the line is gone.
---

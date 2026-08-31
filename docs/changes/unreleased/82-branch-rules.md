---
kind: changed
title: dev is the trunk, staging is a pointer, and nothing publishes by itself
pr: 82
surface: [build, docs]
invalidates:
  - "The trunk was `chat-v3-task`. It no longer exists on origin; branch off `dev` and open the pull request against `dev`."
  - "`master` was the default branch. It is called `main`, it is parked at the released v0.1.0, and `dev` is the default now."
  - "A merge landing on a branch published an edge release. No branch trigger remains: only a semver tag publishes, and only from a commit that is on `staging`."
  - "The known-failing tests were prose in CLAUDE.md that nothing read. They are `.github/known-red.txt`, and CI skips exactly those names."
  - "Pull requests ran no automated checks at all. Every pull request into `dev` now has to pass `check`."
---

Three branches with no written rule between them is three branches nobody trusts.
The model is a single trunk with two fast-forward pointers, which is what makes a
promotion a decision about one tested commit rather than about an untested merge.
`docs/rules/branching.md` carries the reasoning.

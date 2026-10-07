---
kind: added
title: review, pr-af's code review built into codeaf, reviews a GitHub pull request and posts on your yes
pr: 1784
surface: [chat, engine, docs, build]
invalidates:
  - "sec and senior-dev were the only programs codeaf carries. There are three: review (`/review`, `codeaf review`, `via: \"review\"`) is the third, and it changes no files."
  - "pr-af was a separate AgentField node with its own keys and a coding-agent binary per reviewer. It is copied into codeaf at pr-af's tag codeaf-absorb (b70667e) as internal/praf, runs only through codeaf on the shared agent sessions (internal/agentsession), and is frozen in its own repository."
  - "A program codeaf carried never posted anything outside the machine. review posts a review it wrote to its pull request, but only as its own run (`/review post <report>`), which the chat proposes after the person says yes."
  - "Typing a program's name as a word asked for it. A program whose name is an everyday word (`Delegate.Asked`) is heard by its command and its work instead: `/review` and \"take a look at PR 123\" ask for review, and \"review this function\" does not."
  - "The fixed and lean prefix caps were 57,614 and 49,986 bytes and SIZE-BUDGET was 71,363,000. They are 57,817, 50,189 and 72,395,784, raised by exactly what review measured (PERF.md)."
---

`/review <pull request> [focus]` (or a bare `/review` for the current branch's open pull
request) checks the pull request out in a folder of its own, plans the review,
sends a reviewer at each part of the change, checks and challenges what they found
against the code, and hands back every finding, blocking first, with where it is
and a suggested fix. Its review goes to the task's record folder (`review-report.md`,
`review-report.json`) and its account to the conversation, which offers to post it or
to hand the blocking findings to senior-dev. Private repositories use `GH_TOKEN`,
`GITHUB_TOKEN` or `gh auth token`. Every model call goes through the run's model
API, priced and held to its ceiling: $5 and two hours unless the conversation has
less.

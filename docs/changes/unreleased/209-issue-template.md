---
kind: added
title: a defect issue now owes a replication a stranger can run and an acceptance that starts end to end
pr: 209
surface: [docs]
invalidates:
  - "There was no issue template, so an issue could be filed with its evidence on the filer's machine. `.github/ISSUE_TEMPLATE/defect.md` is now the shape a defect is filed in, and a machine-local path — a private store, a worktree, a `/tmp` anything, a `do.log` — is not a replication; it gets one line as the owner's forensics at most."
  - "Acceptance criteria could be a list of unit tests. They are now end-to-end first: the real door named (`aforge do --json`, the tmux suite, `-tags e2e`) with the exact string or receipt field asserted, and unit tests after."
---

Both rules were paid for once already. #185's forensics lived in a private store
on a benchmark box nobody else can reach, which left a correctly observed bug
unactionable by anyone but the person who saw it. The TUI e2e suite was
unit-green and e2e-broken for a week (#184), because a unit test around
behaviour that only shows through the door proves the unit.

Blank issues stay enabled and there is no `config.yml`: most of what is filed
here is a proposal or a release slice, and the defect template is the wrong door
for those. `CLAUDE.md`'s `## Filing an issue` says so, so it does not get
"fixed" later. #185 and #207 are the worked examples.

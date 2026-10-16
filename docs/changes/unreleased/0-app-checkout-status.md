---
kind: added
title: codeaf app reads checkout status and reports its confirmed-land capability
pr: 0
surface: [engine]
invalidates:
  - "The app could list a conversation's unmerged task branches and diff one file, but had no account of the checkout itself: what is staged, edited, untracked, renamed or in conflict. checkout.status {agent, cursor?, limit?} now answers that, 100 entries a page (200 at most), from the same workspace files.diff and branches.list use."
  - "checkout.status now reports whether the engine implements both branches.land.plan and branches.land. A reader cannot claim the capability itself. Availability does not prepare a plan or authorize a merge: the native host still confirms the exact bound plan, and the producer refuses changed or unsafe checkouts."
---

Read-only git through the runner files.diff uses: no optional locks, no
fsmonitor, submodules by recorded commit only, one 3 s deadline for the call, at
most two reads at once (a third is `busy`). Every configured filter driver is
set empty, and `required` set false so a git-lfs style driver cannot fail the status, before git compares a file, because `git status` otherwise runs a
repository's clean filter on a file whose stat data changed; a scratch-repo test
proves a marker-writing filter does not fire and fails when the override is
removed. A status longer than 4 MiB is `incomplete`, with no counts and no
cursor past the part read. A cursor is tied to the exact status it was issued
for and is refused, never re-aimed, once that changes.
Untracked files are listed individually (`--untracked-files=all`) rather than as
their directory, within the same byte budget. A status cut by that budget keeps
only whole records: a rename whose original path was cut is dropped, not sent
as a rename from nothing.

Entries now carry optional `added`/`removed`/`binary` (net tracked change against
the status's base; untracked, conflicted and filter-governed entries have none)
and the reply may carry `lines_unavailable`. They come from `git diff-index`
because `git diff` rewrites the index's stat cache even with optional locks off.
All git reads of a call share one 6 MiB budget. Agreement between the two status
reads is checked by status text plus the index's and each counted path's stat
stamp, which is not a content hash.

`checkout.diff` and `checkout.locate` take a status row's root-relative path
(`workspace=repo/sub`, row `root.txt` means `repo/root.txt`), find that row in one
status read, and diff or locate it; the old route, joining the path to the
workspace for `files.diff`, named a different file. Untracked reads go through an
`os.Root` with no-follow, non-blocking opens. The reply's `open` is a fact the
native host re-derives with `checkout.locate`, not a grant. Copied, conflicted and
submodule rows are `unsupported_row` in this milestone.

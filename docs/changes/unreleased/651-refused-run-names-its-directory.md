---
kind: fixed
title: a refused headless run names the directory it worked in and says nothing reached disk
pr: 651
surface: [chat, docs]
invalidates:
  - "An `aforge do` run that ended without delivering said `artifacts: []` and NOTHING about the directory it had spent the whole run editing. `--dir` was honoured all the way through and never named again once the run was over, so the only way to learn that a run had left the project untouched was to run `git status` by hand — which is how 45 minutes and $1.9783 closed on \"The time limit was reached before anything finished\" over a tree nobody had told the person was empty. A run that did not leave cleanly, whose file record is empty, now ends on `Nothing reached disk: this run worked in <dir>, editing it in place, and no file there was created or changed while it ran.` It names the directory, and it says nothing was kept."
  - "`aforge do --json` had no field naming the working directory, so a machine caller had to infer it from a path inside `files` — and a run that wrote nothing gave it nothing to infer from. `workspace` is now a key on `do`'s own object, ALWAYS PRESENT, holding the absolute directory as `errandWorkspace` resolved it; it is empty only on a run that never got as far as opening one. It is not part of the shared envelope contract, because `exec` and a saved program have no errand workspace."
  - "The sentence is composed once, in `errandRun`, after the settlement watcher returns and before the run is priced — not in any of the five arms that compose an ending. `groundedInTheTree` is the mirror of `groundedInArtifacts`: that one holds a closing line answerable to the file record when the record HOLDS something, this one holds it answerable to the working directory when the record holds nothing."
  - "Four endings deliberately do not get the sentence and are unchanged: a clean run has nothing to say about a tree; a run holding files already named absolute paths through `producedWords`; a run stopped by a question keeps the empty deliverable `blocked_on` is documented against; and a run with no nodes never did anything a tree could show. A fifth is the errand this process handed to a resident that already holds the store — its registry is empty because the work happened in another process, which is not the same fact as an empty tree, so it is not spoken for."
  - "The sentence claims only what the run can prove. `exec.Workspace.Artifacts` drops `ArtifactDeleted` on purpose — a path that is gone is not a path to open — so the wording is that no file was created or changed, and NEVER that the directory stands as it was found, which would be false for a run that only removed something."
  - "This is the first of the three separable pieces issue #539 names, and only that one. Nothing about pacing, the landing reserve, the no-progress guard or which bound a leaf landed on moved here; those are #610, #624 and #629."
---

The run that reported this spent 45 minutes and $1.98 across two leaves that
made 156 shell calls between them and not one `write` or `edit`. The tree was
correctly untouched, `-w` was correctly honoured, and the close said neither
thing — which left the one fact that mattered to be discovered by hand.

Review correction: the file record is refreshed after worker shutdown, so a late
registered artifact reaches the ending. An empty bounded record now says
`No created or changed files were recorded` rather than claiming no file reached
disk; deleted paths and files outside the scan cannot support that stronger claim.

Claude Fable review independently confirmed the shutdown ordering defect and found
two adjacent cases. Cancelled and paused leaves now register their files before
returning; a price refusal makes no workspace-work claim; a deferred resident run
leaves `workspace` empty rather than publishing this invocation's unproven directory.
The real write-then-stall timeout path is covered with a scripted provider.

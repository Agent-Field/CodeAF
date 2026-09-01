# Task families

*Written 2026-09-01 and rewritten the same day against `origin/dev` at `a5877146`,
when the whole wave had landed. Tracking root #228. Status: landed — every lane
under #228 is on `dev`, and this page says what IS true. It is the record of the
decisions, not a changelog; `docs/changes/unreleased/` is that, and the entries
for #236, #237, #239, #240, #262 and #270 are the wording each decision shipped
under.*

A task family is one node that handed work out and the parts under it.
`divide_work` is the mid-run verb (`internal/session/task_divide.go`);
`propose_task` is the groom that names parts up front. Both mint into the same
graph. The lanes under #228 were one redesign of how those parts share a world,
a ledger, and a brief, and the redesign is now the code.

## What was true before

On the base this page was first written against, a landing carried the
**ledger** — the paths the node wrote — and nothing else, meaning the node's own
worker's paths. `stageTaskWork` staged that list on a repository ground and
`landMirror` laid it back over a folder one by name. On a repository ground git
closed the hole underneath, because a part's work arrives as commits on the tree
the parent merges. On a folder ground nothing closed it: the mirror held every
file the family made, the parent's ledger named its own, the person's folder got
that one, and the task said done because the check had been run against the
mirror that DID hold everything.

A parent whose ground was a plain folder got a copied directory and stopped
there. A part of that parent fell through `groundLadder` to the parent's own
directory and worked in it beside its siblings: no isolation, no merge, no
conflict detection — while `prompts/task.md` and `prompts/divide.md` both
promised each part `a copy of the repository taken from yours`.

Overlap was only advised against, in a prompt sentence and in the reviewer's
brief. Both were soft, and the ledger stages once, so one version silently
destroyed the other before git ever saw a conflict.

What a part was told depended on which road minted it. A part drawn out of a
sketch got the parent's brief and its sibling boundary composed around it
(`sketchBrief`); a part a worker wrote with `divide_work` got only the per-part
prose that worker typed — the weakest world on the road a cheap crew drives.

Parts were cut from a HEAD holding none of the parent's mid-run work, each
sealing the parent's directory independently at the moment its own worktree was
prepared, so siblings prepared minutes apart stood in different worlds.

And a folder landing laid its ledger over the person's directory with a remove
and a copy, against a folder nothing had ever measured, so an edit somebody made
while the work ran was overwritten without a word.

## The ledger composes (#229, PR #237, `c1776b76`)

A node's ledger absorbs every landed part's paths before it lands.
`absorbedLedger` (`internal/session/task_ledger.go`) is
`landingFilesFor(node, changed).all()` — the same two halves the checker's packet
keeps apart, read as one list — so the emptiness law and the landed-only law come
from the one existing source of truth (`landingFilesFor`, `task_claims.go`). A
part that wrote nothing absorbs nothing; a part that never landed is not on the
ledger at all. Because a node settles holding what it absorbed, the fold is
recursive by construction: what a part settles with is what its parent ships.
The call is idempotent, so a road that runs after a landing may ask again
without knowing whether an earlier one already did.

**`landHome` and `keepHome` are the one landing road.** They live beside
`absorbedLedger` in `task_ledger.go`: `landHome` finalizes the ledger and merges,
`keepHome` finalizes it and keeps the branch for a node that settles without
merging. Every road goes through one of them — the ordinary finishing line, the
threshold's `landStopped`, the gate's refusals, a ground that moved under the
work, a person's `accept`, and a late verdict. `comeHome` and `landMirror` still
read exactly one list and neither changed; what changed is that the list is
complete, and that the SAME list is what `TaskNode.finish` writes onto the node
under every one of those roads.

It is folded **after** the check and never before. `Files it wrote:` stays a true
claim about this node and `And the parts it handed out wrote, into the same
tree:` is the parts' own sentence (`task_audit.go`'s `auditQuestion`). A path
both a node and a part wrote is filed under the parts, because the second reading
of a folded ledger cannot tell them apart and an attribution that drifts with
every re-audit is worse than one that is stable.

**`TaskNode.workingCopy` carries the ground and the mode.** A tree rebuilt for a
node with no branch used to come back as `{dir, in place}`. A MIRROR has no
branch either, so `comeHome` read the rebuilt tree as an in-place task and
returned having done nothing: an accepted or re-audited folder family laid
NOTHING back over the person's folder, and the check on one restored an empty
world. The rebuilt tree now carries the recorded ground and mode, so a late
accept lands exactly what the run would have. Every other mode still lands in
place.

`Agent.groundShift` folds the family's ledger itself, so the question — has
anything else landed in these files while this ran — is asked about what would
actually ship.

## The family tree of a folder family is a repository (#230, PR #239, `0a82c43c`)

`openFamilyTree` (`internal/session/task_tree_mirror.go`) opens the mirror as a
repository of its own at the moment it is carved: `git init`, `add --all
--force` minus the two machinery corners `sealGroundWork` also excludes, one
`--allow-empty` baseline commit. `familyTreeIsOpen` is what stops a second
baseline being laid over work already in it — and it asks whether this directory
is the top of its OWN repository, because under the legacy layout a mirror sits
beneath the conversation's workspace and a bare `repositoryRoot` would answer
with the person's repository.

That is the whole change, because every road a part needs already existed and all
of them lead through it. The part's workspace is the mirror, so `groundLadder`
resolves it to the repository it is standing in and the mode falls out as
`TaskModeWorktree`; `prepareTaskTreeOn` cuts a real worktree off that tree at the
part's own `taskOwnFolder` path through the ordinary `cutTaskWorktree` road; and
`taskTree.comeHome` commits the part's ledger and merges that branch back into
the mirror exactly as a repository part merges into the person's checkout. **No
second merge path was written.** A part has a branch, so `restoreFromBranch`
judges it; the parent is still judged by `restoreFromFolder` against the folder
with the ledger laid over it.

Degradation is loud, never silent. A tree that could not be opened — no git, a
read-only disk — still runs the work and carries one sentence
(`sharedFamilyTreeNote`), in the job log and in the parent's own brief, saying
its parts will be working in this same folder beside it. A resumed family
revalidates its tree through that same one call: a mirror already open is left
alone, one that was never opened gets the second chance a restart is, and the
sentence is recomputed rather than persisted, because a remembered one would be
an answer about a machine that has since rebooted.

`in place` and `folder` families are out of scope by ruling. The person said
`here`, so nothing initialises a `.git` in their directory, and their parts go on
sharing it — which is what `here` already meant. The prompt lie `a copy of the
repository taken from yours` is gone; the wording everywhere is `a copy of its
own` / `a working copy`, scrubbed across the prompts, the tool strings and four
manual pages by PR #251 (`eb10e4c1`).

## No two parts of one division own the same path (#231, PR #236)

`scopeCollisions` and `scopeRefusal` (`internal/session/task_divide_scope.go`)
refuse a division whose parts claim the same path. `scopeCollisions(parts, tree)`
reads each part's own scope with `pathTokens`, keeps only tokens that
`groundHolds` says land under the family tree, normalises with `resolvePath`, and
collides on the exact same normalised path claimed by two parts. One pass with a
set.

**It is asked twice, through one function.** Once in `divideOnce` above the line
that spends anything — on the parts the worker wrote, which are the only parts
that exist yet, so the commonest overlap costs nothing and the refusal may
honestly say so (`scopeSpentNothing`). Once again after the paid review and above
the claims — because a sharpened brief can land on a file its sibling already
owns, and a rule enforced only on the asked-for shape is a rule the settled shape
walks around. That second refusal says nothing about spend
(`scopeSpentTheRead`), because the reading is paid for by the time it runs.

The refusal is the ordinary tool-result road (`divisionScopesOverlap`): the
colliding path named, nothing admitted, and a worker that can redraw the boundary
and ask again. It is journalled as `refused:scope` (`divisionRefusedScope`) — the
eighth decision on that line, and its own word because it is the one refusal
saying the division was right and its boundaries were wrong.

The check is deliberately dim. Only the exact same normalised path collides.
Parts sharing a directory, or a part owning a folder while another owns a file
inside it, admit exactly as they always did. What a part **claims** is its own
scope, not the family context around it: `partScope` cuts to the harness's own
markers, so the harness-drawn road cannot collide with itself on the first path
the parent ever mentioned.

`prompts/divide.md` now says the law as the code enforces it — `EVERY PART OWNS
ITS OWN FILES, AND THIS ONE IS ENFORCED`, the refusal before anything is handed
out, the free reading, and that two parts in one directory on different files is
fine and always was. That paragraph is what #241 asked for and it is on `dev`;
the issue is open as bookkeeping, not as missing work.

## One composer writes the family's half of a part's brief (#233, PR #240, `e33df6ad`)

`internal/session/task_divide_compose.go` is that composer, and `startTheParts`
is its one call site, above the admission loop. A part's brief is two halves with
two authors:

- **The family's context** — the work being divided and the map of which scopes
  the other parts own — is identical for every part and is a fact the harness
  already holds, so the harness writes it. `familyOf(request, brief, parts)`
  composes it once per division from the **settled** parts list, so it names what
  actually got handed out; `siblingScope` is how one part is named to its
  siblings; `partBrief(index, scope)` is what one part is handed.
- **The scope** — what this one part owns — is the only half the worker in the
  material could write. `divide_work`'s `brief` field IS the scope now, in the
  tool schema, in `prompts/divide.md`, in the division review's own brief and in
  `internal/manual/chat/tasks.md`.

`sketchBrief` is gone. The sketch road writes the scope and nothing else and gets
its context from the same composer `divide_work` does. What the context is
composed on is what a part **inherits** — `TaskNode.inheritedBrief`, over
`TaskGraph.inheritedLocked`, which is the brief the work was admitted with AND
the reports of whatever ran before it, and which stops short of the standing
orders the frontier appends to every part in its own right.

**One budget.** A part's whole brief is bounded by `taskShapeBriefLimit`, and the
arithmetic is done once per division against the worst case any part of it can
present: the boundary's widest form is taken off the top and is never cut, the
longest scope in the division is reserved next, and the ground takes what
remains. So the first part and the fifth read the same document, and the ground
gives way before the boundary or the scope ever does.

The person's ask is untouched and still printed exactly once, by `composeBrief`,
above all of it; a part composed on a parent brief that IS the person's own
sentence carries no ground at all rather than saying it twice.

## The family's world is frozen once, at the division (#232, PR #262, `a5877146`)

`startTheParts` (`internal/session/task_divide_wip.go`) is the whole of a
division coming into existence, and it is one operation for a reason: `admit`
puts a node on the frontier, which starts it, so a freeze taken after the first
admission is a freeze the first part may already have raced past, and a freeze
taken without the parts carrying it is a fact nobody reads.

`freezeFamilyWorld` stages the parent's ledger and commits it onto the **family
branch** (`the work so far on <title>, before its parts were handed out`) before
a single part is admitted. The commit the family tree then stands at rides per
child: `taskSpec.frozen` at admission, written onto `TaskNode.Frozen`, serialized
on the checkpoint (`task_store.go`'s `groundFrozen`) and on the division's
journal line. It is stored per child rather than as one mutable field on the
parent. Work the parent does after the split reaches no part.

`snapshotRung` (`groundladder.go`) carves from that commit and **never reseals** —
asking the parent's directory what it holds NOW, minutes later, would hand one
part a world none of its siblings ever saw. And because the freeze is an ordinary
commit on the family branch rather than scaffolding, there is no `base` to lift
back out: `replayOwnWork` exists to remove a machine commit before it comes home,
and a part's landing is now an ordinary merge onto a shared ancestor. The
universe rung honours the same freeze its own way — `openForkAt` opens the part's
branch AT the frozen commit inside the fork and cleans untracked-and-not-ignored
files, so the tracked world is the freeze exactly and everything git cannot see
is still there. THE FREEZE IS OVER WHAT GIT CAN SEE, which is the edge the
ledger, the landing and the mirror all already draw.

**The guards.** `harnessOwnsThisTree(dir, ground)` is asked structurally, not by
listing modes: the directory must not be the ground (so it is a copy the harness
made) and it must be the top of its own repository (so HEAD is the family's). A
family told to work `here` fails the first and a bare folder under somebody's
repository fails the second; both freeze nothing and commit nothing, and their
parts go on sharing the directory. An empty ledger still takes the freeze, at
HEAD as it stands, because a parent that writes AFTER the division would
otherwise reach the parts that start late and not the ones that started early.

**Loud failure.** `sealGroundWork` now answers `(string, error)` instead of
answering the empty string for both a clean tree and a git failure, and each seal
gets a git index of its own instead of racing siblings on one shared
`.git/aforge-ground-index`. `commitTaskWorkAs` answers the paths, the SHA and the
error. `unheldLedgerPaths` asks the tree whether every ledger path that exists
and is not ignored really went in, because `stageTaskWork` steps over a path git
refuses one at a time — right for a landing, wrong for a family. A family whose
own tree will not take the commit its parts have to start from is refused with
`refused:freeze` (`divisionRefusedFreeze`, `divisionWorldNotFrozen`): nothing is
handed out rather than handed out onto a world the parent does not have. And the
free hands a division holds are one value with one release (`divisionHands`)
rather than a counter every ending had to remember to decrement.

## A folder landing does not write over an edit made under it (#258, PR #270, `c41545aa`)

`internal/session/task_mirror_manners.go`. `rememberGroundBaseline(dir)` writes a
digest per path of the folder as it stood at the moment of the copy — read off
the copy itself, which holds the bytes the family was given, so a save made while
the copy was being taken lands on the refusing side rather than being recorded as
the original. It sits in the tree's own private corner as `ground-baseline.json`,
beside `left-behind.json`, so it survives a dead process, an accept the next
morning and a re-audit, and not a byte of it reaches the checkpoint.

`groundChanged(dir, ground, wrote)` is asked at every landing, before anything is
laid. A file that changed there is NAMED and NOTHING IS LAID: `landMirror`
answers `conflicted` (`mergeConflicted`) — the mark a merge that would not go
already wears, which every landing road already routes to a needs-your-look
settlement. Both versions survive: the person's in their folder as they left it,
the family's in the directory the refusal sentence names. A deletion counts as an
edit; a lay that would not change a byte is never a collision, which is what
keeps an accept over a folder the family has already landed into quiet. A folder
with no such record — an older build, a folder that could not be read in full —
lands exactly as it landed before, because absence is ordinary.

`groundShift` is a different question on a different road: it asks the project
index and the live claims, which know about other aforge tasks and other windows
and nothing at all about a person editing their own file in their own editor.

## Proved end to end

PR #280 is the real-model lane: `internal/e2e/families_e2e_test.go` behind the
`e2e` build tag, every model row pinned at one cheap model and the pin checked
against the machine's own usage ledger. Three scenarios, every assertion on disk
and none of them on a model's prose — a three-section report on a folder ground
(the mirror is a repository with a baseline commit, each part in a worktree cut
off it on its own branch, each merged part's file arriving on its own commit, the
family's ledger in the person's folder, and the folder never gaining a `.git`), a
two-part write-up on a repository ground, and two parts claiming one file refused
at admission with `refused:scope` and zero parts admitted.

## Rejected alternatives

Each of these was argued in a sibling pull request and is recorded here so it is
not reopened. The refusals are one decision set; the next section is the same
list, continued, because a heading that retrieved only half of them would
re-argue the missing half.

**Walking the directory instead of composing the ledger.** A landing that walked
its own tree would ship everything a build left in it. `task_landing_test.go`
exists about this: a measured task landed twenty-six hunks of vendored
virtualenv and not one line of the change; another committed three thousand files
of a `.venv_test`. `stageTaskWork` is not `git add -A`, and that is the whole
point of it. `landMirror` is the audit's own laying (`layWork`) for the same
reason. `absorbedLedger` composes the existing lists; it does not grow a second
walker.

**Prefix-collision rules for scope.** A prefix rule would refuse a part owning a
folder while another owns a file inside it. That is a good division —
`reports/a.md` and `reports/b.md` share a directory and share no file.
`scopeCollisions` collides only on the exact same normalised path. Relative and
absolute spellings of one file are one file. Prose is not a claim. A path outside
the family tree is not this division's to own.

**A second merge path for folder families.** Once the mirror is a repository,
everything already goes through `comeHome`. A part commits its ledger, merges its
branch into the mirror, and the parent later `landMirror`s the composed list onto
the person's folder. A second copier would be a second reading of what shipped.

**A dangling `commit-tree` freeze, and a `.git` stat for its guard** (#246's
shape, closed in favour of #262's). #246 froze the family's world through
`sealGroundWork`, which writes with `commit-tree` and moves no ref — so the
parent's mid-run work belonged to no branch, a `git log` of the family branch
showed none of it, a crash between the division and the landing left it in an
object nothing referenced, and every part carried a machine commit its landing
was written to rebase back out. A commit on the family branch makes the same work
HISTORY instead of scaffolding, and takes one failure mode off every part's
landing. Its guard was likewise wrong in kind: a directory that is not a
repository was left alone, which passes a task working in the person's own
checkout — their folder has a `.git` — so the guard is `harnessOwnsThisTree` and
is structural. The freeze-once plumbing is #246's and was kept; what it is fed
changed, and the SHA is stored per child at admission rather than on the parent.

## Rejected alternatives, continued

The first four refusals sit under *Rejected alternatives* above. These are the
rest of the same decision set.

**Enforcing overlap only in the prompt.** Soft. The ledger stages once, so one
version silently destroys the other before git ever sees a conflict. The reviewer
fails open. A cheap crew's worker wrote both briefs. The refusal sits in
`divideOnce`, once before anything is spent and once after the parts settle.
`Two parts that edit the same file are not independent` remains true on the page
and is no longer the enforcement.

**Reading the composed family-context as a part's claimed scope.** The harness
draws the parent's brief and the siblings' scopes around every part. Treating
that as this part's claim would collide the harness's own markers —
`divisionThisPart` and `divisionOtherParts` — on the first path the parent ever
mentioned. `partScope` cuts to those markers, and #233 puts a bare scope into
`parsed.Parts` so the check never sees a composed document.

**The family tree's baseline commit as the landing's baseline.** It is
byte-exact and free where it exists, and one source of truth beats two files —
but it exists only where `git init` succeeded, and the family whose init failed
is the DEGRADED one whose parts already share a directory and which is the last
one that should also land without a manners check. A road that covers four
families out of five is a road somebody has to remember the shape of. It is also
the dearer reading at the moment it matters: one digest of one ledger path
against a `git` process per landing. The same argument refuses putting the
baseline **on the checkpoint**, which is rewritten as the graph moves and has no
business carrying twenty thousand digests.

**Merging the two versions of a changed folder file, or handing the parent the
resolution.** There is nothing to merge with: the ground is a plain folder and
neither side is a commit. Naming the file and standing back is the honest answer
and the one a person can act on — and it is what git already does for a
repository ground, where a person's own edit to a file the branch touches is
exactly what makes the merge refuse.

**Standing the universe rung down for every part of a frozen family.** The first
answer to the freeze did that, and it would have taken a `.env`, an installed
dependency tree and a dev database away from parts of a family whose parent had
them. The fork honours the freeze instead (`openForkAt`).

**Initialising a `.git` in the person's own folder.** `in place` and `folder`
mode are the person saying `here`. The family tree is that directory. Nothing
initialises a repository in it, and a test pins that the person's folder never
gains a `.git`. Isolation there is a different promise and is not this family.

**Testing a third generation at runtime.** `taskDepthLimit = 2` and depth is
one-based, so a part has neither `propose_task` nor `divide_work`: a part of a
part cannot exist, and the recipe in #229 that divides a part again is
unreachable. The composition law is pinned by folding a nested path into a part's
own ledger and landing that
(`task_nest_test.go`'s `TestAnAcceptedFamilyLandsEveryGenerationsWork`), not by
running a third generation. Whether the bound stays at two is #261's ruling.

## Still open

- **#255 — a landing that could not save the work merges anyway.** `comeHome`
  swallows a stage or commit failure. `commitTaskWorkAs` now answers the SHA and
  the error (#262), but `commitTaskWork` still discards them and `comeHome` is
  unchanged; what a landing owes a failed commit is this issue's seam. PR #277 is
  in flight.
- **#256 — a folder landing that only half happened reports itself as an
  ordinary one.** Same PR.
- **#260 — complexity ratchet on the division and landing seams.** No behaviour
  change: extract the refusal settlement, the child reservation and the admission
  out of `divideOnce`, and the landing finalisation out of `workTaskNode` — which
  is written twice today and is precisely the memory #255 and #256 show failing —
  then pin the ceiling with a ratchet rather than a cliff.
- **#261 — the depth ruling.** `taskDepthLimit = 2`, the manual says *Depth: two
  levels*, and #228's own roadmap asked for a grandchild's writes to ship. The
  tracker is what disagrees. The recommendation on the issue is to keep the bound
  and fix the words; raising it wants isolation at the third level, the nursery
  law reaching a grandchild across a resume, a bound on what one ask can become,
  and a measured answer to the constant's cost claim.
- **`restoreFromFolder` re-mirrors at check time.** A mirrored node is judged
  against a fresh copy of the person's folder taken when the check runs
  (`mirrorGround(tree.ground, dir)`), with the ledger laid over it — not against
  the folder as it stood when the family was given it. An edit the person makes
  mid-run therefore reaches the checker's world, which is a different window from
  the landing's and is not the one `ground-baseline.json` closes. Whether the
  check should read the baseline instead is unruled.

## Outside this family

Two issues share a week with the family and are not under #228.

**#242** — compaction semantic-recovery. A fold marker names a grep-able journal
path so a compacted transcript can still find what the fold hid. It is a
journal-and-compaction change and does not move a ledger, a tree, or a brief.
PR #247 is open.

**#243** — worker-prompt dedup of the deliverable-file law, which is stated more
than once. Prompt hygiene; it does not change `stageTaskWork`, `declaredFiles`,
or `absorbedLedger`. PR #248 is open.

A session opening those two does not need this page, and a session opening this
page does not need those two. The split is the point of recording it.

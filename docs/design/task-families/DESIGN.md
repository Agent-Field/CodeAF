# Task families: one isolation and one landing, on a repository and on a folder

*Written 2026-09-01 against `dev @ a5877146`, after the whole wave landed. Tracking
root #228. Every symbol named below was grepped at that commit; the roadmap this
page opened with is now the record at the end of it. This is the memory of the
decisions, not a changelog — `docs/changes/unreleased/` is that, one entry per
pull request.*

A task family is one node that handed work out and the parts under it.
`divide_work` is the mid-run verb (`internal/session/task_divide.go`'s
`divideOnce`); `divideFromSketch` is the harness's own road, drawing parts out of
a division a groom already named. Both mint into the same graph and, since #240,
through the same door.

The six lanes under #228 were one redesign of how those parts share a world, a
ledger and a brief. All six are on `dev`. Two more defects the same audit turned
up — a folder landing writing over the person's own edits (#258) and the prompt
strings that still promised something that had stopped being true (#251) — landed
with them.

## The two laws

> **THE LEDGER IS THE CONTRACT OF WHAT SHIPS; THE TREE IS ONLY THE MEDIUM.**
>
> **ISOLATION IS ALWAYS A WORKING COPY OF THE FAMILY TREE.**

Both are stated in the code they govern: the first at the head of
`task_ledger.go`, the second at the head of `task_tree_mirror.go`. Everything on
this page is one of them applied to a ground that did not have it.

## What was true

Four cracks, all measured, all in the same place: the seam where a node that
handed work out meets a ground that is not a git repository.

**A landing carried the node's own ledger and nothing else.** `stageTaskWork`
stages that list on a repository ground; `taskTree.landMirror` lays it back over
a folder one by name. A node that divided had a hole in that list: its parts
worked in ITS tree and wrote their paths onto THEIR OWN ledgers. On a repository
ground git closed the hole underneath, because a part's work arrives as commits on
the very tree the parent merges. On a folder ground nothing closed it — the mirror
held all three files, the parent's ledger named one, and the person's folder got
one. The task said done, and the check passed, because the check ran against the
mirror that DID hold everything. Silent data loss (#229).

**Non-git nesting had no isolation and no landing.** For a parent whose ground was
a plain folder, `prepareTaskTreeOn`'s mirror arm handed the node a directory and
stopped there. A part of that parent fell through the ground ladder to
`dir: workspace` — the parent's own directory — and worked in it beside its
siblings, concurrently, with no copy, no merge and no conflict detection. That is
the largest class of general work: research folders, document sweeps, data
directories, a report with a section per region (#230).

**Overlap was advised against and never refused.** `prompts/divide.md` said
`Two parts that edit the same file are not independent.` The reviewer's brief said
it too. Both are soft, and the ledger stages once — so one version silently
destroyed the other before git was ever asked to notice a conflict (#231).

**Parts branched from a HEAD holding none of the parent's work.** Workers are told
never to commit; the harness commits once, at the landing (`commitTaskWork`). So a
parent that divided mid-run handed out parts whose working copies had none of the
repro it had built, the failing test it had written, or the material it had
gathered. Underneath that, a second hole: a part's working copy is prepared lazily
when the frontier starts it, while the parent keeps working — so siblings cut
minutes apart each inherited whatever the directory held at that instant, and the
division that drew their boundaries had described neither world (#232).

And one more, which is a context defect rather than an isolation one: **what a part
was told depended on which road minted it.** A part drawn out of a sketch got the
parent's brief and its sibling boundary composed around it; a part a worker wrote
with `divide_work` got only the per-part prose that worker typed. The weakest world
was handed out on the road a cheap crew actually drives (#233).

## The ledger composes — #229, landed as PR #237

`absorbedLedger` (`internal/session/task_ledger.go`) folds every landed part's
paths into the node's own **at the landing**. `taskTree.comeHome` keeps reading
exactly one list; `stageTaskWork` and `landMirror` did not change. What changed is
that the list is complete.

The fold is recursive by construction, because a node settles holding what it
absorbed: what a part settles with is what its parent ships. `absorbedLedger` is
`landingFilesFor(node, changed).all()`, so the emptiness law and the landed-only
law come from the one existing source of truth — a part that wrote nothing absorbs
nothing, and a part that never landed is not on the ledger at all. It is
idempotent, which is what lets a road that runs after a landing call it again
without knowing whether an earlier one already did.

It is folded **after** the check and never before. The checker's packet has to keep
the two halves apart: `Files it wrote:` stays a true claim about this node, and
`And the parts it handed out wrote, into the same tree:` is the parts' own sentence
(`task_audit.go`'s `auditQuestion`).

**And there is one landing road.** `comeHome` and `keptWork` were called from a
dozen places, each holding its own list. Every road now goes through `landHome` or
`keepHome` — finalize the ledger, then land it or keep it — and each answers the
finalized list so that the `finish` under it writes the SAME ledger onto the node.
That last half is not a convenience: a node's ledger outlives its run. A family
that lands unverified is settled by somebody typing `accept` the next morning, and
that road has nothing to read but what the first landing wrote down.

The same PR repaired the ground fix underneath it. `TaskNode.workingCopy` rebuilt
the tree for a node with no branch as `{dir, in place}` — no ground, no mode. A
MIRROR has no branch either, so `comeHome` read the rebuilt tree as an in-place
task and returned having done nothing: an accepted or re-audited folder family laid
NOTHING back over the person's folder, and the check on one restored an empty
world. The rebuilt tree carries the recorded ground and mode.

## The folder family's tree is a repository — #230, landed as PR #239

`openFamilyTree` (`internal/session/task_tree_mirror.go`) opens the mirror as a
repository of its own at the moment it is carved: `git init`, `add --all --force`
minus the two machinery corners `sealGroundWork` also excludes, one `--allow-empty`
baseline commit. One call site, in `prepareTaskTreeOn`'s `TaskModeMirror` arm.
`familyTreeIsOpen` makes it idempotent for a resumed node, and it asks the right
question — the directory must be the top of ITS OWN repository, because under the
legacy layout a mirror sits inside the person's checkout and a bare
`repositoryRoot` would answer with theirs.

That is the whole change, because everything else already existed. The part's
workspace IS the mirror, so `groundLadder` — which holds a part to where its parent
stands — resolves it to the repository it is standing in, and the mode falls out as
`TaskModeWorktree`. `prepareTaskTreeOn` cuts a real worktree at `taskOwnFolder` off
the mirror's HEAD. `taskTree.comeHome` commits the part's ledger and merges that
branch into the mirror through the same machinery a repository part lands through.
**No second merge path was written.** The audit reaches it too: a part has a branch,
so `restoreFromBranch` judges it, and the parent is still judged by
`restoreFromFolder` against the untouched folder.

The baseline commit is not a baseline for the check. It exists for exactly two
jobs: to give a part something to cut a worktree from, and to give that part's
branch something to merge into.

**Degradation is loud, never silent.** A tree that could not be opened — no git, a
read-only disk, a folder somebody moved — still runs the work, and comes back
carrying one sentence (`sharedFamilyTreeNote`) that the job log prints and the
parent's own brief carries: its parts will be working in this same folder beside
it, so hand out only parts that write different files.

**`in place` and `folder` families are out of scope by ruling.** The person said
`here`, so the family tree IS their directory and nothing initialises a `.git` in
it. A test pins that the person's folder never gains one.

## Scope ownership at admission — #231, landed as PR #236 (into `dev` inside #239)

`internal/session/task_divide_scope.go`. `scopeCollisions(parts, tree)` reads each
part's own scope with `pathTokens`, keeps only tokens that `groundHolds` says land
under the family tree, normalises with `resolvePath`, and collides on the exact
same normalised path claimed by two parts. One pass over the parts with a set, not
a comparison of every part against every other.

**The check is asked twice, and that is the shape.** `Agent.scopeRefusal` is the one
function; what differs between the two askings is the ending and nothing else,
because what differs is what has already been spent.

- The **free gate** runs on the parts the worker wrote, above the paid reading:
  gate three in `divideOnce`, ending `scopeSpentNothing` —
  `nothing is cancelled and nothing is spent.` A division that was never going to
  be allowed to stand should not pay for an adjudication to find that out.
- The **second asking** runs on the parts the reviewer settled, which are not the
  same list: the reviewer may merge two parts into one, or sharpen a brief onto a
  file its sibling already owns. Ending `scopeSpentTheRead` —
  `nothing is cancelled.` A rule enforced only on the asked-for shape is a rule the
  settled shape can walk around.

The refusal is the ordinary tool-result road (`divisionScopesOverlap`, in
`divisionNotAsWritten`'s shape): the colliding path named, nothing admitted, and a
worker that can redraw the boundary and ask again. Journalled as `refused:scope` —
its own word because it is the one refusal here saying the division was *right* and
its boundaries were wrong.

**It is deliberately dim.** Only the exact same normalised path collides. Parts
sharing a directory, or a part owning a folder while another owns a file inside it,
are what a good division looks like. `reports/a.md` and `reports/b.md` share a
directory and share no file.

**What a part CLAIMS is its own scope, not the family context around it.**
`partScope` cuts to the harness's own markers — `divisionThisPart` and
`divisionOtherParts` — because a composed brief carries the parent's whole brief
above the scope and the siblings' scopes below it, and reading either as this part's
claim would make the harness-drawn road collide with itself on the first path the
parent ever mentioned. A brief a worker wrote itself carries neither marker and is
its own scope whole.

The prompt and the reviewer say the rule and now say it is enforced:
`divideDescription` and `prompts/divide.md` carry *EVERY PART OWNS ITS OWN FILES,
AND THIS ONE IS ENFORCED*, with the explicit note that two parts in one directory on
different files are fine.

## The parent's world is frozen onto the family branch — #232, landed as PR #262

`internal/session/task_divide_wip.go`. Before the first part is admitted, the
harness stages the parent's ledger and commits it onto **the family's own branch**
(`wipCheckpointMessage`: `the work so far on <title>, before its parts were handed
out`), and the commit the family tree then stands at is written onto **every part as
it is admitted** — `TaskNode.Frozen`, carried on the checkpoint. One world, on a
branch, shared by every sibling.

**The checkpoint IS the freeze.** They are the same commit, which is why a resumed
part does not reseal: `Frozen` and `Checkpoint` are written together on the journal
line, and the part carries the freeze through `json.Marshal` → `decodeTasks` →
`restoreNode`.

`startTheParts` is the seam: the freeze and the admission are ONE operation, in
that order, because `TaskGraph.admit` puts a node on the frontier and the frontier
STARTS it — a freeze taken after the first admission is a freeze the first part may
already have raced past. `divideOnce` holds none of the bookkeeping; it got about
eighty lines shorter, and the fan cap moved into `divisionHands`, a value with one
`release()` that every road out gives back through.

Because the world is on a branch, the ground ladder's per-child seal is not asked
for a part at all: `snapshotRung` carves straight from the freeze, so there is no
machine commit to rebase back out and a part's landing is an ordinary merge onto a
shared ancestor — one failure mode fewer. The seal keeps its own job, unchanged, for
every task that is nobody's part.

### The guards

- **Nothing written, nothing committed** — the emptiness law, and the whole of the
  sketch road's answer. The world is still *pinned* at the family tree's HEAD,
  because the sibling-divergence half does not need the parent to have written
  anything: a parent that writes AFTER the division would otherwise reach the parts
  that start late and not the ones that started early.
- **The harness may only commit in a tree of its own** — asked structurally by
  `harnessOwnsThisTree`: the directory must not be the ground *and* must be the top
  of its own repository. A `.git` is not enough to answer this; the person's own
  repository has one. A family told to work `here` freezes nothing and its parts go
  on sharing the directory, which is what `here` already meant.
- **A checkpoint that ran is not a checkpoint that carried.** `stageTaskWork` steps
  over a path git refuses, one at a time, which is right for a landing and wrong for
  a family — a ledger path left on the floor is a part opening on a brief that names
  a file its disk does not have. `unheldLedgerPaths` asks the tree with one
  `git status --porcelain -uall -- <ledger>`, and anything still untracked or
  modified refuses the split.
- **A freeze that will not go refuses the division** — `divisionWorldNotFrozen`, no
  part admitted onto a world nobody chose, journalled `refused:freeze`.

### The universe fork honours the freeze

`universeBranch` opens the fork **at** the frozen commit (`openForkAt` in
`groundladder.go`) rather than sealing a second world: the fork is a byte-exact copy
of the family tree, `.git` and all, so the freeze is an object it already holds and
opening it costs one checkout. Untracked-and-not-ignored files are cleaned, so a
part cut late holds no scratch its siblings never saw. **THE FREEZE IS OVER WHAT GIT
CAN SEE** — the same edge the ledger, the landing and `landMirror` all already draw
— which is what keeps a part's `node_modules` and `.env` where the rung exists to
put them.

### The seal underneath, repaired where a family walked through it

Two defects in `sealGroundWork`, raised by an independent reviewer on this seam:

- it staged through **one shared** `.git/aforge-ground-index`, so concurrent
  siblings raced on the file. Each seal now takes an index of its own
  (`groundIndexPrefix`), and a test runs eight at once.
- it turned **every git failure into `""`**, indistinguishable from a clean tree, so
  a child branched from HEAD without its parent's work and nobody was told. It
  answers `(string, error)` now, and `groundRung.carve` gained a third return so a
  rung that *reached* a ground and could not make its world **stops the ladder** with
  git's own words instead of falling through to a lesser one.

`commitTaskWorkAs` answers `([]string, string, error)` for the same reason: a commit
that never ran looked exactly like one that did. `commitTaskWork` and `comeHome` are
deliberately untouched — what a landing owes a failed commit is #255's seam.

## One composer for a part's brief — #233, landed as PR #240

`internal/session/task_divide_compose.go`. The two halves of a part's opening
message are split **by author, not by road**:

- **The family's context** (harness, identical for every part): the work being
  divided — what the part INHERITS, through `TaskGraph.inheritedLocked` — then the
  map of which scopes somebody else owns, built from the **settled** parts list so it
  names what actually got handed out. `divisionOtherParts` is its marker,
  `divisionScopeLimit` bounds one sibling's line at 320 bytes because every part
  carries every other part's.
- **The scope** (worker, per part): what this one part owns, under
  `divisionThisPart` — `WHAT THIS PART OWNS`.

`familyOf(request, brief, parts)` computes the context once for the whole division;
`divisionFamily.partBrief(index, scope)` writes one part's whole world out of it.
`startTheParts` is the one call site, above the loop, for the reason the models and
the ratings are resolved there: the parts of one division must read one document,
not five fittings of it. The parent's brief is fitted to `taskShapeBriefLimit` once,
against the widest sibling sentence.

`sketchBrief` is gone. The sketch road writes the scope and nothing else and comes
through the same door. The person's ask is still printed exactly once, by
`composeBrief`; the composer never carries it, and drops the ground entirely where
the parent's brief *is* the person's own sentence.

In practice the briefs `divideOnce` hands `scopeCollisions` are never composed: both
roads put a bare scope into `parsed.Parts`. That is why this lane needed #231's
`partScope` cut and not the other way around — the collision check must already know
which half of a string is a claim, for the composed briefs that reach it from
anywhere else.

## A landing does not write over a file that changed under it — #258, landed as PR #270

Filed off the same audit once the folder ground became real. `layWork` is `RemoveAll`
and copy, and nothing recorded what the folder held when the copy was made — so an
edit the person made in their own folder while the work ran was overwritten,
silently, state `done`, `merge` `inplace`. Since #237 the window is not one run but
one morning: a family that settled needing a look is laid over the folder when
somebody types `accept` hours later, from a ledger recorded before lunch.

`internal/session/task_mirror_manners.go`:

- **A baseline is recorded when the mirror is made.** `rememberGroundBaseline` walks
  the copy once at `prepareTaskTreeOn`'s mirror case and writes a digest per path
  into the tree's private corner — `.aforge-v3/ground-baseline.json`, beside
  `leftBehindRecord` and for the same reason. It reads the COPY, not the folder,
  which is the one reading with no race in it. It is written where the copy is made
  and nowhere else, so a resumed node keeps the baseline its first run recorded.
- **`landMirror` compares before it lays.** `groundChanged` asks, for every path in
  the ledger: what the folder holds now, what the baseline says it held, and what the
  family would put there. Equal to the baseline (absent-then-absent-now included) →
  laid. Equal instead to what would be laid → laid, because the lay changes nothing,
  which is what keeps the accept road idempotent after a landing that already
  happened. Otherwise → named, and **nothing is laid**.
- The person reads it in the folder's words, settling `TaskUnverified`:
  `its work is in <dir> and was not laid over <ground>: notes.md changed there while
  this ran`.
- **All five roads, one check**, by construction — the mark is `mergeConflicted`, and
  every road already reads that one word.

A file the person deleted is refused: deleting is an edit somebody made on purpose,
and laying the family's version over it would put back the file they had just thrown
away. A file the *family* wrote and then deleted, untouched in the ground, is still
removed. A folder with no record lands as it landed before this file existed —
absence is ordinary, not a refusal, or every family carried across an upgrade would
need somebody's look. **And the window is narrowed, not closed:** a plain folder has
nothing to lock, exactly as git reads its index and then writes the working tree.

## The wording — #251

#239 made the copy real, and strings all over the tree still promised
`a copy of the repository taken from yours`, or named a `worktree` where a folder
ground is just as possible. `divideDescription`, `divideReviewBrief`,
`divisionDone`, the task start notice, `prompts/system.md`, `prompts/task.md`,
`prompts/shape.md`, `tools_standing.go`, `orchestrate.go` and two manual pages say
*a copy of its own* / *a working copy*. Deliberately left standing: the places that
spell both grounds in one sentence — `a worktree of a repository or a copy of a
folder` — which is the wording #240 settled on, and internal comments using
`worktree` as machinery vocabulary.

## Rejected alternatives

Each of these was argued in a sibling pull-request body or a file header and is
recorded here so it is not reopened.

**Walking the directory instead of composing the ledger.** A landing that walked its
own tree would ship everything a build left in it. `task_landing_test.go` exists
about this: a measured task landed twenty-six hunks of vendored virtualenv and not
one line of the change; another committed three thousand files of a `.venv_test`.
`stageTaskWork` is not `git add -A`, and that is the whole point of it. `landMirror`
is the audit's own laying (`layWork`) for the same reason. `absorbedLedger` composes
the existing lists; it does not grow a second walker.

**Prefix-collision rules for scope.** A prefix rule would refuse a part owning a
folder while another owns a file inside it. That is a good division.
`scopeCollisions` collides only on the exact same normalised path. Relative and
absolute spellings of one file are one file. Prose is not a claim. A path outside the
family tree is not this division's to own. *(#281 argues the dimness is not yet dim
enough in the other direction; see Still open.)*

**A second merge path for folder families.** Once the mirror is a repository,
everything goes through `comeHome`: a part commits its ledger, merges its branch into
the mirror, and the parent later lays the composed list onto the person's folder. A
second copier would be a second reading of what shipped.

**Enforcing overlap only in the prompt.** Soft. The ledger stages once, so one
version silently destroys the other before git ever sees a conflict. The reviewer
fails open, and on a cheap crew one worker wrote both briefs. The refusal sits in
`divideOnce`, twice, and `Two parts that edit the same file are not independent.`
remains true and is no longer the enforcement.

**Reading the composed family-context as a part's claimed scope.** The harness draws
the parent's brief and the siblings' scopes around every part. Treating that as this
part's claim collides the harness's own markers on the first path the parent ever
mentioned. `partScope` cuts to those markers.

**Testing depth-3 composition.** `taskDepthLimit = 2`, and the manual states it
outright: `Depth: two levels.` A part cannot hand work out; the tool is absent from
its belt rather than refusing. The composition law is pinned by folding a nested path
into a part's own ledger and landing that, not by running a third generation.
*(#261 asks the owner to rule on whether that stays; see Still open.)*

**Merging the two versions of a changed folder file, or handing the parent the
resolution.** There is nothing to merge with: the ground is a plain folder and neither
side is a commit, so a three-way merge has no base and a model asked to reconcile them
would be inventing one. Naming the file and standing back is the honest answer and the
one a person can act on — and it is what git already does for a repository ground, where
a person's own edit to a file the branch touches is exactly what makes the merge refuse.
With ownership enforced at admission there is no sibling conflict for a parent to
resolve either; what is left is the person's own edit, which is theirs.

**Standing the universe rung down for every part of a frozen family.** That was the
first answer to the freeze, and it was the wrong trade: a part of a family whose parent
had a `node_modules`, a `.env` or a dev database would have lost all three to gain a
guarantee about files it was never going to ship — this rung's own defect written
backwards. The fork honours the freeze instead (`openForkAt`), which costs one checkout
because the freeze is an object the byte-exact copy already holds.

**Initialising a `.git` in the person's own folder.** `in place` and `folder` mode are
the person saying `here`. The family tree is that directory. A test pins that it never
gains a `.git`. Isolation there is a different promise and is not this family.

### Decided during the build

**#246's dangling `commit-tree` freeze, in favour of the family-branch checkpoint.**
PR #246 sealed the parent's world once per division through `sealGroundWork` —
`sealDivisionWorld`, writing a `commit-tree` object that moves no ref, carried on the
parent node and rebased back out at each part's landing. The freeze-once *plumbing*
(`taskStand` → `groundOrder` → `snapshotRung`: cut from it, never re-seal) is that
PR's idea and shape, and it was kept and re-fed. What changed is what it carries.
The seal and the checkpoint answer different questions: the seal answers *what world
does this ONE CHILD stand in* — scaffolding, belonging to no branch, deliberately
lifted out again so what comes home is the child's own work and not its inheritance.
The checkpoint answers *what does the FAMILY'S TREE hold* — history, on the family
branch, made once, and it stays. Leaving the second job to the seal would mean a
family whose shared material exists only inside each part's private branch, in a
commit the landing is written to discard, made once per part off whatever the
directory held at that moment. A `git log` of the family branch would show none of
it, and a crash between the division and the landing would leave the parent's work in
an object nothing references. So `sealDivisionWorld`, `divisionCommitMessage`,
`sealedWorldMessage` and the `.git`-stat guard are **not** in the tree; the freeze is
stored per child at admission rather than as one mutable field on the parent, and it
rides the checkpoint so a resumed part does not reseal. #246 is closed in favour of
#262.

**The #258 baseline living in the tree's private corner, rather than reusing the
family tree's baseline commit.** Being asked to use that commit is the right
question — one source of truth beats two files, and `openFamilyRepository` already
commits the mirror byte-exact the moment it is carved. The accepted reading, written
into `task_mirror_manners.go`'s header: **that commit exists only where `git init`
succeeded, and the family whose init failed is the DEGRADED one** — the read-only
disk, the machine with no git, the folder somebody moved — whose parts already share
a directory, and which is the last family that should also leave without a manners
check. A road that covers four families out of five is a road somebody has to
remember the shape of; this one is asked at every landing and answers for all of
them. It is also the cheaper reading at the moment it matters: one ledger path costs
one digest of one file, where the commit would cost a `git` process per landing in a
directory whose history is the family's medium rather than the person's. And the
digests must not reach the checkpoint, which is rewritten as the graph moves and has
no business carrying twenty thousand of them.

## The record

Landed on `dev` in this order. The design's dependency order put #229 first; the
merge order put #239 first, because #236's commits rode in on it and #237's seam —
one new file plus three insertions in the landing roads — did not touch
`prepareTaskTreeOn` or `divideOnce`.

| # | commit | pull request | issue | what landed |
| --- | --- | --- | --- | --- |
| 1 | `0a82c43c` | #239 | #230, #231 | folder family tree (`openFamilyTree`), carrying #236's scope-ownership commits (`scopeCollisions`) |
| 2 | `c1776b76` | #237 | #229 | ledger absorption (`absorbedLedger`), one landing road (`landHome`/`keepHome`), the `workingCopy` ground fix |
| 3 | `e33df6ad` | #240 | #233 | one part-brief composer (`familyOf`/`partBrief`), `sketchBrief` gone, `system.md` dedup |
| 4 | `eb10e4c1` | #251 | — | the copy-of-the-repository wording scrub after #239 |
| 5 | `c41545aa` | #270 | #258 | folder-landing manners (`rememberGroundBaseline`, `groundChanged`, `mergeConflicted`) |
| 6 | `a5877146` | #262 | #232 | the family-branch checkpoint (`startTheParts`), the universe fork honouring it, the seal repairs |

PR #249 was the `dev`-targeted copy of #236 and was closed once #239 carried the
same commits. PR #246 was closed in favour of #262 (see above).

**And it is proved through the real door — once #280 lands.** That lane pins the
whole page with a real cheap model on both grounds: every model row pinned and
the pin checked against the machine's own usage ledger, every assertion on disk
rather than on a model's prose, about four minutes and six cents for the file.
It is what turns the claims above from arguments into measurements, and it is
where #281 was found. It is still in flight; see *Still open*.

## Still open

- **#231** and **#241** are open on the tracker although the code landed: #231's
  enforcement is in `task_divide_scope.go` and #241's sentence is in
  `prompts/divide.md` under *EVERY PART OWNS ITS OWN FILES, AND THIS ONE IS
  ENFORCED*. What is left on #241 is whether the worker's own page wants more than
  the clause #236 put there.
- **#255** — a landing that could not save the work merges anyway and then deletes
  it. `comeHome` throws `commitTaskWork`'s answer away, merges an empty branch and
  `releaseLanded` removes the worktree. #262 fixed the divide-time half
  (`commitTaskWorkAs`) and deliberately left `comeHome` to this lane.
- **#256** — #255 one road over: `landMirror`'s failing and succeeding roads answer
  the same outcome, so a lay-back that half happened settles `done` with everything
  before the failing path already in the person's folder. PR #277 answers both
  halves and is in flight.
- **#260** — complexity debt on the seams these four defects keep landing in.
  `runTaskChild` at 56, `workTaskNode` at 35, `divideOnce` at 22 by gocyclo's rule.
  No behaviour change; extract the endings and ratchet the ceiling.
- **#261** — the ruling this page's *Testing depth-3 composition* refusal depends on:
  keep the family two levels deep, or raise it to three? The code and the manual
  agree with each other; #229's recipe asked for a generation that cannot exist.
- **#281** — scope ownership is dim on prefixes and not dim enough on reads. A brief
  is prose: it names what a part will write AND what it should read, and two parts
  told to read one plan file are currently refused. Measured while building the
  real-model e2e suite (#280). Ownership is a claim about *writing*; the decision is
  which of the two conservative shapes takes it.

- **`restoreFromFolder` re-mirrors at check time, and nobody has ruled on it.** A
  mirrored node is judged against a fresh copy of the person's folder taken WHEN THE
  CHECK RUNS (`mirrorGround(tree.ground, dir)`, `task_audit.go`), with the ledger laid
  over it — not against the folder as it stood when the family was given it. So an
  edit the person makes mid-run reaches the checker's world and is read as part of the
  family's result, while the landing that follows refuses over the same file. That is a
  different window from the landing's, and it is not the one `ground-baseline.json`
  closes.

## Outside this family

Two issues share the week and are not under #228.

**#242** — compaction semantic-recovery: a fold marker naming a grep-able journal
path so a compacted transcript can still find what the fold hid. It moves no ledger,
no tree, no brief.

**#243** — worker-prompt dedup of the deliverable-file law. `prompts/task.md` states
more than once that what lands is `WHAT YOU WROTE` and that a command-made file comes
home on a `files:` line. Prompt hygiene; it does not touch `stageTaskWork`,
`declaredFiles` or `absorbedLedger`.

A session opening those two does not need this page, and a session opening this page
does not need those two. The split is the point of recording it.

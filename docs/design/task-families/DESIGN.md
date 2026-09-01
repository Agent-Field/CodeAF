# Task families

*Written 2026-09-01 against `origin/dev`. Tracking root #228. Status: design, with four
lanes already opened as pull requests against `feat/task-families`. This page is the
record of the decisions, not a changelog — `docs/rules/changelog.md` is that, and it
is owed when the family lands on `dev`.*

A task family is one node that handed work out and the parts under it. `divide_work`
is the mid-run verb (`internal/session/task_divide.go`); `propose_task` is the groom
that names parts up front. Both mint into the same graph. The six lanes under #228
are one redesign of how those parts share a world, a ledger, and a brief.

## What was true

On `origin/dev` a landing carries the **ledger** — the paths the node wrote — and
nothing else. `stageTaskWork` stages that list on a repository ground.
`landMirror` lays it back over a folder one by name. The auditor already reads a
composed list (`landingFilesFor` in `task_claims.go`, shown as `Files it wrote:`
and `And the parts it handed out wrote, into the same tree:`), but `comeHome` is
still handed the node's own `changed`. On a repository ground git closed the hole
underneath: a part's work arrives as commits on the tree the parent merges. On a
folder ground nothing closed it. The mirror held every file the family made; the
parent's ledger named its own; the person's folder got that one. The task said
done and the check passed against the mirror.

A parent whose ground is a plain folder got a copied directory and stopped there
(`prepareTaskTreeOn`'s `TaskModeMirror` arm). A part of that parent fell through
`groundLadder` to `dir: workspace` — the parent's own directory — and worked in it
beside its siblings: no isolation, no merge, no conflict detection.
`prompts/task.md` and `prompts/divide.md` both promised each part `a copy of the
repository taken from yours`.

Overlap was only advised against. `prompts/divide.md` says `Two parts that edit
the same file are not independent.` The reviewer's brief repeats it. Both are
soft. The ledger stages once, so one version silently destroys the other before
git ever sees a conflict.

What a part was told depended on which road minted it. A part drawn out of a
sketch got the parent's brief and its sibling boundary composed around it
(`sketchBrief`). A part a worker wrote with `divide_work` got only the per-part
prose that worker typed.

## Ledger absorption (#229)

CRITICAL. Silent data loss on folder grounds. Start here. In-flight as PR #237
against `feat/task-families` (`absorbedLedger` in `task_ledger.go`).

The designed end state: the ledger composes. `absorbedLedger` folds every landed
part's paths into the node's own **at the landing** — one line on each of the
three roads that reach `comeHome`: the audit-off road (`nothing checked this
work: the task.audit setting is off`), the checked-done road, and
`landStopped`'s verified road. `comeHome` keeps reading exactly one list;
`stageTaskWork` and `landMirror` do not change. What changed is that the list is
complete.

Because a node settles holding what it absorbed, the fold is recursive by
construction: what a part settles with is what its parent ships.
`absorbedLedger` is `landingFilesFor(...).all()`, so the emptiness law and the
landed-only law come from the one existing source of truth. A part that wrote
nothing absorbs nothing. A part that never landed is not on the ledger at all.

It is folded **after** the check and never before. The checker's packet has to
keep the two halves apart: `Files it wrote:` stays a true claim about this node,
and `And the parts it handed out wrote, into the same tree:` is the parts' own
sentence (`task_audit.go`'s `auditQuestion`).

The manual's folder-landing sentence today says `the files the task wrote are
laid back over it by name`. The end state says the family's files land, that a
part's file needs no second `files:` naming, and that a part which did not
finish is not on the list.

Walking the directory instead of composing the ledger is refused (see Rejected
alternatives). Depth-3 composition is not tested, because it does not exist.

## Folder family tree (#230)

High. Depends on #229. In-flight as PR #239 (`openFamilyTree` in
`task_tree_mirror.go`).

The designed end state: **the mirror is the family tree, and it is a
repository.** `openFamilyTree` opens the copy as a repository of its own —
`git init`, `add --all --force` minus the two machinery corners
`sealGroundWork` also excludes, one `--allow-empty` baseline commit — at the
moment the mirror is carved. One call site, in `prepareTaskTreeOn`'s
`TaskModeMirror` arm.

That is the whole change, because everything else already existed. The part's
workspace is the mirror, so `groundLadder` (which holds a part to where its
parent stands) resolves it to the repository it is standing in, and the mode
falls out as `TaskModeWorktree`. `prepareTaskTreeOn` then cuts a real worktree
off the mirror's HEAD at the part's own `taskOwnFolder` path.
`taskTree.comeHome` commits the part's ledger and merges that branch back into
the mirror through the same machinery a repository part lands through. No
second merge path. A part has a branch, so `restoreFromBranch` judges it; the
parent is still judged by `restoreFromFolder` against the untouched folder.

Degradation is loud, never silent. A tree that could not be opened still runs
the work, and carries one sentence — in the job log and in the parent's brief
— saying its parts will be working in this same folder beside it and to hand
out only parts that write different files.

`in place` and `folder` families are out of scope by ruling. The person said
`here`, so nothing initialises a `.git` in their directory. The prompt lie
`a copy of the repository taken from yours` is gone. The card still says
`its own copy of the folder` or `your own folder`; it never prints `worktree`.

## Scope-ownership enforcement (#231)

High. Depends on #229. In-flight as PR #236 (`scopeCollisions` in
`task_divide_scope.go`).

The designed end state: `divideOnce` refuses a division whose parts claim the
same path — after the reviewer's parts settle, before any hand is claimed, on
the same line the fan cap stands on. `scopeCollisions(parts, tree)` reads each
part's own scope with `pathTokens`, keeps only tokens that `groundHolds` says
land under the family tree, normalises with `resolvePath`, and collides on the
exact same normalised path claimed by two parts. One pass with a set.

The refusal is the ordinary tool-result road (`divisionScopesOverlap`): the
colliding path named, nothing admitted, nothing spent, and a worker that can
redraw the boundary and ask again. Journalled as `refused:scope` — its own word
because it is the one refusal saying the division was right and its boundaries
were wrong.

Only the exact same normalised path collides. Parts sharing a directory, or a
part owning a folder while another owns a file inside it, are what a good
division looks like. A prefix rule would refuse them.

What a part **claims** is its own scope, not the family context around it.
`sketchBrief` today composes the parent's whole brief above every part and its
siblings' scopes below it. Reading either as this part's claim makes the
harness-drawn road collide with itself on the first path the parent ever
mentioned. `partScope` cuts to the harness's own markers (`THIS PART IS ` /
`THE OTHER PARTS ARE IN SOMEBODY ELSE'S HANDS RIGHT NOW: `). A brief a worker
wrote itself carries neither and is its own scope whole. That is the seam #233
formalises.

The prompt and the reviewer still say the rule, but they now say it is
enforced. Soft advice alone is refused (see Rejected alternatives).

## WIP commit at divide time (#232)

Medium. Needs #230 and #231. No pull request yet.

The designed end state: the parent's unfinished world is sealed into the family
tree **before** the first part's worktree is cut, so every part of one division
stands on the same snapshot of what the parent had already written.

On a repository ground this already happens when a part is carved.
`snapshotRung` calls `sealGroundWork` on the way through `cutTaskWorktree` →
`carveGround`: a machine commit of the parent's tree as it stands, untracked
files included, written into an index of its own (`GIT_INDEX_FILE`) so the
parent's index, HEAD and working tree do not move. The commit's message is
`the world this task started from: <title>`. `taskTree.replayOwnWork` rebases
the node's own commits onto the commit that machine commit was made from, so
what comes home is the node's work and not its inheritance. Merging the
inheritance would hand somebody a merge of their own unfinished edits, and git
refuses that (`your local changes would be overwritten`).

#230 makes the mirror a repository. Until this lane, a parent that wrote into
the mirror after `openFamilyTree`'s baseline commit has work sitting uncommitted
in the family tree, and a worktree cut from HEAD misses it. The seam is one
call next to `openFamilyTree` in the same arm, or one line at the `divideOnce`
door — the same `sealGroundWork` / `replayOwnWork` pair, not a second sealer.

#231 has to have landed first: two parts claiming one path, both standing on
the sealed world, is the collision the admission gate exists to refuse. Sealing
first and colliding later is the honest order; colliding first and then sealing
a world nobody will share is wasted work.

`in place` and `folder` families still do not gain a `.git`. There is no
repository to seal into, and the person said `here`.

## Uniform part-brief composition (#233)

Medium. Needs #231. In-flight as PR #240 (`task_divide_compose.go`).

The designed end state: one composer writes the harness's half for both roads,
called once per division from `divideOnce`.

- **Family context** (harness, identical for every part): the work being
  divided — the parent's own brief — then the map of which scopes somebody else
  owns, built from the **settled** parts list so it names what actually got
  handed out.
- **Scope** (worker, per part): what this part owns, under `WHAT THIS PART
  OWNS`.

`sketchBrief` is gone as the composer. The sketch road writes the scope and
nothing else, and gets the same context from the same composer as
`divide_work`. The context is composed once and reused. The parent's brief is
fitted to `taskShapeBriefLimit` once, against the widest sibling sentence, so
every part of one division reads the same document.

The person's ask is still printed exactly once, by `composeBrief`. The composer
never carries it, and drops the ground entirely where the parent's brief *is*
the person's own sentence.

The composed layout is: parent brief, blank line,
`THE OTHER PARTS ARE IN SOMEBODY ELSE'S HANDS RIGHT NOW: …`, blank line,
`WHAT THIS PART OWNS`, newline, the scope to the end of the string. A reader
wanting the scope alone takes everything after `divisionThisPart`. Both markers
stay one shared constant. In practice the briefs `divideOnce` hands
`scopeCollisions` are never composed: both roads put a bare scope into
`parsed.Parts`.

Reading the composed family-context as a part's claimed scope is refused (see
Rejected alternatives). That is why this lane needs #231's `partScope` cut, not
the other way around: the collision check must already know which half of the
string is a claim.

## divide.md scoping sentence (#241)

Low. Prompt-only. Needs #231. No pull request yet.

The designed end state: `internal/session/prompts/divide.md` carries one
self-contained paragraph that states the ownership law the way the code
enforces it — exact path, refused at admission, two parts in one directory on
different files are fine — so a worker inhibits an overlapping ask before it
spends the call. The schema description on `divide_work` and the reviewer's
brief (`divideReviewBrief`) already teach the same fact on the #231 lane; this
page is the worker's own copy of that sentence, written in the asker's
vocabulary and kept under the heading that retrieval will find.

It is a prompt. It does not enforce. #231 is the enforcement. A session that
lands this sentence before #231 would be teaching a refusal the code does not
yet make, which is the half-built-as-working shape the manual law forbids. A
session that lands #231 and leaves `Two parts that edit the same file are not
independent.` as mere advice is the old lie in the other direction.

No new symbol. No new gate. The person-facing wording, once written, is quoted
exactly on the manual page that already answers `When a task turns out to be
too wide for one worker`.

## Rejected alternatives

Each of these was argued in a sibling pull-request body and is recorded here so
it is not reopened. The seven refusals are one decision set; the next section
is the same list, continued, because a heading that retrieved only half of
them would re-argue the missing half.

**Walking the directory instead of composing the ledger.** A landing that
walked its own tree would ship everything a build left in it.
`task_landing_test.go` exists about this: a measured task landed twenty-six
hunks of vendored virtualenv and not one line of the change; another committed
three thousand files of a `.venv_test`. `stageTaskWork` is not `git add -A`,
and that is the whole point of it. `landMirror` is the audit's own laying
(`layWork`) for the same reason. `absorbedLedger` composes the existing lists;
it does not grow a second walker.

**Prefix-collision rules for scope.** A prefix rule would refuse a part owning
a folder while another owns a file inside it. That is a good division —
`reports/a.md` and `reports/b.md` share a directory and share no file.
`scopeCollisions` collides only on the exact same normalised path. Relative
and absolute spellings of one file are one file. Prose is not a claim. A path
outside the family tree is not this division's to own.

**A second merge path for folder families.** Once the mirror is a repository,
everything already goes through `comeHome`. A part commits its ledger, merges
its branch into the mirror, and the parent later `landMirror`s the composed
list onto the person's folder. A second copier would be a second reading of
what shipped.

## Rejected alternatives, continued

The first three refusals sit under *Rejected alternatives* above (walking the
directory, prefix-collision, a second merge path). These four are the rest of
the same decision set.

**Enforcing overlap only in the prompt.** Soft. The ledger stages once, so one
version silently destroys the other before git ever sees a conflict. The
reviewer fails open. A cheap crew's worker wrote both briefs. The refusal has
to sit in `divideOnce`, after the parts settle and before any hand is claimed.
`Two parts that edit the same file are not independent.` remains true and is
no longer the enforcement.

**Reading the composed family-context as a part's claimed scope.** The harness
draws the parent's brief and the siblings' scopes around every part. Treating
that as this part's claim collides the harness's own markers — `THIS PART IS `
and `THE OTHER PARTS ARE IN SOMEBODY ELSE'S HANDS RIGHT NOW: ` — on the first
path the parent ever mentioned. `partScope` cuts to those markers. #233 puts a
bare scope into `parsed.Parts` so the check never sees the composed document.

**Testing depth-3 composition.** `taskDepthLimit = 2`. The manual states it
outright: `Depth: two levels.` A part cannot hand work out; the tool is absent
from its belt, not refusing. The issue recipe that divides a part again is
unreachable. The composition law is pinned by folding a nested path into a
part's own ledger and landing that, not by running a third generation.

**Initialising a `.git` in the person's own folder.** `in place` and `folder`
mode are the person saying `here`. The family tree is that directory. Nothing
initialises a repository in it. A test pins that the person's folder never
gains a `.git`. Isolation there is a different promise and is not this family.

## Roadmap

Native `blockedBy` edges are set on GitHub. The order they encode:

1. **#229 first.** Ledger absorption. Without it every later landing of a
   folder family is still the silent loss, however well the parts were isolated.
   PR #237.
2. **#230 and #231 in parallel**, both blocked by #229. The family tree
   (`openFamilyTree`, PR #239) and the admission gate (`scopeCollisions`,
   PR #236) do not share a seam: one is `prepareTaskTreeOn`, the other is
   `divideOnce`.
3. **#232 after both.** The WIP seal needs a repository to seal into (#230)
   and an admission gate that will refuse two parts standing on that sealed
   world and claiming one path (#231). No pull request yet.
4. **#233 after #231.** The composer needs `partScope` already cutting to the
   harness markers, and it must not hand `scopeCollisions` a composed document.
   PR #240.
5. **#241 after #231.** The prompt sentence states the refusal the code now
   makes. No pull request yet.

The four in-flight pull requests target `feat/task-families`, not `dev`. The
wave into `dev` is one pull request at feature height, and that is when the
changelog entry is owed (`docs/rules/changelog.md`). This page is not that
entry.

## Outside this family

Two issues share a week with the family and are not under #228.

**#242** — compaction semantic-recovery. A fold marker names a grep-able
journal path so a compacted transcript can still find what the fold hid. It is
a journal-and-compaction change. It does not move a ledger, a tree, or a brief.

**#243** — worker-prompt dedup of the deliverable-file law. `prompts/task.md`
already says what lands is `WHAT YOU WROTE` and that a command-made file comes
home on a `files:` line. The law is stated more than once. Dedup is prompt
hygiene. It does not change `stageTaskWork`, `declaredFiles`, or
`absorbedLedger`.

A session opening those two does not need this page, and a session opening this
page does not need those two. The split is the point of recording it.

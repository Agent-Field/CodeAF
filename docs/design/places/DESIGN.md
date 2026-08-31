# Places: a conversation is about somewhere, and it can be about more than one

Written 2026-08-31 against `dev@41c369b5`, after #90 (the ground) landed. Not groomed;
this is the proposal. The name is **places** and not multi-attach, because
`docs/design/multi-attach/` is already the plan for many terminals on one chat — a
different thing that happens to share a word.

## The problem, in the owner's framing

People open aforge *in general* — one instance, often from `~` or wherever the terminal
happened to be — and then work on things that live somewhere else: a repo three levels
down, a folder of notes, two projects at once. Today the program answers that with one
immutable fact captured at launch (`v3LaunchDir`, `session.Config.Workspace`), and
everything hangs off it: the tools' cwd, the system prompt's working directory, the one
`AGENTS.md`, the project config, the home bucket, the one-host-per-workspace socket.

Issue #76 was the bill for this: a chat opened in `~` handed out tasks about a repo
elsewhere, the harness minted an empty repo beside the session, and two commits landed on
the person's own branch. #90 fixed it **for tasks** — a task now stands on a GROUND
resolved from evidence (said → touched → standing in → nothing), writes only there, and
its work lands back (worktree merge, or mirror landed by name). The conversation itself
still has exactly one directory, and the only ways to aim anywhere else are a full path
typed by hand, `propose_task{ground}`, or the one-shot `/workspace` anchor for owned
sessions.

What the owner wants is the ground's idea finished: **the window is where you sat down;
the conversation is about places.** More than one, accrued without ceremony, remembered,
and with the standing default that work done in a place you *refer to* happens in an owned
tree and comes home as a diff — for any folder, not only repositories.

## What already exists that this stands on (verified)

| Piece | Where |
| --- | --- |
| The ground ladder — said / touched / standing-in / nothing; two roots with weight is a question | `internal/session/taskstands.go` |
| One writable ground per task; read anywhere | `internal/session/taskoutside.go` |
| Modes from the deliverable: worktree, reference, **mirror (plain folder, landed by name)**, in place | `internal/session/task_contract.go`, `task_run.go` (`prepareTaskTreeOn`, `landMirror`, `comeHome`) |
| Ground + mode persisted per task record, additively | `task_store.go:173`, `task_index.go:170` |
| Fuzzy path scorer + `@` walk that already **visits** directories and does not emit them | `internal/tui3/files.go` (`pathScore`, `subsequence`, `walkFiles:605`) |
| The palette picker shape (filter box, short list, esc changes nothing) | `internal/tui3/palette.go` |
| A destination cycle over every project on the disk | `internal/tui3/composerlayer.go` (`composerDestinations`, alt+w) |
| The scope chip `here <path>` | `internal/tui3/pages.go:1139` |
| `meta.json`, additive by design, the conversation's identity file | `internal/session/place.go:165` |
| Furrow: copy-on-write universes + sealed timeline for ANY folder, absent-not-broken | `internal/furrow/` |

Rulings already made that this design treats as law (from #76/#90, confirmed with the
lane that owns them):

1. **One ladder.** A conversation-level place is EVIDENCE that feeds the SAID rung of the
   existing ladder — never a parallel resolution mechanism.
2. **One writable ground per task.** Work that spans places becomes a task per place;
   never one task with two grounds. This is NOT "work needing two folders is impossible" —
   a task READS the whole machine, so depending on a second folder is free; what is bound
   is writing. Work that must WRITE in two places is expressed as it already is in the
   task contract: nodes in the graph, one per place, with an edge — a finished node's
   report becomes its dependent's brief (task_contract.go's DAG). And the true escape
   hatch is that a ground is any directory: work that atomically writes two sibling
   folders grounds on their common parent (mirror mode, cheap under clonefile/furrow —
   or in place, when said). The conversation itself may also hold standing trees for
   several places at once; each lands separately. The law is per NODE, so the guard's
   refusal sentence and the landing stay true — never per goal.
3. **The picker is the correction, never the default.** Inference first, shown, one key
   to fix.
4. **Modes are decided, never asked.** Nobody is asked "worktree or copy?".
5. **Generic always.** No hardcoded repo lists, no per-app rules.
6. Multi-root-one-agent as ONE WORKSPACE was rejected (`docs/conversations-design.md:61`)
   for named reasons — which `AGENTS.md`, whose gate. This design keeps one standing
   workspace and answers those objections below rather than reopening that decision.

## The model

A conversation has:

- **The standing place** — where you sat down. Exactly today's `Workspace`. The tools'
  cwd, the prompt's working directory, the gate, the ceilings, the project config, the
  home bucket. Unchanged, immutable, one.
- **Referred places** — an ordered, small set of directories the conversation is about.
  Each is one path plus what the machine learned about it (repository or folder, its
  HEAD, whether furrow watches it, its own `AGENTS.md`), plus a per-place mode override
  when the person said one ("work there directly").

Referred places accrue from evidence, never from configuration:

1. **You name it.** A path in your words, `@`-completion (which starts offering
   directories), a folder dropped on the window, `/attach <dir>` (today's refusal —
   "is a folder · attach a file" — becomes the door), or the picker.
2. **The work resolves it.** When a task's ladder answers at the TOUCHED rung, or you
   answer the two-roots question once, the answer is KEPT on the conversation as a
   referred place — so the next task does not climb, does not ask, and the person is
   never asked the same question twice. This is the whole anti-chore mechanism: you never
   link anything; the conversation notices, says so once in a dim line, and remembers.

Persistence: `Places []PlaceRef` on `session.Meta`, additive like `Effort` and
`Archived` were. A citation, not the record — a missing field is a conversation with no
referred places, which is every conversation today.

And the feed into the ladder is one line of law: **a referred place is a SAID answer for
work that is plainly about it, and a two-roots question whose both roots are referred
places is put to the person as a choice between names it already knows.** The ladder
stays the only decider; `resolveTaskGround` stays the only place the decision is made.

## The standing default — where writes go

**Where you stand, you write. Where you refer, work is staged and landed.**

- In the standing place, the conversation edits directly, exactly as today. A person who
  opened aforge inside their project loses nothing and notices nothing.
- Aimed at a referred place, writes go through a tree cut ON that place — a worktree off
  its HEAD for a repository, a mirror for a plain folder — and come home through the
  landing that already exists (`comeHome`, `landMirror`). Tasks already work this way;
  the new piece is that the CONVERSATION's own edits do too:

  **The standing tree.** The first write aimed at a referred place cuts one tree for
  (conversation × place), lazily, and every later write to that place lands in the same
  tree. The composer grows a dim line — `changes for ~/code/agentfield · 3 files ·
  /land` — and `/land` (or the chip) opens a landing card: the diff, and enter merges it
  home. Until landed, the person's live checkout has not moved. That is "keep changes and
  merge the diff", generalized off git: a repository gets a branch and a merge, a folder
  gets a mirror landed by name, and a furrow-watched place gets its universe — the same
  three answers the task modes already give, reused rather than re-invented.
- "Here", "directly", "in place" in the person's words overrides per place, is remembered
  on that place's `PlaceRef`, and is never guessed. A SAID mode is never overruled.

This also makes the safety story one sentence long: **the only live checkout a
conversation can touch without a landing is the one it is standing in.**

## The picker — `/folder`, and the roads that avoid it

The happy path never opens a modal. Naming a place in any of the four ways above adds it
with one dim confirmation line. The picker exists for "I can't remember where it is",
and for the two moments #90 already owed a surface: the forming card's ground row
(`g` to correct) and the two-roots question. One component, three doors.

`/folder` (aliases `/place`, `/dir`; `/attach <dir>` jumps straight to "referred"):

- **Opens instantly, from memory, no filesystem walk.** The candidate set is layered,
  best first: this conversation's referred places → recency-weighted touched roots from
  this conversation (the TOUCHED machinery, read not re-derived) → every project home
  already knows (`session.World`, the alt+w source) → git roots under `~` from a cached
  index built once in the background and refreshed off the interaction path. This is
  "fastest, not fuzziest": ranking a few hundred known candidates with `pathScore` beats
  crawling the disk. Ordering within a layer is **frecency** — recency × frequency, the
  zoxide model — so after a week of use the first row is almost always the answer and
  the common case is `/folder` `enter`. The empty-query view is the product.
- **Typing filters; typing a path browses.** Free text narrows the candidate list with
  the existing scorer. Input that looks like a path (`/`, `~/`, `./`) switches the same
  surface into **columns** — Miller/Finder columns: parent, current, children; ←/→ walk,
  ↑/↓ move, tab completes, enter takes the highlighted directory. Directories only,
  dot-directories pruned by the same `skipDirs` law the `@` walk uses.
- **The third column tells you what the machine knows** — dim, one line per fact, the
  emptiness law throughout: `repository · main · clean`, `folder · 214 files`,
  `furrow watches this`, `AGENTS.md`. This is the "it knows" moment: you are not picking
  a string, you are picking a place the program has already understood.
- Enter refers the place and drops a chip beside the scope chip. Esc leaves everything
  exactly as it was — the palette's law, kept.

The forming card reuse: the ground row shows `in ~/code/agentfield · branch off main ·
your uncommitted changes not included`; `g` opens this picker seeded with the ladder's
runner-up first. The two-roots ask becomes this picker with exactly two rows and a
question sentence above them — one keypress, as the taskstands header priced it.

## What each surface shows — consequences, never inventory

(Revised 2026-08-31 after the owner's clutter ruling.) There is NO persistent "attached"
row anywhere. A place inferred and only read shows nothing at all — the emptiness law; it
is just the model looking at the disk, which it could always do. A place appears exactly
when it carries news, and the news is the UI:

- **Composer**: the scope chip today says `here <path>`. It stays. A referred place draws
  a chip only while it carries unlanded changes — `changes for agentfield · 3 files ·
  /land` (basename, disambiguated only on collision) — or while an ambiguity question is
  open. Otherwise nothing. alt+w cycles referred places before the world's other
  projects. The full inventory lives behind `/status` and the picker's first rows, on
  demand only.

Why casual inference is safe enough to stay silent: **certainty is placed at the landing,
not at the attach.** Referring wrongly costs nothing — no write reaches the person's real
folder until the landing card has shown them the diff — so inference never blocks (a dim
line, undoable), the only blocking question stays the two-roots ask (one keypress), and
the one moment a person must be sure is the one moment they are shown exactly what
changes.
- **Home**: a conversation row under its standing project carries `· also about
  agentfield` when it refers elsewhere; a place row (`homeplaces.go` pattern) lists the
  conversations that refer to it, whichever bucket they live in. One conversation with
  one place shows nothing new — the emptiness law.
- **The prompt**: keeps ONE working directory and ONE `AGENTS.md`, answering the
  rejected-design objection head on. A referred place's `AGENTS.md` travels with the
  work that goes there: into a task's brief when the ground is that place (the brief is
  the node's whole world already), and to the conversation model as part of the tool
  result the moment a place is first referred — read once, at the seam, not resident in
  the prompt.
- **Config and gate**: the standing place's config governs the conversation. A referred
  place's config governs work grounded there, read where the ground is resolved. One
  gate, one crew, one ceiling per conversation — unchanged.

## The set is alive — how places evolve with the conversation

Places are a cache of resolved answers, never configuration. Inferred places carry the
TOUCHED machinery's recency weights: a place the conversation stops touching decays out
of the picker's top rows and out of ambiguity questions, though its record stays on the
meta as history. A place the person NAMED never silently expires — said is never
overruled, the law taskstands.go already keeps. And a standing tree whose place's HEAD
moves underneath it is exactly the problem taskground.go solves for a task's tree while
a run is out; the standing tree reuses that machinery rather than growing a second copy.

## Space — what a place costs on disk

- **A repository place is near-free.** `git worktree` shares the object store; the only
  cost is one working copy, and teardown at landing already removes it (task_run.go's
  own teardown is the pattern).
- **A plain folder is the expensive case** (mirror = a copy), so three answers in order:
  the tree is cut LAZILY, on the first write and never on refer — reading never copies
  anything, and reference mode reads in place; on APFS the mirror is made with
  `clonefile(2)` (`cp -c`), copy-on-write — instant, zero extra blocks until a file
  actually changes, and Darwin is the platform this program lives on; and when furrow
  watches the place, the universe replaces the mirror entirely, byte-exact and
  cross-platform. A dumb full copy remains only for non-APFS without furrow, where a
  size threshold earns one dim warning before the cut.
- **One tree per (conversation × place)**, never per turn; removed at landing and at
  conversation deletion; a sweeper catches orphans under the session's `trees/`.

## What this deliberately does not do

- **No change to the remote road now.** One host per workspace and the two-roots
  file-transfer law stand. A referred place over `--host` is a wire door of its own and
  belongs to the multi-attach R2 wave; until then, referring a far place is refused with
  a sentence that names the limitation.
- **No model verb for adding places.** The model already names paths in
  `propose_task{ground}`; places accrue from the person's words and from resolved
  grounds. A verb that let the model widen where writes may land is a verb the guard
  would then have to distrust.
- **No second resolution mechanism.** Everything lands as evidence in the one ladder.
- **Reference mode stays narrow**, as #90 made it: a contract that names no file.

## Open rulings for the owner

1. **Staging where you stand?** This design keeps the standing place direct (today's
   behaviour) and stages only referred places. The alternative — every conversation edit
   staged and landed, even at home — is more protection and more ceremony on the path
   people walk most. Recommend: direct where you stand; a person who wants staging at
   home can refer to their own repo from a session standing in `~`, and gets it.
2. **The standing tree's lifetime.** Unlanded changes when the conversation closes:
   kept and offered on resume (recommend), or landed-or-dropped at close?
3. **The word.** `/folder` primary with `/place`, `/dir` as aliases (people say
   "folder"; home already says "place"), or `/place` primary. `/attach` on a directory
   works either way.
4. **Furrow's seat — RULED (owner, 2026-08-31): furrow is EMBEDDED.** The owner's
   words: no variance — every aforge is an aforge with furrow, and if the only blocker
   is size, the limit rises. So furrow ships INSIDE bin/aforge (`go:embed`, gzipped —
   6.0M raw, roughly half that compressed), the SIZE-BUDGET ratchet is raised in the
   same commit that embeds it (PERF.md's law: the cap and the doc move together), and a
   download road exists only as a repair path, never as the plan. What furrow buys, and
   why it is critical for "beyond code": byte-exact forks that carry the DIRTY tree and
   the whole environment (untracked files, .env, the dev database — a git worktree
   carries none of those, so "run the tests in the tree" breaks exactly when it
   matters); plain folders on Linux (clonefile is APFS-only); a workspace-level undo
   timeline (aforge's rewind deliberately touches nothing on disk); and, later, the far
   road — synced universes are what a referred place on another machine wants to be.

   The embedding's own laws, learned from scars this repo already has:

   - **Extraction, never in-place.** At boot (lazily, on first need) the embedded bytes
     are written to a VERSION-STAMPED path under the state root —
     `~/.aforge/bin/furrow-<version>` — fresh file, then rename; never over a path a
     running furrow might occupy (the macOS `Killed: 9` law applies to any binary, not
     just aforge's own). A hash check decides whether extraction is even needed, so
     every boot after the first costs one stat.
   - **The build fetches, the repo does not carry.** A 6M binary committed to git would
     bloat every clone forever; instead `make build` (and CI) fetch the PINNED furrow
     release per GOOS/GOARCH by checksum into a cached, gitignored `third_party/`, and
     the pin file (version + per-platform sha256) is what lives in the repo — auditable,
     and furrow upgrades ride aforge releases in lockstep. A build with no cache and no
     network fails loudly with the fetch command in the message, rather than quietly
     producing an aforge without furrow — no variance starts at the build.
   - **Failure is honest but tiny.** If extraction itself fails (a read-only state
     root), the furrow tools go absent by the existing seam law — but on an embedded
     build that is a reportable defect, and the one place it is said is `/status`, not a
     broken tool on the belt.
   - The seam stays `internal/furrow`, the only contract; nothing else in the tree
     knows the binary was embedded. With furrow always present, the standing tree
     PREFERS a universe over worktree/mirror wherever furrow watches the place, and
     mirror/clonefile is the repair path, not the plan.

## Lanes, if approved

| Lane | Scope | Ships alone? |
| --- | --- | --- |
| P0 furrow embed | pinned furrow fetched at build time per platform (checksummed `third_party/` pin file), `go:embed` gzipped into bin/aforge, version-stamped extraction under the state root on first need, SIZE-BUDGET raised in the same commit, `internal/furrow` untouched as the seam | yes — independent of everything below, and valuable on its own (workspace_restore etc. light up on every machine) |
| P1 picker | the `/folder` palette+columns component, `@` offering directories, `/attach <dir>` door, the chip; forming-card `g` and the two-roots ask rewired onto it | yes — pure surface, immediately useful for `propose_task{ground}` even before places persist |
| P2 places | `PlaceRef` on `Meta`, accrual (said/kept grounds/drop), the SAID-rung feed, per-place mode words | yes — invisible until P1 draws it |
| P3 standing tree | conversation writes aimed at a referred place cut and reuse a tree; `/land` and the landing card; the guard sentence for the live-checkout law | the big one; needs P2 |
| P4 home + manual | home rows, place rows, manual pages (probes: "work on another folder", "open a different repo in this chat", "where did my changes go", "merge what you did"), hunt down pages that say one-directory-only | with each lane |

P1 and P2 are small and honest on their own. P3 is where the owner's "it always works in
the task dir and merges the diff" actually lives, and it reuses the tree/landing
machinery #90 built rather than growing a second copy.

Coordination notes (2026-08-31): the forming block / composer area is free; tui3
room/rail/job files are being reworked by the ux/room-live-life lane — P1 must not touch
them. The ground ladder's owner asked that any conversation-level default feed SAID, which
this does.

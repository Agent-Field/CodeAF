# Home — a place to return to

*Design doc, 2026-08-19. Status: ideation, agreed direction. Nothing here is built
except where noted.*

## The problem, in the user's words

"What is a session? Do I treat it as one sitting, or one project? Do I need multiple
terminals? Why?" — the cognitive struggle is that aforge makes the **directory** the
identity of the surface. A session is "this folder's conversation", so moving between
projects means moving terminals, and there is no one place where everything you have
ever done — or everything running right now — is visible.

Everything wanted already exists on disk in person-centric form
(`~/.aforge/v3/projects/` is literally "everything, one place"). What is missing is a
surface layer that treats the project as a **property you switch**, not an address you
must stand in.

## The mental model

- **Terminal = a window, nothing more.** It owns no state. Closing a terminal loses
  nothing; any terminal can open anything. A second terminal is a second *window* —
  useful for looking at two sessions at once, never required.
- **Home = the place a window opens onto.** Projects by recency, running work at a
  glance, one keystroke into anything, type to start something new.
- **Project = a container** of sessions + task history + a working directory.
  A project *has* a folder; it isn't one:
  - **Adopted folder** — user has a repo; aforge anchors the project to its root.
    Today's behavior; stays the zero-config default.
  - **Owned folder** — user starts from nothing ("plan a trip"); aforge creates and
    owns the directory. The machinery exists today (`work/` dirs for folderless
    sessions); it just isn't promoted to a first-class, nameable, returnable project.
- **Session = one chat inside a project.** Fixed at birth — a chat that migrates
  between projects makes history unanswerable. Cross-project *reference* (a
  `RelatesTo` edge on the task index) covers "two projects working together" without
  breaking containment.
- **Parallelism inside a project is tasks, not terminals.** Reaching for a second
  terminal to do two things on one project is the tool telling you it should have
  been two tasks in one session.

## Home has exactly three jobs

1. **Triage** — what is happening, what needs me.
2. **Door** — start something new with zero ceremony.
3. **Recall** — get back to anything old, fast.

Design each separately and the screen composes itself.

## The layout — two panes, master–detail

Triage-ordered list left, living detail right. Depth goes right, breadth goes down,
and neither fights the other. The full task tree does **not** live in the left list;
focusing a row unfolds it in the right pane.

```
  aforge-v2                                    task-bar view-more · running 12m
  ▲ fix flaky auth test      asking · 2m
  ● task-bar view-more       3 tasks · 12m       plan · build · tests̲ · review
  ● import cleanup           tests · 3m
  ○ session model ideas      8m                  ● taskview.go written, wiring
                                                   the rail's view-more row
  scratch                                        ● manual pages tasks.md, keys.md
  ▲ pricing research         needs your look     ○ waiting: full tui3 suite
  ○ landing page copy        yesterday
                                                 spent $1.20 · 34k tokens
  site-gen
  ○ …2 more, quiet since Tue

  › start something new · @ find · enter open
```

### Left pane — the world, flat-ish and calm

- Projects as **dim section headers**; one line per session under each.
- Ordering inside a project follows the rail's existing triage order
  (`railGroup`: needs-your-look → running → recent → quiet).
- One line per session: state glyph · title · rollup · age. The rollup for a live
  session is *count + the frontier node's activity line* ("3 tasks · writing
  taskview.go").
- **Quiet things collapse to a whisper** (`…2 more, quiet since Tue`) rather than
  rendering. Density through omission, not compression.
- No system telemetry (cpu/ram/net is fleet-cockpit thinking). The honest equivalent
  is the costs line — spend and tokens, dim, only when nonzero (emptiness law).

### Right pane — the focused thing, alive

Content follows what is focused on the left:

- **A running session/task**: phase breadcrumb + node tree with live activity lines.
  The tree rendering is the rail's existing `railForest`/`railWalk` machinery,
  re-hosted at full width. (The in-flight "view more" full-screen task view is a
  stepping stone — its guts become this pane.)
- **A plain chat**: the last exchange, so you remember where you were before you
  press enter.

### The phase breadcrumb — legibility for long runs

For 10–20 SWE-style custom runs, one line says where a run is without a progress bar
or percentage: dim finished steps, lit current one.

```
  plan · build · tests̲ · review · security check
```

Steps come from the run's own shape (harness phases / task steps), not a hardcoded
list.

### What the reference screenshot taught us

An "agent manager" cockpit (tree left, detail right, stats, 20-key footer) gets the
master–detail shape and glyph-state language right, and everything else wrong for
aforge: machine telemetry, `dead`/`errored` machinery vocabulary, everything maximally
visible always, key-soup footer. That surface optimizes for *watching*; home's job is
*moving between*. You glance and go; you don't live there.

**The footer stays at three verbs.** Type = new · `@` = find · `enter` = open.

## Moving between things — two mechanisms, not one

1. **Home as hub.** One key from inside any session pops back to home; home remembers
   the cursor, so bouncing between two sessions is key–enter–key–enter. The ambient,
   spatial way to move.
2. **The switcher overlay.** `@`-style fuzzy search from *anywhere* — not just home.
   Three characters of anything you have ever done, enter, you are there, without
   visiting home at all. For "constantly moving between things" this is the tool used
   fifty times a day; home is for when you *don't know* what you're looking for yet.

Both ride the same global index. A hit is a *(project, session, task)* citation.

## The door — typing is the whole ceremony

The box at the bottom of home is live: start talking and that is a new chat.

**Which project it lands in: defer it.** The session starts unfiled (an owned `work/`
folder — existing machinery), and attaches to a project when it becomes obvious — the
user cds it, names it, or aforge asks once after the first turn ("this sounds like
aforge-v2 — file it there?"). Forcing a project picker *before* the first sentence is
exactly the cognitive tax being removed. Statelessness for the user means: never make
me declare structure before I know what the thing is.

## What already exists (don't rebuild)

| Need | Existing primitive |
| --- | --- |
| everything, one place | `~/.aforge/v3/projects/<workspace>/<session>/` — the global bucket |
| per-project task history, cross-session | `tasks.jsonl` + `Agent.TaskIndex()` (`internal/session/task_index.go`) |
| one-line "what is it doing" | `TaskIndexEntry.Activity`; the rail's `doing/mending/waiting` strings |
| fuzzy search + scoring | `SearchTaskIndex` / `subsequenceSpan` (behind `@`-mentions) |
| task tree rendering | `railForest` / `railWalk` / fold state (`internal/tui3/task.go`) |
| triage ordering | `railGroup` (attention → running → idle → parked → done) |
| session names + last-spoke | `meta.json` per session folder |
| folderless work | `work/` dirs for sessions with no project |
| full-screen modal pattern | the settings sheet (`internal/tui3/settings.go`, `view.go` frame gate) |
| two windows, one folder | per-session file locks; second window gets its own session |

## What is genuinely new

1. **"Needs you" across sessions.** A session waiting on a permission prompt or a
   question has no cross-session flag anywhere today. It is the most valuable row on
   home and the one new signal the design requires. Needs a small, crash-safe
   presence file per session (state + one-line reason) that home can read without
   opening the journal.
2. **The global readdir layer.** Union the per-project buckets: projects list,
   sessions per project, task indexes unioned with a project column. All reads of
   files that already exist.
3. **Cross-project open.** Opening a session whose workspace ≠ cwd. The cwd binding
   is a launch convention, not architecture — the session header already records its
   working directory, and tools already run against the session's workspace.
4. **Promoting `work/` sessions to owned-folder projects** the user can name and
   return to from home.
5. **Home + switcher surfaces themselves**, and the return-to-home key from inside a
   session.

## Decisions taken (this conversation)

- Home is a **hub you return to**, not just a launch screen. That is what actually
  kills multiple terminals.
- Home is **live** — rows update in place — but it *does* nothing on its own: no
  notifications, no history charts. A glance you take, not a thing that talks.
  ("One place where work keeps happening everywhere" is the resident's territory;
  the chat stays a session you sit in front of — sitting in front of it just stops
  requiring standing in the right directory.)
- Bare `aforge` in a repo keeps today's behavior (straight into that project). Home
  greets a bare launch *outside* any known project, and is one key away otherwise.
  Home as escape hatch, not toll booth.
- Multi-column density = **two panes** (master–detail), not more columns in one list.
- A session's project is fixed at birth; `RelatesTo` edges for cross-project links.

## Open questions

- The exact key for "return home" from inside a session (chord budget is tight —
  every `ctrl+<letter>` is taken; candidates: `esc` at top of an idle session,
  `ctrl+.`, an `alt+` chord).
- Whether `@` on home searches everything by default or scopes to the focused
  project first.
- How the "file this chat into a project?" ask is worded and when it triggers
  (end of first turn vs. first time the chat touches a repo).
- Presence-file format for the cross-session "needs you" signal.

## Sequencing

1. **Now (in flight):** rail "view more" + full-screen task view over the
   per-project `TaskIndex` — becomes the right pane's guts.
2. Global readdir layer + switcher overlay (biggest daily win, smallest surface).
3. Cross-project open (kills the terminal-per-project rule).
4. Home surface itself (two panes, hub key, live rows).
5. Owned-folder project promotion + the deferred-filing door.
6. Phase breadcrumbs for long runs; `RelatesTo` edges.

# Memory architecture

Status: shipped 2026-10-14. It replaces the owner-scoped substrate and the
contextual-evidence layer built over 2026-10-03 to 2026-10-13, whose documents
are in git history (`git log -- docs/memory-architecture.md
docs/contextual-memory.md`), as is the audit that led here (`git log --
audit-notes/memory.md`).
Audience: anyone implementing or reviewing memory work in this repository. The
person-facing account is `internal/manual/chat/what-i-remember.md`; where this
document and that page disagree about behaviour, the code decides and both are
fixed.

## 0. What memory is

A memory is **one line that should hold in every future conversation in its
scope** — a preference, a rule, a correction, a decision. It has no time in it.
Anything tied to a time or an event (a reminder, a schedule, a watch) is an
automation, not a memory (`docs/design/automations/DESIGN.md`).

There is **one strength**: every active line in scope rides every conversation,
in front of every request. There is no router choosing lines per message, no use
or miss counts, no "always" flag and no tidy pass. The store refuses any change
that would push some conversation's block past its bound, so a line is in front of
the model or it is forgotten — except in a store an earlier build filled with no
bound, where `store.FitMemories` chooses what rides and the conversation says how
many lines did not.

Three laws follow from the owner's brief:

1. **Never silent.** Every change — kept, noticed, forgotten, replaced, moved,
   put back — says one line in the conversation that caused it.
2. **Never invisible.** The Memories tab (`alt+8`, always on the tab bar)
   shows every line, who kept it and where it holds, and so does `/memories`.
3. **Never irreversible.** Every forgotten or replaced line waits under `let
   go` on the Memories tab, where `u put it back` restores it. Nothing is ever
   deleted outright.

## 1. Owners and scope

| owner | the person reads | held for |
| --- | --- | --- |
| `user` | for you everywhere | the person, every project |
| `machine` | for this machine | this computer only |
| `project:<key>` | for this project | one repository or working area |
| `project:legacy` | couldn't tell which project | rows an older build could not attribute |

The project key is `gitidentity.ProjectKey` — a hash of the normalized origin
remote, or of the canonical path when there is none — and the name written beside
each project line is `gitidentity.ProjectName`, so the tab can say where a line
holds without reversing the key. A conversation sees `user`, `machine` and its
own project. A project-scoped save from a conversation with no provable project
widens to `user` and says so (`for you everywhere, since this conversation has no
project of its own`).

## 2. The store

`~/.codeaf/graph.db`, the `memories` table beside the `events` journal. The store
is event-sourced: `Rebuild` replays the journal, and an event kind this build does
not know is a no-op, so an older journal always replays.

- Events: `memory_add`, `memory_update`, `memory_supersede`, `memory_forget`,
  `memory_restore`, `memory_moved`, `memory_rehomed`, `memory_skipped`,
  `memory_write_failed`.
- Each row carries its `origin` (`person`, `model`, `noticed`) and, for a
  project line, its `project_name`.
- A title is at most 80 runes and a line at most 512; secrets are redacted
  before anything is written. Duplicates are refused by exact normalized text
  per owner.
- The 2026-10-14 migration adds `origin` and `project_name` and drops the
  contextual indexes, the always index and `memories_fts`.

## 3. The one write door

`Agent.keep(text, scope, origin, replaced)` (`internal/session/memory.go`) is every
mouth:

| way in | origin | default scope |
| --- | --- | --- |
| `/remember` (`/always` is the same command), the tab's `+ remember something` | person | this project; everywhere when typed on home |
| the model's `remember` tool | model | what the model names |
| noticing after a turn (§5) | noticed | what the reading names |
| the legacy `memory.md`, once | person | as the file said |

The store measures every change that makes a line active — a new line, a
replacement, a reworded line, a move, a put-back — inside its write transaction,
against every block the owner reaches (`internal/store/memory_room.go`), and refuses
one that would overflow any of them with `store.MemoryFullError` (the owner and name
of the block). The receipt is said from the conversation saying it
(`session.MemoryFullWords`): `memory is full here`, `for <project>`, or `in another
project`; over `--host` the name in the text is compared. A put-back or rewording onto
words the owner already keeps active is refused with `store.ErrMemoryKept`. The legacy
import alone is unbounded. The model's `remember` and `forget` act only on a turn the
person typed: a turn the session opened (a landing, a job's ending) is armed only once
the person's own line is drained into it, or when it was started to answer a line
they typed in the last seconds of the turn before. A person forgets a line on the Memories tab (`f` or `delete`, by its id)
and puts one back there (`u`); the model forgets with its `forget`, which names one
line by its words, lists several matches and forgets nothing, and never guesses.
There is no `/forget` command and no undo stack (both removed 2026-10-14): the tab
is the one place a change is taken back. Moving is `Memory.Move` (`y`/`p`/`m` on
the tab); fixing the wording is `UpdateMemory(id, text)` (`e`).

Receipts: `remembered · <line> · <where>`, `already remembered · …`,
`forgot · <line>`, `put back · …`, `nothing remembered matches "<words>"`,
`now for you everywhere`, `already kept · <line> · nothing was changed`. One made on
the person's behalf ends with `see or forget it on the Memories tab`
(`memoryTakeBack`).

## 4. The read path

`refreshMemoryLocked` runs at every turn start: one indexed `MemoriesFor(owners)`
read renders the `<memory>` block into message[0], ordered person, machine,
project, oldest first, with no clock-derived byte, so the cached prefix stays warm
until a line changes. The bound is PERF.md's *The memory block*. A failed read
keeps the last block.

- **Task workers** get the same section closing their brief, read once per run
  through the read-only `bindingStore` (`TaskGraph.memoryWorld`), and have no verbs.
- **Automation runs** read the block under `bindingOnlyMemory`: no writes, no
  noticing, no lines said.
- **The lean profile and tool-less models** carry the block and not the verbs;
  the page tells the model the person can `/remember`.

## 5. Noticing

After each completed turn, one reflex call (`reflex.Notice`, `roles.RoleReflex`)
updates the state card. It may also keep **one** line, only when the person typed
the turn and the `memory.enabled` row (`notice things to remember`) is on — read
live on every turn, so no restart is needed. It may replace a numbered kept line
(a correction), which supersedes the old one; a correction already kept lets the old
line go and says `noticed · <old> is let go · already kept: <line> · …`. It says
`noticed and remembered · <line> · <where> · see or forget it on the Memories
tab` — on the turn's stream if it is still open, and otherwise on the standing lane
every surface holds, so the last turn before a person quits still says it. Memory's
notices are marked `Event.Unrecorded`, since no transcript carries them: tui3's keeper
holds them for a conversation that is out of sight and files them with its parked
receipts, drawn when the person brings it back. An ask-here errand is not noticed. Turning the row
off stops only this: every kept line still rides.

## 6. Surfaces

- **The Memories tab** (`internal/tui3/memoryplace.go`, `place_memories.go`; it
  was "Memory" until 2026-10-14) is drawn like the Activity tab: groups `you,
  everywhere`, `this machine`, `this project · <name>` open, other projects,
  `couldn't tell which project` and `let go` folded; `→` opens a group and `←`
  folds it (never `enter`); lines hang on tree connectors with `kept by` and `age`
  columns; a pane reads the cursor row out on frames of 110 columns or more; a
  card per line on `enter`; the `→` verbs. Writes and provenance reads are
  asynchronous (`memoryPlaceDoneMsg`, `memoryOriginMsg`).
- **Commands**: `/memory`, `/memories`, `/remember`, `/always`.
- **Over `--host`**: wire Version 22 — `Memory.Snapshot`, `ChangedSince`,
  `Update`, `Forget`, `Restore`, `Provenance`, `Remember`, `Memories`,
  `RememberAlways`, `Always`, `Move`, `Project`. A far engine with no
  store answers `memory is unavailable on this machine · the store could not be
  opened`.

## 7. What was removed, and why

The pre-turn recall and its router, use and miss ranking, the always flag and its
12-rule block, contextual evidence (binding reads, observed outcomes, cross-project
impact, dismissals) and the `memory_evidence` tool, the background tidy and its
`consolidate` registration, and the question-override memories. Each decided
what the model saw in a way the person could neither see nor predict, and several
wrote lines nobody was told about. One strength, one door and one block make
"what does the model know about me?" the same question as "what is on the
Memories tab?".

The `/forget` command and its undo stack went too. A typed query had to name one
line out of fifty and was a guess with a receipt; the tab puts the line in front
of the person, and taking a change back is forgetting or putting back the line
it changed.

## 8. Tests

`internal/store/memory_test.go`, `internal/reflex/reflex_test.go`,
`internal/session/memory_test.go`, `internal/tui3/memory_test.go`,
`internal/remote/memory_wire_test.go`, `cmd/codeaf/chatv3_memory_upgrade_test.go`,
and the manual's probes in `internal/manual/chat_test.go`.

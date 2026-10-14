# Memory audit — the v3 chat's cross-conversation memory, journey by journey

Investigation only. No code changed. Read on `dev` at `8ebf000e2` on 2026-10-14,
so an overhaul can be argued from it. The surface was also driven by hand:
`bin/codeaf` built from that commit, opened on `make demo-home`'s fixture in a
140×45 tmux pane.

Every finding is tagged with how it was established:

- **[seen]** — reproduced on the demo screen.
- **[code]** — read in the code by the author of this note.
- **[reported]** — found by a read-only sub-reader with a citation, not re-read
  line by line here.

## 0. The shape of the thing

What a person is told: "a handful of durable things are carried from one
conversation to the next" (`internal/manual/chat/what-i-remember.md`).

What is behind it:

| Piece | Where | Size |
|---|---|---|
| Event-sourced store: `memories` table, FTS index, owners, evidence events, sync seam | `internal/store/memory*.go`, `contextual*.go` | ~5,100 lines |
| Engine: recall, extraction, decide, binding rules, prior outcomes, impacts, tidy, state card | `internal/session/memory*.go`, `contextual_*.go`, `card.go` | ~11,700 lines |
| Small-model client and prompts (route, extract, decide) | `internal/reflex` | ~1,000 lines |
| Surface: three commands, the memory place | `internal/tui3/memory.go`, `memoryplace.go`, `place_memory.go` | ~1,900 lines |
| Tests for all of the above | | ~19,300 lines |
| Manual page | `what-i-remember.md` | 62 KB, 44 sections |
| Design docs | `docs/memory-architecture.md`, `docs/contextual-memory.md` | 34 KB |

About 19,700 production lines in total. The last three structural changes landed
within one week: #1702 (memory on a plain launch, 2026-09-30), #1777 (owners, one
write door, contextual memory, 2026-10-06) and #1787 (every pre-#1777 store
failed to open with `no such column: owner`, 2026-10-06).

### The pipeline in one paragraph

Memories live in `~/.codeaf/graph.db` (`cmd/codeaf/chat.go:3461`). The file
also holds conversations, facts and skills. Each memory has one owner: `user`,
`machine`, `project:<git-origin or path hash>`, or the quarantine owner
`project:legacy` (`internal/session/memory.go:225-260`). Before each message of
three or more words, SQL shortlists eight titles. A small model (the "reflex",
gemini-2.5-flash by default, or the conversation's own model) picks the ones
that bear on the message, and they ride as a `<memory>` note at the tail of the
transcript (`memory.go:811-877`). A second pass injects "approved rules", prior
failed attempts and cross-project impacts into the same 4,800-rune budget
(`contextual_memory.go:291-347`). After each turn answered in words, the reflex
reads the exchange and may extract a memory, which is settled against near
neighbours by a third call (`memory.go:1092-1167`). A low-tier "tidy" pass runs
when the machine is idle (`memory_consolidate.go`).

## 1. Finding out memory exists

The owner's belief that memory is "hidden in the UI" was true until #1792
(2026-10-07), which put **Memory** 5th on the tab bar **[seen]**. Discovery still
has problems:

- **The shortcut is `alt+8`, not `alt+5`.** Digits follow `placeOrder`, where
  memory is last; the bar follows `placeBarOrder`, where it is 5th
  (`internal/tui3/pages.go:398-402`) **[code]**. `alt+5` opens Spend. The `/help`
  key sheet claims the digits follow "the tab bar's own order"
  (`commands.go:1035`) **[reported]**.
- **The manual still says memory is off the bar.** See
  `what-i-remember.md:207` ("one of seven … memory is not on the tab bar"),
  `places.md:626`, `home.md:1629-1631`, and `keys.md:2333-2336` (which
  contradicts itself) **[reported]**. The chat will tell a person the place is
  hidden.
- **On a small-context model the page looks empty and blames the setting.**
  `memoryReady()` requires `brain()`, and `brain()` requires the current agent's
  `Remembers()`. That is false on the lean profile: a context window under 32k,
  a model with no tools, or `lean` pinned (`place_memory.go:475`, `memory.go:176`,
  `promptprofile.go:131,148`) **[code]**. The page draws nothing plus
  `memory is off for this session · turn it on under /settings`, although the
  setting is on and the store is full. Home's `learned N things` line and the
  tab count read the store directly, so home can point into a page that says
  memory is off.
- **The `Memory N` count never appears for a new person.** It counts only after
  the place has been visited and left once (`placecounts.go:66-69`)
  **[reported]**.
- **Onboarding, first run and `/status` never mention memory** **[reported]**.
- **The settings row has two names.** The panel labels it
  `remember across chats` (`internal/config/settings_presentation.go:46-49`).
  The manual, the off note and the tui3 label table call it "the memory row"
  **[reported]**.

## 2. Getting something remembered

There are six ways in, and they disagree.

| Door | Scope it lands in | What the person sees | Can become an always-shown rule? |
|---|---|---|---|
| `/remember <text>` | **user** (`memory.go:1706-1708`) | `remembered · <title>`, in the conversation only | No |
| Saying "remember that …" in chat (router command) | **project** (`memory.go:902`, `:1580`) | `remembered · <title>` | No |
| The model's `remember` tool | **project** by default (`memory.go:2003`, `:1580`) | A folded tool row; the landed title only if expanded | No |
| Background extraction after the turn | Model proposes; demoted to **project** unless the words say "across projects" (`contextual_memory.go:133-150`) | Nothing on add or refine | **Yes** — the only door that writes evidence (`memory.go:1316-1330`) |
| Overriding a suggested answer with a reason | the answer's scope | Nothing; errors dropped (`question.go:2062-2081`) | No |
| A failed tool call | owner of the file it read | Nothing; replayed later as `<prior_outcomes>` (`contextual_outcome.go:75`) | n/a |

Findings:

- **Saying it explicitly gives a weaker memory than not saying it.** "Binding"
  rules, which are reserved first in every request and handed to task workers,
  come only from approved evidence (`contextual_memory.go:824-862`) **[code]**.
  The explicit doors write none, so `/remember always run the tests` is a plain
  line. It shows up only if the router picks it, and the word-match fallback
  labels it `History only — not an approved rule` **[code / reported]**.
- **The same intent lands in three scopes, under identical receipts** **[code]**.
  Typing `/remember` saves for you everywhere. Saying it in words or letting the
  model save it keeps it to this repository.
- **"remember that …" in chat does nothing on a fresh install.** The router is
  not called when the shortlist is empty (`memory.go:835-842`) **[code]**. Only
  the post-turn extractor might catch it, as an ordinary proposal.
- **`/remember` typed on Home shows no receipt** **[seen]**. Home echoes only
  during the synchronous call, and the receipt arrives later into the
  conversation behind Home (`homeslash.go:354-366`, `memory.go:187-193`)
  **[reported cause]**. The same is true of `/forget` and `/memory <query>`.
- **Conversations started in `~` or `/tmp` orphan their project memories.**
  Their project key is a hash of a per-conversation scratch directory
  (`cmd/codeaf/chatv3_layout.go:418`), and project is the default scope
  **[reported]**.
- **Text the person never typed can become "user" authority.**
  `learnFromTurn(user.text(), …)` runs on every completed turn with no check of
  who started it (`loop.go:1328`) **[code]**. The reader traced task reports,
  team-manager directives and the clarify child's prompt arriving as user turns.
  `userSupported` only checks that the quote is a substring
  (`contextual_memory.go:124-127`) **[reported]**.
- **`writeRemembered` can say `remembered · …` when nothing was written**: when
  the decider skips, or targets an id that is not visible
  (`memory.go:1450-1482`, `:1617-1621`) **[reported]**.
- **A title is the text's first words** (`memoryTitleFrom`, `memory.go:1987`).
  The router sees only titles (`reflex.go:112`), so a long `/remember` line is
  routed on its opening clause **[code]**.

## 3. A later conversation using what was remembered

- **The read path needs a model call on every message.** It runs beside the
  turn. If it lands before the first token, the request is cut and re-sent; if
  later, it rides the next step (`memory.go:616-796`) **[code]**. This
  contradicts the design doc's principle 3, "never required on the read path"
  (`docs/memory-architecture.md:49`).
- **The "helped" ranking is dead.** `recordMemoryOutcome` has no production
  caller since #1777. Only the demo seeder writes `use_count` and `miss_count`
  (`memory.go:1123-1125,1184`, `cmd/codeaf-demo-home/seed_memory.go:106`)
  **[code]**. One of the shortlist's three rankings is therefore empty. The
  extractor is still asked, and paid, for `used`, and the answer is discarded.
- **Editing an auto-learned memory removes it from recall, silently and for
  good.** A row tagged `contextual-evidence` is eligible only while its newest
  evidence is newer than its last update (`contextual_memory.go:969-974`)
  **[code]**. A memory-place edit, a tidy refine, a restore or an explicit update
  bumps `updated_seq` without new evidence. The row stays `active` on screen and
  is never offered again, while still occupying one of the eight shortlist slots
  **[reported]**.
- **A fixed line keeps its old title.** The place's edit writes text only
  (`place_memory.go:566`, which passes `memory.Title` through) **[code]**, and
  titles are what the router picks by. Fix "Ships on Fridays" to say Tuesdays,
  and the router is still shown "Ships on Fridays".
- **The binding pass can overwrite the routed block within a turn.**
  `prepareBindingContext` assigns `a.memoryText = block` with no routed half
  (`contextual_memory.go:337-346`) **[code]**. If the router finished first, its
  memories are replaced for that turn **[reported ordering]**.
- **Assistant- and tool-derived memories are rarely or never eligible.** They
  need an exact source-revision match (`store/contextual.go:500-511`).
  Assistant proposals with no snapshot are never eligible, yet they are stored
  and take shortlist slots **[reported]**.
- **Every tool call costs a git snapshot when memory is on.** `executeTool`
  captures one before each call (`loop.go:3612-3615`); the reader measured a
  2.5 s ceiling **[code]**.
- **Nobody can see which memories a turn used.** The `<memory>` note is invisible
  **[reported]**.
- **A task node's recall can run remember or forget silently.** The router
  command runs even when nothing is recorded, with a nil say function
  (`memory.go:855-857,897-899`) **[reported]**.
- **The system prompt and the memory-off rule disagree for workers.** Workers
  and standing runs receive memory notes but have no `remember`, so they are also
  told "say plainly that memory is off" (`beltfacts.go:833`) **[reported]**.

## 4. Looking at what is remembered — the memory place

On screen (demo, 12 seeded memories):

```
 11 held · 3 shelves · 1 let go                                     type to filter
 shelves · biggest first          fact 6 · preference 2 · decision 2 · correction 1 · project state 1
 ▾ you · 5 · mostly facts · 5 new today                                            now
 · Ships on Fridays · fact · let go                                                now
 · It is the strip, not the toolbar · correction · helped 3 times                  now
 · No emoji anywhere they read · preference · helped 6 · bore on 2                 now
 ▸ 2 more, on this shelf
 ▸ this project · 4 · mostly decisions · 4 new today                               now
 ▸ this machine · 3 · mostly facts · 3 new today                                   now
```

- **"this project" holds every project's memories, quarantined ones included.**
  The snapshot groups by the `scope` column with no owner filter
  (`store/memory_snapshot.go:144-146`; the shelf label is set at
  `memoryplace.go:21-25`) **[code]**. `/forget` and recall see only the current
  project, so the shelf shows lines that never apply here and that `/forget`
  cannot reach. The comment at `memory_snapshot.go:53-57` still says the store
  never kept which project a memory belongs to; #1777 added that.
- **`bore on 2` means the opposite of what it says.** It renders `MissCount`, the
  number of times a line was shown and bore on nothing (`memoryplace.go:378-380`)
  **[seen / code]**. In production, both counts are frozen anyway.
- **The foot advertises keys that are not bound.** On a line it reads
  `enter ask me about it · e fix the wording · f forget it`. Pressing `e` typed
  `e` into the filter **[seen]**. `e` and `f` act only after `→` opens the row's
  strip (`place_memory.go:797-852`).
- **The card's foot is wrong too.** With a card open it still says
  `enter ask me about it`; Enter opened the editor (`edit memory · enter save`)
  **[seen]**. `hint()` never checks `p.expanded` (`place_memory.go:887-911`).
- **Enter on a memory line starts a new conversation about it** **[seen]**. The
  card, which is the only look-only view, sits behind `→ c`.
- **Forgotten lines are mixed into the shelves, sorted first**, and still offer
  ask, fix and forget **[seen]**.
- **Cannot do from the place** **[reported]**:
  - add a memory (every printable key goes to the filter);
  - edit a title or tags;
  - change scope or project;
  - re-home a quarantined row, although `memory.go:220-224` says the person
    re-homes them in the memory place;
  - jump to the source conversation;
  - restore anything except the last forget of this visit;
  - see or delete anything while memory is off, because no store is opened
    (`cmd/codeaf/chatv3.go:2150`).
- **The place's writes block the UI.** Forget, edit, undo and card provenance
  call the store from the key handler. On a normal launch that is a wire call
  with a 10 s deadline (`chatv3_host.go:1638-1712`, `internal/remote/client.go:83`)
  **[reported]**.
- **The page is empty for one beat on a normal launch**, until the first wire
  snapshot arrives (`chatv3_host.go:1669-1675`) **[reported]**.

## 5. Forgetting

- **`/forget` deletes the single best full-text match, without asking.** Query
  words of three or more characters are OR-ed (`memory.go:1730`,
  `store/facts.go:2049-2064`) **[code]**. On the demo, `/forget the` from Home
  forgot `make check is the end-of-change ritual`, on a different shelf from
  anything that was in view. Home showed no receipt; the only trace was
  `let go of 1` under "since you left" **[seen]**.
- **There is no undo from the transcript**; the place's one-deep undo covers only
  its own forgets **[reported]**.
- **`/forget` also retires matching recorded tool failures**, so the receipt can
  name something that is not a memory (`memory.go:1745-1825`) **[code]**.
- **The per-turn decider can retire a person's own preference.** It can
  supersede anything (`memory.go:1473`). Only the tidy refuses to retire
  preferences, decisions and corrections (`memory_consolidate.go:631-637`)
  **[reported]**. The manual says the guard is general.
- **A late `/forget` reports failure** even though the engine may finish the work
  (`tui3/memory.go:249-252`) **[reported]**.

## 6. Turning it off

- **Off kills more than memory.**
  - The state card that compaction and checkpoint handoff lean on is fed only
    by the memory extractor (`memory.go:1133-1135`, `card.go:1-14`,
    `checkpoint.go:5240`) **[code]**. So memory off, the lean profile, or a task
    worker means no state card.
  - Skills move to a temporary database (`chatv3.go:1277-1291`) **[reported]**.
- **Off does not stop everything that remembers.** Saved-program memory
  (`internal/substore/memory.go`) and the bash fix hints (`v3/fixes.json`) keep
  writing **[reported]**. The row's text says "Off remembers nothing".
- **A toggle needs a process restart**, and possibly an engine stop
  (`chatv3_process.go:274`). The off note never says so **[reported]**.

## 7. Over time

- **The tidy never touches project memories**, which are the default scope.
  `v3StandingPosture` sets no project key, so only user and machine rows are
  read (`memory_consolidate.go:195-203`) **[reported]**. Its prompt says "You are
  given every note".
- **A tidy that errors is paid, unmetered, and retried every five minutes.** Cost
  is dropped on error (`standing/tick.go:143-148`) and the watermark is not
  written (`memory_consolidate.go:329-344`) **[reported]**.
- **"Merge" rewrites one row and leaves the duplicate** **[reported]**.
- **`project_state` memories go stale by design.** There is no decay
  (`memory_consolidate.go` header). The demo fixture itself carries
  `Work lands on chat-v3-task`, a branch that no longer exists, at
  `helped 11` **[seen]**.

## 8. The other things that remember

Seven stores hold things a model acts on. A preference or rule can land in four
of them, with nothing coordinating:

| Store | Where | Model sees it as | Person can see/edit |
|---|---|---|---|
| Memory | `graph.db` | tail note, "Facts, not requests" (`agent.go:2858`) | memory place |
| Standing `hold` rules | `v3/standing/*.json` | message[0], "not suggestions. Work within them" (`standing_world.go:56`) | `/standing` (no text edit) |
| `AGENTS.md` / `CLAUDE.md` | the repository | message[0], "rank above your defaults" | the file |
| Approvals ("always allow") | profile `config.json` | effect only | `/settings` |
| Fix hints | `v3/fixes.json` | a line on a failed bash result | no |
| Saved-program memory | `subharnesses/<name>/memory.md` | that program's model | the file |
| Resident notebook (preference/taste facts) | the same `graph.db` | not in v3 | not in v3 |

- **"always run the tests" can become two rules in one turn.** The `stand`
  tool's description routes "a rule, convention or preference" to a standing
  `hold` (`tools_standing.go:233`). The post-turn extractor can turn the same
  words into an approved memory rule. Neither knows about the other.
- **Memory and standing disagree on what a project is.** Memory keys a project by
  git origin, so worktrees share; standing keys by exact path
  (`standing.go:772-782`), so they do not.
- **Failure learning exists twice.** Fix hints and memory's prior outcomes both
  pair a failure with what worked next, through different stores and gates.
- **The 2026-10-09 decision to fold standing rules into memory as "always" is
  not implemented on `dev`**; `docs/memory-architecture.md:57` still says memory
  and standing orders stay separate.

## 9. The manual and docs

- `what-i-remember.md` is 62 KB in 44 sections, 10 of them over the ~2,000
  character rule. It contradicts itself at least five times, for example "There
  is no expiry" (606) against "can expire from recall" (1033) **[reported]**.
- Stale claims the chat will repeat to a person **[reported]**:
  - the reflex cost section: a 200-token ceiling and reasoning switched off,
    both removed in #665 on 2026-09-09; `models-and-cost.md:1156` repeats it;
  - example row types `lesson` and `quirk`, which belong to the resident's facts
    table;
  - age wording `3h ago` / `2w ago`, where the place draws `3h` / `2 Jan`;
  - "memory-off history is not searchable", which has been false since search
    was decoupled;
  - the tab-bar claims in §1.
- `internal/session/prompts/system.md:181` tells the model memory is only
  `<memory>` and lookups. It also receives `<prior_outcomes>`, impacts and
  binding rules. It is never told `/remember`, `/forget` or `/memory` exist.
- The repository's `CLAUDE.md` warns that `note`/`forget` need a
  `Config.MemoryFile`. Neither the tools nor the field exist any more; the model
  has `remember` and `memory_evidence`.

## 10. Dead or half-built code **[reported unless marked]**

- `recordMemoryOutcome` and `SnapshotMemoryRanking` in production **[code]**.
- `memoryNeighborsWithEvidence`.
- `store/memory_sync.go` export/apply: a sync seam with no transport.
- `MemoryPage`: keyset pagination with no production caller, so `/memories`
  prints the newest 50.
- `memoryRemoteWord`: `hostMemory` is always set.
- The `brain()` store fallback.
- The tui3 memory setting label, overridden by the config presentation.
- Test-only helpers: `memoryBlock`, `composeBeforeRequestContext*`,
  `withBindingContext`, `recordContextualMemory`,
  `contextualEligibleMemories`.

<!-- Data and vocabulary audit of the 18 home-rethink mockups against the tree on
     branch home/rethink-v0. Every claim below was checked by opening the file
     named; nothing was taken from the discarded recon dump or concept inventory on trust.
     Where the two disagree with the code, the code wins and the disagreement is
     stated. Written 2026-08-25. No Go file was modified. -->

# DATA AUDIT — what the mockups draw, and whether the program holds it

Three verdicts are used, and they mean exactly this:

| Verdict | Means |
| --- | --- |
| **AVAILABLE** | a Go type/field/func holds it today; the entry names it and says what reading it costs on a draw |
| **DERIVABLE** | the raw records exist, the figure does not; the entry says from what, and whether a new cache or ledger is needed |
| **MISSING** | no record exists; the entry says what would have to be written, by which code path, and whether that is cheap |

"Cost on a draw" is measured against the law at `internal/tui3/tui3.go:633` — *a
seam must not block: home calls it on every three-second beat and on the
keystroke that opens the screen*. Five costs appear: **cached** (already in
`homeView`), **file** (one `os.ReadFile`/walk per beat), **SQLite** (a
`database/sql` round trip), **shell** (an `exec`), **network**.

---

## 0. Three framing facts the mockups get wrong, and everything else follows

**0.1 — There are two stores, two memory concepts, and only one of them is v3's.**
`internal/store` is the **resident's** SQLite graph (`~/.agentfield/graph.db`).
It holds `store.Fact` — nine kinds, free-form scopes, channels, supersession
chains, questions and practice. `internal/session` (the v3 engine) contains
**zero** references to `store.Fact`, `RecordFact`, `SearchFacts` or
`ActiveFacts`. What v3 has is `store.Memory` — a **different table in the same
file** (`internal/store/memory.go:110`), with three scopes and five types, and
nothing joins the two. Screen 2d is drawn almost entirely out of the fact table,
i.e. out of the other product. See §7.

**0.2 — There are two unrelated "role" systems.** The spend page's
`execution / conversation / verification / naming / planning` are
`store.ModelRole` via `config.ModelSlots()` (`internal/config/modelslots.go:89`)
— five router slots. `internal/roles.Role` (`internal/roles/roles.go:35`) is a
*different*, open registry of ~20 auxiliary-call names (title, compaction,
planner, worker, auditor, taskname…) grouped under four tiers. Only the second
is ever recorded beside a cost, and then only for auxiliary calls. See §8.

**0.3 — `internal/tui2/homes/` is the v2 surface, not v3.** The screen notes
cite "homes/spend.go" and "homes/standing.go" as though they were the live
files. They are `internal/tui2/homes/spend.go` (409 lines) and
`internal/tui2/homes/standing.go` (158 lines), package `homes`, imported by
`internal/tui2/chat`, `internal/tui2/rail` and `internal/command` — and by
**nothing in `internal/tui3`**. Worse, `homes/spend.go` is not a page at all: it
is the status line's money segment plus an inline numeric budget editor
(`renderReading` :324 draws one line, `$ 8.65 / 20.00`), and its constructor
`NewSpend` (:94) has **no caller anywhere in the repo**. The laws the mockups
quote from those files ("under a cent", "the rail is edited on the segment that
shows it") are real laws — they are just not laws v3 has ever enforced, because
v3 has no equivalent file. Adopting them means writing them into `internal/tui3`
for the first time.

---

## 1. The pulse — screen 2b, 3b

Drawn today by `pulseLine` / `pulseSegments`, `internal/tui3/pulse.go:77` and
`:109`, off one reader `machineFactsAt` (`internal/tui3/homemachine.go:107`),
cached for `homeEvery` = 3s.

| Mockup fact | Verdict | Where it comes from |
| --- | --- | --- |
| `4 moving` | **AVAILABLE** | `machineFacts.hands` (`homemachine.go:79`), already the third segment (`pulse.go:146`). **cached** |
| `2 want you` | **DERIVABLE**, cheap | there is no needs-you count on `machineFacts` (the struct is `homemachine.go:48-80`: `watching, news, chats, tasks, spent, ceiling, firing, hands`). The list exists — `homeView.attentionNeeds()` (`internal/tui3/homeattention.go:406`) gathers waiting errands, items with `NeedsPerson != ""`, sessions where `row.NeedsPerson()`, and task rows at `session.TaskUnverified`. `len()` of it is the figure, computed at build, **cached**. One new field on `machineFacts` keeps the one-reader law (`pulse.go:109` comment) |
| `$0.55` | **AVAILABLE** | `machineFacts.spent` ← `machineDay` (`homemachine.go:278`). **cached** |
| `/ $20.00` — the fraction | **AVAILABLE data, LAW CONFLICT** | the ceiling is `config.DailyBudgetUSDAt(profileDir)` (`internal/config/budget.go:20`, default `DefaultDailyBudgetUSD = 20.0`, `internal/config/config.go:114`) via `machineAllowance` (`homemachine.go:335`). But `pulse.go:152-154` states in as many words: *"the FIGURE changes colour and never grows a fraction: what the day is allowed is the machine card's `today` band and nowhere else"*, and `homeband_today.go:21-28` says the same from the other end. The mockup reverses a written law. It is a decision, not a data gap |
| `tue 1:11pm` | **AVAILABLE** | `pulseClock` (`pulse.go:172`), `strings.ToLower(now.Format("Mon 3:04pm"))`. **cached** |
| `on watch · N orders` — dropped by the mockups | — | still drawn today (`pulse.go:112-123`); the redesign silently removes it. `keeping-an-eye.md` and `home.md` quote `pulseWatchWord`/`pulseOrderWord` verbatim, so removing it is a manual edit in the same commit |

---

## 2. The tab bar, the counts, and "since you left"

### 2.1 The tab counts (`tasks 1`, `memory 2`)

**MISSING — there is no per-place stamp, and only one stamp of any kind.**

`session.LastLook(root)` / `session.NoteLook(root, at)`
(`internal/session/look.go:33`, `:52`) read and write **one** RFC3339 instant in
one dotfile, `.last-look`, beside the project buckets — one per **places root**,
not per place, not per session, not per tab. It is written **on the way out**
(the file's own comment: *"a stamp taken when the screen opens would declare
everything seen the moment it appeared"*), so it means *since you last closed
home*, and `homeView.seen` (`home.go:573`) is read once on open and does not move
while home is up.

So `memory 2` cannot mean "two memories changed since you left this place": the
memory table has no per-place look stamp, and neither has any other place.

Two honest routes:

- **cheap** — use the one global stamp for all seven tabs. `memories` would
  compare `store.Memory.UpdatedAt` (`internal/store/memory.go:145`) against
  `LastLook`; `tasks` would compare `TaskIndexEntry.EndedAt`
  (`internal/session/task_index.go:181`) against it, which is exactly what
  `phoneNotes` already does against `row.At` today
  (`internal/tui3/homephone.go:424-432`). No new record. The semantics are
  "since you last closed home", which is defensible and must be said in the
  manual.
- **honest but new** — a per-place stamp file (`.last-look-<place>`), written by
  whichever code closes that place. Cheap to write (one small file per place,
  same `NoteLook` shape), but it is a **new record** and seven new writers.

Note the deliberate refusal already in the tree, `homephone.go:421-425`:
*"Nothing is asserted about 'last looked' — this surface has no such record and
will not invent one."* That comment is about the per-conversation case and would
have to be revised, not ignored.

### 2.2 The "since you left" ledger lines

Today's equivalent is `machineFacts.news` = `homeView.phoneNotes()`
(`internal/tui3/homephone.go:398`), drawn by the `sinceleft` band
(`homeband_sinceleft.go:33`). It gathers exactly three things, all **file**
reads once per beat, cached in `home.inbox`:

1. `standing.PeekProjectInbox(root, workspace)` (`internal/standing/inbox.go:172`) — peeked, never drained
2. per-session news files (`readHomeNews(row.Dir)`)
3. task rows whose `EndedAt` is after `row.At`

Line by line against the mockups:

| Ledger line | Verdict |
| --- | --- |
| **"a watch fired at 6am — nothing had changed, and it says so"** → *standing* | **DERIVABLE, and NOT from the inbox — the inbox is silent by law.** A run that came to nothing **writes no note**: `standing.go:42-44` (*"QUIET IS THE DESIGN"*), `tick.go:378-382`, `standing_run.go:662-663` (*"A RUN THAT CAME TO NOTHING TELLS NOBODY"*). So the one line the mockup calls its best is precisely the line the standing contract refuses to deliver. It **is** composable from the item itself, which is already in `home.items` per beat: `Item.LastFired` (`standing.go:351`) + `Item.LastOutcome == "nothing"` (`:352`, `OutcomeNothing` `:701`) + `Item.LastCheckLine` (`:339`) — the field written precisely for this, whose comment says *"what lets a watch that checked faithfully for thirty mornings and found nothing read differently from one that never ran"*. Note the two caveats: `LastCheckLine` is only meaningful for probe/file/idle kinds and for a hinted routine — a plain `WhenEvery` routine's line is always `"it was the time you asked for"` (`tick.go:254`); and today's word is **`checked`**, not "last look" (`standRollup`, `homestanding.go:416-428`). **cached** |
| a note that *did* fire → *standing* | **AVAILABLE.** `standing.Note{At, ItemID, Words, Kind, Text, Run}` (`standing.go:659-668`), `Kind` one of `said / landed / needs-you / failed` (`:663`), written by the runner at `standing_run.go:412-436`, read non-destructively by `PeekProjectInbox`. It carries `At`, so a since-you-left comparison is real. **file**, cached in `home.inbox` |
| **"learned 2 things about codeaf, let go of 1 you corrected"** → *memory* | **MISSING as an event; DERIVABLE as a diff, expensively.** There is no memory event log a home surface can read. On the v3 side, `store.Memory` carries `UpdatedAt` (`memory.go:145`) and `CreatedSeq`/`UpdatedSeq` (`:146-147`), so "learned since X" is a filter over a **full-scan** list (`ListMemories`, `memory.go:552` — with `limit<=0` it appends no `LIMIT` and does a correlated event-table subquery per row). "**let go of 1**" is worse: **every** memory reader hard-filters `status = active` (`GetMemories` :505, `SearchMemories` :541, `ListMemories` :554, `MemoryIndex` :586, `MemoryCandidates` :695). From outside the package there is **no way to enumerate or count forgotten memories at all**. New store method required: a status-agnostic `ChangedMemoriesSince(t)`. Cheap to add (the `memories_status_updated` index already exists, `memory.go:182`); it does not exist |
| **"…you corrected"** — correction provenance | **DERIVABLE on the fact side, MISSING on the memory side.** `store.Fact` has `StatusOrigin FactChangeOrigin` (`internal/store/facts.go:251`) with `FactOriginUser` (`:199`), and `Store.RetractedFacts(limit)` (`facts.go:1169`) is a ready-made read of exactly "quarantined by a user". One call site even threads the person's own message seq — `internal/head/acts.go:454` passes `run.user.Seq` — so the sentence they typed is recoverable. But two other user-origin call sites pass `0` (`internal/resident/craftverbs.go:198`, `internal/command/command.go:1163`) and the provenance is lost. `store.Memory` has **no origin field at all** |
| **"execution moved to sonnet 4.5 when opus hit its rail"** → *spend* | **MISSING outright.** A fall-through ladder exists — `roles.Ladder` (`internal/roles/roles.go:426`) — and `Agent.callRole` (`internal/session/auxiliary.go:77`) walks at most `roleFallThroughs = 1` extra rung (`:55`, `:82-84`). **The substitution is never recorded.** The failed rung's error goes into `lastErr` and is dropped (`auxiliary.go:127-131`); nothing in `internal/roles`, `internal/provider` (retry.go, effortladder.go, limiter.go) or `internal/session` writes an event, a log line or a ledger row. Only trace: `journalUsage.Model` (`internal/session/sessionfile.go:179`) records the model that *answered*, so a diff against configuration could infer it after the fact. **And the five router roles do not use this ladder at all** — so the sentence as written ("execution moved…") describes a mechanism that does not exist for that role. Writing it needs a new event, emitted by `callRole` and by whatever repoints a router client, into a new ledger. Cheap per event; the ledger is new |
| **"gmail moved itself to ask-first after a failed send"** → *settings* | **MISSING twice over.** The three states are real — `connect.CapabilityState` `StateYes`/`StateAsk`/`StateOff` (`internal/connect/capability_state.go:13-19`), persisted in `connections.json` as a flat `map[service]map[capability]state` (`capability_store.go:22`, `:26`). But: (a) **nothing auto-demotes** — the only writer is `Manager.SetCapabilityState` (`capability.go:256`), called from the panel and the mid-chat "always" answer; (b) the file carries **no timestamps and no history**, so even a manual change is not an event. Both the behaviour and the record would be new. The record is cheap (append a line to a small JSONL beside `connections.json`); the behaviour — deciding a send failed in a way that warrants demotion — is a real design |

**Summary: of the five ledger lines the mockups show, none is available verbatim
today. One (the watch) is composable from the item; one (memory) is derivable at
real cost and needs a new store read; three describe records the program does not
keep.**

**The good news is structural: the delivery road is already generic.** The row
type is `homePhoneNote{words string; at time.Time; project, dir string; row
session.SessionRow; hasRow bool; proj session.Project}`
(`internal/tui3/homephone.go:137-148`) — **free text plus a timestamp**, exactly
the shape every mockup line needs. The gatherer `phoneNotes` (`:398`) is a loop
over sources, the cache (`inboxAt`), the ordering, the fold (`machineNewsShown`,
`homeband_sinceleft.go:49`) and the press-to-open are all built. So a new ledger
line is **a fourth source in one loop**, not a new surface. The one wrinkle: a
machine-wide event (a capability change, a model fall-through) is not
project-scoped, so it needs either a machine-level inbox or a fourth source read
directly under the same `homeEvery` cache.

---

## 3. Chat rows — screens 1a, 1d, 2b, 3b

| Cell | Verdict | Source |
| --- | --- | --- |
| state glyph | **AVAILABLE, different shapes** | home's own constants are `homeAskGlyph "▲"`, `homeLiveGlyph "●"`, `homeStuckGlyph "◌"`, `homeIdleGlyph "○"` (`home.go:214-218`), with ASCII fallbacks `! * o -`. The mockups use `? ◐ ○ ✓ ✕` from `internal/tui2/tokens/glyph.go`. Both sets exist; they are not the same set, and `home.md` quotes home's. Changing them is a glyph decision plus a manual edit, not a data question |
| title | **AVAILABLE** | `SessionRow.Title` (`internal/session/world.go:165`), model-named; `homeName(row)` derives a readable one. **cached** |
| project tag (`codeaf`, `~`) | **AVAILABLE** | `SessionRow.Project` / `ProjectDir` (`world.go:160-161`) — the comment says they are on the row precisely so a flattened list can cite where a hit came from. **cached** |
| age (`2h`, `13h`, `1d`) | **AVAILABLE** | `SessionRow.At` (`world.go:172`) — *when the person last spoke*, and `sinceAt` renders it. **cached** |
| **"asks: add a --report-only mode?"** | **PARTLY AVAILABLE** | `SessionRow.Reason()` (`world.go:238`) returns `Presence.Reason`, the one line a live session is stopped on. But see §4 — there is no free-form question kind, so this text is the reason string of a *consent*, *task* or *standing* question and nothing else |
| **"wants to send on your behalf"** | **DERIVABLE** | this is a consent question (`session.QuestionConsent`, `internal/session/answers.go:93`) whose `Text` the gate wrote. Home would be paraphrasing the reason string, not reading a new field. Nothing records "on your behalf" as a category — `connect.Capability.Acts` (`internal/connect/capability.go:62`) is the nearest flag and lives on the capability, not on the question |
| **"2 tasks running"** | **AVAILABLE** | `SessionRow.Tasks.Running` (`world.go:274`), judged by `SessionRow.Runs(entry)` (`world.go:253`) — the one place that judgement is made. **cached** |
| **"· reading filings"** | **AVAILABLE** | `TaskIndexEntry.Activity` (`internal/session/task_index.go:201`) — the call in flight, in one line. **It is `json:"-"` and never written to the file** (`:197-200`), so home only sees it for rows merged in from a live graph in this process. For another window's task it is **empty**. So the clause draws for your own window and vanishes for everyone else's — an honesty problem the design must decide about |
| **"rendering, 4 of 9"** | **DERIVABLE, with a caveat that matters** | there is no `N of M` field anywhere. Two half-answers: (a) `taskLiveState.Steps` (`internal/session/task_live.go:102`) is *steps finished so far* with **no denominator** — rendered today as `"thinking · 4 steps so far"` (`task_live.go:277-281`); the only denominator is `max_steps`, a *checkpoint allowance* that may be granted four more times (`internal/session/task.go:122`), so `4 of 40` would be a lie. (b) For an **adaptive** run the denominator is real: the run writes one `TaskIndexEntry` per node with `Parent` set to the family root (`internal/session/orchestrate.go:1734`), so *children done / children total* is a genuine `4 of 9` from the index alone. The caveat is stated in `orchestrate.go`'s own design: the graph **crystallizes as it goes**, so the denominator is "planned so far" and can grow. `4 of 9` would have to be honest about that |
| **"3 files made"** | **AVAILABLE, worded differently — and two different things share the phrase** | *Files a task wrote*: `homeFilesTouched(row)` (`home.go:4519`) sums `TaskIndexEntry.FilesChanged` (`task_index.go:144`), already drawn as `touched 3 files`. *Deliverables*: `session.Artifact{Path, Session, Title, Kind, Created}` (`internal/session/artifacts.go:36`) in the machine-wide `~/.codeaf/v3/artifacts.jsonl`, attributed to a **session** and **never to a task**. Crucially, that ledger has only five writers and all of them are media or export — `tools_video.go:226`, `tools_speak.go:123`, `tools_music.go:205`, `tools_image.go:185`, `tui3/export.go:137`. **A code file a task wrote never lands in `artifacts.jsonl`.** So "made for you" and "3 files made" are two ledgers with two meanings and must not be merged. **cached** (`home.deliverables`) |
| **"ran a saved harness"** | **MISSING BY ONE FIELD — the cheapest fix in this audit.** | The knowledge exists and is thrown away on the way to the index. A subharness run **is admitted as a task node**: `Agent.admitSubharnessRun` (`internal/session/subharness_run.go:107`), titled `"run · " + name` (`subharnessNodeTitle`, `:89`), with kind `TaskKindSubharness = "subharness"` (`internal/session/task_contract.go:63`, derived by `taskSpec.kind()`, `internal/session/harness_task.go:412`), persisted on the checkpoint as `taskRecord.Kind` (`task_store.go:265`). But `indexEntryLocked` (`task_index.go:595-653`) copies title, status, cost, model, tokens, duration, `EndedAt`, `SessionID` — and **never `n.spec.kind()`**. So home can only string-match the `"run · "` prefix on `entry.Title`, which is fragile. Add `Kind TaskKind \`json:"kind,omitempty"\`` to `TaskIndexEntry` and one line in the builder, and the row carries kind + title + `EndedAt` + `SessionID`. Additive (old rows decode empty, which is what they were), no new file, no new walk. **The same field also solves `adaptive` in §6.** Separately: *which* harness ran is not persisted at all — `taskSpec.run` is deliberately excluded from the checkpoint (`task.go:228-233`), and `substore.RunNote` (`internal/substore/runs.go:26`) is keyed by harness name, overwritten each run, and carries **no session id and no task id** |

---

## 4. 1b — the triage stack

| Fact | Verdict |
| --- | --- |
| the question text, two sentences | **PARTLY.** `session.PresenceQuestion.Text` (`internal/session/taskpresence.go:220`) is *"the one line the session is stopped on, and it is the SAME line `SessionPresence.Reason` carries"*. One line, one sentence. The mockup's second sentence ("It costs a flag and a branch in the writer") has nowhere to live |
| the question's *kind* | **AVAILABLE and closed.** `QuestionKind` has exactly three members: `QuestionConsent`, `QuestionTask`, `QuestionStanding` (`internal/session/answers.go:93-98`). **There is no free-form "the model is asking you a design question" kind**, so "add a `--report-only` mode?" is not a shape home can hold today |
| `y yes n no` | **CONFLICTS WITH A WRITTEN LAW.** `AnswerOption.Key` (`answers.go:104`) is *"a digit on every kind, because the surface that offers these is home, where the letters are already typing"*. The real option sets are: consent `1 allow once / 2 always / 3 deny`; task `1 yes / 2 no`; standing `1 yes / 3 just once / 0 not set up` (`answers.go:144-163`). And home *draws what the session offered, never what this build knows* — the chips come out of the presence file written by the other window (`homeband_answer.go:24-27`). So re-lettering them means changing `answers.go`, which changes the conversation's own card too, on every build in flight. Screen 3a's `→`-strip does make letters safe on home — but the keys are not home's to choose |
| `s skip` | **MISSING**, and it is only UI state — no record needed, but the strip must be built |
| `Options.Answer(dir, kind, id, key)` | **AVAILABLE** — `internal/tui3/tui3.go:319`, and every press re-checks `SessionPresence.Fresh` at the instant of the press (`homeband_answer.go:33-38`). **file** |

---

## 5. 1d — the card

| Fact | Verdict |
| --- | --- |
| `~/codeaf` | **AVAILABLE** — `SessionRow.Workspace` (`world.go:168`). **cached** |
| `master, 1 file dirty` | **AVAILABLE** — the `repo` band, `internal/tui3/homeband_repo.go:26`, from `git -C <ws> status --porcelain=v2 --branch` (`:18`). **shell**, and the one shell-out on the surface: TTL-cached in `home.repos` at `homeRepoTTL = 5s` (`:11`) and refreshed **only on a cursor move** (`refreshHomeRepo`), never on a frame |
| `here` | **AVAILABLE** — `homeHolding(row)` (`home.go:4405`): `row.Transcript == a.file` → `"open here"`. **cached** |
| `it is stopped on you` + the question | **AVAILABLE** — the `state` band (`homebands.go:521`) and the `answer` band (`homeband_answer.go:57`) |
| work rows with cost | **AVAILABLE** — the `work` band (`homeband_work.go:54`) over `row.Tasks.Rows`; `TaskIndexEntry.Cost` (`task_index.go:164`). **cached** |
| `made for you · …/reports/swarm-decomposition.md` | **AVAILABLE** — `deliverables` band (`homeband_deliverables.go:17`) → `session.ReadArtifacts` (`internal/session/artifacts.go:92`). **file**, cached in `home.deliverables` |
| `spent $1.63 · 3.6M tokens` | **AVAILABLE** — `homeFacts` (`home.go:4373`) sums `row.Spend + row.Tasks.Spend` and `row.Tokens + row.Tasks.Tokens`, and `world.go:200-208` states the law that the two are counted in different files by different writers and *added where they are drawn*. **cached** |
| `· thinking high` | **DERIVABLE, deliberately withheld.** The `thinking` band is registered for `bandKindMachine, bandKindItem` only (`homeband_thinking.go:53`), and `ctrl+v` on home refuses a session's rung on purpose (`home.go:2187`). `effort.Resolve(Scope)` (`internal/effort/resolve.go:93`) does carry a conversation scope, so the figure exists. Adding it to a conversation card reverses a stated decision |
| `→ verbs: new chat here, put it away` | **AVAILABLE** — `ctrl+t` (`home.go:2106`), `ctrl+e` → `session.SetArchived` (`home.go:2152`) |

---

## 6. 1e — the tasks page

Today the task page (`internal/tui3/taskview.go`, `/history`, `ctrl+.`) is
**per-project** ("every task this project has run", `commands.go:220`), a live
forest plus past rows, filtered by typing — **not** grouped by state and **not**
machine-wide.

| Fact | Verdict |
| --- | --- |
| grouped by state | **DERIVABLE** — `TaskIndexEntry.Status` is `running/queued/done/failed/unverified`; the surface word is already `taskStateWord` (`taskview.go:1666`), which translates `TaskUnverified` → **`needs your look`** as the design law demands |
| per-task project / session name | **AVAILABLE** — `TaskIndexEntry.SessionID` (`task_index.go:183`); the project is the bucket the index was read from |
| progress `18 of 40` | **see §3** — no denominator exists for an ordinary task; real only for an adaptive family, as *children done / children planned so far* |
| **`adaptive` as a word on the row** | **DERIVABLE, by sniffing a path — and nothing in the repo does.** `TaskIndexEntry` has **no kind field**. The flag exists in memory as `TaskNotice.Run`/`.Node` (`internal/session/task_contract.go:207-210` — *"Empty means ordinary work"*) and on disk only in the per-conversation checkpoint as `runRecord.Run` (`task_store.go:311-337`). All three index writes for an adaptive run (`orchestrate.go:1543`, `:1734`, `:1921`) consume `f.run` **only to build the URI**. The one surviving discriminator is the path shape: adaptive → `~/.codeaf/v3/runs/<session>/<run>/` (`orchestrateJournalPath` :1234, `orchestrateFamilyURI` :1587); ordinary → the session's `tasks/` dir (`task_run.go:3732`). `Parent` does **not** help — ordinary task families set it too (`taskIndexParent`, `task_index.go:655`). Same fix as the harness row above: one `Kind` field. **cheap** |
| cost | **AVAILABLE** — `TaskIndexEntry.Cost` |
| age | **AVAILABLE** — `TaskIndexEntry.EndedAt` |
| `51 since aug 2, $34.10 of it` | **DERIVABLE across projects, not free.** Counting all tasks on the machine means reading every project's index — the world walk already does this per beat (`session.ReadWorld`, `internal/session/world.go:305`), so the rows are in memory; summing them is arithmetic. The **date floor** is what needs deciding: `TaskIndexEntry.EndedAt` is the only date, and `ReadTaskIndex` caps at the newest rows |
| `gave up, said why` | **AVAILABLE** — `TaskIndexEntry.Outcome` (`task_index.go:139`), the first sentence of the node's report, with `Status == failed` |

---

## 7. 1f / 2d — the memory page

**This is the largest gap in the whole set, and it is a gap of identity before it
is a gap of data: screen 2d is drawn out of the resident's fact table.**

### 7.1 Which store

| Concept | v3's own (`store.Memory`, `internal/store/memory.go:110`) | the resident's (`store.Fact`, `internal/store/facts.go:240`) |
| --- | --- | --- |
| kinds | 5 **types**: `fact, preference, decision, correction, project_state` (`memory.go:71-75`) | **9 kinds**: `preference, quirk, lesson, fact, unsettled, skill, playbook, question, trait` (`facts.go:50-73`) |
| scopes | a **closed enum of three**: `user`, `project`, `env` (`memory.go:81-85`, enforced by `validMemoryScope` :1197) | free-form lowercase strings (`normalizeScope`, `facts.go:1945`) |
| statuses | `active`, `superseded`, `forgotten` (`memory.go:90-92`) | `candidate, active, superseded, quarantined` — plus a separate question lifecycle `open, practicing, resolved, retired` (`facts.go:183-193`) |
| help/miss | `UseCount int` / `MissCount int` (`memory.go:134-135`) | `Uses int` / `LastUsed` (`facts.go:268-269`), explicitly **not journaled truth** — `Rebuild` resets them |
| reached by v3 | yes — `Options.Memory MemoryStore` (`tui3.go:252`) | **no** — `internal/session` never references a fact API |

**The mockup's kind list is wrong in both directions.** It names
`preference / quirk / lesson / skill / playbook / question / trait` — seven, and
misses `fact` (the *default* kind) and `unsettled` (a structured pair of
competing approaches, not prose). And these are fact kinds, so a page drawing
them is a page about the resident.

**Scopes.** `repo:/path`, `tool:git`, `env`, `file:/path` are real spellings,
generated by `resident.ExtractCues` (`internal/resident/notebook.go:46-90`:
`"file:"+name` :62, `"repo:"+dir` :68, `"tool:"+word` :84 from a hardcoded
thirteen-word tool list :31-34, plus `user` :87 and `env` :88). **`domain:go` is
accepted by validators but generated by no code** (`compatibleGardenScopes`
`facts.go:1917`, `validPlaybookScope` :1971) — it only exists if a model wrote
it. **`you` is not a scope at all** — it is a display label for the human speaker
(`internal/tui3/export.go:74`).

### 7.2 The individual figures

| Mockup fact | Verdict |
| --- | --- |
| `helped 14 times` | **AVAILABLE on the v3 side** — `store.Memory.UseCount` (`memory.go:134`). The memory panel already draws a `"* "` prefix at `UseCount >= 5` (`memorypanel.go:206`). **SQLite** |
| `helped 6 · bore on 2` | **AVAILABLE** — `UseCount` / `MissCount` (`memory.go:134-135`) |
| `rode 19, held up 18` | **AVAILABLE but EXPENSIVE and resident-side** — `store.FactOutcome{FactSeq, Rides, Bad, LatestBadSeq}` (`facts.go:866`) via `Store.FactOutcomes()` (`facts.go:876`). That is one query, but a **five-CTE join over the whole `events` table** with `json_each` on every `fact_injected` payload, plus `MAX(seq) GROUP BY node_id` over every delivery gate and every node failure, with **no limit and no scope filter**. Cost grows with total journal size. **Must never be called on a draw** |
| age words `3h ago`, `2w ago` | **AVAILABLE** — `store.AgeLabel(at, now)` (`facts.go:29`): `""` for zero, `"just now"` under 1h, `"%dh ago"`, `"%dd ago"` under 14d, `"%dw ago"` under 60d, `"%dmo ago"` beyond. Already used by the memory panel at `memorypanel.go:149` |
| `let go, you corrected it` | **DERIVABLE on facts, MISSING on memories.** `Fact.StatusOrigin` + `FactOriginUser` (`facts.go:251`, `:199`) and the ready-made `Store.RetractedFacts(limit)` (`facts.go:1169`). `applyFactQuarantine` explicitly **does not** write `status_note` (`facts.go:1741-1763`) — so a quarantine carries no reason prose, only an origin and an evidence seq. A *supersession* does (`SupersedeFactWithReason`, `facts.go:710`, writes `status_note`). `store.Memory` has **no origin field at all** |
| `wants your eye` — unsettled / candidate / retracted | **AVAILABLE, resident-side** — `FactCandidate` (`facts.go:184`), `FactUnsettled` (`:61`) with `UnsettledPair`/`UnsettledTrial` (`:77-88`) and the literal string `TrialDidNotSettle = "ran, didn't settle"` (`:90`), `FactQuarantined` (`:187`). Note: **there is no `forgotten` and no `retracted` fact status** — both map to `quarantined`, and only `StatusOrigin` tells them apart |
| `gaps it knows it has · 14 open questions, 2 being practiced` | **AVAILABLE, resident-side, and the vocabulary is banned in spirit.** `FactQuestion` (`facts.go:69`), lifecycle `open/practicing/resolved/retired` (`:189-192`), read by `Store.Questions(status, limit)` (`internal/store/practice.go:140`) — one indexed query each. But the whole practice loop is the resident's (`PracticeCharterShape = "system:practice-loop"`, `practice.go:19`), and its only UI today is v2's (`internal/tui2/chat/notebook.go:323`). See §11 |
| the undo on a memory (not a fact) | **PARTLY — and it fails in the same direction.** `RestoreMemory` (`internal/store/memory.go:452`) restores a **forgotten** memory only; `applyMemoryRestore` (`:1079-1092`) will not lift a **superseded** one. Exactly the asymmetry facts have (`RestoreFact` requires `quarantined`, `facts.go:1767`). And `ForgetMemoryFromSession` (`memory.go:425-450`) records only `{ID, SourceSession}` — **no reason** |
| `1,240 held · 86 shelves · 41 let go` | **MISSING as counts.** There is **no aggregate function for memories** — grepping `COUNT(` in `internal/store` finds only two `memories` hits, both existence probes inside write transactions (`memory.go:304`, `:390`). "held" today means `ListMemories("", 0)` then `len()`, which materializes every active row with a **correlated event-table subquery per row** (`memory.go:929`) and a JSON decode per row, to produce one integer. "shelves" can never exceed **3** for memories (closed enum); free-form shelves exist only on `facts`. "let go" is **unreachable** — every memory reader hard-filters `status = active`. The codebase has already written down the honest version of this problem, at `internal/command/notebook_reads.go:63-65`: *"there is no count read behind the facts table, so a full window reports itself as one"* |

### 7.3 3e — the tidy receipt, and whether it can be undone

- `FactOriginConsolidator` — **AVAILABLE**, `internal/store/facts.go:201`. Three writers: `internal/resident/notebook.go:589` (the sleep pass quarantining a harmful line, gated at `notebook.go:470` on `outcome.Bad >= 2`), `internal/store/activation.go:52` (`AgeFactsBounded`), `internal/store/meta.go:1057`.
- **"82 lines became 9" is NOT an event. MISSING.** There is one journal table — `events` (`internal/store/store.go:507`) — and no consolidation `EventKind` exists in the whole enumeration at `store.go:91-278`. A pass emits N unrelated `EventFactLearned` / `EventFactSuperseded` / `EventFactQuarantined` rows with **no batch id, no pass id, and no single timestamp**. The "82 → 9" grouping could only be reconstructed heuristically, by clustering `fact_superseded` events on adjacent `seq` and a shared `by_seq`.
- **"the 73 it retired are still readable" — TRUE.** `Store.FactLineage(scope, limit)` (`internal/store/lineage.go:21`) returns active *and* superseded lines for one scope, newest first, and its header was written for exactly this reading.
- **"`u` put the 82 back" — NOT POSSIBLE.** `Store.RestoreFact` (`facts.go:819`) is the only inverse and `applyFactRestore` **requires `status = quarantined`** (`facts.go:1767-1771`). There is **no un-supersede** anywhere in the package. So a consolidator's *quarantines* are reversible and its *supersessions* are not — and supersession is the shape a rewrite takes. The undo the screen promises is new store work.
- `SupersedeFact` / `SupersedeFactWithReason` — `facts.go:703`, `:710`. `written by a model` is derivable from `Fact.Channel == FactChannelDistilled` (`facts.go:211`, derived from `FactWriter` by `ChannelForWriter` `:226`).

### 7.4 Blocking — and how tui3's existing memory surface gets around it

**It does not get around it.** `internal/tui3/memorypanel.go` reads SQLite
**synchronously on the keystroke**, on the Update goroutine. `openMemory()`
(`memorypanel.go:255`) calls `ListMemories("", 500)` (`:261`), then loops
`MemoryProvenance` **once per memory** (`:267-272`) — worst case **1 + 500
round trips before the frame returns**. The whole chain from `Update`
(`app.go:1870`) through `a.slash` (`input.go:995`) to `openMemory` (`app.go:5078`)
is synchronous. There is no goroutine, no `tea.Cmd`, no TTL cache.

What *is* right is that `View()` never touches the store: `rows` and `height`
(`memorypanel.go:156`, `:130`) read only the cached `p.all/p.hits/p.origins`. So
the cache exists and the **fill** is what blocks — which is survivable behind a
slash command that opens a panel once, and **not** survivable on a home surface
redrawn every three seconds.

Every fact read is worse still. `factsWhere` (`facts.go:1569`) unconditionally
calls `applyFactCredibility` (`:1579`) → `ChannelSurvivalStats()`
(`internal/store/meta.go:72`), a `COUNT(*) GROUP BY kind, channel` over `facts`
LEFT JOINed against a DISTINCT scan of every supersede/quarantine event. **The
cheapest fact read in the codebase already pays a journal-wide aggregate.**

The existing non-blocking precedent to copy is v2's notebook page,
`internal/tui2/chat/notebook.go:170-187`: a throttled cache at
`notebookRefreshEvery = 1s` (`:144`) that stamps the timestamp **before** the
read is attempted (`:179`), so a backend that cannot answer stops being asked
after the first frame. `PERF.md` has no law about SQLite on the draw path today,
so nothing in the build would fail — but home's own laws would be broken.

---

## 8. 2c / 2e — the spend page

### 8.1 Per-day spend for 14 days

**Two of the three halves exist; the third is the whole problem.**

Records that carry a **date and money**:

| Record | Date | Money | Granularity |
| --- | --- | --- | --- |
| `session.TaskIndexEntry` (`task_index.go:112`) | `EndedAt` (`:181`) | `Cost` (`:164`) | per task, exact instant |
| `standing.Entry` (`internal/standing/standing.go:637`) | `At` (`:638`) | `USD` (`:642`) | per firing; **already one file per local day** (`Store.LedgerPath`, `standing.go:618`) |
| `journalUsage` in each transcript (`internal/session/sessionfile.go:178`) | `Timestamp` (`:165`) | `CostUSD` (`:184`) | **per call** |

**Conversation spend has no date.** `session.Meta.SpentUSD` and `Meta.Tokens`
(`internal/session/place.go:189-190`) are **lifetime scalars**, collapsed at
write time by `Agent.stampSpend`. `machineDay` (`internal/tui3/homemachine.go:278`)
says so in its own header (`:271-277`): *"a conversation's own spend is a
lifetime total on its meta.json with no day in it, so it is NOT split across
days here"* — and it is a *since-midnight filter*, not a date bucket
(`machineDayStart`, `:321`), returning one scalar and never a series.

So:

- **DERIVABLE**: per-day **task** spend (group `TaskIndexEntry` by `EndedAt`), per-day **standing** spend (already day-filed; `RunsSince` walks up to `ledgerReach = 31` days, `internal/standing/ledger.go:82`, so 14 days is inside the bound). Neither has a day-keyed series type — `RunsSince` returns `map[itemID]Spend`, not `map[day]Spend`. New aggregation code, no new record.
- **MISSING**: per-day **conversation** spend. The raw material exists (one `journalUsage` line per turn, with a timestamp) but reading it means opening **every transcript on the machine** — the exact cost `Meta.SpentUSD` was created to avoid. The clean fix is a **new per-day conversation-spend ledger** written beside `Meta.SpentUSD` by the same `stampSpend` path. Cheap to write; it is a new record.
- **MISSING**: `aug 20 was the loudest day` — same gap, nothing ranks days.

### 8.2 Per-model, per-role, with call counts and tokens

**The model, the calls, the tokens, the cost and a timestamp all exist on one
struct — and nothing aggregates them.**

```go
// internal/session/sessionfile.go:178
type journalUsage struct {
    Model      string  `json:"model,omitempty"`      // :179
    Input      int     `json:"input,omitempty"`      // :180
    Output     int     `json:"output,omitempty"`     // :181
    CacheRead  int     `json:"cacheRead,omitempty"`  // :182
    CacheWrite int     `json:"cacheWrite,omitempty"` // :183
    CostUSD    float64 `json:"costUsd,omitempty"`    // :184
    Calls      int     `json:"calls,omitempty"`      // :185
    DurationMS int64   `json:"durationMs,omitempty"` // :186
    Aux        bool    `json:"aux,omitempty"`        // :187
    Role       string  `json:"role,omitempty"`       // :196
}
```

Written by `sessionFile.appendUsage(used, model, aux, role)` (`sessionfile.go:1295`)
from exactly two sites: the turn's own seal (`internal/session/loop.go:569` — model
yes, role empty) and auxiliary calls (`loop.go:2463` — model and role both), each
line stamped `Timestamp` RFC3339Nano (`sessionEntry.Timestamp`, `sessionfile.go:165`).

**Model, role, tokens, cost and a timestamp coexist on exactly one record in the
whole program — and it is flattened on read.** The replay arm at
`sessionfile.go:791-813` sums the numbers into one flat `session.Usage` and
**discards `Model`, `Role` and `Timestamp`**. Everything downstream inherits that
flattening: `session.Usage` (`internal/session/session.go:680`) has `Calls int`
(`:697`) and **no `Model` field**, and `Agent.addUsage` (`loop.go:1703`) folds the
cost in at `:1739` without ever reading `response.Model`. The model is discarded
exactly where the cost is accumulated.

- **"opus 4.1 · 312 calls · 18.1M · $21.40"** → **DERIVABLE only by walking every transcript** and grouping `journalUsage` by `Model`. No aggregate exists. Per-task, `TaskIndexEntry.Model` + `Cost` + `Tokens` give per-model *task* spend with no call count, and `Model` is empty when the task took the conversation's.
- **The ROLE column** → **MISSING, and thinner than it looks.** `journalUsage.Role` holds `internal/roles` names, **not** `store.ModelRole` — and only **three** roles ever reach a spend record at all: `auxRoleTitle`, `auxRoleTaskName`, `auxRoleIntake`, written through `addAuxiliaryUsageAs` (`internal/session/loop.go:2420-2434`) from three call sites (`title.go:126`, `taskname.go:244`, `subharness_intake.go:176`). **Every other auxiliary call journals `role: ""`** (`addAuxiliaryUsage`, `loop.go:2409`), and a turn's own seal journals `role: ""` explicitly (`loop.go:569`). There is no `execution`, `conversation`, `verification` or `planning` on any spend record anywhere. `internal/roles` itself has no cost or token field, and `internal/store/role_bindings.go` is routing only, with zero spend columns. `internal/provider/attribution.go` is **not** spend attribution — it is 66 lines of OpenRouter HTTP headers (`HTTP-Referer`, `X-Title`).
- `internal/store/usage.go` (`TotalUsage` :75, `NodeUsage` :36, `Store.Usage()` :255) is the **resident's** and unreachable from v3 — the only writer of a turn-usage row in the whole tree is `internal/resident/runner.go:1033`. Even there, per-model aggregation is MISSING: the nearest are `Store.NodeModels` (`usage.go:1150`) and `JobSpend.Models []string` (`usage.go:642`), which are name lists.

### 8.3 Role bindings and "planning unbound / follows execution"

**AVAILABLE, and the mockup's phrasing is the code's own.**
`config.ModelSlot{Slot, Label, Role, Follows, Held}` (`internal/config/modelslots.go:29`),
`ModelSlots()` (:89). Slots: `talk`=conversation, `plan`=**planning**,
`work`=**execution**, `verify`=verification, `scribe`=naming, plus
image/speech/music/video/voice. The follow rule is `modelslots.go:100-105`:

```go
if role != store.RoleOrchestrate && role != store.RoleWork {
    slot.Follows = work        // work == "execution"
}
```

and the person-facing hint at `internal/config/settings.go:2170` says *"the model
that plans and reviews the work. Empty follows the work model."* **file** read.
One caveat: `plan` is `Held: true` — the engine holds a real plan client — so
"unbound" is the *empty-row reading of the settings slot*, and it is `verify` and
`scribe` that are genuinely unresolved (`settings.go:2176`: "Nothing reads this
binding yet").

### 8.4 "what it was for"

| Row | Verdict |
| --- | --- |
| a task's cost + label | **AVAILABLE** — `TaskIndexEntry.Label` + `Cost` |
| `a task, adaptive` | **DERIVABLE** — see §6 |
| `standing · 14 firings · under a cent a run · $0.06` | **AVAILABLE, and the only part of this page that needs no new data.** `Store.RunsSince(from)` (`internal/standing/ledger.go:101`) returns `map[itemID]standing.Spend{USD, Fired}` (`standing.go:648`) directly; `USD/Fired` is the rate. `Spend.count` (`ledger.go:133`) is the one place the counting rule lives |
| `which harness / which session` | **MISSING** — see §3, no harness provenance on a task row |

### 8.5 Tokens

`TaskIndexEntry.Tokens` (`task_index.go:176`, input+output as one sum, datable
via `EndedAt`); `Meta.Tokens` (`place.go:190`, lifetime, **not datable**);
`TaskRollup.Tokens` (`world.go:291`); per-call four-way split in `journalUsage`.
**`standing.Entry` carries no token field at all** (`standing.go:637`). So
"41.2M tokens over 14 days" is **partially derivable**: tasks yes, conversations
no, standing never.

---

## 9. 2e — the composer's three task facts

| Line | Verdict |
| --- | --- |
| `in ~/codeaf, on master` | **AVAILABLE** — workspace from the scope chip; branch from the `repo` band's cached git reading (**shell**, TTL 5s, refreshed on cursor move only) |
| `execution runs on opus 4.1` | **AVAILABLE** — `config.ModelSlotFor(slot)` (`modelslots.go:119`). **file**; home does not read it today |
| **`it may spend up to $2.00 before it asks`** | **SPLIT.** For an **adaptive run** this is exactly real: `orchestrate.Fuel{Cap, Spent}` (`internal/orchestrate/orchestrate.go:195`), set from `Options.Cap` (`internal/orchestrate/run.go:57`), exposed as `fuel_dollars` on `run_adaptive` (`internal/session/tools_harness.go:292`), rendered `"$1.60 of $2.00"` by `Fuel.Gauge()` (`internal/orchestrate/fuel.go:206`), warning at `WarnMark = 0.80` (`:35`), and at the cap it **asks rather than kills** — `Charge` (`fuel.go:143`) parks the run at a gate with `GateTopup / GateFinish / GateStop` (`:28-30`). The default is `orchestrateDefaultCap = 10.00` (`internal/session/orchestrate.go:78`), **not $2**. For an **ordinary `propose_task` it is MISSING**: `taskSchemaJSON` (`internal/session/task.go:113-127`) is `additionalProperties:false` and offers `title, summary, brief, deliverable, acceptance, depends_on, wide, model, max_steps, no_progress` — **a task's caps are in steps, not dollars**. The nearest dollar rail is `Config.SpendRailUSD` (`internal/session/session.go:1447`), which is per-**session**, off by default, and checked only *before a turn starts* (`internal/session/rail.go:39`) |
| `Options.Errand` | **EXISTS, CARRIES NO BUDGET.** `Options.Errand func(dir, workspace string) (Agent, error)` (`internal/tui3/tui3.go:301`) — two strings in, an Agent out. `homeExchange` (`internal/tui3/homeexchange.go:240`) has no budget field either, and `Config.Errand bool` (`internal/session/session.go:1358`) is only the marker that this agent is an errand |
| a task's rail, inherited | **MISSING.** `newTaskAgent` builds the child `Config` at `task_run.go:3548-3680` and `SpendRailUSD` does not appear in that literal — **a spawned task inherits no dollar rail from its conversation**. And exhausting `max_steps` **stops, it does not ask** (`task.go:84`: *"a task cannot ask you anything once it starts"*). There is a complete per-subtree dollar rail with a question already written — `store.TaskCeiling`, `TaskRailQuestionPrefix`, `Question()` (`internal/store/task_budget.go:40`, `:78`, `:122`) — but `task_budget.go:22-23` says *"Nothing in the product sets one yet"* and `SetTaskCeiling`/`RaiseTaskCeiling`/`ClearTaskCeiling` have **zero non-test callers**. It is finished machinery with no wire to it |

---

## 10. 2f — the standing page

`standing.Item` (`internal/standing/standing.go:304`) is the best-instrumented
record in the program, and most of this screen lands on it.

| Fact | Verdict |
| --- | --- |
| the verbatim promise | **AVAILABLE** — `Item.Words` (:308), *"the person's verbatim sentence. Permanent anchor; every surface leads with it"* |
| cadence words (`hourly`, `daily · fired 6am`, `weekdays · 8:30am`, `on arrival`) | **PARTLY — the shape is there, the words are not.** `Item.When` (:315) is a closed list of six kinds: `WhenAt`, `WhenEvery` (a five-field cron line **or** a Go duration), `WhenFile`, `WhenIdle`, `WhenProbe`, `WhenHold` (`standing.go:124-143`). What every surface actually prints is `When.Words` (`:152`) **verbatim** — the model writes it at proposal time (`tools_standing.go:209`: *"when_words is the cadence said back plainly ('Mondays at 9am') — never cron"*), and `standRollup` (`homestanding.go:408`) / `standWhenClause` (`homeband_nextup.go:86-101`) just pass it through. There is **no cron→words renderer** anywhere: `ParseEvery` (`internal/standing/every.go:27`) only parses, and the legacy `internal/store/charter_spec.go:222-534` goes words→cron, the other way. So the specific words `hourly`, `daily`, `weekdays`, `on arrival` do not exist in tui3 — the honest row is `When.Words` plus `LastFired.Format("3pm")` |
| `fired 6am` | **AVAILABLE** — `Item.LastFired` (:351) |
| the last-look line | **AVAILABLE** — `Item.LastChecked` (:338) and `Item.LastCheckLine` (:339), whose comment is written for exactly the mockup's sentence |
| cost per firing, `under 1¢`, `not measured` | **PARTLY.** `Item.SpentUSD` (:354) is a **lifetime** figure including checks (`tick.go:373`, `:418`), and `Spend{USD, Fired}` sums the same way — `Spend.count` (`ledger.go:133`) adds a check's money but not its `Fired`. So a per-firing rate is only ever `USD/Fired`, with checks bleeding into the numerator; **an exact last-firing cost is MISSING** and would need `LastFiredUSD` on the item (written at `tick.go:414-418`) or a `Store.Entries(itemID, since)` reader — every exported ledger reader sums, none hands back rows. The wording is missing too: "under a cent a run" / "not measured yet" live only in `internal/tui2/homes/standing.go:144-152` (`charterCost`, `moneyFloor = 0.005` :158), and **tui3's own `dollars()` (`internal/tui3/app.go:6516-6524`) prints `$0.0041` for a sub-cent figure** rather than rounding it to `$0.00`. So v3 does not have the defect the v2 rule was written against — which means adopting the words is a style choice, not a bug fix, and the audit should say so |
| **`asks first` / `earning tenure 3/5` / `tenured`** | **MISSING, refused by the contract in writing, and resident vocabulary besides.** `Item.Grant` (:324) is **one free-text sentence**, not an enum, written only when the person said something like it (`tools_standing.go:247`, `:261`) and **read by nothing in tui3**. And `standing.go:39-40` states the refusal outright: *"There are no probation counters: the rules are the tenure."* The 3-of-5 mechanic is real but belongs to the **resident's charters** — `store.CharterAutonomy` with `CharterProbation`/`CharterTenured` (`internal/store/charter.go:34-35`), the counter `Charter.GreenFirings int` (`:314`) and `Demotions` (`:315`), promoted by `Store.RecordCharterFiringOutcome(assessment, tenureAfter)` at `green >= tenureAfter+refusals`, `tenureAfter` defaulting to **3** (`internal/store/tenure.go:361-362`, `:434`), rendered as `min(selfTenureAfter, GreenFirings)/selfTenureAfter` on the v1 resident's self page (`internal/tui/self_drill.go:490`, `selfTenureAfter = 3` at `internal/tui/self.go:50`) and as *"fired cleanly %d times, stood down %d times"* in `internal/head/lens.go:1195`. Note the wrinkle: **v3's settings sheet already ships the word** — `config.KeyTenureAfter` / `DefaultTenureAfter = 3` (`internal/config/settings.go:87`, `:820`) is drawn at `internal/tui3/settings.go:412-415` as *"how many clean firings a standing charter needs before it earns tenure"* — but nothing in `internal/standing` reads it. To back the mockup: a new `CleanFirings int` (and a rung) on `standing.Item`, written by `Ticker.fire` (`tick.go:414-421`) on a `said`/`landed` outcome and reset or demoted on `needs-you`/`failed`, plus a threshold read from that config key. That is a `Schema` bump (`standing.go:76-79`) **and an edit to the stated law at `:37-40`** |
| `1 waiting to be stood up` | **MISSING BY LAW.** `standing.Status` is a closed three: `active`, `paused`, `retired` (`standing.go:114-116`), and `standing.go:109-110` says why: *"There is no 'proposed': a proposal is a card in a conversation, and only a yes makes an item."* A pending proposal lives only as an in-memory `session.StandingNotice` on `EventStandingProposal` (`internal/session/standing_contract.go:29-73`), answered by `Agent.ResolveStanding` (`:93`) — **not persisted, and it dies with the turn**. So the count could only be read from live sessions' open cards, never from the store, and calling those things *standing items* would be a category error |
| verbs: pause, retire | **AVAILABLE** — on wide home `ctrl+e` → `standing.StatusPaused` (`home.go:2152`), `ctrl+x` → `standing.StatusRetired` (`home.go:2179`, writing `RetiredWhy = "stopped by you"`, `homestanding.go:98`). On the `/standing` page and the phone sheet the same two are bare letters `p` and `s` (`standingpage.go:535-560`, `homesheet.go:762-775`) — which is a live precedent for screen 3c's letter strip. **Existing bug worth knowing:** the item card prints the legend `"p pause · s stop"` (`homestanding.go:73`) on wide home, where the actual bindings are `ctrl+e`/`ctrl+x`; the correct legend is in `homeband_keys.go:36-45` and is only reached via the phone sheet |
| verb: **"trust it alone"** | **MISSING** — no key, no state to set; see the Grant row above |
| the exceptions | **AVAILABLE** — `Item.Exceptions []Exception` (:326), each exactly one of a workspace or a conversation (`Validate`, `:388`) |
| `needsPerson` | **AVAILABLE** — `Item.NeedsPerson` (:356), *"the one line it is stopped on. Home sorts on it"* |

---

## 11. 1g — typed results offering places

- **Chats: AVAILABLE.** `homeRank(row, project, query, now)` (`home.go:1682`) over `session.MatchQuality` (`internal/session/task_index.go:880`), with field weights name ×10, project ×6, task label ×5, task outcome ×3 (`home.go:1650-1653`) and state boosts needs-you 400 / running 200 / incomplete 60 (`:1671`). **cached** — a few hundred rows already in memory.
- **Places: MISSING as data, trivial to add.** Places are not records; a place list is a literal table. Nothing to write.
- **Promises (standing items): CONFIRMED EXCLUDED.** `projectBlock` (`home.go:1411`) draws no standing band under a query, guarded by `if query == ""` at `:1424`, and says why at `:1418-1421`: *"the box searches conversations… a band of items riding along under every hit would be rows the query never considered, drawn as though it had."* Bare projects known only through items are dropped under a query too (`:1278-1280`). The items themselves **are** in memory (`home.items`, keyed by bucket), so including them is **DERIVABLE at zero read cost** — `MatchQuality` is field-agnostic and the field weights are plain constants (`home.go:1650-1653`). Two things would be new: `homeHit` (`:1176-1230`) is built from `session.SessionRow` alone, so an item hit needs its own `homeLine` kind; and the band registry (`homebands.go:214-223`: `name`, `order`, `kinds`, `draw`) has **no "search-visible" flag** to hang the decision on. The natural text fields for an item are `Item.Words`, `Brief.Title`, `When.Words`, `LastCheckLine`.
- **`+ start a chat about "sta"`: AVAILABLE** — `homeAction` / `homeStart` (`home.go:2749`), already always-last and always-present.

---

## 12. 3d — time windows over days, weeks, months

**MISSING as machinery, on every axis.**

| Axis | What exists |
| --- | --- |
| spend | `machineDay` — one scalar, today only. `app.standWeek` (`internal/tui3/homestanding.go:923`) — the **only** multi-day window on the surface, 7 days, standing ledger only, summed per item. `ledgerReach = 31` days caps standing history (`internal/standing/ledger.go:82`). No day-keyed series type anywhere |
| tasks (when it ran) | `TaskIndexEntry.EndedAt` is a real date, so a window is derivable arithmetic over rows already in memory |
| standing (when it fired) | `Item.LastFired` and the per-day ledger files — derivable |
| memory (when it was learned) | `store.Memory.UpdatedAt`; on the fact side `Fact.Time`. Both **SQLite** |

The renderer is not the gap: `tokens.Sparkline` / `SparklineCells[7]`
(`internal/tui2/tokens/glyph.go:195`, `:223`) exists, and
`internal/tui3/homeband_spark.go` already draws a sparkline — but its data is
`a.handsRing`, an in-memory ring of 60 samples ≈ 3 minutes, **lost on exit**.
Nothing on this surface has ever held a multi-day series.

---

## 13. The search page

**The discarded recon dump was wrong on this point, and it matters:** it said
*"There is no cross-conversation full-text index"*. There is one:

- `internal/store/thread_search.go` keeps an **FTS5 index over every message** — `messages_fts` declared `fts5(session_id UNINDEXED, role UNINDEXED, body)` (:29-35), migrated and backfilled by `migrateMessagesFTS` (:44), kept in step inside the write transaction by `refreshMessageFTS` (:73). `Store.SearchMessages(terms, sessionID, limit)` (:129) is **one `MATCH` plus a rowid join**, `ORDER BY bm25(messages_fts, 0.0, 0.0, 1.0), m.seq DESC LIMIT ?` (:152), each hit bounded to `messageSearchBytes = 400` (:120) and stamped with `AgeLabel`.
- It is **populated by the v3 chat**: `internal/session/chatlog.go` posts every user message, reply and tool result into the store as it lands (`j.store.PostMessage(posted)`, `chatlog.go:270`).
- It already has a reader on the belt: `search_conversations` (`internal/session/tools_conversations.go:97`), present only where there is a store (`a.remembers()`, `:93`) — the "absent, not broken" law — and it calls `SearchMessages(query, "", limit)` with an **empty session filter, i.e. machine-wide** (`:131`), 8 hits by default, 20 maximum.
- **Cost: one indexed query. No transcript file is opened.** A semantic/embedding index is a separate matter and is genuinely **MISSING** — no vector column, no similarity search anywhere; `internal/search/` and `tools_search.go` are *web* search (Exa / DuckDuckGo / Jina).

So a search page is **AVAILABLE** — with one hard caveat: it is **SQLite, and
therefore blocking**, so it cannot be driven from home's per-keystroke box the
way `homeRank` is. It needs the throttled-cache or `tea.Cmd` treatment, and
`home.go:1625-1643`'s "no embeddings, no index" law is about *home's own box*,
not about the machine.

What `session.MatchQuality` can reach, by contrast, is **names only** —
session title, project name, task label, task outcome. It never touches
transcript text. Scanning transcripts directly would mean opening every
`transcript.jsonl` on the machine; `session.Peek` (`internal/session/peek.go:73`)
is a forward scan of **one** journal and is already cached in `home.last`
precisely because it is *"far too expensive on every frame"* (`home.go:4530-4533`).

---

## 14. The settings page the design routes things to

**First, the structural fact: home cannot reach settings at all today.** `homeKey`
swallows every key and its `default` arm inserts the character in the box
(`home.go:2320-2328`); `ctrl+,` is bound only in the chat input
(`internal/tui3/input.go:674`), which home never reaches (`input.go:306`). No
`home*.go` file names `openSettings`, `openPermissions`, `openConnect`,
`openHarness` or `runCrew`. A settings *place* is a new door, not a re-route.

The settings sheet (`internal/tui3/settings.go`, `/settings`, `ctrl+,`) has **six
tabs** — `settingTabs` (:85): Session (:69), Context (:71), Workspace (:74),
Display (:77), Providers (:80), Connections (`connectcaps.go:109`). The other
concepts are **separate overlays** with their own commands: `/connect` →
`openConnect` (`connectpanel.go:463`), `/permissions` → `openPermissions`
(`permissions.go:265`), `/harness` → `openHarness` (`harnesspanel.go:209`),
`/crew` → the crew chooser of the time (`crew.go:183`; since replaced by the `/crew` panel). `/fixes` and `/accounts` do not exist.

| Section | Verdict |
| --- | --- |
| crew | **AVAILABLE, and already a settings row** — "crew" (`settings.go:267`) plus the four tier rows reflex/small work/careful work/mastermind (`:275`, `:279`, `:283`, `:292`) and "pinned roles" (`:296`), all on **Providers**. The preset word is **derived, never stored** (`internal/config/crew.go:26-32`): `CrewAt` (`:124`) reads the four tier keys and answers `frugal`/`balanced`/`max`/`custom`. **file — and four separate `os.ReadFile`s of `config.json`, one per tier** |
| accounts | **AVAILABLE, and already the whole Connections tab** — `connect.Manager.Capabilities/CapabilityState/SetCapabilityState` (`internal/connect/capability.go:221`, `:239`, `:256`), rows built by `buildConnections` (`connectcaps.go:273`), cycled by `cycleCapability` (`:1092`). Words are `yes` / `ask first` / `off` (`connectcaps.go:115-117`). Persisted in `connections.json` as `map[service]map[capability]state` (`capability_store.go:22`, `:26`), and a value equal to the default is **deleted rather than written** (`capability.go:265`). No timestamps, no history — see §2.2. **file, uncached: one read per capability per build**, i.e. on every filter keystroke |
| permissions | **AVAILABLE, and already Session-tab rows** — "ask before running" (`settings.go:139`), "tool exceptions" (`:144`), "shell command rules" (`:152`), "guardian" (`:160`), "approval countdown" (`:170`). The policy types are `internal/approval` — `Action`/`ActionAllow`/`ActionPrompt`/`ActionDeny` (`approval.go:32-42`), `Rule` (:148), `Policy` (:158), `Decision` (:174) — a **pure policy package that reads no config and holds no state**. Widths are `session.ConsentScope`: `once` / `tool-session` (**in memory, written nowhere**) / `rule` (`internal/session/consent.go:53`, `:63`, `:80`). Persisted as two text rows in `config.json` (`KeyToolApprovals`, `KeyBashApprovals`, `internal/config/settings.go:111`, `:119`). **Correction to a common misreading: `internal/guard` is NOT permissions** — it is panic recovery (`Fault`, `Recover`, `Go`, `internal/guard/guard.go:20-75`) |
| **fixes** | **MISSING FROM THE SHEET AND MISSING A READER.** `internal/session/fixstore.go` holds `fixEntry{Tool, Error, Fix, OK, Failed, Seen}` (:298-304), `fixDocument{…Asked, Found, Worked, Failed, Entries}` (:338-346) and `fixStore` (:356), in `~/.codeaf/v3/fixes.json` (global) and `<bucket>/fixes.json` (project, `newFixShelf` :793). Counts **halve weekly** rather than expire (`fixDecayInterval = 7*24h` :75, `decayLocked` :416). **Every type and every func is unexported** — `consult` :442, `confirm` :490, `blame` :498, `tally` :502, `readFixDocument` :687 — so no surface can read it. It needs one new exported reader (a `FixRow` projection over `newFixShelf`). The word `fix` does not appear in `settings.go` at all. **file**, loaded once per store |
| harnesses | **MISSING FROM THE SHEET; the data exists.** `Options.Harnesses *subharness.Store` (`tui3.go:499`) is a **live reader handle, not data** — do not confuse it with `session.Config.Harnesses []subharness.Entry` (`session.go:996`), which is a boot-time snapshot stale for the life of the process. The store is a directory of immutable JSON (`internal/subharness/store.go:48`, `Root = "harnesses"` :28) and **holds no cache by design** (`store.go:45-47`). Run history is real and timestamped — `subharness.Trace{Id, Started, Elapsed, Trail, Edges, Spent, Err, Status, Out}` (`run.go:58`), one file per run, `SaveRun` :217 stamping `20060102T150405Z` and never overwriting — but **unbounded, nothing prunes it**, so `Runs(name)` on a harness with 500 runs is a 500-entry `ReadDir`. The only `harness` string in `settings.go` is prose in a hint at `:294` |

---

### 14.1 What each of these costs on a draw

**None of the settings-side data is SQLite, a shell-out or a network call. Every
one is a plain synchronous file read — and almost none of them is cached.**

| Data | Read | Cached? | Safe on home's beat? |
| --- | --- | --- | --- |
| connect capability states | `os.ReadFile` + JSON, **once per capability per build** (`connect/capability_store.go:181`) | no | no — snapshot it |
| connect account list | `os.ReadFile` of `credentials.json` (`connect/store.go:156`) | yes, `connTab.loaded` (`connectcaps.go:228`) | yes |
| permissions rows | `os.ReadFile` of `config.json`, ×2 (`config/budget.go:51`) | **no, deliberately** — `budget.go:160-162` says a stale settings value is worse than a slow one | no |
| crew | `os.ReadFile` of `config.json`, **×4**, one per tier (`config/crew.go:124`) | no | no |
| settings sheet values | `os.ReadFile` of `config.json`, **once per row drawn** (`config/settings.go:1034` → `readProfileConfig`) | no | no |
| learned fixes | `os.ReadFile` of `fixes.json` (`fixstore.go:687`) | once per store (`loaded` :359) | would be — but no exported reader exists |
| harness pages | ~3 `ReadDir` + 2 `ReadFile` + 2 JSON decodes **per harness** | **no, by design** (`store.go:45-47`) | no |
| harness run history | one `ReadDir` of an **unbounded** directory per harness (`store.go:255`) | no | no — the worst on this list |
| task index rows | already in `SessionRow.Tasks.Rows` | yes, home's own clock | **yes** |
| standing inbox / news | `PeekProjectInbox` + per-session `inbox.jsonl` | yes, `home.inboxAt` (`homephone.go:398`) | **yes** |
| memory / facts | **SQLite**, synchronous, no result cache; a fact read additionally pays `ChannelSurvivalStats` | no | **no** |

The pattern the codebase already trusts on this surface is one reading per
`homeEvery` answered from a view-local cache — `machineFactsAt`
(`homemachine.go:107`), `phoneNotes` (`homephone.go:398`), `standWeek`
(`homestanding.go:923`). Anything new on home has to join that pattern, and the
SQLite readers need more than that: a `tea.Cmd` or v2's throttled cache.

---

## 15. The MISSING list, in one place

Records that do not exist and would have to be written:

| # | Record | Who would write it | Cheap? |
| --- | --- | --- | --- |
| 1 | per-day **conversation** spend ledger | `Agent.stampSpend`, beside `Meta.SpentUSD` | yes — one small dated file; unlocks the sparkline, the loudest day, the token total and the per-model table at once |
| 2 | per-model / per-role **rollup** | a walker over `journalUsage`, or a rollup written at seal time | the raw lines already carry model, calls, tokens, cost, timestamp — the rollup is new |
| 3 | which of the five **router roles** a call ran under | `Agent.addUsage` / `appendUsage`, one more string | yes |
| 4 | **model fall-through** events | `Agent.callRole` (`auxiliary.go:82-84`) and whatever repoints a router client | yes per event; the ledger is new |
| 5 | **capability change** events, and any auto-demotion at all | `Manager.SetCapabilityState`, plus a new failure policy | the record is cheap; the behaviour is a design |
| 6 | **one `Kind` field on `TaskIndexEntry`** — solves `adaptive`, `subharness`, `job` and `harness` at once | `indexEntryLocked` (`task_index.go:606`), writing `n.spec.kind()` which already exists | **yes, and it is the single best value-per-line change in this audit** — additive, one field, no new file, no new walk |
| 7 | **which** harness ran, tied to a session or task | `taskSpec.run` is excluded from the checkpoint today (`task.go:228-233`); `substore.RunNote` has no session id | cheap, but two records must agree |
| 8 | a **trust counter** on `standing.Item` (green firings, demotions) | `internal/session/standing_run.go` after each firing | field is cheap; the promotion rule is a behavioural design |
| 9 | a **"proposed / waiting to be stood up"** status | `standing.Store.Create` | cheap, but check it is not just the presence question restated |
| 10 | status-agnostic **memory reads and counts** (`forgotten`, `superseded`, `COUNT(*)`) | `internal/store/memory.go` | yes — the index already exists |
| 11 | a **consolidation event** with a batch id, and an **un-supersede** | `internal/store` | the event is cheap; the undo is real work, because supersession has no inverse today |
| 12 | **per-place look stamps** (if the tab counts are to mean what they say) | each place's close path | cheap; or reuse the one global stamp and say so |
| 13 | an exported reader for **learned fixes** | `internal/session/fixstore.go` | yes |
| 14 | a **task-level dollar cap** for `propose_task`, and a rail inherited by a task child | `taskSchemaJSON` + `newTaskAgent` (`task_run.go:3548`) | schema is cheap. Note `internal/store/task_budget.go` is a **complete, tested per-subtree dollar rail with a question** that nothing in the product sets (`:22-23`) and that has zero non-test callers — the design may already exist |
| 15 | an **auto-demote** for a connected capability, plus the event | a new `DemoteCapability` beside `SetCapabilityState` (`connect/capability.go:256`), called from the tool-result seam where `Agent.capabilityOf` (`session/connectcaps.go:128`) already resolves service and capability; the record written from `(*capabilityStore).replace` (`capability_store.go:117`), which already computes both states under the lock and discards the delta | record cheap; behaviour is a design. Also note the naming problem: there is no "gmail" account — `Service.Name` is "Google" and gmail exists only as capability ids `mail-read`/`mail-send` with phrases "read your mail"/"send mail as you" (`connect/google_capability.go:39-40`) |

---

## 16. VOCABULARY

### 16.1 The banned list, verbatim

`internal/tui3/manual_test.go:44` (`TestTheChatManualDoesNotSpeakOfTheResident`)
bans these substrings, case-folded, from **every page of the chat corpus**:

```
"alt+1", "alt+2", "alt+3",
"the board", "the self page", "standing watch",
"resident employee", "front desk"
```

The test scans page **text**, not code, so it constrains what the manual may say
about a feature — and the manual law (CLAUDE.md:40-50) means a feature that
cannot be written about cannot ship.

### 16.2 Collisions

| Mockup word | Problem | Proposed replacement |
| --- | --- | --- |
| **`alt+1 … alt+7`** (3a, 3b) | **hard fail.** `alt+1`, `alt+2`, `alt+3` are on the banned list literally. The binding itself is free — there is no `alt+<digit>` binding anywhere in `internal/` — but the manual could not describe 1, 2 or 3 | keep the chord, describe it as **"hold alt and press a place's number"**, and spell the examples from four up (`alt+4 memory`). Or move the jump to a non-banned spelling. This must be settled before the map is drawn, because 3b's whole point is that the chord is *drawn on screen* — and a drawn chord the manual cannot name is a feature the chat will deny having |
| **`tenured`, `earning tenure 3/5`** (2f) | resident vocabulary and a resident mechanic — `store.CharterAutonomy`, `CharterTenured`, `Charter.GreenFirings` (`internal/store/charter.go:34-35`, `:314`), surfaced only on the v1 resident's self page. Not on the banned string list, but squarely the thing that list exists to prevent: the chat telling a person about a standing it does not keep. **The word has already leaked once**, and that is the argument for settling it now rather than later: `internal/tui3/settings.go:412-415` ships the row *"how many clean firings a standing charter needs before it earns tenure"* — in v3's own settings sheet, describing a counter no v3 code reads | **`asks first`** is already plain, keep it; **`earning tenure 3/5` → `on its third clean run of five`** or **`still being watched`**; **`tenured` → `runs on its own`** / **`goes alone`**. None of the three is a word the resident owns. And fix the settings row in the same change, or the surface says "tenure" in one place and "runs on its own" in another about the same thing |
| **`standing`** as a place name (every screen) | the banned string is **`standing watch`** (singular), not `standing`. `/standing` is a shipped command (`commands.go:127`) and `standing-orders.md` is a shipped page, so the word is fine. **The trap is adjacency**: a sentence like "a standing watch fired at 6am" would fail the gate | keep **`standing`** as the tab word; never write **`standing watch`**. The existing surface says **`keeping an eye on N`** (`standdoor.go:58`) and the manual page is `keeping-an-eye.md` — that phrasing is safe and already taught |
| **`verification`** as a role label (2c) | CLAUDE.md's machinery-vocabulary law bans `auditor`, `verdict`, `verified`, `refuted`. `verification` is not literally on the list, but it is the same word doing the same job on a screen a person reads — and the codebase already refuses it: `taskStateWord` (`taskview.go:1666`) translates `session.TaskUnverified` into **`needs your look`** on purpose, saying so at `:1662` | the slot's own `Label` is already the answer — `config.ModelSlots()` gives `verification`, so either rename the slot's label to **`checking`** / **`the second look`**, or draw the role as **`checks the work`**. Whichever is chosen, it is one constant in `modelslots.go`, so the two places cannot drift |
| **`unbound`** (2c: `planning · unbound`) | machinery. A person does not bind models | **`planning · follows execution`** alone, which is what the settings hint already says (`internal/config/settings.go:2170`: *"Empty follows the work model"*) |
| **`adaptive`** (1e, 2c) | **fine.** It is already person-facing: `/task adaptive <brief>` (`commands.go:201`), `run_adaptive` on the belt, and a shipped manual page `adaptive-runs.md` | keep |
| **`held`, `shelves`, `let go`, `tidied`** (2d, 3e) | **fine and good.** None is machinery; `let go` is a plain reading of `quarantined`, and `shelves` is a plain reading of a scope. Note only that `let go` and `forgotten`/`quarantined` must map to **one** status, or a person reads two names for one thing | keep, and pin the mapping in one function the way `taskStateWord` does |
| **`retired`** (3e verbs, 2f) | already the code's own word — `standing.StatusRetired` (`standing.go:116`), `Item.RetiredWhy` (:335) | keep |
| **`unsettled`, `candidate`, `retracted`** (2d) | `unsettled` and `candidate` are real fact statuses (`facts.go:184`, `:61`) but **resident-side**; `retracted` is **not a status at all** — the real one is `quarantined` | if the page draws v3 memories, the three statuses are `active / superseded / forgotten` (`memory.go:90-92`) and that is the whole vocabulary. If it draws facts, it is the resident's page and does not belong on home |
| **`open questions`, `being practiced`** (2d) | the practice loop is the resident's (`PracticeCharterShape = "system:practice-loop"`, `internal/store/practice.go:19`); its only UI is v2's. Not banned literally — which is a gap in the list, not a licence | if kept: **`things nobody has told me yet`** for the open ones, and drop the "being practiced" tier entirely on this surface, because v3 has no practice loop to point at |
| **`a model rewrote 3 shelves last night`** (3e) | **fine, and the best line in the set** — `written by a model` maps cleanly to `Fact.Channel == FactChannelDistilled` (`facts.go:211`) | keep the wording; see §7.3 for why the *undo* under it cannot be honoured yet |
| **`the rail`** (2c note: *"the rail is raised on the segment above"*) | in `internal/tui3` "the rail" is the **task rail**, not the money rail — a live overload | say **`the day's allowance`** on a spend page and leave "rail" to the task column |

### 16.3 Machinery-vocabulary check on the rest

No other mockup string trips `auditor / verdict / verified / refuted`. Two
near-misses worth naming: **`gave up, said why`** (1e) is exactly the right
register for `Status == failed` + `Outcome`; and **`needs your look`** (1e) is
already the code's own translation of `TaskUnverified` and must be spelled that
way and no other.

### 16.4 One thing the mockups get right and should be held to

Screen 2a's money rule — *"a figure under half a cent is stated in words —
under a cent"* — is lifted from `internal/tui2/homes/standing.go:130-158`
(`moneyFloor = 0.005`), and the comment there records the real failure it came
from: `tokens.Money` rounds to cents with no sub-cent rung, so a real $0.0017
printed as `$0.00` reads as free, and a model once wrote "$20.00 a run" beside a
measured $0.0017.

**One honest correction: v3 does not have that defect.** `internal/tui3`'s own
`dollars()` (`internal/tui3/app.go:6516-6524`) prints `$0.0041` rather than
rounding it away, and grepping `internal/tui3` for "under a cent" returns
nothing. So adopting the words is a **style decision** — is `under a cent` kinder
than `$0.0041`? — and not a bug fix, and the audit says so rather than letting a
borrowed law arrive as though it were already binding here.

---

## 17. PALETTE

### 17.1 The two tables, side by side

Every number below was computed with this package's own arithmetic:
`nearest256` (`internal/tui3/styles.go:884`, squared RGB distance across the
6×6×6 cube and the 24-step grey ramp), `lightnessOf` (HSL midpoint, as
`designlanguage_test.go:191`), and `contrastOf` (WCAG, against `darkMid =
#1a1b26`, the middle of the assumed dark range, `designlanguage_test.go:337`).

**tui3 `styles.go` — what home actually paints with**

| Role | Hex | 256 idx | L (HSL) | contrast vs `#1a1b26` |
| --- | --- | --- | --- | --- |
| `hueInk` (body) | `#C6CDDA` | 252 | 81.6 | 10.70 |
| `hueLive` | `#D8DEE9` | 254 | 88.0 | 12.65 |
| `hueAccent` | `#9DC3E6` | 146 | 75.9 | 9.26 |
| `hueMuted` | `#7FA6C9` | 110 | 64.3 | 6.67 |
| `hueNarr` | `#848FA6` | 103 | 58.4 | 5.26 |
| `hueDim` | `#6B7280` | 243 | 46.1 | 3.54 |
| `hueAdd` | `#A3BE8C` | 144 | 64.7 | 8.38 |
| `hueDel` | `#C67173` | 167 | 61.0 | 4.89 |
| `hueBad` | `#D08770` | 173 | 62.7 | 6.01 |
| `hueAsk` | `#C08FE8` | 140 | 73.5 | 6.78 |
| `hueWarn` | `#EBCB8B` | 186 | 73.3 | 10.94 |
| `hueData` | `#91C5D4` | 116 | 70.0 | 9.07 |
| `hueViolet` | `#8F6FA8` | 97 | 54.7 | 4.08 |
| `hueCursor` (ground) | `#242932` | 235 | 16.9 | 1.17 |
| `hueSelected` (ground) | `#2E3440` | 237 | 21.6 | 1.37 |
| `hueMark` (ground) | `#434C5E` | 239 | 31.6 | 1.98 |

**tui2 `tokens/palette.go` — what the mockups quote**

| Token | Hex | 256 idx | L (HSL) | contrast vs `#1a1b26` |
| --- | --- | --- | --- | --- |
| `TextPrimary` | `#E6E6F0` | 255 | 92.2 | **13.79** |
| `TextSecondary` | `#A0A6BB` | 248 | 68.0 | 7.05 |
| `TextTertiary` | `#7C8296` | 102 | 53.7 | 4.47 |
| `Amber` (needs a human) | `#EECE96` | 222 | 76.1 | 11.32 |
| `Cyan` (alive) | `#A4D7EA` | 152 | 78.0 | 10.98 |
| `Green` (money + success) | `#A2E2BC` | 151 | 76.1 | 11.51 |
| `Coral` (broken) | `#EFA99F` | 217 | 78.0 | 8.83 |
| `Ground` | `#12121A` | 233 | 8.6 | 1.09 |
| `Band` | `#262633` | 235 | 17.5 | 1.15 |

### 17.2 What home paints today, role by role

| Meaning | Home paints | Call sites |
| --- | --- | --- |
| **ground** | **nothing.** Rest is the terminal's own background. `TestOnlyTheGroundLadderPaintsABackground` (`designlanguage_test.go:155`) permits an SGR 48 in `styles.go` alone; there is no page background anywhere in tui3 | — |
| **selection band** | `hueCursor #242932` for the cursor/hover row, `hueSelected #2E3440` for a marked row | `overlayRow` (`palette.go:421-427`), `homeattention.go:890` |
| **body text** | `hueInk #C6CDDA` | 17 `pal.ink(` sites in home files |
| **dim** | `hueDim #6B7280` (56 sites) with `hueMuted #7FA6C9` for headings/labels (24 sites) | throughout |
| **needs a human** | **two inks today, and they disagree.** The `needs you` strip mark and the answer chips wear `hueAsk #C08FE8` — a **violet** (`homeattention.go:189` `pal.askBold`, `homeband_answer.go:193`). But the "finished and needs your look" task glyph `?` wears `hueWarn #EBCB8B` — an **amber** (`home.go:4477`) | |
| **alive / working** | `hueAccent #9DC3E6` — a **blue**. The spinner and the fresh-landing tick take it (`home.go:4464-4466`, `:4484`) | |
| **money** | **no hue.** A spend figure is `pal.dim`, rising to `pal.warn` only near the ceiling (`pulse.go:150-155`, `homeband_today.go`). `hueAdd #A3BE8C` (an **olive** green) means *landed*, not money — `attentionLanded` (`homeattention.go:291`) is its one home call site | |
| **success / landed** | `hueAdd #A3BE8C`, plus `pal.muted` for a settled `✓` and `pal.accent` for one that landed since you last looked | `home.go:4478-4485` |
| **failure** | `hueBad #D08770` (orange-red) | `home.go:4475`, `homeexchange.go:1479` |

So the mockups' three assignments — amber = needs a human, cyan = alive, green =
money — are **three reassignments**, not three adoptions:

- amber currently means *a bound about to be reached* (`styles.go`'s own note on `hueWarn`), and is already spent on one needs-your-look glyph;
- the alive hue is blue, not cyan;
- there is no money hue at all.

### 17.3 Would the tokens hexes pass `designlanguage_test.go`?

Computed, not guessed. Four laws bite.

**(a) THE GLARE LAW — `TestTheBodyInkDoesNotGlare` (`:499`). `TextPrimary` FAILS.**
`#E6E6F0` measures **13.79:1** against `#1a1b26`; the ceiling is
`glareCeiling = 11.0`. And it is not an accident that it fails: `styles.go:139-140`
names this exact hex as *"the glare this whole law was written about"*, and
`designlanguage_test.go:554` holds it as `oldBodyWhite`, the absolute ceiling
`hueLive` may never reach. **The mockups' body text cannot be adopted.**
`TextSecondary #A0A6BB` (7.05) and `TextTertiary #7C8296` (4.47) would both pass
as second and third voices, and neither collides on the 256 rung (248 and 102 are
free), but they are *quieter* than home's present `hueInk/hueMuted/hueDim`
ladder, so adopting them means re-tuning three tiers together.

**(b) THE SIGNAL BAND — `TestTheSignalHuesAreIsoluminant` (`:217`). All three
signal tokens FAIL, marginally.** The band is `signalBand = 15.0` points of HSL
lightness across `{accent, add, del, bad, ask, warn, data}`. Today that set spans
**14.90** (del L 61.0 → accent L 75.9) — 0.10 points of headroom.

| Substitution | New spread | Verdict |
| --- | --- | --- |
| tokens `Amber #EECE96` (L 76.1) replaces `hueWarn` | 61.0 → 76.1 = **15.10** | FAIL by 0.10 |
| tokens `Green #A2E2BC` (L 76.1) replaces `hueAdd` | 61.0 → 76.1 = **15.10** | FAIL by 0.10 |
| tokens `Cyan #A4D7EA` (L 78.0) replaces `hueData` | 61.0 → 78.0 = **17.00** | FAIL by 2.00 |
| all three at once | 61.0 → 78.0 = **17.06** | FAIL |

To admit all three the floor must rise to **L ≥ 63.0**, which means moving both
`hueDel` (61.0) and `hueBad` (62.7) — the diff-minus and failure hues on the
**chat** surface. That is the cost, stated plainly.

**(c) THE 256 LAW — `TestNoTwoRolesShareA256Index` (`:304`). All three PASS.**
Amber → 222, Cyan → 152, Green → 151; none is claimed on either ladder (the dark
walk holds 252, 254, 146, 110, 103, 243, 144, 167, 173, 140, 186, 116, 97). A
straight substitution introduces **no** 256 collision.

**(d) THE GROUND LADDER — `TestTheGroundLadderLandsInItsBand` (`:365`) and
`…StepsNeverCollapse` (`:431`).**
- `Ground #12121A` is **not adoptable at all**, and not because it fails a number: tui3 **never paints a page ground**. Rest is the absence of a paint, pinned by `TestOnlyTheGroundLadderPaintsABackground`.
- `Band #262633` measures **1.15** against `#1a1b26`. The selected band's band is **1.32–1.52**; 1.15 lands in the **cursor** band (1.10–1.25) instead. It is also `idx 235` — **the same index `hueCursor` already holds**, so using it as a second step fails `TestTheGroundLadderStepsNeverCollapse` outright. tokens' Band is simply a *quieter* band than tui3's selected step, aimed at a darker assumed ground (`#12121A`, where 1.15 reads differently than it does at `#1a1b26`).

**(e) One more thing the tokens table does not have: a light ladder.**
`tokens/palette.go`'s quoted hexes are dark-only. `internal/tui3/styles.go` ships
a full light ladder (`lightInk #3B4252` … `lightMark #B7C0D1`, `styles.go:609-655`)
and **every design-language test walks both**. Any hue adopted has to be authored
twice.

### 17.4 Recommendation

**Do not port the tokens hexes. Port the tokens *meanings*, and note that two of
the three are already in the table.**

Held at the same hue and saturation, tui3's existing signals *are* tokens'
signals, retuned into the isoluminant band:

| tokens | tui3 today | H° | S% | L |
| --- | --- | --- | --- | --- |
| `Amber #EECE96` | `hueWarn #EBCB8B` | 38 → 40 | 72 → 71 | 76.1 → 73.3 |
| `Cyan #A4D7EA` | `hueData #91C5D4` | 196 → 193 | 62 → 44 | 78.0 → 70.0 |
| `Green #A2E2BC` | `hueAdd #A3BE8C` | **144 → 92** | 52 → 28 | 76.1 → 64.7 |

Amber and cyan are the same colours, one lightness step down — exactly the move
`styles.go` documents itself making, twice, in `hueData`'s own note. **Green is
the one genuine difference**: tokens' green is a mint at H 144; `hueAdd` is an
olive at H 92. So:

1. **Ground: nothing to do, and nothing may be done.** Home paints no page ground and must not start. `#12121A` describes the terminal the design assumes, not a colour this surface may author.
2. **Selection band: keep `hueSelected #2E3440`.** tokens' `#262633` fails the selected band and collides with `hueCursor` on the 256 rung. If the *feel* of the mockups' quieter band is wanted, that is a change to `hueSelected`'s authored value inside its existing 1.32–1.52 window, not a substitution.
3. **Body / dim: keep `hueInk / hueMuted / hueDim`.** `TextPrimary` fails the glare law by 2.79 points of contrast and is the exact hex the law was written against.
4. **Needs a human: settle the two inks into one — and make it amber.** This is the single highest-value palette change in the audit, and it needs no new colour: `hueWarn #EBCB8B` **is** tokens' Amber, and home already paints the needs-your-look `?` with it (`home.go:4477`). Move the `needs you` strip mark and the answer chips from `hueAsk` (violet) to `hueWarn`, and home stops saying "a person is needed" in two colours. Cost: `hueWarn`'s stated meaning ("a bound about to be reached") widens to "waiting on you", which is a comment edit in `styles.go` and a manual edit in `screen.md`'s *What each colour means*; `hueAsk` is freed (it stays in the table, as `hueViolet` already does).
5. **Alive: keep `hueAccent #9DC3E6`, or move it to `hueData #91C5D4` if the cyan reading matters.** Both are already in the table, both pass every law. Note the accent budget (`styles.go:192-208`): one lit element per screen, and the accent is currently spent on the spinner — so painting "alive" cyan would actually *free* the accent for the person's own `›`, which is what the budget's own comment says it is for.
6. **Money: add one role, and take the tokens green's HUE at the table's lightness.** There is no money ink today. `hueAdd` cannot be reused without conflating *landed* with *cost*. Author a new `hueMoney` at **H 144, S ~40, L ≈ 68–70** — tokens' mint, held inside the signal band. At that lightness it lands near xterm-256 **114/115**, which is free (`hueData` is 116, `hueAdd` 144) — but the exact index must be recomputed against the whole table when the value is authored, per `TestNoTwoRolesShareA256Index`'s own instruction, and a light twin must be authored beside it.
7. **Failure: keep `hueBad #D08770`.** tokens' Coral `#EFA99F` (L 78.0, idx 217) would push the signal spread to 17.0 and fail the band.

**Does the chat surface change?** Under this recommendation, **almost not at
all**. Items 1, 2, 3 and 7 change nothing. Item 6 adds a hue nothing else spends.
Item 5 is home-local if applied at home's call sites. **Item 4 is the one that
crosses**: `hueAsk` is the chat's question ink too, so moving *home's* needs-you
mark to amber leaves the chat's own question violet unless it moves with it —
which is a deliberate choice, not an accident, and either answer must be written
into `screen.md`'s colour section in the same change.

The alternative — raising `hueDel` and `hueBad` to L ≥ 63 so the tokens hexes fit
the band verbatim — **does** change the chat: those are the diff-minus and
failure inks on every surface. It buys three hexes that are, in two cases out of
three, the colours the table already holds.

<!-- Ends. -->

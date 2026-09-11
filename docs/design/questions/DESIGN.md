# Questions — one object, drawn at the size the evidence needs

*Owner ruling 2026-09-09 (picks D1–D9 below are the orchestrator's defaults,
owner may override any before its lane lands). Copy of record for every lane in
the questions wave. Companions: `ideation/questions-first-class.md` (the
thinking), `ideation/questions-audit.md` (the 21 mechanisms this replaces),
`docs/design/task-states/DESIGN.md` (the `your call` tier this object opens
into), `docs/design/icons/DESIGN.md` / `internal/tui2/tokens` (every glyph).*

## The one sentence

A question is a decision handed to the person with its evidence attached. The
evidence sets the size of the drawing; the stakes set what may answer by itself;
the answer carries the person's intent whole; and a question is never lost,
never modal, and never the first thing the model tries.

## The ladder — law, enforced (D7 = A)

Before the engine puts a question to the person, the asker has climbed:

1. **the record** — never ask what a decision record or a preference answers
2. **assume, and say so** — an assumptions card; the person strikes the wrong ones
3. **act, then ratify** — reversible: do it, show a ratify card (D1 = A)
4. **keep working while asking** (D8, built 2026-09-11) — an `ask` that says `blocking: {turn: false}` comes back at once and the answer arrives later as a message; see *An answer is a message* below
5. **show outcomes** — attach what each answer produces (a diff, a rendered row, a layout)
6. **structured input** — blanks · checklist · this-or-this · dial, never free text where a key would do
7. **the question** — head, options, pick with reason and confidence, what would change its mind
8. **free text** — always available, never the only door

The engine's question gate refuses an `ask` with no reason, no stakes, or fewer
than two answers where the kind needs them, and tells the model to decide or
state the assumption. At most `QuestionCap` open questions per task; past it the
model must consolidate into one sheet.

## The object — `session.Question`

*The names below are the ones that LANDED (lane E1, `internal/session/question.go`).
Where the codebase already owned a word, the codebase won — every such rename is
noted, because one source of truth means this file says what the code says.*

**`QuestionKind` was already taken**, by answers.go, for the LANE a question came
from (`consent · task · standing`) — a value that is already on disk in presence
files and already read by home and by the `--host` link. So the object carries
**two** kind fields and they are orthogonal on purpose: two lanes can raise the
same shape (the approval gate and a sub-harness proposal are both permissions),
and one lane raises two shapes (recovery.go borrows the consent lane to ask about
a turn).

- `Question.Kind QuestionKind` — the **lane**, and therefore the resolver an
  answer is applied through. Extended from three values to eleven — the audit's thirteen
  lanes, with the connect key folded into connect and the harness design folded
  into the harness offer, because each of those pairs is one wait with one
  resolver:
  `consent · task · standing · connect · harness · subharness · subharness-ask ·
  landing · conflict · fuel · recovery`.
- `Question.Ask AskKind` — the **shape of the decision**: `permission · choice ·
  judgement · clarification · confirmation · landing · assumption · ratify`.
  (This is what this document previously called `Kind`.)

```go
type Question struct {
    ID        uint64          // the token an answer names — the lane's OWN id, never a second one
    Ref       string          // that token where the lane's is a string (a connect account, a run)
    Kind      QuestionKind    // the lane (above)
    Ask       AskKind         // the shape of the decision (above)
    Form      QuestionForm    // FormLine · FormCard · FormRoom · FormSheet — promote, never demote
    Asker     Asker           // {Kind AskerKind, Name} — model · engine · task · surface · window
    Head      string          // one sentence, the asker's own words, no machinery vocabulary
    Reason    string          // why now — one dim sentence
    Subject   SubjectRef      // {Kind SubjectKind, ID, CallID, Ref, Name}; the row already drawn
    Options   []AnswerOption  // answers.go's own type, WIDENED rather than duplicated (below)
    Input     InputShape      // {Kind InputKind, Blanks []Blank, Dial *Dial, Prompt}
    Pick      *Pick           // {Key, Reason, Confidence, WouldChange}; nil = no pick
    Stakes    Stakes          // StakesReversible · StakesCostly · StakesIrreversible
    Policy    Policy          // {Kind PolicyKind, After} — ask · recommend-then-auto · decide
    Blocking  Blocking        // {Turn bool, Tasks []string}
    Scope     []AnswerScope   // ScopeOnce · ScopeTask · ScopeProject · ScopeAlways
    Attach    []Block         // {Kind BlockKind, Title, Body, Rows, Path}
    Asked     time.Time
    Deadline  time.Time       // the clock, zero on every question that has none
    Withdrawn *Withdrawal     // {Reason, By, At}
}
```

**`Option` is `AnswerOption`, widened.** answers.go already owned the type the
presence file and every chip row carry, so it grew the fields instead of gaining a
twin: `Key`, `Label` (this document's `Word`), then `Body`, `Consequence`, `Safe`,
`Widening`, `Blocks []Block`, `Dimensions map[string]string` — all `omitempty`, so a
row that only ever read Key and Label reads exactly what it always did.

**`Answer` is answers.go's `Answer`, widened**, for the same reason: it is the line
of `answers.jsonl` an older window writes, and the four fields it started with —
`At`, `Kind`, `ID`, `Key` — are untouched, so `AnswerFromKey` still answers from a
bare key alone.

```go
type Answer struct {
    At   time.Time; Kind QuestionKind; ID uint64; Key string; From string  // unchanged
    Ref  string; Ask AskKind
    Picked []string          // Key is the first of these, kept filled for older readers
    Change string            // "2, but keep the sqlite file as the source of truth"
    Comments map[string]string
    AskedBack []Exchange     // bounded: one question and one reply per option
    Blanks map[string]string
    Dial *float64
    Reframe string           // "the real question is…"
    DecidedBy DecidedBy      // person · dial · record · asker
    Scope AnswerScope        // ScopeOnce · … — NOT ConsentScope, which is a different thing
    Why  string              // the soft ask on an override
    TakingOver bool          // the sub-harness lane's third answer
}
```

`Answer.Keys()`, `Answer.FirstKey()` and `Answer.Words()` are the readers of
either shape, so no lane has to know which it was handed.

**Scope is `AnswerScope`**, not `Scope`: `ConsentScope` already exists and means
something else (where a tool memo is banked, including a value that is about the
place rather than the lifetime).

Events: `EventQuestion` (the object, whole, after any subject rows),
`EventQuestionWithdrawn`, `EventQuestionAnswered` (the answer, whole — this is the
RECORD). They ride `Event.Question *Question` and `Event.Answer *Answer`, on the
turn's hub and on **`Agent.WatchQuestions()`**, a standing subscription of their
own that replays everything already open when a surface attaches. Deliberately
NOT the standing task lane: that one is the roster's, its readers walk a strict
sequence of rows, and a question is not a row.

One door: **`Agent.ResolveQuestion(Answer) error`**, which reads the lane and hands
the answer to that lane's own resolver — `ResolveConsentRemember`, `ResolveTask`,
`ResolveStanding`, `ResolveRecovery`, `ResolveConnect`/`ResolveConnectKey`,
`ResolveHarness`, `ResolveSubharness`, `AnswerSubharness`, `ResolveUnverified`,
`HandUnverifiedToModel`, `TakeBackDecision`, `SteerTask`, `ResolveConflict`,
`ResolveOrchestrate`. `Agent.applyAnswer` (the answers.jsonl drain) now goes
through it, so answers.go's first law is literally true across eleven lanes rather
than nearly true across three.

**`Agent.OpenQuestions() []Question`** is what is open right now, oldest first,
DERIVED from the lanes' own waits — there is no second store. `Agent.questionWords`
keeps only the WORDS a lane said, keyed by lane and token; whether a question is
still open is the lane's fact, and an entry with no wait behind it is never
returned and is swept.

**The gate** is `Question.Check(records []DecisionRecord) error`, plus
`Agent.checkQuestion` for the one bound that belongs to the set: `QuestionCap = 3`
open questions against ONE subject. Its refusals: no head, no reason, no stakes,
fewer than two answers where the kind needs them, more than 4 answers (8 on a
checklist), a pick naming an unknown key, a clock or a policy on irreversible
stakes, a policy with nothing to take — and `already decided: …` where a record
answers it. Every one ends in something the asker can do.

**The record** is `decisions.jsonl` in the session folder: `DecisionRecord`,
`Agent.Decisions()`, `ReadDecisions(dir)`, `DecisionsPath(dir)`, and
`DecisionsSection(records)` — `the record`, one line per decision, which is what
the model's context carries. `DecisionRecord.Line()` is that line: head → picked ·
with · by · when · `cannot change` where it cannot.

**The landing kind is built OVER `TaskAsk`** (`docs/design/task-states/DESIGN.md`),
never beside it: `Agent.landingQuestion` reads `ProjectTask(notice.StatusFacts()).Ask`
and dresses it, and the keys stay that design's own — `LandingYesKey` `a`,
`LandingNoKey` `n`, `LandingTellKey` `s`, with `LandingAgainKey` `r`,
`LandingDecideKey` `d` and `LandingTakeBackKey` `u` reachable through the door but
not on the row. `s` sends words to the work and LEAVES THE QUESTION OPEN — a steer
never resolves a task by itself.

## Kinds and their defaults

| kind | safe answer | may auto on a clock | free text | typical form |
| --- | --- | --- | --- | --- |
| permission | skip it | only when `Stakes == reversible` | "no, because…" goes back to the model as the refusal | line |
| choice | the pick | yes | yes | card / room |
| judgement | keep as is | never | yes | card / room |
| clarification | — | never | first | card |
| confirmation | the non-destructive answer, under the cursor | never | no | line (raised card keeps its laws) |
| landing (`your call`) | leave it | yes, on the dial | `[s] tell it` | card / room (task-states row unchanged) |
| assumption | all stand | yes — goes on after the clock | add one | card |
| ratify | already done | n/a — nothing waits | `c change` | line |

## The forms

- **line** — one row + one answers row, pinned above the box. Permission, confirmation, ratify.
- **card** — head · reason/attribution · one row per option (word · consequence · pick mark; a long word wraps, never cut; a checklist's rows carry its ticks and a digit toggles) · answers row. Transcript, home strip, other window, room foot. Every option is a row: the engine's cap (4, 8 on a checklist) is the only bound. Keys are digits `1`… in order; `ask` renumbers whatever the model wrote and hands the answer back in the model's own names. The asker's `form: line` is honoured only for plain options with no structured input; width decides the rest.
- **pointer** — every question with answers has the person's pointer (`▸` on a card, the band on a line): `↑↓`/`←→`/`tab` walk it, `enter` takes it, a digit takes at once. It starts on the asker's pick, which says `suggested` in its consequence column; on a confirmation it starts on the answer that loses nothing. The task record page (enter on a needs-you row) draws the node's landing question above its foot and takes its keys through the block's door. Landed 2026-09-10.
- **room** — a page over the conversation (the task-room idiom): head and attribution, the question's own `Attach` as ONE dim-titled section (`what it showed you`) above the answers, options as sections (`▸`/`▾`, bodies, blocks), `x` compare on the asker's dimensions (fallback: `+`/`−` lines), `c` comment on the focused part, `?` ask back (one exchange per option, answered in place, closes with the question), the foot composes the answer (pick · with · notes · scope), `d` you decide shows the pick and reason first, `D` sets the dial for the kind. **The pointer walks it**: `↑↓` choose (the foot says so), `→` opens the answer it is on and `←` folds it, `enter` takes it, a digit answers at once; a click on an answer's ROW moves onto it and opens it, and a second click on that row is `enter` — one press to read, one to decide. **An open answer reads in three tiers**: the label bold in the question hue (as on a card), the body ink, and every aside dim behind its own word — `then ·` the consequence, `why this one ·` the pick's reason, `would switch if` (the asker's sentence joined onto the clause, never `if If`) — one blank row closing each open answer.
- **sheet** — many questions from one step or many hands: grouped by shape, `✓`/`?` per row, `enter` opens one, `s` sends what is answered and delegates the rest, "same answer for all like this" on a group; dependent questions withdraw with a reason and the sheet re-flows.

Every form folds down (room → card → line → chip) and opens up (`enter`/`o`).
The chip lives in the status line: `<GlyphNeedsHuman> 3 questions · <key>` and
is reachable from every page; home lists every open question under its
conversation with the same answers row.

## Laws every lane keeps

- **NEVER MODAL, NEVER SUSPENDS THE KEYBOARD.** The box stays live; typing after `›` is answering with words; digits pick; `enter` takes the pick (only when there is one); `esc` is *later* — the question folds to the chip and the turn/task stays paused on it. The consent block's "IT SUSPENDS THE KEYBOARD" law is retired by this one.
- **THE SETTLE GUARD.** A question accepts no key pressed before it had been on screen for `questionSettle` (250ms); a key that arrived earlier is dropped, never applied. Destructive answers never share the routine key; the confirmation kind keeps stop.go's and tabclose.go's laws verbatim.
- **THE BOX IS NEVER MOVED UNDER A HAND.** A question arriving while the box holds words queues behind the chip until send or a pause of `questionQuiet` (3s).
- **PRESENCE-AWARE DELIVERY (D2 = A).** On the page: pinned now. On home or another page: in the row, plus the chip. Away (`awayAfter`, 10m without a key): dial resolves what it may and records `DecidedBy: dial`; the rest go to home and the phone; the terminal bell rings once for a blocking question only (D6 = A).
- **BATCHED AT THE BOUNDARY.** Non-blocking questions raised inside one step arrive together as a sheet at the step's end; a blocking one arrives at once with the count of what is behind it.
- **WITHDRAWN, WITH A REASON.** A question whose subject is gone, whose plan changed, or that another answer made moot is withdrawn by the asker; the surface says `⊘ <head> — no longer needed · <reason>` once, dim, and the chip count drops.
- **THE ANSWER IS THE RECORD.** A dim line stays where the question was: `decided <head> → <picked> · with: … · you · 14:02 · reversible · c change`; `c change` shows the unwind cost before it reopens; irreversible records say `cannot change`. A record is read by the model before it asks anything.
- **RULES ARE OFFERED, VISIBLE, FORGETTABLE (D4 = A).** The third same-shaped yes offers `r make it a rule for <scope>`; a row answered by a rule says `· your rule from <day> · change`. Never a hidden rule.
- **FIRST ANSWER WINS.** Two windows: the second is told who answered and what; a differing answer inside a second is shown, not merged. Late answers are ignored and nothing says so (answers.go).
- **THE EMPTINESS LAW.** No pick → no `enter →` line; no reason → no dim line; no dimensions → no compare.
- **ONE KEY GRAMMAR.** Digits `1–9` pick (letters only where task-states already fixed `a/n/s`); `space` toggles a checklist; `a/b` a pair; `←→` a dial; `tab` the next blank; `c` comment; `x` compare; `?` ask back; `d` you decide; `D` decide this kind from now on; `r` make a rule; `u` undo while real; `esc` later; `o`/`enter` open. Spelled once in one table, read by every form and by the manual.
- **EVERY GLYPH COMES FROM `internal/tui2/tokens`.** The question mark is `GlyphNeedsHuman` (always amber); assumptions `≈`, ratify `GlyphSettled`, withdrawn `⊘`, options `GlyphCollapsed/Expanded`, the room's back `GlyphScopeUp` — new meanings are added to the module with a NerdFont binding, never as a literal in tui3 (the laws-gate test fails otherwise).
- **HUE.** Amber is waiting-on-you and nothing else — which every answer on a question IS, so an answer's label is `askBold` on the card and in the room alike; the pick's reason, every consequence and every attribution are `dim`; option bodies are ink; `+`/`−` use the diff glyphs and tokens; no box drawing except the room's rule lines and blocks the asker drew.
- **ALIGNMENT.** Head at the gutter; option rows indented one key-cell; answers row indented like the options; the room's foot is pinned above the box exactly where every other question sits.
- **SCREEN-READER AND NARROW.** Every form has a linear shape; compare stacks under 80 cols; the reader tier never draws a dial (a number input instead) and never paces a reveal.
- **HEADLESS.** `--once`, `aforge engine`, a task lane: the policy applies and is printed (`asked: <head> → 1 (default · nobody to ask)`); a kind with no pick lands `your call` and pauses; nothing hangs.
- **NO MACHINERY VOCABULARY.** Never "prompt", "modal", "dialog", "approval gate" on screen.

## The model's door — `ask` (D5 = A)

One tool, `ask`, whose schema is the object minus what the engine fills (ID,
Asker, Asked, Policy): head, kind, options with bodies and blocks, dimensions,
input shape, pick with reason/confidence/wouldChange, stakes, scope, attach.
The result is the `Answer`, whole — including `Reframe` and `AskedBack` — behind
one sentence saying who gave it; with `blocking: {turn: false}` the result is
`asked · question N` and that same text arrives later as a message. The
system prompt carries the ladder in the model's own instructions and names
`ask` as the last rung. `internal/session/prompts/system.md` and the manual
state exactly when asking is allowed; the question gate enforces it.

## An answer is a message (lane A, 2026-09-11)

Until this wave an answer to the model's own `ask` was only the RETURN VALUE of
the call that asked: `executeAsk` parked the turn on a channel, and the answer
existed for the model only if that call was still waiting for it. Three things
the owner picked could not be built on that — keeping working while a question
stands, a countdown that goes with the pick, and changing an answer after it is
given — because each of them is an answer arriving when NO call is waiting.

**The road already existed.** A background job's ending, a watch's firing and a
task landing reach the model through ONE door, `Agent.accept(delivery)`
(`internal/session/mailbox.go`): the note goes on the steering queue, the loop
drains that queue at the next STEP boundary (`drainSteering`, before the next
request), and on an idle conversation the note's wake mark starts a turn
(`wakeLocked`). It coalesces, it respects the spend rail, a stopped session and a
task node's runner, and it journals the note in the session's lane. An answer is
now one more thing that rides it. There is no second delivery path.

**The one door is still `ResolveQuestion`.** Its `ask` arm hands the answer to
the lane's book, `Agent.asks` (one `modelAsk` per question the model raised in
this conversation), which renders it ONCE — `askAnswerText`: one sentence saying
what happened, then the `Answer` as JSON in the model's own keys — and then:

- a call parked on the question is handed that text as its result;
- with nobody parked, the text goes through `accept` as an owed note
  (`answerNote`: wake, not batched into "while you worked", the same delivery
  marks `relayNote` carries so a task node's runner is released by it and does
  not close on top of it).

The bytes are the same either way, and a law test holds them the same.

**Deleted:** `askWaits` (a map of channels that could only release a parked
call), the policy clock inside `executeAsk`'s `select` (a clock that existed only
while a call waited), and the ask arm that dropped an answer with nobody parked.

### The three features are three cases of it

1. **Keep working while asking.** `blocking: {turn: false}` — an explicit false —
   registers the question, puts it on every screen, and returns at once:
   `asked · question 3 · <head> · …the answer arrives as a message that starts
   "answered · question 3"`. The question outlives the call and the turn; the
   turn ending does not withdraw it. Its answer lands at the next step boundary
   of whatever turn is running, or starts one. `blocking` omitted, or `turn:
   true`, waits exactly as before — THE ZERO VALUE WAITS, as `Policy`'s does, so
   a model that says nothing gets today's behaviour. A stop (`workStopped`)
   declines the wake and the answer waits on the queue for the person's next
   line. Headless runs are unchanged: the policy decides, or `your call`.
2. **The countdown.** A question with a pick under `PolicyRecommendThenAuto`
   gets its clock from the lane's book, not from the call, so it runs whether or
   not a call is parked. When it runs out the pick goes through the one door as
   `DecidedBy: dial`, and the text says it is PROVISIONAL — the person has not
   said this and may still change it. The length is the person's own number:
   the `After` they set for this kind with `/autonomy`, and where they set none
   (the assumption kind's default) `awayAfter` — the boundary this program
   already uses to conclude nobody is at the keyboard. Irreversible stakes never
   get a clock (the gate). **One source of truth for the deadline:
   `Question.Deadline`.** A key on the question calls
   `Agent.HoldQuestion(kind, token)` (wire `Question.Hold`), which stops the
   clock for good, zeroes the deadline and re-sends the question, so every
   window stops counting on the same beat. A surface reads a recommend-then-auto
   question with a zero deadline as `paused`.
3. **Change an answer.** `Answer.Revises` — one bit on the answer that already
   travels the wire whole — says "this is a new answer to a question already
   settled", which the door would otherwise drop as a late second click. The
   door refuses a revision of an irreversible decision (`that decision cannot
   change — it was marked irreversible`), writes a new record (the gate and the
   record read the newest), emits `EventQuestionAnswered` so other windows
   update their receipts, and delivers a correction through the same road:
   `changed · question 3 · <head> · was SQLite, now JSONL…`. On the consent lane
   a revision away from a widening answer drops the conversation's memo for that
   tool, so the next call asks again; the saved settings rule is the surface's
   to remove, since the surface wrote it.

### The offer to stop asking (2026-09-11, owner)

A setting that changes what the program does on its own is never reachable only by a
slash command. `/autonomy`, the settings row and this offer are three doors onto ONE
store — `.aforge/autonomy.json`, written only through `Agent.SetAutonomy`.

WHEN the offer appears is derived from the record this session already keeps, and from
nothing else: the newest decision is the third of its shape answered the same way, by
the person, reversibly, and that shape has no explicit row yet. It is therefore offered
**once per kind per project** with nothing new to store — an explicit row silences it
for good, and both taking the offer (`recommend then go`) and declining it (`ask me`)
write one. Never for a shape the door itself refuses (a confirmation always asks; a
clarification never runs on a clock), and never on irreversible stakes.

**This is not built.** It was written and then taken out of #914 rather than shipped
with nothing calling it: the dim line on the receipt and its keys are the surface's, and
that surface reads no decision record yet — so the whole of it would have been a
function nobody could reach. Two things it needs when somebody builds it: the record
list on the surface's side of the door, and ONE counter for the `r make it a rule` offer
(tui3's `questionYeses` counts yeses per shape today, which is a second count of the
same thing).

### Limits, stated

- The book is in memory. A session host that restarts loses its open asks
  exactly as it loses a parked turn today; a revision of a question from an
  earlier life is refused with `that question is from an earlier run`.
- Only the model's `ask` and consent take a revision. Every other lane's
  answer has already moved work, and saying otherwise would be a key that lies.
- A consent keeps its own call parked — it is a gate on that call and nothing
  else. The other calls in the same batch already run beside it (`runToolsWarm`
  runs a batch concurrently); the NEXT step waits for the whole batch because a
  request cannot be assembled with a tool result missing.
- Over the engine-host road a turn the conversation starts on its own reaches the
  screen: #690 was the defect there and #856 fixed it. Driven over the default
  host road on 2026-09-11 — a non-blocking ask answered with the turn idle woke
  it, and the answer and the work it caused were drawn.

### Wire

Version 15. An older engine would read `Answer.Revises` as a late answer and
drop it silently, and would answer `Question.Hold` with no such method while its
clock went on and took the pick under somebody's hand — both are silences, and
the door refuses a mismatch rather than let a person believe they changed
something.

## Lanes

| lane | model | owns | lands |
| --- | --- | --- | --- |
| E1 contract | Opus | `session.Question/Answer`, kinds, forms, events, `ResolveQuestion`, adapters from every existing resolver, presence + answers carrying the whole object, decision records, withdrawal, the question gate, `QuestionCap`; no surface change | first, alone |
| E2 door | Codex | the `ask` tool + schema, the ladder in system.md, the assumption kind raised by the engine, preferences from `Why`, away policy + dial storage (`autonomy.json` per project), headless printing | after E1 |
| S1 block | Opus | line · card · ratify · chip · settle guard · esc-later · typed answer · receipt/record · rules offered · consent, task consent, standing, harness card, fuel gate, takeover confirm and the stop/tab-close cards migrated onto the object; the key table | after E1 |
| S2 room | Opus | the room, compare, comment, ask back, compose, you decide, blanks/checklist/pairs/dial forms, blocks (text · diagram · table · diff · image · layout) | after E1 |
| S3 reach | Codex | home strip and rows, other-window chips, room foot, phone, presence-aware delivery, the sheet and batching, withdrawal drawing, `/autonomy` sheet, first-answer-wins | after E1 |
| T proof | Opus on spark | e2e with deepseek/deepseek-v4-flash through the tmux suite: one scenario per form and per ladder rung, screens captured and rendered to PNG; manual probes for every heading | after S1–S3 |

Every lane: its own worktree off `origin/dev`, a PR against `dev`, a change
entry, the manual pages in the same change, `go test ./internal/tui3/ -timeout
15m` green, `make test-laws` green. The old mechanisms are deleted in the lane
that migrates them, never left beside the new one.

## Five numbers

questions per task · share resolved without a key · override rate of the pick ·
median time to answer · share answered off the page. Written to the spend
ledger's neighbour so `/status` can say them.

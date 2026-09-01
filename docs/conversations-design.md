# Conversations — several agents alive behind one surface, home as the switcher

*Design doc, 2026-08-21. **Revision 3**. Status: **K1 and K2 in flight** (worktrees
`~/af-k1-attach` on `feat/agent-attach`, `~/af-k2-open-seam` on `feat/open-seam`);
**K3 starts when both have landed**. Revision 2's plan — partition the surface into
N conversations — was read by the owner and **rejected as too heavy**. This revision
is the same product built the other way round, and it is a third of the code.*

*Every `file:line` below is pinned to `253b019d` on `chat-v3-task` and was
re-verified against the tree at that commit. This tree moves under several
sessions at once, so re-grep the anchor rather than trusting the number if it does
not land.*

One assumption is being removed and nothing else: **that a terminal holds one live
conversation.** Everything below follows from taking it out — and from taking it
out at the cheapest place, which is not where Revision 2 took it out.

---

## Why

### The assumption

Today a terminal running aforge is bound for its whole life to the directory it
was typed in. The workspace is resolved once at launch (`cmd/aforge/chatv3.go:406`,
`openV3Launch`; `v3Workspace`, `cmd/aforge/chatv3_layout.go:83`), the agent is
built on it once (`chatv3.go:141`), and the surface stores it in three fields that
are written in `newApp` and never assigned again — `a.workspace`, `a.place`,
`a.owned` (`internal/tui3/app.go:1246-1250`). Home (`internal/tui3/home.go`) then
reads the whole machine and lists every project on it, and `enter` on a row
belonging to any other project refuses: `elsewhere · <the project's path>`
(`home.go:1244`, through `homeOpens` at `:1336`). The manual states the limit and
says why (`internal/manual/chat/home.md:233-248`): a window's approval rules, its
crew, its spend ceiling and its saved shapes of work were all resolved from the
workspace it launched in, and carrying a conversation across without carrying
those would be a window quietly running under another project's permissions. That
reasoning is correct. The conclusion drawn from it — that the person must open
another terminal — is the part that stops being true here.

What a person gets instead: **several conversations alive at once in one terminal,
each bound to its own workspace, one of them in front.** They keep the thing that
made the refusal right — a conversation never changes the workspace it was born
in, and no gate, crew or ceiling is ever carried across — by never moving a
conversation between projects at all. A second project means a second
conversation, built the way the first one was, on its own workspace, with its own
gate. Home stops being a map of a machine you have to go to a different terminal
to act on, and becomes the switcher. Work that fans out — three tasks in one repo,
a long build in another — carries on while you are looking somewhere else, and
says `waiting on you` on home when it needs a person. That is the calm-parallelism
thesis (`docs/coordination-design.md`, "the fear caps the product") applied one
level up: fan out across projects, not only across tasks.

The three alternatives, each rejected in one line.

- **Switch in place** — close this conversation and open that one in the same
  surface, the way `/resume` already does (`welcome.go:248-309`). Rejected as
  heavy: it drops the flock, kills the tasks (`Agent.Close`,
  `internal/session/agent.go:1021-1023`), replays a journal, and gives back
  nothing you did not already have — and the work you left is the reason you were
  switching.
- **Multi-root, one agent** — one conversation whose tools may reach two
  workspaces. It is the option the manual's refusal is actually about: one gate,
  one crew and one ceiling would then govern two repositories, and there is no
  honest answer to which repository's `AGENTS.md` the prompt should carry
  (`internal/session/prompt.go` reads exactly one, from the agent's own
  workspace).
- **Another terminal** — what people do today. It works, and it is why this is a
  convenience rather than a capability: the cost is that the machine's state is
  spread across N terminals with no one place that shows it, which is the same
  complaint `docs/coordination-design.md` opens with.

### The seam lesson — why Revision 2 was heavy, and where the unit really is

Revision 2 answered "several conversations" by making the **surface** hold N of
them: a `conversation` struct carved out of `app`'s 191 fields, a conversation id
minted onto 28 of the 33 `tea.Msg` types, look-up-then-compare in place of fifteen
generation comparisons, a fold/present split on every event handler, and four
mechanical checkpoints (L1a-L1d) to move the fields without breaking 110 test
files. Ten lanes, roughly 3,500 lines, and the first three of them delivered
nothing a person could see.

That is the cost of partitioning the wrong thing. **The unit of a conversation is
already the agent, and it is already whole.** Every one of the properties this
feature needs is per-`Agent` today and has been since before home existed:

- the workspace and the tool root (`bare.AllTools(cwd)`, `internal/exec/bare/tools.go`),
- the gate, the crew, the ceiling and the saved shapes, resolved from
  `<workspace>/.aforge-v3/config.json` at the moment the agent is built
  (`internal/config/projectconfig.go:69-72`),
- the transcript and its flock (`lockSessionFile`, `internal/session/sessionfile.go:600-612`),
- the presence file and its five-second heartbeat (`internal/session/taskpresence.go:103-113`),
- the tasks, the jobs, the wake lanes, the orchestrations,
- and the snapshot doors that let anything rebuild a view of it from cold:
  `PendingConsent` (`consent.go:420`), `PendingTasks` (`task.go:543`),
  `PendingConnect` (`connect.go:398`), `TaskIndex` (`task_index.go:437`),
  `OrchestrateSnapshot` (`orchestrate.go:285`).

The surface, meanwhile, is one frame, one keyboard and one paint clock, and it has
exactly one job: **draw the agent in front.** It already knows how to be pointed
at a different agent — `openSession` (`welcome.go:248-309`) does it on every
`/resume`: swap `a.agent`, clear the per-conversation fields, bump `a.gen`,
`replay()` the transcript, re-subscribe the standing lanes. The only line in it
that makes the old conversation *stop existing* is `a.agent.Close()`
(`welcome.go:266`).

**So: keep exactly one conversation on the surface, as today, and keep the agents
alive behind it.** Switching is `openSession` minus that one line, plus somewhere
to put the agent that is being left. The field census stays as it is. There is no
`conversation` struct. There are no conversation ids on messages, because no
message from a conversation that is not in front ever reaches the surface at all —
and the generation counters that already exist make anything still in flight
discard itself, which is the job they were written for.

Three lanes, about 1,150 lines, and two of them are running now.

---

## The words

Settled here, and to be used unchanged in the code, the comments and the manual.

**Conversation.** A live agent and the surface state that belongs to it: its
transcript on screen, its rail, its draft, its meters, its lanes. It is already
this surface's word — `/new` "closes a conversation and starts a fresh one", home
calls its action row `start a new conversation` (`homeStartWord`, `home.go:218`),
and the manual has said *conversation* since before home existed. Nothing changes;
there are simply several of them.

**In front.** The one conversation the surface is drawing and the keyboard belongs
to. Exactly one, always — and now that is a statement about the architecture and
not only about the screen.

**Open.** Held by this process: the agent exists, the flock is held, the presence
file is heartbeating. `Open` is already `session.SessionRow`'s word for the same
fact seen from another window, and it keeps that meaning — which is why a
conversation open in *this* terminal and one open in another are told apart by
which window, not by a second word.

**THE PERSON-FACING WORD FOR "OPEN AND NOT IN FRONT" IS `open`.** Not `behind`.
The status line reads `2 open · 1 waiting`, home's rung reads `open`, and both are
consistent with the word another window's rows already carry. `behind` describes a
position on a screen the person cannot see, which makes it furniture; `open`
describes what is true — the agent is alive, holding its lock, running its work.

**Waiting on you.** Unchanged, and deliberately not re-coined: it is what the
presence file writes (`session.PresenceWaiting`, `internal/session/taskpresence.go:501`),
what home's rung already says (`homeNote`, `home.go:1823-1826`), and what a task
card says of work that is holding. A conversation that is open and not in front
and needs a person says exactly this and nothing else.

### The engineering words — used in the code and this document, never on screen

**Behind.** Open, not in front, and therefore not drawn. A conversation behind is
fully alive: its turn streams, its tasks run, its jobs run, its presence
heartbeats. It is not paused and not suspended; the surface simply is not looking
at it.

**The keeper.** `behind map[string]*aside` on `app`, keyed by canonical transcript
path — the agents this process holds that are not in front, each with its sidecar.
It is the whole of the new state. When a second terminal is built later, the
keeper is the thing that moves behind a socket.

**Attach / detach.** Pointing the surface at an agent, and pointing it away.
Detach is not close, and the difference is the feature. These two words are the
reason `attach`/`detach` are on the not-words list below for anything a person
reads: to a person, detaching and attaching is *switching*, and switching is what
the manual will call it.

**Not words here.** *Tab*, *window*, *pane*, *buffer*, *workspace switcher*,
*session-as-tab*, *foreground*/*background* (background already means a promoted
`bash` job — `ctrl+g`, `internal/session/jobs.go`), and — on screen — *behind*,
*keeper*, *attach*, *detach*. And the resident's vocabulary stays out entirely —
`alt+1`, the board, the self page, standing watches, the front desk — which a test
in `internal/manual/chat_test.go` enforces for the corpus and this document keeps
by hand.

The two design laws that govern every string below, restated because this feature
is mostly strings:

- **THE EMPTINESS LAW.** Unknown or zero renders as nothing. One conversation
  draws no count. Nothing waiting draws no `waiting` clause.
- **NO MACHINERY VOCABULARY.** Work is running, finishing, done, incomplete or
  needs your look. Nothing on this screen is a handle, a context, a slot or an
  instance.

---

## What a person sees

Unchanged from Revision 2 in every particular a person could name, because the
product decision did not change — only where the code puts the agents. The one
place a person can tell the difference is at the end of this section, and it is
named there rather than hidden.

### Home's rows

Home already draws everything this needs except two facts. Its rows carry a state
glyph (`homeGlyph`, `home.go:1903`: `▲` waiting, `●` running, `◌` incomplete, `○`
idle, `✓` landed-since), a name, and a dim trailing rung — `waiting on you`,
`N running`, `N incomplete`, `another window`, `N landed`, `N tasks`, then an age
(`homeNote`, `home.go:1807`). Today the conversation this window is in is marked
by paint alone: `overlayRowTinted(..., line.row.Transcript == a.file, ...)` gives
it the current treatment (`home.go:1761`).

**The mark becomes three-state instead of two.** That boolean argument becomes a
small enum — *in front*, *ours*, *not ours* — and the row list paints the front one
as it does today, the ones in the keeper in the same treatment at dim strength,
and everything else exactly as now. It is a paint and not a word on purpose: the
left column is forty-six cells wide and every column spent on furniture is a
column taken from the name (`home.go:223-230` makes that argument for
`another window` already).

**The rung gains exactly one case: `open`.** It goes where `held` goes today —
below the states, above `N landed` — and it is what a conversation this process
holds says when it has nothing more urgent to say. The ordering law in `homeNote`'s
comment (`home.go:1812-1826`) is unchanged and now covers one more row:
`waiting on you` and `N running` still outrank it, because those are facts about
work and this one is a fact about a door, and `enter` works either way.

A conversation open in a *different* aforge process keeps `another window`
(`homeHeldWord`/`homeHeldShort`, `home.go:229-230`) and keeps refusing, for the
reason it refuses now — the flock is held somewhere this process cannot reach
(`lockSessionFile`, `sessionfile.go:600-612`).

So the four spellings, exactly:

```
▲ the parser fix          waiting on you · 4m
● aforge-v2               2 running · 12m
○ notes                   open · 1h
○ somebody else's chat    another window · 3h
```

**Our own rows are read from the agent, not from the presence file.** The presence
file is written on a five-second heartbeat and believed for fifteen
(`taskpresence.go:103-113`), which is right for another window and wrong for an
agent whose pointer is in this process's own map: a person who switches away from
a question and opens home would watch their own row say the wrong thing for up to
five seconds. For a row in the keeper, home asks the agent — `NeedsPerson()` for
the accent, `TaskIndex()` for `N running` — and falls back to the file only for
rows it does not hold.

### `enter`, on each kind of row

`homeEnter` (`home.go:1213`) keeps its shape and loses one case. Its switch
becomes:

| the row is | what `enter` does | what is said |
| --- | --- | --- |
| the conversation in front | closes home into it | nothing — closing into it *is* the thing happening (`home.go:1229-1241` states this) |
| **in the keeper** | closes home and attaches it | nothing, for the same reason |
| not open, this project or any other | opens it through the seam, attaches it, and detaches the one that was in front | nothing on success; the transcript's own `resumed …` note lands in the conversation it belongs to |
| open in another aforge | refuses, home stays up | `open in another window — go there, or start a new conversation here` — today's `sessionBusyWord` (`welcome.go:231`), unchanged |
| the project folder is gone | refuses, home stays up | `that folder is gone · <path>` |
| this process already holds the cap | refuses, home stays up | `8 open is as many as aforge holds — /quit closes this one` |

The order of those rows is the order of the checks, and **the second row must be
tested before the fourth**: see "Identity before flock". `homeHeldNow`
(`home.go:1327`) asks `session.InUse`, and `InUse` (`sessionfile.go:1323`) takes an
`unix.Flock` on a fresh descriptor — which conflicts with a lock **this same
process** holds, because a flock rides the open file description rather than the
process. A conversation in the keeper would therefore report `another window`
about itself unless the keeper is consulted first.

The `elsewhere` case is **deleted**, and with it `homeElsewhereWord`
(`home.go:212`), the `· elsewhere` heading suffix (`home.go:1719-1720`),
`homeOpens` (`home.go:1336-1341`) and `a.home.bucket` (`home.go:342`). That is the
whole point of the feature and it must be hunted out of the manual in the same
lane — the corpus says the limit in three places (`home.md:233-248`,
`commands.md:636-641`, `screen.md:751`) and a gate that only checks that a word is
*mentioned* would pass all three while they lie.

**The folder-gone refusal is new and is needed because of this feature.** Home
never stats the workspace today; it stats the transcript and nothing else
(`readSessionRow`, `internal/session/world.go:385-390`). That was harmless while
`enter` only ever opened this project — you were standing in the folder. It stops
being harmless the moment `enter` opens somebody else's: a repository that has
been deleted or moved since its last conversation would be opened as an agent
whose tool root does not exist, and every `bash` and every relative path in it
would fail in a way nothing on screen explains (`internal/exec/bare/tools.go` fixes
`cwd` at construction and hands it straight to `exec.Command`). One `os.Stat` on
the keystroke, in the same place `homeHeldNow` puts its one syscall
(`home.go:1327-1332`), and the same refusal discipline: home says it and stays up.
The stat is of the **canonicalised** path.

### Starting somewhere new from home

Typing on home already searches the machine and offers `start a new conversation`
as the last row of the list (`homeStartWord`, `home.go:218`; `homeStart`,
`home.go:1289`). Today that row runs `/new` and then a submit, in this project:
`a.renew()` followed by `a.submit(text)` (`home.go:1289-1295`).

It gains one behaviour and keeps the other. **When what was typed resolves to a
directory on this machine — an absolute path, a `~` path, or a project name that
matches exactly one heading on the list — the action row instead reads
`start a new conversation in <place>` and `enter` opens a fresh conversation
there**, in front, with the current one detached and running. When it resolves to
nothing, it is today's row unchanged: a fresh conversation in this project
carrying the sentence that opened it.

The asymmetry the first review flagged — a path *adds* while a non-path *replaces*
— is resolved the same way it was in Revision 2: `/new`'s own semantics change in
the same lane, and both doors go through one function. See "Quit, close, `/new`,
and the cap".

The path is resolved but not created. A path that does not exist gets the same
refusal a gone folder gets, in home's own voice, and no directory is made — a
surface that created a folder because somebody mistyped one would be the worst
possible answer to a typo.

### The status line's count

A new segment, `segOpen`, first on the telemetry row, immediately left of
`segAmbient` (`render.go:963-973` declares the segment enum; `:1320` adds
`segAmbient`). It reads:

```
2 open · 1 waiting
```

`N open` counts the conversations this process holds — the keeper's size plus one.
`N waiting` counts the ones in the keeper whose agent answers true to
`NeedsPerson()` (K1) — the same call the presence file makes, so home, the
notification and this segment cannot disagree.

**It is absent whenever only one conversation is open**, which is the ordinary case
and the emptiness law's plainest application — a permanent `1 open` is a permanent
reminder of the absence of a feature. The `· N waiting` clause is absent when
nothing in the keeper is waiting. It is the first segment sacrificed to width
after `segDelta` (`dropOrder`, `render.go:1356`), because at forty columns the
thing a person needs is what *this* conversation is doing.

The deliberate `$0.00` exception on the live status line (`CLAUDE.md`, the
emptiness law) is not extended here. That exception exists so a cost segment does
not jump sideways as its width changes; this segment appears and disappears with a
real change in what is true, and a placeholder would be a lie about how many
conversations are open.

**Revision 2's fade-clock carve-out is no longer needed.** `segText`/`segAt`
(`app.go:644-645`) are the surface's, there is one set of them, and `segOpen` is a
segment like any other: an attach is a new frame with new numbers, and every
segment's clock restarts together.

### `tab` — the way back to the last one

**`tab`, pressed with an empty message box and nothing else claiming the keyboard,
attaches the previously-in-front conversation.** Press it again and you are back.
It is `cd -`, and it is the key almost every switching surface in computing
already uses for switching.

*Why `tab`.* The manual's own account of this problem is at `keys.md:622`, written
when home needed a door: every `ctrl+<letter>` this surface could use is taken,
`ctrl+.` is the task page, and the `alt+` letters arrive in some terminals and do
nothing at all in others. That survey is still true.

**`tab` is NOT "reliably nothing" today.** It has five other meanings, and the rule
has to be written as input precedence rather than as a claim about the key. The
whole chain, in the order the code runs it:

| # | rung | file:line | what `tab` does there |
| --- | --- | --- | --- |
| 1 | paste bracket | `app.go:4358` | a literal `\t` into `a.pasted` |
| 2 | stop confirmation | `app.go:1495` (`stopKey`) | not taken |
| 3 | the rail holding the keyboard | `app.go:1502` (`railKey`) → `task.go:3204-3232` | **falls through** — see the collision below |
| 4 | an open room | `app.go:1510` (`roomKey`) | not taken |
| 5 | consent / connect / harness cards | `input.go:210`, `:220`, `:228` | not taken |
| 6 | settings sheet | `input.go:248` → `settings.go:1437` | `right`/`tab` changes page |
| 7 | home | `input.go:258` | home's own key map |
| 8 | phone status deck, expand sheet | `input.go:265`, `:273` | that page's key map |
| 9 | model / crew / task pickers, memory panel | `input.go:281-294` → `memorypanel.go:338` | the memory panel's scope toggle |
| 10 | resume roster, deliverables shelf, connect/harness/permissions panels | `input.go:302-331` | that overlay's key map |
| 11 | rewind sheet | `input.go:368` → `rewindsheet.go:93` (`rewindSheetLiftKey = "tab"`) | lifts to the whole conversation |
| 12 | task page | `input.go:377` | that page's key map |
| 13 | copy mode | `input.go:385` | copy mode's key map |
| 14 | inline rewind | `input.go:393` → `rewind.go:328` | lifts into the rewind sheet |
| 15 | the welcome box | `input.go:404-411` | **not taken, and dismisses the box** — see the collision below |
| 16 | path completion | `input.go:417-418` → `completePath` (`files.go:772`) | opens/advances the completion, but only with a `/image ` or `/export ` prefix (`argPrefixes`, `files.go:216`; `argToken`, `:218-235`) |

So the toggle goes in at **rung 17**, immediately after `completePath` returns nil,
and its guard is stated positively rather than as an absence:

- the draft box is empty (`a.input.empty()`),
- `completePath` took nothing,
- and none of rungs 1-16 is claiming the keyboard.

Rungs 1-14 and 16 are already handled by precedence — the toggle is simply
downstream of all of them and never sees the key. **Two collisions are real and are
fixed in K3.**

**Collision one: the welcome box.** `welcomeKey` (`welcome.go:158`) takes only
`up`, `down` and `enter`; anything else is given back, and `input.go:409` then
calls `a.dismissWelcome()` before the key goes on. So `tab` would dismiss the box
**and** switch — one keystroke doing two unrelated things, one of them irreversible
(`dismissWelcome` sets `welcome{spent: true}`, `welcome.go:140`). **Fix:
`welcomeKey` takes `tab` and returns "not taken, and do not dismiss".** A third
case in a function whose whole design is two, and an honest one: switching away is
not "the person starting work" here, which is what dismissal means
(`welcome.go:147-152`). The box is standing when they come back, because the
sidecar kept it.

**Collision two: the rail holding the keyboard.** `railKey` is read at
`app.go:1502`, before `input.key` at all, and while `a.railHold` is set it handles
exactly `esc`, `up`, `down`, `right`, `left`, `w` and `enter`, letting everything
else fall through (`task.go:3204-3232`). So `tab` would switch while the roster
claims the keyboard, and the person would arrive somewhere else with a rail focus
they cannot see. **Fix: `railKey` takes `tab` and does nothing with it, in the same
`switch`** — the key is eaten with the rest of the map, and the rail keeps its rule
that explicit focus outranks ambient place (`app.go:1500-1503`). A person who wants
to switch presses `esc` first, which is already advertised in the rail's own hint
(`railHoldHint`, `task.go:1861`).

With those two, `tab` is correct and no alternative chord is needed. The fallback,
had either fix been unacceptable: `ctrl+^` (`ctrl+6`), which is vi's own
alternate-file key and is unbound on this surface — recorded here so the next
reader does not have to redo the survey.

The rest of the rules, in the shape `space space` already set:

- It does nothing when there is nowhere to go — one conversation open, or none in
  the keeper that has been in front before. Nothing on screen, no note: a key that
  cannot act says so by not being advertised.
- It works while a turn is running in either conversation. Nothing is interrupted;
  the turn behind carries on streaming into its own agent's journal, and the
  in-flight turn is replayed onto the screen when you come back (K1).

The legend slot advertises it on the same terms it advertises home — only while
the door is open and the box is empty (`homeDoorShowing`, `home.go:1419`):

```
space space home · tab last · / commands
```

and the `tab last` clause is absent when there is no last one, which is the
emptiness law again.

`keys.md:622` claims there is no chord for switching because every one is taken.
That paragraph is about home and stays; the page gains a section for this key, and
the sentence "on this surface tab means nothing else at all" (`input.go:414-416`,
quoted into the page) is **deleted**, because it was never true and is now
load-bearing in the other direction.

### `/quit`, and `ctrl+c` twice

The two doors stop meaning the same thing, and that split is the only place this
feature adds a rule a person has to learn.

**`/quit` (aliases `/exit`, `/q` — `commands.go:218`) closes the conversation in
front.** Its agent is closed for real. If the keeper is not empty, the most
recently in front of them is attached and aforge stays up. If it was the last one,
aforge leaves, exactly as it does today. It keeps its "leaves at once, it is typed
out on purpose" property (`keys.md:169`, `commands.md:189`) — closing one
conversation is not something a person types three characters by accident.

What it says, on the way out, in the conversation that comes forward:

```
closed · the parser fix
```

and nothing at all when it was the last one, because the program is gone.

**`ctrl+c` twice quits everything.** The arm is unchanged in shape (`quitarm.go:56`
— `quitArmWindow` 1.5 s; `quitArmWord = "ctrl+c again to quit"`, `:61`), and
unchanged mid-turn: while an answer is streaming `ctrl+c` is still only the
interrupt and does not arm (`input.go:344-348`). What changes is the sentence,
which must now count across the keeper as well as the front:

```
ctrl+c again to quit
ctrl+c again to quit · a task will stop
ctrl+c again to quit · 3 conversations · 2 tasks and a job will stop
```

`quitWorkWord` (`quitarm.go:130`) walks `a.taskOrder` and `a.hudStats().jobs` for
the conversation in front; for each agent in the keeper it asks the agent instead
— `TaskIndex()` for the running rows, the jobs door for the promoted shells — on
the keystroke and never on a frame. The clause order is deliberate: *how many
conversations*, then *what work*. A person who has forgotten they left something
open in another project needs the first number before the second one means
anything. The emptiness law governs both clauses — one conversation drops the
first, nothing running drops the second, and a quiet single conversation reads
exactly `ctrl+c again to quit`, as it does today.

A real `SIGINT` or `SIGTERM` still needs no second press and still runs the full
quit (`tui3.go:472-491`), and it now closes the keeper as well as the front one.

### A question raised in a conversation behind

**Consent holds. The others do not.**

The consent card's clock is already held for exactly this situation one level out.
`tickAsk` refuses to run when the terminal does not have the keyboard
(`consent.go:204-206`: `if !a.asking() || a.askPaused || a.askWait <= 0 ||
!a.focused`), and the comment above it is the argument in full: ten seconds is
"long enough to read a command and a rule", which is a claim about a person
*reading*, and there is nobody reading a terminal that does not have the keyboard.

A conversation behind is that situation with the window in the way replaced by
another conversation in the way — and under this design the clock does not need a
new predicate at all, because **a detached conversation has no card on the surface
to tick.** The countdown stops because the surface stopped drawing it. What the
sidecar carries is how much of it was left, so that attaching gives the person back
the same reading time they had, and not a countdown that ran out in the dark. See
"The sidecar, exactly".

**The other three prompts count down inside the engine, and the surface cannot
pause an engine-owned timer.** Verified, and unchanged from Revision 2:

| prompt | where its clock is | what expiry does |
| --- | --- | --- |
| consent | `tickAsk`, `internal/tui3/consent.go:204` — the frame clock, gated on focus | denies the call (`consent.go:210`) |
| task proposal | `time.NewTimer(countdown)`, `internal/session/task.go:511` | **auto-approves**: `return TaskAnswer{Approved: true}` (`task.go:522`) |
| connect offer | `time.NewTimer(connectAskTimeout)`, `internal/session/connect.go:367`; `connectAskTimeout = 5 * time.Minute` (`:60`) | resolves negative (`connect.go:377`) |
| sub-harness offer | none — `askHarness` waits on `answers` or the turn's context (`internal/session/harness.go:447-476`) | nothing; it waits |
| design approval | none — `ResolveHarness` (`internal/session/harness.go:58`), reached from `resolveHarnessCard` (`internal/tui3/harnesscard.go:323`) | nothing; it waits |

The TUI's own task countdown is presentation only — it draws the `Deadline` the
engine put on the `TaskNotice` (`task.go:497`).

**The decision: limit the promise to consent, and tell the person about the other
two.** Not an engine pause/resume API, and the reasons are unchanged: a pause API
would have to be honest about a clock that starts at `time.Now().Add(countdown)`
before the event is even emitted (`task.go:483`); the two expiries are not
symmetrical (a lapsed connect offer decides nothing, an expired task proposal
**starts work and spends money**); and the person is told either way, because both
put the session into `PresenceWaiting` (`taskpresence.go:501`) — which means
`waiting on you` on home, the accent rung, the `N waiting` clause and the desktop
banner. If a pause API is ever built it is built for `propose_task` alone and it
is its own lane.

So the manual sentence, exactly, on `permissions.md` and `how-tasks-run.md`:
**"A conversation you have switched away from holds its approval question for as
long as you are away. A task it proposed still starts by itself after its
countdown, and a sign-in offer still lapses after five minutes — both say
`waiting on you` on home until they do."**

### A turn that finishes in a conversation behind

**It lands in its own transcript, and nothing is drawn in front.**

Under Revision 2 that took a law and a fold/present split on every handler. Here it
is free: a detached conversation's events do not reach the surface at all, because
the surface unsubscribed when it detached. The turn settles into the agent's own
journal, its meters are the agent's, its stamps are written, and the screen is
rebuilt from all of it when the conversation is attached.

Three things tell the person without a screen change:

- the desktop notification, which fires for a turn finishing on a window nobody is
  looking at (`notifyDone`, `notify.go:63`) — and now fires for a conversation in
  the keeper **even while the terminal is focused**, because a focused terminal is
  no longer evidence that anybody is looking at *this* conversation. The predicate
  becomes `suppress ⟺ a.focused && it is the front conversation`. What raises it is
  a stir from the keeper's watcher, not an event handler; see "The keeper".
- home's `N landed` rung and its `since you last looked` caption
  (`homeLandedWord`/`homeFreshWord`, `home.go:236-237`), which is exactly the delta
  this feature makes common;
- the `✓` glyph in accent on a row whose work landed since home was last closed
  (`homeGlyph`, `home.go:1903`).

No badge, no bell, no unread count. A conversation that finished a turn is not
asking for anything, and a surface that interrupted a person's reading to tell them
so would be spending their attention on the absence of a decision.

### The one thing that is different from a window

**Coming back to a conversation redraws it from its transcript.** It is the cost of
a `/resume` — `replay()` (`replay.go:64`) over the journal plus `measureContext()`
— and on a long conversation it is the one moment a person can tell that this is
not N windows. It is paid on the switch, not on the frame, and it buys the thing
that made this design a third of the size of the last one.

Two consequences, stated rather than discovered:

- **Transient modes are forgotten.** Copy mode, an inline rewind or an open rewind
  sheet, an open expand sheet, the deliverables shelf, a picker, a rail focus, a
  hover: all of them are dropped when the conversation is detached and none of them
  comes back. Each is a mode a person is *in the middle of* — a cut line through a
  transcript, a frozen viewport, a picker with a cursor in it — and there is no
  honest way to be in the middle of one in a conversation you are not looking at.
- **What is kept is what somebody would notice was gone**: the unsent sentence in
  the box and the pictures attached to it, where you were reading, how much of the
  consent countdown was left, and the room you had open. That last one is a
  judgement — it is cheap to reopen from the node id and expensive to explain the
  absence of, since a room is a *place* rather than a mode.

Everything else on the screen is a fact about the agent, and the agent still has
it: the transcript, the rail, the meters, the pending cards, the model, the title.

### `--host`

**Unchanged: home still refuses over a connection, and there is still one
conversation.**

```
home shows this machine's projects, and this session is on another
```

(`homeRemoteWord`, `home.go:209`; the refusal at `home.go:396`.) The reason is why
this feature cannot cross the wire without a design of its own: the world index
under this process is *this* machine's state root, while the agent is on another
machine. A switcher listing the laptop's projects while every conversation it could
open would have to be built on the server's disk is a lie drawn confidently, and "a
capability that cannot work is absent, not broken" is the law that settles it.

Concretely: `tab` does nothing, the status line draws no count (one conversation is
open, so the emptiness law removes it), `/quit` leaves, and `ctrl+c` twice leaves.
The hosted door's compilation side is K2's, and it does **not** fall out of the
general rule — it needs a policy written for it, and it has one below.

---

## The architecture

### The keeper

One new field on `app`:

```go
// behind is every conversation this process holds that is not the one on
// screen: the agent, still running, and the small handful of surface facts
// that would be lost when the surface stops drawing it.
//
// THE KEY IS THE CANONICAL TRANSCRIPT PATH ([convKey]), because that is what
// home names a row by and what the flock is taken on.
behind map[string]*aside
```

plus a `prev []string` — canonical keys, most recently in front last, which `tab`
reads and every close filters — and `convCap = 8`.

That is the whole of the new state. There is no conversation struct, no id
counter, no per-conversation message routing and no partition of `app`'s 191
fields: the surface still has exactly one of everything, because it is still
drawing exactly one conversation.

**The keeper also runs one small goroutine per conversation in it — the stir
watcher.** Its only job is to make sure nothing behind is silently starving or
silently stuck:

1. it **drains** that agent's event stream and throws every event away, which is
   what keeps a producer queue from growing without bound and a `pump` from parking
   on its last send (`agent.go:1579-1651`, and the comment at `:1637-1640`);
2. it keeps two counters — turns finished since detach, and whether the agent now
   answers `NeedsPerson()`;
3. on an edge in either, and **at most one outstanding at a time**, it sends a
   `behindStirMsg{key}` into the program, which carries no content: the surface
   wakes, re-reads the agent it already has a pointer to, and fires the desktop
   banner or refreshes the count.

The stir message is the only thing that crosses from a conversation behind to the
front, and it is deliberately contentless. That is what keeps THE GENERATION LAW
below true, and it is why no `tea.Msg` in this design needs a conversation id.

### Detach and attach are `openSession`, split

`openSession` (`welcome.go:248-309`) already performs every step of a switch. Read
it once and the design is obvious:

| `openSession` line | what it does | detach | attach |
| --- | --- | --- | --- |
| `:254` | build the new agent through the seam | — | K2's `Options.Open`/`Start`, or the keeper's pointer |
| `:262-264` | interrupt a running turn | **no — this is the line that must not run** | — |
| `:265-269` | `a.agent.Close()` | **no — this is the other line that must not run** | — |
| `:270-292` | clear the per-conversation surface fields: `entries`, `live`, `sel`, `think`, `asks`, `follows`, `dropParked`, connect and harness cards, `turn`, `unfolded`, `dropHover`, `stream`, `gen++` | **yes, all of it** | — |
| `:293-296` | state idle, `resetMeters`, model, title | yes | reset again from the arriving agent |
| `:298-299` | `endRecall`, `offset`/`stick` to the top | yes | restored from the sidecar |
| `:301-302` | `replay()`, `measureContext()` | — | **yes** |
| `:303` | `note("resumed …")` | — | no — arriving is the thing happening |
| `:308` | re-subscribe the standing lanes | **inverted: unsubscribe** (K1) | yes |

So:

```
detach(key):
    fold the parked messages into the draft, and put the draft in the sidecar
    take the sidecar's other readings (offset, consent remaining, open room)
    unsubscribe every lane this surface holds on this agent          (K1)
    clear the per-conversation surface fields, exactly as :270-299 does
    dropTasks(), close any room, drop the pilots                     (see below)
    behind[key] = &aside{agent: a.agent, ...}
    start the stir watcher

attach(key):
    stop the stir watcher, take the agent out of the keeper
    a.agent, a.file, a.workspace, a.place, a.owned = ...
    a.gen++ and every lane generation with it
    replay(); measureContext()
    rebuild the cards from the engine's snapshot doors
    re-subscribe the standing lanes; Attach() the in-flight turn      (K1)
    restore the sidecar
```

and `openSession` itself becomes `detach(current)` followed by `attach(new)`, with
the close moved to `closeFront()` where `/quit` and the fresh-and-empty `/new`
call it. **One implementation of "make this conversation the front one", not
two** — which is the property Revision 2 also insisted on and is much easier to
keep when the function already exists.

**`dropTasks` on detach is a correction to today's `/resume` and is required
here.** `openSession` never calls it (`welcome.go:248-309` — there is no
`dropTasks` in it), so a `/resume` today carries the previous conversation's whole
rail, its rooms and its pilots into the resumed session. That is a latent bug
worth its own test; under attach/detach it is not latent, because switching is
common. The arriving conversation's rail is rebuilt from `TaskIndex()`
(`task_index.go:437`), which is the same door the `@` completion already reads
(`taskmention.go:122`).

### The sidecar, exactly

```go
// aside is a detached conversation: the agent, which is still running, and the
// few surface readings that are the person's rather than the agent's.
//
// EVERYTHING NOT IN THIS STRUCT IS FORGOTTEN BY A SWITCH AND REBUILT FROM THE
// AGENT ON THE WAY BACK. Adding a field here is adding a thing the surface must
// keep correct while it is not drawing it, which is the expensive kind of
// state; the test is "would a person notice it was gone", not "could we".
type aside struct {
    agent     Agent         // still streaming, still running its tasks and jobs
    file      string        // the transcript; behind's key is convKey(file)
    workspace string        // for home's row, the status line and the seam
    place     string        // the base name the surface draws
    owned     bool          // whether this workspace is the session's own work/
    draft     string        // the unsent sentence, with parked messages folded in
    chips     []chip        // pictures attached to that sentence
    offset    int           // where they were reading
    stick     bool          // and whether they were pinned to the foot
    askLeft   time.Duration // what was left of the consent countdown, 0 for none
    askPaused bool          // a question the person had already touched stays paused
    room      string        // the node whose page was open, "" for none
    since     time.Time     // when it was detached — home's "since you last looked"
}
```

Three of those need their reason written down.

**`draft`, with the parks folded in.** `a.parks` (`app.go:887`) holds messages
typed at a conversation while it was busy, to be sent when the turn ends — and the
park queue lives in the surface, so a detached conversation has nobody to send
them. `openSession` and `renew` both call `dropParked` (`welcome.go:282`,
`app.go:4113`), which drops them with a note, and the comment above the call says
why: "the person typed those words, so this says that it went rather than dropping
it in silence." A switch is not a close, so the better answer is available: put the
words back where the person can see them. The parked text is appended to the
draft, in order, and the draft goes in the sidecar. Nothing is lost and nothing is
sent behind their back.

**`askLeft`, not `askAt`.** `askAt` is when the question was raised
(`app.go:766-776`); storing it would mean the countdown ran while the person was
in another project, which is exactly the thing the focus gate already refuses to
do (`consent.go:204-206`). The sidecar stores what was *left*, and attach rebases:
`a.askAt = now.Add(askLeft - a.askWait)`, so the card comes back with the same
reading time it had. `askPaused` rides along unchanged, keeping its one existing
exception intact: a question a person already touched stays paused
(`refocusAsk`, `consent.go:225`).

**`room`.** The node id only. The page is rebuilt on attach the way any room is
opened, from the agent's own journal (`TaskJournal`, `task_room.go:217`) and its
watch (`WatchTask`, `:194`). A room whose node has finished or gone while the
conversation was away opens as that node's finished page, which is what opening it
from the rail would do anyway.

**The draft file is not in the sidecar and does not go away.** `a.draftFile`
(`draft.go:70-75`) is the crash insurance and it is per workspace and pid; it keeps
being written for the conversation in front, and the sidecar is what carries a
detached conversation's box in memory. Two conversations on the same workspace in
one process still collide on that filename, which is a real trap and is the
appendix's, not this section's.

### What attach restores, and what it forgets

| restored | from |
| --- | --- |
| the transcript, including the compacted region's seam (`earlier*`, `app.go:513-521`) | `replay()` (`replay.go:64`) |
| the context measure and the window | `measureContext()` (`welcome.go:302`) |
| the model and the title | the agent (`agent.Model()`, `agent.Title()`) |
| the meters, the stamps, the sparkline | `resetMeters()` then the replay's own stamps |
| the rail: the tasks, their states, their families | `TaskIndex()` (`task_index.go:437`) |
| a consent card the engine is still holding | `PendingConsent()` (`consent.go:420`) + the sidecar's `askLeft` |
| a task proposal still waiting | `PendingTasks()` (`task.go:543`), with its own `Deadline` |
| a sign-in offer still waiting | `PendingConnect()` (`connect.go:398`) |
| a paused run | `OrchestrateSnapshot()` (`orchestrate.go:285`) |
| the in-flight turn, from its first token | **K1's `Attach()`** |
| the box, the chips, the scroll position, the open room, the welcome box | the sidecar |

| forgotten | why |
| --- | --- |
| copy mode (`a.copy`), the inline rewind and the rewind sheet (`a.rew*`) | a frozen viewport and a cut line are modes a person is in the middle of; `renew` already drops `copy` for this reason (`app.go:4141`) |
| the expand sheet, the deliverables shelf, every picker and panel, the settings sheet, the status deck | doors onto process-wide things — the catalog, the profile, the shared memory store, the machine's deliverables index. A person who left `/settings` open in one project and found it open in another would reasonably think it was that project's settings |
| the rail's focus (`railHold`), its folds, the hover, the `←` double-tap timer | presentation, rebuilt by the first frame; the rail's width tier is a per-conversation stickiness nobody has ever noticed surviving a `/new` |
| the task page and `/history`'s sheet | it is the *project's* record and it is re-read (`app.go:4162-4167` already does this on `/new`) |
| `pathSeen`, the memoised linkable paths | it memoises one transcript's paths and is cleared at every turn end anyway (`settle`, `app.go:2609`) |

**A switch closes the overlays before it moves.** One function, `closeForSwitch`,
called by `detach` — the model picker, the crew picker, the memory panel, the
connections panel, the harness panel, the permissions panel, the settings sheet,
the status deck, the deliverables shelf, the resume roster. It does **not** close
home, which is often the thing that caused the switch and closes itself.

### THE GENERATION LAW — no routing by conversation id

*The surface has one generation counter per lane, they are bumped by every attach,
and a message that names an old generation discards itself. **Nothing carries a
conversation id, because nothing that could be misdelivered survives long enough
to arrive.***

The mechanism is already in the tree and is already load-bearing: fifteen
comparisons across eight counters (`a.gen` at `app.go:686` compared at `:1946` and
`:1952`; `taskGen` at `:1968`, `:2061`; `designGen` at `:1974`, `:1982`; `orchGen`
at `:1989`, `:1997`; `room.gen` at `:2002`, `:2017` and `roomorch.go:422`;
per-pilot `gen` at `task.go:612` and `app.go:2041`; `wakeGen` at
`followup.go:255`). `/new` and `/resume` bump them today for exactly this reason,
and the comment at `app.go:345-350` states the law they enforce.

Two things make that sufficient here, where Revision 2 needed 28 message ids:

1. **Detach unsubscribes.** After a detach, no lane of that agent is held by the
   surface, so no new message from it is ever created. The only messages that can
   still arrive are ones already in flight at the moment of the switch — and they
   carry the generation from before the bump.
2. **The stir message carries no content.** It names a keeper key and nothing
   else; the surface looks the agent up and reads it directly. A stir for a
   conversation that has since been closed finds nothing in the map and returns
   `nil`, which is the same shape as a stale generation and needs no new rule.

The one message type that needs a small fix is the one Revision 2 also found:
`homeTickMsg` (`home.go:93`) has **no** generation, and `homeBeat`
(`home.go:102-114`) re-arms whenever home is open, so closing and reopening home
before an old tick lands starts a second self-rearming chain. Home is now the
switcher and gets opened constantly, so this stops being theoretical. `homeGen`,
bumped by `closeHome` (`home.go:531`), carried on the tick, checked in `homeBeat`.
One field, and it belongs to the surface rather than to any conversation, because
there is one home.

### Identity before flock

**IDENTITY IS ASKED BEFORE THE LOCK IS.** This is a correctness rule, not a nicety.
`InUse` (`sessionfile.go:1323-1337`) answers by taking an `unix.Flock` on a fresh
descriptor — and a flock rides the **open file description**, so a second
descriptor in the *same* process conflicts with the first. A transcript this
process holds — in front or in the keeper — therefore answers `InUse == true` about
itself, and `enter` or `/resume` on it would say
`open in another window — go there, or start a new conversation here`
(`welcome.go:231`) about a conversation one keystroke away.

So:

- **Canonical transcript identity** is `filepath.Clean` of the
  `filepath.EvalSymlinks` result of the transcript path, falling back to the
  cleaned path when the link cannot be resolved (a transcript just minted, a
  filesystem that will not answer). It is computed in exactly one place,
  `convKey(path string) string`, and nothing else compares transcript paths with
  `==`.
- **Every door consults the keeper and the front conversation first**: `homeEnter`
  (`home.go:1213`), `resumeKey`'s enter (`resume.go:426-440`), the welcome box's
  rows (`welcome.go:213`), and `/resume` by argument. A hit **attaches**. Only a
  miss reaches `homeHeldNow`/`session.InUse` and then the seam.
- `homeEnter`'s existing first case — `line.row.Transcript == a.file`
  (`home.go:1229`) — becomes `convKey` equality, of which it is the
  one-conversation special case.

### Presence is the channel between windows; our own agents are asked directly

"Behind" is not a new kind of thing on this machine. It is **another window, in
this process**, and every mechanism that already makes another window visible
works on it unchanged:

- **The presence file.** Every live agent writes one, per session folder, carrying
  its pid, on a five-second heartbeat believed for fifteen
  (`taskpresence.go:103-113`). An agent in the keeper writes it exactly as the one
  in front does. So every *other* terminal on the machine sees our behind
  conversations as live windows, correctly, for free — and so does our own home,
  which reads the world the same way (`internal/session/world.go`).
- **The flock.** Each open conversation holds its own lock on its own transcript
  (`sessionfile.go:600-612`), which is what stops a second terminal opening it.
  Two conversations in one process hold two locks on two files, which is exactly
  what two terminals do today.
- **`Elsewhere`.** `ReadElsewhere` (`taskelsewhere.go:83`) is every live window in
  one project bucket **except the caller's own session id**. A second conversation
  of ours on the same project would therefore appear in the front conversation's
  `away` rows as `another window` (`taskview.go:362-383`, and the manual's
  `tasks.md:713-753`). That is a real overlap and it is answered under Risks.

**But for our own agents, presence is the wrong instrument.** It is a file, written
every five seconds, and reading our own writes off a disk to learn something we
hold a pointer to would make home and the status line lag the truth by up to five
seconds at the exact moment a person is switching. So:

- the status line's `N waiting` calls `NeedsPerson()` (K1) on each keeper agent;
- home's glyph and rung for a row **we hold** are computed from that same call and
  from `TaskIndex()`, and the presence file is used only for rows we do not hold;
- the notification for a conversation behind is raised by the stir watcher's edge,
  not by a file.

One predicate, three readers, and they cannot disagree — which is the point of
K1's `NeedsPerson()` being the presence file's own expression rather than a second
one.

### The engine primitive, and what else it buys

K1's `Attach()` — late-subscriber replay of the in-flight turn — is the one thing
this design needs that the engine does not have. `eventHub` has no replay
(`agent.go:1484-1494`): a subscriber that arrives mid-turn starts empty and would
draw a half-turn with no beginning. Today that never happens, because nothing ever
unsubscribes.

It is worth naming what else it is:

- **Two terminals on one conversation**, later, is the same primitive over a
  socket. The keeper is the local form of a broker: a map from transcript to a
  running agent, with attach/detach and replay. A second terminal means exposing
  the keeper on a unix socket and speaking the same three verbs.
- **`--host` reconnect** is the same primitive again: a client that dropped mid-turn
  and comes back needs precisely "give me this turn from its first token, and tell
  me whether it is still running".

That is the argument for building it in the engine and building it properly, rather
than having the surface buffer events into the sidecar. A sidecar buffer would work
for this feature and would be worthless for the other two.

### What a behind agent costs

The owner's question, answered plainly: **this is one process, and an agent behind
is a few goroutines, its transcript, and a timer.**

| per behind conversation | cost |
| --- | --- |
| the transcript in memory | the `messages` slice — low hundreds of KB for an ordinary conversation, a few MB for one at a 200k-token window |
| goroutines | the presence heartbeat, the job supervisor, one `pump` per live subscriber, one per running task node, plus the keeper's stir watcher — order ten, at 8 KB of stack each |
| file descriptors | one journal, held with its flock |
| wakeups | one presence tick every five seconds; the stir watcher is blocked on a channel and costs nothing when nothing happens |
| the surface | **nothing.** It draws one conversation, lays out one screen list, and holds one of every overlay, exactly as it does today |

Eight conversations behind is therefore **megabytes, not gigabytes**, and under two
wakeups a second with nothing running. What is expensive is what the *work* costs —
a running task node is a model making calls, and that is the same cost it has in a
second terminal.

The frame is the thing this design protects hardest. A behind conversation cannot
make the surface slower, because the surface never walks it: no second entry list
to lay out, no second rail to fold, no aggregate to recompute per frame. The only
per-frame reads of the keeper are its **length**, and `NeedsPerson()` on each of at
most seven agents, which is a mutex and five slice lengths.

### Quit, close, `/new`, and the cap

**`closeFront()`** is `Agent.Close()` (`agent.go:972-1041`) plus removing the key
from `prev` — every occurrence, written as a filter over the slice — plus
`attach(prev.top)`. `Agent.Close` is bounded on every axis (`closeGrace` 2 s for
the turn, `agent.go:31`; `jobShutdownGrace` 2 s, `:37`; `presenceCloseGrace` 1 s,
`taskpresence.go:112`) and its phases are **sequential**, so "two seconds" is not a
bound for one agent and never was.

**`CloseAll()`** closes the front conversation and every agent in the keeper **in
parallel**, waits for all of them, and is idempotent. Parallel because the graces
overlap rather than sum, and because every one of those clocks exists so a quit
never waits on somebody else's courtesy; the test asserts ordering and concurrency,
**never wall time**. It is K2's, deferred around `tui3.Run` so that a `tea.Program`
returning by any road other than `app.quit` cannot leak flocks, presence goroutines
and jobs — today's outer defer owns exactly one agent (`chatv3.go:163`).

**`/new` adds a conversation, in the same workspace, unless the current one is
fresh and empty.** Fresh and empty is: no entries (`len(a.entries) == 0`), no turn
run (`a.turn == 0`), no tasks, no jobs, nothing waiting. That is the state a
conversation is in when somebody typed `/new` because they had not started yet, and
closing it costs nothing and keeps the cap honest. Otherwise the current one is
detached and keeps running. Three reasons this is the right default and replacement
is not:

1. every neighbouring door adds — home's `enter`, home's typed path, the welcome
   box's rows — and a `/new` that closed a conversation with three tasks running in
   it would be the one place the surface still punished somebody for using it;
2. it makes home's action row (`/new` then a submit, `home.go:1289-1295`) the same
   act whether or not a path was typed, and the branch only about *which*
   workspace;
3. nothing is lost, because the empty case is handled above.

The draft goes **with the person**, not with the conversation: `/new` carries the
box's text into the new conversation and clears it in the old one, which is what
`renew` already promises in those words (`app.go:4149-4151`). The note is
`new session · <path>` when it replaced a fresh empty one and
`new conversation · <place>` when it added one, because the person needs to know
which of the two happened and a count segment appearing is not enough on its own.

**The welcome box** is drawn only on a conversation that is empty by construction,
so `enter` on one of its recent rows hits the fresh-and-empty test: the empty
conversation is closed and the resumed one takes its place. No orphan, no cap spent
on a conversation nobody typed in.

*Superseded 2026-08-31: **there is no cap**. The owner hit eight in a day of
ordinary use and ruled the limit pointless, so `convCap`, `convCapWord`,
`roomForAnother` and `roomToRenew` are all gone and no door counts what is open.
What survives is the ordering — canonicalisation and the keeper lookup before any
door is opened — and the cost table above, which is now a description of what a
window spends rather than the reasoning behind a number. The rest of this section
is kept as the record of what was built.*

**The cap is eight**, checked **after** canonicalisation and **after** the keeper
lookup, in that order: attaching something already open is never capped, and a path
that canonicalises to a transcript already open is an attach, not a second
conversation. At the cap, `enter` refuses in home's own voice and home stays up:
`8 open is as many as aforge holds — /quit closes this one`. Eight because of what
the cost table above says, because it is roughly the number of projects a person
genuinely has in flight, and because a cap that can only be hit on purpose never
has to be explained.

**Conversations behind are never auto-closed and there is no idle timeout on
them.** `Agent.Close` kills background jobs and running task nodes after a
two-second grace (`agent.go:1021-1023`), so an automatic close is an automatic kill
of work a person delegated and paid for, triggered by them not looking at it —
which is the exact thing they were told they could do. There is also nothing to
hang it on: "there is no idle ticker on this surface and there is not going to be
one" (`render.go:945-947`). What reaps is what already reaps — the sweep skips
folders whose transcript is locked (`internal/session/sweep.go`), and an open
conversation is locked.

**The hosted door caps the count at one**, because there is one remote agent by
construction (`cmd/aforge/chatv3_host.go:406-423`, and the comment at `:409-415`
says so). `hosted()` refuses a second conversation with the sentence that already
exists rather than a new one.

---

## The lanes

**Three, ordered, each independently mergeable, each leaving `bin/aforge` working
and the suite green.** K1 and K2 are running now in worktrees off `chat-v3-task`
and are independent of each other. K3 needs both.

Every lane keeps all three manual gates green: `internal/tui3/manual_test.go`
(every slash command and alias appears in the corpus),
`internal/session/manual_test.go` (every belt tool appears by its registered name),
`internal/manual/chat_test.go` (49 real questions still reach the page that answers
them). K1 and K2 change no person-facing string and so touch no page; K3 changes
the manual in the same commit as the code, which is the law.

### K1 — the engine primitive (`internal/session`), ~250 lines, in flight

Worktree `~/af-k1-attach`, branch `feat/agent-attach`.

1. **Late-subscriber replay of the in-flight turn.**
   `Attach() (<-chan Event, bool)` hands back a stream that replays the current
   turn from its first event and then continues live, plus whether a turn is
   running at all. `eventHub` has no replay today (`agent.go:1484-1494`), so this
   is a retained per-turn buffer, cleared at turn end, behind the same mutex the
   hub already uses. With it, a surface that attaches mid-turn draws the turn
   whole; without it, it draws a half-turn with no beginning.
2. **Unsubscribe, for the turn stream and every standing lane.** Eight of them, and
   they are the reason this is engine work rather than a `for range` the surface
   can abandon: the **turn stream** (`Submit`), the **task** lane (`TaskUpdates`,
   `task_run.go:1761`), the **wake** lane (`Wakes`, `agent.go:1451`), the **design**
   lane, the **orchestration** lane (`Orchestrations`, `orchestrate.go:343`), the
   **pilot** lanes (one per running node), the **room** lane (`WatchTask`,
   `task_room.go:194`), and the orchestration polling clock (`orchTick`,
   `roomorch.go:414`), which re-arms itself and so is a lane in every respect that
   matters. A subscriber that walks away without unsubscribing parks a `pump` on
   its last send (`agent.go:1632-1651`, and its own comment at `:1637-1640`) and
   grows an unbounded producer queue (`:1579-1618`).
3. **One exported `NeedsPerson()`.** Extract the inline expression at
   `taskpresence.go:501` into `needsPersonLocked`, export a read-only method over
   it, and **add the term presence omits**: an orchestration in the paused state
   (`orchestrate.go:181-186`, resolved at `:261`). Today a run that has spent its
   tank reads as `PresenceWorking` to every window on the machine while it waits
   for a person — a pre-existing gap that is invisible while you are looking at the
   window and a hole the moment the conversation is behind.

- **Files:** `internal/session/agent.go`, `eventhub`/`eventStream` in the same
  file, `internal/session/taskpresence.go`, `internal/session/orchestrate.go`,
  `internal/session/task_run.go`.
- **Tests:** a subscriber attaching mid-turn receives every event of that turn in
  order and then the live ones; a subscriber attaching between turns receives
  nothing and reports not-running; unsubscribing releases the pump (assert the
  goroutine returns, not a sleep); an agent with a paused run says
  `PresenceWaiting`; `NeedsPerson` and the presence file agree across all five
  lanes, table-driven; a merely-running agent says false.
- **Manual:** none. `waiting on you` already means this, and the fuel gate starting
  to say it is the page becoming true rather than changing.
- **Ships value alone:** the presence fix is worth landing whether or not K3 is
  ever built.

### K2 — the seam (`cmd/aforge`, `tui3.Options`), ~400 lines, in flight

Worktree `~/af-k2-open-seam`, branch `feat/open-seam`.

1. **`v3Process`, built once.** `openV3Launch` (`chatv3.go:406`) loads the model
   catalog (`catalog.LoadLazy`, `:459`), opens the sub-harness registry
   (`subharness.Default()`, `:473`), opens the machine's chat database
   (`v3Memory`, `:497`/`:1034`), resolves the deliverables index
   (`artifactsIndexPath()`, `:503`), builds the connections manager and reads the
   profile — **every time it is called**. Calling it N times would leak stores,
   repeat the catalog warm and re-run boot passes. So it splits: `openV3Process()`
   for the once-only half (including `migrateV3Layout()`, `:436`) and
   `openV3Launch(proc, opts)` for everything that is a function of the workspace.
   The memory store is the one that must be one: `store.Open` builds a SQLite
   handle with an eight-connection pool (`internal/store/store.go:608-618`) and
   every write goes through `beginWriteWithin`, whose reason for existing is that a
   transaction losing the race waits the full `busyWait` and cannot be cancelled
   (`internal/store/writelock.go:1-50`).
2. **`Options.Open(workspace, transcript)` and `Options.Start(workspace)`**,
   replacing `Options.Fresh` (`tui3.go:186`) and `Options.Resume` (`tui3.go:382`),
   each returning a `Conversation` bundle: the agent, the session file, the
   workspace triple, the resumed flag and notice, the context window, the draft
   file (empty when the project says keep nothing), the history enablement, the
   project layer's approval answer, `RecentSessions`, and **the three approval
   closures minted with the agent they belong to**.
3. **Which fixes a live bug.** `SaveApproval`, `SaveBashApproval` and
   `ApplyApprovals` are built once at boot around the **boot agent's pointer**
   (`chatv3.go:340-342`; `chatv3_approval.go:68-108`). After a `/new` or a
   `/resume`, `a.agent` is a different agent and those closures still push the
   rebuilt policy into the one that was closed. The write lands on disk, so the
   symptom is narrow and nasty: answering "always" in a conversation opened after a
   `/new` says `saved`, is saved, and does not take effect until the next launch,
   with nothing on screen saying so.
4. **The `LaunchDir` rule.** `Meta.LaunchDir` is "where the person actually stood
   when the session opened" (`internal/session/place.go:148-151`) and is pinned
   distinct from `Workspace` by `chatv3_layout_test.go:156`. It is captured
   **once** at boot (`v3LaunchDir()` is `os.Getwd()`, `chatv3_layout.go:350`) — the
   process must not chdir — **and left empty when the captured directory is under a
   temp directory and the conversation's workspace is not**, because `sweepIsLitter`
   marks a session disposable if *either* value is under a temp dir
   (`sweep.go:206-208`), and stamping a `/tmp` launch onto a conversation opened in
   `~/work/repo` would have the sweep reap it. `underTempDir("")` is false
   (`sweep.go:225`) and the field is `omitempty`.
5. **`CloseAll()`**, idempotent, deferred around `tui3.Run`.
6. **The hosted door sets only the wrappers**: single-slot `Open`/`Start` over the
   one remote agent (`chatv3_host.go:406-423`). `engine.go:65-95` keeps its chdir,
   its one workspace and its one conversation. `--once` (`chatv3.go:125-135`)
   borrows `v3Process` and instantiates no keeper.

- **Files:** `cmd/aforge/chatv3.go` (`:100-200`, `:406-520`),
  `cmd/aforge/chatv3_approval.go` (`:68-141`), `cmd/aforge/chatv3_host.go`
  (`:383-423`), `cmd/aforge/chatv3_layout.go`, `cmd/aforge/engine.go`,
  `internal/tui3/tui3.go` (`Options`), `internal/tui3/app.go` (`newApp`),
  `internal/tui3/welcome.go`.
- **Tests:** `cmd/aforge` — an "always" answered after a `/new` reaches the gate the
  new conversation is running behind (**this fails on `master` today and is the
  lane's proof**); `openV3Process` is called once and its store pointer is the one
  every launch carries; a launch whose later step fails closes the partial agent
  and leaves no flock (`session.InUse` false afterwards); a conversation opened
  from a temp-dir launch into a real repository is not litter; `CloseAll` after an
  abnormal `tea.Program` return closes every agent and is idempotent.
  `internal/tui3` — `Start` and `Open` are each called with the workspace they were
  given.
- **Manual:** `permissions.md` — the paragraph about when a banked rule takes effect
  describes the *correct* behaviour, so check it says "the very next call" and not
  "the next session", and correct it if the bug had been written down as the truth.
- **Still exactly one conversation at the end of this lane**, which is what makes it
  reviewable.

### K3 — the keeper (`internal/tui3`), ~500 lines, not started

Needs K1 and K2 in the tree.

- Split `openSession` (`welcome.go:248-309`) into `detach()` and `attach()`, with
  the close moved to `closeFront()`; add `closeForSwitch` and the `dropTasks` on
  detach.
- The `behind` map, the `aside` sidecar, `prev`, `convKey`, `convCap = 8`, and the
  stir watcher.
- Home: `enter` attaches a keeper row and opens anything else through the seam;
  **identity before flock**; the three-state mark; the `open` rung; our own rows
  read from the agent; the `elsewhere` refusal, `homeOpens` and `a.home.bucket`
  deleted; the folder-gone refusal; the cap refusal; the typed-path row.
- `segOpen` — `2 open · 1 waiting`, absent at one, `· N waiting` absent at zero.
- `tab` at rung 17, with the `welcomeKey` and `railKey` fixes.
- `/quit` closes the front one and quits when it was the last; `ctrl+c` twice runs
  `CloseAll` and the armed line counts conversations, tasks and jobs across the
  keeper; `/new`'s add-or-replace-if-fresh semantics and its two notes.
- `homeGen`.
- Notifications: `suppress ⟺ focused && front`.

- **Files:** `internal/tui3/welcome.go`, `app.go`, `home.go`, `input.go`,
  `task.go`, `render.go`, `notify.go`, `resume.go`, `quitarm.go`, `commands.go`,
  `consent.go`, `draft.go`.
- **Tests:** `switch_test.go` — attach replays the transcript and the in-flight
  turn; a detached conversation's turn finishes into its own journal and nothing is
  drawn in front; the sidecar's every field survives a round trip and nothing else
  does; the consent countdown has the same time left on the way back; parked
  messages come back in the box. `home_test.go` —
  `TestHomeOpensThisProjectAndNamesWhereTheOthersLive` is **rewritten**, not
  deleted: another project's row now opens, and the test that replaces it asserts
  the conversation left behind is still open and still running; the lock tests stay
  green untouched, which is the check that `another window` still refuses another
  *process*; a row we hold never says `another window`; the cap refuses in home's
  own voice; a gone folder refuses and home stays up. `input_test.go` — `tab` in
  each of the sixteen precedence states does what the table says. Plus the armed
  line with two conversations and work in both; `/quit` with two open leaves aforge
  running; `/new` on a fresh empty conversation replaces it and on a used one adds;
  the count segment absent at one and present at two.
- **Manual, in the same change:** `home.md:233-248` — the whole
  `## What home will not do yet — opening another project's work` section **dies**,
  replaced by what `enter` does on each kind of row, plus the `open` rung and the
  front/ours mark. `commands.md:636-641` — the same limit stated again there, and
  the `/quit` and `/new` rows. `screen.md:751` — the `elsewhere · …` line is named
  as a clickable path and no longer exists. `keys.md` — a section for `tab` written
  from the precedence table, the deletion of "on this surface tab means nothing
  else at all" from the `:622` survey, and the quitting section (`:124-169`).
  `sessions-and-rewind.md` — what switching keeps and what it forgets, beside what
  `Agent.Close` does (`:494-510`). `starting-aforge.md:60-70` — "which folder does
  aforge work in" gains the second case. `tasks.md:695-760` — the `away` rows and
  what they say about a conversation of ours on the same project (see Risks).
  `permissions.md` and `how-tasks-run.md` — the countdown sentence quoted verbatim.
- **Retrieval headings to add**, in the asker's own words:
  `## Open another project from home`, `## Why does it say elsewhere`,
  `## Switch between projects without leaving`, plus questions in
  `internal/manual/chat_test.go`: "can I work on two projects at once", "how do I
  switch to my other chat", "how do I get back to the last conversation", "is my
  other conversation still running", "does my approval question expire while I am
  in another chat", "how do I close just this chat", "will ctrl+c kill my other
  project's tasks".

### Merge order

K1 and K2 are independent and may land in either order; K3 lands after both.
Rebuild `bin/aforge` after each merge — the user runs that binary — and tell the
other lanes to rebase, because K2 touches `chatv3.go` and K3 touches `app.go`,
which every feature wave is in. Never `git add -A`; stage explicit paths.

---

## Risks and open questions

**Replaying a long in-flight turn.** K1's `Attach()` buffers the current turn and
hands it to a late subscriber. A turn that has been running for ten minutes with a
wide tool batch is the worst case: the buffer is the turn's events, and the surface
folds all of them in one `Update` before the first frame after an attach. Two
mitigations, and the lane should take the first and measure before taking the
second: the buffer holds **events, not frames**, and the surface's fold path is the
same one it runs live at full speed; and the replay can be handed over in chunks
with the frame drawn between them, exactly as `replay()` already sizes its work
(`replay.go:64`). The number to watch is a turn with several hundred tool events,
which is an orchestration, not a chat.

**Standing lanes of a conversation behind: unsubscribed, and drained by one
watcher. DECIDED.** The two candidate rules were (a) keep every lane subscribed
and drop the messages on arrival, and (b) unsubscribe on detach and re-subscribe on
attach. **(b), with the stir watcher.** The justification is three-part. First, (a)
would put every behind conversation's traffic through `Update`, which means the
surface wakes for events it will discard — on eight conversations that is the frame
paying for work nobody is watching, and it is exactly the cost this design exists
to avoid. Second, (a) needs the discard to be *correct*, which is the 28 message
ids and the fold/present split Revision 2 needed and this one does not. Third, (b)
is the shape the two-terminals feature needs anyway: a detached surface is a
disconnected client. The cost of (b) is that a detached agent has no reader, which
is precisely what parks a `pump` (`agent.go:1637-1640`) — so the keeper's watcher
drains and discards, and turns the interesting edges into one contentless stir
message. **The comment where the watcher takes its subscription must say all of
this**, because a future lane that "saves memory" by deleting the watcher inherits
a parked goroutine and an unbounded queue.

**The stir watcher and the sleeping surface.** The surface has no idle ticker and
is not getting one (`render.go:945-947`), so between stirs it sleeps. That is fine
while stirs are edges — a turn finishing behind and a question raised behind both
produce one. The residual risk is a state change the watcher's two counters do not
notice: if `NeedsPerson()` and "turns finished" are not enough, the fix is a third
counter and not a ticker. **Open:** whether the watcher should also stir when a
task node lands, so home's `N running` is right the instant a person opens it.
Recommendation: no — home's own tick covers home, and a stir per landed node on
eight conversations is a wake per node for a number nobody is reading.

**The consent countdown through the sidecar.** `askLeft` is a duration measured at
detach and rebased at attach, which is right, and it is also a value that can be
stale in one direction: the engine may have resolved the question while the
conversation was away (the turn was cancelled, the agent closed, something else
answered). Attach must therefore rebuild the card from `PendingConsent()`
(`consent.go:420`) and apply `askLeft` **only to a question the engine still
holds** — the same discipline `syncTaskAsk` already uses for proposals
(`task.go:1187-1215`), which asks the engine rather than assuming. A card restored
for a question nobody is asking any more is the worst possible outcome, because a
person would answer it.

**Rooms and task pages of a conversation behind.** The sidecar keeps one node id
and nothing else. A room is a live watch (`WatchTask`, `task_room.go:194`) and it
is unsubscribed on detach with everything else, so the page is rebuilt on attach
from the journal (`TaskJournal`, `:217`) plus a fresh watch. The gap: a node that
finished while away opens as a finished page with no "this happened while you were
gone" mark, which is the same thing that happens when you close a room and reopen
it. **Open:** whether the task page (`ctrl+.`) should say anything at all about
conversations other than the front one. Recommendation: no. The rail deliberately
holds this conversation's work and nothing else (`keys.md:684-690` states that law),
and the status count is the whole of the cross-conversation answer.

**`a.away` overlaps the keeper, and will lie unless it is told.** `Elsewhere`
(`taskelsewhere.go:83`, `:134`) excludes exactly one session — the caller's own —
so a second conversation of ours on the same project shows up in the front
conversation's `away` rows and in `tasks.md:741-746` as `another window`. It is not
another window; it is two keystrokes away, and telling somebody to "go to that
window to act on it" (`tasks.md:752`) when the answer is `tab` is the same
wrong-refusal shape as `elsewhere`. **Recommendation: `Elsewhere` gains a
multi-exclude** — the front session id plus every keeper session id — and K3 passes
it, so our own conversations never appear as somebody else's; if the rows should
distinguish "ours, not in front" later, that is a word on a row and a separate
change. Left as an open question only because it touches `internal/session` and may
therefore belong to K1's tail rather than K3's head; it is small either way.
Until it is done, the manual page must not gain a claim that contradicts it.

**The toggle key.** `tab` at rung 17 needs two behaviour changes to keys that do
something today (`welcomeKey` must stop dismissing on it; `railKey` must eat it),
and both are argued above. The risk is not the collisions, which are enumerated —
it is the seventeenth rung itself: a key whose meaning depends on sixteen prior
conditions is a key a person can be surprised by. The mitigation is that it is
advertised only where it works (`homeDoorShowing`, `home.go:1419`) and does nothing
where it does not. **Fallback if either fix is rejected in review:** `ctrl+^`
(`ctrl+6`), vi's alternate-file key, unbound on this surface.

**Two conversations on one workspace share a draft filename.** `DraftFile` is
`draft-<sha256(workspace)[:8]>-<pid>.txt` (`draft.go:70-75`), so `/new` twice in one
project collides. The sidecar hides it while a conversation is behind (the box is
held in memory), but the crash-insurance file is still one file for two
conversations. The Revision 2 fix stands and is in the appendix: an ordinal before
the pid, `adoptDraft` globbing from `draftPrefix(workspace)` rather than from its
own name, and a process-owned `adopted` set. **Open:** whether K3 needs it on day
one. Recommendation: yes if `/new` can produce two live conversations on one
workspace, which under the semantics above it can.

**Other lanes are in `app.go` and `home.go` constantly.** K3 is a surgical change
to two files every feature wave touches. Land it as the only thing in its wave,
rebuild, and tell the other lanes to rebase.

**The suite.** `go test ./internal/tui3/` takes ~150 s; budget it. These fail on a
clean tree and are **not** this work's: `cmd/aforge TestHarnessEntriesFromStore`,
`internal/tui TestSettingsSheetIsOneCalmColumnAtEveryWidth` and `internal/plan`.
Confirm with a stash-and-rerun before chasing anything in that list.

**Eight.** The cap is a judgement, not a measurement. If it is ever hit in practice
by somebody who was not testing it, that is evidence the number is wrong, not that
the person is.

---

## Appendix — Revision 2 constraints that still hold

Revision 2 was a rigorous audit of this surface with a twenty-finding review
behind it, and most of what it *found* survives the change of shape. What died
with the partition is only the machinery invented to carry it: the `conversation`
struct and its 191-field census, the L1a-L1d field-move checkpoints, and the
conversation id on 28 message types. Everything below was verified against
`253b019d` and is a constraint K3 must respect.

The countdown table (consent, task proposal, connect, harness, design approval) is
not repeated here — it is in "A question raised in a conversation behind", above,
because that is where the decision it supports is made. One source of truth.

### A1 — what `openSession` does NOT clear, and now must

Revision 2's transition table compared `/new` (`renew`, `app.go:4087-4168`) with
`/resume` (`openSession`, `welcome.go:248-309`) field by field. Under Revision 3
the two are the same function — `detach` then `attach` — so the table collapses to
one list: **the state `openSession` leaves over today, which `detach` must clear.**
Each was verified by reading both functions.

| left over by `/resume` today | evidence | what detach does |
| --- | --- | --- |
| the whole rail: tasks, `taskOrder`, `taskSeen`, the lanes' generations, the rooms, the pilots, `away` | `renew` calls `dropTasks` (`app.go:4143`); `openSession` never does | `dropTasks()`, and the arriving rail is rebuilt from `TaskIndex()` |
| `copy` — a frozen viewport of a transcript that is gone | reset by `renew:4141`, untouched by `openSession` | reset |
| `rew`, `rewSheet`, `rewSay`, `rewSayAt` — a cut line through a transcript that is gone | untouched by **both** | reset |
| `workOpen` — per-turn fold state carried into another conversation's turn numbers | reset by `renew:4136`, untouched by `openSession` | reset |
| `harnPanel`, `harnessStep`, `harnPick`, `harnChip` — a choice about the next message of the conversation being left | cleared by `renew:4118-4123`, untouched by `openSession` | cleared |
| `taskSheet` — the *project's* record, re-read on `/new`'s way out (`:4162-4167`) | untouched by `openSession` | closed, re-read on attach |
| `pathSeen` — which paths in *this* transcript were linkable | kept by both; cleared at every turn end anyway (`settle`, `app.go:2609`) | cleared |
| the lane generations on a `/resume` — re-subscribed without bumping every counter | `openSession:308` re-watches; only `gen` is bumped at `:292` | **all** bumped by attach |
| `earlier`, `earlierFloor`, `earlierFrom`, `earlierSeam` — the compacted region spliced into this transcript | untouched by both; corrected by the `replay()` that follows | zeroed, then `replay()` — so the correction is the design and not luck |

**And the four that are preserved on purpose.** `renew` keeps the draft
deliberately — "The draft is NOT cleared: /new closes a conversation, and the
sentence in the box is the person's next one" (`app.go:4149-4151`) — along with
`chips` and `sent`, because a picture attached to an unsent sentence goes with the
sentence, and `draftFile`. Under Revision 3 those four are what the **sidecar**
carries, which is the same promise kept per conversation instead of per surface.
A test must pin them; they are the rows a future cleanup will delete by accident.

### A2 — channel safety, and the eight lanes

Verified, and the reason detach must unsubscribe rather than simply stop reading:

- **`eventStream` is an unbounded producer queue in front of an unbuffered
  channel.** `out` is `make(chan Event)` (`agent.go:1580`, `:1605`); `send` appends
  under a mutex and signals a cond var, never blocking and never dropping
  (`:1611-1618`); one `pump` goroutine per stream drains the queue into the channel
  (`:1632-1651`). The comment at `:1573-1577` says why: a blocking send would stall
  the loop mid-tool-batch, and a dropping send would lose text deltas, which are
  the one event whose loss reads as corruption.
- **So an undrawn conversation cannot stall its agent** — the producer never waits.
  This is the load-bearing fact for "a conversation behind is fully alive".
- **A pump parks on its last send if the consumer walks away** — its own comment
  (`agent.go:1637-1640`). Hence: unsubscribe, or drain. The keeper does both.
- **The wake lane is buffered 8 and deliberately dropping.** `Wakes()` returns
  `make(chan (<-chan Event), wakeLaneDepth)` (`agent.go:1451-1466`) and `wakeLocked`
  sends non-blockingly with `default: stream.close()` (`:1414-1423`).
- **`eventHub` has no replay** (`agent.go:1484-1494`) — which is exactly what K1
  fixes for the in-flight turn, and which nothing else in this design needs.

**The eight lanes**, each of which detach must release and attach must retake: the
turn stream (`Submit`), the task lane (`TaskUpdates`, `task_run.go:1761`), the wake
lane (`Wakes`, `agent.go:1451`), the design lane, the orchestration lane
(`Orchestrations`, `orchestrate.go:343`), the pilot lanes (one per running node),
the room lane (`WatchTask`, `task_room.go:194`), and the orchestration polling
clock (`orchTick`, `roomorch.go:414`), which re-arms itself.

### A3 — what is shared, once, per process (K2's table)

| thing | where | why one |
| --- | --- | --- |
| the memory store | `v3Memory`, `chatv3.go:497` / `:1034` | **must** be one — `internal/store/store.go:608-618`, `writelock.go:40-50`. Two `*Store` values on one file are two pools with nothing but SQLite's `busy_timeout` arbitrating them |
| the model catalog | `catalog.LoadLazy`, `chatv3.go:459` | one lazy warm, one cache on disk |
| the sub-harness registry | `subharness.Default()`, `chatv3.go:473` | the law is written at `chatv3.go:466-472`: two stores at one directory is how `/harness` and the offer card come to name different harnesses |
| the deliverables index | `artifactsIndexPath()`, `chatv3.go:503` | one file per machine, and `/export` and `/files` must resolve the same one |
| the history store | `history.New(<v3dir>/history.jsonl)`, `chatv3.go:190-197` | **one file, filtered per workspace** — the entry carries `Cwd` and the surface asks `RecentFor(a.workspace, …)` (`recall.go:130`). The *enablement* is per workspace (`KeyHistoryEnabled`, `chatv3.go:182`) and rides on the `Conversation` bundle |
| the connections manager | `v3Connections(cfg.Connect)` | an account connected on the panel is connected for every conversation's belt in the same breath |
| the profile and its settings registry | `settings.ProfileDir` | one profile, and the panel must write the file the door reads |
| the idle reaper | `startPlaceSweep`, `chatv3_sweep.go:40-44` | already a `sync.Once`, and it already skips folders whose transcript is locked |

**Memory is legitimately profile-scoped and stays so** — `KeyMemoryEnabled` is not
in `config.ProjectKeys`, so there is no per-workspace answer to give. Each agent
still gets its own memory pass and its own context.

**One shared-store race this design does not fix and must name:**
`subharness.Store.Save` (`internal/subharness/store.go:77`, `os.MkdirAll` at `:105`)
has no lock, so two conversations saving a design under the same name at the same
moment can race at file creation. It is unchanged by this feature — two terminals
can do it today — and it is recorded so nobody reads "one store per process" as
"the store is now safe".

### A4 — the draft name, and adoption

Today the draft file is `draft-<sha256(workspace)[:8]>-<pid>.txt` (`DraftFile`,
`internal/tui3/draft.go:70-75`; `draftPrefix`, `:79-84`; `draftOwner = os.Getpid()`,
`:52`). The shared prefix is what lets a window adopt an orphan another window left
on the same directory (`adoptDraft`, `:147-200`), and the trailing pid is what
`draftWindowAlive` parses to decide whether that other window is gone (`:202-217`,
answering "alive" to anything it cannot tell, on purpose).

Two conversations on one workspace in one process collide on that name, so it gains
an ordinal — `draft-<hash>-<n>-<pid>.txt` — **and that alone is not enough.**
`adoptDraft` does not glob from the workspace prefix; it globs from its own filename
with the last dash-separated token removed:

```go
name := strings.TrimSuffix(filepath.Base(own), filepath.Ext(own))
cut := strings.LastIndex(name, "-")                                    // draft.go:150
candidates, err := filepath.Glob(filepath.Join(directory, name[:cut+1]+"*.txt"))  // :156
```

which with an ordinal becomes `draft-<hash>-<n>-*.txt` — sweeping only drafts whose
*ordinal* matches, so conversation 2 would never see the orphan a dead
single-conversation window left as `draft-<hash>-0-<pid>.txt`, which is the only
case adoption exists for. So:

- **`adoptDraft` takes the workspace** and globs `draftPrefix(workspace) + "*.txt"`
  — the stable prefix, spelled by the function that already exists for it
  (`draft.go:79-84`).
- **The pid stays the last token**, so `draftWindowAlive` (`:202`) is unchanged.
- **Firstness is not inferred from the ordinal** — the first conversation on
  workspace B is the third conversation of the process. The process holds
  `adopted map[string]bool` keyed by the canonical workspace, and `restoreDraft`
  (`draft.go:119-131`) falls through to `adoptDraft` only when this process has not
  yet attempted adoption on that workspace. A conversation opened from home an hour
  in must not paste a stranger's unfinished sentence into its box.
- The "adopted once, by the next window to open" law (`draft.go:139-142`) is
  unchanged; the rename is still the claim.

`keepDraft` is read through the project layer (`chatv3.go:186-189`,
`config.ProjectBoolAt(workspace, …, KeyDraftPersist)`) and rides on the
`Conversation` bundle, because a repository that says "keep nothing from this
directory" is answering about **its** directory. A conversation whose project says
no gets an empty `DraftFile`, which is already how the surface spells "not kept at
all" (`draft.go:71`, `:100`).

### A5 — `LaunchDir`, and the shell's cwd

`Meta.LaunchDir` is "where the person actually stood when the session opened — the
repo subdirectory, or the temp dir whose presence marks the session as sweepable
litter" (`internal/session/place.go:148-151`), and `chatv3_layout_test.go:156` pins
it distinct from `Workspace`. Both values are consumed: `projectPath` reads
`LaunchDir` to name the project of an **owned** session
(`internal/session/world.go:458-470`), and `sweepIsLitter` marks a session
disposable if **either** value is under a temp directory (`sweep.go:206-208`,
`underTempDir` at `:220-238`). The rule, and its exception, are in K2 above.

**The shell's cwd does not change, ever.** `cmd/aforge/engine.go:75-77` chdirs into
the workspace before assembling, because a remote engine has one conversation and a
process that agreed to be in one place. The chat door must not: the tool root is a
property of the agent, not of the process (`bare.AllTools(cwd)` fixes it at
construction and `cmd.Dir = cwd` uses it per call, `internal/exec/bare/tools.go`),
and the person's shell is theirs. A process that chdir'd on a switch would break
`/image ./shot.png`, the `@` completion walk, and anybody's expectation about where
they will be standing when aforge exits. It is also why `v3LaunchDir()` is captured
once.

### A6 — `gitRoot` is a global mutex, not keyed by root

The repository lock for a landing task is span-scoped rather than lifetime-scoped
(`lockGitRoot`, `internal/session/task_lock.go:83-96`), so it is held across a
merge and released. Its in-process half is a bare package-level `sync.Mutex` —
`var gitRoot sync.Mutex` (`task_run.go:3190`) — **not keyed by repository root.**
Today that costs nothing, because one process works in one repository. With two
conversations in two repositories, a node landing in project A serializes against a
node landing in project B for the duration of a merge. A latency wart, not a
correctness bug (the file lock beside it is already per-root — `gitRootLockName`,
`task_lock.go:34-53`).

**The fix, with its lifecycle:** a `map[string]*sync.Mutex` guarded by one small
`sync.Mutex`, keyed by the **canonical** repository root — the same
`EvalSymlinks`-then-`Clean` rule `convKey` uses, because `/tmp` and `/private/tmp`
are one repository on this platform and two map keys otherwise. **Entries are never
deleted.** The map is bounded by the repositories a process visits, which is bounded
by the conversation cap; deleting an entry while another goroutine holds or waits on
it is a bug with no upside, and reference counting is machinery bought for nothing.

### A7 — `approval` reads the profile while the gate reads the project layer

`a.approval` (`app.go:628`) is the gate's blanket posture drawn as the YOLO
segment, and today it is `readApproval(a.profileDir)` →
`config.ToolApprovalModeAt(profileDir)` — profile only, no project layer
(`app.go:5043-5048`, `:5062-5067`, assigned at `:1318`, recomputed at `:2649`).
Meanwhile the gate the conversation actually runs behind is resolved per workspace,
and `KeyToolApprovalMode` **is** in `config.ProjectKeys`
(`internal/config/projectconfig.go:83`), so a repository can answer it. That is
already a small lie — a project that sets `allow` shows no YOLO segment — and with
two workspaces in one process it becomes a visible one. It rides on the
`Conversation` bundle (K2) and is read through the project layer.

### A8 — independent bugs found while auditing, each shippable alone

- The **boot-pointer capture in the approval closures** (`chatv3.go:340-342`,
  `chatv3_approval.go:68-108`) is a live bug today and K2 is what fixes it. If this
  design is shelved, lift the fix out and ship it on its own.
- The **orchestration fuel gate does not reach the presence file**
  (`taskpresence.go:501` never looks at `a.orchestrations`), so a run waiting on a
  person reads as *working* to every window on the machine. That is K1.
- **`homeTickMsg` has no generation** (`home.go:93`), so closing and reopening home
  can multiply its clock. One field, K3.
- **Home never stats the workspace** (`world.go:385-390`), which is safe only while
  `enter` cannot leave this project.
- **`subharness.Store.Save` has no lock** (`store.go:77`, `:105`). Not this
  feature's, and not fixed here.
- The comment at `chatv3.go:727` names the project config directory as
  `.openaf/config.json`; the code says `.aforge-v3` (`projectconfig.go:69`). One
  line, in a file K2 is already in.

### A9 — the test plan, in one place

By lane, so that none of it is discovered late:

- **The engine primitive**: replay mid-turn is complete and in order; replay
  between turns is empty and reports not-running; unsubscribe releases the pump,
  asserted by the goroutine returning rather than by a sleep. (K1)
- **The shared predicate**: `NeedsPerson` and the presence file agree on all five
  lanes; a paused run says `waiting on you` to every window. (K1)
- **The approval seam**: an "always" answered after a `/new` reaches the gate the
  new conversation is running behind. **Fails on `master` today.** (K2)
- **Process resources**: `openV3Process` called once, its store pointer carried by
  every launch; a launch whose later step fails closes the partial agent and leaves
  no flock; `CloseAll` after an abnormal `tea.Program` return closes every agent
  and is idempotent, asserting ordering and concurrency and **never wall time**. (K2)
- **`LaunchDir`**: a conversation opened from a temp-dir launch into a real
  repository is not litter. (K2)
- **Detach/attach**: the sidecar round-trips exactly its fields and nothing else;
  the transcript and the in-flight turn are redrawn whole; a detached conversation's
  turn settles into its own journal and draws nothing in front; the consent card
  comes back with the same time left, and is **not** restored when the engine no
  longer holds the question. (K3)
- **Identity before flock**: `/resume` and `enter` on a transcript this process
  holds attach and never say `another window`; a row another *process* holds still
  refuses — the existing lock tests must stay green untouched. (K3)
- **Home**: `TestHomeOpensThisProjectAndNamesWhereTheOthersLive` rewritten so
  another project's row opens and the conversation left behind is still running; a
  gone folder refuses and home stays up; the cap refuses in home's own voice. (K3)
- **`tab` precedence**: one case per rung of the sixteen-row table. (K3)
- **Close semantics**: the armed line with two conversations and work in both;
  `/quit` with two open leaves aforge running and attaches the other; `/quit` with
  one open quits; the closed key is gone from `prev`; `/new` on a fresh empty
  conversation replaces it and on a used one adds, carrying the draft. (K3)
- **The count segment**: absent at one conversation, present at two, `· N waiting`
  absent when nothing is waiting. (K3)
- **The engine countdowns, asserted as designed rather than as hoped**: a task
  proposal in a conversation behind still auto-approves at its deadline
  (`internal/session/task.go:522`); a connect offer behind still lapses; both say
  `waiting on you` until they do. (K1/K3)
- **The draft adoption matrix** across pid × ordinal × liveness, plus: the second
  conversation on a workspace does not adopt. (K3)

### A10 — the review log, trimmed

Revision 2 answered twenty findings from a review of revision 1. Five of them were
about the machinery this revision does not build and are **retired**: the 191-field
census and its bucket classifications (1), the "fields `openSession` touches are
per-conversation" rule (2), the L1 split into four checkpoints (3), the
33-message-type routing inventory (4), and the generation-comparison census as a
routing problem (5) — of which the useful residue is kept above: A1's leftover
list, A2's lane inventory, and `homeGen` in A8.

The rest still bind, and each was verified against `253b019d`:

| # | finding | where it lives now |
| --- | --- | --- |
| 6 | events of a conversation nobody is looking at must not move the screen | free here: detach unsubscribes, so nothing arrives. The notification predicate (`notify.go:65`, `:96`) still changes to `focused && front` |
| 7 | channel-safety description imprecise; more than four lanes | A2, with the eight lanes and the parked-pump risk that makes the stir watcher necessary |
| 8 | "countdowns hold" is true only for consent | the countdown table and its decision, in the body |
| 9 | the shared "needs the person" predicate does not exist | K1's `NeedsPerson()`, plus the fuel-gate term |
| 10 | the proposed draft name breaks adoption | A4, whole |
| 11 | `LaunchDir = workspace` violates the contract | A5 and K2, including the temp-directory exception the review itself did not have |
| 12 | identity must precede flock | "Identity before flock", in the body — sharper than the review stated it, because a flock rides the open file description |
| 13 | keyed `gitRoot` needs a lifecycle | A6: canonical key, entries never deleted, no reference counting |
| 14 | the old L3 did not say how process resources are built | A3 and K2's `v3Process` |
| 15 | parallel close fine; shared-resource claims overstated | "Quit, close, `/new`, and the cap": phases are sequential (`agent.go:1000-1039`), so two seconds is not a bound; the store closes after every agent by LIFO defer (`chatv3.go:159-163`); the file is `internal/store/writelock.go` — there is no `internal/memory` package |
| 16 | `tab` is not "reliably nothing" | the sixteen-rung precedence table and its two fixes, in the body |
| 17 | `/new`, `/resume`, welcome transitions underspecified | "Quit, close, `/new`, and the cap": `/new` adds unless fresh and empty, `/resume` attaches an open transcript, the welcome box's empty conversation is closed by the same test |
| 18 | remote and headless entry paths | K2 item 6: single-slot hosted `Open`/`Start`, `engine.go` keeps its chdir, `--once` instantiates no keeper |
| 19 | test plan too narrow | A9 |
| 20 | strengthen open-target validation and rollback | K2: canonicalise, require a directory, refuse with `that folder is gone · <path>`, and close a partial agent before returning an error so a failed open never leaves a flock or a heartbeat behind |

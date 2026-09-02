# Models, context, and what it costs

## Which model am I talking to, which model is it using right now, and how do I switch or change it

The model in use is written in the status line. There are two doors to the picker:

- type `/model` with nothing after it, or
- press the model's name in the status line.

If you have turned the mouse off (`ui.mouse`), only the command works.

**The name you press is the model you move.** Out in the conversation that is the
conversation's model. Inside a running task's room the status line names *that task's*
model — `task <name>` — and pressing it opens the same picker aimed at that task alone,
from its next turn onward. Nothing else moves: not the conversation, not any other task.
See "Changing the model for one task while it is running" on the tasks page. Inside a
task that has finished the name is still there to read and cannot be pressed.

The picker is a filter box in the input line's place with a short list of models under it.
It is bottom-anchored: the conversation shrinks above it, so nothing pops up over what you
were reading.

Choosing a model does four things: the model is set on the session, the machine running
the session learns that model's context window for compaction, a note appears reading
`model · <model>`, and the choice is written into your profile.

Over `--host`, the picker and its prices are this laptop's catalog, while the context
window used for compaction comes from the far machine's catalog. The machine doing the
work owns that execution limit even when the two catalog caches differ.

## Does aforge remember the model I picked, or does it go back to the default?

**It remembers.** The model you last switched to is the model the next `aforge` opens on,
whether you chose it in the picker, typed `/model <slug>`, or set the conversation row in
`/settings` — all three are the same road.

The order a launch resolves is: `--model <slug>` on the command line beats everything for
that session alone; then the model you last chose; then `AFORGE_MODEL`; then the built-in
default. `AFORGE_MODEL` seeds a model for somebody who has never chosen one and does not
override somebody who has — which is why the settings row stays editable while it is set.

Two things do not persist. Over `--host` the switch takes for the session and is not
written anywhere: the model a remote session opens on is resolved on that machine, from
that machine's profile. And reasoning effort is kept per model for the session, not
written to the profile.

esc leaves the picker and changes **nothing** — your half-typed draft, the model in use and
the frame all come back exactly as they were. The picker holds its own filter text, and the
filter is forgotten when it closes.

## Moving and filtering in the model picker

Type to filter. The keys:

| Key | What it does |
|---|---|
| ↑ / ctrl+p, ↓ / ctrl+n | move a row |
| pgup / pgdown | move 12 rows |
| left, right, home, end, ctrl+u, ctrl+w | edit the filter text |
| ctrl+t | walk the reasoning effort of the model under the cursor |
| tab, → | open the lanes — the providers serving the model under the cursor |
| tab, ← | close them again |
| enter | switch to the row under the cursor — or, on an open lane, pin it |
| esc | cancel, changing nothing |

`→` and `←` open and close the lanes only from the **end** and the **start** of what
you have typed; with characters to step over they move the caret through the filter
instead. `tab` always opens and closes.

**This is one list with two doors.** `/model` opens it, and so does the **your model**
row at the top of the Providers tab in `/settings` — the same rows, the same filter
grammar, the same lanes under `→`, and `enter` on a lane pins it either way. The media
slots on that tab (**drawing**, **speaking**, **looking** and the rest) open the same
component over their own models, but they have no lane row behind them, so nothing
unfolds under them and the foot does not offer the key.

The cursor opens **on the model in use**, which is also the marked row, so enter with
nothing typed confirms rather than changes.

Filtering splits what you type on whitespace; every token must match, each in one of three
tiers — prefix, then substring, then subsequence. So `ds v4` finds
`deepseek/deepseek-v4-flash` and `claude 4.5` finds `anthropic/claude-sonnet-4.5`, and fuzzy
hits sit at the bottom rather than mixed through. Twelve rows show at a time.

The picker **never fetches**. The list comes from what is already known, in this order: the
catalog the door passed in, then `~/.aforge/v3/models.json`, then five names this build
remembers (`deepseek/deepseek-v4-flash`, `openai/gpt-4.1-mini`,
`anthropic/claude-sonnet-4.5`, `google/gemini-2.5-flash`, `moonshotai/kimi-k3`). Each rung is
tried only when the one above it came back empty after filtering.

The placeholder in the empty filter box is the only place the overlay explains itself:
`filter · ↑↓ · → lanes · ctrl+t effort · enter · esc`

There is no mouse commit on the picker's rows.

## What each row in the model picker tells you

A row reads `<id>:<level>` on the left and, dimly on the right, the facts about it — in
this order, which is the order they are given up in when the window is narrow: the
machine that would serve it (`via cloudflare`), the wait before the first word
(`▲0.8s`), the price per million prompt and completion tokens, the context window, how
fast it writes (`58t/s`), the arena elo, and what the model can do besides write.
**Each part is hidden when nobody published it.** A price shows only when both halves are
known — a zero means "nobody said", never "free".

**A narrow window shows fewer numbers, never a shortened name.** The id keeps every cell
it needs first; then the facts are added from the front of that list, each in the longest
spelling that still fits — `$0.09/$0.18 per M` becomes `$0.18/M` becomes `$0.18`, and
`via cloudflare` becomes `cloudflare` — and the ones that do not fit are simply not
drawn. So a sixty-column terminal shows the whole model name with the lane, the wait and
the price beside it, and nothing is ever half a number. Under sixty columns the facts
move to a line of their own under the name. The only time a name is shortened is when the
window cannot hold it alone, and then it loses its author first (`nvidia/nemotron-3.5-lightning`
becomes `nemotron-3.5-lightning`) — unless two models on the list share that slug, which
is the one case where the author is what tells them apart.

Only models you can hold a conversation with are listed: text in, text out. A model that
publishes `["image","text"]` out (a drawing model that captions) is excluded, and so is a
transcription model (audio in, text out). A row that publishes nothing about itself is judged
by its id against a narrow list of generation and sidecar words.

## What "sees", "draws", "speaks", "films", "hears" mean on a model row

The dim tail of a picker row ends with what the model can do besides hold a conversation,
in one word each — last in the row's order, so it is the first thing a narrow window
drops:

| Word | What the catalog published |
|---|---|
| `sees` | it reads images |
| `hears` | it reads sound |
| `watches` | it reads video |
| `draws` | it answers with images |
| `speaks` | it answers with speech, audio or music |
| `films` | it answers with video |

Input words come first, so a model that reads and paints pictures reads `sees · draws`.

**A plain text chat model shows nothing here at all**, and neither does a model that
published no modalities — silence means text in, text out, and nothing more. The same tail
appears on `aforge models`.

## Switching model by name in one command

`/model <slug>` switches straight to that slug — no list, no confirmation.

The words after `/model` are read for their **shape**, not for a flag:

| What you type | What happens |
|---|---|
| `/model` | the picker opens |
| `/model deepseek/deepseek-v4-flash` | switches to that slug |
| `/model @cloudflare` | pins the provider that serves your model — the model does not change |
| `/model auto` | gives the choice of provider back to aforge |
| `/model deepseek <1s` | opens the picker with `deepseek <1s` already in the filter |

Anything with a space in it, and any single word the picker's filter grammar understands
(`fast`, `cheap`, `tools`, `<1s`, `>50t/s`, `$<0.3`, `fp8`), opens the list already
narrowed. A slug has no spaces in it, so two words were never a name.

There is one check, and only one. If the slug **is** in the catalog and cannot hold a
conversation — a drawing model, a speech model, a transcriber — aforge refuses in one line
and the conversation does not move:

```
openai/gpt-4o-mini-tts cannot hold a conversation — it speaks. Still on moonshotai/kimi-k3.
```

A slug the catalog has never carried is still **taken at its word**, exactly as before:
aforge may be offline, or you may be naming a model this build has never listed. In that
case the context window is left alone.

## I changed the model but my task is still on the old one — /model does not move a running task's model

`/model` moves the **conversation**. Work already handed over is not moved: a task's model
is settled the moment the task is admitted and kept for its whole life, so a task that was
running when you switched carries on in the voice it started in. That is deliberate — the
switch you made mid-thought does not silently change the terms of work you already
approved.

When you switch while tasks are running, the note in the conversation says so in the same
line that names the new model:

```
model · anthropic/claude-opus-5 — tasks already running keep the model they started on
```

With nothing running, the note is just `model · <the model>`.

**To move one running task**, walk into its room and press the `task <model>` part of the
status line — the ordinary picker opens aimed at that task, and the change takes effect on
the task's next turn. That room is the only door; there is no command or setting that
re-models running work from outside.

**New tasks follow the switch.** Work admitted after `/model` runs on the model the
conversation is now on — unless you have set `task.model` in settings, which always wins,
or you name a model for that one task in words.

## The crew — which models aforge uses on my behalf, and /crew

aforge makes calls you did not type: naming a session, naming a piece of work on the roster,
the summary a compaction keeps, the safety gate, the check on finished task work, the second
look before a task starts itself, the reading of a task's parts before they are handed out,
the planner of an adaptive run and the nodes under it, the designer of a saved harness page,
looking at an image. Each of those is a
**role**, and every role sits on one of five **classes** — the **crew** — which you set in
`/settings` → Providers, or in one word with `/crew`:

- **reflex** — near-free · reads every turn — memory, titles, safety.
- **small work** — cheap · the small calls — names, digests, the safety gate.
- **worker** — does the work · every task you hand off, the parts it divides into, every
  node of an adaptive run. Most of what a task costs is spent here.
- **careful work** — careful · checks what must not be wrong — audits, compaction, vision.
- **mastermind** — thinks · plans runs and designs harnesses.

**All five arrive with a model already in them**, and the five together are the `balanced`
preset:

| class | as shipped |
| --- | --- |
| reflex | `mistralai/mistral-nemo` |
| small work | `deepseek/deepseek-v4-flash-0731` |
| worker | `z-ai/glm-5.3-flash` |
| careful work | `qwen/qwen3.8-27b` |
| mastermind | `z-ai/glm-5.3:high` |

They are all open-weight models, and none of them is the model you are talking to. A crew
that followed your conversation would put the most expensive model in the build on the
cheapest questions in it — a call made twice every turn on a frontier model is a bill nobody
agreed to. Closed models that are cheaper on their own vendor's platform than through the
router are deliberately not in any preset; you can still pin one on any row.

**Why these ids.** They were picked on 2026-09-01 off the catalog's own published scores —
OpenRouter republishes Artificial Analysis's coding and agentic indexes on every model row
— against blended price, open weights only. The worker seat is the dial: `glm-5.3-flash`
scores 58 on the agentic index and 72 on coding at about $0.12 per million tokens blended,
one point under `glm-5.3` at a twentieth of its price, and it can see images. The
mastermind buys the thinking rung rather than a bigger model, because its calls are few.
The careful class is always a different vendor from the worker and always sees images:
`qwen/qwen3.8-27b` scores 68 on coding at $0.42/M input and $2.55/M output with a 1M-token
window. The small-work row is pinned to the July build of DeepSeek V4 Flash on purpose —
the bare `deepseek/deepseek-v4-flash` id resolves to the April build, and the July build at
the same price scores thirteen coding points higher.

**Clearing a row is still an answer.** A class you empty on purpose reads
`follows the conversation`, and every role on it runs on the model you are talking to. That
is the only way to say "use my model for this", and it is deliberately something you have to
say rather than the default.

### The three presets

| | frugal | balanced | max |
| --- | --- | --- | --- |
| reflex | `mistral-nemo` | `mistral-nemo` | `mistral-nemo` |
| small work | `deepseek-v4-flash-0731` | `deepseek-v4-flash-0731` | `deepseek-v4-flash-0731` |
| worker | `deepseek-v4-flash-0731` | `glm-5.3-flash` | `glm-5.3` |
| careful work | `glm-5.3-flash` | `qwen3.8-27b` | `kimi-k3` |
| mastermind | `glm-5.3-flash:high` | `glm-5.3:high` | `kimi-k3:high` |

- **frugal** — deepseek works, glm-flash thinks · pennies a day
- **balanced** — glm-flash works, glm-5.3 thinks, qwen checks
- **max** — glm-5.3 works, kimi-k3 thinks and checks

The worker column climbs the open-weight front one step per preset, because it is the seat
that pays most of a task's bill. The reflex and small-work columns never vary — they are the
same near-free models in all three — so `/crew` never names them: the confirmation and
`/status` say **brain**, **hands** and **checks**, which are the mastermind, the worker and
the careful class.

`/crew` opens all three as a chooser with yours marked, under a scope line — `the five
models aforge uses on its own behalf — not the one you chat with` — and a `you talk to ·
<model>` line naming the seat the presets do not touch. ↑ / ctrl+p and ↓ / ctrl+n move;
enter applies and esc cancels. If the five classes make a custom crew, no row is marked and
the chooser says picking one puts all five back. `/crew max` still sets it directly and
confirms in one line, which ends `· you are still talking to deepseek-v4-flash — /model
changes that` — naming the conversation's own model by id, because the crew changes
nothing about it and the model segment on the status line goes on saying what it said before.
The **crew** row in `/settings` → Providers is the same thing: enter or space walks it
frugal → balanced → max.

**The crew row is not stored — it is worked out from the five.** Answer any one of the five
rows yourself and the crew row reads `custom`, because that is what is true. `/crew balanced`
puts all five back in one write. A profile that applied a crew before the worker row
existed reads `custom` until a preset is applied again, because its four old rows and the
new fifth are not any of the three.

### The roles under each class

Directly under the **pinned roles** row the panel lists **every registered role**, grouped
under the class answering it, saying which model comes out. As shipped:

| role | class | what it is |
| --- | --- | --- |
| `reflex` | reflex | reads every turn for memory — routing and keeping |
| `title` | small work | the name a session gives itself |
| `worker` | worker | one node of an adaptive run, and the worker of every task |
| `guardian` | small work | is this one tool call plainly safe |
| `router` | small work | whether a turn should have been work |
| `consolidate` | small work | tidies what is remembered while nobody is here |
| `taskname` | small work | the two or three words a task is called |
| `compaction` | careful work | the summary that survives a compaction |
| `auditor` | careful work | whether finished-looking work is actually finished |
| `vision` | careful work | reads images for a model that cannot see them |
| `shaper` | careful work | the brief a task you started yourself is given |
| `careful` | careful work | a part of a task that needs judgement |
| `repair` | careful work | the second go at work a check found gaps in |
| `planner` | mastermind | the plan that steers an adaptive run |
| `designer` | mastermind | writes and reviews a harness page |
| `routerconfirm` | mastermind | a second look before work starts itself |
| `markreader` | mastermind | what is left of a long answer, and whether it has parts |
| `handoff` | mastermind | the instruction a handed-over turn gives whoever finishes it |
| `division` | mastermind | the parts a worker hands its own work out in |

The list is built from what is registered in the running binary, so it is the truth about
this build rather than a table someone kept up to date. Stop on a row and the line under the
list is that role's own description followed by which class it follows.

**What the mastermind's roles have in common is that one answer decides what all the other
calls do.** `planner` and `designer` used to sit on careful work beside the compaction
summary, which made one model id answer two unrelated bills: the careful calls are many and
short, and these are few. A planner that cuts badly spends a whole run on work nobody wanted;
a designer that writes badly puts a wrong answer on the menu with a name on it;
`routerconfirm` stands between a cheap model's "that should have been work" and a task
starting itself, and it is asked on nothing else, so it costs a call only where something was
about to be spent; `division` reads a task's parts before any of them exists, and every turn
every part ever takes runs on the brief it leaves behind.

`markreader` and `handoff` are the two calls a long answer makes (*Tasks*). `markreader` is
asked at most three times, and only on an answer that has already spent ten rounds of tool
calls, plus once at the end of any answer that touched a tool at all — it reads the account of
the work and says what is left of your question. `handoff` writes the instruction the task
opens on when an answer is handed over. Both sit on mastermind for the same measured reason:
a cheap model asked "is this finished" answered `(done)` about half-finished work 15 times out
of 18, and that is the one answer that quietly drops a handover you were owed. There is no
cheaper reading of that question — there is only a wrong one.

**`careful` is not a call at all** — it is the model a *part* of a divided task runs on when
the worker graded that part careful (*Tasks*). It sits on careful work beside the audit for
the same reason: the failure it guards against is work that looks finished and is quietly
wrong.

## I changed the crew but the model at the bottom did not change — why did my model not change

That is right, and nothing is broken. **`/crew` does not change the model you are talking
to**, and the readout at the bottom of the frame is that model — the conversation's. The
only thing that moves it is `/model`, the model row in `/settings`, or naming one with
`/model <name>`. The confirmation says so by name: `/crew max` ends
`· you are still talking to deepseek-v4-flash — /model changes that`, and the status line
now carries `crew max` beside the model so the two dials read as two.

The crew is a different dial: the five **classes** aforge makes its own calls on — reflex,
small work, worker, careful work, mastermind — used for titles, memory, the safety gate,
the work inside every task, checks on finished work, compaction summaries, adaptive-run
planners and their nodes, harness pages, and looking at an image. Setting it writes all
five class rows in one write, and **it is live from that moment**: the next call aforge
makes on its own uses the new crew, with no relaunch and no new session. A task already
running keeps the model it was admitted on.

**Where to read the crew back:**

- `/status` prints a `crew` line directly under `model`:
  `crew     max · brain kimi-k3:high · hands glm-5.3 · checks kimi-k3`. The word is
  the preset, or `custom` when the five classes are your own arrangement. **brain** is the
  mastermind, **hands** is the worker, **checks** is the careful class.
- `/settings` → Providers has the **crew** row above the five class rows.
- Bare `/crew` opens the three presets with yours marked, under a `you talk to · <model>`
  line naming the seat they do not touch.
- The live status line says `crew max` at the head of the telemetry, across the gap from
  the model segment — the same word `/status` prints, read from the same five rows.
- The hint line under the model picker says `crew max` beside its keys, so the picker you
  opened looking for the change tells you the crew is a separate thing.

**Tasks DO follow the crew, through its worker seat.** A plain task runs on the
`task.model` row if you set one, otherwise on the crew's **worker** class, and only when
that row is blank on the conversation's model. An **adaptive run** uses the classes the
same way: its planner takes the **mastermind** class and every node under it takes the
**worker** class. Those ids are settled once, when the task or run is admitted, so changing
the crew — or `/model` — half way through does not move work already going.

## Which model does a task run on — why did my task run on glm-5.3-flash and not my chat model

**The crew's worker seat**, unless you said otherwise. The ladder, first answer wins:

1. a model named in the ask — "do this on deepseek" — or picked on the proposal's chips;
2. the `task model` row under `/settings` → Tasks, when you have set one;
3. the crew's **worker** class — `hands` in the `/crew` confirmation and the `/status`
   crew line;
4. the model you are talking to, only when the worker row is blank.

So on the shipped `balanced` crew a task runs on `z-ai/glm-5.3-flash` whatever you are
chatting on, and `/crew max` moves the next task onto `z-ai/glm-5.3`. The task's row on the
roster, its room's status line and its finished card all name the model it actually ran
on. The worker of an adaptive run's nodes is the same seat, and so is the work model of
`aforge do` — one row, every door.

This is new: until the worker seat existed a task rode the model you were talking to, and
the crew moved everything about a task except its cost.

## Does my crew reach aforge do, or only this conversation — what models a headless run uses

**It reaches both.** A crew you set here is the crew a run started from a script or a
terminal uses — `aforge do`, `aforge exec`, `aforge plan`, `aforge run`, `aforge revise`
and `aforge run subharness`. Set it once with `/crew frugal` and the same policy holds
whether the work is asked for here or run with nobody watching.

Those runs seat two models, and each one is resolved the same way. The first of these that
answers wins:

1. a model named on the command line — `--model` for the work, `--plan-model` for the
   planning;
2. `AFORGE_MODEL` / `AFORGE_PLAN_MODEL` in the environment;
3. **your crew** — the planning seat takes the **mastermind** class, the work seat takes
   the **worker** class, the same row a task handed off in conversation rides;
4. **your crew again, through an older class**, when your profile was set before a class
   existed — the worker class inherits the small-work class it was split out of, and the
   run says it did;
5. what the build ships with.

So `--model` is one voice of four rather than the only one. This was not always true: until
recently a run outside the chat read only the flag and the environment, and a crew set here
was silently lost the moment the same brain ran from a script.

Each of those runs opens by saying which voice answered, so nothing has to be guessed at:

```
models: work deepseek/deepseek-v4-flash-0731 (crew frugal) · plan z-ai/glm-5.3-flash:high (crew frugal)
```

Two details worth knowing. A crew answers only once you have actually set one — a profile
nobody has touched takes the build's default rather than reading its own shipped values back
as a crew. And a class carrying a thinking level, like `kimi-k3:low`, carries it there too:
the run plans on that model at that level, the same as it does here.

## Why does my run say inherited — my crew is older than the worker class

The worker class arrived after the other four. A crew set before it exists on disk as four
classes with no worker among them, so a run has no worker row of its own to read. It does
**not** fall back to the shipped default: it takes the class the worker was split out of —
**small work**, the row that used to do this job — and it tells you, in one line under the
models line:

```
models: work deepseek/deepseek-v4-flash (crew custom, inherited) · plan deepseek/deepseek-v4-flash (crew custom)
your crew was set before the work seat existed · it is running on your small work model until you pick a crew again
```

`inherited` beside the class means exactly that: **the model came from your crew, but from a
row you did not write.** The line is said once, when the run opens, and never again — not on
every call.

The same thing happens in the conversation, where there is no models line to carry the word —
see "Why is my task running on a model I did not pick".

To end it, set the crew again with `/crew frugal`, `/crew balanced` or `/crew max`, which
writes all five classes including the worker, or pin the worker row alone in `/settings` →
Providers. Either one, and the next run reads `crew frugal` with no second line.

A crew set with this build already pins every class, so this only ever appears on a profile
older than the class. And it is only for a row you never wrote: a row you **emptied on
purpose** means "follow the conversation", which a run outside the chat has no conversation
for, so that falls to the build's default the way it always has.

## Why is my task running on a model I did not pick — inherited work seat in the conversation

Tasks you hand off in a conversation run on the **worker** class, not on the model you are
talking to. If your crew was set before that class existed, you have no worker row — so the
work takes the class the worker was split out of, **small work**, and the thread tells you
once, the first time a task starts:

```
your crew was set before the work seat existed · it is running on your small work model until you pick a crew again
```

**It is said once per session**, when work actually starts, and never per task or per part.
Twenty tasks in one sitting is one line. Start aforge again tomorrow with the same profile and
you get it again — it is true until you answer it.

`/crew` shows the same fact about the row itself, under the three presets:

```
your work seat is inherited from small work — picking one writes it
```

**To end it, pick any crew** — `/crew frugal`, `/crew balanced`, `/crew max`, or the chooser
that bare `/crew` opens. Every preset writes all five classes including the worker, so the
line stops on both surfaces at once. You can also pin the worker row on its own in
`/settings` → Providers.

Three things this is **not**:

- It is not the model you talk to. That one is on the status line and only `/model` moves it.
- It is not a row you emptied. A worker row you cleared on purpose means "follow the
  conversation", and a task then rides the model you are talking to — that is an answer, and
  nothing is said about it.
- It is not a fresh install. A profile that has never named any model runs this build's own
  choice for each class, silently, the way it always has.

The word `inherited` is the same word `aforge do` prints beside the model on its `models:`
line, so the two surfaces are telling you about one thing.

## What are the six models — the one you talk to and the five crew seats

aforge runs **six model seats**. **Seat one is the model you talk to**: it answers every
message you type, it is the id on the left of the status line, and `/model` is the only thing
that moves it. The other five are the **crew** — the models aforge uses on its own behalf,
for calls you did not type:

| seat | word | what it answers |
| --- | --- | --- |
| 1 | you talk to | your messages — set with `/model` |
| 2 | reflex | memory, titles, the safety gate — near-free, reads every turn |
| 3 | small work | digests, task names, the safety gate's yes-or-no — cheap |
| 4 | worker | every task you hand off, its parts, every run node — most of the bill |
| 5 | careful work | checks on finished work, compaction summaries, vision |
| 6 | mastermind | plans adaptive runs and designs harnesses — thinks |

`/crew` shows all six and sets seats two to six in one word — `frugal`, `balanced` or
`max` — and never seat one. Bare `/crew` opens with `you talk to · <model>` above the three
presets, so the seat the presets do not touch is on the same page as the ones they do.
`/settings` → Providers pins any one of the five on its own, which turns the crew word to
`custom`. The live status line says both dials: the model segment on the left is seat one,
and `crew max` at the head of the telemetry on the right is the other five.

## Does /crew change my chat model — no, and what crew max on the status line means

No. `/crew max` moves the five crew seats and leaves the model you talk to exactly where it
was. The confirmation names it:

```
crew → max · brain kimi-k3:high · hands glm-5.3 · checks kimi-k3 · you are still talking to deepseek-v4-flash — /model changes that
```

`/model`, `/model <name>` or the model row in `/settings` are the only ways to change the
chat model, and `/crew` never offers to. The two dials also stay separate on the frame: the
status line shows the chat model on the left and `crew max` — or `crew balanced`,
`crew frugal`, `crew custom` when you pinned a seat yourself — at the head of the telemetry
on the right. That segment is a setting, not a measurement, so it is among the first things
a narrow row gives up; `/status` prints `model` and `crew` on neighbouring lines at any
width. The one session with no `crew` segment at all is a **remote** one opened with
`--host`: that crew lives on the other machine.

## Asking a class to think harder — a level on a class value

A class value may carry a thinking level as well as a model:

```
moonshotai/kimi-k3:high
```

`:low`, `:medium` and `:high` are the three, and the shipped **mastermind** carries `:low`.
The level is not part of the model id — it travels as its own request option, exactly as
the picker's **ctrl+t** effort does — so the id sent to the provider is
`moonshotai/kimi-k3` and the thinking is asked for separately.

- **Any of the five class rows takes one**, though the mastermind is the one it is for. On
  the worker row it reaches the one-shot role calls only, never the work inside a task.
- **Any other suffix is refused**, in words: *"off" is not a thinking level. Add `low`, `medium`,
  `high` to a model id, or leave the level off*. It is a different request shape — it asks the
  provider to suppress thinking outright — and some endpoints refuse it. `:max`, `:none`,
  `:xhigh` and the other near-misses are refused the same way. That refusal is about this
  notation alone — the effort ladder has rungs called `xhigh` and `max`, and they are a
  separate thing from a suffix on a class value (see *Making the model think harder, deeper,
  or less*).
- Where a level is set, the role rows print it after the id, `kimi-k3:low`, which is the
  same notation the model picker and `/status` use.

The **mastermind** row is a text box rather than a picker for exactly this reason: a picker
hands back a bare id, and this row's value may be an id with an instruction on it.

One thing about `reflex`: it is the only role called **twice on every message** — once
before, to pick which remembered lines belong in this one, and once after, to decide
whether the exchange held anything worth keeping (what-i-remember). That is why it has a
class of its own rather than sharing "small work", and why it is the one row where a large
model is an expensive mistake rather than a preference. Both calls are folded into the
session's total, not into the message that triggered them, so `/cost` includes them
without any one message reading as three times the price of its neighbours.

One caveat on `vision`: the **looking** row further down the Providers tab is the front door
for which model sees, and it wins over this role's class. The `vision` pin is the second rung
of that ladder — set the looking row for the ordinary case, and pin the role only when
you want a pin that also binds the older surfaces.

## Which model is planning my adaptive run — what `planner:` means on that page

An adaptive run's page pins one line at the top, and it names the model doing the thinking
next to the money it is spending:

```
◐ main ▸ ship the parser fix · planner: kimi-k3 · $0.87 / $100.00 · working
```

`planner: <model>` is the model amending the plan after every node — resolved once when the
run started, from the model whatever started the run named, then the `planner` role's pin,
then the **mastermind** class, then the model you are talking to. Since the mastermind ships
with a model in it, this is usually *not* the model in the rest of this conversation, which
is why the run's own page says it rather than leaving you to work it out. You cannot name it
yourself from a conversation, because a conversation cannot start a run at all — see
*adaptive runs*.

It never changes while a run is going: the ladder is walked once, at the start.

On a narrow screen (**under 60 columns**) the segment comes off that line and is drawn dim
on the first row of the page instead, above the chips. It is moved, not dropped — what the
header sheds first is the goal, which you can still read in the conversation.

The nodes under the planner run on the `worker` role, which sits on the **worker** class —
a different model, and not on this line. `/settings` → Providers lists both.

## Pinning one role to its own model, and unpinning it

In `/settings` → Providers, move onto any row of the roles list and press **enter**. That
opens the model picker — the same one `/model` opens, same filter box, same ranking — and
what you choose is **pinned** to that role alone. The row then reads
`<model>  pinned`, and the legend at the foot offers **del unpin**. Press
**del** on a pinned row to clear it; the role goes back to following its class.

The picker a role opens asks that role's own question. `vision` offers only models that
can see; every other role offers the models you can hold a conversation with.

Every pin lives in the one **pinned roles** row, written as
`planner:openai/gpt-5, worker:openai/gpt-5-mini`. Pinning from the list and typing into
that row are **the same setting** — a pin you typed by hand shows in the list as pinned,
and pinning from the list rewrites the row without disturbing the other pins in it.

**A third door: just ask.** "Use `deepseek/deepseek-v4-pro` for planning and for designing
harnesses" is a sentence aforge acts on — it looks the row up with `settings` and writes it
with `change_setting`, into the same `models.roles` row, after asking you. The five class
rows (`models.tiers.reflex`, `models.tiers.low`, `models.tiers.worker`, `models.tiers.high`,
`models.tiers.mastermind`), the crew word (`models.crew`) and the pins are all writable that
way; only the role **slots** further down the Providers tab are not, because those are
bindings the running session holds rather than values in your profile.

So the ladder for any role, most specific first: **its pin**, then **its class's model**,
then **the model you are talking to**.

Two things worth knowing:

- **A change is live.** The next call aforge makes on its own uses it — whether you changed
  it in the panel, with `/crew`, or by asking. It used to land on the next session, and it no
  longer does. A turn already in flight finishes on what it started with: nothing you change
  lands in the middle of one. The one thing that *can* move a turn mid-flight is aforge
  rescuing it from a model that has stopped answering — see *The model went quiet*.
- Naming a model in the sentence outranks all of it for that piece of work. `make a
  harness for triaging flakes with opus` designs on opus. The roles decide only when you
  named nothing. (There is no such sentence for an **adaptive run**: a conversation cannot
  start one at all — see *adaptive runs* — so a run's models are whatever started it.)

## Does aforge learn which model is good at which kind of work — aforge models, ratings, why a part ran on the careful model by itself

Yes, from the checks it was already running. **Every task that settles is written down**:
the model it ran on, the name the work was given, how it ended in plain words — `landed`,
`not accepted`, `did not finish`, `needs your look`, `stopped` — how many times the work was handed back, what
it cost and how long it took. The check at the end of a task had already read the work and
said whether it holds, so that answer *is* the grade: **nothing extra is spent, and no
second model is asked to judge anything.** Work nobody could check teaches nothing, which
is the honest answer rather than a guess.

`aforge models` in a terminal is where you read it back. It prints a row per model per
kind of work, with the rating, the chance of it holding, and how many settled tasks stand
behind the number — that last column matters, because a rating with two behind it and one
with two hundred are different claims. A row that is not yet driving anything says so at
the end of the line: `under the gate — a part moves up once 2 of this kind have settled`.
Until something has settled at all it says
`nothing measured yet. Ratings appear once calls have been graded.`

**What aforge does with it** is one thing only: when a task splits itself, a part the
worker called ordinary work is minted on your **careful work** model instead if work
named like it has been turned down twice or more on the model the task is on. That is the
whole of it — no model is ever swapped out from under you, your chat model is untouched,
and an install with no crew classes set never lifts anything, because there is nowhere
dearer to lift it to. *Tasks*, under *When a task turns out to be too wide for one
worker*, has the rest.

The record lives with your settings, in `router-ledger.json` and `router-events.jsonl`.
Several aforge windows write to it at once and it is kept across restarts.

## What happens when a crew model is down, or a pinned model stops answering — the ladder falls through one rung

The calls aforge makes on its own — the session's name, the two or three words a task is
called, the judge that reads a turn, the planner sizing a piece of work — used to be
abandoned outright when the model the ladder picked could not answer: a role pinned to a
small model that was down cost you the name and said nothing, while the model you were
talking to sat there able to do it.

Now the call **falls through one rung of the same ladder** and asks again: pin, then the
class's model, then the model you are talking to. That last rung is the floor, and it is a
model that demonstrably works — it is the one answering your own turns. The cost lands
against the model that actually answered, not the one that refused, so `/cost` and the
usage rows reconcile.

**One rung, and then the failure is real.** A ladder walked to the bottom on every errand
would turn one bad minute at a provider into three charges and three waits for an answer
nobody asked for. Nothing is said on screen either way — these are errands you did not ask
for, and there is no state for "a small thing did not work".

**Each of these calls also has its own patience**, taken from its class rather than from a
per-call setting: a reflex call has **45s**, a cheap-class call **2m**, a capable-class one
**5m**, and a mastermind call **10m**. Some calls set something tighter still and keep it —
the guardian answers in ten seconds or not at all. What this replaced was the ordinary
five-to-fifteen-minute bound a completion carries, which is right for your own turn and
absurd for eight words of title.

## Use one model for everything for one run — `--one-model`, and why a run spent money on a model I did not pick

`aforge chat --one-model` and `aforge resume --one-model` run **every text call on the model
you are talking to**, for that session only.

Without it, the calls aforge makes on your behalf go to the crew, which is the point of the
crew — but it means a session started with `--model X` did not spend all of its money on X.
Measured on one trivial task: 22% of the dollars went to a model the run never named. That is
correct behaviour and a surprise to anyone reading a bill, so this is the flag for the case
where **one model has to answer for the whole run** — comparing two models against each
other, timing a benchmark cell, or attributing a cost.

It settles four things on your model: the five crew classes, any role you pinned, the model
that work leaving the conversation runs on, and the fallback chain aforge would otherwise
move to when a model cannot answer. Under this flag **nothing hops** — not on a refusal,
not on a reply that keeps stalling, not on rate limiting that will not clear — because a
run whose cost is being attributed to one model cannot have finished a single reply on
another. That includes the catalog's own guess: with no `fallback models` row written, an
ordinary run falls back to the nearest same-class model, and this flag withholds that too.

**It changes no setting and writes nothing.** Your crew rows and pins are untouched, `/crew`
still says what it said, and the next session without the flag reads them exactly as before.
It is a posture for one run, not an edit.

Two things it deliberately does not do:

- **Drawing, seeing, speaking and filming are untouched.** Those roles need a model that can
  do them — the model you are talking to may be text-only, and pointing `view_image` at it
  would not make the run single-model, it would make it fail.
- **It cannot travel over `--host`.** The session is built on the far machine and that
  machine's rows are the ones answering, so combining them is refused rather than quietly
  ignored: `--one-model settles this machine's model rows; over --host the far machine
  answers them, so the two cannot be combined`.

Standing items never take this posture, whatever the session that set them up was started
with. They fire on their own clock long after your run ended, and the crew answers for them.

## What temperature does aforge use — sampling settings like temperature, top-p and seed

**None of its own.** No call aforge makes sets `temperature`, `top_p`, `top_k`, a seed
or any other sampling knob — the request simply omits them, and the provider's own
default answers. OpenRouter passes an absent sampling parameter through as absent rather
than substituting a value of its own, so what you get is whatever the endpoint's model
ships with.

There is no setting and no flag to change this. If a reply reads as too predictable or
too wild, the dials aforge does have are the model itself and how hard it thinks (the
effort rungs below).

## Reasoning effort — making the model think harder or faster

Reasoning effort is set in the model picker with **ctrl+t**, on the model under the cursor.
Each press walks it round: off → low → medium → high → off.

- On a model whose catalog row does not accept a reasoning knob, ctrl+t does **nothing at
  all, silently** — the level would be a 400 at the next turn. A row that published nothing
  counts as "does not accept", so the knob is missing rather than offered wrongly.
- The level lives on the session, **per model id**. It survives switching away to another
  model and back. `/new` forgets it.
- Where the level is set, it is shown after the id as `<id>:<level>` — in the picker row, and
  on the `model` line of `/status`.

**A level set here wins over everything else that asks for thinking.** It is the most
specific thing anybody said about how hard this model should work, so it beats the
conversation's own rung, a task's rung and the **thinking** default — see *Making the model
think harder, deeper, or less*. The cycle itself is unchanged and still walks
off → low → medium → high → off; it does not offer `xhigh` or `max`.

## Making the model think harder, deeper, or less — the effort ladder from low to max

How hard the model thinks is one dial with five rungs, cheapest first: `low`, `medium`,
`high`, `xhigh`, `max`. There is also **off**, which is the dial left alone — aforge asks
for nothing and the model thinks however it thinks.

**The default is `high`.** It is the **thinking** row in `/settings`, among the model rows
beside the model you talk to, and its choices are `off, low, medium, high, xhigh, max`. The
row is written to the profile as `effort`.
Move it down to make the model think less, which is what gives you faster and cheaper
answers; move it to `xhigh` or `max` when you would rather wait and get the careful one.

Several things can name a rung, and the most specific one wins:

1. **The level dialled onto the model in use** — the model picker's **ctrl+t**, or
   `--reasoning` on the command line. It beats everything under it.
2. **This conversation's own rung.** It is sticky: it is kept in the session's own
   `meta.json`, so it is still there after you close aforge and come back.
3. **The piece of work's own rung** — a task carries one in `tasks.json`, and a standing
   item carries one as its `does.effort`.
4. **What the call is for.** A standing item firing, and the sentinel run that watches for
   it, think at `low`. The errands aforge runs beside your turn — naming a conversation,
   summarising it, judging where a request belongs — ask for nothing at all. Your own turn,
   and the task workers you hand work out to, take the default.
5. **The default** — the **thinking** row, which is `high` until somebody chooses otherwise.

**`ctrl+v` moves the rung of whatever you are standing on.** In the message box it moves
**this conversation's** rung, which has a chip above the box naming it. On a task — the
roster row under the cursor, or the page you are inside — it moves that task's rung. On
home with the cursor on no row at all, it moves the **thinking** row itself, the
machine-wide default. On a standing item's card it moves that item's. The rung climbs one
step each press and wraps from `max` back to `low`; it never goes back to "nobody said".
The **thinking** row in `/settings` stays what it is: the answer for every conversation
that has not been dialled by hand. The keys page has the whole of it — see *The thinking
chip above the message box* and *ctrl+v — how hard the thing you are looking at thinks*.
There is no slash command for it.

## What low, medium, high, xhigh and max actually ask the model for

`low`, `medium` and `high` are the provider's own three words, and they are sent as they
are.

`xhigh` and `max` send **a thinking budget instead of a word** — 32,000 tokens for `xhigh`,
64,000 for `max`. `high` is the top of the word ladder every provider shares, so the two
rungs above it say "more than high" the only way that travels: as a number. The request
carries the word or the number and never both — a request carrying both is refused with
`Only one of "reasoning.effort" and "reasoning.max_tokens" can be specified` — and the
provider translates whichever one it was given for a model that reads the other. An
endpoint that cannot be given a number at all falls back to `high`, which is the honest
answer: it cannot think harder than its own ceiling.

**None of this can fail a turn.**

- A model whose endpoint refuses the thinking budget has the budget dropped and the refusal
  remembered, so it costs one rejected request for that model and never a failed reply.
- A model whose catalog row says it takes no reasoning knob at all is sent nothing about
  thinking.

These rungs are not the same notation as a thinking level written onto a crew class value
(`moonshotai/kimi-k3:high`), which still takes only `low`, `medium` and `high`.

## The model went quiet, or stopped answering halfway through — the request is cut when nothing comes back, and how long it waits first

A request that has been accepted and then produces nothing is cut and sent again. Two
clocks decide, and only the **model writing** moves either of them — a token of answer, a
token of thinking, a piece of a tool call.

| The clock | How long | What it catches |
| --- | --- | --- |
| first word | **1m30s** | accepted the request and never started |
| a gap mid-reply | **45s** on an endpoint aforge has not timed, less on one it has | started writing and stopped |

There is a third clock for the opposite problem — a reply that keeps writing and never
finishes. It is not a fixed number, so it has its own section below: *A reply that never
finished*.

The first bound is generous on purpose: a reasoning model at a long context legitimately
thinks for a minute before its first token, and cutting a request that was about to answer
costs the whole prompt again. The second is shorter because the question is different — a
model that has started writing has finished deciding.

**And the second one gets shorter still on an endpoint aforge has measured.** Forty-five
seconds is what a stranger gets. Once aforge knows how fast an endpoint writes — the
`t/s` figure the status line shows you — the gap it will sit through is how long *that*
endpoint would take to write about three and a half thousand tokens: roughly **15 seconds**
on one sustaining 250 tokens a second, **42** on one sustaining 83. A minute of silence from
an endpoint that has been writing two hundred and fifty words a second is not patience, it
is a dead stream, and waiting it out costs the same minute on every retry. It never goes
the other way: a slow endpoint gets the full 45 seconds and no more is ever granted.

**Keepalives buy patience, never progress.** Some endpoints assemble a whole answer — most
often one large tool call — on their own side and deliver it in one piece, sending
keepalive comments the entire time. That quiet is not a dead connection, so while
keepalives are still arriving the wait is extended, up to a hard cap of **2m30s** of total
quiet. Past the cap the request is cut whatever the endpoint is saying, because an
endpoint that speaks forever and answers never has failed too — the error then honestly
names the longer wait, `went quiet for 2m30s`. An endpoint that delivers its answers this
way is also remembered as a slow one and sorted behind the endpoints that stream, so it
stops being the first pick for the next request.

A cut request is asked again **twice**. When the router named the endpoint that went
quiet, that endpoint is avoided on the retry so another endpoint serving the same model
can answer. The screen says `trying again · 12s` while it is (see *What is on the
screen*), and a dim line lands saying `nothing came back from the model — asking again`
or `the model went quiet mid-reply — asking again`.

**If all three attempts come back with nothing, aforge finishes the reply on another
model** — the next one in your `fallback models` row, or the nearest same-class model in
the catalog when you have written no row. It is said out loud before it happens, naming
where the rest of the answer is coming from:

```
the model kept going quiet mid-reply — finishing this one on openai/gpt-5-mini
```

The turn finishes there and the cost lands against the model that actually answered. **It
is a rescue, not a choice you made**: your model is untouched, `/status` still shows it,
and your next message goes back to it. If it keeps stalling, `/model` is how you move for
good.

Only when there is nowhere to go — you are on `--one-model`, or no chain resolves — does
the turn end instead:

```
error: nothing came back from the model in 1m30s, three times. a different model may answer — /model, or set models.fallbacks so this can move on its own
```

And when the fallbacks could not finish it either, the sentence says so rather than
repeating advice already taken:

```
error: nothing came back from the model in 1m30s, three times. openai/gpt-5-mini and anthropic/claude-sonnet-4 could not finish it either — /model to pick another one yourself
```

These retries are **their own budget**. A request nobody answered is not evidence that the
endpoint is failing, so it does not spend the three retries a real provider error gets.

**Sometimes it moves after two attempts instead of three.** Three attempts are worth
making only when they can reach *different* endpoints. If the stream died before naming
which endpoint served it, or you have set `routing` to `off` on the **Providers** tab, then
nothing is being routed around and the next attempt lands in exactly the same place — so
aforge stops asking and moves to the next model a try earlier. Setting `routing` to `off`
switches off **endpoint** steering; it does not switch off moving to another model.

## A reply that never finished — the turn ran for half an hour, aforge looked frozen, nothing happened for ages, the model kept writing and never stopped

The two clocks above are both about **silence**. A reply that keeps producing a token every
few seconds resets both of them forever, and for a long time nothing in aforge ended a
request like that: a turn could sit there for half an hour with the reply still technically
arriving, and the session log recorded nothing at all while it did.

So every request also carries a **wall** — the longest it may run before it is cut, whether
or not it is still writing.

**The wall is not a fixed number.** It is worked out from what that endpoint has actually
done for you: **five times the longest reply it has finished** in this session, never less
than **2m30s** and never more than **20 minutes**. Two endpoints serving the same model
therefore get two different walls, and one that routinely writes long answers earns a
longer one by writing them.

**A model aforge has not spoken to yet gets 5 minutes**, because there is nothing measured
to work from. That figure used to be the floor under *everybody*, which meant the
measurement could never make anything shorter than what a stranger got: an endpoint whose
longest finished reply was twenty-four seconds still sat there for five whole minutes, and
two hung streams in one measured run did exactly that. It is the outer bound for a stranger
now, and an endpoint you have timed is held to its own history instead. The numbers are
forgotten when aforge closes, so a fresh session starts from the 5-minute bound again.

The lower clamp is 2m30s and not less, because that is the longest an endpoint is allowed to
go quiet while assembling an answer on its own side (above). A wall shorter than that would
cut a reply the silence clocks were still being patient with.

When a reply hits the wall it is cut and asked again exactly like a reply that went quiet —
the endpoint is avoided on the retry, and a dim line lands:

```
the reply kept going and never finished — asking again
```

and if it keeps happening, the turn moves to your next fallback model:

```
the reply kept running on without finishing — finishing this one on openai/gpt-5-mini
```

with the same ending when there is nowhere to move:

```
error: the reply ran past 15m0s without finishing and was cut, three times. a different model may answer — /model, or set models.fallbacks so this can move on its own
```

**Nothing you can set changes the wall.** It has no settings row, because a number you had
to pick would be a number nobody could pick correctly — that is the whole reason it is
measured instead.

**A reply that is not streamed is held to the same wall once it has been measured.** The
calls that arrive whole rather than token by token — the headless run's planner and its
workers, `aforge do` — carry a total deadline sized from the room the reply was given (one
second for every 64 tokens it may write, never less than 5 minutes and never more than 15).
Once that endpoint has finished a reply for you, the measured wall applies to those calls
too, and whichever of the two is shorter is the one that cuts. A model aforge has not heard
back from yet keeps the room-sized deadline, because a first reply from a model that thinks
at length may need all of it.

**A cut reply is thrown away whole**, like every other cut: none of the text reaches the
conversation, and the retry starts the reply from the beginning.

## I keep getting rate limited — 429, "too many requests", the provider telling aforge to slow down

A provider that answers `429` is pacing aforge, not failing. That is not an error, so the
call waits and comes back rather than giving up: up to **six attempts** or **two minutes**,
whichever runs out first, for a turn you are sitting in front of. Work that left the
conversation gets far more — see *How a task actually runs*.

Two things happen while it waits. If the refusal names *which* endpoint hit its limit —
routers often do, when the limit is one provider's shared pool rather than your account —
that endpoint is avoided on every request after it, so the next attempt queues somewhere
else. And the wait itself is capped at a minute however long the provider asked for, so a
provider naming tomorrow morning does not park your turn.

**When that patience runs out, aforge tries the next model in your `fallback models` row**
rather than handing you the refusal. It says so on the same line a refusal uses:

```
Retry 1/1: Falling back to openai/gpt-5-mini
```

With no chain to move to, you get the provider's own words and the status, which is what
this did before:

```
error: after 6 attempts: API error (429): rate limit exceeded
```

This only covers *pacing*. A server fault — a `500`, a `503`, a torn connection — keeps the
short patience it always had and never moves your model: a broken endpoint is not a claim
that the model cannot answer.

## "Provider returned error" — a 400, what the error actually was, and why my reply just stopped

`Provider returned error` is your router saying that **somebody else refused** — it handed
your request to one endpoint, that endpoint said no, and the router is passing the refusal
along. On its own it explains nothing, so aforge now shows what came with it: the name of
the endpoint that refused, and the first sentence of what *it* said.

```
error: API error (400): Provider returned error (via Baidu: input length 97445 exceeds the maximum this endpoint accepts)
```

**An endpoint that refuses is routed around.** Its name goes on the same five-minute
refusal list a rate-limited or silent endpoint earns, so the next attempt is sent to a
different machine serving the same model. Before this, three attempts in a row could be
three deliveries of the same request to the same endpoint — a measured run lost an evening
to exactly that.

**And a refusal that names no endpoint is not retried at all.** If the router refused on
its own account, it read the request aforge built and said no to it — every endpoint alive
would say the same thing, so asking again at 2s, 4s and 8s only spends the time to be told
three times. The turn ends immediately with the refusal instead. That is the whole rule:
**named an endpoint → try another one; named nobody → stop**. It is not a list of status
codes, so it works the same on a `400`, a `403` or anything else a router invents.

**Where to read it afterwards.** Every failed request now writes a line into the session
file — the model, the endpoint, the status, the endpoint's name, its own words, which
attempt it was and how big the request was. Nothing like this was written down before, so a
turn that died left the file saying only that it had ended.

## My reply stopped and nothing was retried — a reply that broke is not carried on, and an empty reply is asked again

A reply that ends **because a call failed** is not a reply that stopped early, and aforge no
longer treats it as one. Two endings count as broken: the provider said it stopped on an
error, and a reply that came back completely empty — no words, no tool call, nothing
counted.

Neither is read by the second reader that carries a reply on (see *How tasks and adaptive
runs work*), because there is nothing left to carry on *to*: the failure is what is left,
and the retry above already owns it. A measured run read a broken reply three times in
fifteen seconds, paid a thinking-tier model each time, and re-opened a reply that could not
move. An empty reply is also written down as a **failed request** rather than as an empty
answer from the model, so what is on the file matches what happened.

**An empty reply is asked again, straight away, and your turn carries on.** An endpoint that
answers with nothing did not answer, so aforge sends the same request again — up to four
tries in all — and there is no pause between them: the endpoint is up and fast and simply
broken, and waiting eight seconds gets you the same nothing. What helps is being served by a
different machine, which is what the next try asks for. Only when all four come back empty
does the turn end. Before this, one empty reply ended a whole turn, and a measured run
stopped eighteen minutes in with hours of budget unspent.

## Why did my task not move to a stronger model — trouble with the connection never buys a dearer model

Three different things used to look the same to aforge: **the connection** failed, **the
model** was not good enough, or **the work** could not be done. Only the middle one is worth
paying more for, and aforge now tells them apart before it spends anything.

- **The connection.** Nobody answered, an endpoint refused, a reply came back empty, or a
  tool call arrived mangled. aforge asks again on the *same* model and lets the router send
  it somewhere else. It never ends your turn and it never buys a dearer model — nothing
  about *who served* a request says anything about *who was asked*.
- **The model.** The check read the finished work and said something was missing, and the
  run it read had no connection trouble under it. That, and only that, sends the work back
  on the careful class.
- **The work.** The job could not be done, or the request itself is what your router is
  refusing. There is nothing to buy; you get the report.

Three runs of a measured comparison read the first as the second — four bad responses in a
row, and the task moved onto a model seven times the price for the rest of the run — and
that was between 57% and 82% of each bill. The run that never rolled four bad responses in a
row cost a fifth as much.

**And a dearer model is given back.** When the next check passes, the work goes back to the
model it started on, so one bad minute at a provider cannot become the price of the whole
job. There is also a ceiling of **$2** on what the careful class may spend on one task; past
it the task comes back to you with its report instead of buying another round.

**Where to read it afterwards.** Every one of these decisions writes a line into the session
file saying which of the three it was and what aforge did about it, beside the failed
request it was made about.

## The model was printing garbage — a reply that repeats itself, started repeating the same line over and over, or comes back as gibberish

A model can lose the thread and stop writing language: one line or one letter repeated
until the token budget is gone, or words with two and three alphabets inside them. It
happens most at long contexts, and it feeds itself — a bad reply goes back into the
conversation, and the model reads its own nonsense before writing the next one.

So aforge watches the reply as it arrives and cuts it where it went wrong. **None of that
text is kept**: it is not in the conversation, not in the session file, not sent back to
the model, and it comes off your screen. A dim line says so —
`the reply lost its thread — that text was dropped, asking again` — and the same question
is asked **once** more. If the second reply comes apart too, aforge finishes it on the next
model in your `fallback models` row, saying so first:

```
the reply kept losing its thread — finishing this one on openai/gpt-5-mini
```

With nowhere to go, the turn ends in the sentence that says what to do about it instead:

```
error: the reply lost its thread twice — it came back as repetition and jumbled text, so none of it was kept. a different model may hold it (/model), or /compact to lighten the conversation
```

Both doors are real. A different model is different weights on the same conversation;
`/compact` is the same weights on a shorter one, and length is the condition this happens
in — which is why `/compact` is still worth doing even after a fallback model has rescued
the turn.

**How soon.** The reply is read at two lengths as it arrives. One token or one line over
and over — the shape a screen full of `</think></think></think>` is — is cut after about
a kilobyte of it, a second or two at the rates these endpoints write; a whole paragraph
repeated needs about four kilobytes before the repetition is plain. Before 2026-09-02
everything waited for the four, which was long enough for a person to give up and stop
the turn by hand first.

**The thinking is watched too.** A model that loops one glyph inside its thinking pass
shows you nothing while it runs to the output ceiling and bills every token of it. That
is cut the same way, with the same line, and nothing of it is kept.

**If you stop it yourself.** Press esc while a reply is coming apart and the text you
watched stays on the screen, but it does not go into the conversation: a dim line says
`the reply you stopped had lost its thread — that text was not kept`, and the next thing
you ask is answered as if it had never been written. A reply you stopped that was still
language is kept as it always was, up to the word it stopped on.

**What it will not cut.** Fenced code blocks are not judged, so a page of zeros, a long
test log, a generated table or a big JSON dump is safe however repetitive it is — up to
sixty-four kilobytes of one block. A fence that opens and never closes past that point is
read like everything else, because a code block that long with no end is a model that
opened one and then came apart inside it. Neither is a reply that is simply multilingual
cut: switching language between words is ordinary writing, and only switching *inside*
words counts. A short repetitive answer is never cut either — there has to be about a
kilobyte of it.

**The endpoint that served it loses standing.** A reply that had to be cut — because it lost
its thread, because it came back as tool markup, or because it went quiet and never came
back — is recorded against the endpoint that served it as an answer aforge could not use,
and that endpoint drops down the order for the requests that follow. So does an answer that
came back with nothing in it at all. Its row in the lane fold then reads `bad replies` (see
*What the note on a lane row means*). It is not a ban: the mark fades on its own over about
an hour, and every usable answer it serves afterwards walks it back up. Recovery is by
serving properly, which is the only evidence there could be.

**Turning it off.** The row is `reply guard` on the **Providers** tab of `/settings`, `on`
or `off`, and the default is **on**. Off means you see whatever arrives, and keep whatever
you stop. You can also just
ask aforge to turn it off; it is not one of the rows it refuses. The two clocks in the
section above have no switch — a request that produced nothing at all has failed by any
reading. Setting `routing` to `off` on the same tab stops aforge steering between endpoints
at all, and with it stops any of this being recorded.

## I stopped a reply and the text is gone — where the reply went after I hit esc, and why pressing escape on a broken reply deletes it

Press `esc` on a reply that has come apart — one line or one letter repeating, words with
two alphabets inside them — and the text you were watching **is not kept**. It stays on the
screen where you saw it, and nothing more happens to it: it is not in the conversation, not
in the session file, and it is never sent back to the model. A dim line says so:

```
the reply you stopped had lost its thread — that text was not kept
```

The next thing you ask is answered as though those words had never been written. That is
the point of dropping them. A reply that has lost the thread goes back into the
conversation as the model's own last words, and the model reads its own nonsense before
writing anything else — one bad minute from a provider becoming a bad afternoon for the
conversation. Stopping it by hand used to hand you the mess as your own kept reply.

**A reply you stopped that was still language is kept**, up to the word it stopped on,
exactly as it always was. The judgement is the same one aforge makes on its own while a
reply arrives (*The model was printing garbage* above), so only the text that had actually
stopped being language is dropped — and the line above is the only time you are told, which
is how you can tell the two apart.

**It does not happen at all with the guard off.** `reply guard` on the **Providers** tab of
`/settings` is `on` by default; set it to `off` and you see, and keep, whatever arrives —
including whatever you stop.

Nothing here is recoverable. If the half-written reply was worth having, copy it off the
screen before you ask the next thing.

## Strange tags instead of an answer — the reply was tool markup, odd tokens like `<|...|>` on the screen

Some providers serve a model without translating its private tool-calling syntax, and the
model — asked to use a tool — writes the call as visible text: angle brackets, bars, a tool
name, a run of JSON, and no answer anywhere in it. The reply is well-formed as far as the
connection can tell, so without its own guard aforge would show it to you and keep going.

aforge reads the finished reply's shape — mostly symbols, a tool it was actually offered
spelled inside the markup, and no real tool call attached — and cuts it. **None of the
markup is kept**: not in the conversation, not sent back to the model. A dim line says

```
the model answered in its own internal markup instead of words — that text was dropped, asking again
```

and the same question is asked **once** more. The provider that served the markup is set
aside first, so the retry genuinely lands somewhere else. If the markup keeps coming, the
turn moves to the next model in your `fallback models` row, saying so:

```
the model kept answering in its own internal markup — finishing this one on openai/gpt-5-mini
```

With nowhere left to go, the turn ends in
`the reply was the model's own internal markup instead of an answer and was cut`, and
`/model` is the door — a different model, or the same model once its provider recovers.

**What it will not cut.** A reply with a fenced code block in it is never judged — asking
what a tool call looks like gets you an honest answer full of exactly this syntax. Prose
that merely names a tool is safe: the markup has to be the substance of the reply, not a
word in a sentence. And a conversation with no tools available cannot trigger it at all.

**The switch is the same one.** `reply guard` on the **Providers** tab of `/settings`
turns this off together with the repetition guard above; off means you see whatever
arrives.

## What has this conversation cost me — /cost, how much this chat has cost, and why the same conversation suddenly costs more

`/cost` (also `/usage`, `/tokens`, `/spend`) prints what this conversation has spent, and on
what, into the conversation. Up to eight aligned lines:

| Line | What it is |
|---|---|
| `spend` | the money, printed only when it is above zero — this conversation **and every task it started** |
| `conversation` | what the conversation's own calls cost |
| `tasks` | what the work it started has cost, running or finished — tasks, the hands a reply forked, and the nodes of an adaptive run |
| `tokens` | `48.1k in · 3.2k out`, or one half alone, or the combined figure |
| `cache` | `31.2k read · saved $0.0180` — the money half only when a price pair was published |
| `model calls` | **requests to the provider**, deliberately not "turns" |
| `empty reflex answers` | paid memory-routing or extraction requests that reached their output ceiling without returning any answer |
| `time` | how long |

`conversation` and `tasks` are dropped together unless the work has spent something, so a
conversation that has started no tasks prints `spend` alone. When they are there they add
up to the line above them, always — that is the whole point of printing them.

## Does the status line's money include what my tasks are spending, or what its hands are spending — yes, live

**The `$` on the status line is the whole tree: this conversation and every task it
started, at every depth, while they are still running.** It is one figure, not two, and it
is the same figure `/cost` leads with.

**Hands and adaptive runs are in it too.** A reply that splits itself into hands, and a
node of an adaptive run, are both work this conversation started: their money is on the
row while they are still working, under `tasks` when you ask `/cost` for the halves.

It used to be the conversation's own half alone. A task's money only reaches the
conversation's books when the task **closes**, so a family working for two hours left the
row saying `$2.53` while $51.05 was being spent under it, and the true figure could only be
found by widening the task column and reading the parent row.

Where the figure comes from: every model call writes one line into the spending ledger
where the call was made, and a task's line names the conversation the work belongs to. The
status line adds that up on the same clock the rest of the telemetry moves on — so a
number is on the row within a second or two of being spent, not at the end of the task.
Nothing is counted twice: a task finishing, a hand coming home, a run's node ending — each
moves its tally into the conversation's books and writes **no** new ledger line.

Two things follow that are worth knowing:

- **It never goes backwards.** If the conversation's own books hold more than the ledger
  can account for — an old session resumed, a ledger that was moved — the larger figure is
  the one shown.
- **The warm colour follows the figure you can see.** The `$` leaves the dim at four fifths
  of this conversation's own limit, measured on the whole tree. The refusal itself still
  reads the conversation's books, which each task's tally lands in as it closes.

To see the halves, ask `/cost`. To see one task's own bill, open its card.

## Why does the row show the wrong cost after I switch conversations — the money jumped when I switched chats, the figure came from the chat I left

**Switching hands the row over completely.** `/resume`, a row of the welcome list, taking
over a conversation another window was holding — every one of those doors zeroes the
money, the tokens, the cache and the context meter, because each of those is a fact about
one conversation, and then reads the arriving conversation's own bill out of its
transcript and its work's bill out of the spending ledger. Both readings are taken before
the first frame is drawn, so the row is right immediately rather than once the task column
has come up.

There was a spell where the tree's half did not do this: the conversation you left kept
its figure on the row of the conversation you arrived in, until something happened to
recompute it — typing `/cost` was usually what did, which is why the note and the row
could disagree for a moment. They cannot: the row and `/cost` read one figure through one
function, and if you ever see them differ, that is a bug worth reporting.

## Why the same conversation can suddenly cost more — one turn can make several model calls

`model calls` is the line people misread. One thing you type can become several requests to
the provider — every step of a turn is its own request — so this figure is normally larger
than the number of times you have spoken.

It counts **every** request, not only the ones in your turns: naming the session, a judge
deciding where something should be routed, looking at a picture, every request a task's
own agent made on its own lane, and every request a harness run made while it walked its
program. That is deliberate, because the `spend` line above it is the
sum over exactly those requests — a smaller count beside it would be a bill divided by the
wrong number.

`empty reflex answers` appears only when that failure happened. Those requests remain in
the token, call and spend totals because the provider billed them; the separate count says
that the money bought no memory decision. aforge retries one such answer with more room,
then uses the configured low-tier model for the rest of this session if it is still empty.

**Every line is dropped when its figure is absent.** A provider that publishes no cache
accounting says nothing about caches, rather than teaching you that your cache never hits.

It will not go silent. A session with no figures at all answers exactly:

```
nothing spent yet — this session has not sent a turn.
```

## Does /cost remember after I close and resume — is the spend kept across restarts

Yes. The figures are the **whole conversation's**, not this sitting's.

Every turn that spent something, and every call made beside a turn, appends a line to the
conversation's own transcript recording what it cost. When you resume, those lines are read
back and added up before anything else happens, and that sum *is* the session's totals. So
`/cost`, `/status` and the status line the day after show yesterday's money and tokens with
today's added on top, in one figure.

**The status line has it on the first frame** — you do not have to type `/cost` or send a
turn to make it appear, and the same is true of the phone status deck and the full status
sheet. It is the same figure on all of them, because they all read the one total.

Two things follow from that:

- A conversation whose transcript has no such lines yet — one written by an older build, or
  one that has genuinely never spent anything — reports only what has happened **since you
  reopened it**. There is nothing to rebuild from, and aforge does not invent a figure.
- `/new` starts a fresh conversation with a fresh file, so it starts at nothing. Resuming an
  old conversation is the opposite: it picks the old bill back up.

The `model calls` count is rebuilt the same way, so a resumed conversation's call count also
covers the requests made before the restart.

## Is there a record of what I spent across all my conversations, by day or by model

Yes — a file on disk, and **the spend place reads it**. Press `alt+5`, or `tab` to it from any
other place, and it draws that file: which days, which models, and what the money was for.

Every cost line written into a conversation's transcript is also appended to one file for the
whole machine, `~/.aforge/v3/usage.jsonl`, moved by `AFORGE_HOME` like everything else aforge
keeps. **One line per model call, written the moment that call's bill comes back** — the
requests of a turn, and the calls made beside a turn such as naming a session or judging a
route. Each line carries `calls: 1`, so a turn that used three tools is four lines rather than
one.

**That is why the figure moves while a reply is still being written.** The line is written as
each call is paid for rather than when the turn finishes, so a turn you interrupt, stop, or are
simply still watching has every call it has made so far on the file already. It used to be
written once at the end of a turn, which meant an interrupted turn's money reached the status
line and never reached this file, and the two disagreed about the turn in front of you.
Older lines, written before that changed, may carry a whole turn's worth on one row with a
larger `calls` figure; they add up the same.

Every line records: when it happened and which local calendar day that was, which model answered,
how many requests and how many tokens in and out, what it cost, the conversation it was made
in, the piece of work or the standing promise it was made for, and the project directory it
ran against.

Four things are worth knowing about it:

- **The figures are the bill, not an estimate.** Until this file existed, "what did opus cost
  me this month" could only be answered by opening every transcript on the machine, and "what
  did I spend on Tuesday" could not be answered at all — a conversation's own total has no day
  in it.
- **A call that cost nothing writes no line.** So a day with no lines is a day
  nothing was spent, rather than a day of zeroes. The place obeys the same law and draws
  nothing for an unpriced call rather than calling it free.
- **A record that could not be written is counted and said.** Writing this file never makes a
  reply wait: if the disk stops answering, the row is dropped rather than the turn. When that
  happens the spend place's top line and the Spending tab both grow a reading — `3 spending
  records could not be written` — so a figure that is short says so instead of quietly reading
  as a cheaper day. Nothing is drawn when nothing was lost, which is nearly always.
- **Work is counted once.** A task's own requests are recorded where they were made. Its total
  is added to the conversation that started it afterwards, and that addition is deliberately
  not written here, or the same money would be counted twice. **That holds for every kind of
  work, not only tasks** — the hands a reply forks and the nodes of an adaptive run each
  record their own requests and are added up afterwards the same way. Until this was fixed
  both were on this file twice, so a day that included a fork or a run read high, and the
  daily limit was reached before that much had actually been spent.

`/cost` and the status line are **this conversation's** own running total, kept by the same
step that writes the line above — so the two cannot drift apart. The spend place is the whole
machine; `/cost` is this conversation. They answer two different questions and neither is a
correction of the other.

## The spend place — what days and models cost, and what the money was for

`alt+5` opens it. It reads the machine-wide ledger above when you walk in and again on the
same three-second beat every place runs on, and it draws three things:

- **the window and its total** — `$34.10 · 41.2M tokens` on the left of the head row and the
  window itself at the right, as the control `shift+← aug 12 – aug 25 →` with `shift+↑
  coarser` beside it — then a sparkline under it, one cell per day, and today's figure at the
  right. The span is spelled once, between the arrows;
- **what ran it**, by the model and **the role it is bound to**, dearest first, each row with
  a bar, its call count and its tokens. The role is the **crew binding** — `execution`,
  `conversation`, `verification`, `naming`, `planning` — read from the settings as they
  stand right now, and never the auxiliary word one call gave itself. That is the point of
  the column: seeing that execution is most of the bill sends you to the one row that
  changes it. A model that is on the bill and is bound to nothing today draws **no role word
  at all**, and a model bound to two slots says both. A model is drawn by the word you say
  out loud — `claude-opus-4-1`, not `anthropic/claude-opus-4-1` — which is the spelling
  `/model`, the crew chips and the status line all use;
- **a role slot with nothing bound to it** gets a row of its own under the models —
  `planning · unbound · follows execution` — because "planning costs nothing" and "nothing
  is bound to planning" are opposite facts about the same blank. There is no figure on that
  row: no line in the ledger names a slot, so there is nothing measured to put there.
  **Today only the conversation slot is drawn at all.** A window holds a client for the
  model you are talking to and for no other; the five crew slots are answered where their
  own session is opened, so this window cannot tell "nothing is bound" from "I cannot ask" —
  and the emptiness law says an unknown is drawn as nothing rather than guessed at;
- **what it was for** — the three things money is ever spent on, because the ledger holds
  three ids: a piece of work, a standing promise, or a conversation. The dearest three are
  shown and the rest fold into one line. Work with **no id of its own** — the hands a reply
  forks, the check that reads what a piece of work left — is on the row of the conversation
  it belongs to, because that is the only name it has.

The ledger holds **ids and no titles**, so the place joins each id against the records it is
already reading — the project's own index of what it ran, and the standing store — to put a
name on the row. A thing neither of them knows keeps its id.

**`enter` on a row of "what it was for" opens what it was for**: a task goes to the tasks
place, a standing promise to the standing place, a conversation to home.

**There is no budget editor here and there will not be one.** The page answers *what did it
cost*; *what may it spend* is the Spending tab, and this page **points** at it rather than
holding a second copy of it. The first line of the page is that pointer, dim:

```
today $3.42 of $500 · /budget sets the limits
```

`enter` on that line opens the Spending tab, and so does `→` on any row of the page followed
by `b` — the verb strip that opens there is one letter, `b the limits`. A cap on **one task**
is a different figure and is set where that task is started — on the composer layer's third
line, before you send it (the tasks page, *a task started from the composer carries a cap*).

The pointer keeps the emptiness law on both halves: a day with nothing on it says nothing
about today, and a machine with no daily limit reads `today $3.42 · no limit` rather than
drawing a fraction with nothing under the line.

**An empty window is not an empty machine.** Until the ledger has a priced line at all, the
place says what it is for and nothing else, which is what every place with nothing to draw
does. Paged onto a fortnight nothing was spent in, it keeps its head row — `nothing spent`
on the left and the control on the right — because that control is the only thing on the
frame naming the window the arrows move.

## Moving the spend window — the time keys

Time is two questions, so it gets two arrow axes and no letters:

| | |
| --- | --- |
| `shift+←` `shift+→` | move the window **by its own length** — one press is the previous or next fortnight, not the previous day |
| `shift+↑` | coarser — a fortnight of days becomes a fortnight of weeks, then of months |
| `shift+↓` | finer, the exact inverse |

The window opens on **the last 14 days, by the day**. The label between the arrows is the
reading and the control at once, and the same head row is drawn on the tasks place and the
standing place. A terminal too narrow to draw the control has no window there at all — the
keys do nothing rather than moving something nothing on screen reports — and the zoom keys
are bound only where `shift+↑ coarser` fits beside the arrows. A week buckets from Monday; there is no year rung, because a
window of years is a question about a machine older than this program.

Moving the window costs nothing on disk: the lines are already in memory, so a fortnight back
is the same reading answering a different question.

## Everything at once — /status

`/status` (also `/info`, `/context`) prints **every fact the status line can carry**, one per
line, into the conversation. It is not a panel. Usage totals are refreshed first, so a
command typed between turns answers from what the session holds now.

The labels come in this order, each dropped when its value is empty: `session`, `task` (only
inside a task room), `model` (the full routing address, with `:level` when a reasoning level
is set), `crew`, `task model` (only in a room), `served`, then the telemetry segments under
their own words — `background`, `changes`, `spend`, `context`, `cache`, `rate`,
`compaction`, `approvals`, `connection`, `state` — then `tasks`, `place`, `keys`, and
finally `file`.
Labels are padded into two aligned columns.

The `crew` line sits directly under `model` and reads the preset word — or `custom` — with
the three classes after it:

```
crew     max · brain kimi-k3:high · hands glm-5.3 · checks kimi-k3
```

On the live status line the crew is one short segment — `crew max`, or `crew custom` — at
the head of the telemetry beside the model, and among the first a narrow row gives up; the
`crew` line here and on the phone's status sheet is the full reading. A **remote** session
opened with `--host` has no crew of its own to read — it is the other machine's — and gets
no `crew` line or segment at all.

Two things differ deliberately from the status line on screen:

- the session **file** is added, because a path is a thing you copy into another program;
- the `spend` line is **dropped** when nothing has been spent. The live status line keeps
  `$0.00`; a note printed into the conversation must not.

Over `--host`, the `place` and `file` values are written in full as `machine:/path`.
After the first connection measurement answers, `connection` is a sentence such as
`the round trip to devbox is about 3ms`; while redialling it is the reconnecting sentence
instead. With no measurement there is no connection line, never `0ms`.

## How much room the conversation has, and giving it a longer context window

The context window is, most specific first: the window the session's own model catalog
publishes for the model actually in use, then the window the session was configured with, then a conservative default of
**128,000 tokens** (the smallest window among the models this surface routes to).

Compaction is triggered at **`window − max(15% of window, 16384)`**, with that reserve
clamped to at most half the window. When a pass runs, the **verbatim tail** it keeps is
**20,000 tokens**, capped at a quarter of the window. A pass folds down to a target **half
a reserve below the trigger** — about 99,200 tokens on the default window against a trigger
of 108,800 — so the next few steps of growth do not start another pass; the page on
compacting over and over explains why that headroom exists.

The current size is the larger of two figures: the context size the provider last reported,
and an estimate of the transcript at 4 bytes per token. That way a 300KB tool result appended
since the last response is already visible to the threshold.

An unknown window has no threshold at all.

## The most tokens one request can carry — the model's own window, and the ceiling an endpoint puts on it

**The threshold follows the model's own window.** On a model claiming 1,310,720 tokens,
compaction fires at **1,114,112** — not at some smaller figure of aforge's choosing. On the
default 128,000-token window it fires at 108,800. The line is always
`window − max(15% of window, 16384)`, and `window` is what the model card says.

There used to be a flat ceiling of 256,000 over every model alike, and it made a
million-token model fold exactly like a small one — nineteen passes in one two-and-a-half
hour run, each at around a hundred thousand tokens, each one throwing the provider's prompt
cache away. That ceiling is gone.

**What can still lower it is an endpoint refusing.** If a provider answers that a request
would not fit, aforge writes down how big that request was and never trusts that model past
that size again — in this conversation from the next check onward, and on this machine for
good, because the note is kept in `model-quirks.json` beside your other settings. That is
the one thing allowed to contradict a model card, and it is the only thing: a published
window is a claim, and a refusal is a measurement.

It exists because a claim can be very wrong. A session on
`~deepseek/deepseek-v4-flash-latest` — a row claiming 1.3M tokens — grew to 386,309 tokens
without compaction firing once, and what came back at that size was the model's own template
turned inside out rather than an answer. That now costs one turn on that model on this
machine, instead of costing every model with real room every turn for ever.

What the status line reports is still the model's own window, because that line is
describing the model.

**A request that would not fit is never sent.** Immediately before each request goes out,
a transcript already past the trusted window is compacted first — and unlike the ordinary
pass, this one runs **even when automatic compaction is switched off**. Fitting is not a
preference. Nothing is truncated and nothing of yours is dropped; it is the same pass
`/compact` runs, and every message you typed survives it.

**Accuracy note.** aforge also carries a shared context-budget package with a 60%-fill rule,
a 160k working set and a 250% reuse law. **That package is not used by this chat.** Its
consumer is the sub-harness leaf sizing elsewhere in aforge. The chat's own law is the one
above — do not describe this conversation as filling to 60%.

## A task or a worker on another model gets that model's window

Work that leaves the conversation — a task's worker, an adaptive run's worker, a fork, the
reader that checks a task — often runs on a different model from the one you are talking
to. Each of those asks the same model catalog the conversation asks, for **its own** model,
so a worker on a million-token model folds at a million-token model's line.

Before this it was handed nothing at all whenever its model differed from yours, and so
fell back to the conservative 128,000-token default however much room its model really had.
That was the whole of why a long run could fold its work again and again while every model
in it advertised ten times the space.

When nothing can say — a model the catalog has never carried, a machine that has not
reached the catalog yet — the answer is still the 128,000-token default, which is the
smallest window this surface routes to and the safe direction for a guess to be wrong in.

## What happens before the conversation is summarized

aforge does not jump straight to summarizing. There are rungs before it.

**During one long turn, tool output has its own working-set bound.** Once the live request
estimate crosses **64,000 tokens** — or half the trusted context window when that is smaller
— aforge replaces already-seen tool results from that turn with the same readable pointer
lines described below. It works in whole tool batches, oldest first, while leaving the
latest **20,000 tokens** verbatim (capped at a quarter of a smaller window). The result from
the batch that just ran is never folded before the model has seen it, and neither your
message nor any assistant text is folded.

The pass aims below the trigger, at the midpoint between the kept tail and the working-set
line. That headroom matters because rewriting a result makes the provider's cached prefix
cold from that point; one deeper pass is cheaper than another rewrite every round. When it
runs, the transcript gets one line such as `[folded 8 results · ~24k tokens]`. The full
result bytes remain in `logs/stubs/` (or the store), the model can `read` the path in each
stub, and the session journal keeps the original result bytes.

**Rung 1 — stubbing.** At the end of every completed turn, tool results older than the last
**4 turns** and larger than **1500 bytes** are replaced *in the live context* by a pointer
line naming the tool, its first line, its size and where the whole of it lives:

```
[tool: bash · go build ./... — 0 exit · 41208 bytes · full: ~/.aforge/v3/projects/-you-work/<session>/logs/stubs/<hash>.txt]
```

The bytes are written to disk first, named by their own digest — or the pointer is the id of
the result already posted to the store — and the model can `read` them back at any time.
**They are written in this conversation's own folder, under `logs/stubs/`, and never in your
project**: a stubbed result is the harness's own droppings, not your work. That holds for a
task's worker too, however long the files it reads — its stubs are filed with the
conversation that sent it out, not in the checkout it is working in. Only a conversation
with no folder at all falls back to `<workspace>/.aforge-v3/stubs/`.
**The journal is never stubbed** — the record on disk keeps the whole result. An interrupted
or failed turn is left alone, and a session with no workspace does nothing here.

**A pass that would not pay for itself does not run.** Replacing a result part-way down the
conversation makes every byte behind it new again as far as the model's provider is
concerned, and new bytes cost about five times cached ones. So a pass only goes ahead when
what it reclaims is at least an **eighth** of what it would put back on the meter — otherwise
it leaves everything alone and looks again at the end of the next turn, by which time the
same results are usually part of a batch worth doing. This is why one middling tool result
sitting in a long conversation can stay whole for several turns and then vanish all at once
alongside others.

**What changes while you work is kept at the back, for the same arithmetic.** The two short
notes aforge keeps in front of the model that move as the work moves — `<state>`, what this
conversation is doing, and `<elsewhere>`, what other windows on this project have landed —
are appended at the *end* of the conversation and never written into the system message,
because a system message that changed would make every message behind it new again, while a
note at the end costs only the note.

**Rung 2 — page images.** Instead of summarizing the part being dropped, it can be
photographed: rendered verbatim to monospaced page images that the model reads back. No model
call, nothing paraphrased. This rung is chosen only when you gave `/compact` no focus, there
is a workspace, there is page budget, and the model in use can read images. Pages are 120
columns by 64 lines, greyscale, deterministic, footed `<title> | context page 1 of 4`, and
saved under `<workspace>/.aforge-v3/frames/`. The ceiling is **8 pages**; anything past it is
summarized and appended after the pages.

**Rung 3 — the summary.** One call on the session's own model, with no tools, that must
produce six sections — `## Goal`, `## Constraints & Preferences`, `## Progress`,
`## Key Decisions`, `## Next Steps`, `## Critical Context` — and must reproduce any
unanswered question verbatim and preserve exact paths, symbols, commands and error text.

## What a compaction pass keeps

The transcript is cut at a message boundary, walking back from the tail until the keep-recent
budget is spent. The system message is never cut.

What is rebuilt, in order: the system message, then the page images if there are any, then
the summary note if there is one, then **the state block verbatim**, then the kept tail. The
state block — what `track` and `commit` recorded — is injected directly and is **never routed
through the summarizer**, so working state cannot be paraphrased away.

The note the model reads above a summary begins:

```
[context compacted] Everything before this point was summarized to fit the context window. This note is the record of that conversation — it is not something either of us said, and any question inside it is still open.
```

A pass can decline: `session: nothing to compact` (everything already fits in the tail),
`session: a compaction pass is already running`, or `session: summarizer returned nothing`.

## What happens when the conversation gets too long — when compaction happens by itself

When the conversation gets too long to fit, nothing is lost and nothing stops: the oldest
part of it is summarized away and the recent tail is kept, which is what compaction is.

**Nothing is lost is meant literally, and you can go and look.** The session file keeps
every original line, and scrolling up above the boundary is given those rather than the
shortened copy — with one dim line, `· above here the model keeps a shortened record — you
can still read it all`, where the two meet. What shrank is the model's copy, not yours (the
screen page has the whole of that line's meaning, and the limit: a session compacted by an
older aforge is still drawn from the shortened copy). The fold marker the model sees names
that journal as a real path — `[folded 31 messages · grep or read /home/x/.aforge/v3/sessions/abc.jsonl, lines 12..40]`
— so aforge can open the lines that left the window itself. *Where did the folded messages
go* on the compacting page is the whole of that.

Four ways a pass starts:

- **Automatically**, after any step where the estimate is over the threshold. A failed pass is
  not a failed turn.
- **Just before a request that would not fit**, when the transcript is already past the
  256,000-token ceiling. This one runs **even when automatic compaction is switched off** —
  the switch governs headroom, and fitting is not headroom.
- **On a context-overflow error from the provider**, once per turn. This one runs **even when
  automatic compaction is switched off** — the switch governs the automatic pass, not the
  recovery from a request the provider has already refused. Overflow errors are never retried.
- **On demand**, when you ask for it.

While a pass runs you see `compacting ~84k tokens` (`~842` under a thousand). On success:
`compacted from ~84k tokens, kept last ~20k`, or with page images
`compacted from ~84k tokens to 4 page images, kept last ~20k`. On failure:
`compaction failed · context unchanged`. A failed pass always settles its row.

## /compact — compacting now

`/compact` summarizes the conversation on demand. It notes `compacting…` immediately and runs
the pass off the loop, so the surface stays alive.

**Success is silent.** There is no "done" message — a compaction that worked simply leaves the
conversation shorter. A failure comes back as `compact failed: ` followed by the error.

A `/compact` given a focus always goes to the summary rung rather than page images, because a
renderer cannot be careful about anything. The focus is *appended* to the summarizer's
instructions and never replaces them, so one careless phrase cannot cost the next session its
file paths. It travels on that one request and no further.

## Turning automatic compaction off

Launch with `aforge chat --no-compact` or `aforge resume --no-compact`. The flag's own help
text reads `never compact automatically`.

What it turns off is exactly the automatic threshold check. Still working:

- `/compact`, when you ask for it, and
- the recovery pass when the provider itself refuses a request as too long.

With a `--host` remote launch the flag is **refused rather than ignored**, because it cannot
travel to the other machine.

## What am I allowed to spend — the limits aforge ships with, and turning them off

Every limit aforge ships with is a **backstop against something going wrong**, not a
budget. Nothing here is a number anybody chose for you, so all of them start large enough
that ordinary work never reaches them.

They live on **one tab**: `/settings` → **Spending**, which `/budget` opens directly.

| Row | Ships reading | What happens at the line |
| --- | --- | --- |
| **per day** | `$500` | new work waits for midnight or for you to raise it here |
| **per conversation** | `no limit` | this conversation stops starting new turns; the turn in flight always finishes |
| **per plan** | `asks first above $100` | a planned job estimated above it quotes its step count and its price and waits for your go-ahead — it asks, it does not stop |
| **per task** | `no limit of its own` | nothing of its own; a task spends against the day and this conversation |
| **per standing run** | `$5 a firing` | that one firing stops there; each order may name its own |
| **practice** | `$50 of the day` | aforge's practice on itself stops until tomorrow, and your own work is untouched |

Above those six the tab leads with **`today`**, which is a reading and not a setting:
`$3.42 of $500 · resets at midnight`, or `$3.42 · no limit` on a machine with no daily
limit. Before the first model call of the day it is **not on the tab at all** — a machine
that has not spent anything has not spent zero.

Under `today` a second reading appears **only when something went wrong writing spending
down** — `unwritten · 3 spending records could not be written`. Writing the ledger never
makes a reply wait, so a disk that stops answering costs a record rather than a turn; the
row is there so a figure that is short says so. It is absent on any ordinary day.

The `per conversation` row carries a receipt of its own, `this one $53.58`, and it is the
**same figure the money segment on the status line draws** — this conversation and every
piece of work it started, whether or not that work has finished. It used to say only what
the conversation itself had spent, so the tab and the row a person pressed to get here
disagreed while a task was running.

**Four of the six are rows you can edit** — `per day`, `per conversation`, `per plan`,
`practice`. `per task` and `per standing run` are **readings**: they are real rails, and
neither is a number a settings row could hold. The sections below say why.

The rows used to be spread across two other tabs — the money on **Workspace**, the
conversation's own ceiling on **Session**. They are all on **Spending** now, and
**Workspace holds no money row at all**.

## Where are the spending limits — the Spending tab, and every door onto it

The settings panel's tab bar reads, in order:

```
Session · Context · Workspace · Display · Spending · Safety · Tasks · Providers · Connections
```

Money is on **Spending** and nowhere else. The rows that used to share it are on the two
tabs beside it: **Safety** is what aforge may do without asking you first (ask before
running, tool exceptions, shell command rules, guardian, approval countdown, task
countdown, who settles work that needs a look), and **Tasks** is how work you can walk
away from is run (starting a task, check task work, task repair rounds, tasks at once,
busy machine, memory floor, task model).

**Six doors open that one tab, and none of them is a second editor** — every one lands on
the same registry row, so what you set through one is what the others show:

| Door | What you do |
| --- | --- |
| `/budget`, also `/limits` | opens the tab with the cursor on `per day` |
| the money segment on the status line | press `$0.14` — it opens the tab. It brightens under the pointer to say it is a door |
| the spend place (`alt+5`) | `enter` on its first line, the dim `today $3.42 of $500 · /budget sets the limits` |
| the spend place, from a row | `→` opens the verb strip, where `b` is `the limits` |
| a refused turn | the message names `/budget` |
| the first-run setup | its third screen, `what may aforge spend?` |

`ctrl+,` opens the panel itself, and `←`/`→` walk to **Spending** from wherever it opened.

## How do I remove the daily limit — no limit, none, and why $0 is never shown

Type **`none`** on the row, or `/budget none`. Every one of these spellings lands the same
thing on any money row: `none`, `no`, `off`, `unlimited`, `∞`, `no limit`, `nolimit`,
`never`, and plain `0`. The row then **reads `no limit`** back to you, and it stays that
way across restarts — a written-down `0` is your own instruction, not a value to be
reverted at the next launch.

**`$0` is never rendered.** A money row with nothing set reads its own word for that, and
each row has a different word because each rail means something different at zero:

| Row | What it reads with no limit |
| --- | --- |
| `per day` | `no limit` |
| `per conversation` | `no limit` |
| `per plan` | `never asks` |
| `practice` | `practice off` |

That is the emptiness law applied to money: `$0` would read as *zero dollars allowed*,
which is the exact opposite of what it means on three of these four rows.

**`practice` is the one row where `0` is not "no limit".** Zero turns aforge's
self-practice **off** rather than uncapping it. Practice is work aforge does while nobody
is watching, so it is the one pocket that always has a bottom — there is no way to ask for
unbounded practice, on purpose.

What a money row will accept, in its own words: `an amount in dollars, like 5 or 2.50 — or
none for no limit`. Anything else comes back as `that's not a dollar amount — a number, or
none for no limit`, and nothing is written.

## Why did it stop and ask me about money

Four different limits can put a question or a stop in front of you, and each says which
one it was:

- **`per plan` — it asks, it does not stop.** A planned job estimated above `$100` quotes
  itself before it starts: *"… comes to 12 steps, about $4.10 at what work like this has
  cost here. Start it, or trim it first?"*, with `yes, start it` and `hold it — I'll trim
  it first` as the two answers. Answered once, the decision stands for that job. Set the
  row to `none` and it never asks.
- **`per conversation` — it stops.** `conversation limit reached · $2.05 spent of $2 ·
  /budget changes it`. The section on that below has the whole of it.
- **`per day` — the day's work waits.** When the day's calls reach the daily limit, new
  work waits for midnight or for you to raise it. `/budget 800` raises it where you stand.
- **A task's own cap.** A task started from the composer layer (`alt+enter`) carries the
  figure on that layer's third line — `it may spend up to $100.00 before it asks` — and
  stops before its next turn when it reaches it. That figure is set where the task is
  started, not on the Spending tab.

An **adaptive run** is the fifth: its tank empties, it finishes what is in flight, starts
nothing new, and asks you to top it up, finish on what is done, or stop.

If you want to see where you stand before anything asks, `today` at the top of the
Spending tab and `/cost` are the two readings — `/cost` is this conversation, `today` is
the whole machine since midnight.

## What does per plan mean — the limit that asks instead of stopping

`per plan` is the only money row that **asks rather than stops**, and its value says so:
it reads `asks first above $100`, not a bare figure.

When a planned job is estimated to cost more than that figure, aforge quotes the step
count and the price and waits for your go-ahead before any of it runs. Nothing has been
spent at the moment it asks — the question comes before the first worker says a word — so
holding it costs nothing. Answering it settles that job for good; you are not asked again
for the same one.

The estimate is grounded in what work like it has actually cost on this machine, so a job
with no priced history behind it is not held.

Set the row to `none` and it reads **`never asks`**: no plan is ever quoted and every one
starts straight away, bounded then only by the day's limit and this conversation's.

The row was called `ask before spending` when it lived on the Workspace tab, and the
setting key behind it is still `plan_consent_usd` — the panel's search matches the key as
well as the label, so typing either finds it.

## What may a task spend — a task has no dollar limit of its own

**A task carries no dollar cap of its own.** The Spending tab says so on the `per task`
row, in those words: `no limit of its own`, with the dim receipt `it spends against the
day and this conversation`.

That is not a missing feature — it is what the rail actually is. A task's own bounds are
**steps and time**, not money: a deadline it may renew, a step count, and a limit on how
long it may go without progress. The money it spends is counted against the day's limit
and against the limit on the conversation that started it, which are the two rows above it
on the same tab.

**What you get instead of a per-task limit is seeing it happen.** The `$` on the status
line counts what the tasks are spending while they are spending it, and `/cost` splits that
figure into `conversation` and `tasks`. A task is bounded by the wallet and watched on the
row — it is never stopped on its own dollar count.

So **there is no per-task money row to edit**, and `/budget task 20` is not a shape this
command takes. Where you *can* put a figure on one piece of work is the **composer layer**:
`alt+enter` before you send a task, and its third line reads `it may spend up to $100.00
before it asks`. Type a number there and that errand gets that ceiling — it stops before
its next turn once it reaches it, and any adaptive run it starts is held to a tank no
bigger. A task started any other way — `/task <brief>`, a proposal card, aforge's own
hands — runs under the day's limit and this conversation's.

`per standing run` beside it is the same kind of reading for a different reason: it reads
`$5 a firing · each order may name its own`, because that rail is written **per standing
order** where the order is made, not in one settings row.

## This conversation stopped starting turns — conversation limit reached

When `per conversation` is set and this conversation has spent it, the next turn is
refused before it starts, with exactly this line:

```
conversation limit reached · $2.05 spent of $2 · /budget changes it
```

The figures are yours; whole dollars are written without cents.

Four things are true of that refusal, and each is deliberate:

- **The turn in flight always finishes.** The limit stops the *next* turn. A turn with
  tool calls out is never cut in half.
- **The refused message is still yours.** Nothing was journaled, no request was sent, no
  tool ran — your text stays in the box, and sending it again once you raise the limit
  runs it for the first time.
- **It reads the recorded bill, not an estimate.** The figure is the provider's own cost
  numbers, folded in per answer.
- **It counts what this conversation spent before you resumed it.** The total is rebuilt
  from every earlier sitting, so a limit reached yesterday is still reached when you open
  the conversation today.

`/budget conversation 20` raises it where you stand, `/budget conversation none` removes
it, and `/new` starts a conversation with a fresh figure. There is no way to zero a
conversation's recorded spend while keeping the conversation.

The row is `per conversation` on the **Spending** tab; it sat on **Session** as `session
ceiling` until this wave, and the panel's search still matches the key `spendRail`.

**The status line warns before it stops.** The money segment takes the warm ink once this
conversation has spent four fifths of its own limit — the figure leaves the dim and
nothing else changes. With no `per conversation` limit set there is no fraction and no
colour.

## Which endpoint answers, and what it charges

One model id is served by many endpoints, and they differ in two ways at once: how fast they answer, and what they charge. The published list price beside a model is the model's own figure — no endpoint is obliged to match it, and the fastest one often does not.

So, with the **routing** row on the Providers tab left alone, aforge asks for two different things depending on who is waiting. **Your own turns** ask for the fastest endpoint, capped at **a quarter over the model's published list price**: an endpoint 25% dearer buys a head start you can feel, and one four times dearer buys nothing you would notice on a five-minute task. **Work you are not waiting on** — task workers, a divided part, the check on a piece of work, the model that names a task or a conversation, the memory pass — asks for the cheapest endpoint instead, because speed is worth nothing to a call nobody is watching.

Where a model publishes no price, no cap is sent at all rather than one guessed from something else. If no endpoint can serve a request under the cap, aforge lifts the cap rather than failing the turn, and says so on the attempt line.

**One thing about background work is not quite "speed is worth nothing".** Work you are not watching still asks the router for the cheapest endpoint — that part is unchanged — but among the machines behind that model, aforge will pay a little for a quicker one **while your window is open**, because you are there to read what the work lands. With no window open on this machine it will not: the cheapest machine wins outright, however slowly it writes. Nothing about this is a setting; it follows whether you are here.

Setting **routing** yourself overrides all of that everywhere: `latency` asks for the fastest endpoint (still under the price cap) for every call including background work, `price` asks for the cheapest for every call including your own turns, and `off` sends no preference and stops timing endpoints. A change lands on the next session.

**You can also name the endpoint yourself.** routing says what a request prefers; the **lane** row above it, and `→` on a row in the model picker, say which provider your conversation actually goes to — see "choose a provider" above.

With `routing: off` there is nothing measured, so there is no lane to choose, no sheet of them to open under a model row, and no speed guard.

## Choose a provider — pinning the endpoint that serves your model, and what the lanes under a model row are

One model id is served by a dozen different endpoints, and they are not alike: on one
model measured on one afternoon they differed by **seven times** on the wait before the
first word and by **twelve times** on how fast they wrote, at roughly the same price.
Some of them will not take a tool call at all; some stop writing at 65,000 tokens; some
serve four-bit weights. Which endpoint answers you is often a bigger difference than which
model you picked.

aforge calls one of those endpoints a **lane**, and you can see them and choose one.

**Three rows on the Providers tab of `/settings` sit directly under **your model**, in
that order** — **lane**, **speed guard**, **routing** — because the machine that serves
your model is part of the same decision as the model:

```
 your model    deepseek/deepseek-v4-flash · auto (cloudflare now)
 lane          auto
 speed guard   on
 routing       latency
```

The tail on the model row is the machine: `auto (cloudflare now)` when the choosing is
left to aforge, `pinned: cloudflare` when it is not, `openrouter` when you have asked for
no endpoint at all. A session that has measured nothing shows the model id alone.

In the model picker — `/model`, or `enter` on that **your model** row — press `→` or
`tab` on a row and the model's lanes open underneath it:

```
 deepseek-v4-flash   via cloudflare · ▲0.8s · $0.09/$0.18 per M · 1M · 58t/s
   ● auto        picks the fastest lane each answer — cloudflare now · recommended
     cloudflare    0.8s · 58 t/s · $1.3/M · no tools · 100% · ▁▂▁▃▁▂
     coreweave     0.4s · 24 t/s · $0.28/M · tail 12s · 99% · ▁▁▇▁▂▁
     deepinfra     0.8s · 27 t/s · $0.18/M · out ≤ 65k · 99%
   ○ openrouter  let the router balance on price
```

Each lane row reads, in order: its name, the wait before the first word, how fast it
writes, what a million output tokens cost there, one short note about what is wrong with
it, how much of the last five minutes it was answering, and a sparkline of **your own**
last eight first-token waits on it (taller is slower). That is also the order a narrow
window gives them up in — the sparkline goes first, and the note about capability outranks
the uptime because `no tools` changes the answer you get. `←` or `tab` closes the lanes
again.

`enter` on a lane **pins** it: every request for this conversation goes to that lane
and nowhere else. `enter` on `auto` un-pins. `enter` on `openrouter` asks for no lane
at all and lets the router balance on price. If the lanes were open under a model you are
not talking to, `enter` switches to that model as well — choosing a lane under a name
means you want that name served from there.

`enter` on the **lane** row opens that same fold directly, on the model you are talking
to, with the cursor already on the lane in force — so choosing an endpoint is reading
the measured numbers and pressing enter, never guessing at a word. When nothing has been
measured there are no machines to list, and the row walks between the only two honest
answers instead: `auto` and `openrouter`.

From the keyboard alone: `/model @cloudflare` pins, `/model auto` un-pins.

**A model nobody has measured has no lanes to open, and `→` does nothing on it.** There
is nothing truthful to put under it, so nothing is drawn — the same rule that leaves the
speed off its row.

## What the note on a lane row means — no tools, out ≤ 65k, tail 12s, fp4

One note at most, and it is the thing that would spoil the answer soonest:

| Note | What it means |
|---|---|
| `bad replies` | enough of its answers came back unusable that aforge would rather ask elsewhere |
| `no tools` | the lane does not honour a tool call — a fast wrong answer |
| `out ≤ 65k` | it stops writing well before other lanes do, so a long answer is cut |
| `fp4` | it serves weights at a lower precision than the others |
| `tail 12s` | its worst answers start about that late — five times its own median |

A lane with none of those shows no note, and a lane whose answers nobody has judged never
shows `bad replies` — an untried lane is not a suspect.

`bad replies` counts a reply that lost its thread, one that came back as tool markup, one
that went quiet and had to be cut, and one that arrived with nothing in it. It fades over
about an hour on its own, and every usable answer the lane serves takes it further off.

## Where the numbers on a lane row come from — the sheet, and your own answers

Every figure is aforge's own **belief** about that lane, never a raw published number.
It starts from the router's public sheet — first-token and throughput percentiles over
the last half hour, over everybody's prompts — and every answer you get moves it toward
what that lane did for **you**, from where you are, with the prompts you send.

The belief also **forgets**: with nothing new arriving, aforge's confidence in it halves
about every ten minutes, so a lane that misbehaved once at breakfast is not held to it
all day and there is no penalty box to let anything out of. What aforge believes about a
lane's **answers** rather than its speed forgets more slowly — about an hour — because real
requests are minutes apart and a belief that forgot faster than the evidence arrived would
never be worth anything.

The dim line under the cursor says both halves out loud:

```
cloudflare: first token 0.8s, steady 58 t/s, no tail — from the sheet + your last 12 answers
```

## Filtering the picker by speed, price and capability — @cloudflare, <1s, >50t/s, $<0.3

The filter box takes a few words that are not names at all. Each narrows the list, and
they combine:

| What you type | What it keeps |
|---|---|
| `@cloudflare` | models with a lane whose name carries that word — and it opens the first one on that lane |
| `<1s`, `<800ms` | the best lane starts within that |
| `>50t/s` | the best lane writes at least that fast |
| `$<0.3` | the best lane charges under that per million output tokens |
| `fp8`, `bf16` | it has a lane serving at least that precision |
| `tools` | it has a lane that honours a tool call |
| `sees`, `draws` | the model reads images, or answers with them |
| `fast` | sorts what is left by how soon an answer would start |
| `cheap` | sorts what is left by price |

**Anything else you type is still the search it has always been** — prefix, then
substring, then subsequence over the model id — so `ds v4` and `claude 4.5` work exactly
as before, and a word this grammar does not know is simply a word to search for.

## Why did it say via cloudflare — the lane named on the status line

Beside your model on the status line, `via <name>` is the lane that actually answered,
and it is a fact rather than a decision: it is the name that came back on the answer. When
aforge knows the timings it reads `via cloudflare · 0.6s · 61 t/s` — the wait before the
first word, and how fast it was writing. The rate is only there **while a turn is
running**, because a rate is a claim about now; the name alone goes quiet after ten
minutes.

It is left off entirely when the lane's name is already in the model id: `gpt-4.1 ·
via openai` is a row saying the same thing twice.

**While a turn is running you usually see something better than `via`.** The connection
reports what it is doing right now, and that outranks both readings under it, so the same
spot reads `thinking · 12s · friendli 38 t/s` or `first word · 3.1s → parasail at 4.4s`
until the request ends. The ranking is by tense: the phase is what this request is doing,
`via <name>` is what the **last** answer did, and the older sighting under that is what
some answer did in the last ten minutes. Drawing the older one under a request that has
been stalled for a minute is exactly the thing this ordering exists to stop.

## What "rescued" means on the status line, and "slow · trying …" and "refused"

Those two only appear together, and only when the **speed guard** is on.

When an answer takes much longer to start than that lane normally takes, aforge asks
the next-best lane the same question, and you read whichever one replies first.

The moment the second request goes out, the status line says so and says **why**:

```
  stalled 9s · switching to coreweave
```

The stall comes first because it is the reason — the switch on its own is the same
sentence with the cause taken out of it. After that the line goes back to the ordinary
phases for the new request (`first word`, then `thinking` or `writing`). Where nothing on
the wire is reporting, the wording `slow · trying coreweave…` is drawn instead —
either way this is the only place the program calls anything slow, and it says it while
something is already being done about it.

**A machine that REFUSED is not a machine that was slow, and the line says so.** When the
router answers that the machine aforge asked for is not one that serves this model —
`your request's provider.only preference permits only: coreweave` — the same spot reads
`refused · trying nextbit…`. That machine is then finished for this model: it is not asked
again, and it leaves the set aforge chooses from for thirty minutes. If the
machine the answer moved to refuses as well, the promise is withdrawn rather than left on
the screen, and the row reads `nextbit refused`.

If the second lane wins, the line reads `via coreweave · rescued` for that answer once the
request has finished, and goes back to normal on the next one.

Whichever way it lands, the loser is cancelled and what it told aforge about that lane
is kept, so a rescue is also a free measurement.

## Speed guard — what it costs and when to turn it off

**speed guard** is a row on the Providers tab of the settings panel, directly under
**lane** and two rows under your model, and it is **on**.

It hedges **at most one extra call** per answer and stays under **a tenth** of what the
session spends. It does nothing under `routing: price` — nobody is buying seconds there —
and nothing while an answer is already flowing normally.

Turn it off if you are paying for every token and never mind waiting. With it off, the
`auto` row in the model picker says `no rescue`, so you can see the promise it is making
— and the measurement described in the next section stops being bought as well. The two
are one row because they are one promise: aforge may spend a little extra to keep an
answer moving.

## Why aforge sends something when you start typing — the one-token measurement

While you are typing, and before you press enter, aforge sends **one token** to each of
the two lanes your next message would most likely go to, and times how long the first
word took to come back. It does that for two reasons: the router's own published figures
are a half-hour average over everybody's prompts, and this is a measurement of **your**
path to that machine taken seconds ago — and the connection is left warm, so the real
answer's first word is not also paying for a handshake.

**What it costs.** About **two hundredths of a cent** per turn: ten tokens in and one
token out, twice.

**How often.** At most one pair every **twenty seconds** per model, however fast you
type — so a long message buys one, not one per keystroke. None at all when the **speed
guard** is off, when `routing` is `off`, when the lane row says `openrouter`, or when
the connection pool is already being rate-limited.

**Nothing ever waits for it.** It is sent and forgotten; a message you send a moment
later does not wait on it, and a probe that fails teaches nothing and changes nothing.

## The lane row in settings — auto, pinned, pinned but borrowable, openrouter

Settings → Providers has two rows under **routing**:

```
 your model     deepseek-v4-flash · auto (cloudflare now)
 lane           auto
 speed guard    on
```

`enter` on **lane** walks it through four answers:

| Value | What it does |
|---|---|
| `auto` | aforge picks the fastest lane each answer |
| `pinned: cloudflare` | every request goes to that lane and nowhere else |
| `pinned: cloudflare, borrow when slow` | it goes there, but a slow answer may still be rescued elsewhere |
| `openrouter` | no lane is asked for; the router balances on price |

The pinned rungs are missing until aforge has measured something — there is no honest
lane to name yet, so the walk is `auto` ↔ `openrouter`.

The **your model** row says which lane is answering it beside the model id — `auto
(cloudflare now)` while the choice is aforge's, `pinned: cloudflare` once it is yours.
`lane` and `routing` are different questions: routing is what every request **prefers**
(fastest, cheapest, or nothing at all), and lane is which endpoint your conversation
actually lands on.

## Why does the same conversation suddenly cost more? Keeping the prompt cache warm

Every request in a conversation re-sends the whole conversation. What keeps that from costing a fortune is the **prompt cache**: the endpoint that answered you a moment ago still has those tokens, and re-reading them costs a fraction of sending them fresh. The catch is that the cache sits on **one machine**. An endpoint that has never seen your conversation charges full price for all of it — measured on a real run, the same 94,000-token context cost **4.7 times more** on a cold endpoint than on the warm one, and that alone is where a quarter of the requests in that run ate half its money.

So aforge remembers which endpoint answered your last request and **asks for that same endpoint first on the next one**. It is a preference, not a demand: if that endpoint is busy or gone, the request still goes through somewhere else rather than failing. Nothing extra is sent and nothing is probed to work this out — it is the name that came back on the last answer.

It moves off that endpoint when the endpoint stops earning it, and there are three ways that happens:

- **The request failed there** — an error, a refusal, or a reply that went quiet or turned to garbage halfway through. The next request is routed afresh.
- **The cache was gone anyway.** If a long prompt comes back having read nothing from the cache, there is no warm context left to come back for, so the next request is free to land anywhere.
- **It charged too much.** The same quarter-over-list price cap described above rides on every one of these requests, and an endpoint that billed above it loses its place. A warm cache is never worth any price.

Each of your conversations keeps its own endpoint, and so does each worker on a task, because each of them is sending a different transcript. Background work is kept warm the same way: its first request still asks for the cheapest endpoint, and after that it comes back to whichever one answered. Setting **routing** to `off` turns this off with everything else.

## A model that cannot stop thinking — what turning thinking off does on it, and why some models think at "max" by default

Some models cannot have their thinking pass switched off at all; the model list says so
for each one (GLM 5.3, Gemini 3.7 Flash and Grok 4.6 were among them on 2026-08-28).
Asking such a model to think less does not fail and is not ignored: aforge sends the
lowest thinking level the model offers instead of a switch-off it would refuse. That
matters more than it sounds. Sent nothing at all, one of these models runs at its own
published default — for GLM 5.3 that default is "max" — and can spend ten thousand tokens
thinking before it writes a word, which is how a headless run once came back with an empty
plan. At its lowest level the same model answered the same question in twenty seconds with
a hundred tokens of thinking.

The reply room is sized to match: thinking tokens count against the same ceiling as the
answer, so the ceiling is grown by the share the chosen level takes (roughly a fifth at
low, half at medium, four fifths at high), and the wait for the reply is sized from that
same ceiling. If a model's list does not say how much it thinks, the first time an answer
comes back empty with the whole ceiling spent, aforge remembers that model thinks
regardless and leaves room from then on; it never remembers it on a guess.

## Where are the logs of what aforge sent the model — the model-call log, and reading it with `aforge logs`

Every call aforge makes to a model writes a line to one file, always, with nothing to
switch on first. It lives at `~/.aforge/logs/calls.jsonl` — beside the rest of what aforge
keeps, and under `AFORGE_PROFILE_DIR` when you have moved that. Read it with:

```
aforge logs                 the last 40 calls, newest last
aforge logs --tail 200      more of them
aforge logs --follow        keep printing calls as they happen
aforge logs --path          print the file and nothing else
```

One line per call, and it reads like this:

```
21:12:53  compile  z-ai/glm-5.3-flash  low  max 10240  → 200  12.7s  stop  466 tok  $0.0003
21:12:41  compile  z-ai/glm-5.3-flash  low  max 10240  → 400  0.2s  Reasoning is mandatory for this endpoint  learned reasoning_mandatory
21:13:04  leaf  #build  z-ai/glm-5.3  high  max 65536  ⋯ in flight 3m12s
```

What one line holds: when the call went out, what it was for (`turn`, `leaf`, `task`,
`compile`, `brief`, `contract`, `gate`, `reflex`), which model was asked and which endpoint
actually answered, the thinking level and the **ceiling that really travelled** — which is
larger than the one asked for, because the thinking pass is given room in front of the
answer — how many messages and tools the request carried, the status it came back with,
how long it took, how it finished, the tokens and the cost, and anything the refusal
taught aforge about that model.

**A call still running shows as `⋯ in flight`.** That is the reason a line is written when
a call goes *out* as well as when it comes back: a planning call four minutes into a
65,536-token ceiling used to look exactly like a machine doing nothing.

**A line may leave a number out and say which one in its `note`** — `cost_s was +Inf
and is not on this row.` — because a figure the endpoint or the wait never really measured
is missing rather than invented, so a row short of `cost_s`, `wait_s`, `waste_usd` or
`cost` beside a sentence like that is an honest line and not a broken one.

**The headless waiting line reads the same record.** When `aforge do` has nothing new to
say it prints `still waiting: … · last call <model> <n> ago`, and that `last call` is the
newest answer this process has heard — the call log's own memory, kept even with the file
switched off — not the moment the work was booked. A worker ten minutes into its work says
`last call … 1s ago`, because that is what is true.

The file rotates at 32 MB and keeps one predecessor, `calls.1.jsonl`. `aforge doctor` names
the file and its size. Set `AFORGE_CALL_LOG=off` to write nothing at all, or
`AFORGE_CALL_LOG=/some/path.jsonl` to put it somewhere you can watch.

## What did you send the model — the prompts are not in the log unless you ask for them

The model-call log records the **shape** of every request and never its contents. It says
how many messages went, how many tools were offered, which knobs were set and what came
back — and nothing of what you wrote, what your files say, or what the model answered.
That is deliberate: a debugging record that quietly accumulated your prompts would be a
liability rather than a tool.

When you genuinely need the exact bytes — a request the endpoint refused for a reason
nothing else explains, a reply that came back malformed — run one session with:

```
AFORGE_CALL_LOG_BODIES=1 aforge
```

Every line then also carries `request_body` and `response_body`, whole and unedited: your
prompts, your attached file contents, the model's whole reply. Turn it on for the run you
are debugging and off again afterwards, and treat the file as you would the conversation
itself.

**Why did that call fail?** The line says. A `→ 400` carries the endpoint's own first
sentence; a line with no status at all is a request that never reached an endpoint; a line
marked `empty at the ceiling` is the thinking pass having spent the whole reply room
before the answer began, which is the one failure a larger ceiling actually fixes. Failed
attempts get their own lines, so a call that was rate limited four times before it landed
is five lines rather than one slow one.

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
| enter | switch to the row under the cursor |
| esc | cancel, changing nothing |

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
`filter · ↑↓ · ctrl+t effort · enter switch · esc cancel`

There is no mouse commit on the picker's rows.

## What each row in the model picker tells you

A row reads `<id>:<level>` on the left and, dimly on the right, the facts the catalog
published: the context window, the price per million prompt and completion tokens, the
arena elo, and what the model can do besides write. **Each part is hidden when nobody
published it.** A price shows only when both halves are known — a zero means "nobody said",
never "free".

Only models you can hold a conversation with are listed: text in, text out. A model that
publishes `["image","text"]` out (a drawing model that captions) is excluded, and so is a
transcription model (audio in, text out). A row that publishes nothing about itself is judged
by its id against a narrow list of generation and sidecar words.

## What "sees", "draws", "speaks", "films", "hears" mean on a model row

The dim tail of a picker row ends with what the model can do besides hold a conversation,
in one word each:

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

There is one check, and only one. If the slug **is** in the catalog and cannot hold a
conversation — a drawing model, a speech model, a transcriber — aforge refuses in one line
and the conversation does not move:

```
openai/gpt-4o-mini-tts cannot hold a conversation — it speaks. Still on moonshotai/kimi-k3.
```

A slug the catalog has never carried is still **taken at its word**, exactly as before:
aforge may be offline, or you may be naming a model this build has never listed. In that
case the context window is left alone.

## The crew — which models aforge uses on my behalf, and /crew

aforge makes calls you did not type: naming a session, naming a piece of work on the roster,
the summary a compaction keeps, the safety gate, the check on finished task work, the second
look before a task starts itself, the reading of a task's parts before they are handed out,
the planner of an adaptive run and the nodes under it, the designer of a saved harness page,
looking at an image. Each of those is a
**role**, and every role sits on one of four **classes** — the **crew** — which you set in
`/settings` → Providers, or in one word with `/crew`:

- **reflex** — near-free · reads every turn — memory, titles, safety.
- **small work** — cheap · does the bulk work — run nodes, digests.
- **careful work** — careful · checks what must not be wrong — audits, compaction, vision.
- **mastermind** — thinks · plans runs and designs harnesses.

**All four arrive with a model already in them**, and the four together are the `balanced`
preset:

| class | as shipped |
| --- | --- |
| reflex | `nex-agi/nex-n2-mini` |
| small work | `deepseek/deepseek-v4-flash` |
| careful work | `qwen/qwen3.8-27b` |
| mastermind | `moonshotai/kimi-k3:low` |

They are all open-source models, and none of them is the model you are talking to. A crew
that followed your conversation would put the most expensive model in the build on the
cheapest questions in it — a call made twice every turn on a frontier model is a bill nobody
agreed to.

The careful class's `qwen/qwen3.8-27b` is vision-capable, so the shipped crew can fund the
vision role as well as text checks. Its OpenRouter rates are $0.45/M input tokens and
$3.20/M output tokens, with a 262k-token context window.

**Clearing a row is still an answer.** A class you empty on purpose reads
`follows the conversation`, and every role on it runs on the model you are talking to. That
is the only way to say "use my model for this", and it is deliberately something you have to
say rather than the default.

### The three presets

| | frugal | balanced | max |
| --- | --- | --- | --- |
| reflex | `nex-n2-mini` | `nex-n2-mini` | `nex-n2-mini` |
| small work | `deepseek-v4-flash` | `deepseek-v4-flash` | `deepseek-v4-pro` |
| careful work | `qwen3.8-27b` | `qwen3.8-27b` | `kimi-k3` |
| mastermind | `qwen3.8-27b` | `kimi-k3:low` | `kimi-k3:high` |

- **frugal** — qwen handles careful work · pennies a day
- **balanced** — kimi-k3 thinks, qwen checks
- **max** — kimi-k3 everywhere, thinks longer

`/crew` opens all three as a chooser with yours marked, under a scope line — `the four
models aforge uses on its own behalf — not the one you chat with` — and a `you talk to ·
<model>` line naming the seat the presets do not touch. ↑ / ctrl+p and ↓ / ctrl+n move;
enter applies and esc cancels. If the four classes make a custom crew, no row is marked and
the chooser says picking one puts all four back. `/crew max` still sets it directly and
confirms in one line, which ends `· you are still talking to deepseek-v4-flash — /model
changes that` — naming the conversation's own model by id, because the crew changes
nothing about it and the model segment on the status line goes on saying what it said before.
The **crew** row in `/settings` → Providers is the same thing: enter or space walks it
frugal → balanced → max.

**The crew row is not stored — it is worked out from the four.** Answer any one of the four
rows yourself and the crew row reads `custom`, because that is what is true. `/crew balanced`
puts all four back in one write.

### The roles under each class

Directly under the **pinned roles** row the panel lists **every registered role**, grouped
under the class answering it, saying which model comes out. As shipped:

| role | class | what it is |
| --- | --- | --- |
| `reflex` | reflex | reads every turn for memory — routing and keeping |
| `title` | small work | the name a session gives itself |
| `worker` | small work | one node of an adaptive run |
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

The crew is a different dial: the four **classes** aforge makes its own calls on — reflex,
small work, careful work, mastermind — used for titles, memory, the safety gate, checks on
finished work, compaction summaries, adaptive-run planners and their nodes, harness pages,
and looking at an image. Setting it writes all four class rows in one write, and **it is
live from that moment**: the next call aforge makes on its own uses the new crew, with no
relaunch and no new session.

**Where to read the crew back:**

- `/status` prints a `crew` line directly under `model`:
  `crew     max · brain kimi-k3:high · hands deepseek-v4-pro · checks kimi-k3`. The word is
  the preset, or `custom` when the four classes are your own arrangement.
- `/settings` → Providers has the **crew** row above the four class rows.
- Bare `/crew` opens the three presets with yours marked, under a `you talk to · <model>`
  line naming the seat they do not touch.
- The live status line says `crew max` at the head of the telemetry, across the gap from
  the model segment — the same word `/status` prints, read from the same four rows.
- The hint line under the model picker says `crew max` beside its keys, so the picker you
  opened looking for the change tells you the crew is a separate thing.

**Tasks do not follow the crew either.** A plain task runs on the `task.model` row, or on
the conversation's model when that row is empty. Only an **adaptive run** uses the classes:
its planner takes the **mastermind** class and every node under it takes **small work**.
Those ids are settled once, when the run starts, so changing the crew — or `/model` — half
way through does not move a run already going.

## What are the five models — the one you talk to and the four crew seats

aforge runs **five model seats**. **Seat one is the model you talk to**: it answers every
message you type, it is the id on the left of the status line, and `/model` is the only thing
that moves it. The other four are the **crew** — the models aforge uses on its own behalf,
for calls you did not type:

| seat | word | what it answers |
| --- | --- | --- |
| 1 | you talk to | your messages — set with `/model` |
| 2 | reflex | memory, titles, the safety gate — near-free, reads every turn |
| 3 | small work | run nodes, digests, task names — cheap |
| 4 | careful work | checks on finished work, compaction summaries, vision |
| 5 | mastermind | plans adaptive runs and designs harnesses — thinks |

`/crew` shows all five and sets seats two to five in one word — `frugal`, `balanced` or
`max` — and never seat one. Bare `/crew` opens with `you talk to · <model>` above the three
presets, so the seat the presets do not touch is on the same page as the ones they do.
`/settings` → Providers pins any one of the four on its own, which turns the crew word to
`custom`. The live status line says both dials: the model segment on the left is seat one,
and `crew max` at the head of the telemetry on the right is the other four.

## Does /crew change my chat model — no, and what crew max on the status line means

No. `/crew max` moves the four crew seats and leaves the model you talk to exactly where it
was. The confirmation names it:

```
crew → max · brain kimi-k3:high · hands deepseek-v4-pro · checks kimi-k3 · you are still talking to deepseek-v4-flash — /model changes that
```

`/model`, `/model <name>` or the model row in `/settings` are the only ways to change the
chat model, and `/crew` never offers to. The two dials also stay separate on the frame: the
status line shows the chat model on the left and `crew max` — or `crew balanced`,
`crew frugal`, `crew custom` when you pinned a seat yourself — at the head of the telemetry
on the right. That segment is a setting, not a measurement, so it is among the first things
a narrow row gives up; `/status` prints `model` and `crew` on neighbouring lines at any
width. A session opened without a profile has no crew and shows no `crew` segment at all.

## Asking a class to think harder — a level on a class value

A class value may carry a thinking level as well as a model:

```
moonshotai/kimi-k3:high
```

`:low`, `:medium` and `:high` are the three, and the shipped **mastermind** carries `:low`.
The level is not part of the model id — it travels as its own request option, exactly as
the picker's **ctrl+t** effort does — so the id sent to the provider is
`moonshotai/kimi-k3` and the thinking is asked for separately.

- **Any of the four class rows takes one**, though the mastermind is the one it is for.
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
◐ main ▸ ship the parser fix · planner: kimi-k3 · $0.87 / $2.00 · working
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

The nodes under the planner run on the `worker` role, which sits on **small work** — a
different model, and not on this line. `/settings` → Providers lists both.

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
with `change_setting`, into the same `models.roles` row, after asking you. The four class
rows (`models.tiers.reflex`, `models.tiers.low`, `models.tiers.high`,
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

It settles four things on your model: the four crew classes, any role you pinned, the model
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
| a gap mid-reply | **45s** | started writing and stopped |

There is a third clock for the opposite problem — a reply that keeps writing and never
finishes. It is not a fixed number, so it has its own section below: *A reply that never
finished*.

The first bound is generous on purpose: a reasoning model at a long context legitimately
thinks for a minute before its first token, and cutting a request that was about to answer
costs the whole prompt again. The second is shorter because the question is different — a
model that has started writing has finished deciding.

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
than **5 minutes** and never more than **20 minutes**. Two endpoints serving the same model
therefore get two different walls, and one that routinely writes long answers earns a
longer one by writing them. A model aforge has not spoken to yet gets the 5-minute floor,
because there is nothing measured to work from; the numbers are forgotten when aforge
closes, so a fresh session starts from the floor again.

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

**What it will not cut.** Fenced code blocks are never judged, so a page of zeros, a long
test log, a generated table or a big JSON dump is safe however repetitive it is. Neither
is a reply that is simply multilingual: switching language between words is ordinary
writing, and only switching *inside* words counts. A short repetitive answer is never cut
either — there has to be several kilobytes of it.

**Turning it off.** The row is `reply guard` on the **Providers** tab of `/settings`, `on`
or `off`, and the default is **on**. Off means you see whatever arrives. You can also just
ask aforge to turn it off; it is not one of the rows it refuses. The two clocks in the
section above have no switch — a request that produced nothing at all has failed by any
reading.

## What this conversation has cost — /cost

`/cost` (also `/usage`, `/tokens`, `/spend`) prints what this conversation has spent, and on
what, into the conversation. Up to five aligned lines:

| Line | What it is |
|---|---|
| `spend` | the money, printed only when it is above zero |
| `tokens` | `48.1k in · 3.2k out`, or one half alone, or the combined figure |
| `cache` | `31.2k read · saved $0.0180` — the money half only when a price pair was published |
| `model calls` | **requests to the provider**, deliberately not "turns" |
| `time` | how long |

`model calls` is the line people misread. One thing you type can become several requests to
the provider — every step of a turn is its own request — so this figure is normally larger
than the number of times you have spoken.

It counts **every** request, not only the ones in your turns: naming the session, a judge
deciding where something should be routed, looking at a picture, every request a task's
own agent made on its own lane, and every request a harness run made while it walked its
program. That is deliberate, because the `spend` line above it is the
sum over exactly those requests — a smaller count beside it would be a bill divided by the
wrong number.

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
keeps. **One line per turn — the turn as a whole, however many requests it took — and one per
call made beside a turn**, such as naming a session or judging a route. Each line carries a
`calls` figure saying how many provider requests it covers, so a turn that used three tools is
one line reading `calls: 4` rather than four lines. A turn that failed or that you interrupted
writes its line too, for whatever it spent before it stopped.

Every line records: when it happened and which local calendar day that was, which model answered,
how many requests and how many tokens in and out, what it cost, the conversation it was made
in, the piece of work or the standing promise it was made for, and the project directory it
ran against.

Three things are worth knowing about it:

- **The figures are the bill, not an estimate.** Until this file existed, "what did opus cost
  me this month" could only be answered by opening every transcript on the machine, and "what
  did I spend on Tuesday" could not be answered at all — a conversation's own total has no day
  in it.
- **A turn or a call that cost nothing writes no line.** So a day with no lines is a day
  nothing was spent, rather than a day of zeroes. The place obeys the same law and draws
  nothing for an unpriced call rather than calling it free.
- **Work is counted once.** A task's own requests are recorded where they were made. Its total
  is added to the conversation that started it afterwards, and that addition is deliberately
  not written here, or the same money would be counted twice.

`/cost` and the status line are unchanged and are still rebuilt from **this conversation's**
own transcript. The spend place is the whole machine; `/cost` is this conversation. They
answer two different questions and neither is a correction of the other.

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
  model you are talking to and for no other; the four crew slots are answered where their
  own session is opened, so this window cannot tell "nothing is bound" from "I cannot ask" —
  and the emptiness law says an unknown is drawn as nothing rather than guessed at;
- **what it was for** — the three things money is ever spent on, because the ledger holds
  three ids: a piece of work, a standing promise, or a conversation. The dearest three are
  shown and the rest fold into one line.

The ledger holds **ids and no titles**, so the place joins each id against the records it is
already reading — the project's own index of what it ran, and the standing store — to put a
name on the row. A thing neither of them knows keeps its id.

**`enter` on a row of "what it was for" opens what it was for**: a task goes to the tasks
place, a standing promise to the standing place, a conversation to home.

**There is no budget editor here and there will not be one.** The allowance is a rail and it
is edited on the status line's money segment — the one that shows it. See "Spending limits"
below. A cap on **one task** is a different figure and is set where that task is started —
on the composer layer's third line, before you send it (the tasks page, *a task started from
the composer carries a cap*).

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
crew     max · brain kimi-k3:high · hands deepseek-v4-pro · checks kimi-k3
```

On the live status line the crew is one short segment — `crew max`, or `crew custom` — at
the head of the telemetry beside the model, and among the first a narrow row gives up; the
`crew` line here and on the phone's status sheet is the full reading. A session opened
without a profile directory has no crew to read and gets no `crew` line or segment at all.

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

## The most tokens one request can carry — the 256,000-token ceiling

**Whatever a model claims, this conversation never carries more than 256,000 tokens into a
single request.** That is a hard ceiling, twice the 128,000 default, and the threshold above
is worked out from the smaller of it and the model's own window. On a model claiming
1,310,720 tokens, compaction therefore fires at **217,600**, not at 1,114,112.

It exists because the claim is published by the provider and a published number can be
enormous. A session on `~deepseek/deepseek-v4-flash-latest` — a row claiming 1.3M tokens —
grew to 386,309 tokens with compaction checked after every step and never once firing, and
what came back at that size was the model's own template turned inside out rather than an
answer.

A model with a real 200,000 or 400,000-token window is not affected: only a claim above
256,000 is clamped. What the status line reports is still the model's own window, because
that line is describing the model.

**A request that would not fit is never sent.** Immediately before each request goes out,
a transcript already past the ceiling is compacted first — and unlike the ordinary pass,
this one runs **even when automatic compaction is switched off**. Fitting is not a
preference. Nothing is truncated and nothing of yours is dropped; it is the same pass
`/compact` runs, and every message you typed survives it.

**Accuracy note.** aforge also carries a shared context-budget package with a 60%-fill rule,
a 160k working set and a 250% reuse law. **That package is not used by this chat.** Its
consumer is the sub-harness leaf sizing elsewhere in aforge. The chat's own law is the one
above — do not describe this conversation as filling to 60%.

## What happens before the conversation is summarized

aforge does not jump straight to summarizing. There are rungs before it.

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
older aforge is still drawn from the shortened copy).

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

## Spending limits

A session can carry a spend ceiling. When it is set, a turn is stopped **before it starts**,
reading the session's own recorded usage — so a turn already in flight is never cut in half.
The refusal arrives as an error carrying the sentence:

```
session: the spend rail was reached
```

A ceiling of zero means there is none.

**The ceiling counts what this conversation spent before you resumed it.** The usage the
rail reads is the rebuilt total, which includes every turn from every earlier sitting — so a
ceiling reached yesterday is still reached when you open the conversation today, and the
first thing you type is refused before it runs. That is the one place the rebuilt bill
changes what happens rather than only what is printed. If you want a clean allowance, raise
the ceiling on the `session ceiling` row, or start a fresh conversation with `/new`; there
is no way to zero a conversation's recorded spend while keeping the conversation.

The settings panel's **Session** tab carries a row for it, labelled `session ceiling`. The
panel's search matches a row's registry key as well as its label, so typing either `spendRail`
or `ceiling` finds it.

**A task started from the composer carries a ceiling of its own**, whether or not this row
is set: `alt+enter` opens the composer layer, and its third line is the figure that errand
may spend before it stops and asks you. It is the same mechanism — the errand's own session
gets that ceiling — so everything above is true of it, and any adaptive run it starts is
held to a tank no bigger than the same figure. The tasks page has it in full.

## Which endpoint answers, and what it charges

One model id is served by many endpoints, and they differ in two ways at once: how fast they answer, and what they charge. The published list price beside a model is the model's own figure — no endpoint is obliged to match it, and the fastest one often does not.

So, with the **routing** row on the Providers tab left alone, aforge asks for two different things depending on who is waiting. **Your own turns** ask for the fastest endpoint, capped at **a quarter over the model's published list price**: an endpoint 25% dearer buys a head start you can feel, and one four times dearer buys nothing you would notice on a five-minute task. **Work you are not waiting on** — task workers, a divided part, the check on a piece of work, the model that names a task or a conversation, the memory pass — asks for the cheapest endpoint instead, because speed is worth nothing to a call nobody is watching.

Where a model publishes no price, no cap is sent at all rather than one guessed from something else. If no endpoint can serve a request under the cap, aforge lifts the cap rather than failing the turn, and says so on the attempt line.

Setting **routing** yourself overrides all of that everywhere: `latency` asks for the fastest endpoint (still under the price cap) for every call including background work, `price` asks for the cheapest for every call including your own turns, and `off` sends no preference and stops timing endpoints. A change lands on the next session.

## Why does the same conversation suddenly cost more? Keeping the prompt cache warm

Every request in a conversation re-sends the whole conversation. What keeps that from costing a fortune is the **prompt cache**: the endpoint that answered you a moment ago still has those tokens, and re-reading them costs a fraction of sending them fresh. The catch is that the cache sits on **one machine**. An endpoint that has never seen your conversation charges full price for all of it — measured on a real run, the same 94,000-token context cost **4.7 times more** on a cold endpoint than on the warm one, and that alone is where a quarter of the requests in that run ate half its money.

So aforge remembers which endpoint answered your last request and **asks for that same endpoint first on the next one**. It is a preference, not a demand: if that endpoint is busy or gone, the request still goes through somewhere else rather than failing. Nothing extra is sent and nothing is probed to work this out — it is the name that came back on the last answer.

It moves off that endpoint when the endpoint stops earning it, and there are three ways that happens:

- **The request failed there** — an error, a refusal, or a reply that went quiet or turned to garbage halfway through. The next request is routed afresh.
- **The cache was gone anyway.** If a long prompt comes back having read nothing from the cache, there is no warm context left to come back for, so the next request is free to land anywhere.
- **It charged too much.** The same quarter-over-list price cap described above rides on every one of these requests, and an endpoint that billed above it loses its place. A warm cache is never worth any price.

Each of your conversations keeps its own endpoint, and so does each worker on a task, because each of them is sending a different transcript. Background work is kept warm the same way: its first request still asks for the cheapest endpoint, and after that it comes back to whichever one answered. Setting **routing** to `off` turns this off with everything else.

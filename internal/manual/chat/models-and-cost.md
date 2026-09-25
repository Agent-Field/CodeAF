# Models, context, and what it costs

## Lost internet, Wi-Fi disconnected, DNS errors, and waiting for connection

When DNS or a connection attempt fails before the request is accepted, codeaf
shows `waiting for connection`. It pauses requests on that client and checks
whether the configured endpoint is reachable. This is a small request without
your prompt or API key; it does not ask a model to generate anything.

Waiting calls share a check. After each failed check, codeaf waits about one to
one and a half seconds before checking again. Each check has a two-second limit.
When the endpoint first answers, your request resumes immediately. If the check
answers but your request still cannot go out, codeaf waits a little longer
before each further try. A response proves endpoint reachability, not that every
internet service is healthy. No separate public ping service is involved.

Connection recovery waits up to two minutes, or less if that call already had
a shorter deadline. Esc or Stop work cancels your call immediately; other calls
still waiting keep their shared check. If the connection does not return, codeaf
says `connection is still unavailable; try again when connected`.

A picture, video, speech or transcription request shows `waiting for
connection` against the model it asked for, just as a chat reply does. If its
checks answer but the request still cannot go out, codeaf eventually gives up
with `connection is still unavailable; try again when connected`.

Chat, auxiliary requests, document parsing and authenticated media requests use
this recovery for pre-send connection failures. A cut-off reply still follows
the existing stream recovery rules. A lost response to an accepted media job
does not automatically submit that job again. Rate limits keep their existing
recovery policy; invalid credentials, invalid requests and certificate errors are
not repaired by a connection wait.

## Why a longer conversation does not get a full cache discount

When choosing a provider, codeaf can estimate that it still holds some of this
conversation's earlier input. That estimate is capped at the input length the
provider previously reported receiving for this conversation, and at the current
request's estimated length. An unknown earlier length earns no discount. Requests
running at the same time keep their own conversation identity and usage together.

This affects routing estimates; it is not proof of a cache hit. A provider may
evict cached input, and compaction or a rewrite can change earlier text even when
the conversation identity stays the same.

## How do I pick a different model — which model am I talking to, which model is it using right now, and how do I switch or change it

The model in use is the first thing written on the legend line directly above the message
box, with the endpoint answering for it in brackets (`glm-5.3-flash (deepinfra):high ·
◇ asks`; on a `--host` session the machine leads it). There are two doors to the picker:

- type `/model` with nothing after it, or
- press the model's name on that line above the box.

If you have turned the mouse off (`ui.mouse`), only the command works.

**The name you press is the model you move.** Out in the conversation that is the
conversation's model. Inside a running task's room the status line at the very bottom
names *that task's* model — `task <name>` — and pressing it opens the same picker aimed at that task alone,
from its next request onward — and the first time that step has to be rescued, it is
rescued onto your pick. Nothing else moves: not the conversation, not any other task.
See "Changing the model for one task while it is running" on the tasks page. Inside a
task that has finished the name is still there to read and cannot be pressed.

If the task's work is being checked when you press, there is no request left to move:
the pick is saved for the next run and the row keeps naming the model the work actually
ran on. The room shows `next model <id>` while that choice is held.

The picker is a filter box in the input line's place with a short list of models under it.
It is bottom-anchored: the conversation shrinks above it, so nothing pops up over what you
were reading.

Choosing a model does four things: the model is set on the session, the machine running
the session learns that model's context window for compaction, a note appears reading
`model · <model>`, and the choice is written into your profile.

Over `--host`, the picker and its prices are this laptop's catalog, while the context
window used for compaction comes from the far machine's catalog. The machine doing the
work owns that execution limit even when the two catalog caches differ.

## Sign in with ChatGPT and use my Codex plan — models, context window, price, limits and expiry

Open `/connect`, choose **Codex**, and finish the browser sign-in. This signs in the way
the Codex CLI does; OpenAI's terms for a ChatGPT plan apply to what runs on it. The
service reads the model list belonging to that account, and a new connection moves this
conversation to `codex/gpt-5.5`. Every model from it is qualified as `codex/<slug>`.

A Codex model's context window is the one that account's model list gives it — `272k` on
every model it lists today — so the status line reads `…/272k` on `codex/gpt-5.5` and
compaction fires from that figure. When the list cannot be reached at sign-in, the four
models codeaf already knows (`gpt-5.5`, `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`)
carry that same `272k`. Over `--host` the far machine still compacts at `272k`, but
this screen's meter reads this laptop's own list. When that list has no Codex rows, the
meter keeps the figure of the model you switched from, or shows none if the conversation
opened on Codex. The
conversation file's `call` lines name `Codex` as the endpoint that served each request.

A Codex call has no dollar price codeaf can know. The spend page therefore shows no
invented `$0.00` or unknown-price label; when the backend reports usage, it still counts
the prompt, completion, cached-prompt and reasoning tokens. The plan is paid for outside
codeaf. When its allowance is exhausted, the turn says exactly:

```
codex reached your chatgpt plan's usage limit · it resets on its own
```

An expired sign-in says:

```
codex sign-in has expired · /connect or codeaf connect codex signs in again
```

This sign-in does not add an OpenAI API key, cannot connect a custom endpoint, does not
put Codex on first-run setup, and does not replace codeaf's own instructions with the
Codex CLI's base instructions. Use the custom-service row for an OpenAI-compatible API.

## Can I switch models while it is replying — I changed the model in the middle of an answer, does it change now or wait?

**Your word wins at the next request, within a second, and never at the next turn.**

It depends on one thing only: whether the request in flight has given you anything yet.

- **Nothing has come back.** It is still reaching a machine, waiting out a pace, or
  walking away from a refusal — the screen has your question on it and nothing else.
  That request is **let go of at once** and asked again on the model you chose. Nothing
  is lost, because nothing had arrived.
- **Something has already come back.** That request **finishes on the model it started
  on**, and everything the work asks for after it is on the new model. Killing a reply
  you are reading would throw away words you have paid for and waited through.

**Thinking counts as something coming back.** If the model is showing you its thinking —
the `thought for …` line, or the thought itself open under `ctrl+e` — that is on your
screen and it is not taken away from you, so your pick rides the next request rather
than cutting this one. It is the same rule and the same reason: codeaf never withdraws
something you are looking at to obey you faster.

The same rule holds inside a task's room, where it matters most: a task step is one turn
and can run for twenty minutes, so "the next turn" would mean your pick did nothing today.
The room tells you which of the two you got — `switching now`, or `the next request takes
it`. See "Changing the model for one task while it is running" on the tasks page.

**If it was already moving, it moves to yours.** When a model stops answering, codeaf
moves the work to another one on its own — a rescue, not a preference. A model you name
while that is happening is the head of that chain: the move goes to yours, the line you
read names yours, and everything after it is read off yours. This holds even when codeaf
has nowhere of its own left to go; naming a model is itself somewhere to go, so the turn
moves instead of ending on "there is nowhere else to try".

**What it does not do.** It does not stop the turn, and it does not throw away anything
already in the conversation: a partial reply that had arrived stays where it is. It does
not reach work that has already finished — a task being checked, or one that has landed,
saves the pick for the next run instead. And it is not the same rule as the thinking
level: a level you change lands on the next thing you ask, because setting a level is not
redirecting work you are watching.

## Does codeaf remember the model I picked, or does it go back to the default?

**It remembers.** The model you last switched to is the model the next `codeaf` opens on,
whether you chose it in the picker, typed `/model <slug>`, or set the conversation row in
`/settings` — all three are the same road.

The order a launch resolves is: `--model <slug>` on the command line beats everything for
that session alone; then the model you last chose; then `CODEAF_MODEL`; then the built-in
default. `CODEAF_MODEL` seeds a model for somebody who has never chosen one and does not
override somebody who has — which is why the settings row stays editable while it is set.

Two things do not persist. Over `--host` the switch takes for the session and is not
written anywhere: the model a remote session opens on is resolved on that machine, from
that machine's profile. And reasoning effort is kept per model for the session, not
written to the profile.

esc closes the picker and **undoes nothing**. It gives your half-typed draft and the frame
back as they were — the picker holds its own filter text, and the filter is forgotten when
it closes — but the model in use does not come back: enter already switched it, then and
there, and esc is only the way out. To go back to the model you were on, choose it.

## Moving and filtering in the model picker

Type to filter. The keys:

| Key | What it does |
|---|---|
| ↑ / ctrl+p, ↓ / ctrl+n | move a row |
| pgup / pgdown | move 12 rows |
| left, right, home, end, ctrl+u, ctrl+w | edit the filter text |
| ctrl+t | walk the reasoning effort of the model under the cursor |
| tab, → | open the providers — the providers serving the model under the cursor — and move the cursor into them |
| tab, ← | close them again, back on the model |
| enter | switch to the row under the cursor — or, on an open provider, pin it — and **leave the list up** |
| esc | close it; what enter already did stays done |
| alt+s, alt+shift+s | order the list by the next column, and turn that column round |

## Why left and right arrows do the wrong thing in the model picker — the caret and the providers share one pair of keys

`→` and `←` belong to the providers **unless you are mid-typing**, where they move the caret
through the filter instead. Mid-typing means within 0.6 seconds of the last change to the box,
and every keystroke pushes that out — so the arrows are the caret's while you type and the
tree's once you stop. They are the tree's at the very end and start of the text regardless,
where there is no character to step over, which is why an empty box never waits.

**This is what made coming back out of a fold cost four presses.** With `deep` typed and the
providers open, `←` used to step through `p`, `e`, `e`, `d` before it would close anything.
Now you pause and press it once.

**Any edit takes the caret back** — a letter, `backspace`, `ctrl+w`, a paste — and so do
`ctrl+b`/`ctrl+f`, which are never the providers' keys and so move the caret without changing
your query. `↑`/`↓` do not count as editing. `tab` always opens and closes.

**This is one list with two doors.** `/model` opens it, and so does the **your model**
row at the top of the Providers tab in `/settings` — the same rows, the same name
search, the same providers under `→`, and `enter` on a provider pins it either way. The media
slots on that tab (**drawing**, **speaking**, **looking** and the rest) open the same
component over their own models, but they have no provider row behind them, so nothing
unfolds under them and the foot does not offer the key.

The cursor opens **on the model in use**, which is also the marked row, so enter with
nothing typed confirms rather than changes.

**Enter does not close the list.** It switches, the mark moves to the row you chose, and
the list stays where it is — so two models can be compared on their prices, chosen between,
and changed back without reopening anything. `esc` is the way out, and it undoes nothing:
what enter did is already done.

**The box searches the model's name and nothing else.** Filtering splits what you type on
whitespace; every word must match, each scored by the fuzzy alignment every picker on this
surface shares — a word that starts an id, or lands right after a `/` or a hyphen, outranks
the same letters sitting loose inside it. So `ds v4` finds `deepseek/deepseek-v4-flash` and
`claude 4.5` finds `anthropic/claude-sonnet-4.5`, and a tight prefix sits above a scattered
match. Twelve rows show at a time. No word means anything but itself — the speed, price and
capability terms this box used to take are gone, and the section "You cannot filter the
picker by speed, price or capability" says what to read instead.

The picker **never fetches on its own** — only when you press `ctrl+r` in it, which asks the
router for the newest list (the *commands* page, "Refreshing the model list"). Otherwise
the list comes from what is already known, in this order: the
catalog the door passed in, then `~/.codeaf/v3/models.json`, then five names this build
remembers (`deepseek/deepseek-v4-flash`, `openai/gpt-4.1-mini`,
`anthropic/claude-sonnet-4.5`, `google/gemini-2.5-flash`, `moonshotai/kimi-k3`). Each rung is
tried only when the one above it came back empty after filtering.

**The `/model` you typed stays in the box**, drawn as the chip it was, with the filter after
it: `› /model filter by name`. The list is a different box from the one you typed the command
into, and without the chip the line was a `›` and a grey phrase that could have belonged to
any list on this surface. What you type goes after the chip and the chip cannot be edited.

The placeholder in the empty filter box is the only place the overlay explains itself:
`filter by name · ctrl+r refresh` — it says `by name` because that is the whole scope of the
box, and on a frame too narrow for it the words fall back to `filter`. The keys themselves are named on the foot under the list,
where they stay while you type and say what they do on the row the cursor is on

There is no mouse commit on the picker's rows.

## What each row in the model picker tells you — the columns, and what the headings mean

The list is a **table**. The model's id is on the left under the heading `model`, and the
facts about it stand in columns, each with a dim heading that names the unit so the figures
under it do not have to:

| Heading | The column | Spelled |
|---|---|---|
| `via` | the machine that would serve it | `cloudflare` |
| `first` | the wait before the first word | `0.8s` |
| `in/M` | what a million prompt tokens cost | `$0.09` |
| `out/M` | what a million completion tokens cost | `$0.18` |
| `window` | how much it can hold | `1M`, `128k` |
| `t/s` | how fast it writes once it has started | `58` |
| `elo` | its Design Arena score | `1290` |
| `inputs` | what you can put in besides text | `image`, `image file` |
| `outputs` | what comes back besides text | `image`, `speech` |

That is also the ORDER, and it is the order the columns are given up in when the window is
narrow — `outputs` goes first, `via` last. **A column nobody on this list published is not
drawn at all**, heading and all: a catalog with nothing measured behind it shows no `via`,
no `first` and no `t/s` rather than three headings over three hundred blanks.

**An empty cell means the catalog published nothing.** Every row shows every column the
table drew, so a gap is never "it did not fit" — that is the one thing the old ragged row
could not tell you. A price shows only when both halves are known; a zero means "nobody
said", never "free".

A name that begins with `~` — `~deepseek/deepseek-v4-flash-latest` — is not a typo and not
a home folder: `~` is the router's own marker for a *floating* name, one that points at
whichever build of a model is current rather than at a fixed one. The *lanes* page, "A model
name that ends in latest, and the tilde in front of it", says what that costs and where the
speeds behind it are filed.

**The names are measured first and the columns take what is left**, so the id is never
shortened to make room for an arena score — a column goes instead. A name is shortened only
when the window cannot hold the longest one on the list, and then it loses its author first
(`nvidia/nemotron-3.5-lightning` becomes `nemotron-3.5-lightning`) — unless two models on
the list share that slug, which is the one case where the author is what tells them apart.

**Under sixty columns there is no table.** There is no second column to put anything in, so
the row falls back to the ranked tail it always drew: the facts in the same order, joined
with `·` on a line of their own under the name, each in the longest spelling that fits —
`$0.09/$0.18 per M` becomes `$0.18/M` becomes `$0.18`, and `via cloudflare` becomes
`cloudflare`. Nothing is ever half a number either way.

The heading line costs the overlay a line of its own rather than costing you a model:
twelve models still show at a time.

Only models you can hold a conversation with are listed: text in, text out. A model that
publishes `["image","text"]` out (a drawing model that captions) is excluded, and so is a
transcription model (audio in, text out). A row that publishes nothing about itself is judged
by its id against a narrow list of generation and sidecar words.

## Why the via name keeps changing on the model list

It does not, not while the list is open. `via <machine>` on a `/model` row is which
provider would typically serve that model, frozen when the list opened — so a turn
running underneath cannot make the names jump, and the `▲0.5s` and `58t/s` next to them
stay still too. Close the list and open it again to see the latest.

The `via` beside the model above the message box is a different fact: that one is who is
answering the turn that is in flight, and it is allowed to move. It carries no rate: how
fast the stream is producing stands at the right of the status line beside the state word
while the turn writes, as `38 tok/s`.

## Why does the model picker keep jumping

The `/model` list you are reading is a snapshot. Typing in the filter still narrows it;
the `via` and the speeds do not rewrite themselves while you look. A turn running
underneath can still update the status line. Close the list and open it again if you
want the latest machines.

## What the inputs and outputs columns mean — image, audio, video, file on a model row

`inputs` is what you can put into the model besides text, and `outputs` is what comes back
besides text, each in the catalog's own word:

| Word | Under `inputs` it means | Under `outputs` it means |
|---|---|---|
| `image` | it takes pictures — screenshots, photos | it answers with pictures |
| `audio` | it takes sound | it answers with sound |
| `video` | it takes video | it answers with video |
| `file` | it takes attachments, a PDF among them | — |
| `speech` | — | it answers with a voice reading words |
| `music` | — | it answers with music |

A model that takes several says them in one cell, separated by a space and commonest
first: `image audio video file`. **The order is codeaf's, not the catalog's** — the catalog
publishes the same set three different ways on neighbouring rows, so echoing it would put
one fact in three places down a column.

**`text` is never shown on either side.** Every model on the `/model` list reads and writes
it — that is what makes it a model you can talk to — so the word would be the same five
cells on five hundred rows, and what a cell is for is what the model can do **beyond**
holding a conversation. **Two empty cells therefore mean text in, text out**, which is also
what a row that published no modalities at all means.

**A word this build has never seen is still shown**, after the ones it knows: the catalog
carries `embeddings`, `transcription` and `rerank` today and will carry something else
tomorrow, and a row that said nothing about a family codeaf did not recognise would be
indistinguishable from a plain text model.

**`outputs` is usually not drawn at all, and that is not an accident of width.** A list
here is always a filtered view of one catalog, and what each list filters on is a
modality — so a modality column can end up saying the same thing on every row, which is
the list's own definition written out once per row rather than a fact about any of them.
Where that happens the column is dropped, head and all:

| List | What `outputs` would say | Drawn? |
|---|---|---|
| `/model` | nothing, on every row — a model you can converse with answers in text and nothing else, so a drawing model that also captions is off the list entirely | no |
| **drawing** | `image`, on every row | no |
| **speaking** | `speech`, on every row | no |
| **filming** | `video`, on every row | no |

`inputs` survives the same test on most lists because it genuinely varies: on `/model` it
has eleven different values across three hundred-odd models, and in the **filming** slot
some models take a picture to animate and some take a clip.

**This is only asked of `inputs` and `outputs`,** because they are the only columns a list
is ever chosen by. A price or a window that happens to be the same on every row of a short
list is a coincidence, not a definition, and those columns are always drawn.

**The cells report only what was published — they never read the id.** A model whose name
says `vl` or `vision` but whose catalog row lists no modalities draws two blank cells,
because a cell is a fact about the catalog and not a guess about a name. The **looking**
slot does fall back to those two words in a name when a row published nothing, so a silent
`…-vl` row can be offered there while showing nothing under `inputs` here. The two are
asking different questions: the cell says what is known, the slot has to decide whether to
offer the row at all.

**On a line with no heading over it the side is spelled out.** `codeaf models`, a frame too
narrow for the table, and a phone all draw the facts as a `·` tail instead — `inputs image
file · outputs image` — and a plain chat model says nothing there either.

## Older names for these — sees, draws, speaks, films, hears, watches, and the reads and makes columns

Rows used to carry one invented verb per modality — `sees` for image in, `hears` for audio
in, `watches` for video in, `draws` for image out, `films` for video out, and `speaks` for
all three of speech, audio and music. They are gone from every row: a verb had to carry
the side as well as the thing, which is six words to learn before a row could be read, and
`speaks` folded three different kinds of product into one word.

For a short while the two columns were headed `reads` and `makes`. They are `inputs` and
`outputs` now.

**None of them survives anywhere.** `sees` and `draws` outlived the rest for a while as
filter words in the model picker's box — typing `sees` kept the models that read images —
and that box now searches names only, so `sees` is four letters to look for like any other.
It still finds `deepseek/deepseek-v4-flash`, because those letters run through that id in
order; it no longer finds a model because of what the model can see.

## Switching model by name in one command

`/model <slug>` switches straight to that slug — no list, no confirmation.

The words after `/model` are read for their **shape**, not for a flag:

| What you type | What happens |
|---|---|
| `/model` | the picker opens |
| `/model deepseek/deepseek-v4-flash` | switches to that slug |
| `/model @cloudflare` | pins the provider that serves your model — the model does not change |
| `/model auto` | gives the choice of provider back to codeaf |
| `/model deepseek flash` | opens the picker with `deepseek flash` already in the filter |

**Anything with a space in it opens the list already narrowed**, because a slug has no
spaces in it — so two words were never a name, and the two sensible words to do with them
are to search names with them. A **single** word is always taken as a slug. Until
2026-09-17 a single word the picker's filter grammar recognised (`fast`, `cheap`, `tools`,
`<1s`, `>50t/s`, `$<0.3`, `fp8`) opened a narrowed list instead; that grammar is gone, and
`/model fast` is now a request to switch to a model called `fast`.

There is one check, and only one. If the slug **is** in the catalog and cannot hold a
conversation — a drawing model, a speech model, a transcriber — codeaf refuses in one line
and the conversation does not move:

```
openai/gpt-4o-mini-tts cannot hold a conversation — it answers with speech. Still on moonshotai/kimi-k3.
```

A slug the catalog has never carried is still **taken at its word**, exactly as before:
codeaf may be offline, or you may be naming a model this build has never listed. In that
case the context window is left alone.

## I changed the model but my task is still on the old one — change the model inside a task

In the conversation, `/model` changes the model you talk to. Inside an ordinary task, `/model` opens the picker for **that task only**, and
`/model <slug>` changes that task. Clicking its model in the status line or
**Task setup** opens the same picker. A filtered `/model` search keeps that
same task scope. The change takes effect at the task's **next request**, which is
within a second of your press and never a whole turn — a task step is one turn
and can run for twenty minutes. If the request the step is inside has given you
nothing yet — still reaching a machine, waiting out a pace, walking away from a
refusal — that request is let go of at once and asked again on the model you
chose, and the room says `switching now`. If anything has come back, including
thinking you can see, it finishes on the model it started on and everything
after it is on the new one, and the room says `the next request takes it`. Other
tasks and the conversation stay as before.

For a completed, incomplete or `your call` ordinary task, the picker saves the
model for when you continue. It does not restart work or change the completed
attempt's recorded model. A queued task takes the choice when it starts.

An adaptive run or its nodes, and a task being read through
another conversation cannot use this model-changing door. `/model` says
`this task's model cannot be changed here` instead of changing the conversation
behind that page. Provider pinning with `@provider` or `auto` remains available
from the conversation's `/model`.

Changing the conversation model does not move existing tasks. New tasks resolve
their model from an explicit choice, the task model setting, the crew's worker
class, then the conversation model when that worker class is blank.

## The crew — which models codeaf uses on my behalf, and /crew

codeaf makes calls you did not type: naming a session, naming a piece of work on the roster,
the brief a task opens on, the safety gate, the check on finished task work, the second
look before a task starts itself, the reading of a task's parts before they are handed out,
the planner of an adaptive run and the nodes under it, the designer of a saved harness page,
looking at an image. Each of those is a
**role**, and every role sits on one of five **classes** — the **crew** — which you set in
`/settings` → Providers, or in one word with `/crew`:

- **reflex** — near-free · reads every turn — memory, titles, safety.
- **small work** — cheap · the small calls — names, digests, the safety gate.
- **worker** — does the work · every task you hand off, the parts it divides into, every
  node of an adaptive run. Most of what a task costs is spent here.
- **careful work** — careful · checks what must not be wrong — audits, briefs, vision.
- **mastermind** — thinks · plans runs and designs harnesses.

**All five arrive with a model already in them**, and on an account that is not
known low on OpenRouter credits the five together are the `balanced` preset.
While the account is known low, unwritten table seats use the free crew described
in *OpenRouter credits and free models*, and `/status` calls that crew `free`:

| class | as shipped |
| --- | --- |
| reflex | `google/gemini-2.5-flash` |
| small work | `deepseek/deepseek-v4-flash-0731` |
| worker | `z-ai/glm-5.3-flash` |
| careful work | `anthropic/claude-fable-5.1` |
| mastermind | `anthropic/claude-opus-5` |

None of them is the model you are talking to. A crew that followed your conversation would
put the most expensive model in the build on the cheapest questions in it — a call made
twice every turn on a frontier model is a bill nobody agreed to. That is also why the two
seats that read every turn stay on near-free models while the frontier ids sit on the seats
that answer a handful of times. You can pin any vendor's model on any row yourself, and you
can connect that vendor as a direct service; the
[services page](services.md) explains its names, limits and missing Phase 1 cost record.

**The `model family` row** (`models.crew.source`) decides which family
the three preset words draw from. `all` is the default and is the table above: the whole
catalog, closed and frontier models included, costing what those models cost. `open` reads the
same three words, `frugal`, `balanced` and `max`, off the open-weight rows only, so no seat is a
bet on one vendor's pricing. Flip the row and the seats nobody pinned move with it at once,
because an unwritten seat is the default crew resolved in the family you are on; the rows already
on disk keep their ids until you pick the crew again, and the crew word reads `custom` while they
match no preset in the family you flipped to.

Under `open`, the same three words resolve to these:

| class | frugal | balanced | max |
| --- | --- | --- | --- |
| reflex | `mistralai/mistral-nemo` | `mistralai/mistral-nemo` | `mistralai/mistral-nemo` |
| small work | `deepseek/deepseek-v4-flash-0731` | `deepseek/deepseek-v4-flash-0731` | `deepseek/deepseek-v4-flash-0731` |
| worker | `z-ai/glm-5.3-flash` | `z-ai/glm-5.3-flash` | `z-ai/glm-5.3` |
| careful work | `z-ai/glm-5.3-flash` | `moonshotai/kimi-k3` | `moonshotai/kimi-k3` |
| mastermind | `z-ai/glm-5.3-flash` | `z-ai/glm-5.3` | `z-ai/glm-5.3` |

The worker column is the dial in that family — it holds `glm-5.3-flash` through `balanced`, and
`max` moves it to `glm-5.3` — because it is the seat that pays most of a task's bill and a
preset that moved every other seat would change everything about a task except its cost.

**Why these ids.** Every row of the catalog was placed on two axes on 2026-09-16: the bill a
seat's own call shape runs up, built from the catalog's published prompt, completion and
cache-read prices, against that seat's quality, taken from its published intelligence, coding
and agentic indexes. The call shape is part of the price, so the seats were priced apart — the
worker and the careful seats as long cached loops, where a large prompt is read back turn
after turn and the cache-read price carries most of the weight, and the mastermind as one-shot
calls, where the prompt is paid in full each time and there are few of them. Each preset then
takes, for each seat, a point on the pareto front of that plot at the bill it is willing to
run: nothing on the front costs less at the same quality, and nothing at the same bill scores
higher. The two families are the same plot over two sets of rows, which is why the columns do
not climb together in either of them.

In the `all` family the worker stays on `glm-5.3-flash` through `balanced`, because the worker
seat carries most of a task's tokens: a step there multiplies through the whole bill, where a
step on the careful or the mastermind seat is paid a handful of times. So `frugal` to
`balanced` spends on those two low-volume seats, and `max` moves the worker itself. The
careful and mastermind seats are settled by `balanced` and stay there through `max`.

The careful class always sees images — the vision role rides that row — and is a different
vendor from the worker in every preset of the `all` family; the open family's `frugal` row
is the one standing exception, with worker and careful both on `glm-5.3-flash`, because at
that bill the open-weight front has no second vendor for the careful seat. The small-work row is pinned to
the July build of DeepSeek V4 Flash on purpose: the bare `deepseek/deepseek-v4-flash` id
resolves to the April build, and the July build costs the same.

**Clearing a row is still an answer.** A class you empty on purpose reads
`follows the conversation`, and every role on it runs on the model you are talking to. That
is the only way to say "use my model for this", and it is deliberately something you have to
say rather than the default.

### The three presets

These are the three in the family you are on unless you changed the `model family` row — the
default one, `all`:

| | frugal | balanced | max |
| --- | --- | --- | --- |
| reflex | `gemini-2.5-flash` | `gemini-2.5-flash` | `gemini-2.5-flash` |
| small work | `deepseek-v4-flash-0731` | `deepseek-v4-flash-0731` | `deepseek-v4-flash-0731` |
| worker | `glm-5.3-flash` | `glm-5.3-flash` | `glm-5.3` |
| careful work | `qwen3.8-max-0902` | `claude-fable-5.1` | `claude-fable-5.1` |
| mastermind | `glm-5.3-flash` | `claude-opus-5` | `claude-opus-5` |

- **frugal**: glm-flash works and thinks, qwen-max checks
- **balanced**: glm-flash works, fable checks, opus thinks
- **max**: glm-5.3 works, fable checks, opus thinks

The reflex and small-work columns never vary — they are the
same near-free models in all three — so `/crew` never names them: the confirmation and
`/status` say **brain**, **hands** and **checks**, which are the mastermind, the worker and
the careful class.

`/crew` opens all three as a chooser with yours marked, under a scope line — `the five
models codeaf uses on its own behalf — not the one you chat with` — and a `you talk to ·
<model>` line naming the seat the presets do not touch. ↑ / ctrl+p and ↓ / ctrl+n move;
enter applies and esc cancels. If the five classes make a custom crew, no row is marked and
the chooser says picking one puts all five back. `/crew max` still sets it directly and
confirms in one line, which ends `· you are still talking to deepseek-v4-flash — /model
changes that` — naming the conversation's own model by id, because the crew changes
nothing about it and the model segment on the status line goes on saying what it said
before. `/crew learn`, `/crew catalog` and `/crew table` move the **picked from** row and
nothing else — the three words the row takes, answered without touching a model id — and
when the pick is off `table` the crew word says so beside the preset: `balanced · learn`
in `/status`, `crew balanced · learn` on the status line. The **crew** row in `/settings`
→ Providers is the same thing: enter or space walks it frugal → balanced → max, and the
**picked from** row under it walks table → catalog → learn.

**The crew row is not stored by codeaf — it is worked out from the five.** Answer any one
of the five rows yourself and the crew row reads `custom`, because that is what is true.
`/crew balanced` puts all five back in one write. A profile that applied a crew before the
worker row existed reads `custom` until a preset is applied again, because its four old
rows and the new fifth are not any of the three. A run that writes the crew word into the
profile itself — nothing this build does, but a harness or a hand edit may — is read as
the budget its seats run at, and the class rows under it are that run's own pins.

### Where the seats are picked from — table, catalog, learn

The **picked from** row (`models.crew.pick`, just under the crew row) says where the
seats' models come from when a class row does not hold a model id of its own. The crew
row above it still says how much to spend; this row says where the models for that
money are read from:

- **table** — the rows we measured: the ids this build shipped with, the same ones every
  preset table holds. This is the default, and it is what an unwritten class has always
  read.
- **catalog** — recomputed from today's published prices and scores at your crew's
  budget, on every read. Nothing is stored; a catalog that moves moves the seat with it,
  and your profile never holds a model id this build chose for you.
- **learn** — the catalog computation plus the Model Pool's measurements and your own
  judged runs, carried as a quality rating the better-measured models read on top of
  their published scores.

`catalog` and `learn` are Pareto crewing: the crew is picked on the cost-quality front,
per role and per task, from evidence rather than from a fixed table.

A pick moves the three seats the presets dial — **worker**, **careful work** and
**mastermind** — and never the two that read every turn: **reflex** and **small work**
keep their near-free ids, the same ones in every preset.

**A model you typed by hand wins.** The pick answers for the seats nobody named. A class
row YOU wrote keeps its model, spelled as you typed it — the crew table's own id included
— and a `models.crew` word stored in the profile marks every class row beside it as yours.
`/crew balanced` writes all five classes at once, so it is the preset answering rather
than a pin and the pick computes its three dial seats. **A flag or an environment variable
still outranks the pick** — the pick reads the profile, and `--model` and `CODEAF_MODEL`
are what an invocation said.

When the catalog cannot compute a seat — no catalog yet, or no pick off its front — the
seat falls back to the table row for your preset, never to `auto` and never to empty. A
seat the pick computed names it where the preset would be: `crew balanced, computed
from the catalog` under **catalog**, `crew balanced, learned` under **learn**.

**The per-seat alias is the bare word `auto`.** Any of the five class rows may hold the
bare word `auto` instead of a model id, case folded. The seat's model is then
**computed from the catalog** — the three published capability indexes against the three
published prices, under that seat's own call shape — every time the row is read. The
word stays on disk; the id is worked out on every read.

**The budget it computes at comes from the other four rows.** `auto` has no opinion about
cost of its own, so it runs at whatever preset the rows around it name: four rows that are
`balanced`'s make an `auto` row a balanced seat, four that are `max`'s make it a max seat.

**When the other rows match more than one preset, `balanced` wins.** It is the default
preset, and the budget an undecided profile runs at is the budget an `auto` seat runs at.
This is not a rare corner: `max` differs from `balanced` only in the worker seat, so a
crew with `auto` on the worker and the other four rows as shipped matches both, and reads
as `balanced`. Pin the worker to `max`'s own id and put the `auto` row on a seat above it
if you want a computed seat at `max`'s budget.

A seat on a computed row names both facts where a seat is shown — `crew balanced,
computed from the catalog` — so the reading is never a guess.

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
| `auditor` | careful work | whether finished-looking work is actually finished |
| `vision` | careful work | reads images for a model that cannot see them |
| `shaper` | careful work | the brief a task you started yourself is given |
| `careful` | careful work | a part of a task that needs judgement |
| `repair` | careful work | the second go at work a check found gaps in |
| `planner` | mastermind | the plan that steers an adaptive run |
| `designer` | mastermind | writes and reviews a harness page |
| `routerconfirm` | mastermind | a second look before work starts itself |
| `markreader` | mastermind | what is left of an answer that is being taken out of your hands, drawn as parts |
| `handoff` | mastermind | the instruction a handed-over turn gives whoever finishes it |
| `division` | mastermind | the parts a worker hands its own work out in |

The list is built from what is registered in the running binary, so it is the truth about
this build rather than a table someone kept up to date. Stop on a row and the line under the
list is that role's own description followed by which class it follows.

**What the mastermind's roles have in common is that one answer decides what all the other
calls do.** `planner` and `designer` used to sit on careful work beside the check on
finished work, which made one model id answer two unrelated bills: the careful calls are
many and short, and these are few. A planner that cuts badly spends a whole run on work nobody wanted;
a designer that writes badly puts a wrong answer on the menu with a name on it;
`routerconfirm` stands between a cheap model's "that should have been work" and a task
starting itself, and it is asked on nothing else, so it costs a call only where something was
about to be spent; `division` reads a task's parts before any of them exists, and every turn
every part ever takes runs on the brief it leaves behind.

`markreader` and `handoff` are the two calls a long answer makes (*Tasks*). `markreader` is
asked **at most once during an answer** — only when that answer can no longer work where it
is: its context full, the loop watch already spent, or the wall run out — plus once at the end
of any answer that touched a tool at all. It reads the account of the work and says what is
left of your question. **It runs beside the work rather than stopping it**: the next step of
the answer goes out immediately and the reading happens alongside it. What it draws is the
list the work carries on with; the step it lands beside is stopped either way, because the
decision to stop was taken before it was asked. It used to be awaited, and a measured one held
the work for 8.1 seconds to decide nothing. **A long answer no longer buys one of these
every ten rounds**: the two earlier moments cost no call at all now — codeaf tells the model
what its answer has run up and the model decides for itself (*Tasks*, under *An answer that
runs long is told*). `handoff` writes the instruction the task
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
`· you are still talking to deepseek-v4-flash — /model changes that`, and `/status` prints
`model` and `crew` on neighbouring lines so the two dials read as two.

The crew is a different dial: the five **classes** codeaf makes its own calls on — reflex,
small work, worker, careful work, mastermind — used for titles, memory, the safety gate,
the work inside every task, checks on finished work, the brief a task is shaped into, adaptive-run
planners and their nodes, harness pages, and looking at an image. Setting it writes all
five class rows in one write, and **it is live from that moment**: the next call codeaf
makes on its own uses the new crew, with no relaunch and no new session. A task already
running keeps the model it was admitted on.

**Where to read the crew back:**

- `/status` prints a `crew` line directly under `model`:
  `crew     max · brain claude-opus-5 · hands glm-5.3 · checks claude-fable-5.1`. The word is
  the preset, or `custom` when the five classes are your own arrangement. **brain** is the
  mastermind, **hands** is the worker, **checks** is the careful class.
- `/settings` → Providers has the **crew** row above the five class rows.
- Bare `/crew` opens the three presets with yours marked, under a `you talk to · <model>`
  line naming the seat they do not touch.
- `/status` and the phone status sheet say `crew max` on their own line — the status row
  itself stopped carrying the crew word on 2026-09-09; it is a setting, not a measurement.
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
chatting on, and `/crew max` moves the next task onto `z-ai/glm-5.3`. While a known
OpenRouter balance is low, an unwritten worker row uses the free worker model
instead; a crew or worker row you chose keeps its model. The task's row on the
roster, its room's status line and its finished card all name the model it actually ran
on. The worker of an adaptive run's nodes is the same seat, and so is the work model of
`codeaf do` — one row, every door.

This is new: until the worker seat existed a task rode the model you were talking to, and
the crew moved everything about a task except its cost.

## Does my crew reach codeaf do, or only this conversation — what models a headless run uses

**It reaches both.** A crew you set here is the crew a run started from a script or a
terminal uses — `codeaf do`, `codeaf exec`, `codeaf run`, `codeaf plan new`,
`codeaf plan revise` and `codeaf plan run`. Set it once with `/crew frugal` and the same policy holds
whether the work is asked for here or run with nobody watching.

Those runs seat two models, and each one is resolved the same way. The first of these that
answers wins:

1. a model named on the command line: `--model` for the work, `--plan-model` for the
   planning, and `--check-model` for the checks;
2. `CODEAF_MODEL`, `CODEAF_PLAN_MODEL`, or `CODEAF_CHECK_MODEL` in the environment.
   Without its own pin, a check rides a plan seat pinned by flag or environment,
   so a third model never arrives from the profile;
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
models: work z-ai/glm-5.3-flash (crew frugal) · plan z-ai/glm-5.3-flash (crew frugal)
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
your crew was set before the work seat existed · it is running on your small work seat's model until you pick a crew with /crew in the conversation
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
your crew was set before the work seat existed · it is running on your small work seat's model until you pick a crew with /crew in the conversation
```

**It is said once per session**, when work actually starts, and never per task or per part.
Twenty tasks in one sitting is one line. Start codeaf again tomorrow with the same profile and
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

The word `inherited` is the same word `codeaf do` prints beside the model on its `models:`
line, so the two surfaces are telling you about one thing.

## What are the six models — the one you talk to and the five crew seats

codeaf runs **six model seats**. **Seat one is the model you talk to**: it answers every
message you type, it is the id written above the message box, and `/model` is the only thing
that moves it. The other five are the **crew** — the models codeaf uses on its own behalf,
for calls you did not type:

| seat | word | what it answers |
| --- | --- | --- |
| 1 | you talk to | your messages — set with `/model` |
| 2 | reflex | memory, titles, the safety gate — near-free, reads every turn |
| 3 | small work | digests, task names, the safety gate's yes-or-no — cheap |
| 4 | worker | every task you hand off, its parts, every run node — most of the bill |
| 5 | careful work | checks on finished work, the brief a task is shaped into, vision |
| 6 | mastermind | plans adaptive runs and designs harnesses — thinks |

`/crew` shows all six and sets seats two to six in one word — `frugal`, `balanced` or
`max` — and never seat one. Bare `/crew` opens with `you talk to · <model>` above the three
presets, so the seat the presets do not touch is on the same page as the ones they do.
`/settings` → Providers pins any one of the five on its own, which turns the crew word to
`custom`. Seat one is the model named above the message box; the other five are the
`crew` line of `/status`.

## Does /crew change my chat model — no, and what crew max on the status line means

No. `/crew max` moves the five crew seats and leaves the model you talk to exactly where it
was. The confirmation names it:

```
crew → max · brain claude-opus-5 · hands glm-5.3 · checks claude-fable-5.1 · you are still talking to deepseek-v4-flash — /model changes that
```

`/model`, `/model <name>` or the model row in `/settings` are the only ways to change the
chat model, and `/crew` never offers to. The two dials also stay separate on the frame: the
chat model is written above the message box, and `crew max` — or `crew balanced`,
`crew frugal`, `crew custom` when you pinned a seat yourself — is a line of `/status` and of
the phone status sheet. It is not on the status row: a setting is not a measurement, and
the row is for numbers now. `/status` prints `model` and `crew` on neighbouring lines at any
width. The one session with no `crew` line at all is a **remote** one opened with
`--host`: that crew lives on the other machine.

## Asking a class to think harder — a level on a class value

A class value may carry a thinking level as well as a model:

```
moonshotai/kimi-k3:high
```

`:low`, `:medium` and `:high` are the three. No shipped crew preset adds one; a level travels only when you add it.
The level is not part of the model id. It travels as its own request option, exactly as
the picker's **ctrl+t** effort does, so the example sends model `moonshotai/kimi-k3` and
asks for high thinking separately.

- **Any of the five class rows takes one**, though the mastermind is the one it is for. On
  the worker row in a conversation it reaches the one-shot role calls only, never the work
  inside a task. At a headless door — `codeaf do`, `exec`, `plan` or `run` — that row fills
  a seat instead, and every request the seat sends carries its level.
- **Any other suffix is refused**, in words: *"off" is not a thinking level. Add `low`, `medium`,
  `high` to a model id, or leave the level off*. It is a different request shape — it asks the
  provider to suppress thinking outright — and some endpoints refuse it. `:max`, `:none`,
  `:xhigh` and the other near-misses are refused the same way. That refusal is about this
  notation alone — the effort ladder has rungs called `xhigh` and `max`, and they are a
  separate thing from a suffix on a class value (see *Making the model think harder, deeper,
  or less*).
- Where a level is set, the role rows print it after the id, `kimi-k3:low`, which is the
  same notation the model picker and `/status` use.

The **mastermind** row is a text box because a picker hands back a bare id, while this row may
hold an id with a thinking instruction on it.

## Why is my crew thinking at low — the pin is being ignored, effort=low in the log

A level written onto a class value is a pin, and it reaches the wire on **every** request the
seat that holds it sends — the conversation's one-shot role calls, and every call of a
`codeaf do`, `exec`, `plan` or `run`. It is not a preference something further in gets to
reconsider.

So a row in the model-call log that reads a level you did not ask for has one of two
explanations, and the row says which. `codeaf logs` prints the word that **actually
travelled**, and where something overrode the pin it prints `pinned <word>` beside it — the
level that did not go out. A row with no `pinned` word is a row where the pin travelled, and
that is almost all of them.

Two things can displace a pin, and both name themselves that way. A **reflex** call disables
thinking because its answer cap is tiny and a thinking pass would leave no room for the
answer. And a model whose published row says it takes no reasoning knob at all is sent none,
pin or no pin — a knob that breaks the call is worse than a knob that did not travel.

One reading that is *not* an override: a model that cannot have its thinking switched off is
sent the lowest level it offers instead of a switch-off it would refuse, so `off` becomes
`low` on the wire and the row says `low` with no pin word beside it. That is the section
*A model that cannot stop thinking*.

## Why reflex has its own model class

`reflex` is the only role called **twice on every message** — once
beside your own model's first request, to pick which remembered lines belong in this one, and
once after, to decide whether the exchange held anything worth keeping (what-i-remember).
**Neither of them is ever waited for**: the first used to be, and the answer to every message
was held behind it for a measured mean of 4.3 seconds. Both now run alongside your answer,
and what they find is applied to whichever step of the answer is still ahead of them. That is
why it has a
class of its own rather than sharing "small work", and why it is the one row where a large
model is an expensive mistake rather than a preference. Both calls are folded into the
session's total, not into the message that triggered them, so `/cost` includes them
without any one message reading as three times the price of its neighbours.

## Which model the vision role uses

For `vision`, the **looking** row further down the Providers tab is the front door
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
harnesses" is a sentence codeaf acts on — it looks the row up with `settings` and writes it
with `change_setting`, into the same `models.roles` row, after asking you. The five class
rows (`models.tiers.reflex`, `models.tiers.low`, `models.tiers.worker`, `models.tiers.high`,
`models.tiers.mastermind`), the crew word (`models.crew`) and the pins are all writable that
way; only the role **slots** further down the Providers tab are not, because those are
bindings the running session holds rather than values in your profile.

So the ladder for any role, most specific first: **its pin**, then **its class's model**,
then **the model you are talking to**.

Two things worth knowing:

- **A change is live.** The next call codeaf makes on its own uses it — whether you changed
  it in the panel, with `/crew`, or by asking. It used to land on the next session, and it no
  longer does. A turn already in flight finishes on what it started with: nothing you change
  lands in the middle of one. The one thing that *can* move a turn mid-flight is codeaf
  rescuing it from a model that has stopped answering — see *The model went quiet*.
- Naming a model in the sentence outranks all of it for that piece of work. `make a
  harness for triaging flakes with opus` designs on opus. The roles decide only when you
  named nothing. (There is no such sentence for an **adaptive run**: a conversation cannot
  start one at all — see *adaptive runs* — so a run's models are whatever started it.)

## Does codeaf learn which model is good at which kind of work — codeaf models, ratings, why a part ran on the careful model by itself

Yes, from the checks it was already running. **Every task that settles is written down**:
the model it ran on, the name the work was given, how it ended in plain words — `landed`,
`not accepted`, `did not finish`, `your call`, `stopped` — how many times the work was handed back, what
it cost and how long it took. The check at the end of a task had already read the work and
said whether it holds, so that answer *is* the grade: **nothing extra is spent, and no
second model is asked to judge anything.** Work nobody could check teaches nothing, which
is the honest answer rather than a guess.

`codeaf models` in a terminal is where you read it back. It prints a row per model per
kind of work, with the rating, the chance of it holding, and how many settled tasks stand
behind the number — that last column matters, because a rating with two behind it and one
with two hundred are different claims. A row that is not yet driving anything says so at
the end of the line: `under the gate — a part moves up once 2 of this kind have settled`.
Until something has settled at all it says
`nothing measured yet. Ratings appear once calls have been graded.`

**What codeaf does with it** is one thing only: when a task splits itself, a part the
worker called ordinary work is minted on your **careful work** model instead if work
named like it has been turned down twice or more on the model the task is on. That is the
whole of it — no model is ever swapped out from under you, your chat model is untouched,
and an install with no crew classes set never lifts anything, because there is nowhere
dearer to lift it to. *Tasks*, under *When a task turns out to be too wide for one
worker*, has the rest.

The record lives with your settings, in `router-ledger.json` and `router-events.jsonl`.
Several codeaf windows write to it at once and it is kept across restarts.

## What happens when a crew model is down, or a pinned model stops answering — the ladder falls through one rung

The calls codeaf makes on its own — the session's name, the two or three words a task is
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
the guardian answers in ten seconds or not at all, the memory lookup moves to another machine
after **two seconds** and stops after **eighteen**, and the two readers at the end of an
answer get **20s** and **30s**. What this replaced was the ordinary
five-to-fifteen-minute bound a completion carries, which is right for your own turn and
absurd for eight words of title.

## Use one model for everything for one run — `--one-model`, and why a run spent money on a model I did not pick

`codeaf chat --one-model` and `codeaf resume --one-model` run **every text call on the model
you are talking to**, for that session only.

Without it, the calls codeaf makes on your behalf go to the crew, which is the point of the
crew — but it means a session started with `--model X` did not spend all of its money on X.
Measured on one trivial task: 22% of the dollars went to a model the run never named. That is
correct behaviour and a surprise to anyone reading a bill, so this is the flag for the case
where **one model has to answer for the whole run** — comparing two models against each
other, timing a benchmark cell, or attributing a cost.

It settles four things on your model: the five crew classes, any role you pinned, the model
that work leaving the conversation runs on, and the fallback chain codeaf would otherwise
move to when a model cannot answer. Under this flag **nothing hops** — not on a refusal,
not on a reply that keeps stalling, not on rate limiting that will not clear — because a
run whose cost is being attributed to one model cannot have finished a single reply on
another. That includes the catalog's own guess: with no `fallback models` row written, an
ordinary run falls back to the nearest same-class model, and this flag withholds that too.

**The two calls that ordinarily refuse the conversation's model ride it too.** The reader that
decides whether a long answer is moved to a task, and the writer of the brief that task opens
on, normally run on the thinking tier and on nothing else: with no crew they are skipped
rather than handed to the model that just wrote the answer. Under this flag they run on your
model like everything else, because you have said your model is the crew. Without the flag and
without a thinking-tier row, a move that needs them says `no second model is set`.

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

## What the screen says under `--one-model` — why does the status line say one model, where did my crew word go, no crew receipt when a task starts

**The crew line names the flag, because the flag is what seats the call.** Under
`--one-model` the crew line of `/status` and the phone sheet reads `one model` rather than the preset your four
rows derive to, and `/status` answers its crew line with `one model · every call rides the model
you are talking to`. The model picker's hint slot and the welcome line under the wordmark say
the same word. All of them read one answer, so none of them can disagree with another.

**Your crew word is not gone, it is overridden.** The rows are untouched on disk — this flag
writes nothing — and the next session started without it draws `crew balanced`, `crew max` or
`crew custom` again exactly as before. What the flag refuses to do is print a crew that is not
seating anything this run.

**And no crew receipt is posted.** The line a profile older than the work seat ordinarily gets
when its first task starts — `your crew was set before the work seat existed · it is running on
your small work seat's model until you pick a crew with /crew in the conversation` — is not said
under this flag. That line
reports a substitution, and under the flag there is none: every call is already on the model
you are talking to, which is your own answer to the question it asks. Picking a crew would not
change what runs, so the sentence is not offered, and the `/crew` sheet says nothing about an
inherited work seat either. Without the flag, the same profile draws `crew custom` and says
that line once, exactly as it always did.

## What temperature does codeaf use — sampling settings like temperature, top-p and seed

**None of its own.** No call codeaf makes sets `temperature`, `top_p`, `top_k`, a seed
or any other sampling knob — the request simply omits them, and the provider's own
default answers. OpenRouter passes an absent sampling parameter through as absent rather
than substituting a value of its own, so what you get is whatever the endpoint's model
ships with.

There is no setting and no flag to change this. If a reply reads as too predictable or
too wild, the dials codeaf does have are the model itself and how hard it thinks (the
effort rungs below).

## Reasoning effort — making the model think harder or faster

Reasoning effort is set in the model picker with **ctrl+t**, on the model under the cursor.
Each press walks it round: `auto → low → medium → high → xhigh → max → auto`. `auto`
hands the dial back to whatever is under it.

- On a model whose catalog row does not accept a reasoning knob, ctrl+t does **nothing at
  all, silently** — the level would be a 400 at the next turn. A row that published nothing
  counts as "does not accept", so the knob is missing rather than offered wrongly.
- The level lives on the session, **per model id**. It survives switching away to another
  model and back. `/new` forgets it.
- Where the level is set, it is shown after the id as `<id>:<level>` — in the picker row, and
  on the `model` line of `/status`. It is **not** spelled that way on the line above the
  message box: that line carries one thinking rung, and that rung is the resolved one —
  the level you dialled here is folded into it, because a level on the model wins.

**A level set here wins over everything else that asks for thinking.** It is the most
specific thing anybody said about how hard this model should work, so it beats the
conversation's own rung, a task's rung and the **thinking** default — see *Making the model
think harder, deeper, or less*. This is the same walk the task control and the settings row
use, and all five rungs are reachable here.

## Making the model think harder, deeper, or less — the effort ladder from low to max

How hard the model thinks is one dial with five rungs, cheapest first: `low`, `medium`,
`high`, `xhigh`, `max`. There is also **auto**, which is the dial left alone — codeaf asks
for nothing and the model thinks however it thinks. Auto is the **shipped** setting, and
`auto` is what the line above the message box reads until something is dialled; no rung
is the shipped one.

**The default is `auto`.** It is the **thinking** row in `/settings`, among the model rows
beside the model you talk to, and its choices are `auto, low, medium, high, xhigh, max` —
the same six the `/effort` ladder offers this conversation. The row is written to the
profile as `effort`. Existing explicit settings remain in force.
Move it down to make the model think less, which is what gives you faster and cheaper
answers; move it to `xhigh` or `max` when you would rather wait and get the careful one.

Several things can name a rung, and the most specific one wins:

1. **The level dialled onto the model in use** — the model picker's **ctrl+t**, or
   `--reasoning` on the command line. It beats everything under it.
2. **This conversation's own rung.** It is sticky: it is kept in the session's own
   `meta.json`, so it is still there after you close codeaf and come back.
3. **The piece of work's own rung** — a task carries one in `tasks.json`, and a standing
   item carries one as its `does.effort`.
4. **What the call is for.** A standing item's firing and the sentinel check in front of it
   take the item's own rung, and ask for nothing at all when the item has none — nothing
   else on this machine reaches them. The errands codeaf runs beside your turn — naming a
   conversation, summarising it, judging where a request belongs — ask for nothing whatever
   anybody set. Your own turn, and the task workers you hand work out to, take the default.
5. **The default** — the **thinking** row, which is `auto` until somebody chooses otherwise.

**`alt+e` moves the rung of whatever you are standing on.** In the message box it moves
**this conversation's** rung, which is named on the line above the box, beside the model:
`glm-5.3-flash:high`. On a task — the roster row under the cursor, or the page you are
inside — it moves that task's rung. On a standing item's card it moves that item's. A
conversation's rung, a task's rung, and the level `ctrl+t` dials onto one model in
`/model` all climb one step each press and come back to `auto` off the top — that is how
you hand this chat, this piece of work, or this model back to whatever stands above it.
(This conversation's rung joined them on 2026-09-15; its wheel had five stops until then
and wrapped from `max` to `low`.) `/effort auto` and the top row of `/effort` still clear
this conversation in one move from any rung. A standing item's rung is the one that never
walks back to "nobody said": it is cleared in the item's own document. The **thinking**
row in `/settings` stays what it is: the answer for every conversation that has not been
dialled by hand. The keys page has the whole of it — see *The thinking chip above the message box* and *alt+e — how hard the
thing you are looking at thinks*.

**Three doors, one rung.** `alt+e`, a press on the rung itself, and `/effort`:

| What you do | What happens |
|---|---|
| `alt+e` | one step up the ladder, and back to `auto` off the top |
| press the rung on the line above the box | the same one step, the same six stops, and it lights under the pointer first |
| `/effort` (or `/thinking`, `/think`) | six rows — `auto` and the five rungs — with what each one buys and the one in force marked |
| `/effort max` | that rung, outright |
| `/effort auto` (or `/effort off`) | clears this conversation's rung and hands it back to whatever stands over it |

A word that is none of the six changes nothing and prints them all. This is
**this conversation's** rung in every one of those forms, and it reaches the work this
conversation hands out: a task worker starts at it.

**It works over `--host` too.** The rung is set on the machine the conversation is running
on and the word on your line is the one that machine resolved — `auto` included, on a
hosted conversation nobody has dialled. Against an engine too old to know the ladder there
is no rung on the line and neither the chord nor `/effort` offers one — a capability that
cannot work is absent rather than broken.

## Auto reasoning — use OpenRouter defaults instead of forcing high

The **thinking** row in `/settings` defaults to `auto`. It omits the reasoning override
entirely, leaving the selected model's defaults to OpenRouter. It does not disable
thinking or force a token budget, and the model may still spend time reasoning.
Existing explicit conversation, task, model and install levels remain in force.
On the chat dial, the legacy word `off` clears the override just like `auto`.
The headless environment settings `CODEAF_REASONING=off` and
`CODEAF_EXEC_REASONING=off` retain their existing meaning: they explicitly ask
the provider to disable reasoning.

`--reasoning auto` clears the launch override and inherits the conversation or install
setting; choose auto in `/settings` to change the install default. Scoped overrides
still take precedence.

**Standing work and its checks no longer carry a level of their own.** A standing item's
firing and the yes-or-no check in front of it used to be sent at `low` whatever anybody
had chosen, because they run unattended and forever. That level is gone: the only rung
that reaches a firing is the one written on the item's own card, and an item that was
never dialled sends no reasoning field at all. The conversation you set the item up in
still does not reach it — that is what keeps an install dialled to `max` from turning
every check on the machine into a deep pass.

## Thinking between tool calls

A completed model reply keeps the reasoning supplied by that model alongside its
tool calls, so the next step can continue from the same work. Streamed pieces of
one text or summary block are joined before that history is sent back. Separate
blocks stay separate, and encrypted reasoning is retained without rewriting it.

This does not choose a thinking level or add a token budget. An unfinished attempt
does not supply a completed reasoning continuation, and switching models does not
send one model's private reasoning to another.

## What a request carries when nobody has chosen anything

Nothing about how the model generates. A turn you have not dialled goes out with the
model, your conversation, the tools on the belt and the usage receipt — and **no output
cap, no temperature, no `top_p`, and no reasoning object**. Every one of those is
optional upstream, and leaving it out is what makes the model answer at its own published
default rather than at a number this program picked for it.

That is true of the headless doors too (`codeaf run`, `codeaf do`), which used to impose
a 32,768-token output ceiling and send `reasoning: off` on planning and execution.
Neither happens now.

What still travels is what somebody asked for: a level you dialled, an explicit
`--reasoning` level, `CODEAF_REASONING` and `CODEAF_EXEC_REASONING` at a headless
door, a crew class value like `moonshotai/kimi-k3:high`, and a rung on a task or
a standing card. `off` on the headless environment settings really does send the
disable; `off` on the chat dial is the legacy spelling of `auto`. Errands the
session runs for itself — naming a conversation, judging a route — still ask for
nothing, because your dial is not spent on a title.

codeaf also leaves generation defaults alone on its own auxiliary calls: task and
conversation names, reflex sorting, memory upkeep, task planning and checks, standing
work, document parsing, saved harness execution, and resident work all omit output and
sampling controls unless an operator-facing option supplied one. Context reserves,
response byte limits, task budgets and deadlines still bound the local process; they are
not sent as instructions for how the provider should generate.

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
| a gap mid-reply | **45s** on an endpoint codeaf has not timed, less on one it has | started writing and stopped |

There is a third clock for the opposite problem — a reply that keeps writing and never
finishes. It is not a fixed number, so it has its own section below: *A reply that never
finished*.

The first bound is generous on purpose: a reasoning model at a long context legitimately
thinks for a minute before its first token, and cutting a request that was about to answer
costs the whole prompt again. The second is shorter because the question is different — a
model that has started writing has finished deciding.

**And the second one gets shorter still on an endpoint codeaf has measured.** Forty-five
seconds is what a stranger gets. Once codeaf knows how fast an endpoint writes — the
`t/s` figure the status line shows you beside the state word while a turn writes — the gap it will sit through is how long *that*
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
screen*), and a dim row lands in the conversation saying which — `nothing came back from
the model · asking again · 2 of 3`, or `the model went quiet mid-reply · asking again ·
2 of 3`.

**If all three attempts come back with nothing, codeaf finishes the reply on another
model** — the next one in your `fallback models` row, or the nearest same-class model in
the catalog when you have written no row. It is said out loud before it happens, naming
where the rest of the answer is coming from:

```
the model went quiet mid-reply · moving to gpt-5-mini
```

The turn finishes there and the cost lands against the model that actually answered. **It
is a rescue, not a choice you made**: your model is untouched, `/status` still shows it,
and your next message goes back to it. If it keeps stalling, `/model` is how you move for
good — and it does not wait for the stall to finish: name a model while nothing has come
back and that request is let go of and asked again on yours.

Only when there is nowhere to go — you are on `--one-model`, or no chain resolves, **and
you have not named a model yourself** — does the turn end instead. (The one exception is a
conversation against **your own server**, which never ends this way while you watch it: see
*My own server keeps going quiet* below.)

```
error: nothing came back from the model in 1m30s, three times. a different model may answer — /model, or set models.fallbacks so this can move on its own
```

And when the fallbacks could not finish it either, the sentence says so rather than
repeating advice already taken:

```
error: nothing came back from the model in 1m30s, three times. openai/gpt-5-mini and anthropic/claude-sonnet-4 could not finish it either — /model to pick another one yourself
```

These retries are **their own budget**, and they are the only count left in the request
path. A request nobody answered is not evidence that the endpoint is failing, so it does
not spend the patience a real provider error gets — which is a length of time rather than a
number of tries (see *How long codeaf keeps trying*).

**Sometimes it moves after two attempts instead of three.** Three attempts are worth
making only when they can reach *different* endpoints. If the stream died before naming
which endpoint served it, or you have set `routing` to `off` or `simple` on the **Providers** tab, then
nothing is being routed around and the next attempt lands in exactly the same place — so
codeaf stops asking and moves to the next model a try earlier. Setting `routing` to `off`
or `simple` switches off **endpoint** steering; it does not switch off moving to another model.

## My own server keeps going quiet — a local model or my own base url, and codeaf keeps asking instead of giving up

When there is only one machine behind a request, a quiet reply is handled differently.
That means your own base url or a local server with no router in front of it, a single
connected service, or a model pinned by hand to one lane. Another endpoint and a pause
cannot help there, and giving up would only hand you the retry to do by hand.

So in a conversation you are watching, with no fallback model left to move to, **codeaf
keeps asking that machine until it answers or you stop it**. It waits before every ask:
1 second, then 2, 4 and 8, then every **10 seconds** from then on. The wait never grows
past ten seconds, because what you are waiting for (weights loading, one busy slot)
finishes at a moment nobody can predict. The give-up deadline does not end it either. The
status line says how long it has been:

```
  ··· trying again · no answer 7 times in 2m · still asking · esc stops
```

`esc` stops the turn. `/model` moves it to another model at the next ask.

A task running on its own, with nobody watching, does not wait like this. It asks the one
machine at most **four** times, with the same waits between them, and then moves to a
fallback or ends. So does a watched conversation that still has a fallback model to go to. A router's pool is **never** treated as one machine, whatever your `routing` row
says: it keeps the short allowance above, because the next ask can land on another machine.

## Was I charged for a reply that got cut off — money on a stream that was cut, stopped, or lost the race

Yes, a provider may still charge for the prompt and the tokens it produced before a stream
was cut. The last usage block never arrives in that case, so codeaf does not guess from the
text it happened to receive. When the stream named the provider's generation id, codeaf asks
for that generation's own receipt in the background. Your reply does not wait for this.

When the receipt arrives, its own cost and token counts move the conversation's meter and add
one late line to the machine's usage ledger. That line is marked `reconciled`, meaning its
figures came from the receipt rather than the cut stream. A losing rescue arm is recorded as
hedged waste from its own receipt too; it is real provider money, but it is not added twice.

When no generation id arrived, the base has no receipt route, or the receipt still cannot be
had after the short retry schedule, codeaf writes an `unbilled` marker with no invented
price or token count. The marker survives a restart. `/cost` counts missing prices for this
conversation and its tasks; `/spend` counts the markers in its selected time window. Both
say, for example, `2 calls the provider charged for and could not be priced`. At zero they
say nothing. Settings→Spending also shows missing receipts learned during this process.
Receipt workers exit when their queue is empty and start again when another receipt arrives.

## A reply that never finished — the turn ran for half an hour, codeaf looked frozen, nothing happened for ages, the model kept writing and never stopped

The two clocks above are both about **silence**. A reply that keeps producing a token every
few seconds resets both of them forever, and for a long time nothing in codeaf ended a
request like that: a turn could sit there for half an hour with the reply still technically
arriving, and the session log recorded nothing at all while it did.

So every request also carries a **wall**. **A long reply that is still writing at its
endpoint's normal speed is not cut at the wall** — only a reply that has stopped keeping
up, or one that reaches 20 minutes.

**The wall is not a fixed number.** It is worked out from what that endpoint has actually
done for you: **five times the longest reply it has finished** in this session, never less
than **2m30s** and never more than **20 minutes**. Two endpoints serving the same model
therefore get two different walls, and one that routinely writes long answers earns a
longer one by writing them.

**When a reply reaches the wall, codeaf checks its speed before it cuts.** If the reply
wrote at least a fifth of what that endpoint normally writes in the same time, it is a long
answer and not a stuck one, and it gets another wall's worth of time. It is checked again at
the end of that, and again, up to **20 minutes**, which is the one limit nothing extends. A
reply that is dripping — a token every few seconds from an endpoint that writes forty a
second — is cut at the first wall. The speed of an endpoint codeaf has not timed yet is
taken as 30 tokens a second, so the check is six a second.

**A long file write is a long reply like any other.** A tool call that writes a whole file
streams its contents the way an answer streams words, and it counts as the reply arriving.
Before this, a model writing one large file on an endpoint that had only ever finished short
turns was cut at 2m30s every time, on every retry, while it wrote at full speed — the wall
was too short for the file, and the endpoint could never finish a long reply to earn a
longer wall.

**A model codeaf has not spoken to yet gets 5 minutes**, because there is nothing measured
to work from. That figure used to be the floor under *everybody*, which meant the
measurement could never make anything shorter than what a stranger got: an endpoint whose
longest finished reply was twenty-four seconds still sat there for five whole minutes, and
two hung streams in one measured run did exactly that. It is the outer bound for a stranger
now, and an endpoint you have timed is held to its own history instead. The numbers are
forgotten when codeaf closes, so a fresh session starts from the 5-minute bound again.

The lower clamp is 2m30s and not less, because that is the longest an endpoint is allowed to
go quiet while assembling an answer on its own side (above). A wall shorter than that would
cut a reply the silence clocks were still being patient with.

When a reply is cut at the wall it is asked again exactly like a reply that went quiet —
the endpoint is avoided on the retry, and a dim line lands:

```
the reply kept going and never finished · asking again · 2 of 3
```

and if it keeps happening, the turn moves to your next fallback model:

```
the reply kept going and never finished · moving to gpt-5-mini
```

with the same ending when there is nowhere to move:

```
error: the reply ran past 15m0s without finishing and was cut, three times. a different model may answer — /model, or set models.fallbacks so this can move on its own
```

The time in that sentence is the whole limit the reply reached — after extensions, if it
got any — and the session log records how many tokens had arrived before the cut, a file
being written included.

**Nothing you can set changes the wall.** It has no settings row, because a number you had
to pick would be a number nobody could pick correctly — that is the whole reason it is
measured instead.

**A reply that is not streamed is held to the same wall once it has been measured.** The
calls that arrive whole rather than token by token — the headless run's planner and its
workers, `codeaf do` — carry a total deadline sized from the room the reply was given (one
second for every 64 tokens it may write, never less than 5 minutes and never more than 15).
Once that endpoint has finished a reply for you, the measured wall applies to those calls
too, and whichever of the two is shorter is the one that cuts. A model codeaf has not heard
back from yet keeps the room-sized deadline, because a first reply from a model that thinks
at length may need all of it. **That wall is never extended**: a reply that arrives in one
piece has no speed to check until it is over.

**A cut reply is thrown away whole**, like every other cut: none of the text reaches the
conversation, and the retry starts the reply from the beginning.

## I keep getting rate limited — 429, "too many requests", the provider telling codeaf to slow down

A provider that answers `429` is pacing codeaf, not failing. What happens next depends
entirely on **who** it says is out of room, and the two answers are different roads.

**A rate limit that names a machine is a move, not a wait.** Routers usually do name one —
the limit is some provider's shared pool rather than your account, and it arrives as
`(via Wafer: … is temporarily rate-limited upstream.)`. That endpoint is taken off the next
request and another machine serving the same model is asked **at once, with no wait at
all**, for as long as the turn's own patience lasts — **90 seconds** for a turn you are
sitting in front of, four and a half minutes for a task's own call, nine for a standing
pass. There is no count of attempts anywhere in this; see *How long codeaf keeps trying*.

**A rate limit that names nobody is your whole account**, and there is no machine to step
around: every machine behind the model is behind the same ceiling. codeaf waits **once**,
for exactly as long as the answer itself asked for — capped at a minute, so a provider
naming tomorrow morning does not park your turn, and not at all when it asked for nothing —
and then moves to another model. A second machine would only spend the same allowance
faster; a different model is not on that allowance at all. On your screen it reads
`we are being asked to slow down`. *The providers behind a model* in the providers page is the
longer account of both roads.

**And a machine that goes on answering after you have stepped around it ends the walk.**
When the next request says "not that one" and that one serves it anyway, routing cannot
help this request, so codeaf stops asking and hands the refusal up rather than buying the
same answer a third time.

**When the patience runs out, the refusal goes back to your turn**, which moves to the
next model in your `fallback models` row — the next section is what that looks like.

*Until 2026-09-11 a named rate limit took the account road too: the identical request went
back to the machine that had just refused it, behind a wait that doubled each time — 0.7s,
1.4, 2.8, 5.6 and on — for the whole of the give-up. One measured task spent eight sends on
one machine over ninety seconds while six other machines on the same model were answering
in under five.*

There used to be a second, quieter move here: the call itself would switch models and say
`Retry 1/1: Falling back to openai/gpt-5-mini`. That is gone. **Your model is changed in
exactly one place now**, by the turn, out loud, and the line you read for it is the one in
the next section. Two things moving one model meant a turn could pay for the same fallback
twice and skip another entirely, and one of them carried the old model's pinned machine
across to the new model, which the router then refused.

With no chain to move to, you get the provider's own words and the status:

```
error: after 4 attempts: API error (429): rate limit exceeded
```

The number in that sentence is what the call actually spent, not a ceiling it was allowed:
there is no ceiling, only the deadline. That patience is the *call's* own, inside one
request. What happens when the whole request
keeps failing — several 429s in a row, a `502` between them — is the next section.

**Picking another model is worth doing while this is happening, and it is the fastest way
out of it.** A call that is only waiting has given you nothing, so it is **let go of at
once** and asked again on the model you named — within a second, in a task's room and in
the conversation alike. A step being paced no longer just sits there. If the answer had
already begun arriving, it finishes on the model it started on and the step's next request
is on yours; either way the move goes to **the model you picked**, rather than to the next
name in your `fallback models` row, and it says so in the run's own log. Before 2026-09-11
a rate limit was the one failure that moved nothing at all, so a pick made over a stuck
step was read only after something else had already rescued it. See *Can I switch models
while it is replying* above, and *I changed the model but my task is still on the old one*.

## The model kept refusing and codeaf moved to another one — 429 and 502 in a row, my turn died while another model was working, does a refusal reach my fallback models

Yes. **A model that will not take your request at all is given up on the same way a model
that goes quiet is: codeaf finishes the reply on the next model in your `fallback models`
row**, or on the nearest same-class model in the catalog when you have written no row.

This is what happens. A request that fails outright — a refusal from the machine serving
your model, a `502`, a torn connection, a deadline — is asked again, with a dim line each
time:

```
the model would not take the request · asking again · 2 of 4
```

**There is no count of tries.** The `2 of 4` is which machine behind your model is being
asked, out of how many codeaf knows of — so it counts down real places left to go, and it
draws no number at all when nobody has named a set. What bounds the whole thing is the
give-up above, one deadline in your own time: 90 seconds on a turn you are sitting in front
of, four and a half minutes for a task's own call. A machine that named a comeback is
waited for exactly that long; a fault nobody named a wait for is asked again behind a wait
that doubles.

When that give-up is gone, the turn does not end. It moves, and says so before the next
words appear in a different voice:

```
the model would not take the request · moving to gpt-5-mini
```

The new model gets a whole give-up of its own — what the last one did says nothing about
this one — and the cost lands against the model that actually answered. **It is a rescue,
not a choice you made**: your model is untouched, `/status` still shows it, and your next
message goes back to it. **But a model you name is the head of that chain** — pick one with
`/model` while the moving is happening and the next move goes to yours instead of to the
next name in the row, in the conversation exactly as in a task's room. *Changing the model
for one task while it is running* on the tasks page says what a room adds to that.

**It did not use to.** Until this changed, only a *cut* reply reached your fallback models;
a refusal walked the four tries and then ended the turn, so a measured conversation on
2026-09-10 took one `502` and three `429`s inside seventy-five seconds and died — while a
second model in the same session was answering every call put to it.

Only when there is nowhere left to ask does the turn end. With no chain, it ends on the
words it always did:

```
error: after 3 retries: API error (429): rate limit exceeded (via Together)
```

and when the chain was walked and could not answer either, the sentence names every model
that was tried rather than advising a move you have already made:

```
error: the model kept turning the request away: deepseek/deepseek-v4.1-flash was asked four times, and openai/gpt-5-mini could not finish it either. /model to pick another one yourself
```

## A model with no machines, a key that was not accepted, a conversation too long — what codeaf says instead of the router's error

**You never read the router's own sentence about a failed turn.** `API error (404): 0
endpoints out of 1 requested are available matching your guardrail restrictions and data
policy` is a true thing to write in a log and a useless thing to hand somebody whose reply
just stopped: it names machinery you have no access to, about a decision you did not make.
So every ending is said in words about your conversation, with the provider's own text kept
on the record for an autopsy.

These are the sentences and what each one means.

| what you read | what happened | what codeaf does |
| --- | --- | --- |
| `that model is not being served any more` | the router has no machines behind that model id at all | moves to your next fallback model at once, with no tries wasted |
| `your key was not accepted for this model` | a key that is missing, not permitted for this model, or out of balance | stops and tells you — no machine, shape or model changes this |
| `this conversation got too long for the model` | the transcript is past the model's window | shortens the conversation once and asks the same question again |
| `this conversation is too long for the model even after shortening it` | it still did not fit | stops; start a new conversation, or `/model` to one with a bigger window |
| `the request could not be sent as it was` | the router read the request itself and refused it | the request was already retried with its optional parts taken off; nothing else will help |
| `nothing came back from the model — asking again` | a reply arrived with no words and no tool call | asks again on the same budget as any other failure |

**A model with no machines costs you nothing to discover twice.** The first turn that meets
one moves on and codeaf remembers it for as long as the program is running, so every later
turn skips it before a single request goes out and never offers it as a fallback. An answer
from that model clears the memory again — a model with no machines this afternoon often has
some next week, and nothing is written to disk.

**The overflow move fires whatever your compaction setting is.** That setting governs the
automatic pass that keeps a long conversation trimmed; this is the recovery from a request
a provider has already refused, and it runs either way.

**Two things stop the move, and both make it absent rather than broken.** `--one-model`
settles every call this run makes onto the model you named, so nothing is ever asked of
another one. And a refusal your router made **on its own account** — a `400` that names no
machine — is your request being read and rejected, which every model would do, so it stops
at once with no retry and no move (see *"Provider returned error"* below).

## "Provider returned error" — a 400, what the error actually was, and why my reply just stopped

`Provider returned error` is your router saying that **somebody else refused** — it handed
your request to one endpoint, that endpoint said no, and the router is passing the refusal
along. On its own it explains nothing, so codeaf now shows what came with it: the name of
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
its own account, it read the request codeaf built and said no to it — every endpoint alive
would say the same thing, so asking again at 2s, 4s and 8s only spends the time to be told
three times. The turn ends immediately with the refusal instead. That is the whole rule:
**named an endpoint → try another one; named nobody → stop**. It is not a list of status
codes, so it works the same on a `400`, a `403` or anything else a router invents.

**Where to read it afterwards.** Every failed request now writes a line into the session
file — the model, the endpoint, the status, the endpoint's name, its own words, which
attempt it was and how big the request was. Nothing like this was written down before, so a
turn that died left the file saying only that it had ended.

## My reply stopped and nothing was retried — a reply that broke is not carried on, and an empty reply is asked again

A reply that ends **because a call failed** is not a reply that stopped early, and codeaf no
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
answers with nothing did not answer, so codeaf sends the same request again — up to four
tries in all — and there is no pause between them: the endpoint is up and fast and simply
broken, and waiting eight seconds gets you the same nothing. What helps is being served by a
different machine, which is what the next try asks for. Only when all four come back empty
does the turn end. Before this, one empty reply ended a whole turn, and a measured run
stopped eighteen minutes in with hours of budget unspent.

## Where do I see that it is asking again — the retry rows in the conversation, gave up, moving to another model, and the request that did not answer in time

**Every failed attempt at a request leaves a row where you are reading**, in the
dim lane codeaf writes everything about itself in. You do not have to be looking
at the status line at the moment it happens, and you do not lose the story by
looking away:

```
· the model went quiet · asking again · 2 of 4
· the model would not take the request · asking again · 3 of 4
· the model kept going quiet · moving to glm-5.3
· gave up after 4 tries · API error (429) rate limited
```

The first three are **something still being done**. A row is three things: what
went wrong, what is being done about it, and how far in it is.

- **What went wrong** is codeaf's own reading of the failure, in the same words
  everywhere: `nothing came back from the model`, `the model did not answer in
  time`, `the model went quiet`, `the reply lost its thread`, `the reply stopped
  part-way`, `the connection to the model dropped`, `the model would not take the
  request`, `the model could not be reached`.
- **`asking again`** means the same model, once more. **`moving to <model>`**
  means that model's tries are spent and the rest of the answer arrives from
  another one — a different voice at a different price, which is why it is said
  before the text starts appearing.
- **`2 of 4`** is which try this is out of how many that model gets. It is the
  budget the run is actually walking rather than a number codeaf holds, so it
  moves with your settings and with the kind of failure. **A row that is moving
  to another model carries no count**: the count belonged to the model being
  left, and beside a new name it would read as that new model's.

The last one is **the end**. `gave up after 4 tries · …` is drawn when a turn
that was asked again runs out of tries, and what follows the dot is what the
provider actually said. It is the row that used to be missing: a turn could fail
four requests over ninety seconds, give up, and leave nothing in the
conversation at all except a two-word state on the status line, which is gone by
the next redraw. If nothing is being done any more, a row says so.

**An error nobody tried again for is still just an error.** A turn that failed
on its first and only attempt — a request too large for the window, a refusal of
the request itself — draws `error: <what went wrong>`. "Gave up" is a claim about
a struggle, and codeaf does not make it about a single attempt.

**The status line has its own short form of the same event** while a second
attempt is on the wire, with a count-up beside it — the words are in *The model
went quiet, or stopped answering halfway through* above. The row and the status
line are two readings of one moment, so they cannot disagree.

**A task's own page draws the same rows.** A step of a task that is asked again,
or that gives up, says so on the task's page in exactly these words, so a task
whose work keeps failing is not a page that says nothing has arrived yet.

## My reply just stopped and nothing was said — a turn that ended with no answer, no error and no note, my answer disappeared when I opened the conversation in another window, who ended my reply, do I have to type my question again

If a reply ends without arriving, codeaf says one sentence about it. There is
exactly one case where it says nothing, and that is when **you** stopped it: the
screen already drew your stop, and repeating it back to you would be noise.

Everything else is machinery taking a reply away from somebody who was waiting
for it, and each door has its own sentence:

| what ended it | what you read |
| --- | --- |
| this conversation was opened in another window | `this conversation was opened in another window, so the reply stopped here — ask again to pick it up`, said in the window letting go. The window you moved it to asks your question again by itself — see below |
| the conversation was closed or left under the turn | `the reply stopped when this conversation was left — ask again to pick it up` |
| your stop took too long and was let go of | `the reply was let go of after the stop took too long` |
| you stopped all the work in the conversation | `everything running here was stopped, the reply with it` |
| nobody was left watching a conversation on another machine | `nobody was left watching this conversation, so the reply stopped — ask again to pick it up`. Only the unattended door says this. If the engine knew which window it thought had gone, the sentence names it: `nobody was left watching this conversation from studio, so the reply stopped — ask again to pick it up` |
| the engine holding this conversation was stopped | `the engine holding this conversation was stopped, so the reply stopped — ask again to pick it up` |

**You do not have to type your question again when a conversation moved.** If the
reply had said *nothing at all* when another window took the conversation — which
is what a long stretch of thinking looks like, because working is not kept — the
window it arrived in asks your question again for you, straight away, through the
ordinary turn door. Your question is on the page once, where it always was; under
it is one dim line,

```
  the reply stopped when this conversation moved — asking again
```

and then the answer. The stopped attempt's thinking and any half-written words
are gone, because nothing kept them.

**A reply that had already started is not asked again.** Whatever had been
written is in the transcript that arrives with the conversation, and tasks that
were running land `paused — it resumes` and start again from their checkpoint —
so nothing is run a second time. This only ever fires on the one shape the
conversation's own file ends in: your words, and then codeaf stopping the turn
that was answering them with nothing said. A turn **you** stopped is never asked
again, and neither is one that ended any other way — a conversation you left, a
window that closed, an engine that was stopped while you were still in it, or a
session on another machine nobody was watching. Those keep their sentence and
wait for you.

Whatever the door, the ending is also written into the conversation's own file
as a failed call naming the door — for whoever reads the file afterwards, not
for the screen: an error line is never replayed into a conversation, so a
reopened conversation shows what was said and not a note about how the last turn
ended. The model-call log names it too. A row that used to read `context
canceled` now reads `context canceled (turn ended: taken over)`, which is the
one thing an autopsy of a vanished reply needs and did not have. `codeaf logs`
is where to look.

**A request cut out from under a turn that is still going is asked again rather
than reported.** You see `the reply was cut short · asking again · 2 of 4`, the text that
had arrived is thrown away, and the turn carries on. Nothing is silently lost:
if every attempt is spent, the turn ends with the reason said out loud.

Before 2026-09-09 none of this existed. A turn whose reply was taken away ended
with no answer, no error, no note and an idle status line, and there was no way
— on the screen, in the transcript, or in the log — to tell your own stop from a
second window taking the conversation over.

## It said nobody was left watching but I was sitting right here — the reply stopped, bash cancelled, which window did it think had gone, the engine holding this conversation was stopped

`nobody was left watching this conversation, so the reply stopped — ask again to pick it up` is only what you read when the conversation was let go of because nobody was watching it. A goodbye of your own says `the reply stopped when this conversation was left — ask again to pick it up`. An engine stopped on somebody's word while a window is still in the room says `the engine holding this conversation was stopped, so the reply stopped — ask again to pick it up`.

A window that just did something — pressed a key, answered a card — is still watching, even if its link dropped for a few seconds. That gap is a stalled call and the redial after it, not an empty room, and the reply is not stopped for want of a watcher.

When the unattended door does take a stop, it names the window it believed had gone: `nobody was left watching this conversation from studio, so the reply stopped — ask again to pick it up`. Without a name the sentence is the one above, unchanged.

## Why did my task not move to a stronger model — trouble with the connection never buys a dearer model

Three different things used to look the same to codeaf: **the connection** failed, **the
model** was not good enough, or **the work** could not be done. Only the middle one is worth
paying more for, and codeaf now tells them apart before it spends anything.

- **The connection.** Nobody answered, an endpoint refused, a reply came back empty, or a
  tool call arrived mangled. codeaf asks again on the *same* model and lets the router send
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
file saying which of the three it was and what codeaf did about it, beside the failed
request it was made about.

## The model was printing garbage — a reply that repeats itself, started repeating the same line over and over, or comes back as gibberish

A model can lose the thread and stop writing language: one line or one letter repeated
until the token budget is gone, or words with two and three alphabets inside them. It
happens most at long contexts, and it feeds itself — a bad reply goes back into the
conversation, and the model reads its own nonsense before writing the next one.

So codeaf watches the reply as it arrives and cuts it where it went wrong. **None of that
text is kept**: it is not in the conversation, not in the session file, not sent back to
the model, and it comes off your screen. A dim line says so —
`the reply lost its thread · asking again · 2 of 2` — and the same question
is asked **once** more. If the second reply comes apart too, codeaf finishes it on the next
model in your `fallback models` row, saying so first:

```
the reply lost its thread · moving to gpt-5-mini
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
back — is recorded against the endpoint that served it as an answer codeaf could not use,
and that endpoint drops down the order for the requests that follow. So does an answer that
came back with nothing in it at all. Its row in the provider fold then reads `bad replies` (see
*What the note on a provider row means*). It is not a ban: the mark fades on its own over about
an hour, and every usable answer it serves afterwards walks it back up. Recovery is by
serving properly, which is the only evidence there could be.

**Turning it off.** The row is `reply guard` on the **Providers** tab of `/settings`, `on`
or `off`, and the default is **on**. Off means you see whatever arrives, and keep whatever
you stop. You can also just
ask codeaf to turn it off; it is not one of the rows it refuses. The two clocks in the
section above have no switch — a request that produced nothing at all has failed by any
reading. Setting `routing` to `off` or `simple` on the same tab stops codeaf steering between endpoints
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
exactly as it always was. The judgement is the same one codeaf makes on its own while a
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
connection can tell, so without its own guard codeaf would show it to you and keep going.

codeaf reads the finished reply's shape — mostly symbols, a tool it was actually offered
spelled inside the markup, and no real tool call attached — and cuts it. **None of the
markup is kept**: not in the conversation, not sent back to the model. A dim line says

```
the model answered in its own internal markup instead of words — that text was dropped, asking again
```

and the same question is asked **once** more. The provider that served the markup is set
aside first, so the retry genuinely lands somewhere else. If the markup keeps coming, the
turn moves to the next model in your `fallback models` row, saying so:

```
the model answered in its own internal markup instead of words · moving to gpt-5-mini
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

`/cost` (also `/usage`, `/tokens`) prints what this conversation has spent, and on
what, into the conversation. `/spend` is a different command and opens the **spend place**,
which is the whole machine's ledger rather than this conversation's — it was an alias of
`/cost` until the polish wave and is not one now. Up to eight aligned lines:

| Line | What it is |
|---|---|
| `spend` | the money, printed only when it is above zero — this conversation **and every task it started** |
| `conversation` | what the conversation's own calls cost |
| `tasks` | what the work it started has cost, running or finished — tasks and the nodes of an adaptive run |
| `tokens` | `48.1k in · 3.2k out`, or one half alone, or the combined figure |
| `cache` | `31.2k read · saved $0.0180` — the money half only when a price pair was published |
| `model calls` | **requests to the provider**, deliberately not "turns" |
| `empty reflex answers` | paid memory-routing or extraction requests that returned no answer |
| `time` | how long |

`conversation` and `tasks` are dropped together unless the work has spent something, so a
conversation that has started no tasks prints `spend` alone. When they are there they add
up to the line above them, always — that is the whole point of printing them.

## Why do the tokens on the status line jump instead of counting up

They count up. While a turn is running, the money and the token figures on the status
line **walk toward the new reading** over a couple of tenths of a second — the same
ease as the reply writing in — rather than popping from one number to the next. The
books stay exact; only what the line paints is in motion.

`/cost` and `/status` print the exact books, not the figure in motion. Opening a
resumed conversation, or switching to another one, lands on that conversation's own
bill at once. A screen-reader session never eases a number.

## Does the status line's money include what my tasks are spending — yes, live

**The `$` on the status line is the whole tree: this conversation and every task it
started, at every depth, while they are still running.** It is one figure, not two, and it
is the same figure `/cost` leads with.

**Adaptive runs are in it too.** A node of an adaptive run is work this conversation
started: its money is on the row while it is still working, under `tasks` when you ask
`/cost` for the halves.

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

It counts every request that is written down, not only the ones in your turns: naming the
session, a judge deciding where something should be routed, looking at a picture, every
request a task's own agent made on its own lane, and every request a harness run made
while it walked its program. That is deliberate, because the `spend` line above it is the
sum over exactly those requests — a smaller count beside it would be a bill divided by the
wrong number.

**One request is not written down and so is not in either figure**: the one-token
measurement sent while you are typing. Your provider bills it and codeaf does not count
it — there is a section on that below, `Spend that /cost does not show`.

`empty reflex answers` appears only when that failure happened. Those requests remain in
the token, call and spend totals because the provider billed them; the separate count says
that the money bought no memory decision. codeaf retries one such answer with more room,
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
  reopened it**. There is nothing to rebuild from, and codeaf does not invent a figure.
- `/new` starts a fresh conversation with a fresh file, so it starts at nothing. Resuming an
  old conversation is the opposite: it picks the old bill back up.

The `model calls` count is rebuilt the same way, so a resumed conversation's call count also
covers the requests made before the restart.

## Is there a record of what I spent across all my conversations, by day or by model

Yes, a file on disk, and **the spend place reads it**. Press `alt+5`, or `tab` to it from any
other place, and it draws that file: which days, which models, and what the money was for.

Every cost line written into a conversation's transcript is also appended to one file for the
whole machine, `~/.codeaf/v3/usage.jsonl`, moved by `CODEAF_HOME` like everything else codeaf
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

Five things are worth knowing about it:

- **The figures are the bill, not an estimate.** Until this file existed, "what did opus cost
  me this month" could only be answered by opening every transcript on the machine, and "what
  did I spend on Tuesday" could not be answered at all — a conversation's own total has no day
  in it.
- **A call that cost nothing writes no priced line.** Missing receipts instead leave
  an explicit `unbilled` marker, so a missing price is never called free. The place obeys the same law and draws
  nothing for an unpriced call rather than calling it free.
- **A call the provider charged for and the stream never priced is asked about late.** A
  cut stream that named its generation is matched to the provider's own receipt in the
  background. A found receipt writes its exact cost and token counts on a row marked
  `reconciled`; when no receipt can be had, no figure is invented and a durable `unbilled` marker is
  written instead. The reply never waits for this accounting.
- **A record that could not be written is counted and said.** Writing this file never makes a
  reply wait: if the disk stops answering, the row is dropped rather than the turn. When that
  happens the spend place's top line and the Spending tab both grow a reading — `3 spending
  records could not be written` — so a figure that is short says so instead of quietly reading
  as a cheaper day. Nothing is drawn when nothing was lost, which is nearly always.
- **Work is counted once.** A task's own requests are recorded where they were made. Its spend
  is added to the conversation that started it as it arrives, and that addition is deliberately
  not written here, or the same money would be counted twice. **That holds for every kind of
  work, not only tasks** — the nodes of an adaptive run record their own requests and are
  added up afterwards the same way. Until this was fixed they were on this file twice, so a
  day that included a run read high, and the daily limit was reached before that much had
  actually been spent.

The status line shows **this conversation and its task family**, combining the current
ledger reading with the conversation meter. `/cost` prints that same scope with its
breakdown; `/spend` opens the ledger for the whole machine.

## The spend place — what days and models cost, and what the money was for

`alt+5` opens it. It reads the machine-wide ledger above when you walk in and again on the
same three-second beat every place runs on, and it draws three things:

- **the window and its total** — `14 days came to $34.10 · 41.2M tokens` on the left of the
  head row and the window itself at the right, as the control `shift+← aug 12 – aug 25 →`
  with `shift+↑ coarser` beside it. The left field **says which total it is**: the line above
  it is the day (`today $3.42 of $500`) and this one is the whole window, so each figure
  carries the period it is the total of. It is counted in the grain's own noun — `14 days`,
  `4 weeks`, `6 months` — and on a narrow frame it gives up the words before the figure:
  `14 days · $34.10`, then `$34.10`. The dates themselves are spelled once, between the
  arrows;
- **a chart of the window**, under that row, as wide as the frame allows — every bucket gets
  the same number of cells, up to eight, so a fortnight on a wide terminal is a shape you can
  read rather than fourteen cells in the corner. Its axis is **two dates**: the first bucket
  at the left, and at the right the last one, called `today` when it is today. There is no
  money on the axis — the money is the line above it;
- **which day was loudest**, as the last clause of that same head line:
  `14 days came to $34.10 · 41.2M tokens · loudest day: $21.40 aug 20 (the-filings-sweep)`.
  It had a row of its own between the chart and the first table, which is a sentence saying
  what the line above it already says three facts of. On a narrower frame it gives up what
  the day was mostly spent on — that name is a row of *by topic* a few lines below —
  and then the clause altogether, before ever crowding `shift+↑ coarser` off the line: a
  key that is not drawn is a key that does not work, so the sentence yields to the control.
  **Both figures on this line are exact** — `$0.0068`, not the `$0.01` the tables floor to —
  because a floor is a column's rule and a total in a sentence has nothing to line up with.
  `/cost` and Settings→Spending say the same figure;
- **one cut of the ledger at a time**, and its heading is the control that swaps them.
  `by model` and `by topic` are the same money added up two ways — every dollar under one
  heading is also a dollar under the other — so a page drawing both asked you to read one
  bill twice. Walk the cursor onto the heading and it wears arrows: `← by topic →`.
  **`←`, `→` and `enter` all step it**, and the ring wraps, so neither arrow is ever a key
  that does nothing. The arrows are drawn only while the cursor is on that row, because `→`
  on every other row of this place opens that row's verbs — a key is drawn where it is
  bound — and the heading keeps its own colour in both states: the **band** is what says the
  cursor is there, and the arrows say what the keys do. The foot names the cut the arrows
  lead to. **An empty window keeps the control**, for the reason it keeps the window
  header: paged back onto a quiet fortnight it is the only thing on the frame a key can act
  on, and without it `by model` had no way back;
- **by model**, dearest first and **walkable like any list here**, each row carrying
  **the role it is bound to**, its call
  count, its token volume and what it cost. The role stands **first after the name**,
  because what a model *is* on this machine reads with the name it follows, while the
  calls, the tokens and the money are three readings of one quantity and belong together.
  It is the **crew binding** — `execution`, `conversation`, `verification`, `naming`,
  `planning` — read from the settings as they stand right now, and never the auxiliary word
  one call gave itself. That is the point of the column: seeing that execution is most of
  the bill sends you to the one row that changes it. A model that is on the bill and is
  bound to nothing today draws **no role word at all**, and a model bound to two slots says
  both. The rows **open nothing** — a model is not a thing money was spent *on* — so the
  foot over one keeps `→ the limits` and never promises `enter`; they are stops so that a
  long table scrolls under the cursor. A model is drawn by the word you say out loud — `claude-opus-4-1`, not
  `anthropic/claude-opus-4-1` — which is the spelling `/model`, the crew chips and the
  status line all use;
- **a role slot with nothing bound to it** gets a row of its own under the models —
  `planning · unbound · follows execution` — because "planning costs nothing" and "nothing
  is bound to planning" are opposite facts about the same blank. There is no figure on that
  row: no line in the ledger names a slot, so there is nothing measured to put there.
  **Today only the conversation slot is drawn at all.** A window holds a client for the
  model you are talking to and for no other; the five crew slots are answered where their
  own session is opened, so this window cannot tell "nothing is bound" from "I cannot ask" —
  and the emptiness law says an unknown is drawn as nothing rather than guessed at;
- **by topic** — the three things money is ever spent on, because the ledger holds
  three ids: a piece of work, a standing promise, or a conversation. **The dearest twenty
  are shown** and the rest fold into one line — the body scrolls and the cursor carries the
  window with it, so a long table costs a short terminal nothing. It showed three, which is
  a headline rather than an answer to the question this page is for. Work with **no id of its own** — the hands a reply
  forks, the check that reads what a piece of work left — is on the row of the conversation
  it belongs to, because that is the only name it has;
- **by standing order** — the standing promises, under a heading and columns of their own,
  **drawn with `by topic`**. A promise is one of the things money was *for*, so it is a
  third heading rather than a third cut, and the control above it stays on `by topic`.
  A promise's facts are not a task's: what you want of one is how often it went off and what
  a single firing costs, so its row reads `repo-watch · 88 firings · $0.04 a run · $3.31`
  rather than carrying a project and a kind word it has no use for.

**A row gives up its words before its figure.** A long name or project pushes the fields
behind it, but only as far as the cells the money needs: what gives way is the words, never
the figure the row is read for. The project column is bounded where it is drawn as well as
where it is measured, so a folder named `agentfield-control-plane-web-ui` reads
`agentfield-control-plane-…` rather than shoving the money off the edge.

**The money on every row of both tables is a column of cents.** A row that cost less than
a cent reads `$0.01` — the smallest figure the column can say and still be read — rather
than `$0.0068` or the words `under a cent`, both of which this page used to draw and
neither of which a person can line up against the row above. It rounds up, so a window
full of slivers can show rows that add to more than the heading above them; the heading,
the `today` pointer line and the Spending tab all keep the exact arithmetic. A call that
cost nothing has no row at all, which is the emptiness law and not a rounding.

**The three headings are one set, and the tab already said `spend`.** They read `by model`,
`by topic` and `by standing order` — four to eight cells each — and each says only which way
that table cuts the money. They were sentences (`what ran it · by the model, and the role it
was bound to`, `what it was for`, `what kept running · standing orders, and what a firing
cost`), each naming the page's subject again before getting to the point. A heading in
this set says how its table cuts the money and leaves the columns to say what they hold —
the role, the calls and the tokens are all unnamed up there.

**Every table on this page has four columns**, and they are the same four questions asked
of the same money:

| | | | | |
| --- | --- | --- | --- | --- |
| *by model* | the model | the role it is bound to | its calls · its tokens | what it cost |
| *by topic* | the task or the conversation | `task` or `chat` | its project | what it cost |
| *by standing order* | the standing order | its firings | what a firing cost | what it cost |

**What a row *is* stands first, right after its name**; the figures stand together behind
it. The kind word is `task` and `chat` — column words, not sentences. It read `a task` and
`a conversation`, which is how prose names those things and twice what a column needs.

**The name is left-aligned and everything else is pushed right and right-aligned.** What a
row is *about* is read from the left, where your eye already is; what it *cost*, how many
calls it took and what it was bound to are read by comparing them with the row above, and a
comparison is made on a figure's right-hand edge. So the facts travel together in one block
and the names run out to meet them. The call count keeps its unit word — `9,400 calls` —
because the page has no header row; the token column does not, since `3.2B` beside
`128,400 calls` is already plainly a different kind of number.

**That block ends where the chart above it ends** — the `today` point, with the last
bucket's date already standing under it. The money used to be flushed to the frame, which
on a wide terminal put the one figure every row is read for forty cells away from the counts
it belongs with. A window zoomed so far in that its chart is only a few cells wide falls
back to the frame, because a table has to be drawn somewhere.

**There used to be a bar beside every model**, its share of the dearest one. It is gone: the
list is sorted dearest first and every row says what it cost, which is the same comparison
in figures you can also subtract — and the bar cost a reserved column, plus a second
reservation in front of it so a role word on one row could not push it out of line.

A name column is the width of what it holds and is **never squeezed** to keep a field behind
it; a name wider than its column keeps every cell of itself and starts the next field one
space late. On a frame too narrow for the whole table, **whole fields go** in order of what
they are worth — the token volume first, then the role, then the calls; the kind word before
the project — never a figure with its tail cut off, and never the money. Where a field
*stands* and what it is *worth* are two different questions: the kind word leads the block
and is still the first thing given up.

**Figures are written the same way wherever they appear.** Money over a thousand carries the
mark — `$4,210.55`, and a limit `$50,000` — and so do call and firing counts: `128,400 calls`. Token
volumes climb `842`, `12.4k`, `1.2M`, `3.2B`, one decimal and no more, so a number never
keeps a unit it has outgrown.

The ledger holds **ids and no titles**, so the place joins each id against the records it is
already reading — the project's own index of what it ran, and the standing store — to put a
name on the row. A thing neither of them knows keeps its id.

**`enter` on any row under "by topic" or "by standing order" opens the thing itself**, and
the foot says so on every row that is a door — `enter opens what spent it`. The headings
said it too for a while; the heading over the cut is the control that swaps cuts now, and
a control with an unrelated instruction after it is two objects on one line. A task opens **its own record card** in the sessions place, with the list
behind it parked on that row; a standing promise opens the standing place **on that
order**; and a conversation **opens** — brought forward if this terminal already has it,
otherwise opened beside the one you are in, with all of that door's refusals (a folder that
has since gone says so). A task is found by the pair that identifies one, **its id and the
conversation that ran it**, because ids restart with every conversation — two conversations
each holding a task `7` are two different pieces of work. A row whose thing the record no
longer holds refuses where it stands: `that piece of work is not on this machine any more`,
or `that conversation is not on this machine any more`. It used to open the *place* and leave you to find your own row, and
a conversation went to home rather than into the conversation.

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

The cut of the ledger is its own control and has nothing to do with time: walk onto the
heading and `←`/`→` swap `by topic` for `by model`. The window opens on **the last 14 days,
by the day**. The label between the arrows is the
reading and the control at once, and the same head row is drawn on the sessions place and the
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
crew     max · brain claude-opus-5 · hands glm-5.3 · checks claude-fable-5.1
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
compaction fires at **1,114,112** — not at some smaller figure of codeaf's choosing. On the
default 128,000-token window it fires at 108,800. The line is always
`window − max(15% of window, 16384)`, and `window` is what the model card says.

There used to be a flat ceiling of 256,000 over every model alike, and it made a
million-token model fold exactly like a small one — nineteen passes in one two-and-a-half
hour run, each at around a hundred thousand tokens, each one throwing the provider's prompt
cache away. That ceiling is gone.

**What can still lower it is an endpoint refusing.** If a provider answers that a request
would not fit, codeaf writes down how big that request was and never trusts that model past
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

**Accuracy note.** codeaf also carries a shared context-budget package with a 60%-fill rule,
a 160k working set and a 250% reuse law. **That package is not used by this chat.** Its
consumer is the sub-harness leaf sizing elsewhere in codeaf. The chat's own law is the one
above — do not describe this conversation as filling to 60%.

## A task or a worker on another model gets that model's window

Work that leaves the conversation — a task's worker, an adaptive run's worker, the
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

## What happens before the conversation is compacted

codeaf does not jump straight to summarizing. There are rungs before it.

**During one long turn, tool output has its own working-set bound.** Once the live request
estimate crosses **64,000 tokens** — or half the trusted context window when that is smaller
— codeaf replaces already-seen tool results from that turn with the same readable pointer
lines described below. It works in whole tool batches, oldest first, while leaving the
latest **20,000 tokens** verbatim (capped at a quarter of a smaller window). The result from
the batch that just ran is never folded before the model has seen it, and neither your
message nor any assistant text is folded.

The pass aims below the trigger, at the midpoint between the kept tail and the working-set
line. That headroom matters because rewriting a result makes the provider's cached prefix
cold from that point; one deeper pass is cheaper than another rewrite every round. When it
runs, the transcript gets one line such as `[folded 8 results · ~24k tokens]`. The full
result bytes remain in `logs/stubs/`, the model can `read` the path in each stub, and the
session journal keeps the original result bytes.

One shape of result needs no copy filed. When the turn has read the **same file several
times**, the older slices point at the file itself — the stub names the path with the
`offset` and `limit` that bring that slice back — and the newest slice is kept whole,
because those bytes were never anywhere else to begin with. The journal keeps them too,
as it keeps everything.

**Rung 0 — what an old result looks like in the request.** Before any of the rungs below
fire, the copy of the conversation that goes to the model already carries the tool results
of *earlier* turns shortened. Your transcript and the session journal keep every byte; only
the request is shortened, and only for results the model has already worked from — the newest
batch and everything the running turn has produced go verbatim. A shortened result reads:

```
[reduced view: bash · 41208 bytes · full: /home/x/.codeaf/v3/projects/-you-work/<session>/logs/stubs/9c2f.txt]
go build ./...
…[40608 bytes elided]…
FAIL	./internal/session	0.412s
```

Both ends are kept — the first 200 bytes, which is what ran and where an error message
lands, and the last 400, which is what it concluded — with the exact count of what was cut
between them. The pointer is the same one a stub gives (next section): the result's own
bytes, filed under `logs/stubs/`, which `read` opens and pages through at any size. Where
there is nowhere to file them it is the session journal with the call id to grep for — a
real file, though a journal line is JSON and `grep` clips a long one. **A conversation that
can name neither says `full: not retrievable`** rather than a path that is not there.

**A `store:` id is never given as a pointer.** It used to be, whenever memory was on, and
at that time no tool could fetch a store message by id. `search_conversations` now opens
a bounded exchange by a source reference, but it does not replay a whole tool
result. A stub still names the file containing the full bytes. When a turn has made so many calls that even these views
are too much, the oldest fall back to the same one-line stub described next.

**Rung 1 — stubbing.** At the end of every completed turn, tool results older than the last
**4 turns** and larger than **1500 bytes** are replaced *in the live context* by a pointer
line naming the tool, its first line, its size and where the whole of it lives:

```
[tool: bash · go build ./... — 0 exit · 41208 bytes · full: ~/.codeaf/v3/projects/-you-work/<session>/logs/stubs/<hash>.txt]
```

The bytes are written to disk first, named by their own digest, and the model can `read`
them back at any time.
**They are written in this conversation's own folder, under `logs/stubs/`, and never in your
project**: a stubbed result is the harness's own droppings, not your work. That holds for a
task's worker too, however long the files it reads — its stubs are filed with the
conversation that sent it out, not in the checkout it is working in. Only a conversation
with no folder at all falls back to `<workspace>/.codeaf/stubs/`.
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
notes codeaf keeps in front of the model that move as the work moves — `<state>`, what this
conversation is doing, and `<elsewhere>`, what other windows on this project have landed —
are appended at the *end* of the conversation and never written into the system message,
because a system message that changed would make every message behind it new again, while a
note at the end costs only the note.

**Rung 2 — page images.** Instead of summarizing the part being dropped, it can be
photographed: rendered verbatim to monospaced page images that the model reads back. No model
call, nothing paraphrased. This rung is chosen only when you gave `/compact` no focus, there
is a workspace, there is page budget, and the model in use can read images. Pages are 120
columns by 64 lines, greyscale, deterministic, and footed
`<title> | context page 1 of 4`. The ceiling is **8 pages**; anything past it is folded to a
marker after the pages.

**Rung 3 — the fold.** If the transcript is still too big after stubbing, the oldest
**assistant** work is replaced by one marker line. It is not a summary: nothing is described
and nothing is decided.

```
[folded 43 messages · grep or read ~/.codeaf/v3/projects/-you-work/<session>/journal.jsonl, lines 12..40]
```

**Your own words are never folded.** A person's messages are the one thing in a transcript
nothing else can reconstruct, so the fold walks past them and takes only the assistant's.

## What a compaction pass keeps

**A compaction asks no model, costs nothing, and takes no time you can feel.** There is no
summarizer behind it — there was one, and it was deleted. It paid a model to write prose
about the text it was about to throw away, at the worst possible moment, and the loss was
unrecoverable because the transcript the prose came from went with it.

What replaces it is two mechanical passes over messages this session already has: tool
results become pointers to their own bytes, and then the oldest assistant work becomes one
marker line naming where the whole of it can still be read.

What the model is handed instead of a summary is the **state card** — what `track` and
`commit` recorded — which rides in the system prompt on every turn and is kept up to date
after each one. So what the conversation is about is never paraphrased, because it was never
written as prose in the first place.

A pass can decline: `session: nothing to compact` (everything already fits in the tail), or
`session: a compaction pass is already running`.

## What happens when the conversation gets too long — when compaction happens by itself

When the conversation gets too long to fit, nothing is lost and nothing stops: the oldest
part of it is stubbed and folded down to a marker and the recent tail is kept, which is what
compaction is.

**Nothing is lost is meant literally, and you can go and look.** The session file keeps
every original line, and scrolling up above the boundary is given those rather than the
shortened copy — with one dim line, `· above here the model keeps a shortened record — you
can still read it all`, where the two meet. What shrank is the model's copy, not yours (the
screen page has the whole of that line's meaning, and the limit: a session compacted by an
older codeaf is still drawn from the shortened copy). The fold marker the model sees names
that journal as a real path — `[folded 31 messages · grep or read /home/x/.codeaf/v3/sessions/abc.jsonl, lines 12..40]`
— so codeaf can open the lines that left the window itself. *Where did the folded messages
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

`/compact` compacts the conversation on demand. It notes `compacting…` immediately and runs
the pass off the loop, so the surface stays alive.

**Success is silent.** There is no "done" message — a compaction that worked simply leaves the
conversation shorter. A failure comes back as `compact failed: ` followed by the error.

**It costs nothing and asks no model**, so there is no reason not to run it, and no `compaction`
role in settings to point at a model for it.

## Turning automatic compaction off

Launch with `codeaf chat --no-compact` or `codeaf resume --no-compact`. The flag's own help
text reads `never compact automatically`.

What it turns off is exactly the automatic threshold check. Still working:

- `/compact`, when you ask for it, and
- the recovery pass when the provider itself refuses a request as too long.

With a `--host` remote launch the flag is **refused rather than ignored**, because it cannot
travel to the other machine.

## What am I allowed to spend — the limits codeaf ships with, and turning them off

Every limit codeaf ships with is a **backstop against something going wrong**, not a
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
| **practice** | `$50 of the day` | codeaf's practice on itself stops until tomorrow, and your own work is untouched |

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
disagreed while a task was running. It also counts a call whose receipt arrived after its
turn ended, the same moment `today` does.

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
tabs beside it: **Safety** is what codeaf may do without asking you first (ask before
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
| the spend place (`alt+5`) | `enter` on its first line, the dim `today $3.42 of $500 · /budget sets the limits`, the same figure the top line of every place draws |
| the spend place, from a row | `→` opens the verb strip, where `b` is `the limits` |
| a refused turn | the message names `/budget` |
| the first-run setup | its `Models and spending` screen, whose **Daily limit** row writes this same row. It asks about the day's limit only — `per plan` and `per conversation` keep their defaults there and are changed here |

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

**`practice` is the one row where `0` is not "no limit".** Zero turns codeaf's
self-practice **off** rather than uncapping it. Practice is work codeaf does while nobody
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

The machine's day is drawn in **three** places and they are **one reading of one file**:
`today` on the Spending tab, `today $3.42 of $500` on the spend place (`alt+5`), and the
green figure on the **top line of every place** — `$3.42 / $500.00`, beside the clock. All
three sum the same rows of the machine ledger, so they cannot come apart, and the top line
says the same thing whichever place you are standing on.

## What does per plan mean — the limit that asks instead of stopping

`per plan` is the only money row that **asks rather than stops**, and its value says so:
it reads `asks first above $100`, not a bare figure.

When a planned job is estimated to cost more than that figure, codeaf quotes the step
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
bigger. A task started any other way — `/task <brief>`, a proposal card, codeaf's own
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

## I started a task after my dollar limit was spent — why did it still pay for a call

**A task started after this conversation's dollar limit is already spent still gets
one paid call before it ends.** `/task` is not a turn, so the refusal that stops the
next turn — `conversation limit reached · … · /budget changes it` — is not asked in
front of it. The run is handed the smallest figure above nothing rather than zero,
because zero would mean no limit at all. Its first worker makes one model call, that
call puts the run over the figure, and the run ends there: its row says
`a dollar limit you set stopped it`. The call is small, but it is real money, and it
shows in `/cost`.

The dollar limit here is the smaller of `per conversation` and `--max-cost`, measured
against what this conversation has already spent. To let the task do its work, raise
`per conversation` first — `/budget conversation 20`, or `/budget conversation none`
to remove it — or relaunch with a larger `--max-cost`, then start the task again.

`codeaf do` has no such call: when today's spending limit is already spent it starts
nothing and says, for a $5 limit,
`today's spending limit of $5.00 is spent, so nothing was started; rerun with --yes-spend to spend past it`.

## Which provider answers, and what it charges

One model id is served by many providers, and they differ in two ways at once: how fast they answer, and what they charge. The published list price beside a model is the model's own figure — no provider is obliged to match it, and the fastest one often does not.

**Left alone, codeaf asks for nothing.** The **routing** row on the Providers tab ships as `simple`, and `simple` means the request carries no preference of codeaf's own: with no provider pinned there is no `provider` object on it at all, and OpenRouter's own default routing picks the provider. Pin a provider and that pin is the whole request — that provider, `only`, no fallbacks, and nothing else added to it. Nothing is ranked, nothing is capped, nothing is retired behind your back, and what the picker shows, what is chosen and what the record says are the same thing.

It has not always been this way: until this build the shipped row was `latency`, and codeaf asked for the fastest provider on your own turns and the cheapest on work you were not waiting on. That choosing was invisible — the one decision in a turn you could not see being made — so it is now something you turn on rather than something you turn off.

Setting **routing** yourself is how you turn it on, and it applies everywhere:

- **`latency`** asks for the fastest provider on every call, capped at **a quarter over the model's published list price**: an provider 25% dearer buys a head start you can feel, and one four times dearer buys nothing you would notice on a five-minute task. It also times every answer and demotes an provider that keeps being slow.
- **`price`** asks for the cheapest provider on every call, including your own turns.
- **`simple`** is the shipped row described above.
- **`off`** sends no preference and stops timing providers altogether.

A change here takes effect on your **next message** — the row goes straight to the layer
that sends requests, so nothing waits for a relaunch. The `provider` row under it re-reads what
`auto` means in the new word on the same frame.

Under `latency` or `price`, where a model publishes no price, no cap is sent at all rather than one guessed from something else. If the router refuses a request, codeaf first widens the provider set while keeping the cap. Only if that wider request is refused too does codeaf lift the cap rather than fail the turn. Each change has its own attempt line.

**Under `price`, one thing is not quite "speed is worth nothing".** Work you are not watching asks the router for the cheapest provider — but among the providers behind that model, codeaf will pay a little for a quicker one **while your window is open**, because you are there to read what the work lands. With no window open on this machine it will not: the cheapest provider wins outright, however slowly it writes. Nothing about this is a setting; it follows whether you are here.

**You can also name the provider yourself, under any row.** routing says what a request prefers; the **provider** row above it, and `→` on a row in the model picker, say which provider requests from your home actually go to — see "choose a provider" above. A pin is the one instruction `simple` sends.

With `routing: off` there is nothing measured, so there is no provider to choose, no sheet of them to open under a model row, and no speed guard.

## "0 endpoints … guardrail restrictions and data policy" — paid model training violation, what it means and what codeaf does

On the default service, this sentence means the providers your request was down to were all excluded by your
OpenRouter account's privacy setting, because their providers may train on prompts. It
does not mean the model disappeared or that your prompt was rejected. The request can be
down to one provider because codeaf's price cap left only one, because it asked for one
provider by name, or because its list of slow providers covered the rest.

codeaf answers it without ending your turn. A provider asked for by name is remembered as
out of reach for your account — for every model, for a day, across restarts — and the
answer moves to another provider. So is the one provider the price cap left, when the
router's count and codeaf's list of providers agree on which it was. A price cap that only
out-of-reach providers fit under is not sent at all, so the next turn is not refused. When there is nowhere left to move, codeaf relaxes the
endpoint filter and lets the router choose, then drops the cap and asks again. The attempt
lines say `relaxed the endpoint filter` and then `dropped the price ceiling`. A rescue
request or a request pinned to one provider never carries the cap, because that provider has
already passed codeaf's price choice. Once the price rung is reached, the cap stays off
that model for the rest of this session.

You can change the account policy at `https://openrouter.ai/settings/privacy`, choose
another model, or pin a provider that serves this model. Pinning chooses the provider for this
home — including terminal runs and background work; it does not change your OpenRouter
privacy setting.

A direct service does not use OpenRouter's endpoint list, price cap, or data-policy
negotiation. Its single provider sends directly to the service you connected.

## Choose a provider — pinning the machine that serves your model, the lanes under a model row, and how to change the provider for a model

One model id is served by a dozen different providers, and they are not alike: on one
model measured on one afternoon they differed by **seven times** on the wait before the
first word and by **twelve times** on how fast they wrote, at roughly the same price.
Some of them will not take a tool call at all; some stop writing at 65,000 tokens; some
serve four-bit weights. Which provider answers you is often a bigger difference than which
model you picked.

codeaf calls one of those a **provider** — older builds called it a *lane*, and the
setting on disk still does — and you can see them and choose one.

**Three rows on the Providers tab of `/settings` sit directly under **your model**, in
that order** — **provider**, **speed guard**, **routing** — because the provider that serves
your model is part of the same decision as the model:

```
 your model    deepseek/deepseek-v4-flash
 provider      auto
 speed guard   on
 routing       simple
```

The tail on the model row is the provider **requests are actually going to**:
`pinned: cloudflare` once you have pinned one and the wire is still carrying it,
`auto (cloudflare cannot serve this model)` once that provider has refused the pairing —
your `provider` row is untouched, but nothing is asking for it any more —
`openrouter` when you have asked for no provider at all, and `auto (cloudflare now)` — a
prediction of where the next turn would land — only under `latency` or `price`, where
codeaf is the one choosing. Under the shipped `simple` row nobody here is predicting, so
there is no tail, and a session that has measured nothing shows the model id alone too.

In the model picker — `/model`, or `enter` on that **your model** row — press `→` or
`tab` on a row and the model's providers open underneath it, with the cursor already on the
provider in force (`auto` when nothing is pinned). The providers are a block under the row
and not part of the table, so they keep the `·` tail the model rows gave up:

```
 model               via         first  in/M   out/M  window  t/s
 deepseek-v4-flash   cloudflare   0.8s  $0.09  $0.18      1M   58
   auto          auto-route based on /settings
   openrouter    default routing
```

**`→` opens two answers, and `→` again opens the providers.** The key means the same thing
at both depths — show me what is inside this — and it walks the cursor in each time. The
providers live under **openrouter** because every one of them is a machine OpenRouter routes
to; naming one is a narrower answer inside that row rather than a third thing beside it.
`←` closes one level at a time, so the way out is as many presses as the way in.

```
   auto          auto-route based on /settings
   openrouter    default routing
     provider ↓  first  t/s    $/M    up  note       last 8
     cloudflare   0.8s   58   $1.3  100%  no tools   ▁▂▁▃▁▂
     coreweave    0.4s   24  $0.28   99%  tail 12s   ▁▁▇▁▂▁
     deepinfra    0.8s   27  $0.18   99%  out ≤ 65k
```

**The providers are a table**, drawn by the same engine as the model list above and read the
same way: down the page, comparing. The heading carries the unit so the cell does not —
`58` under `t/s`, `$1.3` under `$/M` — and a column no provider on the list published is not
drawn at all.

**Both headings are drawn apart from their cells** — a different colour, and italic where
the terminal can draw one — so a line of labels is never mistaken for a row whose figures
have gone missing.

**They open in alphabetical order, and `alt+s` reorders them** — the same key that orders the
model list, applied to whichever table the cursor is in. codeaf's own ranking —
fastest-feeling first — is still what `auto` and the model row's `via` read; it is the wrong
order for a list a person reads, because it moves a provider every time the ledger learns
something and the eye has to start over on each visit.

Inside the providers the cycle is `provider → first → t/s → $/M → up` and back, each column
forwards then reversed. `note` and `last 8` do not sort: a note is whichever one thing is
worth saying about a provider, so ordering by its text would rank "bad replies" against
"tail 3s" alphabetically, and the sparkline is a picture rather than a value. The two tables
keep their own orders, so sorting the providers leaves the model list where it was.

**Neither answer wears a mark.** They carried a filled and a hollow bullet for a while, meant
to say which of them chooses for you — but under the shipped routing row neither of them
does, so the mark was making a distinction the wire does not. The indent says they are not
machines and the sentence beside each says what it is.

Each provider row reads, in order: its name, the wait before the first word, how fast it
writes, what a million output tokens cost there, how much of the last five minutes it was
answering, one short note about what is wrong with it, and a sparkline of **your own** last
eight first-token waits on it (taller is slower).

**`up` stands with the figures and the note after them.** Uptime is a number and is read down
its last digit with the three numbers before it; a note is prose, and prose in the middle of
a run of figures breaks the run the eye is following. That is also the order a narrow window
gives them up in — the sparkline first, then the note — so a narrow fold keeps `100%`, which
can be compared between rows, over one row's own caveat. `←` or `tab` closes the providers
again.

`enter` on a provider **pins** it in your home: chat, `codeaf do`, `codeaf exec`, `codeaf
plan`, `codeaf run` and background work all ask for that provider and nowhere else — unless
the router says that provider cannot serve that model at all, which is the one thing that ends
a pin without you. It says so once, in the conversation
(`coreweave cannot serve this model; routing on auto for this model until you pin again`),
routes that one model on auto for the rest of the run, and leaves your row and every other
model alone. *Providers → Pinning one provider yourself* has the whole of it. `enter` on `auto` un-pins. `enter` on `openrouter` asks for no provider
at all, lets the router balance on price, and opens the fold under it (*Providers → Pinning one
provider yourself* says why). If the providers were open under a model you are
not talking to, `enter` switches to that model as well — choosing a provider under a name
means you want that name served from there.

`enter` on the **provider** row opens that same fold directly, on the model you are talking
to, with the cursor already on the provider in force — so choosing an provider is reading
the measured numbers and pressing enter, never guessing at a word. When nothing has been
measured it opens all the same, onto the only two honest answers: `auto` and `openrouter`.

From the keyboard alone: `/model @cloudflare` pins, `/model auto` un-pins.

**Under `routing: simple` — the row codeaf ships with — the `auto` row does something
else.** It reads the same sentence, and it names no provider beside it: under that row nothing on codeaf's side chooses,
so there is no provider it could honestly say the next turn will land on, and no `no
rescue` note either, because there is no rescue running under any setting of the speed
guard. The fold still opens and `enter` still pins: a pin is the one instruction that row
sends. The **provider** row in `/settings` is explained the same way, and the `auto (cloudflare
now)` tail on the **your model** row is gone with it — it was a prediction, and under
`simple` nobody here is predicting. Choose `latency` or `price` and the takeover sentence,
the named provider and the `no rescue` note all come back.

**A model nobody has measured opens onto its two answers and no providers.** `→` shows
`auto` and `openrouter`, and in the providers' place one line —
`no provider has been measured for this model yet — providers show up after its first answer`
— with no number anywhere, the same rule that leaves the speed off its row. Opening it
asks for that model's list of providers in the background.

A pinned provider is written on the model's name as `model@provider` — see *Providers → Pinning one
provider yourself*.

## What the note on a provider row means — no tools, out ≤ 65k, tail 12s, fp4

One note at most, and it is the thing that would spoil the answer soonest:

| Note | What it means |
|---|---|
| `bad replies` | enough of its answers came back unusable that codeaf would rather ask elsewhere |
| `no tools` | the provider does not honour a tool call — a fast wrong answer |
| `out ≤ 65k` | it stops writing well before other providers do, so a long answer is cut |
| `fp4` | it serves weights at a lower precision than the others |
| `tail 12s` | its worst answers start about that late — five times its own median |

A provider with none of those shows no note, and a provider whose answers nobody has judged never
shows `bad replies` — an untried provider is not a suspect.

`bad replies` counts a reply that lost its thread, one that came back as tool markup, one
that went quiet and had to be cut, and one that arrived with nothing in it. It fades over
about an hour on its own, and every usable answer the provider serves takes it further off.

## Where the numbers on a provider row come from — the sheet, and your own answers

Every figure is codeaf's own **belief** about that provider, never a raw published number.
It starts from the router's public sheet — first-token and throughput percentiles over
the last half hour, over everybody's prompts — and every answer you get moves it toward
what that provider did for **you**, from where you are, with the prompts you send.

The belief also **forgets**: with nothing new arriving, codeaf's confidence in it halves
about every ten minutes, so a provider that misbehaved once at breakfast is not held to it
all day and there is no penalty box to let anything out of. What codeaf believes about a
provider's **answers** rather than its speed forgets more slowly — about an hour — because real
requests are minutes apart and a belief that forgot faster than the evidence arrived would
never be worth anything.

Forgetting has an end, and a provider nobody has heard from in hours reaches it. Such a row
says **nothing at all about its worst answers** — no `tail 12s`, and no `no tail` either,
because that is a claim about the worst case too. The speed and throughput figures stay:
they are still the best guess there is. Ask that provider one question and the row has a tail
again, or has honestly none.

**Where the numbers come from is the same everywhere:** a public sheet of what each
provider is like, corrected by the answers your own conversations have actually had. The
row itself carries the figures; a sentence under the cursor used to repeat them in prose
and say where they came from, and it is gone — it said the row's own three numbers a second
time, under a name the row had just written, and cost the open fold a line on every move
of the cursor.

## Sorting the model list by a column — alt+s, cheapest first, biggest window, highest score

**`alt+s` walks the sort. `alt+shift+s` walks it backwards.**

**The list is always sorted, and it opens on its first column — the name, A to Z.** There is
no unsorted state, and the heading always carries an arrow saying which order you are in.

**Every column is two presses: its own direction, then reversed.** So the cycle is

```
model ↓  model ↑  via ↓  via ↑  first ↓  first ↑  in/M ↓  in/M ↑
out/M ↓  out/M ↑  window ↓  window ↑  t/s ↓  t/s ↑  elo ↓  elo ↑   → back to model ↓
```

and one key reaches every order the table has. **The first of each pair is the way that
column is asked about** — cheapest for `in/M` and `out/M`, quickest for `first`, biggest for
`window`, fastest for `t/s`, highest for `elo`, A to Z for `model` and `via`. Nobody opens a
price column to find the most expensive model, so the useful order is never two presses away.

**The sorted column wears the arrow** — `out/M ↓`, and `↑` reversed. The foot names the key
(`alt+s sort`) and not the column, because the heading is already saying which column it is.

Four things worth knowing:

- **A column this list published nothing in is skipped.** With no providers measured yet
  there is no `via`, `first` or `t/s` column, so the cycle steps over them rather than
  stopping on a press that changes nothing you can see.
- **Rows that published nothing sort to the bottom, both ways round.** A model with no
  price is not the cheapest one, and reversing the column does not make it the dearest: it
  is not in the comparison at all. That holds for the sparse columns too — `via`, `first`
  and `t/s` are blank on every model nobody has measured yet, and those rows sit together
  under the ones that carry a figure. A price needs both halves to count: a model that
  published a prompt price and no completion price draws no price, and sorts with the
  blanks.
- **Rows a column cannot tell apart come back alphabetically.** A sparse column leaves a
  whole block of them, and the name is the one order every row has — so the same press
  draws the same screen twice instead of leaving the block in whatever order the press
  before it produced.
- **With something typed, the name column is best match first.** `gpt` puts `gpt-5-classic`
  above `anthropic/claude-gpt-echo`, because that is what you asked for; the arrow then
  decides the order inside a band of equal matches. With an empty box every row matches equally and the
  column is plainly alphabetical.
- **A service heading keeps its place, and the sort happens under it.** If you have
  connected your own service the list is drawn under one dim heading per service, in the
  order those services are held, and no column reorders them — `out/M ↓` puts the cheapest
  `openrouter` model at the top of the `openrouter` rows, not your local box above them.
  With one service, which is most doors, there are no headings and the arrow means the
  whole list.
- **Pressing the key puts the cursor on the top row**, because the top row is the answer to
  the question you just asked. Opening the list still lands on the model in use. It sorts
  whatever the filter kept, so `deep` then `alt+s` is the deepseek rows in that column's
  order.

**Inside the providers it sorts the providers** — whichever table the cursor is in. The two
keep their own orders.

This is where the filter box's `fast` and `cheap` went. They sorted the list too, and the
problem with them was never sorting — it was that a word typed into a name box is an
undiscoverable way to ask for it, and that two words were two opinions about seven columns.

## You cannot filter the picker by speed, price or capability — @cloudflare, <1s, >50t/s, $<0.3, fast, cheap

**The filter box searches model names and nothing else.** There is no way to type a
question about speed, price, tool support, precision or modality into it.

Until 2026-09-17 there was: `@cloudflare` kept the models one provider serves, `<1s` and
`<800ms` bounded how soon an answer starts, `>50t/s` put a floor under how fast it writes,
`$<0.3` capped the price per million output tokens, `fp8` and `bf16` asked for a precision,
`tools` asked for tool support, `sees` and `draws` asked about images in and out, and `fast`
and `cheap` reordered what was left. **Every one of those is now an ordinary thing to search
for.** Typing `$<0.3` looks for a model whose name carries those characters, finds none, and
the list is empty — it does not quietly answer the old question.

**What replaced it is the table.** Every fact those words asked about is a column you can
read: `first` is how soon an answer starts, `t/s` how fast it writes, `in/M` and `out/M` the
price, `window` the context, `inputs` and `outputs` the modalities — and inside a provider
fold, `$/M`, `note` and `up` per provider. A question you can see the answer to does not
need a syntax, and a syntax nobody can discover is a feature only the person who wrote it
can use.

**To sort or filter by these, use the places that do it:**

- **`codeaf models`** from a terminal prints the same catalog as text, where `grep`, `sort`
  and `awk` do anything this box ever did and more.
- **`→` on a model** lists its providers with their own figures, alphabetically, so the
  cheapest or quickest of them is one column-read away.
- **`/settings` → routing** is how you say *which* of them to prefer for every model at
  once — `price`, `latency` or `simple` — rather than hunting one model at a time.

The name search itself is unchanged: whitespace splits, every word must match, and each is
scored by the same fuzzy alignment the rest of this surface searches with — a word that
starts an id, or lands right after a `/`, outranks the same letters scattered through it.
So `ds v4` finds `deepseek/deepseek-v4-flash` and `claude 4.5` finds
`anthropic/claude-sonnet-4.5`.

## Why did it say via cloudflare — the provider named beside your model

Beside your model on the line above the message box, `via <name>` is the provider that
actually answered, and it is a fact rather than a decision: it is the name that came back
on the answer. While an answer is being written it names the provider writing it, as soon
as that provider has named itself — so the first answer of a conversation carries it too.
It goes quiet only when nothing is being written and no answer has come back in the last
ten minutes.

**It is drawn whoever served, the vendor's own providers included.** `glm-5.3-flash · via
z-ai` identifies the machine that served the answer. The model now keeps its full
address, including the organization prefix, on both home and conversation seams;
the serving machine is a separate fact. Until
2026-09-09 the rider was hidden in exactly that case, and what it produced was a name
that came and went as the router moved between a vendor's own providers and everybody
else's — which reads as codeaf having lost track of who is answering. The `served` row on
`/status` and the phone sheet still leaves it out, because the line above it there is the
model's whole routing address.

**The rate is not on that rider.** How fast the stream is producing is a claim about now
and stands at the right edge of the status row instead, as `38 tok/s`, while the answer
is being thought or written.

**While a turn is running the right edge says what the connection is doing.** The phase
outranks the older readings, so that spot reads `first word · 3.1s → parasail at 4.4s` or
`paced · retry in 6s` until something starts arriving, and then the rate alone. The
ranking is by tense: the phase is what this request is doing, `via <name>` is who
answered, and a sighting's own rate is what some answer did in the last ten minutes —
which is why no such rate is ever drawn as though it were now.

## What "rescued" means on the status line, and "slow · trying …" and "refused"

Those two only appear together, and only when the **speed guard** is on.

When an answer takes much longer to start than that provider normally takes, codeaf asks
the next-best provider the same question, and you read whichever one replies first.

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

**A provider that REFUSED is not a provider that was slow, and the line says so.** When the
router answers that the provider codeaf asked for is not one that serves this model —
`No allowed providers are available for the selected model. … but your request's
provider.only preference permits only: coreweave` — the same spot reads
`refused · trying nextbit…`. That provider is then finished for this model: it is not asked
again, and it leaves the set codeaf chooses from for thirty minutes. If the
provider the answer moved to refuses as well, the promise is withdrawn rather than left on
the screen, and the row reads `nextbit refused`. If the second provider fails for any other
reason, or the turn is stopped while it is out, the promise comes off too and the row goes
back to naming the provider that answered last.

If the second provider wins, the line reads `via coreweave · rescued` for that answer once the
request has finished, and goes back to normal on the next one.

Whichever way it lands, the loser is cancelled and what it told codeaf about that provider
is kept, so a rescue is also a free measurement.

## Speed guard — what it costs and when to turn it off

**speed guard** is a row on the Providers tab of the settings panel, directly under
**provider** and two rows under your model, and it is **on**.

It hedges **at most one extra call** per answer and stays under **a tenth** of what the
session spends. It does nothing under `routing: price` — nobody is buying seconds there —
and nothing while an answer is already flowing normally.

**Under the shipped `routing` row it buys no measurement.** `simple` sends what you asked
for and nothing else, so the one-token measurement in the next section is not bought at
all — nothing on codeaf's side is choosing a provider for it to inform. Set **routing** to
`latency` or `price` and it is bought again.

Turn it off if you are paying for every token and never mind waiting. With it off, the
`auto` row in the model picker says `no rescue`, so you can see the promise it is making
— and the measurement described in the next section stops being bought as well. The two
are one row because they are one promise: codeaf may spend a little extra to keep an
answer moving.

## Does codeaf send anything while I am typing — the one-token measurement it sends before you press enter

While you are typing, and before you press enter, codeaf sends **one token** to each of
the two providers your next message would most likely go to, and times how long the first
word took to come back. It does that for two reasons: the router's own published figures
are a half-hour average over everybody's prompts, and this is a measurement of **your**
path to that provider taken seconds ago — and the connection is left warm, so the real
answer's first word is not also paying for a handshake.

**What it costs.** About **two hundredths of a cent** per turn: ten tokens in and one
token out, twice.

**How often.** At most one pair every **twenty seconds** per model, however fast you
type — so a long message buys one, not one per keystroke. **None at all under the shipped
`routing` row**: `simple` buys no measurements, because nothing on codeaf's side is
choosing a provider for them to inform. Also none when the **speed
guard** is off, when `routing` is `off`, when the provider row says `openrouter`, when the
pool is already backing off a rate limit, when codeaf is still recovering a dropped
connection, or when **nobody is waiting on that model** — a task working on its own and
an errand buy none, because the measurement exists to shorten a wait somebody is sitting
through.

**Nothing ever waits for it.** It is sent and forgotten; a message you send a moment
later does not wait on it, and a probe that fails teaches nothing and changes nothing.

**It is not in `/cost`.** It is a real request to a real provider and your provider bills
you for it, and codeaf's own figures do not include it. The next section says why, what
it adds up to, and where to see it.

## Spend that /cost does not show — why the typing measurement is missing from the figures, and how to stop codeaf sending requests you did not ask for

There is exactly one request codeaf makes that its own money figures do not count: the
**one-token measurement** it sends while you are typing, to warm the connection and time
the provider your next message is heading for. Your provider bills you for it. `/cost`,
the status line, the spend place (`alt+5`) and the total at the end of `codeaf do` all
leave it out, and so do the call-log rows and `codeaf-census`.

**Why it is missing.** Those figures are all counts of the **call log**, and the
measurement deliberately writes no row there — it skips the shaping, the retries and the
record on purpose, so that what it times is one clean request to one named provider and
not a retry of one. And it hangs up **at the first word**, so the token count a provider
sends at the end of a reply never arrives: there is no measured figure to add up, only
the fixed shape of the request.

**What it comes to.** Ten tokens in and one token out, twice — about **two hundredths of
a cent** a pair, at most one pair every twenty seconds per model, and only while somebody
is actually sitting there waiting. An unbroken hour of typing is a few cents. It cannot
run in the background, and a task working on its own buys none.

**How to see it anyway.** Every measurement it buys is appended to
`~/.codeaf/v3/lanes.log`, one line of JSON each, with the ones bought this way marked as
probes. That file is the record of what was sent.

**How to make it zero.** It is already zero on a home where nobody has touched
**routing**: the shipped row is `simple` and it buys none of these. If you have set
`latency` or `price` and want it back to zero: Settings → Providers → **speed guard**,
off. The same row governs asking a second provider when an answer is slow to start, so
turning it off stops both. `routing simple`, `routing off`, and a provider row set to
`openrouter`, also stop it.

## The provider row in settings — auto, pinned, pinned but borrowable, openrouter

Settings → Providers has two rows under **routing**:

```
 your model     deepseek-v4-flash
 provider       auto
 speed guard    on
```

`enter` on **provider** walks it through four answers:

| Value | What it does |
|---|---|
| `auto` | the router routes, and codeaf takes over choosing the provider if its answers start coming back refused or unusable — handing it back once it has been well for a while |
| `pinned: cloudflare` | every request goes to that provider and nowhere else, until the router says that provider cannot serve this model — then this model routes on auto for the rest of the run and codeaf says so once |
| `pinned: cloudflare, borrow when slow` | it goes there, but a slow answer may still be rescued elsewhere |
| `openrouter` | no provider is asked for; the router balances on price, and codeaf never takes over |

The pinned rungs are missing until codeaf has measured something — there is no honest
provider to name yet, so the walk is `auto` ↔ `openrouter`.

The **your model** row says which provider is answering it beside the model id — `pinned:
cloudflare` once the choice is yours, and `auto (cloudflare now)` while it is codeaf's,
which under the shipped `simple` row it never is.
`provider` and `routing` are different questions: routing is what every request **prefers**
(fastest, cheapest, or nothing at all), and provider is which provider requests from your home
actually land on. Under `simple` routing — the shipped row — the borrow rung is moot: the pin goes out
strictly — that one provider, `only`, fallbacks off, nothing else on the request — because
simple runs no choosing of its own for a slow answer to borrow. The `switch to auto?`
question a slow pinned provider raises still has somewhere to send you — it asks whether to
let go of the pin for that one answer, and asking is all it ever does. The `auto` row of
the table above is the other rung that reads differently there: under `simple` nothing
takes over, so the `auto` row names no provider beside its sentence and the **your model**
row drops its `auto (cloudflare now)` tail rather than name a provider nobody chose.

## Why does the same conversation suddenly cost more? Keeping the prompt cache warm

Every request in a conversation re-sends the whole conversation. What keeps that from costing a fortune is the **prompt cache**: the provider that answered you a moment ago still has those tokens, and re-reading them costs a fraction of sending them fresh. The catch is that the cache sits on **one provider**. An provider that has never seen your conversation charges full price for all of it — measured on a real run, the same 94,000-token context cost **4.7 times more** on a cold provider than on the warm one, and that alone is where a quarter of the requests in that run ate half its money.

**This is something codeaf does under `routing: latency` and `routing: price`, and not under the shipped `simple` row.** Under `simple` the request carries no preference of codeaf's own at all, and asking for last time's provider is a preference — so keeping the cache warm is the router's business there, as the rest of the choosing is. The row is one word away if you want it: `/settings` → Providers → **routing**.

Under those two rows, codeaf remembers which provider answered your last request and **asks for that same provider first on the next one**. It is a preference, not a demand: if that provider is busy or gone, the request still goes through somewhere else rather than failing. Nothing extra is sent and nothing is probed to work this out — it is the name that came back on the last answer.

It moves off that provider when the provider stops earning it:

- **The request failed there** — an error, a refusal, or a reply that went quiet or turned to garbage halfway through. The next request is routed afresh.
- **It charged too much.** The same quarter-over-list price cap described above rides on every one of these requests, and an provider that billed above it loses its place. A warm cache is never worth any price.

A successful answer with no reported cache hit keeps its place. The prefix may have changed, the old cache may have expired, or the provider may have omitted its cache accounting. That answer can warm the next request; switching immediately would make it cold again. The slow-response monitor still applies.

The same stable identity also travels in OpenRouter's session header so a successful cold request can establish continuity before the first reported cache hit. A changed opening after compaction keeps that identity.

**Answering a question does not cost the cache.** What sits in front of every message — the instructions, the folders you attached, your standing orders, the newest few decisions — is re-sent unchanged on every request, and one changed byte in it re-prices the whole conversation at full price. Answering a question used to change it, so every `allow once` on a tool bought that re-send on the very next message. It does not any more: the decision is written to the record on disk, the model reads the answer in the result that comes back to it, and the copy in front of the conversation is brought up to date only when something else there moves anyway — a folder attached, a standing order agreed.

Each of your conversations keeps its own provider, and so does each worker on a task, because each of them is sending a different transcript. Background work is kept warm the same way: its first request asks by whatever the row asks for, and after that it comes back to whichever one answered. `routing` at `simple` — the shipped row — or at `off` sends none of it.

## A model that cannot stop thinking — what turning thinking off does on it, and why some models think at "max" by default

Some models cannot have their thinking pass switched off at all; the model list says so
for each one (GLM 5.3, Gemini 3.7 Flash and Grok 4.6 were among them on 2026-08-28).
Asking such a model to think less does not fail and is not ignored: codeaf sends the
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
comes back empty with the whole ceiling spent, codeaf remembers that model thinks
regardless and leaves room from then on; it never remembers it on a guess.

## Where are the logs of what codeaf sent the model — the model-call log, and reading it with `codeaf logs`

Every call codeaf makes to a model writes a line to one file, always, with nothing to
switch on first. It lives at `~/.codeaf/logs/calls.jsonl` — beside the rest of what codeaf
keeps, and under `CODEAF_PROFILE_DIR` when you have moved that. Read it with:

```
codeaf logs                 the last 40 calls, newest last
codeaf logs --tail 200      more of them
codeaf logs --follow        keep printing calls as they happen
codeaf logs --path          print the file and nothing else
```

One line per call, and it reads like this:

```
21:12:53  compile  z-ai/glm-5.3-flash  auto→coreweave  low  max 10240  → 200  12.7s  first token 0.4s  deadline 8.0s  stop  1204 in  466 out  1024 cached  $0.0003  acted hedge  drift  2 arms  hedged  waste $0.0012
21:12:41  compile  z-ai/glm-5.3-flash  deepinfra  low  max 10240  → 400  0.2s  Reasoning is mandatory for this endpoint  learned reasoning_mandatory
21:13:04  leaf  #build  z-ai/glm-5.3  novita  high  max 65536  deadline 30.0s  ⋯ in flight 3m12s
21:14:06  leaf  #build  z-ai/glm-5.3  novita  high  max 65536  → 200  1m2s  acted hedge  rate collapsed  no rescue: budget
```

What one line holds: when the call went out, what it was for (`turn`, `leaf`, `task`,
`compile`, `ground`, `brief`, `contract`, `gate`, `reflex`, `satisfied`, and the errands codeaf runs
for itself — `distill`, `narrate`, `title`, `consolidate`, `reflect`, `sentinel`, `quorum`,
`morning-brief`, `craft-repair`, `craft-params`), which model was asked, **which endpoint
was asked for and which one actually answered**, the thinking level and the **ceiling that
really travelled** — which is larger than the one asked for, because the thinking pass is
given room in front of the answer — how many messages and tools the request carried, the
status it came back with, how long it took and **how long the first token took**, the
deadline the wait was being held against, how it finished, **what it spent in tokens and
what it cost**, anything that was **done about a silence**, and anything the refusal taught
codeaf about that model.

**`auto→coreweave` is the router overriding a choice** — the endpoint asked for on the
left, the one that answered on the right. When they are the same you see one name, and a
call to something that is not a router shows none. **`acted hedge · drift · 2 arms ·
hedged · waste $0.0012`** is a call that went quiet, had a second request fired at another
endpoint to rescue it, names the controller's reason, and says what the arm that lost cost.
That reason can be `first token late`, `drift`, `long think`, `ceiling`, `no heartbeat`, or
`rate collapsed`. **`acted hedge · rate collapsed · no rescue: budget`** means the visible
answer had slowed to a crawl and the controller called for a hedge, but the spending limit
kept the second request off the wire. The other refusal words are `no alt` when no untried
machine remained and `no room` when the arm limit had already been reached. Almost every
line has none of that, because almost nothing has to be done or refused.

**`pinned high` after the thinking level is a level that did not travel** — the seat's class
value asked for it and something else decided this one call. The word on the left is what the
request really carried; the pinned word is what was overridden. A line with no pinned word is
a line where the level asked for is the level sent, which is almost all of them. See *Why is
my crew thinking at low*.

## How many tokens did one call use — the N in and N out figures on a log line

Two figures, and the line says which is which:

```
1204 in  466 out (312 thinking)  1024 cached  $0.0003
```

**`in` is the prompt** — everything the request carried to the model, which is also the only
place a run's context size is written down. **`out` is the completion** — what the model
wrote back. `(312 thinking)` sits beside `out` when the endpoint said how much of what it
wrote went to the thinking pass rather than to the answer; most endpoints do not say, and
then there is no bracket. `1024 cached` is the share of `in` the provider billed at the
cached rate.

Each figure is left off when the provider did not report it, so a reply that came back with
no usage block at all shows neither a token figure nor a cost — a zero there would be a
measurement nobody made. There is no single unlabelled `tok` figure any more: it carried the
completion count only, and a number that does not say which half it is cannot be checked
against a bill or against a context window.

## Find one call in the log — filtering `codeaf logs` by run, call, tag, model or node

The filters are exact matches and they combine, so each one you add narrows further:

```
codeaf logs --tag turn            only the chat's own turns
codeaf logs --model z-ai/glm-5.3  only calls that asked for that model
codeaf logs --node build          only calls belonging to that piece of work
codeaf logs --call 4f2a91c7       one call — both its rows, out and back
codeaf logs --run r-7f3a          one run's calls
codeaf logs --tail 200 --follow --tag leaf    they work with everything else
```

`--call` takes the eight-character call id and is the one filter that shows you **both** rows
of an attempt: the row written when the request went out — marked `sent` — and the row
written when it came back. Everywhere else the reader shows the answer only, because a
start whose end has arrived says nothing the end does not say better, and it is only a
call with no answer yet that reads `⋯ in flight`.

**When a filter finds nothing, the reader says which kind of nothing it is.** A search that
came back empty and a question the log cannot answer yet are different answers, and being
given the first when the truth is the second costs an afternoon:

```
no row in this log carries a run id yet     nothing on this machine is stamped with a run
no calls for run r-7f3a                     rows do carry runs, and none of them is that one
no call 4f2a91c7 in this log                that call id is not in the file
```

**`--run` is honest about today.** The run id is not written onto the rows yet — that is
the debug-record foundation, still being built — so on every machine right now `--run`
prints `no row in this log carries a run id yet` rather than pretending every call belongs
to the run you named. It starts working the day the writer starts writing it, with no
change to the command. **`--node` is equally empty on a headless run's rows** — the node a
call belongs to is not stamped on them yet either, and that lands with the headless
attribution change, so `--node build` today matches only the chat's own work. A tag, a
model or a node that matched nothing gets no sentence: an empty listing already says a
search came back empty, and only an id you pasted is something you believed was there.

## Show me the raw rows, and open one call's body

```
codeaf logs --json                 the matching rows exactly as they are on disk
codeaf logs --json --tag leaf      and only the leaf calls
codeaf logs --body 4f2a91c7        what that call sent and what came back
```

`--json` is a passthrough, not a rendering. It prints the file's own lines, one per line,
unchanged, and **without the path header in front of them**, because the reason to ask for
it is that another program — `jq`, a script, a spreadsheet — is reading what comes out. The
filters apply first, so `--json --tag leaf` is exactly the leaf rows and nothing else.

`--body` takes a call id and prints the request and reply that were recorded for it,
looking first in the run trace (`~/.codeaf/logs/trace/<run>/calls/<id>.json`) and then in
the kept-failure folder (`~/.codeaf/logs/failures/<id>.json`). **Neither is written yet.**
The switch that fills the trace and the always-on keeping of a failed call are both still
being built, so today `--body` almost always prints:

```
no body recorded for 4f2a91c7
```

That is the truthful answer and not a fault. Until those land, the way to get the exact
bytes is `CODEAF_CALL_LOG_BODIES=1`, described below, which puts them on the log line
itself.

**A call still running shows as `⋯ in flight`.** That is the reason a line is written when
a call goes *out* as well as when it comes back: a planning call four minutes into a
65,536-token ceiling used to look exactly like a machine doing nothing.

**A line leaves a number out when nothing measured it.** A row short of `cost_s`,
`wait_s`, `waste_usd` or `cost` is an honest line and not a broken one: `cost_s` is
what a rescue would have cost and there is none to price on a call with nowhere else
to go, and `wait_s` is how much longer the wait was expected to run, which a belief
nobody has measured cannot say. A figure that was never measured is missing rather
than invented, and the row says nothing about it.

Rows written before 2026-09-09 may carry a sentence in their `note` instead —
`cost_s was +Inf and is not on this row.` The wait controller used to price "nowhere
to act to" as infinity, which JSON cannot write, so the figure came off the row and
the sentence explained it. It no longer produces one, and a row that is short of a
figure now simply says nothing.

**The headless waiting line reads the same record.** When `codeaf do` has nothing new to
say it prints `still waiting: … · last call <model> <n> ago`, and that `last call` is the
newest answer this process has heard — the call log's own memory, kept even with the file
switched off — not the moment the work was booked. A worker ten minutes into its work says
`last call … 1s ago`, because that is what is true.

The file rotates at 32 MB and keeps one predecessor, `calls.1.jsonl`. With
`CODEAF_CALL_LOG_BODIES=1` the live file is allowed 256 MB instead — a body-bearing
line is tens of kilobytes and the ordinary cap would turn over after a few dozen
calls. `codeaf doctor` names the file and its size. Set `CODEAF_CALL_LOG=off` to
write nothing at all, or `CODEAF_CALL_LOG=/some/path.jsonl` to put it somewhere
you can watch.

## A log row says "let go of because you chose another model" — what that row is, did my call fail, was I charged for it

**Nothing failed, and the row is there on purpose.** It is written when you name
another model while a request is out that had given you nothing back — see *Can I
switch models while it is replying* above. codeaf let that request go and asked the
model you chose instead, and the row is the record of the one it let go of:

```
let go of because you chose another model
```

- **It is not a failure and nothing is broken.** Your own word is what ended that
  request. The reply you eventually read is on the model you picked.
- **It is written down because it cost something.** codeaf had already reached a
  machine and may have been charged for getting there, and a long step that is later
  read back should show where every second and every cent went — a request that
  simply vanished from the log would make the arithmetic on that step wrong.
- **It does not say your turn stopped.** Rows that mean *codeaf itself ended the
  turn* carry a door (`person stopped`, `taken over`); this one carries none,
  because the turn carried on. Anything reading the log to ask "did this turn end"
  will not count it.
- **You will see one per pick**, so picking twice inside one wait writes two rows and
  each names the request it ended.

## What does the total at the end of codeaf do include — the last line, and why the printed cost should match the call log

A headless run prints one last line, under the answer and any files or lines it learned:

```
16s · 1 node · $0.0074
```

That is `<elapsed> · <n> node(s) · $<amount>` — how long it took, how many nodes ran, and
what it cost. `--json` carries the same money in `spend`, with the two halves beside it:
`spend_work` is what this run's own nodes cost, `spend_overhead` what it cost to decide
what those nodes should be. They always add up to `spend`.

**The amount is every model call the run made**, not the workers' calls alone: compiling
the request, the plan model's reading of what the request states, the planning passes, each
worker's own calls, and the delivery gate at the end. It is summed out of the run's own
usage ledger once the work has stopped moving, rather than guessed at from a day's total.

**So it equals the call log's end rows to the cent.** Add up the `$` on the rows in `codeaf
logs` that came back — the end rows, never the `⋯ in flight` starts, which have no cost
yet — and you get the printed figure. When the two disagree, the receipt is the one that is
wrong: a total under its own ledger is worse than no total at all.

That agreement is newer than the line. The plan model's reading of the request — the pass
the log tags `ground` — used to run outside the ledger and its row was dropped, so a run
printed `$0.0041` against `$0.0076` in its own call log. It is billed with the rest of the
planning now.

## What did you send the model — the prompts are not in the log unless you ask for them

The model-call log records the **shape** of every request and never its contents. It says
how many messages went, how many tools were offered, which knobs were set and what came
back — and nothing of what you wrote, what your files say, or what the model answered.
That is deliberate: a debugging record that quietly accumulated your prompts would be a
liability rather than a tool.

When you genuinely need the exact bytes — a request the endpoint refused for a reason
nothing else explains, a reply that came back malformed — run one session with:

```
CODEAF_CALL_LOG_BODIES=1 codeaf
```

Every line then also carries `request_body` and `response_body`, whole and unedited: your
prompts, your attached file contents, the model's whole reply. Turn it on for the run you
are debugging and off again afterwards, and treat the file as you would the conversation
itself. The live file is allowed 256 MB with this pin on (32 MB without it), so a long
session keeps the bodies you asked for rather than rotating them away after a few dozen
calls.

That pin is also the old spelling of one switch — `CODEAF_DEBUG=1`, `--debug`, or `/debug`
in a conversation — which keeps the **debug record** of a run in a folder of its own.
The bodies live there now (each model call under `calls/`, each tool call and each
choice on `events.jsonl`), so this file can stay small enough to grep and a long run
cannot rotate away the failure you came for. The pin still also writes the bodies onto
the log for one release, so a shell history that uses the old word still gets them. The
debug-record page says where the folder is and what is in it.

**Why did that call fail?** The line says. A `→ 400` carries the endpoint's own first
sentence; a line with no status at all is a request that never reached an endpoint; a line
marked `empty at the ceiling` is the thinking pass having spent the whole reply room
before the answer began, which is the one failure a larger ceiling actually fixes. Failed
attempts get their own lines, so a call that was rate limited four times before it landed
is five lines rather than one slow one.

## Why did a provider error keep the same endpoint?

A provider can accept a request and later end its reply with
`finish_reason=error`. codeaf treats that as a failed request, including when
the provider sends no separate error message. It releases the automatic cache
preference, records the failure, and leaves recovery to the existing bounded
retry policy. That failed generation does not teach a successful provider
speed. Any usage the provider reports is still counted.

This does not guarantee a different endpoint: your routing settings, available
providers and recovery budget still apply. A valid tool call or a normal
reasoning response keeps its existing handling.

## Does losing my connection change provider ratings?

No. The connection wait pauses provider-switch timers. Once the endpoint is
reachable, those timers restart, and that call is excluded from learned provider
speed because the local outage was not time spent generating an answer.

## Why a small model gets a shorter page and fewer tools — the lean profile

On a model with a small context window, codeaf sends a smaller set of
instructions and a smaller tool list. Nobody is asked to choose: the
`prompt profile` row is `auto` out of the box and works it out. Lean applies in
exactly three cases, and nothing else:

- the model's context window is under 32,000 tokens — the figure the catalog or
  the endpoint reports, which is what a local runner like llama.cpp, ollama or
  LM Studio tells codeaf about the model it has loaded; or
- the `prompt profile` row under `models` in `/settings` says `lean`; or
- you put `CODEAF_PROMPT_PROFILE=lean` in front of the command, which pins it
  for that one launch and holds the row read-only while it is set.

Lean changes four things:

- `# Interrupts and steering` comes off the page. What it explains, each
  interrupting message now says in its own first words.
- Seven verbs wait one call away instead of riding in front of every request:
  `propose_task`, `tasks`, `watch`, `track`, `commit`, `recall` and
  `read_document`. They are all still here — `load_capability` fetches a group
  and the schemas arrive on the next request, in the same turn.
- `ask` is put straight in the tool list rather than waiting to be fetched, so
  a model that makes one call per message can still put a question to you.
- Saved memories are off. There is no `remember` verb and no `<memory>` block,
  and the reply says so plainly if you ask. Nothing is deleted, and a larger
  model brings them back. The record of what was said is untouched, and still
  searchable.

The project's own instructions still ride, cut at 2KiB instead of 8KiB, and
only the first file found of `AGENTS.md` and `CLAUDE.md`. The reply says the
file was cut and where the rest is.

Why: everything in front of a request is re-sent on every round of every turn.
On a 128,000-token window that is a few percent; on a 16,000-token one it is
most of the room the model has to think in.

## Is an open-weight or local model given the lean profile? Does deepseek or glm get a shorter page?

Only if its context window is under 32,000 tokens, or you chose `lean` yourself
on the `prompt profile` row or pinned it for the launch. Nothing about a model's
licence, its vendor, its name or which crew seat it sits in makes a session
lean.

So an open-weight model with a large window is NOT lean. `glm-5.3-flash` and
`glm-5.3` are served with 128,000 tokens of room, so they get the full
page, the full tool list and saved memories, exactly like any other
128,000-token model — including when they are the model your crew preset picked
for the `worker` seat, and including when you then choose that same model in
chat. Open weights are a licence, not a size.

A model you run yourself usually is small, and it is recognised by the window it
reports, not by its name: llama.cpp, ollama and LM Studio all tell codeaf the
window the loaded model was given.

The `prompt profile` row and `CODEAF_PROMPT_PROFILE` both overrule the window,
in the same three words. The next section says which wins.

## The prompt profile setting — choosing lean or full yourself

`/settings`, under `models`, has a row called `prompt profile`. It takes three
words:

- **`auto`** is the default and the shipped behaviour: the window decides, lean
  under 32,000 tokens and full at or above it.
- **`lean`** sends the shorter page and the shorter tool list whatever the model
  reports.
- **`full`** sends everything whatever the model reports.

**A change lands the next time codeaf starts.** The profile is settled once when
a conversation opens, because it decides the page and the tool list every
request in that conversation is sent with.

**`CODEAF_PROMPT_PROFILE` still pins it for one launch, over the row.** Put
`CODEAF_PROMPT_PROFILE=lean` or `CODEAF_PROMPT_PROFILE=full` in front of the
command and that launch uses it; the settings row goes read-only for as long as
the variable is set and says which variable owns it, exactly as every other
pinned row does. `CODEAF_PROMPT_PROFILE=auto` puts the window back in charge for
that launch. Any other value is not a pin at all: your row stands and, if it is
`auto`, the window decides as usual.

**When to touch it.** Almost never — the window is right almost every time. The
case it is there for is an endpoint that reports a window its loaded model does
not really have, which is where `lean` is you telling codeaf the truth. `full`
is the other direction: a small window you would rather spend on the whole tool
list than on the conversation.

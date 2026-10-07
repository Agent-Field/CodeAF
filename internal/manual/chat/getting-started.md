# Getting started — the first-time setup

## I just installed it — what is the first thing to do after installing codeaf

Run `codeaf`, or press `enter` when the installer asks `Start codeaf in <folder> now?`.
That is the whole of it: the installer leaves the program at `~/.codeaf/bin/codeaf`,
links it into a folder already on `PATH` when it can, and asks nothing else of you, and the setup described below is the
only setup there is. It opens by itself the first time, so there is no command to go
looking for and nothing to configure by hand first.

The install line itself — and updating to a newer build — is on the *running from the
terminal* page, under *How do I install or update codeaf*.

## Getting started — first time setup, what happens the first time I run codeaf

The first time `codeaf` opens a new local conversation on a profile with nothing in it,
setup appears in the chat instead of an empty prompt and a provider error. When the
default provider needs a key and no daily limit is configured, it has **two screens** — under a minute,
nothing else on the frame:

1. **connect openrouter** — the default provider; `enter` signs in in your browser, and pasting an existing key also works
2. **Basic settings** — one screen with two controls on it, **Daily limit** and
   **Chat model**, each already showing the value that is in force

**With a key already found, there is no setup screen.** When a provider key is saved in
the profile or set in the environment, such as `OPENROUTER_API_KEY`, a plain launch skips
the connection screen and **Basic settings**. With nothing elsewhere to show, it
opens the chat's greeting, `What would you like to work on?`; when other conversations
are available, it opens home. `/budget` sets a daily limit
later. A `--no-host` launch on such a profile skips only the connection screen,
and still opens **Basic settings** while no daily limit is set. A resumed conversation,
or one on another machine, never opens first-run setup.

The second screen's way out is **`Start a conversation`**. Every control on it opens on
the value you already have, so pressing `enter` there agrees to exactly what is on the
screen. Its heading is `Basic settings`, one line, with the keys line directly under it
(until 2026-10-01 it was `Models and spending` over `Keep these choices or change them.`).

## Skipping setup and reading its header

`esc` on the first screen skips the setup: the flow is marked seen and it does not open
again. `esc` on the controls screen goes **back** to the connection when there is one
behind it, and skips when the controls are the whole of the setup. A skip leaves one dim
line naming the doors onto what it walked past: `still yours to set · /budget sets what
codeaf may spend · /model and /crew pick the models`. If the default OpenRouter provider is
still not connected and the conversation is using one of its models, its one-step screen
returns on the next local interactive launch because that model cannot work without it. A
conversation on a connected direct provider's model does not owe OpenRouter a key, so that
step stays away.

Codex is deliberately not another first-run step. After setup, its browser sign-in is
available from the Codex row in `/connect`, or from `codeaf connect codex` without
opening the chat.

The header reads `codeaf`, with `setting up · 1 of 2` under the wordmark on the first of two
steps. With only one step it reads `setting up`, without a count. The foot names the keys that work on the row you
are standing on — `tab` walks the rows, `?` opens a control's detail — and on a narrow
window it is cut by whole clauses rather than mid-word.

**On a window too short for the whole screen the explanations are what go**, a whole
sentence at a time and never half of one. The two values, `Start a conversation` and
the keyboard line are never given up, so a sixteen-row window still shows a screen you
can answer and leave.

## Set up my api key — the default provider's openrouter key step, and what happens with no key

On a local interactive launch using codeaf's built-in default model provider, the first step reads
*connect openrouter*. Press `enter`: codeaf opens OpenRouter in your browser, waits on a
random return address bound only to `127.0.0.1`, and uses an S256 proof key for the trip.
After you sign in and approve it, OpenRouter makes a user-controlled API key for the default provider in this
profile and sends the browser back to codeaf. The browser says it is connected, the screen
continues, and the running conversation can use the key immediately. No prompt is sent and
no model is called during the connection.

The address is also written on the waiting screen. If the browser cannot be opened, select
or click that address yourself. `esc` while waiting cancels the return listener and leaves
you on the default provider's OpenRouter step; another `enter` tries again.

## What the setup screen says when something goes wrong

Every refusal on this screen is a sentence about what happened and what to do — never a
programmer's error text. There are four of them, and the settings row's own words for
anything you typed:

| What failed | What the line under the box says |
| --- | --- |
| the browser sign-in never started | `could not reach openrouter to start the sign-in — check the network, or paste a key instead` |
| your browser would not open | `could not open your browser · open the link above` |
| the sign-in started and never came back | `the browser sign-in did not finish — enter tries again, or paste a key instead` |
| the answer could not be written down | `could not save that — the folder codeaf keeps your settings in is not writable` |

Anything you typed that a setting refuses keeps that setting's own wording — `that's not a
dollar amount — a number, or none for no limit` on the rails step, or
`not the shape of an openrouter key — they start with sk-or-` on the key step — because
those are written for you to read. What is never shown is the operating system's version of
a failure: a path inside codeaf's own storage with an errno after it tells you nothing you
can act on.

## Paste an existing OpenRouter API key for the default provider instead of connecting in the browser

Already have a key? Paste it on the same first screen instead of pressing `enter` on an
empty box. The key is masked while it is typed, and the manual-key address remains on the
screen: `https://openrouter.ai/settings/keys`. A pasted key is checked for **shape only** —
it has to start with `sk-`, be at least 20 characters long, and hold no spaces. Nothing is
sent anywhere to find out whether it works; a key of the right shape that the provider does
not accept is discovered by the first message you send. One that fails the shape check
leaves this line under the box and stays on the step:
`not the shape of an openrouter key — they start with sk-or-`.

What it writes for the default provider: the `api_key` field of your profile's `config.json` (under `~/.codeaf`),
owner-readable only. That is the same field the **openrouter key** row on the settings
panel's Connections category writes, and the one every later launch reads. The running
conversation takes it at once — the next message rides it, no restart.

## Skip the default OpenRouter provider, retry later, and keep the message I typed

`esc` on the idle step skips setup. When the conversation is using the default provider, it
then says one dim line:
`openrouter is not connected · enter on your message connects in a browser, or export
OPENROUTER_API_KEY`. Your draft is not sacrificed to a provider error: type it normally and
press `enter`, and the one-step connection opens over the conversation before the draft is
cleared. Connect, then press `enter` again to send those same words.

When the conversation is on a connected direct provider's model, pressing `enter` sends
those words instead. The OpenRouter step does not open and the missing-OpenRouter line is
absent, because that turn already has a provider that can answer.

This default-provider step also opens over an existing or resumed conversation and over a profile
whose first-run setup was already shown. It appears whenever all of these are true: the
launch is local and interactive, the built-in OpenRouter endpoint is still the model
provider for the conversation's model, and neither the shell nor the profile holds a key.
A connected direct provider carrying the conversation, a custom `CODEAF_BASE_URL`, a
`--host` session, and a headless `--once` run are not offered an OpenRouter browser trip.
For a headless run using the default provider, start bare `codeaf` once to connect in a terminal, or export
`OPENROUTER_API_KEY` before running it.

**If the default provider's `OPENROUTER_API_KEY` is already set in your shell, this step is not shown at all.**
That variable outranks the profile. On OpenRouter the profile key comes next, then
`OPENAI_API_KEY` only when it starts with `sk-or-`; an unrelated OpenAI key does not
skip setup. Custom `CODEAF_BASE_URL` endpoints use `OPENROUTER_API_KEY`, then
`OPENAI_API_KEY` of any shape, then the profile key. The setup only asks for what
nothing else has answered. `codeaf doctor` names the key source without printing it.

## The daily limit on the setup screen — what may codeaf spend in a day

The first control is **Daily limit**, and it opens on the amount that is actually in
force — `$500` on a profile that has never chosen one, or your own figure if you have.
Its one line reads:

> When all codeaf spends today reaches this amount, new work waits until midnight or you
> raise it. /budget changes it later.

`/budget` in the line is painted as a command chip, the way the message box paints one.

Type a number to change it — the `$` is drawn for you rather than typed — or type
**`none`** for no limit, which is a first-class answer and makes the row read `no limit`.
`?` on the row explains the two ceilings and how they meet: *The day's ceiling for
everything codeaf does: '/budget 50' changes it later and '/budget none' removes it. A
conversation can carry a smaller ceiling of its own set by e.g. '/budget conversation 20'.
Both hold at once, and whichever is reached first stops the work.* The commands are quoted
whole, and each `/budget` in it is painted as a command chip. The day's limit holds
everything until midnight or you raise it; a conversation's holds just that conversation;
the turn already running always finishes (*Models and cost* has the rest). (Until 2026-10-01 the detail said only that the figure counts
recorded spending, that running calls can carry it a little past, and that task crews have
a cap of their own in `/crew`.)

## Where the daily limit is saved and why setup does not ask about every spending limit

The setup screen’s **Daily limit** writes the same setting as `/budget`.
`$500` is **the amount codeaf has always shipped** and this screen did not change it.

What it writes: `daily_budget_usd` in your profile's `config.json`, through **the same
settings row** the Spending tab and `/budget` write, so what this screen lands is
byte-for-byte what a settings edit lands. If `CODEAF_DAILY_BUDGET` is set in your shell it
owns the row: the value is shown with `set by CODEAF_DAILY_BUDGET` beside it and nothing
is written over it.

The **per-plan approval amount** and the **per-conversation ceiling** are no longer asked
here. They keep their shipped defaults — plan approval asks first above `$100`, the
conversation ceiling is `no limit` — and `/budget` or `/settings` → Spending changes them
when they start to matter.

## The first screen asks for a daily limit and a chat model — choosing the model you talk to

**Chat model** is the model you talk to, shown by name — `DeepSeek V4 Flash` rather than
`deepseek/deepseek-v4-flash`. Its line reads *The model you talk to in this
conversation. /model changes it later.* (`/model` worn as a command chip), and `?` adds the exact catalog id, that it also handles this
conversation's tool use, and that tasks get their own crew, picked per task. Opening the
row draws the real catalog: five rows at a time, `↑`/`↓` scroll the rest past, and
**typing narrows it**, so two hundred models are reachable from a form with five rows on
it. Each row is the model's exact catalog id — `qwen/qwen3.8-27b:free` — one flat list, no
friendly names (typing still finds a model by its friendly name). The model you are already on is always on
that list and the cursor opens on it, even with no catalog yet, so accepting confirms
rather than changes. Choosing one goes through the same settings row `/model` writes and
is kept for the next launch, and `enter` then goes on to the next row, as it does on the
limit — so pressing `enter` alone walks the whole form down to `Start a conversation`.

## Enter on the setup model list when nothing matches

On **Basic settings**, the chat-model list accepts only a row it can show.
Enter with nothing matching leaves the list open with
`nothing matches · backspace widens it`. It changes no model, answers no row and
keeps the focus on **Chat model**. Backspace widens the list; `esc` closes it without
taking a model.

## Clicking and scrolling on the setup screen

The **Basic settings** controls take clicks and scrolling. A click on a row is the
key that row would have taken: a
click on the limit focuses it to type into, a click on the chat model opens its list, a
click on one model of the list takes it, and a click on `Start a conversation` leaves. The wheel scrolls the open list. A click on a sentence, a
blank row or the example panel does nothing.

## Why the setup only lists free models when my account is low

**With no credit on the OpenRouter account, the list shows free models only.** When the
account's balance has read low — $0.50 or less, the same reading that puts the low-credits
warning under the message box — the list is cut to the `:free` ids and the catalog rows
priced at zero, its count line says `free only`, and
`Your OpenRouter account is low on credits · the list shows free models only` stands on the
last row of the screen, right-aligned in the warning colour — where the low-credits warning
stands under the message box once you are in a conversation. The model you
are already on stays on the list whatever it costs, so accepting still confirms. The balance
is read right after the key lands, so the cut usually arrives a moment after the screen does;
a top-up is read on the next launch (*OpenRouter credits and free models*).

## My OpenRouter key has expired on the setup screen

On **Basic settings**, an expired OpenRouter key is shown on the last row. The list
is not cut to free models, because they fail on an expired key too. The line names the door to a new key, and the door
depends on whether a connect step came before this screen: with one, it reads
`Your OpenRouter key has expired · esc to paste a new one from openrouter.ai/settings/keys` and
`esc` goes back there; when the key was already in the shell or the profile and only this
screen was asked, `esc` skips the setup instead — the keys line says so — and the line reads
`Your OpenRouter key has expired · /connect takes a new one from openrouter.ai/settings/keys`.

## Why setup does not ask about the task crew

**There is no crew question**, because the crew is three seats — the worker, the planner and the
checker — and codeaf picks all three for each task from what kind of work it is, so there
is nothing to choose before the first task. `/crew` shows the crew, and pins a seat when
you want one model there every time. **Chat model** chooses the model that answers
you; changing it does not change the task crew.

If a **task model** is pinned (`task.model`), one dim line under the chat model says so —
`Tasks are pinned to … · /settings changes that` — because that pin decides the worker
seat, and a screen that did not mention it would be hiding where tasks run.

## What the example panel above the setup form shows

**Above the form**, under the setup header, a bordered panel stands on
any window with the rows to hold the whole of it — the only bordered surface codeaf draws,
so it cannot be read as more form. Its top edge is labelled `○ Example · ` followed by the
example's own title (`Understand an unfamiliar project`, `Hand off something longer`,
`Follow the work and its cost`, `Compare the options`, `Hand off complex coding tasks`); its
bottom edge carries `←  3 / 5  →`, the arrows that browse it. It is as wide as the screen allows,
up to 92 columns, so the request in it stands on one row. It holds one request you could
type and what it leads to.

## Why the setup examples change every three seconds and how to pause them

The **Basic settings** example panel opens on the first (`Understand an unfamiliar project`) and
**turns to the next by itself every 3 seconds**, round and round through the five
(`Hand off something longer` with `/task Fix the failing tests and explain the changes.`,
`Follow the work and its cost`, `Compare the options`, `Hand off complex coding tasks`
with `/senior-dev Add retries with backoff to the HTTP client, with tests.`); a command in
a request is painted as the same chip the message box paints a recognised command with.
`←`/`→` browse by hand and go round the same ring. **Any key or click holds the clock** for 3
seconds from that key — typing an amount, walking the rows, browsing — so the panel never
turns under your hands. Walking the rows does not move it (until 2026-10-01 it followed the
row you were on). The screen-reader tier never turns by itself. The clock stops when setup closes or returns
to the connection step. Each example **types itself out once** on arriving, then settles; typing
settles it at once.

## Why the setup example disappears on a short window

The **Basic settings** form keeps its values and keys when the example panel cannot fit.
Two blank rows separate the panel from the form's heading. On a window
too short to hold the form and the whole panel — 24 rows, say — the panel is not drawn and
the form is unchanged. (Until 2026-10-01 the panel was a second column to the right of the
form, drawn only from 112 columns up, and carried `An illustration. Nothing here has run.`
at its foot; the label on its edge now says that once.)

## What the setup row colours and keys mean

**A row's name says where you are.** Each name — `Daily limit`, `Chat model`,
`Start a conversation` — is in the body colour until you have answered it, blue while
you are on it, and grey once `enter` has acted on it: the limit set, a model taken from the
list. Walking back onto an answered row makes it blue again while you are
there. Opening the list and leaving it with `esc` answers nothing.

The keys line is the form's second row, directly under `Basic settings`. It reads `enter
sets the limit · ↑↓ moves · esc back · type an amount or none · ? detail` on the limit
row; `enter` on the other rows says what it does there (`opens the list`, `starts`).
`↑`/`↓` and `tab` both walk the rows. It does not name the example's
arrows; the panel's own edge does.

The controls screen shows **once, ever**. The default OpenRouter prerequisite above is the only
step that may return.

## What appears once — and why the default provider's OpenRouter step can return

The **Basic settings screen** is shown once per profile. When the first-run screen
closes — finished or skipped — `setup_seen_at` is written into `config.json` with the time,
and no later launch asks those preference questions again. Skipping with `esc` counts as
shown.

The **OpenRouter connection is a prerequisite, not a preference**, and is not suppressed by
that marker. It returns as a one-step screen on a later eligible launch while the key is
still missing. It can also return in the same launch when an unsent model message reaches
`enter`; the draft stays in the box.

That prerequisite is only for the default provider during first run. A second provider is
not required; add one later through `/connect`, as described on the
[services page](services.md).

The once-only controls screen stays away from `--session <path>`, `codeaf
resume`, `--once`, `--host`, pipes, existing conversations, and profiles that have already
seen them. If every answer already exists, the marker is written silently.

The default provider's OpenRouter prerequisite follows a narrower rule of its own. A missing connection is
shown for local interactive `--session <path>` and `codeaf resume` launches too, because
those conversations still need a model. It stays away from `--once`, `--host`, pipes,
custom endpoints, and profiles whose shell or profile already supplies a key.

A person who has **some** of it configured sees only what is missing, and the count in
the header is the count of those: a key already in the shell leaves the controls screen
alone on the frame, with no `1 of 1` counting to one at anybody.

While it is up it is the whole screen: every keystroke belongs to it except `ctrl+c`,
which is still the door (twice, as always). The controls take clicks and scrolling. The returning
provider step may open after you type, but the draft is held untouched underneath it.

## Change what I picked during setup — where each answer lives afterwards

Every answer went through a settings row, so every answer has a door:

| What you answered | Where to change it later |
| --- | --- |
| the default provider's openrouter key | clear or remove it and the next local interactive launch offers **connect openrouter** again; `/settings`, Connections category, the **openrouter key** row still accepts a pasted replacement |
| the crew | nothing was asked — it is auto. `/crew` shows it, and `/crew pin <seat> <model>` pins a seat |
| the daily limit | `/budget` (also `/limits`), or `/settings` → **Spending**. `CODEAF_DAILY_BUDGET` in your shell outranks the row |
| the model you talk to | `/model`, or the **Chat model** row on the setup screen — the same settings row either way |
| memory, permissions, the task countdown | `/settings` — the setup screen does not show them; the note under **Start a conversation** points there |

A credential changed in the settings row reaches the running conversation at once,
exactly as the setup's does. A crew pin and the budget are read live too: the next task
starts on the new pin, and the rail is checked against the new ceiling.

## Which settings the setup does not ask about

**The setup asks about two things and no more.** Memory stays on, tool approvals keep
prompting, and a proposed task keeps its 15-second countdown — none of them becomes a
question there, because none can be answered usefully before you have seen codeaf do
anything. They are taught where they happen: the countdown is on the task card, and the
first permission question explains the actual tool that asked for something.

Under `Start a conversation` one dim line reads **`Everything else is in /settings`**, with
`/settings` painted as a command chip; that is the whole of what the screen says about the
settings it does not ask about. (Until 2026-10-01 a row `Other settings use defaults ·
review` stood between the chat model and the way out and opened three read-only rows —
memory, ask before running, task countdown; a row that showed settings and let nobody change
them was removed.)

Per-plan approval, the per-conversation ceiling, individual crew seats, reasoning,
routing, extra provider keys, concurrency and appearance are all deliberately absent from
the setup. They have doors — `/budget`, `/settings`, `/crew`, `/model` — and they are
asked about at the moment they matter rather than before you have started.

## The first prompt hung — still waiting, /model switches

A first run opens on this build's default unless you picked something else on the setup
screen's **Chat model** row, and `/model` is the door that moves it afterwards. If that first prompt's provider
goes quiet before a word arrives, codeaf does not sit silent until the ninety-second
cut: it tries another provider and says so, naming the door —

```
still no answer — trying another provider · /model switches
```

The line is a rescue of this answer, not a choice you made. Your model is untouched
until you run `/model`. The status row says `switching` while the second request is
out. When no replacement request can start, the message instead says
`still waiting for an answer · /model switches`, and the status remains waiting.
It does not claim to switch. If the default keeps stalling, `/model` is how you
move for good.

## Set your terminal up for codeaf — the font, and Option on macOS

Two settings live in your terminal rather than in codeaf, and both are worth the minute.
Neither is required: codeaf draws a correct screen without them, and everything they buy
has a drawn way to it as well.

**The font.** codeaf is drawn for **JetBrains Mono, regular and bold** — 14px at 21px line
height is the size the design was cut at. Any monospace font with the block and
box-drawing ranges works, and no patched nerd-font is needed anywhere: every mark on home
and the places is a standard Unicode character. Set it in iTerm2 under Profiles → Text →
Font, in Terminal.app under Profiles → Text → Font → Change…, and in kitty, alacritty and
ghostty with `font_family`, `[font.normal] family` and `font-family` in their config
files.

## My Option key types letters on macOS instead of moving to a place

**Option as meta, on macOS.** Every chord codeaf binds is the option key, and on a Mac it is
drawn the way the keycap names it, `opt+enter` to send what you typed off as a task, `opt+1`…`opt+8`
to jump to a place, `opt+.` for the map. (On Linux and Windows the same chords are drawn
`alt+enter`, `alt+1`…`alt+8`, `alt+.`; this manual names both spellings together.) Most Mac
terminals send Option as an accent-composing key until you tell them otherwise, so those
chords type `¡ ™ £ ≥` instead of doing anything. Turn on **iTerm2** → Profiles → Keys →
*Left Option key: Esc+*, or **Terminal.app** → Profiles → Keyboard → *Use Option as Meta
key*, or set `macos_option_as_alt yes` (kitty), `option_as_alt = "Both"` (alacritty),
`macos-option-as-alt = true` (ghostty), `send_composed_key_when_left_alt_is_pressed = false`
(WezTerm). With it on, `opt+1` arrives as escape-then-`1`, which is how meta has been sent for
forty years. With it off, `tab` still walks the places in order and the foot line under the
composer still names what `enter` does — and the first place you land on says so in one dim
line: `your terminal sends opt as a letter — turn on "use option as meta" in …`, naming the
terminal you are actually in.

## Why setup mentions Option on a Mac and which terminals also accept Control

**The first-run setup explains Option on a Mac.** When the questions are done, a Mac gets one more
line: `the places answer opt+1…opt+8 · if opt types a character instead, turn on "use option as
meta" in …`. It is a condition rather than a report — nothing has been pressed yet — and it is
said once.

**On kitty, ghostty and WezTerm there is also a way in with no setting at all:** those
terminals report that they run the kitty keyboard protocol, and where that report arrives
`ctrl+1` … `ctrl+8` jump to the same eight places and `ctrl+.` draws the same map. The map's
own line says `alt+1…8 or ctrl+1…8 go to a place` exactly when the alias is live.

On Linux and on Windows terminals, Alt is already meta and there is nothing to set. The
whole of this is also in *Screen* — see *The font codeaf is drawn for*, *alt or option or opt —
how the chords are spelled on a Mac, on Linux and on Windows*, and *Why my option key types
¡ ™ £ instead of jumping*.

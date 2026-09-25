# Getting started — the first-time setup

## I just installed it — what is the first thing to do after installing codeaf

Run `codeaf`. That is the whole of it: the installer leaves the program at
`~/.codeaf/bin/codeaf` and asks nothing else of you, and the setup described below is the
only setup there is. It opens by itself the first time, so there is no command to go
looking for and nothing to configure by hand first.

The install line itself — and updating to a newer build — is on the *running from the
terminal* page, under *How do I install or update codeaf*.

## Getting started — first time setup, what happens the first time I run codeaf

The first time `codeaf` opens on a profile with nothing in it, the chat does not open on
an empty prompt and a provider error. It opens in the chat itself, on **two screens** —
under a minute, nothing else on the frame:

1. **connect openrouter** — the default service; `enter` signs in in your browser, and pasting an existing key also works
2. **Models and spending** — one screen with three controls on it: **Daily limit**,
   **Chat model** and **Work crew**, each already showing the value that is in force

The second screen's way out is **`Start a conversation`**. Every control on it opens on
the value you already have, so pressing `enter` there agrees to exactly what is on the
screen. Its heading is `Models and spending` and the line under it is
`Keep these choices or change them.`

`esc` on the first screen skips the setup: the flow is marked seen and it does not open
again. `esc` on the controls screen goes **back** to the connection when there is one
behind it, and skips when the controls are the whole of the setup. A skip leaves one dim
line naming the doors onto what it walked past: `still yours to set · /budget sets what
codeaf may spend · /model and /crew pick the models`. If the default OpenRouter service is
still not connected and the conversation is using one of its models, its one-step screen
returns on the next local interactive launch because that model cannot work without it. A
conversation on a connected direct service's model does not owe OpenRouter a key, so that
step stays away.

Codex is deliberately not another first-run step. After setup, its browser sign-in is
available from the Codex row in `/connect`, or from `codeaf connect codex` without
opening the chat.

The header reads `codeaf` on the left and `setup · 2 of 2` on the right; with only one
screen to show there is no count at all. The foot names the keys that work on the row you
are standing on — `tab` walks the rows, `?` opens a control's detail — and on a narrow
window it is cut by whole clauses rather than mid-word.

**On a window too short for the whole screen the explanations are what go**, a whole
sentence at a time and never half of one. The three values, `Start a conversation` and
the keyboard line are never given up, so a sixteen-row window still shows a screen you
can answer and leave.

## Set up my api key — the default service's openrouter key step, and what happens with no key

On a local interactive launch using codeaf's built-in default model service, the first step reads
*connect openrouter*. Press `enter`: codeaf opens OpenRouter in your browser, waits on a
random return address bound only to `127.0.0.1`, and uses an S256 proof key for the trip.
After you sign in and approve it, OpenRouter makes a user-controlled API key for the default service in this
profile and sends the browser back to codeaf. The browser says it is connected, the screen
continues, and the running conversation can use the key immediately. No prompt is sent and
no model is called during the connection.

The address is also written on the waiting screen. If the browser cannot be opened, select
or click that address yourself. `esc` while waiting cancels the return listener and leaves
you on the default service's OpenRouter step; another `enter` tries again.

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

## Paste an existing OpenRouter API key for the default service instead of connecting in the browser

Already have a key? Paste it on the same first screen instead of pressing `enter` on an
empty box. The key is masked while it is typed, and the manual-key address remains on the
screen: `https://openrouter.ai/settings/keys`. A pasted key is checked for **shape only** —
it has to start with `sk-`, be at least 20 characters long, and hold no spaces. Nothing is
sent anywhere to find out whether it works; a key of the right shape that the provider does
not accept is discovered by the first message you send. One that fails the shape check
leaves this line under the box and stays on the step:
`not the shape of an openrouter key — they start with sk-or-`.

What it writes for the default service: the `api_key` field of your profile's `config.json` (under `~/.codeaf`),
owner-readable only. That is the same field the **openrouter key** row on the settings
panel's Providers tab writes, and the one every later launch reads. The running
conversation takes it at once — the next message rides it, no restart.

## Skip the default OpenRouter service, retry later, and keep the message I typed

`esc` on the idle step skips setup. When the conversation is using the default service, it
then says one dim line:
`openrouter is not connected · enter on your message connects in a browser, or export
OPENROUTER_API_KEY`. Your draft is not sacrificed to a provider error: type it normally and
press `enter`, and the one-step connection opens over the conversation before the draft is
cleared. Connect, then press `enter` again to send those same words.

When the conversation is on a connected direct service's model, pressing `enter` sends
those words instead. The OpenRouter step does not open and the missing-OpenRouter line is
absent, because that turn already has a service that can answer.

This default-service step also opens over an existing or resumed conversation and over a profile
whose first-run setup was already shown. It appears whenever all of these are true: the
launch is local and interactive, the built-in OpenRouter endpoint is still the model
provider for the conversation's model, and neither the shell nor the profile holds a key.
A connected direct service carrying the conversation, a custom `CODEAF_BASE_URL`, a
`--host` session, and a headless `--once` run are not offered an OpenRouter browser trip.
For a headless run using the default service, start bare `codeaf` once to connect in a terminal, or export
`OPENROUTER_API_KEY` (or `OPENAI_API_KEY`) before running it.

**If the default service's `OPENROUTER_API_KEY` is already set in your shell, this step is not shown at all.**
The environment outranks the file, always; the setup only asks for what nothing else has
answered.

## The daily limit on the setup screen — what may codeaf spend in a day

The first control is **Daily limit**, and it opens on the amount that is actually in
force — `$500` on a profile that has never chosen one, or your own figure if you have.
Its one line reads:

> When codeaf's spending today reaches this amount, new work waits until midnight or you
> raise it.

Type a number to change it — the `$` is drawn for you rather than typed — or type
**`none`** for no limit, which is a first-class answer and makes the row read `no limit`.
`?` on the row adds the part that matters when the bill arrives: *it counts spending
codeaf records here. Calls already running can carry it a little past. Your provider
account has its own controls.* It is a backstop against a runaway, not a promise about
your whole bill. Something that is not a dollar amount is refused in the settings row's
own words — `that's not a dollar amount — a number, or none for no limit` — and the
screen stays.

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

## The model you talk to and the work crew on the setup screen

**Chat model** is the model you talk to, shown by name — `DeepSeek V4 Flash` rather than
`deepseek/deepseek-v4-flash`. Its line reads *The model you talk to in this
conversation.*, and `?` adds the exact catalog id and that it also handles this
conversation's tool use. Opening the row draws the real catalog: five rows at a time,
`↑`/`↓` scroll the rest past, and **typing narrows it**, so two hundred models are
reachable from a form with five rows on it. The row under the cursor shows its exact id.
The model you are already on is always on that list and the cursor opens on it, even with
no catalog yet, so accepting confirms rather than changes. Choosing one goes through the
same settings row `/model` writes and is kept for the next launch.

**Work crew** is the five models codeaf uses on its own behalf — *Models used to plan,
run, and check tasks.* Opening it draws the three presets — `Frugal`, `Balanced` and
`Max` — each with a whole one-line description of **the choice** (how much model goes on
the work), never a price: this screen makes no claim about what anything will cost you.
`?` on the row shows the seats the crew is actually made of (`brain … · hands … ·
checks …`) and that **a crew change leaves the model you talk to alone**. The crew is the five class rows (`models.tiers.reflex`, `models.tiers.low`,
`models.tiers.worker`, `models.tiers.high`, `models.tiers.mastermind`); the model that
answers you is the row above it, and neither touches the other.

Two rules keep this screen from writing something you did not ask for:

- **`esc` out of either list leaves the row exactly as it was.** A cursor inside a list
  is provisional until you accept it.
- **A crew you arranged yourself is never overwritten.** If any of the five class rows is
  already in your profile — you pinned one by hand, or an earlier `/crew` wrote them — the
  row reads `Custom`, `Start a conversation` writes no preset over it, and the list still
  says `yours is none of the three — picking one puts all five back` if you want one.

If a **task model** is pinned (`task.model`), one dim line under the crew says so —
`Tasks are pinned to … · /settings changes that` — because that pin takes the worker seat
out of the preset's hands and a crew row that did not mention it would be selling you a
dial that is disconnected.

At **112 columns and wider** a bordered panel stands beside these rows, labelled
`○ Example · what you can do` and footed `An illustration. Nothing here has run.` — the
only bordered surface codeaf draws, so it cannot be read as more form. It holds one
request you could type and what it leads to, and follows the row you are on: beside the
crew it shows `/task Fix the failing tests and explain the changes.` That request **types
itself out once** on arriving and on `←`/`→`, then settles; typing settles it at once.
Under 112 columns it is not drawn and the form is unchanged.

The controls screen shows **once, ever**. The default OpenRouter prerequisite above is the only
step that may return.

## What appears once — and why the default service's OpenRouter step can return

The **Models and spending screen** is shown once per profile. When the first-run screen
closes — finished or skipped — `setup_seen_at` is written into `config.json` with the time,
and no later launch asks those preference questions again. Skipping with `esc` counts as
shown.

The **OpenRouter connection is a prerequisite, not a preference**, and is not suppressed by
that marker. It returns as a one-step screen on a later eligible launch while the key is
still missing. It can also return in the same launch when an unsent model message reaches
`enter`; the draft stays in the box.

That prerequisite is only for the default service during first run. A second service is
not required; add one later through `/connect`, as described on the
[services page](services.md).

The once-only controls screen stays away from `--session <path>`, `codeaf
resume`, `--once`, `--host`, pipes, existing conversations, and profiles that have already
seen them. If every answer already exists, the marker is written silently.

The default service's OpenRouter prerequisite follows a narrower rule of its own. A missing connection is
shown for local interactive `--session <path>` and `codeaf resume` launches too, because
those conversations still need a model. It stays away from `--once`, `--host`, pipes,
custom endpoints, and profiles whose shell or profile already supplies a key.

A person who has **some** of it configured sees only what is missing, and the count in
the header is the count of those: a key already in the shell leaves the controls screen
alone on the frame, with no `1 of 1` counting to one at anybody.

While it is up it is the whole screen: every keystroke belongs to it except `ctrl+c`,
which is still the door (twice, as always), and the mouse does nothing. The returning
provider step may open after you type, but the draft is held untouched underneath it.

## Change what I picked during setup — where each answer lives afterwards

Every answer went through a settings row, so every answer has a door:

| What you answered | Where to change it later |
| --- | --- |
| the default service's openrouter key | clear or remove it and the next local interactive launch offers **connect openrouter** again; `/settings`, Providers tab, the **openrouter key** row still accepts a pasted replacement |
| the crew | `/crew` (bare shows the three, `/crew max` sets one), or the **crew** row on the settings panel |
| the daily limit | `/budget` (also `/limits`), or `/settings` → **Spending**. `CODEAF_DAILY_BUDGET` in your shell outranks the row |
| the model you talk to | `/model`, or the **Chat model** row on the setup screen — the same settings row either way |
| memory, permissions, the task countdown | `/settings`; the setup screen only shows them, under `Other settings` |

A credential changed in the settings row reaches the running conversation at once,
exactly as the setup's does. The crew and the budget are read live too: the next call
codeaf makes on its own behalf uses the new crew, and the rail is checked against the
new ceiling.

**The setup asks about three things and no more.** Memory stays on, tool approvals keep
prompting, and a proposed task keeps its 15-second countdown — none of them becomes a
question there, because none can be answered usefully before you have seen codeaf do
anything. They are taught where they happen: the countdown is on the task card, and the
first permission question explains the actual tool that asked for something.

Under the three fields is one row that shows them: **`Other settings use defaults ·
review`** on a fresh profile, and **`Review other settings`** on a profile that has
already written any of them down — it never claims your own settings are defaults.
`enter` on that row opens three read-only rows straight off the settings registry:

```
  memory              on
  ask before running  prompt
  task countdown      15s
  /settings changes these and every other one.
```

Per-plan approval, the per-conversation ceiling, individual crew seats, reasoning,
routing, extra service keys, concurrency and appearance are all deliberately absent from
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

**Option as meta, on macOS.** Every chord codeaf binds is the option key, and on a Mac it is
drawn the way the keycap names it — `opt+enter` to send what you typed off as a task, `opt+1`…`opt+8`
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

**The first-run setup says it too.** When the three questions are done, a Mac gets one more
line: `the places answer opt+1…opt+8 · if opt types a character instead, turn on "use option as
meta" in …`. It is a condition rather than a report — nothing has been pressed yet — and it is
said once.

**On kitty, ghostty and WezTerm there is also a way in with no setting at all:** those
terminals report that they run the kitty keyboard protocol, and where that report arrives
`ctrl+1` … `ctrl+8` jump to the same seven places and `ctrl+.` draws the same map. The map's
own line says `alt+1…8 or ctrl+1…8 go to a place` exactly when the alias is live.

On Linux and on Windows terminals, Alt is already meta and there is nothing to set. The
whole of this is also in *Screen* — see *The font codeaf is drawn for*, *alt or option or opt —
how the chords are spelled on a Mac, on Linux and on Windows*, and *Why my option key types
¡ ™ £ instead of jumping*.

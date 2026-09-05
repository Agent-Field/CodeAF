# Getting started — the first-time setup

## Getting started — first time setup, what happens the first time I run aforge

The first time `aforge` opens on a profile with nothing in it, the chat does not open on
an empty prompt and a provider error. It opens on one centred screen, in the chat itself,
that asks for three things one at a time — under a minute, no borders, nothing else on
the frame:

1. **connect openrouter** — `enter` signs in in your browser; pasting an existing key also works
2. **the crew** — `frugal`, `balanced` or `max`, the same three rows `/crew` draws
3. **the limits** — one screen with three rows on it: `per day`, `per plan`, `per
   conversation`

`enter` accepts each step's default and goes on. Every step's foot names `esc` the same
way — `esc skips setup` — because that is what it does on any of them: the flow is marked
seen and it does not open again. When it is done, or skipped, the ordinary empty
conversation appears — the wordmark box and the prompt. The crew and limit questions
never come back, so a skip leaves one dim line naming the doors onto the ones it walked
past: `still yours to set · /crew picks the five models aforge works with · /budget sets
what it may spend`. If OpenRouter is still not connected, its one-step screen returns on the
next local interactive launch because the model cannot work without it.

The line over the question reads `setting up · 1 of 3`; with only one thing missing it
reads `setting up`. The foot says what `enter` does right now — `enter takes balanced`,
`enter keeps $500`, `enter connects in browser` — and what `esc` does now.

**On a window too short for the whole block the explanation is what goes**, a line at a
time from the bottom of the prose up, and the wordmark with it if it comes to that. The
question, the `›` box you type into and the foot naming `enter` and `esc` are the last
three rows to be given up, so a twelve-row split pane still shows a screen you can answer
and leave.

## Set up my api key — the openrouter key step, and what happens with no key

On a local interactive launch using aforge's built-in model endpoint, the first step reads
*connect openrouter*. Press `enter`: aforge opens OpenRouter in your browser, waits on a
random return address bound only to `127.0.0.1`, and uses an S256 proof key for the trip.
After you sign in and approve it, OpenRouter makes a user-controlled API key for this
profile and sends the browser back to aforge. The browser says it is connected, the screen
continues, and the running conversation can use the key immediately. No prompt is sent and
no model is called during the connection.

The address is also written on the waiting screen. If the browser cannot be opened, select
or click that address yourself. `esc` while waiting cancels the return listener and leaves
you on the OpenRouter step; another `enter` tries again.

## What the setup screen says when something goes wrong

Every refusal on this screen is a sentence about what happened and what to do — never a
programmer's error text. There are four of them, and the settings row's own words for
anything you typed:

| What failed | What the line under the box says |
| --- | --- |
| the browser sign-in never started | `could not reach openrouter to start the sign-in — check the network, or paste a key instead` |
| your browser would not open | `could not open your browser · open the link above` |
| the sign-in started and never came back | `the browser sign-in did not finish — enter tries again, or paste a key instead` |
| the answer could not be written down | `could not save that — the folder aforge keeps your settings in is not writable` |

Anything you typed that a setting refuses keeps that setting's own wording — `that's not a
dollar amount — a number, or none for no limit` on the rails step, or
`not the shape of an openrouter key — they start with sk-or-` on the key step — because
those are written for you to read. What is never shown is the operating system's version of
a failure: a path inside aforge's own storage with an errno after it tells you nothing you
can act on.

## Paste an existing OpenRouter API key instead of connecting in the browser

Already have a key? Paste it on the same first screen instead of pressing `enter` on an
empty box. The key is masked while it is typed, and the manual-key address remains on the
screen: `https://openrouter.ai/settings/keys`. A pasted key is checked for **shape only** —
it has to start with `sk-`, be at least 20 characters long, and hold no spaces. Nothing is
sent anywhere to find out whether it works; a key of the right shape that the provider does
not accept is discovered by the first message you send. One that fails the shape check
leaves this line under the box and stays on the step:
`not the shape of an openrouter key — they start with sk-or-`.

What it writes: the `api_key` field of your profile's `config.json` (under `~/.aforge`),
owner-readable only. That is the same field the **openrouter key** row on the settings
panel's Providers tab writes, and the one every later launch reads. The running
conversation takes it at once — the next message rides it, no restart.

## Skip OpenRouter, retry later, and keep the message I typed

`esc` on the idle step skips setup. The conversation then says one dim line:
`openrouter is not connected · enter on your message connects in a browser, or export
OPENROUTER_API_KEY`. Your draft is not sacrificed to a provider error: type it normally and
press `enter`, and the one-step connection opens over the conversation before the draft is
cleared. Connect, then press `enter` again to send those same words.

This provider step also opens over an existing or resumed conversation and over a profile
whose first-run setup was already shown. It appears whenever all of these are true: the
launch is local and interactive, the built-in OpenRouter endpoint is still the model
provider, and neither the shell nor the profile holds a key. A custom `AFORGE_BASE_URL`, a
`--host` session, and a headless `--once` run are not offered an OpenRouter browser trip.
For a headless run, start bare `aforge` once to connect in a terminal, or export
`OPENROUTER_API_KEY` (or `OPENAI_API_KEY`) before running it.

**If `OPENROUTER_API_KEY` is already set in your shell, this step is not shown at all.**
The environment outranks the file, always; the setup only asks for what nothing else has
answered.

## The crew step — the five models aforge uses on its own behalf

The second step draws the three presets exactly as bare `/crew` does — each preset's
word, its one-line description and the five class models under it — with the cursor on
`balanced`. `↑`/`↓` move it, `enter` takes the row under the cursor.

The sentence above the rows is the one people most need on their first day:

> these five are the models aforge uses on its own behalf — the work inside every
> task, planning, checking, reading every turn. the model you talk to is a separate
> choice, made with /model.

The crew and the model you talk to are **two different settings**. The crew is the five
class rows (`models.tiers.reflex`, `models.tiers.low`, `models.tiers.worker`,
`models.tiers.high`, `models.tiers.mastermind`) that aforge's own side-calls run on; the
model that answers you in the conversation is chosen with `/model` and shown in the status
line, and the crew never touches it. Choosing a crew here writes those five rows in one go,
which is exactly what `/crew balanced` does.

If any of the five rows is already in your profile — you pinned one by hand, or an
earlier `/crew` wrote them — this step is not shown.

## The first-run rails screen — what may aforge spend

The third step is a screen with **three rows on it, not one number**. Its title is

```
what may aforge spend?
enter keeps a default · type a number · none means no limit
```

and under that, in this order, the three rows a new person can answer:

```
 per day            $500
 per plan           asks first above $100
 per conversation   no limit
```

`↑` and `↓` walk between the rows and write nothing — only `enter` writes. The default is
drawn dim where the answer goes, and `enter` keeps it and walks to the next row; typing a
number replaces it, and the `$` is drawn for you rather than typed. Typing **`none`** —
the word the header offers — removes that limit, and the answered row then reads
`no limit`. An answered row keeps its answer on the screen while you finish the rest.
Something that is not a dollar amount is refused in the row's own words and the step
stays. `esc` skips the whole setup.

The foot says what `enter` does right now: `enter keeps $500 · esc skips setup`, or
`enter sets $50 · esc skips setup` once you have typed something.

One dim line under the rows says what it is not asking about: *the rest — a task, a
standing run, aforge's own practice — start with a small limit or none. change any of
them later with /budget.*

What it writes: `daily_budget_usd`, `plan_consent_usd` and `session.spendRailUSD` in your
profile's `config.json` — through **the same settings rows** the Spending tab and
`/budget` write, so what this screen lands is byte-for-byte what a settings edit lands. If
`AFORGE_DAILY_BUDGET` is set in your shell, this step is not shown — the variable outranks
the file.

The rails show **once, ever**. The OpenRouter prerequisite above is the only step that may
return.

## What appears once — and why the OpenRouter step can return

The **crew and spending questions** are shown once per profile. When the first-run screen
closes — finished or skipped — `setup_seen_at` is written into `config.json` with the time,
and no later launch asks those preference questions again. Skipping with `esc` counts as
shown.

The **OpenRouter connection is a prerequisite, not a preference**, and is not suppressed by
that marker. It returns as a one-step screen on a later eligible launch while the key is
still missing. It can also return in the same launch when an unsent model message reaches
`enter`; the draft stays in the box.

The once-only crew and spending questions stay away from `--session <path>`, `aforge
resume`, `--once`, `--host`, pipes, existing conversations, and profiles that have already
seen them. If every answer already exists, the marker is written silently.

The OpenRouter prerequisite follows a narrower rule of its own. A missing connection is
shown for local interactive `--session <path>` and `aforge resume` launches too, because
those conversations still need a model. It stays away from `--once`, `--host`, pipes,
custom endpoints, and profiles whose shell or profile already supplies a key.

A person who has **some** of the three configured sees only the missing steps, and the
count in the title is the count of those.

While it is up it is the whole screen: every keystroke belongs to it except `ctrl+c`,
which is still the door (twice, as always), and the mouse does nothing. The returning
provider step may open after you type, but the draft is held untouched underneath it.

## Change what I picked during setup — where each answer lives afterwards

Every answer went through a settings row, so every answer has a door:

| What you answered | Where to change it later |
| --- | --- |
| the openrouter key | clear or remove it and the next local interactive launch offers **connect openrouter** again; `/settings`, Providers tab, the **openrouter key** row still accepts a pasted replacement |
| the crew | `/crew` (bare shows the three, `/crew max` sets one), or the **crew** row on the settings panel |
| the limits | `/budget` (also `/limits`), or `/settings` → **Spending** — `per day`, `per plan`, `per conversation`. `AFORGE_DAILY_BUDGET` in your shell outranks the day's row |
| the model you talk to | `/model` — this was never part of the setup |

A credential changed in the settings row reaches the running conversation at once,
exactly as the setup's does. The crew and the budget are read live too: the next call
aforge makes on its own behalf uses the new crew, and the rail is checked against the
new ceiling.

## The first prompt hung — still waiting, /model switches

The model you talk to is not chosen on the setup screen. A first run opens on this
build's default, and `/model` is the door that moves it. If that first prompt's lane
goes quiet before a word arrives, aforge does not sit silent until the ninety-second
cut: it tries another lane and says so, naming the door —

```
still no answer — trying another lane · /model switches
```

The line is a rescue of this answer, not a choice you made. Your model is untouched
until you run `/model`. The status row says `switching` while the second request is
out. When no replacement request can start, the message instead says
`still waiting for an answer · /model switches`, and the status remains waiting.
It does not claim to switch. If the default keeps stalling, `/model` is how you
move for good.

## Set your terminal up for aforge — the font, and Option on macOS

Two settings live in your terminal rather than in aforge, and both are worth the minute.
Neither is required: aforge draws a correct screen without them, and everything they buy
has a drawn way to it as well.

**The font.** aforge is drawn for **JetBrains Mono, regular and bold** — 14px at 21px line
height is the size the design was cut at. Any monospace font with the block and
box-drawing ranges works, and no patched nerd-font is needed anywhere: every mark on home
and the places is a standard Unicode character. Set it in iTerm2 under Profiles → Text →
Font, in Terminal.app under Profiles → Text → Font → Change…, and in kitty, alacritty and
ghostty with `font_family`, `[font.normal] family` and `font-family` in their config
files.

**Option as meta, on macOS.** Every chord aforge binds is the option key, and on a Mac it is
drawn the way the keycap spells it — `⌥enter` to send what you typed off as a task, `⌥1`…`⌥7`
to jump to a place, `⌥.` for the map. (On Linux and Windows the same chords are drawn
`alt+enter`, `alt+1`…`alt+7`, `alt+.`; this manual names both spellings together.) Most Mac
terminals send Option as an accent-composing key until you tell them otherwise, so those
chords type `¡ ™ £ ≥` instead of doing anything. Turn on **iTerm2** → Profiles → Keys →
*Left Option key: Esc+*, or **Terminal.app** → Profiles → Keyboard → *Use Option as Meta
key*, or set `macos_option_as_alt yes` (kitty), `option_as_alt = "Both"` (alacritty),
`macos-option-as-alt = true` (ghostty), `send_composed_key_when_left_alt_is_pressed = false`
(WezTerm). With it on, `⌥1` arrives as escape-then-`1`, which is how meta has been sent for
forty years. With it off, `tab` still walks the places in order and the foot line under the
composer still names what `enter` does — and the first place you land on says so in one dim
line: `your terminal sends ⌥ as a letter — turn on "use option as meta" in …`, naming the
terminal you are actually in.

**The first-run setup says it too.** When the three questions are done, a Mac gets one more
line: `the seven places answer ⌥1…⌥7 · if ⌥ types a character instead, turn on "use option as
meta" in …`. It is a condition rather than a report — nothing has been pressed yet — and it is
said once.

**On kitty, ghostty and WezTerm there is also a way in with no setting at all:** those
terminals report that they run the kitty keyboard protocol, and where that report arrives
`ctrl+1` … `ctrl+7` jump to the same seven places and `ctrl+.` draws the same map. The map's
own line says `alt+1…7 or ctrl+1…7 go to a place` exactly when the alias is live.

On Linux and on Windows terminals, Alt is already meta and there is nothing to set. The
whole of this is also in *Screen* — see *The font aforge is drawn for*, *alt or option or ⌥ —
how the chords are spelled on a Mac, on Linux and on Windows*, and *Why my option key types
¡ ™ £ instead of jumping*.

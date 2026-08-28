# Getting started — the first-time setup

## Getting started — first time setup, what happens the first time I run aforge

The first time `aforge` opens on a profile with nothing in it, the chat does not open on
an empty prompt and a provider error. It opens on one centred screen, in the chat itself,
that asks for three things one at a time — under a minute, no borders, nothing else on
the frame:

1. **your openrouter key** — a masked paste box
2. **the crew** — `frugal`, `balanced` or `max`, the same three rows `/crew` draws
3. **a daily ceiling** — one number, `$20` by default

`enter` accepts each step's default and goes on. `esc` skips the whole thing. When it is
done, or skipped, the ordinary empty conversation appears — the wordmark box and the
prompt — and the screen never comes back.

The line over the question reads `setting up · 1 of 3`; with only one thing missing it
reads `setting up`. The foot says what `enter` does right now — `enter takes balanced`,
`enter keeps $20`, `enter goes on without a key` — and that `esc skips setup`.

## Set up my api key — the openrouter key step, and what happens with no key

The first step is a masked paste box under the words *your openrouter key*, with one
line of context: *aforge talks to models through openrouter, on your key and your card.
nothing is sent until you do.* Where to get one is written on the screen:
`https://openrouter.ai/settings/keys`.

Paste it (or type it) and press `enter`. It is checked for **shape only** — it has to
start with `sk-` and hold no spaces — and never against the network, because the setup
runs before you have agreed to spend anything. One that fails the shape check leaves
this line under the box and stays on the step:
`not the shape of an openrouter key — they start with sk-or-`.

What it writes: the `api_key` field of your profile's `config.json` (under `~/.aforge`),
owner-readable only. That is the same field the **openrouter key** row on the settings
panel's Providers tab writes, and the one every later launch reads. The running
conversation takes it at once — the next message rides it, no restart.

`enter` on an empty box goes on without one. The empty conversation then says one dim
line — `no openrouter key yet · paste one into /settings, or export OPENROUTER_API_KEY` —
and the first message you send is refused until one of those has happened. That refusal
is not a fault in the message; it is the setup's `esc` having been honoured. Paste into
`/settings` (Providers tab, **openrouter key**) and send again, no restart, or export the
variable and start aforge again. The setup itself does not return.

**If `OPENROUTER_API_KEY` is already set in your shell, this step is not shown at all.**
The environment outranks the file, always; the setup only asks for what nothing else has
answered.

## The crew step — the four models aforge uses on its own behalf

The second step draws the three presets exactly as bare `/crew` does — each preset's
word, its one-line description and the four class models under it — with the cursor on
`balanced`. `↑`/`↓` move it, `enter` takes the row under the cursor.

The sentence above the rows is the one people most need on their first day:

> these four are the models aforge uses on its own behalf — planning, checking, reading
> every turn. the model you talk to is a separate choice, made with /model.

The crew and the model you talk to are **two different settings**. The crew is the four
class rows (`models.tiers.reflex`, `models.tiers.low`, `models.tiers.high`,
`models.tiers.mastermind`) that aforge's own side-calls run on; the model that answers
you in the conversation is chosen with `/model` and shown in the status line, and the
crew never touches it. Choosing a crew here writes those four rows in one go, which is
exactly what `/crew balanced` does.

If any of the four rows is already in your profile — you pinned one by hand, or an
earlier `/crew` wrote them — this step is not shown.

## The daily ceiling step — the budget

The third step is one number under the words *a daily ceiling*, with the daily budget
row's own sentence beside it: *what aforge may spend on your work in a day. 0 removes the
rail. A change lands at the next rail check.* The default is drawn dim where the answer
goes — `$20` — and `enter` keeps it; typing a number replaces it. Something that is not a
dollar amount is refused in the row's own words and the step stays.

What it writes: `daily_budget_usd` in your profile's `config.json`, through the same
settings row the panel's Workspace tab edits as **daily budget**. If `AFORGE_DAILY_BUDGET`
is set in your shell, this step is not shown — the variable outranks the file.

## It only appears once — when the setup is and is not shown

The setup is shown **once per profile, ever**. When it closes — finished or skipped —
`setup_seen_at` is written into `config.json` with the time, and no later launch opens
it. Skipping with `esc` counts as shown. Nothing on the surface brings it back: there is
no command for it, and the only follow-up it ever leaves is the one dim line about the
missing credential described above.

It is **never shown** when:

- `OPENROUTER_API_KEY` is in your shell **and** the crew and the daily budget are already
  in your profile — there is nothing to ask, and the marker is written silently;
- the launch is `--once`, `--host`, `--session <path>`, or `aforge resume`;
- stdin is not a terminal — a pipe, a script, a headless frame;
- the conversation it would open over has anything in it, or was resumed;
- the profile has already been shown it.

A person who has **some** of the three configured sees only the missing steps, and the
count in the title is the count of those.

While it is up it is the whole screen: every keystroke belongs to it except `ctrl+c`,
which is still the door (twice, as always), and the mouse does nothing. Nothing you type
is lost to it — it opens only on a launch where nothing has been typed yet.

## Change what I picked during setup — where each answer lives afterwards

Every answer went through a settings row, so every answer has a door:

| What you answered | Where to change it later |
| --- | --- |
| the openrouter key | `/settings`, Providers tab, the **openrouter key** row — masked, `enter` to paste a new one, empty to clear |
| the crew | `/crew` (bare shows the three, `/crew max` sets one), or the **crew** row on the settings panel |
| the daily ceiling | `/settings`, Workspace tab, **daily budget** — or `AFORGE_DAILY_BUDGET` in your shell |
| the model you talk to | `/model` — this was never part of the setup |

A credential changed in the settings row reaches the running conversation at once,
exactly as the setup's does. The crew and the budget are read live too: the next call
aforge makes on its own behalf uses the new crew, and the rail is checked against the
new ceiling.

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

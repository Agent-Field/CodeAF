# ssh/wire — what a frame costs on the wire, and whether it arrives whole

Two questions this branch exists to answer with numbers instead of opinions.

**What does the surface cost per second?** Over a link with a round trip in it,
a TUI is not slow because it renders slowly; it is slow because every frame it
renders becomes bytes that have to cross. Nobody could say from memory what a
second of streaming costs, or what an idle prompt costs.

**Do frames arrive whole?** DEC mode 2026 — synchronized output — is the
terminal's promise not to paint half an update. Bubble Tea v2 implements it.
Whether it actually *engages* under tmux, and under SSH, was unverified.

Nothing here is user-visible and nothing here is configurable. The meter turns
on only when a developer sets `AFORGE_WIRE_LOG`, and a launch without it is the
launch this door has always made. Every number below is a count of bytes the
program handed to the terminal — nothing sampled, modelled, or averaged across
runs unless it says so.

Measured on linux/arm64, 20 cores, go1.26.5, tmux 3.4, OpenSSH 9.6p1, in a
130x40 terminal, with `deepseek-v4-flash-latest` over OpenRouter. The tree is
`ssh/wire-meter` off `dfe1ffa`, whose only change to anything that draws is the
meter itself — which is to say this is the baseline the adaptive work will be
measured against.

---

## The instrument

`internal/wirelog` wraps the writer the Bubble Tea program paints through and
appends one line per wall-clock second: `unix_ms bytes writes total_bytes`,
silent seconds included as zeros so "nothing was drawn" reads differently from
"the process was gone".

Three properties it had to have:

- **Free when off.** `wirelog.FromEnv` returns a nil `*Meter` when the variable
  is unset, `cmd/aforge/wire.go` hands `tui3.Options.Output` a plain nil, and
  Bubble Tea paints into `os.Stdout` with nothing in between. Not an empty
  wrapper — no wrapper. The cost is one `LookupEnv` at boot.
- **Invisible to the surface.** The meter satisfies `term.File` (`Read`,
  `Write`, `Close`, `Fd`) and carries the descriptor through, because Bubble Tea
  type-asserts its output to exactly that before it will use raw mode, ask for
  the window size, or believe the terminal has color. A bare `io.Writer` wrapper
  would have measured a different, degraded surface.
- **Honest.** Run once with both the meter and Bubble Tea's own `TEA_TRACE`
  going, and the two agree exactly:

  | | writes | bytes |
  |---|---|---|
  | `TEA_TRACE` (Bubble Tea's log of every flush) | 54 | 6,040 |
  | `AFORGE_WIRE_LOG` (this meter) | **54** | **6,040** |

  Not close — equal. Which also settles the flush question below.

## Writes per frame: exactly one

The renderer builds a frame in a `bytes.Buffer` and empties it with
`io.Copy(s.w, &buf)` (`cursed_renderer.go:568`); `bytes.Buffer` implements
`WriteTo`, so that is a single `Write` call. The trace agrees line for line: 52
of those 54 writes are renderer flushes, one is the startup mode query, one is
the teardown.

Confirmed a second way in the pty probe: twelve keystrokes typed into the
surface produce twelve frames, twelve writes, and — with synchronized output
engaged — twelve BSU/ESU pairs. **1.00 writes per frame.** There is no
per-frame write amplification to fix.

---

## The baseline: a scripted minute

`measure.sh` drives the same six phases every time — boot, idle at the prompt,
typing a 56-character prompt at ~12 keys/s, the wait after Enter, the streaming
reply, and the quiet after it — and slices the log by wall-clock phase marks.

**Local (the binary in a tmux pane; tmux is the terminal):**

```
    phase   s  live   bytes     B/s  live B/s  peak B/s  writes/s  B/write
     boot   8     2   5,553   694.1    2776.5      2895       3.2    213.6
     idle  12     0       0     0.0       0.0         0       0.0      0.0
   typing   5     5   1,432   286.4     286.4       324      10.4     27.5
  waiting   5     5   5,804  1160.8    1160.8      1706      18.0     64.5
streaming  40    36  26,084   652.1     724.6      1445      14.2     45.8
     rest  20     1      67     3.4      67.0        67       0.1     67.0
      ALL  91    50  39,315   432.0     786.3      2895       8.2     53.0
```

**Over SSH (`ssh -t localhost` from that pane, so the app has a real SSH pty
with `SSH_TTY` set and these bytes are the bytes that cross the link):**

```
    phase   s  live   bytes     B/s  live B/s  peak B/s  writes/s  B/write
     boot   7     2   5,535   790.7    2767.5      4049       3.6    221.4
     idle  12     0       0     0.0       0.0         0       0.0      0.0
   typing   5     5   1,351   270.2     270.2       324       9.8     27.6
  waiting   5     5   5,698  1139.6    1139.6      2024      18.2     62.6
streaming  40    18  17,755   443.9     986.4      1872       8.3     53.5
     rest  20     0       0     0.0       0.0         0       0.0      0.0
      ALL  90    31  30,813   342.4     994.0      4049       5.6     61.6
```

`live` is the number of seconds in that window in which anything at all was
drawn. The two runs are not comparable line by line and are not meant to be:
the model answered at 12 tok/s in the first and 30 tok/s in the second, so the
`B/s` column is mostly a statement about the provider. The `live B/s`,
`B/write` and per-keystroke columns are the ones that hold still.

### The numbers that hold still

| | |
|---|---|
| **idle at the prompt** | **0 B/s, 0 writes.** Twelve seconds, both runs, exactly nothing |
| **idle after an answer** | 0–3.4 B/s: one 67–89 byte footer repaint every 5–10 s, or none |
| **one keystroke** | **27.5 B** local, 27.6 B over SSH — one frame each |
| **boot to first paint** | **5,553 B** local — the same to the byte in both runs — and 5,535–5,838 B over SSH, in 2–3 s and 25–26 writes, then silence |
| **spinner, no tokens yet** | **565 B/s at 11.2 frames/s ≈ 50 B/frame** (a five-second window with an 8 tok/s model; the first second after Enter elsewhere: 631 B / 11 writes local, 601 B / 11 writes over SSH) |
| **streaming** | **642–986 B/s while tokens are arriving**, 46–64 B/frame, peak 1.4–2.0 KB/s |
| **a whole answer** | 24.7 B/token at 51 tok/s · 39.1 B/token at 30 tok/s · 69.9 B/token at 12 tok/s · 81.4 B/token at 8 tok/s |

The last row is the one to keep. **The wire cost of streaming is set by the
frame rate, not the token rate.** Across four runs the model's speed varied 6.4×
(8 → 51 tok/s) and the bytes per second while it talked varied 1.5× (642 → 986);
the bytes per *token* fell by exactly as much as the tokens sped up, because
more tokens land inside the same frame. Anything that changes how often the
surface paints moves this number. Anything that changes how fast the model
talks does not.

### Repeatability

A second pair of runs, `baseline-*-run2.txt`, against a much slower and a much
faster answer than the first pair got:

| | run 1 local | run 1 ssh | run 2 local | run 2 ssh |
|---|---|---|---|---|
| model | 12 tok/s | 30 tok/s | 51 tok/s | 8 tok/s |
| boot | 5,553 B | 5,535 B | **5,553 B** | 5,838 B |
| idle (12 s) | 0 | 0 | 0 | 0 |
| per keystroke | 27.5 B | 27.6 B | 27.6 B | 27.6 B |
| spinner frames/s | 11 | 11 | 11 | 11.2 |
| streaming, live | 724.6 B/s | 986.4 B/s | 926.0 B/s | 642.3 B/s |
| per frame | 45.8 B | 53.5 B | 64.3 B | 53.8 B |

Boot is identical to the byte across the two local runs. The keystroke is the
same number four times. What moves is what the model did.

---

## Synchronized output: three findings

Verified with `probe2026.py`, which runs the binary on a 130x40 pty it owns, so
it can play the terminal: it answers the background-color, cursor-position and
device-attribute queries a real terminal owes a program, and — depending on the
flag — either answers or ignores the mode-2026 DECRQM. Then it types twelve
characters, because an idle aforge draws nothing at all and a verdict taken over
an idle second is a verdict about nothing. Each case below ran in its own fresh
workspace, and each printed the four variables the decision actually turns on
(`probe2026.txt`) rather than the ones the case name claims.

| environment (as Bubble Tea sees it) | asks? | terminal answers? | frames wrapped |
|---|---|---|---|
| bare terminal, no `SSH_TTY`, terminal answers DECRQM | yes | yes | **12/12 — engaged** |
| tmux pane shape (`TERM_PROGRAM=tmux`), terminal answers | yes | yes | **12/12 — engaged** |
| same, terminal silent (this is tmux 3.4) | yes | **no** | 0 |
| **bare SSH** (`SSH_TTY` set, `TERM=xterm-256color`) | **no** | — | 0 |
| **SSH + tmux** (`SSH_TTY` set, `TERM_PROGRAM=tmux`) | **no** | — | 0 |
| SSH with `TERM=xterm-ghostty` forwarded | yes | yes | **12/12 — engaged** |

**1. The program's side is correct and complete.** When a terminal answers,
every frame is bracketed, one pair per frame, no exceptions. Nothing in tui3 or
in the wiring needs fixing for mode 2026 to work.

**2. Under tmux 3.4 the query is sent and never answered.** tmux is the
terminal an aforge in a pane is talking to, and tmux 3.4 does not implement
DECRQM at all — not for 2026 and not for modes it certainly supports. Asked
directly from a pane, it answers a device-attributes query and says nothing to
either DECRQM:

```
$ tmux new-session -d -s decrqm -x 130 -y 40
$ tmux send-keys -t decrqm "stty raw -echo; printf '\033[c\033[?2004\$p\033[?2026\$p'; \
    timeout 2 cat > /tmp/reply; stty sane" Enter
$ cat -v /tmp/reply
^[[?1;2;4c            # device attributes — answered
                      # ?2004$p (bracketed paste, which tmux has) — silence
                      # ?2026$p (synchronized output)            — silence
```

So: **no synchronized output under tmux, on this machine, today.** Frames are
still one write, so tearing requires the write itself to be split — but the
guarantee is not there, and it is not there for the reason a reader would
guess.

**3. Under SSH, Bubble Tea never asks.** `shouldQuerySynchronizedOutput`
(`bubbletea/v2@v2.0.8/tea.go:960-986`) returns false whenever `SSH_TTY` is set,
unless `WT_SESSION` is set or `TERM` contains ghostty, wezterm, alacritty, kitty
or rio. The reasoning is in their comment — SSH sessions "may be unreliable" —
and the effect is that the environment with the most to gain from atomic frames
is the one that opts out of them by default. `SSH_TTY`, not `SSH_CONNECTION`,
is the trigger; a pane opened by `tmux` does not inherit `SSH_TTY` (it is
absent from tmux's default `update-environment`), so a remote tmux hides the
SSH-ness from this check and then fails the query on its own account anyway.

### What would fix it, and why this lane did not

Bubble Tea offers no option and honors no environment variable for this; the
only lever is `tea.WithEnvironment`, which is the environment the program
believes it is in. Passing an environment with `SSH_TTY` removed would make it
ask, and finding #1 says the rest of the path then works.

That lever is not pulled here. Sending the query costs nothing when the
terminal ignores it — measured, in the `--silent` probe: the program asks, gets
nothing, carries on, and draws its 352 bytes of keystroke frames either way.
But the failure mode Bubble Tea is guarding against is a terminal that echoes
an unrecognized query as text, and that failure lands on the screen of a person
we cannot test from here. The rule for this lane is nothing user-visible; a
change whose worst case is visible garbage over someone's SSH session is
exactly that, and it belongs with the lane that owns adaptive rendering, along
with this evidence.

One number for whoever picks it up. With synchronized output engaged, the same
twelve one-keystroke frames cost **400 B instead of 352 B — +4.0 B per frame**,
not the +16 the two escape sequences weigh, because a renderer that brackets an
update with BSU/ESU stops bracketing it with cursor hide/show
(`cursed_renderer.go:528-558`). At the streaming frame rate measured above that
is under 50 B/s. Atomic frames are close to free.

---

## What this does not measure

The meter counts what the *program* writes. When aforge runs in a remote tmux
and a person attaches over SSH, those bytes go to tmux, and what crosses the
link is tmux's own re-render of its screen — related, but not this number. The
SSH table above avoids that by giving the app a real SSH pty (`ssh -t` into the
binary, tmux only on the near side), so there the two are the same bytes.

Also not counted: SSH compression (off by default), TCP and cipher framing, and
whatever the terminal on the far end does after it receives them.

---

## Reproduce

```
export OPENROUTER_API_KEY=...                 # any model that answers

perf-report/ssh/measure.sh local /tmp/wire    # scripted minute, tmux pane
perf-report/ssh/measure.sh ssh   /tmp/wire    # same, through ssh -t localhost

python3 perf-report/ssh/summarize.py /tmp/wire/wire-local.log \
        /tmp/wire/phases-local.txt --seconds  # per-second detail

python3 perf-report/ssh/probe2026.py bin/aforge chat            # answers DECRQM
python3 perf-report/ssh/probe2026.py --silent bin/aforge chat   # plays tmux 3.4
```

The environment matrix is the probe with the variables set by hand, e.g.

```
env -u TERM_PROGRAM SSH_TTY=/dev/pts/99 TERM=xterm-256color \
    python3 perf-report/ssh/probe2026.py bin/aforge chat
```

Any run of the binary can be metered on its own:

```
AFORGE_WIRE_LOG=/tmp/wire.log bin/aforge chat
```

## Files

| file | what it is |
|---|---|
| `measure.sh` | the scripted minute, local or over SSH, ending in a summary |
| `summarize.py` | wire log + phase marks → the table above; `--seconds` for every row |
| `probe2026.py` | a pty that plays a terminal, to see whether frames get bracketed |
| `baseline-local.txt`, `baseline-ssh.txt` | the summaries above, with per-second detail |
| `baseline-local-run2.txt`, `baseline-ssh-run2.txt` | the repeat pair |
| `wire-*.log`, `phases-*.txt` | the raw logs and phase marks all four runs came from |
| `probe2026.txt` | the environment matrix, as the probe printed it |

# 1747 — mention live-state capture (evidence for #197)

Captured from the compiled TUI in an isolated environment on the capture host. Artifacts were moved into the tree as files only. Nothing was posted or pushed
to GitHub.

## Deliverables

| Path | What |
| --- | --- |
| `docs/design/1747-mention-live-state.gif` | 8 frames, 2000×294, 700–900 ms each, 150,649 bytes (GIF 89a, verified `file`) |
| `docs/design/1747-mention-live-state.png` | still of the working-mark frame, full 200×46 pane → 2000×966, 53,055 bytes |
| `docs/design/capture-frames/*.ans` | every captured tmux frame (73 files), raw ANSI bytes, checkpointed after every capture step |

## Binary and head

- Binary: `/tmp/codeaf-1747`, built 2026-10-03 18:20 (before this task began),
  `shasum -a 256 = 9b52b5b21a205950a6d4dd6d984131b0d5552b0cfa00883363da5fac1fd05834`
  (81,971,700 bytes).
- Head SHA claimed by the inherited state: **f731fb813e49bc55995828eaf856bed8fd552e5b**,
  built from `~/work/mention-final`.
- **Caveat, measured:** that tree has **no `.git` directory** on the capture host
  (`git rev-parse HEAD` → exit 128, "not a git repository"), and the binary
  itself reports `codeaf dev (no revision stamped — no .git directory for the
  toolchain to read…) · go1.26.5 linux/arm64`. So f731fb8 could not be
  re-verified on the machine; the shasum above is the identity evidence that
  does hold. Everything captured is this one binary.

## The feature, in bytes (conversation A row 11)

Message row in A ("triage retry failures"): `› hey @authfix can you take the
retry case while I look at the queue?`. The `@authfix` token has three byte
forms across the run:

1. **No mark** (idle / cleared) — one link run, whole token underlined:
   `hey ` + `[4m[38;5;146m@authfix` + `[0m[38;5;110m can you take…`
2. **Working mark** — first cell is the signal mark in the signal's ink, the
   rest keeps link ink (`mentionSignalSplit`, mention.go:765):
   `hey ` + `[38;5;146m@` + `[4mauthfix` + `[0m[38;5;110m can you take…`
   (underline drops off the `@` cell — pixel probe: `@`-cell underline 0 px,
   slug 70 px; no-mark form: 10 px / 70 px).
3. A needs-you mark (amber, `tabSignalInk` warn) would be the third signal
   (`tabNeedsPerson`); no consent/question occurred in this run, so it is not
   in the frames.

This matches the source law: the mark is the token's first cell in
`tabSignalInk` ink (accent = work in flight, tabsignal.go:329), the rest keeps
`teamLinkInk`, and no mark at all draws nothing (tabsignal.go emptiness law).

## What the frames show (8-frame story, in order)

1. `seq-idle-1` — A front, idle. `@authfix` link form, **no mark**.
2. `seq-idle-2` — same, 62 s later (clock tick makes it a distinct frame).
3. `seq-bwork-1` — conversation **B ("authfix") front, a real turn in flight**:
   status line says `working`, the holder accepted one connection and never
   answered. This is the working window's cause.
4. `seq-mark-1` — back in A: the token now wears the **working mark** (accent
   `@` cell, underline under `authfix` only). It appears on the frame of the
   hold and holds while B works.
5. `seq-mark-2` — still working (+62 s).
6. `seq-mark-3` — still working (+124 s; turn ran ≈ 127 s total).
7. `seq-clear-1` — 1.5 s after the turn ended: **mark cleared**, whole-token
   link form back.
8. `seq-clear-2` — settled (+62 s).

Still PNG = `seq-mark-2` at full 200×46 pane size.

Verified per frame after rendering (pixel geometry probe over the rendered
GIF): frames 1–2 and 7–8 `mark_at_cell=10 slug=70` (no mark), frames 4–6
`mark_at_cell=0 slug=70` (working mark), frame 3 is B's screen
(`mark_at_cell=0 slug=0`, no mention row there). All 8 frames pairwise
pixel-distinct (Pillow 12.3.0 merges identical consecutive GIF frames, so the
first assembly collapsed 9 frames to 5; the final sequence samples across
minute boundaries and survives whole).

## The honest working window

The brief's fake key failed instantly, as predicted:
`· error: your key was not accepted for this model — the shell's
OPENROUTER_API_KEY` (<1 s). So the turn was pointed at an endpoint that never
answers — a real turn really in flight, no tokens spent.

- The never-answering socket: `python3 /tmp/cap1747-holder.py` — bind
  `127.0.0.1:18444`, `accept()` then `sleep(600)`. Probe: `NO_ANSWER_AFTER
  TimeoutError 3.0` (exit 0).
- **Deviation from the brief:** port **18443 is occupied on the capture host** by an
  unrelated long-running fixture (`srv_tls.py 18443 FixtureTLS certA keyA.pem
  certA.pem`, pid 2339841, up since Sep 18) which resets plain HTTP. It is not
  ours to kill, so the holder uses **18444**.
- **Deviation from the brief:** the brief said to write the scratch home's
  config with the base URL. The config struct's own live seam is the env
  variable `CODEAF_BASE_URL` (config.go:511 reads it first), so the relaunch
  used `env CODEAF_BASE_URL=http://127.0.0.1:18444/v1`; the scratch
  `config.json` still carries only `setup_seen_at`.
- **Discovery worth knowing:** the provider call is made by the workspace's
  engine daemon (`/tmp/codeaf-1747 engine --daemon --workspace
  /tmp/pr1747-demo`), not the TUI — a `go` sent while that daemon predates the
  env still reached OpenRouter (instant key error). Killing the scratch
  daemon let it respawn under the new env; the next turn connected to the stub
  (`SYN-SENT …18444`, holder `accepted` 1) and held `working` until the holder
  was killed.

## Exact commands and exit codes

Commands ran on the capture host over its remote shell; file copies moved artifacts into the tree only.

```
ls /tmp/codeaf-1747; tmux ls; ls …/projects/-tmp-pr1747-demo/     EXIT 0   (binary, cap1747, seed a/b present)
cd ~/work/mention-final && git rev-parse HEAD                      EXIT 128 (no .git — see caveat)
tmux capture-pane -t cap1747 -e -p > f00{1,2,3}.ans               EXIT 0   (baseline, checkpointed by scp)
tmux send-keys -t cap1747 "?"  → key map                           EXIT 0   (map names alt+k / Tab / ctrl+t)
tmux send-keys -t cap1747 M-k; Right; Enter  → B opens as tab      EXIT 0   (Right expands "→ show closed")
tmux send-keys "go" + Enter; 14×0.5s status poll                   EXIT 0   (fake key: instant "key not accepted")
nohup python3 /tmp/cap1747-holder.py (18444) &                     EXIT 0   (probe: accept, then TimeoutError 3s)
tmux kill-session cap1747; tmux new-session …CODEAF_BASE_URL…      EXIT 0   (relaunch; TUI env verified set)
kill <workspace engine daemon pid 146005>                          EXIT 0   (respawned as 1459635 with env)
tmux send-keys "go" + Enter; 12×0.5s status poll                   EXIT 0   (status=working 11/12; SYN-SENT 18444)
tmux send-keys Tab; 12×0.7s capture mark-*.ans                     EXIT 0   (mark present, checked in bytes)
kill <holder 938330>                                               EXIT 0   (turn ends)
12×0.5s capture clear-*.ans; settle-*.ans                          EXIT 0   (mark cleared, checked in bytes)
python3 /tmp/render1747.py … (assembly)                            EXIT 0   (8 frames, 150,649 bytes)
python3 frame/state/distinctness probes (Pillow)                   EXIT 0   (output above)
scp spark:…/1747-mention-live-state.{gif,png} docs/design/         EXIT 0
scp spark:…/seq-*.ans docs/design/capture-frames/                  EXIT 0
/tmp/codeaf-1747 --version; shasum -a 256 /tmp/codeaf-1747         EXIT 0
```

One command failed along the way and ran nothing: the first attempt to write
the capture script used a mis-quoted heredoc delimiter (remote shell swallowed
the whole command as heredoc body; no capture started). Rewritten with
`<<\EOF`, then run synchronously; the run log below is its output.

Run log of the final sequence (epoch seconds):

```
RUN_START 1791068418.762   idle-1  1791068419.769
idle-2     1791068481.774   sent-go 1791068483.384   bwork-1 1791068484.895 accepts=1
mark-1     1791068486.103   mark-2  1791068548.110   mark-3  1791068610.116
holder-killed 2639074      1791068610.118
clear-1    1791068611.625   clear-2 1791068673.631   RUN_END 1791068673.632 (SCRIPT_EXIT=0)
```

## Renderer

`/tmp/render1743.py` (proven) cloned to `/tmp/render1747.py` with two changes:
SGR `4`/`24` (underline) support — the mark IS the underline geometry, so the
proven renderer would have drawn working and cleared frames identically — and
env-driven `CAPCOLS`/`CAPROWS` (captures are 200×46; the proven constants are
140×40). It still renders captured tmux bytes only. A pixel probe confirmed the
underline geometry against the raw SGR bytes before assembly.

## Standing orders

- **Tags/milestones:** no GitHub issue or PR was created for this work (GitHub
  contact is forbidden here), so there is nothing to tag or milestone.
- **Production bar:** every artifact verified in place — `file` on the GIF,
  byte-level state audit per frame, pixel-geometry probe, pairwise-distinct
  check, frame count and durations read back from the saved GIF.
- **Attribution:** captured and rendered with codeaf (`/tmp/codeaf-1747`, head
  f731fb8 per the built tree); this report is the change's record.

Captured 2026-10-03 18:30–19:09 EDT on the capture host · evidence for #197 ·
rendered by codeaf.

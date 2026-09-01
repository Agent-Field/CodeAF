# Compacting over and over

## Why does it keep compacting — it compacts after every step

It should not, and if you see it, it is a defect: a pass is meant to buy several steps of
room, not one.

The rule is that compaction **fires** at one line and **folds down to a lower one**. It
fires when the conversation's estimated size crosses the threshold — `window − max(15% of
window, 16384)`, so `108,800` tokens on the default 128,000-token window. A pass then folds
the oldest assistant work until the estimate is a whole **headroom** under that line, where
the headroom is half of the reserve the threshold subtracts: on the default window a pass
stops near **99,200** tokens, about ten thousand under the trigger. Those ten thousand
tokens are several steps of ordinary growth, and until the conversation has used them up
the automatic check after each step finds nothing to do.

An earlier version stopped folding the moment it dipped under the threshold. One task
compacted fifteen times in six minutes — after nearly every step, four to six messages a
pass, with the estimate never once going down — because a step's growth put it straight
back over. That is what the headroom ends.

What does not change with the headroom: every message you typed survives every pass, the
system prompt and the verbatim tail (20,000 tokens, at most a quarter of the window) are
never folded, and the full record stays readable in the session journal — the fold marker
names that file, and `grep` or `read` opens it (see *Where did the folded messages go*).

## Why is it compacting at a hundred thousand tokens when my model holds a million

It should not, and it no longer does. **The line it fires at follows the model's own
window** — `window − max(15% of window, 16384)` — so a model claiming 1,310,720 tokens
folds at 1,114,112 and one claiming 128,000 folds at 108,800. That is the line whenever
you have not set one yourself; the next section is how to set one.

Two things used to make a big model fold like a small one, and both are fixed:

- aforge refused to believe any claim above 256,000 tokens, for every model alike. That
  ceiling is gone; what can lower a claim now is an endpoint actually **refusing** a request
  for being too long, which aforge writes down and never trusts that model past again.
- work that left the conversation — a task's worker, an adaptive run's worker, a fork, the
  reader that checks a task — was handed nothing at all when its model differed from yours,
  and so folded against the conservative 128,000-token default whatever its own model held.
  Each of those now asks the catalog for its own model.

A measured run in August 2026 compacted nineteen times in two and a half hours for exactly
those two reasons, at about a twentieth of the room its model advertised.

If you are still seeing it on a model you know is large, the two things to check are both
on `/status`: the `context` line reports the model's own figure, and a much smaller number
there means the model catalog on this machine has not answered for it; the `compacts at`
line under it says where the fold line actually is and whether you pinned it.

## What --context-fill does — the context fill setting, and compacting sooner

Context fill is how full a window may get before it folds, as a percent, and when you set
it that is the line. Three ways say the same thing and all set the same context fill:

- the `context fill` row on the settings sheet's Models tab (`/settings`), which is
  written down and holds for every later session;
- `AFORGE_CONTEXT_FILL_PCT` exported in your shell, which holds for every aforge started
  from it;
- `--context-fill N` on a headless run, which sets that variable for that run.

The fill is clamped to **10–90**. Lower it to compact sooner, raise it to compact later. A
session where you have set a context fill says so on `/status`: the `compacts at` line
reads `60% of 1M (pinned)` where an untouched session reads `85% of 1.3M (derived)`.

**Not setting the context fill is not the same as setting it to 60.** The shipped fill is
60 and it sizes a task's own workers; the conversation you type in ignores it until you
set one, and follows the window instead. Honouring an untouched 60 would fold a
1,310,720-token model at 786,432 rather than 1,114,112 — every session paying for a
number nobody chose.

## Two clamps on a context fill you set

Both only ever bind at the edges, and between them a fill you set is the line exactly as
you typed it.

- **The answer room is kept.** The line never rises so far that the reply aforge is
  waiting for has nowhere to go — that room is the `answer room` setting, 65,536 tokens by
  default (`--completion-reserve`). On a window under about 437,000 tokens the derived
  line is the higher of the two and becomes the ceiling instead, so asking for 90 on a
  128,000-token model gives you 108,800 rather than something lower than an untouched
  session would have got.
- **The line never falls under the tail.** The most recent 20,000 tokens are never folded,
  so a threshold at or below them would fire on every step and find nothing to take. The
  floor is twice that tail. On a large window it never binds; on a small one it is what
  stops a fill of 10 from being a fold that cannot work.

## Why compaction costs more than it looks like it should — the prompt cache

Every pass rewrites the front of the transcript, and the provider's prompt cache is keyed
on that prefix. So a pass throws the cache away, and the next request pays for the whole
prompt again. One pass every few dozen steps is a fair price for the room it makes; one
pass per step meant paying the full prompt on every request, which is exactly what the
headroom above is for. The `cache` row of `/status` shows how much of the last request was
served from cache; a figure that stays near zero across steps while `compacted · …` lines
keep appearing is the sign of the defect this page describes.

## A pass that cannot reach its target

The fold walks the oldest assistant work first and stops at the target, but it never folds
your own messages, the system prompt, the verbatim tail, or a tool call whose result has
been stubbed. A conversation that is mostly your own words and recent work can run out of
foldable material above the target. The pass still succeeds with what it took, and the
`compacted · …` line reports the real counts; the next step's check may then fire again,
honestly, because there was nothing more to take. That is the one case where two passes
in quick succession are not a defect.

## Where did the folded messages go — how do I get the compacted text back, what happened to the earlier messages

They are still on disk. A fold replaces the oldest assistant work in the model's window with
one line such as

```
[folded 31 messages · grep or read /home/x/.aforge/v3/sessions/abc.jsonl, lines 12..40]
```

The path is this conversation's own journal — a real file, the one the session is writing —
and the marker names the two tools that open it, so aforge can go back for the words without
being told to. The path is its own word and the lines are said after it, because a
`path:12..40` token is not something either tool takes. The original lines stay above the
compaction marker in that file, and 12 to 40 is the span the folded run sits on.

A session with no journal file names no path: it says `full record in the store` where a
store is keeping one, and `full record in the session journal` where the record is only
ever the journal. Neither invents a file to open.

Scrolling up above the fold on the screen also still shows the words; what shrank is the
model's copy, not yours. The fold is not unrecoverable.

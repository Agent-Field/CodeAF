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
never folded, and the full record stays readable in the store or the session journal.

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

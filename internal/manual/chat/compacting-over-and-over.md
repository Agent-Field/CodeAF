# Compacting over and over

## Why does it keep compacting — it compacts after every step, why it keeps summarizing the conversation

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

A pass turns old tool results into pointers first and folds afterwards, and **the fold is
measured against the lower line too**. Stubbing on its own often lands the estimate just
under the trigger and nowhere near the target; a pass that stopped there had bought no room
at all, and the next step put it straight back over. It now keeps folding down to the target
whatever the stubbing already saved.

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

- codeaf refused to believe any claim above 256,000 tokens, for every model alike. That
  ceiling is gone; request admission now reads endpoint-specific windows and explicit limits reported by
  refusals. A rejected prompt size is not saved as a model-wide window.
- work that left the conversation — a task's worker, an adaptive run's worker, the
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
- `CODEAF_CONTEXT_FILL_PCT` exported in your shell, which holds for every codeaf started
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

- **The answer room is kept.** The line never rises so far that the reply codeaf is
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

## What happens to tool results while one long answer is still working

A running answer keeps its assistant notes, tool calls, their exact arguments and every
mutating result in the model's context. Routine conversation compaction does not fold that
current-turn work. Manual compaction and necessary overflow recovery may archive older
completed batches while retaining the newest batch and a smaller recent working tail. This is deliberate: it is the working record of what the model tried
and what it changed, and removing it can make the model inspect the same files or repeat
an edit.

Read results have a separate **64,000-token working-set line** (or half the model's trusted
window when smaller). That line counts the actual tool observations, not the system prompt,
tool definitions or assistant prose. Crossing it is not enough by itself. A read result can
become a pointer only after the model successfully changes a file from the context that
contained it, and only when one pass can reclaim a full stretch of headroom. Research that
has not produced work stays verbatim, even when keeping it costs more tokens. The full bytes
of a result that does become a pointer remain in the session journal and the path named by
the pointer.

One refinement for repetition: when the turn has read the **same file several times**, only
the newest slice stays whole. Each older slice becomes a pointer naming the file with the
`offset` and `limit` that read it, and no copy is filed — the file itself is where those
bytes came from, and `read` brings the slice back.

## A pass that cannot reach its target — when folding is not enough

The fold walks the oldest assistant work first and stops at the target, but it never folds
your own messages, the system prompt, the verbatim tail, or a tool call whose result has
been stubbed. A conversation that is mostly your own words and recent work can run out of
foldable material above the target.

When that leaves the conversation above the line that fired the pass, the pass ends with a
**summary**: the conversation's own model rewrites the oldest part of the conversation as
one note, and the `compacted · …` line says `summarized N messages`. See *When compaction
writes a summary*. A summary is skipped when the tool definitions alone already exceed the
line — no summary could get under it — and then the next step may fire again, honestly,
because there was nothing more to take.

## What happened to the earlier messages — where did the folded messages go — how do I get the compacted text back, why it loses the earlier part of our chat

They are still on disk. A fold replaces the oldest assistant work in the model's window with
one line such as

```
[folded 31 messages · grep or read /home/x/.codeaf/v3/sessions/abc.jsonl, lines 12..40]
```

The path is this conversation's own journal — a real file, the one the session is writing —
and the marker names the two tools that open it, so codeaf can go back for the words without
being told to. The path is its own word and the lines are said after it, because a
`path:12..40` token is not something either tool takes. The original lines stay above the
compaction marker in that file, and 12 to 40 is the span the folded run sits on.

A session with no journal file names no path: it says `full record in the store` where a
store is keeping one, and `full record in the session journal` where the record is only
ever the journal. Neither invents a file to open.

Scrolling up above the fold on the screen also still shows the words; what shrank is the
model's copy, not yours. The fold is not unrecoverable.

## Why /compact says nothing to compact — manual compaction before the automatic trigger

`/compact` now has its own reduction policy. It can fold older completed assistant work
before the automatic trigger, including completed batches inside one long turn. It keeps
your messages, the system prompt, the newest assistant/tool batch, and 4,096 recent tokens
(at most an eighth of the trusted window). Then, in the same pass, it summarizes whatever
older conversation is left (see *When compaction writes a summary*), so one `/compact` goes
as far as it can; a second one right after has nothing left to do.

The old command reused the automatic target. A conversation with 60,000 tokens on a 128k
model could have older history and still receive `session: nothing to compact`, because it
was below that target. The manual command no longer has that threshold gate.

A no-op says `nothing to compact — ` and why:

- `only ~400 tokens since the last summary — too little to summarize` (or `before your
  latest message`): a summary needs about 1,000 tokens of conversation it has not read;
- `nothing new since the last summary`, or `there is nothing before your last 3 messages
  to summarize` (or `your latest message`): the messages kept word for word are all that
  is left;
- `the model could not write a summary: …`, `the model declined to write a summary`,
  `the model's summary came back empty or unreadable`, `the summary was interrupted`, or
  `the conversation changed while the summary was being written` — the conversation is
  left exactly as it was.

When the free steps did shorten something but the summary did not land, the pass still
reports what it folded. Its line includes `summary skipped: ` and why the summary did not
land — the model could not write it, declined, sent back nothing usable, was interrupted,
or the conversation changed meanwhile;
an automatic line may have size and journal details after that clause. `/compact` says
`⚭ compacted · about X to Y tokens · summary skipped: <why>` when its size fell, or
`⚭ compacted · summary skipped: <why>` when there was no size drop.

None of these means the next request fits: admission also counts schemas, replayed
reasoning and reserved output.

## Why the provider says maximum context length when the status shows 20 percent — a request refused as too long

The status shows the model catalog's window. The serving endpoint may have less room, and
its window must hold input **plus output**, including thinking. codeaf budgets the assembled
request against known endpoint limits before sending and remembers explicit limits from
errors by base URL, model and endpoint. Old rejected-prompt-size guesses are ignored.

Recovery shortens by what is missing plus a little room — a thirty-second of the window —
rather than a quarter of the conversation, and it will summarize even a small older part
when that is what the request is short of. Overflow recovery can run twice per failed
generation, only while the request changes.
A DeepInfra refusal saying `Requested input length … exceeds maximum input length …`
counts as an overflow; its stated maximum is used as that endpoint's limit.
A successful response resets the allowance. A second overflow later in a long tool turn
can therefore recover instead of ending the turn just because it compacted earlier.
If protected material still cannot fit, codeaf explains that locally; it does not
knowingly send the same oversized request again.

## When compaction writes a summary — does codeaf summarize my conversation, what the summary keeps

Yes, as a last resort. Folding and pointers are tried first because they are free and
nothing is paraphrased. A summary is written only when they cannot bring the conversation
under the line the pass needs: the automatic threshold, the size a refused request has to
shrink to. `/compact` is the exception: it always goes on to summarize whatever older
conversation is left, because you asked for it as short as it can be.

The summary is written by **the model you are talking to**, with no tools, and it is billed
like any other call, counting toward the session's spending. It replaces the
oldest part of the conversation — your older messages and the assistant's work alike —
with one note that starts `[context compacted]`. It never touches:

- the system prompt;
- your **up to three most recent messages** and everything after them. If a cut can reach
  the line, codeaf keeps as many of those messages as that cut allows. If no cut can
  reach it, an automatic pass or `/compact` still keeps all of the most recent three
  that exist: summarizing more would not reach the line either. With only two messages
  since the last summary, `/compact` leaves both word for word when neither can reach
  the line; if there is too little older conversation, it writes no summary and says why.
  Recovery from a refused request tries two, then
  your latest message **with the reply just before it** (what "translate it" or
  "keep going" is about), then your latest alone — the message being answered always stays;
- the turn that is running.

Messages codeaf writes into the conversation itself — the note after a reply was cut off
at the output limit, a `[carry on]` — are not counted as yours.

A later summary folds the earlier one in, so there is only ever one note. The original
words stay in the session journal, which the note names, and on your screen when you
scroll up. A summary that fails, is not prose, or is not smaller than what it replaces is
thrown away and the conversation is left as the free steps left it. While it is being
written the status row shows `tidying` with a clock.

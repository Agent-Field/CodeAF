# Lanes — which machine answers, and the question a slow lane asks

A model name is an address, not a machine. Behind one name there are usually a
dozen **lanes** — endpoints run by different companies — and they are not alike: on a
measured day in August, seventeen endpoints serving one model differed by **7×**
on how long they took to say their first word and by **12×** on how fast they
wrote, at roughly the same price. Which one answers you is often a bigger
difference than which model you picked.

So aforge keeps an opinion about them, per model, learned from every answer it
has ever timed — and it tells you which one served you, in the status line:

```
via cloudflare · 0.6s · 61 t/s
```

## A model name that ends in latest, and the tilde in front of it — what the pointer names, and what via says instead

The model aforge ships with is spelled `~deepseek/deepseek-v4-flash-latest`, and that is
the name on the model picker and on the status line. Two things about it are worth
knowing, because neither is guessable.

**The leading `~` is not a typo and not a home directory.** It is the router's own marker
for a *floating* name: one that does not point at a fixed build of a model but at
whichever build is current. Everywhere else on a terminal a leading `~` means your home
folder, and aforge still reads it that way when it is followed by a slash — `~/` is a
path, `~deepseek/…` is a model.

**A pointer is not a machine, so what is learned is filed under what it points at.**
`…-latest` names whichever dated build the model's makers published most recently — today
`deepseek/deepseek-v4-flash-0731` — and it is that dated build the router publishes
machines for. So the lanes aforge asks about, the speeds it writes down, and the row it
keeps in `~/.aforge/v3/lanes.json` are all filed under the dated name, never under the
pointer.

That name is not something the screen says back to you, which is why it surprises people
who go looking. The picker and the status line show the name **you** chose, and `via
cloudflare` names the machine that answered rather than the model it answered for.

**When the pointer moves, nothing is carried across.** The newer build is a different
model with its own machines and its own speeds, so it starts its own record from the sheet
the router publishes for it, and the older build's record stays where it is instead of
being spent on a model nobody has measured. That is the same rule as everywhere else here:
a measured thing is about the thing that was measured.

## Auto, and which lanes it is choosing between — how it picks a provider on the very first message, and whether aforge do routes too

Left alone, aforge is on **auto**. Before each request it drops every endpoint
that cannot do the job at all — too small an answer for what you asked for, not
enough room for the conversation, weights served at a coarser precision than the
model is meant to run at, a share of usable answers below what this kind of work
needs — and then ranks what is left by the only thing you actually feel: how long
you will be sitting there, plus what it costs, with the money converted into
seconds by how much your waiting is worth.

Nothing is waiting on this when nobody is waiting on you. A background errand is
ranked on price, because a second saved for a machine is a second nobody spends.

**A run started from a terminal is routed on the same terms.** `aforge do`, `aforge run`
and `aforge plan run` open no conversation and draw no status line, and they used to
take whatever machine the router happened to hand them. They fetch the same sheet now and
rank it with the same arithmetic — so a headless machine, one that only ever runs work
from a terminal, is choosing between endpoints rather than between none, and every run
leaves a record the next one starts from. Nobody is sitting in front of an errand, so it is
the price ranking above that applies to it. `auto` is the answer only while your home's
lane row says auto; a pin in that home replaces this ranking at every terminal door.

**A model it has never sent to is not a model it knows nothing about.** The
public sheet names every endpoint serving it, and what aforge has learned about
a *company* — that this one is quick, that one queues — carries across every
model that company serves. So the first request to a brand-new model is still
routed, still has a clock on it, and asks for a fresh sheet in the background
while it goes. You never wait for that fetch.

## When the provider list says a machine cannot take tool calls, or is half down — why aforge tries it anyway

The public sheet carries three claims about each machine that aforge used to
treat as final: whether it honours a tool call, what share of the last five
minutes it was answering, and whether the router's own operators have marked it
down. A machine failing any of them was removed from the candidate set outright.

**They are opinions now, not doors.** A machine the sheet doubts is **ranked
last** — behind every machine nothing is doubted about, never asked first while
something better can serve you — and it is still there when the machines in
front of it are busy or refuse. About **one request in ten** is sent to it first
on purpose, because a machine nobody ever asks can never show the sheet was
wrong about it.

This changed because the sheet was measurably wrong. On 2026-09-10 a task was
answered three times in a row, six seconds each, by a machine the sheet flags as
unable to take tool calls — while the same task sat on a busy machine collecting
nine refusals, because the one that was working had been removed from every
request carrying tools.

**What a machine's own answers say beats what the sheet says about it.** Once
aforge has seen a machine return usable answers to this kind of work, the sheet's
doubt stops applying to it and it is ranked on its numbers like anything else.
That belief fades over about an hour if the machine stops answering well, so
nothing learned here is learned forever.

One claim is still a closed door, and it is not the sheet's: when the **router
itself** answers that a machine cannot serve this model, that machine is not a
candidate at any rank. That is an answer to a request aforge really made, not a
page published some minutes ago.

## Learning which provider finishes my work faster — why the first message does not go to the most expensive provider

Auto considers both the first words and the generation that must finish before
the next step can run. Readable prose can arrive while you read. Reasoning and
tool arguments keep the next operation waiting, so their completion speed matters.

Completed calls teach aforge how much of each kind to expect for that model,
whether tools are available, and its reasoning setting. Recent evidence counts
more; stale evidence gives way to the conversation's previous answers. A new
conversation with no evidence still has no measurement of how long its first
answer will run. To compare machines, aforge reads that absence as a typical
readable answer rather than no answer at all, so a machine that charges ten
times as much to write does not win the first turn on its first word alone. It
does not keep that comparison as a measurement. The request's output limit
bounds a learned estimate. Capped, interrupted and unusable replies do not teach
it that a complete answer is short.

The measurements share the existing local routing history across sessions, with
a bounded number of remembered request types. Endpoint names, prices and speeds
come from the provider information and actual calls; there is no preferred-provider
list to maintain. A successful endpoint stays preferred for that conversation's
cache, while the slow-response monitor watches that endpoint and can still rescue
a stalled request under the existing spending limits.

Text arriving in a batch earns progress for its approximate token count, so a
provider that sends whole phrases is not judged as though each phrase were one
token. Tool-only replies also teach the first-token and generation clocks.

When a watched request fails and its recovery allowance can fund another
endpoint, that endpoint is tried before repeating the failed request. Rate
limits still respect their retry delay. Without an affordable alternative, the
existing bounded retries and wait reporting remain.

## Pinning one lane yourself — how to change the provider for a model, left and right arrows in the model picker, the @ after the model name, and whether aforge do uses the lane I pinned

You can name the lane yourself. Open `/model` and press `→` (or `tab`) on the model:
its lanes — the endpoints serving it — open under it, the cursor **moves into them**,
onto the lane you pinned or onto `auto` when you have not, and the list scrolls so the
model and every lane are in view. `enter` pins the lane under the cursor — every request
for that model goes there until you say otherwise — and `←` (or `tab`) walks back out.
The hint slot says which: `→ lanes · enter switch · esc` on a model,
`enter choose · ← back · esc` inside. On the default service, the `openrouter` row means
"no opinion from me — let the router balance it".

**A model nobody has measured still opens**, onto `auto` and `openrouter`, with one
line where the machines would be:
`no machine has been measured for this model yet — machines show up after its first answer`.
Opening it asks for that model's list of machines in the background. With the routing
row at `off` nothing opens at all.

**The lane you are pinned to is written on the model's name** — `deepseek-v4-flash@cloudflare`
on the line above the box and on a phone's status deck — with the same `@` you would
type in `/model @cloudflare`. `/status` says it on a `lane` line under `model`. On `auto`
and `openrouter` there is no `@`, and none once a pin has been retired. Pressing the
name opens the picker with the cursor on the pinned lane.

A pin is an instruction, so aforge keeps it. It does not quietly send your work
somewhere else because it thinks it knows better.

**The pin belongs to your home, not to one conversation.** Every door reads the same
profile row when it opens: `aforge do`, `aforge exec`, `aforge plan`, `aforge run` and
the background pass all honour the lane you picked, just as the chat does. A run from a
terminal and a task running overnight therefore ask for your pinned provider too.

## When the machine I pinned cannot serve the model — the one thing that ends a pin without me

**There is exactly one thing that ends a pin without you: the machine you named
saying it will not serve that model at all.** That is not a wait and not a bad
afternoon — the router answers `No allowed providers are available for the
selected model. … but your request's provider.only preference permits only:
coreweave`, which is the wire saying this machine and this model do not go
together. Asking again buys the same 404, so aforge stops asking, and says so
once, in the conversation, at the moment it happens:

```
coreweave cannot serve this model; routing on auto for this model until you pin again
```

What that means, exactly:

- **for that model**, every later request in this run goes out with no machine
  demanded at all — routed the way `auto` routes;
- **the request that collected the refusal is widened and sent again**, once, so
  your answer still arrives. If that is refused too the turn ends, and says so;
- **your settings row is not touched.** It still reads `pinned: coreweave`;
- **every other model still goes to that machine.** The refusal was about one
  pairing;
- **pinning again puts it straight back**, on the very next request — and that
  includes choosing the machine you already had, which is a row that did not
  change and an instruction that did;
- it lasts until you pin again or you close the window, and a task the
  conversation starts inherits it rather than paying for the refusal again.

The sentence is said **once** per machine and model, for the whole run.

## When a pinned lane goes quiet — the `switch to auto?` question, and how to say no to it

It still has to do something about a wait, and what it does is **ask you**:

```
coreweave is slow · switch to auto? (y)
```

Press **y** and the answer is fetched from somewhere else, at once, from the
endpoint that was already ranked second — no new decision made at the worst
possible moment. The question is asked **once** per answer, and it disappears
the moment your answer starts arriving, because by then it is moot.

**`y` is the only key the question takes.** There is nothing to press to say no, because
there is nothing to decline — the wait is happening either way, and refusing would only
leave you in it. So the question is not a card you have to clear: it takes itself down the
moment an answer starts arriving, or when the request ends, and every other key you press
is still your own.

Two things it does not do. It does **not** take the `y` out of a sentence you
are typing: the key only counts while the box is empty, and while you are
writing, `y` is a `y`. And it does **not** change your pin. Saying yes rescues
*this* answer; the next request goes to the machine you pinned, because that is
what pinning means.

With nobody watching — a task running unattended, a standing order firing
overnight — there is nobody to ask, so a pinned endpoint that has gone quiet
past the patience for that kind of work borrows another one for that answer and
says so in the log. An instruction whose author cannot be reached is honoured by
getting them their answer.

## Waiting on a model that is thinking

A reasoning model writes its thinking before it writes a word you can read, and
none of that is on your screen. It is not a stall, so aforge does not treat it
as one. While a thought is arriving, its patience is measured against **how the
model itself usually thinks** — learned from every
thought aforge has timed for it, at the effort it was asked at, and from nothing
else.

So a thought that has gone quiet far beyond that model's usual thinking is
treated as a stall and rescued the same way a slow lane is: a second request
goes out and the status line shows `slow · trying …`. A deep thought that is
still arriving is left alone for all the patience it needs, because leaving one
costs a whole fresh thought and buys you nothing.

Until aforge has watched a model think a few dozen times it has no opinion about
that model's thinking, and only the ceiling on silence can end a hung one. That
is why a model you have been using feels quicker to rescue than one you have
just picked.

**The ceiling on silence is a ceiling on a still wire.** A model writing
reasoning is writing, so the clock the ceiling runs on is the time since the
endpoint last sent anything at all — readable or not. A thought that has been
arriving steadily for two minutes has never been silent for one second of it,
and nothing acts on it. The moment the deltas stop, the ceiling starts from
there and fires exactly where it always did.

Keepalives buy nothing. A router that holds the connection open by saying
nothing in a well-formed way is proof about the path and about nothing else, so
a lane that has stopped writing reaches the ceiling however politely it keeps
the line open.

Before 2026-09-09 that clock ran from the last word you could READ, which is
none at all during a thought — so every model that thought for longer than the
ceiling was reported as a stall at exactly the ceiling while it was writing at
full rate, and one measured turn wrote 6,174 tokens of reasoning in 108 seconds
and was called slow ten seconds in.

## How long aforge waits before it does something — is it ten seconds, five, two minutes, and why the line is never blank

Two different clocks, and mixing them up is why waiting used to feel slow.

**The first is speech, and it is one second.** Any wait aforge is holding you in
says what it is waiting for within a second of starting. Nothing is cut at one
second and nothing is retried; it is the moment the line has to stop being
blank. So `connecting`, `first word`, `thinking`, `paced`, `waiting for
connection` — one of those is on the status line the whole time, with the clock
counting up under it.

**The second is action, and it is ten seconds.** Ten seconds of nothing arriving
is when aforge stops waiting and does something about it: a second request to
another machine, and the line changes to `switching`. That is a ceiling, not a
target — a machine aforge has timed is acted on at its own measured pace, which
for a fast one is a second or two.

Ten seconds is measured, not chosen. Across ten days of real calls the first
word of a conversation turn arrives in 1.6 seconds at the middle, 8.4 seconds
for nine turns in ten, and 13 seconds for nineteen in twenty. Acting at five
seconds would touch twice as many calls and rescue a smaller share of them,
because under ten seconds almost everything still quiet is an ordinary call in
progress. Of the calls still silent at ten seconds, more than three quarters
answer perfectly well.

**Work nobody is watching waits longer, on purpose.** A task node gets thirty
seconds and a standing pass sixty, because nobody is sitting in front of them
and a second request costs money. They are never silent either — the same
sentence is on their row.

**Ten seconds always does something, even when a second request is too
expensive.** aforge only runs a small number of rescues — a second request to
another machine costs real money, so it keeps a small allowance and spends it
where it helps most. When the allowance is gone and the machine has sent nothing
at all, not one byte, aforge stops that attempt instead and asks somewhere else.
Before 2026-09-10 it did neither: four tasks that evening sat on one machine for
six and seven minutes after the ten seconds were up, because the only way to act
was the one aforge could not afford. If the machine IS sending something — the
router is talking, or the model is writing where you cannot see it — nothing is
stopped, because nine such calls in ten turn out to be seconds from an answer.

**A conversation turn gives up after ninety seconds** of not reaching any model
at all, and tells you so in one line. It is the point where every model in the
chain has had one fair try with a move between them: of the calls that recovered
in ten days of logs, two thirds had landed by then, and the ones that took
longer were spending the time asking the same machine again — which aforge no
longer does.

**And that one number is the whole of how long a failed call goes on trying.**
There is no separate allowance for how many times to ask, how long to wait out a
busy machine, how many machines to walk, or how many things to take off the
request — each of those was its own number until 2026-09-11, and together they
came to a total nobody could have told you. Now there is a clock, it scales with
who the work is for, and it is the same clock for every kind of failure:

| whose work | gives up after |
| --- | --- |
| a turn you are watching, or a task node with its room open | 90 seconds |
| a task node nobody is watching, a memory pass, a side errand | 4 minutes 30 |
| a standing order, a check, a design pass | 9 minutes |
| the one-token measurement behind the model list | 45 seconds |

While it is trying, the status row counts the machines rather than the tries:
`2 of 5` means the second of five machines that can serve this model, and when
aforge cannot tell how many there are it shows no number instead of a made-up
one.

## How long aforge keeps trying, and the one setting that changes it

The table above is the whole answer, and **`response.attempts` is the one thing
you can turn about it**. It is a multiplier on those times, not a number of
requests: `3` means three times as long — four and a half minutes on a turn you
are watching instead of ninety seconds — and the default is `1`, which is exactly
the table. Set it on the **Providers** tab of `/settings`, or with
`AFORGE_RESPONSE_ATTEMPTS`.

```
response.attempts: 3      # every give-up above, three times as long
```

**It used to be a count of sends, and it is not any more.** Until 2026-09-11 it
said how many times one request would be repeated — so asking for more patience
bought more identical requests inside the same deadline, which ended the call
anyway. The intent behind the setting was always "try harder before you tell me
you could not", and trying harder is time: more machines walked, more shapes of
the request tried, longer waited out of a busy pool. **What it will never buy is
the same bytes sent to the same machine again.** If you had written a number into
this row when it meant sends, it now means that many times the patience — a `3`
you set to get three tries is three times ninety seconds.

**Nothing else in aforge counts attempts.** Not the turn, not a task's worker,
not the naming errand, not the side calls that write a title or judge a route.
Each of them runs until its own clock above is gone, and the one number in a
failure sentence — `after 4 attempts` — is what that call actually spent, never a
ceiling it was allowed.

**Nothing waits behind a busy moment in silence.** When every request aforge is
allowed to have in the air at once is already in the air — which happens when
several windows and a task are working at the same time, or a machine has been
pacing the account — the next call queues. It says `connecting` while it does,
with no countdown, because nothing in aforge knows which of the calls ahead of it
will finish first, and a countdown to a moment nobody can name is worse than
none.

**A reply that is arriving is never cut for taking a long time.** The clocks
above are all clocks on SILENCE. A model writing steadily is left alone however
long the answer is; the only bound on a reply that is still arriving is twenty
minutes, which no healthy reply in ten days of logs has come close to — the
longest was twelve minutes.

## Why is it writing one word at a time — it never stopped, it just crawled

A stream does not have to stop completely to need rescuing. Once aforge has
measured how quickly a lane normally puts visible words on the page, it watches
the gaps between those words together. A long run at a small fraction of that
usual rate stops counting as progress toward the patience limit. If the crawl
continues for that kind of work's full ceiling, aforge acts just as it does on a
stream that went silent: it tries another lane, asks before leaving a pin, or
says the wait is real when there is nowhere to go.

One slow gap is still only one slow gap. The judgment comes from the run of
visible gaps, fades over the same time as the ceiling, and clears when the
stream recovers. A batch containing several visible tokens is counted at its
per-token rate, so ordinary batching does not look like a crawl.
Hidden thinking does not count as a visible word, so a model that interleaves
long thoughts between single words can still be rescued this way — but only
once its MEASURED visible rate has collapsed. A pause between words is not
enough on its own, however long, as long as the endpoint is still writing
something.

If aforge has never measured a visible rate for that lane, it invents none and
cannot judge a crawl this way. Only a period with no visible progress long
enough to reach the ordinary ceiling can then trigger action.

## Why a fast machine was skipped, or a cheap one never used — how long the work has to wait decides which machines it may go to

Every kind of call this build makes says how long it is willing to wait before
something is done about a silence: ten seconds for a chat turn, for a step of a
task you are watching, and for the quick lookups behind a keypress; thirty for
work running in the background; a minute for a standing pass; five seconds for
the one-token checks aforge makes of a machine itself. That number is not only a
stopwatch. It is also what decides which machines the
request is allowed to go to at all.

Before sending, aforge works out for every machine serving the model how long
it expects the WHOLE answer to take there — how long until the first word, plus
how long the rest takes at the speed that machine writes, plus the fact that a
machine which refuses four requests in five is really being asked five times.
Machines are ranked by that number, and any machine whose number is longer than
the wait this kind of call is willing to sit through is **left off the request
altogether**, by name, so the provider cannot fall back onto it.

There is no separate rule and no threshold anybody picked. A machine is refused
exactly when the answer is expected to take longer than this work waits. Two
things follow from that, and both are deliberate:

- **The same machine is refused for one kind of call and used for another.** A
  machine that takes twenty seconds is out of the question for something in
  front of your typing and perfectly fine for a standing pass.
- **Nothing is ever refused when there is nothing better.** If every machine
  serving a model is beyond the limit, none of them is refused — the request
  goes to the best of them rather than nowhere.

The speed that counts is the whole answer and not just the first word. A machine
can say its first word promptly and then write at two tokens a second, which is
a healthy start and a four-minute answer; that is what the 2026-09-11 reading of
a task step stuck for three and a half minutes turned out to be.

## A machine that is usually fast and sometimes takes a minute

For the answers you READ as they arrive, aforge does not rank machines by their
typical speed. It ranks them by how long an unlucky request takes.

A machine that starts in three seconds nine times out of ten and in a minute the
tenth is not a three-second machine to whoever drew the tenth, and a typical
figure cannot tell it apart from one that takes three seconds every time. So for
anything you watch, each machine is judged at roughly its own worst-in-ten, using
how much its answers have actually been seen to vary rather than an assumed
figure. A machine that is genuinely steady is barely moved by this and loses
nothing; an erratic one falls behind a slightly slower machine that is reliable.

For work nobody reads as it arrives, the typical figure is used instead — those
calls are many and small and what matters is their total.

## When a machine suddenly gets slower than it has ever been

Beliefs about a machine are built from many answers, which normally makes them
steady and occasionally makes them stubborn: one bad answer against fifty good
ones barely moves anything. So aforge also watches for a **step change** — a
run of answers that is not bad luck but a different machine than the one it was
measuring. When it sees one, the old evidence is thrown away rather than
averaged, and the next choice is made on what is happening now.

Before this, a machine whose writing speed collapsed about ninefold was still
being chosen five steps later, over half an hour, because each slow answer
arrived as one reading against a belief far too settled to move.

## When every lane is slow

Sometimes there is nowhere better to go — everything serving that model is
believed slow at once, which happens when a whole region is having a bad
afternoon. Switching would buy nothing, so aforge says the true thing instead:

```
all lanes slow · still waiting · 12s
```

That line means the wait is real, it is not a stall this build can end, and
nothing is being spent trying. It is the one honest thing left to say, and
saying nothing was the old behaviour.

**The number on the end is how long you have been waiting**, counting up from the moment
this request went out — whole seconds, and `1m 20s` once it is past a minute. It is not a
countdown, and there is nothing behind it about when the answer will come: it is there so
that a line which cannot promise you anything can at least be honest about the size of what
it is asking you to sit through.

## What the status line is telling you

| what you see | what happened |
| --- | --- |
| `via cloudflare · 0.6s · 61 t/s` | an ordinary answer, and who wrote it |
| `deepseek-v4-flash@cloudflare` | you pinned cloudflare, and every request for the model goes there |
| `slow · trying parasail…` | a machine was late or its visible answer had slowed to a crawl; a second request is out and the first to answer wins |
| `refused · trying parasail…` | a machine said it will not serve this model; the answer has already moved |
| `parasail refused` | the machine that second request went to said no as well |
| `via parasail · rescued` | it worked, for this answer only |
| `coreweave is slow · switch to auto? (y)` | your pinned machine is quiet, and you can end the wait |
| `coreweave cannot serve this model; routing on auto for this model until you pin again` | the machine you pinned said no, so the pin is retired for this model |
| `all lanes slow · still waiting · 12s` | everywhere is slow; nothing to be done but tell you, and how long you have waited |

## When a machine refuses to serve the model

`slow` and `refused` are two different facts and the row says which. **Slow** is
a wait: the machine is answering and taking its time, or its visible words have
slowed far below the rate aforge measured for it. **Refused** is a machine saying
it will not serve this model at all — the router answers
`No allowed providers are available for the selected model. Providers serving
<model>: digitalocean, deepinfra, … but your request's provider.only preference
permits only: coreweave`, which means the machine aforge asked for is not in the
set that serves this model right now.

A refusal is final for that machine, immediately:

- the next request leaves at once, for a different machine, and does not name
  the refused one;
- that machine is taken out of the set aforge will choose from for this model,
  so it is not picked again later in the session;
- if there is nowhere left to move to, the request itself is widened — the
  demand for one machine is the first thing dropped — and the answer usually
  arrives from wherever the router picks. This happens even when the last
  machine tried failed some other way (busy, or went quiet): a widening that was
  put off for a move is always done before you are shown anything, with its
  `Retry 1/N: relaxed the endpoint filter` lines. If nothing lands, the error you
  see is the most useful one — a machine's rate limit and its wait before an
  earlier machine's refusal.

**A machine refusing your request is a move too, not the end of the turn.** When
the answer carries the name of the machine that produced it — a `400`, a `404`,
an account policy, a model that machine will not serve — that is one machine's
answer about this request and the others have said nothing about it, so aforge
sends the next one straight to a different machine with that one left off. It is
the same walk a busy machine gets, and until 2026-09-11 it was not: the turn
ended there, and the move only happened on your *next* message, after aforge had
remembered the refusal. What still ends a turn is a refusal that names **nobody**
— that is the router reading the request itself and saying no, and every machine
alive would say the same thing.

If a later machine accepts the request and starts writing but that stream is
cut, the cut is the failure aforge acts on. The partial reply is cleared and the
existing bounded call retry routes around the machine that failed. An earlier
`No endpoints found` answer is not shown as the final error after another
machine demonstrably accepted the request.

## When a machine is too busy — a rate limit, too many requests, a 429, and how long aforge stays away from it

**Too many requests is not a refusal.** A machine that answers
`API error (429): Provider returned error (via Io Net)` has not said anything
about your request — its queue is full for the moment. So it is not written off
the way a refusal is. It is **stepped around for a while**, and it comes back on
its own.

- **When the answer names the machine, aforge stops sending there.** Every
  request after it goes to a different machine for as long as that one asked to
  be left alone, and for **five minutes** when it named no time.
- **And that includes the request that collected it.** Its next try is written
  fresh, with the busy machine left off, so it walks on to another one instead
  of queueing behind the same full queue. Before 2026-09-10 it did not: the
  request was written once and sent again unchanged, which is how a single ask
  spent seventeen tries on one machine over eleven minutes and still ended
  `too many requests`. You see the walk as `2 of 6` on the status row while it
  happens.
- **The same machine is only ever asked twice when it is the only one there
  is** — a lane you pinned yourself, or a model with one machine behind it —
  and then aforge waits exactly as long as that machine asked for before trying
  again. That wait is shown as what it is: `waiting for coreweave · 12s`,
  counting down to the moment the machine named.
- **Moving to another machine costs no wait at all.** A pause between tries is
  what aforge pays to ask the *same* machine again; going somewhere else is a
  different request and it goes out immediately.
- **You never have to switch models to get past this.** When every machine
  behind the model is busy at once, aforge stops waiting and moves your turn to
  the next model instead, because another model is always quicker than a window.
  Work running inside a task has no other model to move to, so that is the one
  place aforge waits the window out — and it tells you which machine it is
  waiting for and how long is left.
- **It counts wherever the message arrived.** A rate limit can come back before
  a single word is written, or in the middle of a reply that had already started
  arriving. The machine is stepped around either way. Before 2026-09-10 only the
  first kind counted, so a busy machine that said "too many requests" halfway
  through a reply was handed the next request, and the one after that — three
  times in a minute and a half, on one measured turn.
- **A rate limit that names nobody is your whole account**, not one machine, and
  nothing is stepped around: there is nowhere better to go. aforge waits it out
  for as long as that kind of work is given — **ninety seconds** on a turn you
  are sitting in front of, four and a half minutes inside a task, nine for a
  standing order — and then hands you what the provider said. Sending the same
  request to a second machine would only spend the account's allowance faster.
  (Before 2026-09-11 these were separate numbers of their own, two minutes and
  ten; there is one clock now and it is the same one everything else on this
  page is measured against.)

## What all providers have been ignored means — a refusal from nobody

When the router answers `All
providers have been ignored`, no machine was ever asked: a list had removed the
whole set before the request left — either aforge's own running list of slow and
unavailable machines, or the ignored providers set on your account. Nothing is
taken away from any machine on that answer, because a machine that never got the
request has said nothing about it — it keeps its place for every other request.
If the machine you had pinned is the one nobody could reach, the pin itself is
still stood down and you are told, because a pairing your account cannot use is
one to stop asking for. aforge stops sending the list that emptied the set for
that model and the next request lands, so this is at most one wasted round trip
in a session rather than every request for five minutes.

The status row keeps up with the refusal and retry: `refused · trying parasail…`
while the answer is moving, and if parasail refuses too the promise is **taken
back** rather than left standing. The row reads `parasail refused`, which is what
actually happened; it never says `trying …` about a request that has already failed.

## When the base refuses a lane choice — why a proxy may not honour my pin

Some bases
take no lane choice at all — a plain OpenAI-compatible endpoint behind
`AFORGE_BASE_URL`, a proxy that strips the field, a gateway that never heard of
it. aforge finds out by asking: your pin goes out on a real request, once, and
if that is refused the same request is sent again without it — whether *that*
lands is the answer, so an unrelated bad request never costs you your pin. If
the base will not take the choice, you are told once, in the conversation:

```
api.example.com does not take a lane choice; coreweave is not being asked for, and your requests still go out
```

Your work still goes out; only the choice is left off. The settings row says it
too, so `pinned:` never stands as a claim about a request that did not carry it:
`pinned: coreweave (not taken on this base)`.

## Providers switched off on your account — OpenRouter's ignored-providers list, and the one refused round trip it costs

Your OpenRouter account can carry its own ignored-providers list: providers you
switched off and OpenRouter will not use. aforge cannot read that list.
Separately, aforge keeps its own running list of machines that are slow or have
refused. The two lists can leave no machine to ask even though neither list
emptied the set alone. OpenRouter then says `All providers have been ignored`
before any machine is asked.

That sentence is the only thing that tells aforge which machines your account
will not reach. Every machine aforge has timed for this model that aforge was
not itself refusing in that request stops being counted as somewhere the request
can land. On the next request, aforge drops the machine on its own list that is
nearest returning, and the request lands. A switched-off provider therefore
costs one refused round trip per model in a session, rather than one on every
request.

If a machine later answers, aforge counts it again immediately. Switching a
provider back on needs nothing from you.

The **privacy switch for providers that may train on paid prompts** is the same
kind of list, and OpenRouter names it: `0 endpoints out of 1 requested are
available matching your guardrail restrictions and data policy … Paid model
training violation (account settings)`. When that answer is about a machine
aforge asked for by name, the machine is remembered as out of reach for your
account — for **every model**, for **a day**, and across restarts
(`~/.aforge/v3/account-exclusions.json`) — so no later request names it and it
costs one refused round trip, once. A strict pin on it is stood down on every
model with the usual `cannot serve this model` line. Nothing about its speed is
written; an answer from it, or pinning it again, takes it back at once.

## Lanes on a custom base URL, a proxy, a mirror, or a self-hosted router — `AFORGE_BASE_URL`

Lanes are not tied to the OpenRouter hostname. Point aforge at any base with
`AFORGE_BASE_URL` — a proxy in front of the router, a mirror, a router of your
own, the router by its IP — and it **asks that base whether it publishes an
endpoints page**: the first background fetch of a model's sheet is the question.
A base that answers with a page has lanes exactly as the built-in endpoint does,
with the same auto ranking, pins, hedges and status line. Nothing about the
address is inspected; a router is recognised by what it answers.

A base with **no endpoints page at all** — it answers with a not-found page
rather than the router's own error message, the way a plain proxy does — is
remembered as having none for five minutes, then asked again in the background.
A router that has the page but **does not publish that one model** says so in
its own words, about the model, and nothing is remembered about the base. A 500,
a timeout or a rate limit is a bad afternoon rather than an answer.

## Does a proxy honour my pinned lane — how a custom base answers

**Whether a base honours a lane choice is learned the same way**, never from
its address. A base that served an endpoints page takes one. Any other base is
asked once, and only once you have **pinned** something — a pin is the only
thing there is to ask with, so a base nobody pinned anything on is sent no lane
opinion at all, exactly as before. Your pin goes out on a real request; if the
base refuses it, aforge sends that request again once without it, and whether
*that* lands is the answer. A base that refuses the choice, or that answers
without ever naming the machine that served, is remembered as not taking one and
**says so** (the refusal section above has the sentence).

**A proxy that forwards to the router but strips the lane name out of its
answers is read as not taking your choice**, deliberately. aforge cannot tell
that proxy from one honouring your pin silently — nothing in the answer says
which machine served — so it tells you, sends later requests bare, and the proxy
then routes your model however it likes. Your work still goes out; your pin is
not honoured there, and you know rather than guess.

Neither question costs an extra call of its own, and pointing `AFORGE_BASE_URL`
somewhere else asks the new address afresh about both.

A directly connected service is simpler: it has one lane, so there is nothing to choose
between and no lane sheet to open. That is not a fault. The service name carried by the
model id is already the whole route.

## Turning lane routing off

Set routing off (`/settings`, or the `routing` row) and aforge sends every
request with no opinion at all. It still will not let you wait forever — a
ceiling on how long a silence runs before *something* is said about it is not
steering, it is the promise this surface makes — but it stops choosing endpoints
for you, stops sending second requests, and stops spending anything on speed.

**The row has three answers, not two, and the third is not off.** Left alone, aforge asks
for the fastest machine on the turns you are waiting through, and on the work you are not
watching it still weighs speed, at a quarter of that weight — a task ends when its slowest
call ends, and a machine that refuses four requests in five costs five sends for one answer,
so its seconds are never free. That is the split the rest of this page describes. Writing a word in the row
overrides that everywhere: `latency` asks for the fastest one on every call, background
work included; `price` ranks on price alone on every call, your own turns included, which
is you saying that speed is not worth money anywhere; and `off` is the paragraph above.
`price` still measures machines and still chooses between them. `off` stops the choosing.

**`off` does not stop the remembering, and that is deliberate.** aforge still writes down
which machine answered and which one refused, because that is what lets a request that
has just been refused go somewhere else instead of back to the same place — recovery is
not steering, and a build that forgot a refusal the moment you switched routing off would
be a build that could only ever retry into it. Nothing it remembers reaches the wire:
with `off`, every request goes out with no preference on it at all.

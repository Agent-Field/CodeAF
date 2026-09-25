# Hosts — which host answers your model, and the question a slow one asks

**The word is host.** Earlier builds called the same thing a *lane*, and the
settings row, the picker's hints and this page all said so; `lane` still spells the
setting on disk (`lane.talk`) and the file of learned speeds (`lanes.json`), but
nothing you read says it any more. If you are looking for lanes, or for the machine
or endpoint behind a model, this page is the one.

A model name is an address, not a host. Behind one name there are usually a
dozen **hosts** — different companies running the same model — and they are not
alike: on a measured day in August, seventeen of them serving one model differed by
**7×** on how long they took to say their first word and by **12×** on how fast they
wrote, at roughly the same price. Which one answers you is often a bigger
difference than which model you picked.

So codeaf keeps an opinion about them, per model, learned from every answer it
has ever timed — and it tells you which one served you, in the status line:

```
via cloudflare · 0.6s · 61 t/s
```

## Why a model name starts with ~ — the tilde or squiggle in front of a model name, and whether it is a typo

Some rows on the model list are prefixed with `~`. The model codeaf ships with is spelled
`~deepseek/deepseek-v4-flash-latest`, and that is the name on the model picker and on the
status line.

**The leading `~` is not a typo and not a home directory.** It is the router's own marker
for a *floating* name: one that does not point at a fixed build of a model but at
whichever build is current. It is part of the id as the router publishes it, not something
codeaf puts there, and a name without one points at a fixed build. Everywhere else on a
terminal a leading `~` means your home folder, and codeaf still reads it that way when it
is followed by a slash — `~/` is a path, `~deepseek/…` is a model.

What that costs is the next section: a floating name and the build it points at are two
different names, and only one of them has hosts behind it.

## A model name that ends in latest — what the pointer names, and what via says instead

`…-latest` is the other half of the same row. Two things about it are worth knowing,
because neither is guessable.

**A pointer is not a host, so what is learned is filed under what it points at.**
`…-latest` names whichever dated build the model's makers published most recently — today
`deepseek/deepseek-v4-flash-0731` — and it is that dated build the router publishes
hosts for. So the hosts codeaf asks about, the speeds it writes down, and the row it
keeps in `~/.codeaf/v3/lanes.json` are all filed under the dated name, never under the
pointer.

That name is not something the screen says back to you, which is why it surprises people
who go looking. The picker and the status line show the name **you** chose, and `via
cloudflare` names the host that answered rather than the model it answered for.

**When the pointer moves, nothing is carried across.** The newer build is a different
model with its own hosts and its own speeds, so it starts its own record from the sheet
the router publishes for it, and the older build's record stays where it is instead of
being spent on a model nobody has measured. That is the same rule as everywhere else here:
a measured thing is about the thing that was measured.

## Auto, and who is actually choosing (this used to be called the lane) — the router first, and when codeaf takes over

Left alone, codeaf is on **auto**, and `auto` means the router routes. OpenRouter
balances the hosts behind your model on its own queues and prices, and codeaf
watches: every answer names the host that served it, so the speed and the
quality of what the router hands you are learned exactly as if codeaf had asked
for them. You see the host in the status line — `via cloudflare · 0.6s · 61 t/s`
— and the picker's `auto` row tells you what codeaf would choose if it were
choosing.

**Why did it pick that host on the very first message?** Because on the first
message nobody has chosen anything: no pin, no takeover earned yet, so the pick
is the router's own — whichever host its balance landed on. The host is
named in the status line so the choice is never invisible, and from that first
answer on it is being learned like any other.

**codeaf takes over when the router lets go.** If a model's answers start coming
back refused (a 429, a host that cannot serve the shape) or unusable (the
thread lost, tool markup, a stream that had to be cut) — twice in a short while —
codeaf stops lending the router the choice and picks the host itself, from the
hosts it has been watching all along. Only a refusal the ROUTER earned counts:
once codeaf is the one choosing a host — during a takeover, or under your
pin — a refusal of that pick is about the pick, not another strike against
the router, so a takeover's own demands cannot keep it alive. The conversation
says so once, in one sentence, and after about half an hour of good answers the
choice is the router's again. Pinning a host yourself in `/model` ends it
there and then: your word outranks either of them.

**`openrouter` is `auto` without the safety.** It is the same router routing, and
codeaf never takes over no matter what comes back. Choose it when you would
rather have the router's price balance than be rescued from its bad minute.

The rest of this page — the closed set, the refusal walk, the probe — describes
what happens while codeaf is choosing: during a takeover, and whenever you have
pinned a host yourself. All of it is written about `routing` at `latency` or
`price`. With the row at `simple` — which is what it ships as — none of it runs:
no takeover, no ranking, no measuring, because that row sends exactly what you
asked for and nothing else; *How do I stop codeaf choosing the host itself* below has it. With
the row at `off` the choosing stops too, and the last section says what that
leaves standing.

**A run started from a terminal is routed on the same terms.** `codeaf do`, `codeaf run`
and `codeaf plan run` open no conversation and draw no status line, and they used to
take whatever host the router happened to hand them. They fetch the same sheet now and
rank it with the same arithmetic — so a headless machine, one that only ever runs work
from a terminal, is choosing between hosts rather than between none, and every run
leaves a record the next one starts from. Nobody is sitting in front of an errand, so it is
the price ranking above that applies to it. `auto` is the answer only while your home's
host row says auto; a pin in that home replaces this ranking at every terminal door.

**A model it has never sent to is not a model it knows nothing about.** The
public sheet names every host serving it, and what codeaf has learned about
a *company* — that this one is quick, that one queues — carries across every
model that company serves. So the first request to a brand-new model is still
routed, still has a clock on it, and asks for a fresh sheet in the background
while it goes. You never wait for that fetch.

## The hosts your message may go to are asked for by name — and the router may not go outside them

When codeaf has measured enough to have an opinion, it does not merely *rank* the
hosts it wants. It **names the set it will accept, and closes it**: the
router may not serve your message from a host outside that set.

The list used to be advice. The router read it, weighed it against its own
queues and prices, and was free to hand your message to somebody else — and often
did. Over ten days of this build's own call record, the host codeaf asked for
first served 29 requests in every 100; the host a closed set names served 93.
Everything codeaf works out before sending — which hosts can do the job at all,
which are quick enough for how long this kind of work waits, what each one costs —
was being spent on a list the router could put aside.

**What it costs, and it is a real cost.** A closed set can run out. If every
host in it is busy at once, your message is refused rather than handed to
whoever happened to be free. What follows is a move, not an ending: the host
that refused comes off the set and the request goes straight to the next one in
it, with no wait (`trying another host · 2 of 3`); and when the last one has
gone, the set comes off the request entirely, so the router has its whole
roster back for the one send that needs it.

**A set of one is never made this way.** One host named with nothing to fall
back on is a pin — it is exactly what pinning a host yourself sends — so codeaf
closes a set only when it has at least two hosts it is happy with. With one,
it ranks what it has and leaves the router its usual freedom. Pinning a host
still means what it always did, and nothing here narrows a set you asked for.

## The hosts one message may go to are decided once — why a retry walks the same set, and why something learned mid-answer waits for your next message

Before the first byte of a request leaves, codeaf decides which hosts that
request may go to: the ranked few it asks for by name, and the ones it asks the
router to skip. **That decision is made once and it lasts the whole request.**

It matters because one message is often sent more than once without you seeing
it. A host answers with a fault, a pool turns out to be full, the shape has to
be widened and tried again: each of those is the same request going out afresh.
What changes between them is only what this request has learned about itself —
the hosts that have already refused *it*, which are left off the next one.
What does not change is the ranking.

So something measured while your message is in flight — the host list
finishing a refresh a second late, another conversation discovering that a
host has got quick — is spent on your **next** message and not this one. That
is deliberate. The line that tells you what is happening (`trying another
host · 2 of 3`), the clock that decides when to stop waiting on a host, and
the names on the request itself all have to be about one set of hosts. A
ranking that appeared on the third try would be a set nothing else had heard of,
and you would be told about a walk through hosts that were never asked for.

If a request starts on a model codeaf has measured nothing about, it has nothing
to rank and asks for nothing by name — the router chooses — and it stays that way
for the whole of that request even if the host list lands halfway through.
Your next message is routed.

## When the host list says a machine cannot take tool calls, or is half down — why codeaf tries it anyway

The public sheet carries three claims about each host that codeaf used to
treat as final: whether it honours a tool call, what share of the last five
minutes it was answering, and whether the router's own operators have marked it
down. A host failing any of them was removed from the candidate set outright.

**They are opinions now, not doors.** A host the sheet doubts is **ranked
last** — behind every host nothing is doubted about, never asked first while
something better can serve you — and it is still there when the hosts in
front of it are busy or refuse. About **one request in ten** is sent to it first
on purpose, because a host nobody ever asks can never show the sheet was
wrong about it.

This changed because the sheet was measurably wrong. On 2026-09-10 a task was
answered three times in a row, six seconds each, by a host the sheet flags as
unable to take tool calls — while the same task sat on a busy host collecting
nine refusals, because the one that was working had been removed from every
request carrying tools.

**What a host's own answers say beats what the sheet says about it.** Once
codeaf has seen a host return usable answers to this kind of work, the sheet's
doubt stops applying to it and it is ranked on its numbers like anything else.
That belief fades over about an hour if the host stops answering well, so
nothing learned here is learned forever.

One claim is still a closed door, and it is not the sheet's: when the **router
itself** answers that a host cannot serve this model, that host is not a
candidate at any rank. That is an answer to a request codeaf really made, not a
page published some minutes ago.

## Learning which host finishes my work faster — why the first message does not go to the most expensive host

Auto considers both the first words and the generation that must finish before
the next step can run. Readable prose can arrive while you read. Reasoning and
tool arguments keep the next operation waiting, so their completion speed matters.

Completed calls teach codeaf how much of each kind to expect for that model,
whether tools are available, and its reasoning setting. Recent evidence counts
more; stale evidence gives way to the conversation's previous answers. A new
conversation with no evidence still has no measurement of how long its first
answer will run. To compare hosts, codeaf reads that absence as a typical
readable answer rather than no answer at all, so a host that charges ten
times as much to write does not win the first turn on its first word alone. It
does not keep that comparison as a measurement. The request's output limit
bounds a learned estimate. Capped, interrupted and unusable replies do not teach
it that a complete answer is short.

The measurements share the existing local routing history across sessions, with
a bounded number of remembered request types. Host names, prices and speeds
come from the host information and actual calls; there is no preferred-host
list to maintain. A successful host stays preferred for that conversation's
cache, while the slow-response monitor watches that host and can still rescue
a stalled request under the existing spending limits.

Text arriving in a batch earns progress for its approximate token count, so a
host that sends whole phrases is not judged as though each phrase were one
token. Tool-only replies also teach the first-token and generation clocks.

When a watched request fails and this call's own budget can pay for another
host, that host is tried before repeating the failed request. Rate
limits still respect their retry delay. Without an affordable alternative, the
existing bounded retries and wait reporting remain.

## Which host am I pinned to — pinning one host yourself (pinning a lane), how to change the host for a model, left and right arrows in the model picker, the @ after the model name, and whether codeaf do uses the lane I pinned

You can name the host yourself. Open `/model` and press `→` (or `tab`) on the model:
its hosts — the machines serving it — open under it, the cursor **moves into them**,
onto the host you pinned or onto `auto` when you have not, and the list scrolls so the
model and every host are in view. `enter` pins the host under the cursor — every request
for that model goes there until you say otherwise — and `←` (or `tab`) walks back out.
The hint slot says which: `→ hosts · alt+s sort · enter switch · ctrl+t effort · esc` on a
model, `← back · alt+s sort · enter choose · esc` inside. On the default service, the `openrouter` row means
"no opinion from me — let the router balance it".

## What enter on the openrouter row does — it chooses default and opens the list under it

`enter` on the bare `openrouter` row writes the same answer the `default` row **inside** that
row's own list writes: ask for no host, let the router balance. It is that answer reached
one press earlier, not a fourth option.

**So it opens the list as well as choosing**, and the cursor lands on `default` with the
selected band on it. Shut, the gesture reads as "you have chosen openrouter", which sounds
like a destination and hides that there was a list of machines under it at all. Open, it
reads as the true sentence: here are the machines this routes between, and you have chosen not
to pick among them.

Pressing `enter` there again does **not** shut it. `enter` never means "close" anywhere in
this list — `←` or `tab` is the way back out.

## Going back to auto — unpinning with the same key that pinned, and filtering inside an open fold

**`enter` on the host you are already pinned to takes the pin off.** It is a toggle on
the one key that put it there, and the hint slot says so while the cursor is on that row:
`enter unpin · ← back · esc`. The row goes back to `auto`, the `@host` comes off the
model's name, and the next request carries no host at all. The `auto` row at the top of
the fold still does the same thing and is still the explicit way to say it — the toggle
exists because reaching that row meant walking `↑` past every host in the list, and one
press too far lands on another model's row, where `enter` switches the model instead.

One case is deliberately not a toggle: after the host you pinned has refused the model
(below), nothing is asking for it any more, so `enter` there **pins it again** rather than
unpinning — which is the "pinning again puts it straight back" the refusal promises.

**Typing in the box while a fold is open filters the hosts, not the models.** With
`morph`'s fold open, typing `mor` narrows it to the hosts whose names carry those
letters and leaves the fold standing. The matching is the same as for a model id — every
word you type has to match, prefix first — and a query that matches none of that model's
hosts falls through to filtering the model list as it always has, closing the fold with
it.

**But `@cloudflare` in the picker's box finds nothing.** Until 2026-09-17 typing it there
kept the models that host serves and opened the first of them on it; the box searches
names only now, and no model id carries an `@`, so the list comes back empty. The two doors
onto a host are still open and are the ones to use: `→` on a model lists its hosts,
and `/model @cloudflare` from the box pins one outright.

**The host you are pinned to is written on the model's name** — `deepseek-v4-flash@cloudflare`
on the line above the box and on a phone's status deck — with the same `@` you would
type in `/model @cloudflare`. `/status` says it on a `lane` line under `model` — that one row
keeps the old word because it is also the key `/status --json` prints. On `auto`
and `openrouter` there is no `@`, and none once a pin has been retired. Pressing the
name opens the picker with the cursor on the pinned host.

A pin is an instruction, so codeaf keeps it. It does not quietly send your work
somewhere else because it thinks it knows better.

**The pin belongs to your home, not to one conversation.** Every door reads the same
profile row when it opens: `codeaf do`, `codeaf exec`, `codeaf plan`, `codeaf run` and
the background pass all honour the host you picked, just as the chat does. A run from a
terminal and a task running overnight therefore ask for your pinned host too.

## A model with no hosts measured yet — the model picker says no machine has been measured for this model, and no host list opens

**A model nobody has measured still opens**, onto `auto` and `openrouter` — and
`openrouter` opens too, because the `default` row is always inside it. Where the machines
would be there is one line:
`no host has been measured for this model yet — hosts show up after its first answer`.
Opening it asks for that model's list of hosts in the background. With the routing
row at `off` nothing opens at all.

## When the host I pinned cannot serve the model — a machine that will not serve it, the one thing that ends a pin without me

**There is exactly one thing that ends a pin without you: the host you named
saying it will not serve that model at all.** That is not a wait and not a bad
afternoon — the router answers `No allowed hosts are available for the
selected model. … but your request's host.only preference permits only:
coreweave`, which is the wire saying this host and this model do not go
together. Asking again buys the same 404, so codeaf stops asking, and says so
once, in the conversation, at the moment it happens:

```
coreweave cannot serve this model; routing on auto for this model until you pin again
```

What that means, exactly:

- **for that model**, every later request in this run goes out with no host
  demanded at all — routed the way `auto` routes;
- **the request that collected the refusal is widened and sent again**, once, so
  your answer still arrives. If that is refused too the turn ends, and says so;
- **the line stays in the conversation.** It is not one of the dim retry notes
  the work chip collapses when an answer lands, so it is still on the screen
  after the turn finishes;
- **your settings row is not touched.** The `host` row on the Providers tab still reads
  `pinned: coreweave`, exactly as you wrote it. What changes is everything that names the
  host **requests are going to**: the `@coreweave` comes off the model's name, the tail
  on the `your model` row reads `auto (coreweave cannot serve this model)`, and the fold's
  mark moves to `auto`;
- **every other model still goes to that host.** The refusal was about one
  pairing;
- **pinning again puts it straight back**, on the very next request — and that
  includes choosing the host you already had, which is a row that did not
  change and an instruction that did;
- it lasts until you pin again or you close the window, and a task the
  conversation starts inherits it rather than paying for the refusal again.

The sentence is said **once** per host and model, for the whole run.

## When a pinned host goes quiet — the `switch to auto?` question, and how to say no to it

It still has to do something about a wait, and what it does is **ask you**:

```
coreweave is slow · switch to auto? (y)
```

Press **y** and the answer is fetched from somewhere else, at once, from the
host that was already ranked second — no new decision made at the worst
possible moment. The question is asked **once** per answer, and it disappears
the moment your answer starts arriving, because by then it is moot.

**You are only ever asked about a host you pinned yourself.** On auto, codeaf
also asks for hosts by name — the closed set above — but that set is its own,
so a slow host in it is simply left: the answer is started somewhere else and
you are told what is happening (`trying another host`) rather than asked to
decide anything. The question is what your own instruction earns.

**`y` is the only key the question takes.** There is nothing to press to say no, because
there is nothing to decline — the wait is happening either way, and refusing would only
leave you in it. So the question is not a card you have to clear: it takes itself down the
moment an answer starts arriving, or when the request ends, and every other key you press
is still your own.

Two things it does not do. It does **not** take the `y` out of a sentence you
are typing: the key only counts while the box is empty, and while you are
writing, `y` is a `y`. And it does **not** change your pin. Saying yes rescues
*this* answer; the next request goes to the host you pinned, because that is
what pinning means.

With nobody watching — a task running unattended, a standing order firing
overnight — there is nobody to ask, so a pinned host that has gone quiet
past the patience for that kind of work borrows another one for that answer and
says so in the log. An instruction whose author cannot be reached is honoured by
getting them their answer.

## Waiting on a model that is thinking

A reasoning model writes its thinking before it writes a word you can read, and
none of that is on your screen. It is not a stall, so codeaf does not treat it
as one. While a thought is arriving, its patience is measured against **how the
model itself usually thinks** — learned from every
thought codeaf has timed for it, at the effort it was asked at, and from nothing
else.

So a thought that has gone quiet far beyond that model's usual thinking is
treated as a stall and rescued the same way a slow host is: a second request
goes out and the status line shows `slow · trying …`. A deep thought that is
still arriving is left alone for all the patience it needs, because leaving one
costs a whole fresh thought and buys you nothing.

Until codeaf has watched a model think a few dozen times it has no opinion about
that model's thinking, and only the ceiling on silence can end a hung one. That
is why a model you have been using feels quicker to rescue than one you have
just picked.

**The ceiling on silence is a ceiling on a still wire.** A model writing
reasoning is writing, so the clock the ceiling runs on is the time since the
host last sent anything at all — readable or not. A thought that has been
arriving steadily for two minutes has never been silent for one second of it,
and nothing acts on it. The moment the deltas stop, the ceiling starts from
there and fires exactly where it always did.

Keepalives buy nothing. A router that holds the connection open by saying
nothing in a well-formed way is proof about the path and about nothing else, so
a host that has stopped writing reaches the ceiling however politely it keeps
the line open.

Before 2026-09-09 that clock ran from the last word you could READ, which is
none at all during a thought — so every model that thought for longer than the
ceiling was reported as a stall at exactly the ceiling while it was writing at
full rate, and one measured turn wrote 6,174 tokens of reasoning in 108 seconds
and was called slow ten seconds in.

## How long codeaf waits before it does something — is it ten seconds, five, two minutes, and why the line is never blank

Two different clocks, and mixing them up is why waiting used to feel slow.

**The first is speech, and it is one second.** Any wait codeaf is holding you in
says what it is waiting for within a second of starting. Nothing is cut at one
second and nothing is retried; it is the moment the line has to stop being
blank. So `connecting`, `first word`, `thinking`, `paced`, `waiting for
connection` — one of those is on the status line the whole time, with the clock
counting up under it.

**The second is action, and it is ten seconds.** Ten seconds of nothing arriving
is when codeaf stops waiting and does something about it: a second request to
another host, and the line changes to `switching`. That is a ceiling, not a
target — a host codeaf has timed is acted on at its own measured pace, which
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
expensive.** A second request to another host costs real money, so every
rescue is priced before it goes out — against what THIS call may spend, which is
how long it is allowed to keep trying converted into money at what a second of
your waiting is worth. A rescue costing a couple of cents against a minute and a
half of your time is afforded; one costing more than the whole wait is worth is
not. When a rescue is refused and the host has sent nothing at all, not one
byte, codeaf stops that attempt instead and asks somewhere else. Before
2026-09-10 it did neither: four tasks that evening sat on one host for six and
seven minutes after the ten seconds were up, because the only way to act was the
one codeaf could not afford. If the host IS sending something — the router is
talking, or the model is writing where you cannot see it — nothing is stopped,
because nine such calls in ten turn out to be seconds from an answer.

**There is no per-session rescue quota.** There used to be: at most two rescues
in any twenty requests, and at most a tenth of the last hour's bill, shared by
everything running in codeaf at once. That is gone, and it is gone because it
answered the wrong question — a count spread over twenty requests cannot tell the
one that needs rescuing from the nineteen that do not, so it refused whichever
asked last. On 2026-09-11 that is exactly what happened: a host wrote 604
words in 86 seconds with somebody watching, the rescue was called for, and the
quota said no on behalf of requests that had already finished. What bounds a
rescue now is this call's own budget and how many requests one question may have
running at once, which is four.

**A conversation turn gives up after ninety seconds** of not reaching any model
at all, and tells you so in one line. It is the point where every model in the
chain has had one fair try with a move between them: of the calls that recovered
in ten days of logs, two thirds had landed by then, and the ones that took
longer were spending the time asking the same host again — which codeaf no
longer does.

**And that one number is the whole of how long a failed call goes on trying.**
There is no separate allowance for how many times to ask, how long to wait out a
busy host, how many hosts to walk, or how many things to take off the
request — each of those was its own number until 2026-09-11, and together they
came to a total nobody could have told you. Now there is a clock, it scales with
who the work is for, and it is the same clock for every kind of failure:

| whose work | gives up after |
| --- | --- |
| a turn you are watching, or a task node with its room open | 90 seconds |
| a task node nobody is watching, a memory pass, a side errand | 4 minutes 30 |
| a standing order, a check, a design pass | 9 minutes |
| the one-token measurement behind the model list | 45 seconds |

While it is trying, the status row counts the hosts rather than the tries:
`2 of 5` means the second of five hosts that can serve this model, and when
codeaf cannot tell how many there are it shows no number instead of a made-up
one.

## How long codeaf keeps trying, and the one setting that changes it

The table above is the whole answer, and **`response.attempts` is the one thing
you can turn about it**. It is a multiplier on those times, not a number of
requests: `3` means three times as long — four and a half minutes on a turn you
are watching instead of ninety seconds — and the default is `1`, which is exactly
the table. Set it on the **Providers** tab of `/settings`, or with
`CODEAF_RESPONSE_ATTEMPTS`.

```
response.attempts: 3      # every give-up above, three times as long
```

**It used to be a count of sends, and it is not any more.** Until 2026-09-11 it
said how many times one request would be repeated — so asking for more patience
bought more identical requests inside the same deadline, which ended the call
anyway. The intent behind the setting was always "try harder before you tell me
you could not", and trying harder is time: more hosts walked, more shapes of
the request tried, longer waited out of a busy pool. **What it will never buy is
the same bytes sent to the same host again.** If you had written a number into
this row when it meant sends, it now means that many times the patience — a `3`
you set to get three tries is three times ninety seconds.

**Nothing else in codeaf counts attempts.** Not the turn, not a task's worker,
not the naming errand, not the side calls that write a title or judge a route.
Each of them runs until its own clock above is gone, and the one number in a
failure sentence — `after 4 attempts` — is what that call actually spent, never a
ceiling it was allowed.

**Nothing waits behind a busy moment in silence.** When every request codeaf is
allowed to have in the air at once is already in the air — which happens when
several windows and a task are working at the same time, or a host has been
pacing the account — the next call queues. It says `connecting` while it does,
with no countdown, because nothing in codeaf knows which of the calls ahead of it
will finish first, and a countdown to a moment nobody can name is worse than
none.

**A reply that is arriving is never cut for taking a long time.** The clocks
above are all clocks on SILENCE. A model writing steadily is left alone however
long the answer is; the only bound on a reply that is still arriving is twenty
minutes, which no healthy reply in ten days of logs has come close to — the
longest was twelve minutes.

## Why is it writing one word at a time — it never stopped, it just crawled

A stream does not have to stop completely to need rescuing. Once codeaf has
measured how quickly a host normally puts visible words on the page, it watches
the gaps between those words together. A long run at a small fraction of that
usual rate stops counting as progress toward the patience limit. If the crawl
continues for that kind of work's full ceiling, codeaf acts just as it does on a
stream that went silent: it tries another host, asks before leaving a pin, or
says the wait is real when there is nowhere to go.

One slow gap is still only one slow gap. The judgment comes from the run of
visible gaps, fades over the same time as the ceiling, and clears when the
stream recovers. A batch containing several visible tokens is counted at its
per-token rate, so ordinary batching does not look like a crawl.
Hidden thinking does not count as a visible word, so a model that interleaves
long thoughts between single words can still be rescued this way — but only
once its MEASURED visible rate has collapsed. A pause between words is not
enough on its own, however long, as long as the host is still writing
something.

If codeaf has never measured a visible rate for that host, it invents none and
cannot judge a crawl this way. Only a period with no visible progress long
enough to reach the ordinary ceiling can then trigger action.

## Why a fast host was skipped, or a cheap one never used — how long the work has to wait decides which hosts it may go to

Every kind of call this build makes says how long it is willing to wait before
something is done about a silence: ten seconds for a chat turn, for a step of a
task you are watching, and for the quick lookups behind a keypress; thirty for
work running in the background; a minute for a standing pass; five seconds for
the one-token checks codeaf makes of a host itself. That number is not only a
stopwatch. It is also what decides which hosts the
request is allowed to go to at all.

Before sending, codeaf works out for every host serving the model how long
it expects the WHOLE answer to take there — how long until the first word, plus
how long the rest takes at the speed that host writes, plus the fact that a
host which refuses four requests in five is really being asked five times.
Hosts are ranked by that number, and any host whose number is longer than
the wait this kind of call is willing to sit through is **left off the request
altogether**, by name, so the router cannot fall back onto it.

There is no separate rule and no threshold anybody picked. A host is refused
exactly when the answer is expected to take longer than this work waits. Two
things follow from that, and both are deliberate:

- **The same host is refused for one kind of call and used for another.** A
  host that takes twenty seconds is out of the question for something in
  front of your typing and perfectly fine for a standing pass.
- **Nothing is ever refused when there is nothing better.** If every host
  serving a model is beyond the limit, none of them is refused — the request
  goes to the best of them rather than nowhere.

The speed that counts is the whole answer and not just the first word. A host
can say its first word promptly and then write at two tokens a second, which is
a healthy start and a four-minute answer; that is what the 2026-09-11 reading of
a task step stuck for three and a half minutes turned out to be.

## A host that is usually fast and sometimes takes a minute

For the answers you READ as they arrive, codeaf does not rank hosts by their
typical speed. It ranks them by how long an unlucky request takes.

A host that starts in three seconds nine times out of ten and in a minute the
tenth is not a three-second host to whoever drew the tenth, and a typical
figure cannot tell it apart from one that takes three seconds every time. So for
anything you watch, each host is judged at roughly its own worst-in-ten, using
how much its answers have actually been seen to vary rather than an assumed
figure. A host that is genuinely steady is barely moved by this and loses
nothing; an erratic one falls behind a slightly slower host that is reliable.

For work nobody reads as it arrives, the typical figure is used instead — those
calls are many and small and what matters is their total.

## When a host suddenly gets slower than it has ever been

Beliefs about a host are built from many answers, which normally makes them
steady and occasionally makes them stubborn: one bad answer against fifty good
ones barely moves anything. So codeaf also watches for a **step change** — a
run of answers that is not bad luck but a different host than the one it was
measuring. When it sees one, the old evidence is thrown away rather than
averaged, and the next choice is made on what is happening now.

Before this, a host whose writing speed collapsed about ninefold was still
being chosen five steps later, over half an hour, because each slow answer
arrived as one reading against a belief far too settled to move.

## When every host is slow — `all hosts slow`, which older builds spelled `all lanes slow`

Sometimes there is nowhere better to go — everything serving that model is
believed slow at once, which happens when a whole region is having a bad
afternoon. Switching would buy nothing, so codeaf says the true thing instead:

```
all hosts slow · still waiting · 12s
```

That line means the wait is real, it is not a stall this build can end, and
nothing is being spent trying. It is the one honest thing left to say, and
saying nothing was the old behaviour.

**The number on the end is how long you have been waiting**, counting up from the moment
this request went out — whole seconds, and `1m 20s` once it is past a minute. It is not a
countdown, and there is nothing behind it about when the answer will come: it is there so
that a line which cannot promise you anything can at least be honest about the size of what
it is asking you to sit through.

## When the answer is arriving too slowly to read

This is not the same thing as the line above, and it took a real afternoon to
learn the difference. `all hosts slow · still waiting` is about a **silence** —
nothing is arriving. Sometimes words ARE arriving and the wait is just as real,
because they are arriving at a crawl:

```
answering slowly · nowhere faster · 1m 26s
```

That line means codeaf measured the words appearing against the pace the host
it asked for was expected to write at, found the stream far under it, and has
nowhere better to send the question — every other host has been tried, or
this call cannot pay for a second request. The answer is still coming and the
words still appear as they arrive; the line is there so that the wait has a name.

On 2026-09-11 the same call showed one nudge and then nothing for eighty-six
seconds, because neither of the two lines codeaf had was true: it was not
writing at any speed a person would call writing, and it was not silent either.

**A host is judged against the pace its question was sent expecting**, not
against its own recent form. That distinction is the whole fix: as codeaf learned
that one host had slowed to a seventh of its usual speed, every stream it
served started to look normal *for that host*, and the guard quietly stopped
firing. What it is held to now is the host the routing choice named — the
reason the request went out at all — so a router that quietly hands your question
to something ten times slower is noticed.

## What the status line is telling you

| what you see | what happened |
| --- | --- |
| `via cloudflare · 0.6s · 61 t/s` | an ordinary answer, and who wrote it |
| `deepseek-v4-flash@cloudflare` | you pinned cloudflare, and every request for the model goes there |
| `slow · trying parasail…` | a host was late or its visible answer had slowed to a crawl; a second request is out and the first to answer wins |
| `refused · trying parasail…` | a host said it will not serve this model; the answer has already moved |
| `parasail refused` | the host that second request went to said no as well |
| `via parasail · rescued` | it worked, for this answer only |
| `coreweave is slow · switch to auto? (y)` | your pinned host is quiet, and you can end the wait |
| `coreweave cannot serve this model; routing on auto for this model until you pin again` | the host you pinned said no, so the pin is retired for this model |
| `all hosts slow · still waiting · 12s` | everywhere is slow; nothing to be done but tell you, and how long you have waited |
| `answering slowly · nowhere faster · 1m 26s` | words ARE arriving, too slowly to be worth reading, and there is no faster host to move to |

## When a host refuses to serve the model — a machine that will not serve my model

`slow` and `refused` are two different facts and the row says which. **Slow** is
a wait: the host is answering and taking its time, or its visible words have
slowed far below the rate codeaf measured for it. **Refused** is a host saying
it will not serve this model at all — the router answers
`No allowed hosts are available for the selected model. Hosts serving
<model>: digitalocean, deepinfra, … but your request's host.only preference
permits only: coreweave`, which means the host codeaf asked for is not in the
set that serves this model right now.

A refusal is final for that host, immediately:

- the next request leaves at once, for a different host, and does not name
  the refused one;
- that host is taken out of the set codeaf will choose from for this model,
  so it is not picked again later in the session;
- if there is nowhere left to move to, the request itself is widened — the
  demand for one host is the first thing dropped — and the answer usually
  arrives from wherever the router picks. This happens even when the last
  host tried failed some other way (busy, or went quiet): a widening that was
  put off for a move is always done before you are shown anything, with its
  `Retry 1/N: relaxed the endpoint filter` lines. If nothing lands, the error you
  see is the most useful one — a host's rate limit and its wait before an
  earlier host's refusal.

**A host refusing your request is a move too, not the end of the turn.** When
the answer carries the name of the host that produced it — a `400`, a `404`,
an account policy, a model that host will not serve — that is one host's
answer about this request and the others have said nothing about it, so codeaf
sends the next one straight to a different host with that one left off. It is
the same walk a busy host gets, and until 2026-09-11 it was not: the turn
ended there, and the move only happened on your *next* message, after codeaf had
remembered the refusal. What still ends a turn is a refusal that names **nobody**
— that is the router reading the request itself and saying no, and every host
alive would say the same thing.

If a later host accepts the request and starts writing but that stream is
cut, the cut is the failure codeaf acts on. The partial reply is cleared and the
existing bounded call retry routes around the host that failed. An earlier
`No endpoints found` answer is not shown as the final error after another
host demonstrably accepted the request.

## When a host is too busy — a rate limit, too many requests, a 429, and how long codeaf stays away from it

**Too many requests is not a refusal.** A host that answers
`API error (429): Host returned error (via Io Net)` has not said anything
about your request — its queue is full for the moment. So it is not written off
the way a refusal is. It is **stepped around for a while**, and it comes back on
its own.

- **When the answer names the host, codeaf stops sending there.** Every
  request after it goes to a different host for as long as that one asked to
  be left alone, and for **five minutes** when it named no time.
- **And that includes the request that collected it.** Its next try is written
  fresh, with the busy host left off, so it walks on to another one instead
  of queueing behind the same full queue. Before 2026-09-10 it did not: the
  request was written once and sent again unchanged, which is how a single ask
  spent seventeen tries on one host over eleven minutes and still ended
  `too many requests`. You see the walk as `2 of 6` on the status row while it
  happens.
- **The same host is only ever asked twice when it is the only one there
  is** — a host you pinned yourself, or a model with one host behind it —
  and then codeaf waits exactly as long as that host asked for before trying
  again. That wait is shown as what it is: `waiting for coreweave · 12s`,
  counting down to the moment the host named.
- **Moving to another host costs no wait at all.** A pause between tries is
  what codeaf pays to ask the *same* host again; going somewhere else is a
  different request and it goes out immediately.
- **You never have to switch models to get past this.** When every host
  behind the model is busy at once, codeaf stops waiting and moves your turn to
  the next model instead, because another model is always quicker than a window.
  Work running inside a task has no other model to move to, so that is the one
  place codeaf waits the window out — and it tells you which host it is
  waiting for and how long is left.
- **It counts wherever the message arrived.** A rate limit can come back before
  a single word is written, or in the middle of a reply that had already started
  arriving. The host is stepped around either way. Before 2026-09-10 only the
  first kind counted, so a busy host that said "too many requests" halfway
  through a reply was handed the next request, and the one after that — three
  times in a minute and a half, on one measured turn.
- **A rate limit that names nobody is your whole account**, not one host, and
  nothing is stepped around: every host behind the model is behind the same
  ceiling, so there is nowhere better to go and nothing to leave off the next
  request. codeaf waits **once**, for exactly as long as the answer itself asked
  for — and not at all when it asked for nothing, because a wait nobody named is
  a wait codeaf would be inventing — and then moves to another model, because a
  second host would only
  spend the account's allowance faster, and a different model is not on the same
  allowance at all. With no model left to move to you are handed what the
  host said. On your screen it reads `we are being asked to slow down`.
  (Until 2026-09-11 this kept re-sending the identical request behind a wait that
  doubled each time — 0.7s, 1.4, 2.8, 5.6 and on — for the whole of the time that
  kind of work is given: ninety seconds on a turn, four and a half minutes inside
  a task. Nothing changed between those sends, because there was nothing that
  could change.)
- **And a host that keeps answering after you have stepped around it stops
  the walk.** The name in `(via Io Net)` is the upstream's, and not every
  upstream name is one the router will route around — so when the next request
  says "not that one" and that one answers it anyway, codeaf has learned that
  routing cannot help this request, and it goes on to the wider set and then to
  another model instead of asking a third time. Until 2026-09-11 only a plain
  refusal did this and a rate limit was exempt, which cost one measured task
  eight sends to one host over ninety seconds while six other hosts on the
  same model were answering in under five.

## What all hosts have been ignored means — a refusal from nobody

When the router answers `All
hosts have been ignored`, no host was ever asked: a list had removed the
whole set before the request left — either codeaf's own running list of slow and
unavailable hosts, or the ignored hosts set on your account. Nothing is
taken away from any host on that answer, because a host that never got the
request has said nothing about it — it keeps its place for every other request.
If the host you had pinned is the one nobody could reach, the pin itself is
still stood down and you are told, because a pairing your account cannot use is
one to stop asking for. codeaf stops sending the list that emptied the set for
that model and the next request lands, so this is at most one wasted round trip
in a session rather than every request for five minutes.

The status row keeps up with the refusal and retry: `refused · trying parasail…`
while the answer is moving, and if parasail refuses too the promise is **taken
back** rather than left standing. The row reads `parasail refused`, which is what
actually happened; it never says `trying …` about a request that has already failed.

## When the base refuses a host choice — why a proxy may not honour my pinned lane

Some bases
take no host choice at all — a plain OpenAI-compatible endpoint behind
`CODEAF_BASE_URL`, a proxy that strips the field, a gateway that never heard of
it. codeaf finds out by asking: your pin goes out on a real request, once, and
if that is refused the same request is sent again without it — whether *that*
lands is the answer, so an unrelated bad request never costs you your pin. If
the base will not take the choice, you are told once, in the conversation:

```
api.example.com does not take a host choice; coreweave is not being asked for, and your requests still go out
```

Your work still goes out; only the choice is left off. The settings row says it
too, so `pinned:` never stands as a claim about a request that did not carry it:
`pinned: coreweave (not taken on this base)`.

## Hosts switched off on your account — OpenRouter's ignored-hosts list, and the one refused round trip it costs

Your OpenRouter account can carry its own ignored-hosts list: hosts you
switched off and OpenRouter will not use. codeaf cannot read that list.
Separately, codeaf keeps its own running list of hosts that are slow or have
refused. The two lists can leave no host to ask even though neither list
emptied the set alone. OpenRouter then says `All hosts have been ignored`
before any host is asked.

That sentence is the only thing that tells codeaf which hosts your account
will not reach. Every host codeaf has timed for this model that codeaf was
not itself refusing in that request stops being counted as somewhere the request
can land. On the next request, codeaf drops the host on its own list that is
nearest returning, and the request lands. A switched-off host therefore
costs one refused round trip per model in a session, rather than one on every
request.

If a host later answers, codeaf counts it again immediately. Switching a
host back on needs nothing from you.

The **privacy switch for hosts that may train on paid prompts** is the same
kind of list, and OpenRouter names it: `0 endpoints out of 1 requested are
available matching your guardrail restrictions and data policy … Paid model
training violation (account settings)`. When that answer is about a host
codeaf asked for by name, the host is remembered as out of reach for your
account — for **every model**, for **a day**, and across restarts
(`~/.codeaf/v3/account-exclusions.json`) — so no later request names it and it
costs one refused round trip, once. A strict pin on it is stood down on every
model with the usual `cannot serve this model` line. Nothing about its speed is
written; an answer from it, or pinning it again, takes it back at once.

**Under `routing: simple` that memory never stands your pin down by itself.**
The row promises that what you wrote is what goes on the wire, so the pin is
sent — once — and OpenRouter is left to be the one that says no. You pay the
refused round trip again on the first turn of a new window, and you get the
`cannot serve this model` line in the conversation, in the same breath as the
`@host` coming off the model on the status line. That is the trade: a
sentence you can act on instead of a request that quietly went somewhere else.

## Hosts (lanes) on a custom base URL, a proxy, a mirror, or a self-hosted router — `CODEAF_BASE_URL`

Hosts are not tied to the OpenRouter hostname. Point codeaf at any base with
`CODEAF_BASE_URL` — a proxy in front of the router, a mirror, a router of your
own, the router by its IP — and it **asks that base whether it publishes an
endpoints page**: the first background fetch of a model's sheet is the question.
A base that answers with a page has hosts exactly as the built-in endpoint does,
with the same auto ranking, pins, hedges and status line. Nothing about the
address is inspected; a router is recognised by what it answers.

A base with **no endpoints page at all** — it answers with a not-found page
rather than the router's own error message, the way a plain proxy does — is
remembered as having none for five minutes, then asked again in the background.
A router that has the page but **does not publish that one model** says so in
its own words, about the model, and nothing is remembered about the base. A 500,
a timeout or a rate limit is a bad afternoon rather than an answer.

## Does a proxy honour my pinned host, the lane I pinned — how a custom base answers

**Whether a base honours a host choice is learned the same way**, never from
its address. A base that served an endpoints page takes one. Any other base is
asked once, and only once you have **pinned** something — a pin is the only
thing there is to ask with, so a base nobody pinned anything on is sent no host
opinion at all, exactly as before. Your pin goes out on a real request; if the
base refuses it, codeaf sends that request again once without it, and whether
*that* lands is the answer. A base that refuses the choice, or that answers
without ever naming the host that served, is remembered as not taking one and
**says so** (the refusal section above has the sentence).

**A proxy that forwards to the router but strips the host name out of its
answers is read as not taking your choice**, deliberately. codeaf cannot tell
that proxy from one honouring your pin silently — nothing in the answer says
which host served — so it tells you, sends later requests bare, and the proxy
then routes your model however it likes. Your work still goes out; your pin is
not honoured there, and you know rather than guess.

Neither question costs an extra call of its own, and pointing `CODEAF_BASE_URL`
somewhere else asks the new address afresh about both.

A directly connected service is simpler: it has one host, so there is nothing to choose
between and no host sheet to open. That is not a fault. The service name carried by the
model id is already the whole route.

## How do I stop codeaf choosing the host itself — the simple routing mode, OpenRouter's default routing, and what my pinned host still sends

The `routing` row (`/settings` → **Providers**) has a fourth answer, **`simple`**,
for exactly this. Under it codeaf keeps no opinion of its own about the hosts
behind your model, and sends none:

- **No host pinned** — the request carries no routing preference at all: no sort
  word, no price ceiling, no hosts named or excluded. OpenRouter's own default
  routing picks the host, exactly as it would for a request codeaf had never
  touched. There is no measuring, no second request hedged alongside yours, not
  even the one-token measurement sent while you type, and no takeover when
  answers come back refused.
- **A host pinned** (`/model @deepseek`, or enter on the **host** row) — your
  turn demands exactly that one host: `only`, fallbacks off, and nothing else
  rides along. Your word is the whole request. A pin written `borrow when slow`
  changes nothing here — there is no rescue running for it to borrow. The row is
  named `lane.talk` and that is its scope: the errands that run beside a turn go
  out bare (the next section).

What does not change: the host that answered is still named on the status
line, and the `switch to auto?` question a slow pinned host asks still has
somewhere to send you. A pin the router itself refuses — the host saying it
cannot serve that model at all — is still retired for that model, with the same
one-sentence note, and pinning again puts it straight back on the very next
request.

`simple` is not `off`. `off` stops the measuring, and with nothing measured
there is no host to choose, no sheet of hosts to open and no speed guard.
`simple` leaves the pin standing: the one instruction you gave is the only one
sent. **`simple` is also what the row ships as**, so this is what a home nobody
has changed does; everything else this page describes — the ranking, the
takeover, the rescue, the measuring — is what `latency` and `price` do, one word
away on the same row.

## Does simple routing cover everything, or only my own messages — harness runs, reading a document, looking at an image

The **row** does. It is about **this session**, not one request road, so every
part of a conversation that opens its own connection answers the same word: your
turns, a task node's work, a **subharness** run, the model that reads a document
for you, the one that looks at an image, each member of a `/model` panel. None
of them measures, ranks or hedges under `simple`. The **pin** is narrower — the
next section says how.

That was not always true. Until 2026-09-13 those extra roads were built without
the row and ran `latency` whatever you had written — which was quiet and wrong
in one specific way. A road on `latency` is allowed to stand a pin down on
codeaf's own saved belief that your account cannot reach the host, and that
stand-down covers the whole window: your very next message, on `simple`, doing
nothing wrong, went out with no host demanded while the status line still
read `@deepseek`. The row reaching every road is what closes it.

If you want to check: pin a host, set `routing` to `simple`, and send a
message. Either the answer comes from the host you named, or you get the
`cannot serve this model` sentence and the `@host` disappears from the model
word. There is no third outcome — a bare request under a pin that is still
being drawn is the bug above, and it is worth reporting.

## Does my pinned host apply to the title, the memory reflex and a subharness too, or only to what I type

**Only to the calls you are reading.** Under `routing: simple` the pinned
host is demanded on your own turn, on a task room you are sitting in front
of, and on a headless `codeaf exec` you typed — all three are you, waiting. The
errands that run beside a turn send no host name at all: a conversation's
conversation title, the memory reflex, the question that routes your message, a
hand asking a model about a document, a subharness node. The row is spelled
`lane.talk` and the slot is its whole scope.

That is what one refusal costs. A pin the router refuses is retired **per
host and model** — one refused round trip, once — but an errand runs on a
model of its own, and before 2026-09-13 a single turn bought three of them:
yours, the title's and the reflex's, on three different models, each with its
own 404 and none of them a host you had asked for. One turn, one refusal,
one sentence.

Under `latency` and `price` nothing changes: there is no demand to scope,
because a pin on those roads is drawn against everything the belief knows about
the hosts behind each model.

## Turning host routing off — endpoint routing, lane routing, all the same row

Set routing off (`/settings`, or the `routing` row) and codeaf sends every
request with no opinion at all. It still will not let you wait forever — a
ceiling on how long a silence runs before *something* is said about it is not
steering, it is the promise this surface makes — but it stops choosing hosts
for you, stops sending second requests, and stops spending anything on speed.

**The row has four answers, and the two quiet ones are not the same nothing.** Left alone
it reads `simple`, and codeaf does not pick a host for you at all — it asks for no
fastest host and no cheapest one, sends no preference of its own, and your pin, if you
made one, is the whole request (the section above). Writing another word in the row turns
the choosing on everywhere: `latency` picks the fastest host on every call, background
work included; `price` ranks on price alone on every call, your own turns included, which
is you saying that speed is not worth money anywhere; and `off` is the paragraph above.
Under `latency` and `price` the work you are not watching still weighs speed, at a quarter
of the weight your own turns give it — a task ends when its slowest call ends, and a
host that refuses four requests in five costs five sends for one answer, so its seconds
are never free. That is the split the rest of this page describes.
`price` still measures hosts and still chooses between them. `simple` and `off` stop the choosing.

**`off` does not stop the remembering, and that is deliberate.** codeaf still writes down
which host answered and which one refused, because that is what lets a request that
has just been refused go somewhere else instead of back to the same place — recovery is
not steering, and a build that forgot a refusal the moment you switched routing off would
be a build that could only ever retry into it. Nothing it remembers reaches the wire:
with `off`, every request goes out with no preference on it at all.

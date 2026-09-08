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
that cannot do the job at all — no tool calls when you sent tools, too small an
answer, weights served at a coarser precision than the model is meant to run at,
one the router itself has marked down — and then ranks what is left by the only
thing you actually feel: how long you will be sitting there, plus what it costs,
with the money converted into seconds by how much your waiting is worth.

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

## Pinning one lane yourself — does aforge do use the lane I pinned, and is my pinned provider used from a terminal

You can name the lane yourself. In the model picker, the lanes under a model are
its endpoints; picking one pins it, and every request for that model goes
there until you say otherwise. There is a plain `openrouter` row too, which
means "no opinion from me — let the router balance it".

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
| `slow · trying parasail…` | a machine was late; a second request is out and the first to answer wins |
| `refused · trying parasail…` | a machine said it will not serve this model; the answer has already moved |
| `parasail refused` | the machine that second request went to said no as well |
| `via parasail · rescued` | it worked, for this answer only |
| `coreweave is slow · switch to auto? (y)` | your pinned machine is quiet, and you can end the wait |
| `coreweave cannot serve this model; routing on auto for this model until you pin again` | the machine you pinned said no, so the pin is retired for this model |
| `all lanes slow · still waiting · 12s` | everywhere is slow; nothing to be done but tell you, and how long you have waited |

## When a machine refuses to serve the model

`slow` and `refused` are two different facts and the row says which. **Slow** is
a wait: the machine is answering and taking its time. **Refused** is a machine
saying it will not serve this model at all — the router answers
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
  arrives from wherever the router picks.

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

## Turning lane routing off

Set routing off (`/settings`, or the `routing` row) and aforge sends every
request with no opinion at all. It still will not let you wait forever — a
ceiling on how long a silence runs before *something* is said about it is not
steering, it is the promise this surface makes — but it stops choosing endpoints
for you, stops sending second requests, and stops spending anything on speed.

**The row has three answers, not two, and the third is not off.** Left alone, aforge asks
for the fastest machine on the turns you are waiting through and the cheapest on the work
you are not — the split the rest of this page describes. Writing a word in the row
overrides that everywhere: `latency` asks for the fastest one on every call, background
work included; `price` ranks on price alone on every call, your own turns included, which
is you saying that speed is not worth money anywhere; and `off` is the paragraph above.
`price` still measures machines and still chooses between them. Only `off` stops both.

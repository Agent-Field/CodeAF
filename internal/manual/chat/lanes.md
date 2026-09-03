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
and `aforge run subharness` open no conversation and draw no status line, and they used to
take whatever machine the router happened to hand them. They fetch the same sheet now and
rank it with the same arithmetic — so a headless machine, one that only ever runs work
from a terminal, is choosing between endpoints rather than between none, and every run
leaves a record the next one starts from. Nobody is sitting in front of an errand, so it is
the price ranking above that applies to it.

**A model it has never sent to is not a model it knows nothing about.** The
public sheet names every endpoint serving it, and what aforge has learned about
a *company* — that this one is quick, that one queues — carries across every
model that company serves. So the first request to a brand-new model is still
routed, still has a clock on it, and asks for a fresh sheet in the background
while it goes. You never wait for that fetch.

## Pinning one lane yourself — naming the machine that answers, and whether a pin is honoured

You can name the lane yourself. In the model picker, the lanes under a model are
its endpoints; picking one pins it, and every request for that model goes
there until you say otherwise. There is a plain `openrouter` row too, which
means "no opinion from me — let the router balance it".

A pin is an instruction, so aforge keeps it. It does not quietly send your work
somewhere else because it thinks it knows better.

**There is exactly one thing that ends a pin without you: the machine you named
saying it will not serve that model at all.** That is not a wait and not a bad
afternoon — the router answers `no endpoints found … your request's
provider.only preference permits only: coreweave`, which is the wire saying this
machine and this model do not go together. Asking again would buy the same 404,
so aforge stops asking, and says so once, in the conversation, at the moment it
happens:

```
coreweave cannot serve this model; routing on auto for this model until you pin again
```

What that means, exactly:

- **for that model**, every later request in this run goes out with no machine
  demanded at all — routed the way `auto` routes;
- **the request that collected the refusal is widened and sent again**, once, so
  your answer still arrives. If that one is refused too the turn ends and the
  refusal is named;
- **your settings row is not touched.** It still reads `pinned: coreweave` —
  nothing on disk changed;
- **every other model still goes to that machine.** The refusal was about one
  pairing;
- **pinning again puts it straight back**, on the very next request;
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
`no endpoints found … your request's provider.only preference permits only:
coreweave`, which means the machine aforge asked for is not in the set that
serves this model right now.

A refusal is final for that machine, immediately:

- the next request leaves at once, for a different machine, and does not name
  the refused one;
- that machine is taken out of the set aforge will choose from for this model,
  so it is not picked again later in the session;
- if there is nowhere left to move to, the request itself is widened — the
  demand for one machine is the first thing dropped — and the answer usually
  arrives from wherever the router picks.

The row keeps up with all of it. `refused · trying parasail…` while the answer
is moving, and if parasail refuses too the promise is **taken back** rather than
left standing: the row reads `parasail refused`, which is what actually
happened. A row still saying `trying …` about a request that has already failed
is the one thing it will not do.

## Lanes on a custom base URL, a proxy, a mirror, or a self-hosted router — `AFORGE_BASE_URL`

Lanes are not tied to the OpenRouter hostname. Point aforge at any base with
`AFORGE_BASE_URL` — a proxy in front of the router, a mirror, a router of your
own, or the router reached by its IP address — and it **asks that base whether
it publishes an endpoints page**: the first fetch of a model's sheet, made in the
background, is the question. A base that answers with a page has lanes exactly
as the built-in endpoint does, with the same auto ranking, pins, hedges and
status line. Nothing about the address is inspected; a router is recognised by
what it answers, not by where it lives.

A base that **has no endpoints page at all** — it answers the ask with a
not-found page rather than the router's own error message, the way a plain
proxy or a mirror of the completions route does — is remembered as having none
for five minutes. aforge then sends every request with no lane opinion, as it
would to any single endpoint, and it does not keep asking: that base is asked
again once every five minutes, in the background, and never in front of a
request. A router that has the page but simply **does not publish that one
model** — it answers not found in its own words, about the model — is not
treated that way: the model has no sheet this round, nothing is remembered
about the base, and the next model is fetched at once. An error that is not a
not-found — a 500, a timeout, a rate limit — is a bad afternoon and not an
answer, so the base is simply asked again on the next beat.

Two things worth knowing. The check costs nothing extra: it is the sheet fetch
aforge was going to make anyway, so the built-in endpoint pays no additional
request. And a base that has said it has no page at all is believed for those
five minutes even if you ask about a different model — that answer is about the
address, not the model. Only the answer about one model is about the model.

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

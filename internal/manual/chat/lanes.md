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

## Auto, and which lanes it is choosing between

Left alone, aforge is on **auto**. Before each request it drops every endpoint
that cannot do the job at all — no tool calls when you sent tools, too small an
answer, weights served at a coarser precision than the model is meant to run at,
one the router itself has marked down — and then ranks what is left by the only
thing you actually feel: how long you will be sitting there, plus what it costs,
with the money converted into seconds by how much your waiting is worth.

Nothing is waiting on this when nobody is waiting on you. A background errand is
ranked on price, because a second saved for a machine is a second nobody spends.

**A model it has never sent to is not a model it knows nothing about.** The
public sheet names every endpoint serving it, and what aforge has learned about
a *company* — that this one is quick, that one queues — carries across every
model that company serves. So the first request to a brand-new model is still
routed, still has a clock on it, and asks for a fresh sheet in the background
while it goes. You never wait for that fetch.

## Pinning one lane yourself

You can name the lane yourself. In the model picker, the lanes under a model are
its endpoints; picking one pins it, and every request for that model goes
there until you say otherwise. There is a plain `openrouter` row too, which
means "no opinion from me — let the router balance it".

A pin is an instruction, so aforge keeps it. It does not quietly send your work
somewhere else because it thinks it knows better.

## When a pinned lane goes quiet

It still has to do something about a wait, and what it does is **ask you**:

```
coreweave is slow · switch to auto? (y)
```

Press **y** and the answer is fetched from somewhere else, at once, from the
endpoint that was already ranked second — no new decision made at the worst
possible moment. The question is asked **once** per answer, and it disappears
the moment your answer starts arriving, because by then it is moot.

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

## When every lane is slow

Sometimes there is nowhere better to go — everything serving that model is
believed slow at once, which happens when a whole region is having a bad
afternoon. Switching would buy nothing, so aforge says the true thing instead:

```
all lanes slow · still waiting
```

That line means the wait is real, it is not a stall this build can end, and
nothing is being spent trying. It is the one honest thing left to say, and
saying nothing was the old behaviour.

## What the status line is telling you

| what you see | what happened |
| --- | --- |
| `via cloudflare · 0.6s · 61 t/s` | an ordinary answer, and who wrote it |
| `slow · trying parasail…` | a second request is out; the first one to answer wins |
| `via parasail · rescued` | it worked, for this answer only |
| `coreweave is slow · switch to auto? (y)` | your pinned machine is quiet, and you can end the wait |
| `all lanes slow · still waiting` | everywhere is slow; nothing to be done but tell you |

## Turning lane routing off

Set routing off (`/settings`, or the `routing` row) and aforge sends every
request with no opinion at all. It still will not let you wait forever — a
ceiling on how long a silence runs before *something* is said about it is not
steering, it is the promise this surface makes — but it stops choosing endpoints
for you, stops sending second requests, and stops spending anything on speed.

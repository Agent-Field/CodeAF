# Connections: the accounts you already have

## What a connection is

Aforge can reach accounts you hold somewhere else — Google first — so that
"what did Priya say about the invoice" and "am I free Thursday afternoon" are
questions it answers by looking, instead of questions it asks you to go and
answer yourself.

A connection is **read-only and per-account**. Connecting Google lets aforge
search and open your mail and look at your calendar. It does not let it send
anything, delete anything, or move a meeting.

## Connecting one

Two ways in, and they are the same connection:

- **You start it.** `/connect` lists what can be connected and what already is.
  Pick one and a browser page opens for you to sign in.
- **Aforge asks.** When the work in front of it needs an account you have not
  connected — you asked about a mail thread it cannot see — it stops and asks:

> Connect Google? It will be able to search and read your mail, and look at
> your calendar.
>   1 connect  ·  2 not now

Nothing is connected until you answer yes, and **nothing happening is a no**:
the question waits five minutes and then answers itself with "not now". Say no
and aforge does the rest of the work without that account, and tells you plainly
what it could not reach.

When you say yes, a browser page opens. Finish there and you are connected:

> Connected as you@example.com

## Where the tools show up

The moment an account is connected, aforge picks up the tools that come with it
— your mail, your calendar — and they are in its hands **from its next reply
on**, not mid-sentence. So the shape of a first connection is: it asks, you say
yes, it says the account is connected, and then it goes and reads. That second
step is not hesitation; it is when it actually has the hands.

Connecting once is connecting for good. Every conversation on this machine, from
then on, starts with the account already in place — you are never asked twice.

## Seeing and undoing

`/connect` is also the list. It shows each account, whether it is connected, and
the address it is connected as, and it is where you disconnect one. Disconnecting
takes effect immediately: aforge forgets the account on this machine, and the
next conversation is offered the chance to connect it again like the first one
was.

## What it takes to have this at all

Connections need one thing from you first, in `⚙` settings: a **google app id**
and its **google app secret**, from your own Google account. Aforge does not
ship one, deliberately — an application id baked into the binary would be an id
every copy shares, so one person's mistake would be everybody's.

Without those two rows, connections are simply not there: no `/connect`, and
aforge never offers to reach an account it has no way to ask about. That is the
same rule it follows everywhere — a capability it cannot deliver is one it never
mentions.

## Tasks and connections

Work you hand off with a task inherits your connections: a task briefed to go
through last week's mail can read the mail. What it cannot do is connect
something new — there is nobody in a task's worktree to ask — so it says so in
its report rather than stopping to wait for an answer that could never come.

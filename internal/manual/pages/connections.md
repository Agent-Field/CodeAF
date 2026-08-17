# Connections: the accounts you already have

## What a connection is

Aforge can reach accounts you hold somewhere else — Google first — so that
"what did Priya say about the invoice" and "am I free Thursday afternoon" are
questions it answers by looking, instead of questions it asks you to go and
answer yourself.

A connection is **per-account, and it acts as well as reads**. Connecting Google
lets aforge search and open your mail and look at your calendar, and it lets it
send a message from your address and put an event on your calendar. It cannot
empty your mailbox, and it cannot make or delete a calendar.

## Two kinds, and you can tell them apart by what they ask you for

- **Google opens a browser.** You sign in on Google's own page, Google shows you
  exactly what is being asked for, and aforge keeps the sign-in fresh for you.
- **A few hundred others want a key.** Stripe, Freshdesk, Brevo, Mailgun,
  Airtable's neighbours — the systems you already pay for and already hold a key
  for. There is no page to open: you paste the key once and that is the whole of
  it. Ask "what can you connect" and aforge lists them; give it a word to search
  by if the list is long.

A handful of them live at an address with your own name in it —
`yourcompany.freshdesk.com`. Those ask for **two things in one line, with a
space between them**: the part that is yours, then the key.

> `yourcompany sk-live-1234`

The question says which part it wants first, in the words that service uses for
it — a domain, a site name, a workspace.

An account connected with a key shows as connected and **nothing else**. It does
not say who you are, because a key does not carry a name and aforge will not
invent one.

Anything that leaves — a message, an invitation — **stops and asks you first**,
with the recipient and the subject in the question, and it goes only when you
say so. Reading never asks.

## Connecting one

Two ways in, and they are the same connection:

- **You start it.** `/connect` lists what can be connected and what already is.
  Pick one and either a browser page opens for you to sign in, or a line opens
  for you to paste the key into.
- **Aforge asks.** When the work in front of it needs an account you have not
  connected — you asked about a mail thread it cannot see — it stops and asks:

> Connect Google? It will be able to read and send your mail, and read and
> manage your calendar.
>   1 connect  ·  2 not now

Nothing is connected until you answer yes, and **nothing happening is a no**:
the question waits five minutes and then answers itself with "not now". Say no
and aforge does the rest of the work without that account, and tells you plainly
what it could not reach.

When you say yes, a browser page opens. Finish there and you are connected:

> Connected as you@example.com

## Where the tools show up

The moment an account is connected, aforge picks up the tools that come with it
— your mail, your calendar, or for a key account one tool that calls it
directly — and they are in its hands **from its next reply on**, not
mid-sentence. So the shape of a first connection is: it asks, you say
yes, it says the account is connected, and then it goes and reads. That second
step is not hesitation; it is when it actually has the hands.

Connecting once is connecting for good. Every conversation on this machine, from
then on, starts with the account already in place — you are never asked twice.

The exception is a version of aforge that can do more with an account than the
one you connected under. If you signed in when it could only read your mail, the
first time it needs to send one it asks you to sign in again, and Google shows
you exactly what is being asked for. It is the same question as the first time,
and answering it once is enough.

## Seeing and undoing

`/connect` is also the list. It shows each account, whether it is connected, and
the address it is connected as, and it is where you disconnect one. Disconnecting
takes effect immediately: aforge forgets the account on this machine, and the
next conversation is offered the chance to connect it again like the first one
was.

## What a key account can do

One tool, and it is the account itself: aforge makes the calls that service's
own documentation describes. Reading is free to try. **Anything that changes
something — creating, updating, deleting — stops and asks you first**, with the
service, what it is about to do and where, in the question. That is the same
rule that stands over sending a message, for the same reason: it happens in your
name, in a system other people can see, and there is no undo.

What aforge does not have is a hand-written tool per service. There are hundreds
of them and no two agree on what a contact is, so it reads their documentation
the way you would rather than pretending to know in advance. Expect it to say
what it is about to call.

## What it takes to have this at all

The **Google** connection needs one thing from you first, in `⚙` settings: a
**google app id** and its **google app secret**, from your own Google account.
Aforge does not ship one, deliberately — an application id baked into the binary
would be an id every copy shares, so one person's mistake would be everybody's.
Without those two rows Google is simply not offered, which is the same rule
aforge follows everywhere: a capability it cannot deliver is one it never
mentions.

The key accounts need nothing set up. Your key is the whole of what it takes, so
they are on the list from the first run.

## Tasks and connections

Work you hand off with a task inherits your connections: a task briefed to go
through last week's mail can read the mail. What it cannot do is connect
something new — there is nobody in a task's worktree to ask — so it says so in
its report rather than stopping to wait for an answer that could never come.

Nor can it send anything. A message and an invitation go out in your name and
are asked about first, and there is nobody standing over a task to ask, so a
task that wanted to send one comes back with the message it drafted instead. You
send it, from a conversation, after reading it.

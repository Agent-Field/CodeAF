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

## Or don't paste the key at all

Wherever a box asks you for a key, **the name of an environment variable is an
answer too**:

> `$STRIPE_KEY`

Write it with the dollar — `$STRIPE_KEY` or `${STRIPE_KEY}` — and aforge stores
**the name**, not the key. Every call reads the variable again, so:

- The key never enters aforge's file. If your keys already live in your shell,
  your `.envrc` or a secret manager, they stay there.
- **Rotating is rotating once.** Change it where it is set and the next call
  uses the new one. There is nothing here to update.
- If the variable is not set when aforge reaches for it, it says so and names
  it — `Stripe reads its key from $STRIPE_KEY, and nothing is set there` — rather
  than making a call that fails somewhere else for a reason you cannot see.

An account connected this way says so where an account address would be:

> ✓ Stripe                                          from $STRIPE_KEY

That is the one thing a key connection ever says about itself. The name of a
variable is a fact about your machine; the key is not, and it is never shown.

Everything else about a key is unchanged: a value that is not exactly `$NAME` or
`${NAME}` is treated as the key itself, so a real key that happens to start with
a dollar still works.

## Where the key actually is

The box that asks for a key also says **where to get one**, while it is open:

> · Chargebee
>     Give the domain and then the key, one space between them.
>   › paste your Chargebee key
>     find it at apidocs.chargebee.com

It is the vendor's own page, and where the vendor names none, aforge's own note
of where people find it. It is shown only while the box is open — not on every
row of a two-hundred-row list, and not after the account is connected, because
by then you have found it.

Anything that leaves — a message, an invitation — **stops and asks you first**,
with the recipient and the subject in the question, and it goes only when you
say so. Reading never asks. Both of those are only where the dials start: what
each account may be used for is yours to set, one sentence at a time, and the
next section is how.

## Connecting one

Three ways in, and they are the same connection:

- **You start it.** `/connect` lists what can be connected and what already is.
  Pick one and either a browser page opens for you to sign in, or a line opens
  for you to paste the key into.
- **From the settings page.** `⚙` settings under **Connections** is the same
  list at rest, and it connects too: enter on a service opens the sign-in, or
  opens the key box **on the row itself**. It is the same box, and the page
  stays where it was — the account you just connected gains its tick and opens
  on what it may do, under your cursor.
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

## What each account may be used for

A connection is one grant — the whole mailbox, the whole calendar — because that
is the only shape a sign-in has. What you actually want to decide is finer than
that, and it is not technical: reading your mail is not sending it, and a meeting
that lands on somebody else's calendar is not a meeting you read about.

So each account is a handful of **sentences**, in `⚙` settings under
**Connections**, and each one carries one of three words:

> ✓ Google                                     jane@example.com
>     read your mail                            yes
>     send mail as you                          ask first
>     read your calendar                        yes
>     put things on your calendar               ask first
>     disconnect

- **yes** — this runs without asking. It is worth exactly what your own approval
  rule for that tool would be worth, because you wrote it about that sentence.
- **ask first** — you are asked before every one, with what is about to happen in
  the question.
- **off** — **the hand is not there**. Not refused when it is reached for: the
  tool never arrives in the conversation at all, and aforge is never told the
  account can do that. Nothing it plans will be built on it, and you are not
  asked a question about something you have already answered.

Enter walks the three words and the answer is **saved the moment you change it**.
It applies to conversations that are already open: turn sending off while aforge
is drafting a message and the send stops there, with the conversation told
plainly that you turned it off rather than that something broke.

Reading is `yes` to begin with and anything that acts in your name is `ask first`,
which is exactly how aforge behaved before these rows existed. Nothing is `off`
until you say so.

When a question stops you mid-conversation, answering **"always"** is the same
sentence said in the other room: that capability is set to `yes`, and you will
find it that way on the settings page. One vocabulary, one answer, two places to
give it.

## Seeing and undoing

Both lists — `/connect` and the settings page — show each account, whether it is
connected, and the address it is connected as, and both are where you disconnect
one. What you have connected is at the top, flat; everything else is under **the
word it is filed by** — billing, support, crm, calls & meetings — because a few
hundred services is a list you search rather than one you read. Typing narrows
it, and it narrows on the category as well as on the name: `billing` finds
Stripe, Chargebee and Recurly, none of which contain the word.

On the settings page the whole thing is meant to be **read at rest**:

> ✓ Google                                     jane@example.com
>     yes: read your mail, read your calendar · ask first: send mail as you
>
> ✓ Stripe                                          from $STRIPE_KEY
>     yes: read what is in this account · ask first: act in your name
>
> billing
> · Chargebee                                                    key
> · Recurly                                                      key

Each account you hold is a block with a line of air over it, and the dim line
under its name is **what it may do, without opening it** — the same three words
the rows inside carry, so four accounts can be audited by reading rather than by
expanding. Under them, what you could connect: one row each, the word saying
what pressing enter will ask you for (`key` or `sign in`), and the sentence
about what a service is for shown **only under the row your cursor is on**. Two
hundred sentences at once is not a catalog, it is a wall.

Disconnecting takes effect immediately: aforge forgets the account on this
machine, and the next conversation is offered the chance to connect it again like
the first one was.

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

## Some accounts bring their own tools

Airtable, Atlassian, Buildkite, Calendly, Canva, CircleCI, ClickUp, Cloudflare,
GitLab, Grafana, Heroku, Hugging Face, Klaviyo, LaunchDarkly, Linear, Miro,
Neon, Netlify, Notion, PayPal, PostHog, Railway, Sanity, Sentry, Supabase and
Todoist sign in through a browser the way Google does, and then they do one
thing the others do not: **they say for themselves what they can do**. Aforge
asks the account what it brings the moment it picks it up, and what comes back —
search this, open that, file the other — is what it holds for the rest of the
conversation. None of that list is written into aforge, so an account that
gains a tool next month is an account aforge picks that tool up from, with
nothing to change here.

| Service | Address | What it brings |
| --- | --- | --- |
| Airtable | `https://mcp.airtable.com/mcp` | your bases, tables and records |
| Atlassian | `https://mcp.atlassian.com/v1/mcp/authv2` | Jira issues and Confluence pages |
| Buildkite | `https://mcp.buildkite.com/mcp` | your pipelines, builds and logs |
| Calendly | `https://mcp.calendly.com` | your event types, availability and scheduled meetings |
| Canva | `https://mcp.canva.com/mcp` | your designs, folders and brand kits |
| CircleCI | `https://mcp.circleci.com/v1/mcp` | your pipelines, workflows and build logs |
| ClickUp | `https://mcp.clickup.com/mcp` | your tasks, lists and docs |
| Cloudflare | `https://mcp.cloudflare.com/mcp` | your zones, DNS records and Workers |
| GitLab | `https://gitlab.com/api/v4/mcp` | your projects, issues and merge requests |
| Grafana | `https://mcp.grafana.com/mcp` | your dashboards, queries and alerts |
| Heroku | `https://mcp.heroku.com/mcp` | your apps, dynos and add-ons |
| Hugging Face | `https://huggingface.co/mcp` | your models, datasets and Spaces |
| Klaviyo | `https://mcp.klaviyo.com/mcp` | your campaigns, flows and audiences |
| LaunchDarkly | `https://mcp.launchdarkly.com/mcp/launchdarkly` | your feature flags, segments and environments |
| Linear | `https://mcp.linear.app/mcp` | your issues, projects and cycles |
| Miro | `https://mcp.miro.com` | your boards, frames and notes |
| Neon | `https://mcp.neon.tech/mcp` | your projects, branches and queries |
| Netlify | `https://netlify-mcp.netlify.app/mcp` | your sites, deploys and domains |
| Notion | `https://mcp.notion.com/mcp` | your pages, databases and search |
| PayPal | `https://mcp.paypal.com/http` | your payments, invoices and payouts |
| PostHog | `https://mcp.posthog.com/mcp` | your events, insights and feature flags |
| Railway | `https://mcp.railway.com/` | your projects, services and deployments |
| Sanity | `https://mcp.sanity.io` | your content, datasets and schemas |
| Sentry | `https://mcp.sentry.dev/mcp` | your issues, events and releases |
| Supabase | `https://mcp.supabase.com/mcp` | your projects, tables and queries |
| Todoist | `https://ai.todoist.net/mcp` | your tasks, projects and labels |

**All 26 work with zero setup here.** GitLab may need its admin to turn its AI
features on first, and an Airtable enterprise account may need its admin to
allow the connection first.

Two things follow from a list nobody here wrote:

- **It is read fresh every time.** A tool that was there yesterday and is gone
  today is simply not picked up, and one that appeared overnight is. Nothing is
  remembered between conversations, so nothing can go stale.
- **A few of them serve dozens.** Carrying sixty tools would cost you on every
  turn of every conversation, so past about thirty aforge picks up none of them,
  reads the list, and asks again for the few the work needs. You will see it name
  a handful and carry on.

The rows in settings are the same two sentences a key account has: reading is
`yes`, anything that changes something in the account is `ask first`, and either
can be turned off. Off means the tools that do it never arrive at all.

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

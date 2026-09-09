# Connected accounts

aforge can act on accounts you already hold — your mail, your calendar, your Notion
pages, a billing service you have a key for. This page covers what connecting one
gives aforge, how to connect, what you can turn on and off per account, and where
the keys are kept.

## What a connected account is

A connected account is a service aforge holds a credential for. Connecting one does
two things: it stores the credential in your profile directory, and it puts that
account's tools on aforge's toolbelt so the model can call them.

What arrives depends on the account:

- **Google** brings five tools: `gmail_search`, `gmail_read`, `gmail_send`,
  `calendar_list`, `calendar_create`.
- **Slack** brings four tools: `slack_search`, `slack_read_thread`,
  `slack_list_channels`, `slack_send`.
- **A key account** brings exactly one tool, `<id>_request` — `stripe_request`, for
  example — taking `method` (get, post, put, patch, delete; default get), `path`,
  `query` and `body`. The path is always relative to the service's own address. An
  absolute address is refused with `the path is relative to the service's own
  address, not a whole address of its own`, because an absolute one would send your
  key to a host nobody vouched for.
- **A tool server** brings whatever tools it serves.

Tools are added to the belt and never taken off again, so the prompt cache behind
them survives. Answers from these tools are cut at 12 KB and the cut is announced;
response bodies are read up to 8 MiB.

An account with no tools in this build answers `<Name> is connected, and this build
has no tools for it. Do the work without it and say so plainly.`

## How many services can be connected

**129 services register in this build: 99 are connected with a pasted key, and 30 are
connected in a browser.**

The 30 browser ones are **Google** (Gmail and Calendar), **Slack**, and the 28 tool servers
**Airtable, Atlassian, Buildkite, Calendly, Canva, CircleCI, ClickUp, Cloudflare,
Datadog, GitLab, Grafana, Heroku, Hugging Face, Klaviyo, LaunchDarkly, Linear,
Miro, Neon, Netlify, Notion, PayPal, PostHog, Postman, Railway, Sanity, Sentry,
Supabase** and **Todoist**.
The 99 key services come from the bundled connectors catalog and are filed under
eleven categories: `crm`, `support`, `billing`, `marketing`, `sales & outreach`,
`calls & meetings`, `analytics`, `hr & recruiting`, `developer`, `productivity`,
`communication`. A service with none of these is shown under `other`.

Every menu is ordered by name, case-insensitive — never registration order.

Google and Slack both ship with the application their browser sign-in needs, so both
are listed on a fresh install. The `google_oauth_client` and `slack_oauth_client`
settings replace those shipped applications for somebody who wants their own. A
browser service with no application configured is not listed at all: no greyed row,
no explanation. Key services and tool servers need nothing configured and are always
listed.

## The two ways to sign in: Google and Slack in a browser, or a pasted key

**In a browser.** aforge starts a loopback listener and gives you an address to
visit; it never opens a browser for you and never logs a key. The loopback addresses
tried, in order, are `127.0.0.1:8765`, `127.0.0.1:18765`, then any free port. When
the sign-in lands, the tab shows a page reading **"Connected."** and **"You can close
this tab."**

Google asks for exactly two permissions:
`https://www.googleapis.com/auth/gmail.modify` and
`https://www.googleapis.com/auth/calendar.events` — read and write mail, but not
permanent deletion, and read and write calendar events, not the calendars themselves.
Google's consent screen is forced every time, because Google only issues a refresh
key on a fresh grant. A connection short of a permission is not a connection: you are
put back through the sign-in rather than left to fail at the far end.

Slack asks for twelve user permissions: `search:read`; `channels:read`, `groups:read`,
`im:read`, `mpim:read`; `channels:history`, `groups:history`, `im:history`,
`mpim:history`; `users:read`, `users:read.email`; and `chat:write`. It signs you in as
**you**, never as a bot. Slack sends the browser back through
`https://agentfield.ai/connect/slack/8765` or
`https://agentfield.ai/connect/slack/18765`; each address only forwards the answer to
the matching listener on this machine, so Slack tries only those two fixed addresses
and never a free port. A workspace that requires admin approval shows Slack's own
request screen and sends the request to the admin, and the browser does not come back
until the admin says yes. When the model asked (`use_service`), aforge gives up after
five minutes and says the sign-in did not complete. From `/connect` there is no clock:
the card keeps waiting until Slack sends the browser back, until the conversation is
replaced, or until aforge is closed. Either way nothing is connected until the browser
comes back. Slack limits channel-history reads for applications
outside its Marketplace to one thread read a minute, with at most 15 messages in that
read; searching, listing channels and posting are not under that limit.

**The 28 tool servers listed below need nothing registered first.** aforge introduces
itself to the service at connect time and is issued an identity on the spot, then
makes the same browser trip.

Some accounts ask one thing before they open. **Datadog asks which Datadog site your
account is on**, shows the seven commercial sites it accepts, and refuses anything
else before making a connection. That answer is kept beside the keys so later calls
and renewals return to the same site.

**With a key.** Nothing opens and nothing renews; the key is as good as the day it
was made. Most services want one key and nothing else. A few whose address contains
your own workspace want the workspace, one space, then the key — the service's own
line says so. Where the catalog names a cheap health check, the key is proved before
anything is stored, and a refusal fails with the far end's own words and writes
nothing.

Trying a browser sign-in on a key service errors with
`<Name> is connected with a key, not in a browser`.

## Naming an environment variable instead of pasting a key

A pasted value of the form `$NAME` or `${NAME}` is stored as a **reference**, not as a
key. The environment variable is read again on every use, so a key you rotate in your
environment is rotated here with no further step. The variable's name is kept in its
own field and never guessed from the value. A screen shows `from $STRIPE_KEY`; a
pasted key shows nothing at all.

Only exactly `$NAME` or `${NAME}`, in upper case, counts. `$name`, `${NAME` and
`$NAME extra` are all read as literal keys.

If you name a variable that is not set, aforge refuses before writing anything:
`<Name> reads its key from $NAME, and nothing is set there`.

A key of the wrong shape refuses with `<Name> needs one key and nothing else` or
`<Name> needs your <label> and then the key, one space between them`.

A key account never reports an account address — a key says nothing about whose key
it is.

## Opening the connect panel with /connect

`/connect` (alias `/connections`) opens the list. Its menu line reads
`your connected accounts · connect another`.

Connected accounts come first, flat and with no heading; everything else is grouped
under one dim lowercase category word, alphabetical, with `other` last. A tick marks
an account you hold, a dim dot marks one you do not. The right-hand tail carries one
fact: the account address when held, otherwise `key` or `sign in` on a long list, or
the service's blurb on a short one.

Past 10 available services the box under the list becomes a filter and the list
narrows as you type; the placeholder reads `filter · ↑↓ · enter connect · esc close`.
Under ten there is no filter box. The filter matches name and category, so typing
`billing` reaches Stripe, Chargebee and Recurly. Accounts you already hold stay
pinned first.

| Key | What it does |
| --- | --- |
| `enter` on a browser row | closes the panel and starts the sign-in |
| `enter` on a key row | opens the masked key box in place, panel still up |
| `enter` on a connected row | arms it; a second press disconnects |
| `esc` | un-arms, then clears the filter, then closes |

Headings and gaps cannot be pressed.

With no accounts layer the command says `connections are unavailable here`; with
nothing to offer, `there is nothing to connect yet`; a filter that matches nothing
says `nothing matches`.

## What each account may be used for: yes, ask first, off

Each connected account has capabilities written in plain sentences rather than tool
names — Google's four are `read your mail`, `send mail as you`, `read your calendar`,
and `put things on your calendar and invite people`; Slack's two are `read your Slack`
and `send Slack messages as you`. Every key account and every tool server has the same
pair: `read what is in this account` and `act in this account in your name`.

Reach them with `/settings` → the **Connections** tab → `enter` on a connected account
→ `enter` on a capability row, which cycles **yes → ask first → off → yes**. A change
is saved the moment you make it: no save step, no pending state.

- **yes** — your named allow. It is worth exactly what a per-tool allow rule is worth,
  including lifting the floor that would otherwise ask about a call acting in your
  name.
- **ask first** — a floor of its own, on any capability. It turns an allow into a
  question. Somebody who asks to be asked is asked.
- **off** — the capability is taken away entirely.

Neither `yes` nor `ask first` ever touches a refusal: a rule that refuses outright
stays a refusal. The state is read fresh at the moment of every call, so a word you
change mid-conversation takes effect on the next call.

**Defaults, when you have set nothing:** a capability that only looks defaults to
**yes**; a capability that acts in your name defaults to **ask**. Nothing is off by
default.

**Where it is stored:** `connections.json` in your profile directory, as
`{service: {capability: "yes"|"ask"|"off"}}`. Only what you actually said is written —
setting a control back to its default forgets the row rather than writing the default
down, and forgetting the last answer deletes the file. A damaged or absent file reads
as all defaults, never as an error.

## What "off" does — the tool is not there at all

Off is not a refusal at the gate. It is absence. The tool is left **off the belt
entirely**, the account's line in the `services` listing stops advertising it, and the
conversation never learns the capability exists. The reason is plain: a refused call
costs a turn, teaches the model to try again in different words, and puts a question in
front of somebody who already answered it.

Because the capability answers live in your profile directory, aforge's toolbelt
differs from profile to profile. The same build, opened under a different profile,
holds a different set of tools.

An account whose capabilities are all off answers `<Name> is connected, and the person
has turned off everything it can do. Do the work without it and say so plainly; asking
again will not change their answer.`

A key account's one raw call is armed while either half is on, and its description
names only the half that is left — for example, "get reads, and that is all this
account may be used for: the person has turned off changing anything in it, so post,
put, patch and delete will not run."

You can turn something off mid-conversation while the model already holds the tool,
because the belt cannot un-arm without invalidating the prompt cache. A second
safeguard in the gate catches that, checked first. The model then reads:
`The person has turned off "<phrase>" for their <Name> account. Nothing was done. Do
the work without it and say so plainly; calling again, or calling it another way, will
not change their answer.`

## The services and use_service tools

Two tools are always on the belt when an accounts layer exists.

- **`services`** — lists what can be connected and what is connected already, with the
  address each is held as. Connected ones are written out in full; the rest are a block
  of ids only. It takes an optional `filter` argument.
- **`use_service`** — picks up one account's tools. An optional `tools` argument names a
  subset.

**The tools `use_service` picks up arrive in the tool list on the next request, not
the one it was called on — and that next request is still part of the same turn.** So a
request that needs an account takes a beat: aforge picks up the account, then uses it,
without waiting for you to say anything else.

If `use_service` names an account you have not connected, aforge raises a quiet
question: `? <Name>` / `  <product> wants to connect your <Name> account` /
`  [enter] connect · [esc] not now`. There is deliberately no "always" — an account is
connected once and stays connected. Declining is "not now": nothing is remembered,
nothing is written to the transcript either way, and the next time the account is
needed you are asked again.

A yes on a key service opens a masked box reading `paste your <Name> key`. The key is
never drawn: one bullet per character plus a dim count, so you can tell a whole paste
arrived. An empty box is a decline.

The question waits **5 minutes**, and silence is a no. When nobody is watching the
conversation, the tool answers instead: `Connecting <Name> needs the person to say
yes, and nobody is watching this conversation. Do what you can without their <Name>
account and say plainly that you could not reach it.`

## Connecting while the conversation is idle

An account you connect from `/connect` or the settings sheet tells the running session
straight away. Its tools go on the belt and one line is queued as an ambient note,
read whenever you next say something, rather than starting a paid turn:

```
<Name> is connected as <account>. Its tools are in your tool list from this turn on.
```

If there is nothing armed for it, the line instead ends `…, and there is nothing it
can be used for in this conversation.`

## MCP: accounts that bring their own tools

Some services run a server whose whole job is to hand a program a list of tools and
run one when asked. aforge fetches that list per account at the moment the account is
picked up. You connect "Notion" — no protocol, server or grant is ever named in front
of you. A tool server appears in `/connect` as a browser connection like any other, is
connected with the same sign-in, forgotten with the same disconnect, and its keys live
in the same store file.

Twenty-eight ship, each at the address on the vendor's own page:

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
| Datadog | `https://mcp.<site>/v1/mcp` | your metrics, logs and monitors |
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
| Postman | `https://mcp.postman.com/minimal` | your collections, specs and environments |
| Railway | `https://mcp.railway.com/` | your projects, services and deployments |
| Sanity | `https://mcp.sanity.io` | your content, datasets and schemas |
| Sentry | `https://mcp.sentry.dev/mcp` | your issues, events and releases |
| Supabase | `https://mcp.supabase.com/mcp` | your projects, tables and queries |
| Todoist | `https://ai.todoist.net/mcp` | your tasks, projects and labels |

GitLab's own line adds: "Your GitLab admin may have to turn its AI features on first."
Airtable's adds: "An enterprise admin may have to allow it first."
Postman's adds: "Postman's EU workspaces cannot be reached this way." — Postman's EU
address signs in with a key and nothing else, so it is deliberately not shipped.

**All 28 work with zero registration.** aforge introduces itself to the service at
connect time and is issued an identity on the spot, kept in `toolservers.json`. Keys
minted for one service cannot be spent at another.

**GitHub is deliberately not shipped** — its sign-in does not let a program introduce
itself, and its maintainers say that will not change, so it can return only with an
application registered by hand in a later wave. Slack now signs in through a browser
with the application aforge ships; the Slack paragraph above describes that trip. Any
service whose sign-in refuses an introduction cannot be connected this way at all, and
aforge says so in one sentence the moment you ask.

An identity is reused only when the service address, the issuer, the resource and the
loopback port all still match. The registration file survives a disconnect, so
reconnecting is one browser trip and not a second registration.

## How MCP tool names are built, and how many can be armed

A served tool is named **`<account id>_<the service's own name, folded>`**. Notion's
`Create Page` becomes `notion_create_page`, and two accounts both serving `search`
become `notion_search` and `linear_search`. Folding is lower case, letters, digits and
single underscores; everything else reads as a word break, so `Create Page`,
`create-page` and `create.page` all fold to `create_page`. The fold loses information
on purpose, so the service's own spelling is kept beside the belt name and the call is
always made with the service's spelling. Names are cut at **64 characters**.

Two names that fold to one are one name here: the first stands, the second is left off
and named in the reply. A tool whose argument schema cannot be read is left off and
named too. Neither can fail the whole arming — nine good tools out of ten still arm.

**The ceiling is 32 tools per account.** Under it, everything the account serves
arrives. Over it, **nothing does** — the reply names them all and tells the model to
call `use_service` again with `tools` naming the few the work needs. Silent trimming
was rejected outright: the model would plan around a list it was never told was cut.

The list a service gives is fetched once per run and remembered for the life of the
process, so a tool newly added at the service needs aforge restarted.

Each call opens a connection, does its one thing and closes it. The outer backstop is
2 minutes. A tool that refuses comes back as an error carrying the service's own
sentence. Images, sounds and resources come back named — `[an image]`, `[a sound]` —
rather than as bytes.

## Anything that acts in your name asks you first

Calls that leave this machine in your own name are asked about even under a blanket
allow. That covers:

- **`gmail_send`**, **`calendar_create`** and **`slack_send`**;
- any key account's raw call — a tool whose name ends in `_request` — judged by its
  verb: a `GET`, or no arguments at all, is a read; every other method acts. **An
  argument payload that cannot be read counts as one that acts**, because the safe
  reading of "I do not know" is the one that asks;
- any tool an MCP server serves that the service did not mark read-only. Absent means
  false, and false is the stricter reading: a service that says nothing has not
  promised its tool only looks.

**No blanket setting turns this off.** Setting the approval default to `allow`, or
launching with `--yolo`, turns such a call into a question rather than running it. The
guardian — the optional small model that can answer easy questions for you — may never
stand in for you on one of these calls either.

It yields only to something that names the tool: a per-tool allow rule such as
`gmail_send:allow`, the account's capability set to `yes`, or an "always" you already
answered. For the approval question itself, its keys and how "always" is banked, see
the permissions page.

A tool an MCP server serves that is marked read-only is governed by the account's
`read` capability; everything else by `act`. An acting tool's description says so:
"It changes something in their account in their name, and the person is asked before
it goes."

## Where your keys are kept on disk

Everything the accounts layer writes lives in your profile directory —
`$AFORGE_PROFILE_DIR` when set, otherwise aforge's state root `$AFORGE_HOME` or
`~/.aforge`.

| File | What is in it | Permissions |
| --- | --- | --- |
| `credentials.json` | every connection's keys: access and refresh tokens, pasted keys or the name of the variable holding one, the granted permissions, the account address | **0600**, inside a **0700** directory |
| `connections.json` | your capability answers | **0644 and readable, deliberately** |
| `toolservers.json` | aforge's own registered identity with each MCP tool server | **0600** |

`connections.json` is readable on purpose: this is what you agreed to, not what lets
anybody act on it. `toolservers.json` is 0600 like the keys because a server may issue
a secret to aforge, but **no key of yours is in it**; it survives a disconnect on
purpose.

Every write to `credentials.json` goes through a temp file that is set to 0600 before
a byte is written, then renamed over the old one — so keys are never briefly readable
by anybody else, and a killed process leaves the previous file intact.

aforge never logs a key. No access key, refresh key, client secret or pasted key is
printed, returned in an error, or written anywhere but the store file. And a key that
reaches aforge from somewhere else entirely — a shell command that printed one, a config
file that was read — is replaced with `[redacted token · …]` before the result is kept,
shown or sent to the model; the conversations page has the shapes it recognises.

A `credentials.json` that has become unreadable reads as nothing connected — the safe
answer, which asks you to sign in again rather than promising access that cannot be
delivered. A damaged `toolservers.json` reads as no registrations, so aforge registers
again.

Two processes writing at once are safe because of the rename, but can lose each
other's last change.

## Connecting an account over --host

Over `--host` you cannot connect a **new** account in a browser. `/connect` answers,
literally:

```
connecting an account is not available over --host yet — the sign-in opens a browser here and the account belongs to the machine over there. accounts already connected on that machine keep working.
```

When the model raises the connect question over `--host`, a browser service says
`connecting an account is not available over --host yet` and offers only
`[esc] not now`. `enter` and `y` do nothing there, rather than being turned quietly
into a no.

**A key sign-in works fine over `--host`** — pasting a key needs no browser.

And accounts already connected on the far machine keep working: their tools are on the
belt as usual.

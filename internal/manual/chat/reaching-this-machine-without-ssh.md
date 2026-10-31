# Reaching a machine with a pairing code

For an always-on computer on your own Railway account, see [A dev box](a-dev-box-on-your-own-railway.md) and `codeaf devbox up <name>`.

## Two codes, two grants — remote access and your chats

You learn one thing: **a code shown on a screen you own, typed on the other computer.** A code can grant two different things. They stay two things, because they are two different powers.

| | remote access (`codeaf serve`, `codeaf chat --at <name>`) | your chats (`/pair`, `codeaf pair`) |
| --- | --- | --- |
| What the other computer gets | a key to this machine's tools: a hand on that one machine | your identity: your chats, your saved keys and your sync, on every computer you own |
| The code | six digits, shown `715 302`, typed with the name of the machine | a number, then six digits, shown `42-715-302`; the number is the place, so the code carries it |
| This machine must | stay on and stay connected | be there only while you pair |
| Who can undo it | this machine alone, with `codeaf devices revoke <name>`, at once | you, with `r` in `/devices` or `/settings` → `Devices`, or `codeaf devices revoke <name>`, but it cuts the relay only; see the page *Pairing your chats with a second computer* |

**One code never gives both.** A code for your chats does not give a shell on any machine. A code for a machine does not give your chats.

Both codes are good for 10 minutes and take 1 attempt. For both, the two screens show the same three words, and a person answers `y / n` on the machine being opened. The page *Pairing your chats with a second computer* has the chats door.

## Reaching a machine behind a router or a firewall

`--host` needs `ssh <machine>` to already work. When it does not — a home server behind a
router with no forwarded port, a work machine behind a firewall, a laptop on a café
network — there is a second door: one machine runs `codeaf serve`, and you reach it by the
name that prints, with a pairing code instead of a key.

```
big-machine$ codeaf serve
  this machine is reachable as  otter-lamp-42
  let a device use this machine with code   715 302   (valid 10 minutes)

laptop$ codeaf chat --at otter-lamp-42
```

**Nothing is opened on the machine that runs the work.** No port, no inbound rule, no
router setting. That machine dials *out* to a relay and stays connected, which is why
firewalls and NAT stop mattering.

**There is nothing to set up.** Both machines go through the relay their chats already
sync through: the hosted relay, unless sync was pointed somewhere else with
`CODEAF_SYNC_URL` or by pairing. `CODEAF_RELAY`, or `--via` on `codeaf serve`, picks a
different relay for this door alone, such as one you run yourself.

`--host` works today and is not going anywhere. If you can already reach the machine that
way, use that; the two doors are compared at the bottom of this page.

## codeaf serve, and the name a machine gets

`codeaf serve` keeps the identity's saved keys and settings in step the way an open terminal does only when it is started with `CODEAF_SERVE_SYNC=1`, which a dev box sets. Without it, serve syncs nothing while it serves, as it always has.

Plain `codeaf serve` opens only the machine connection; it does not take tasks.
On a paired Linux or macOS computer, `codeaf serve --take-tasks` also prints
`takes tasks from your paired computers`. Those computers can send a task with
`/task --on <shown device name> <brief>` while keeping the chat and transcript
where they are. This task door uses the shared identity and needs no separate
machine-access grant. Only work with its own independent copy can go; Windows
cannot send or take these tasks yet. See *Tasks on another computer*. The
connection name below is for reaching the whole machine, not choosing a task's
computer.

Run it on the machine that owns the work — the one with the files, the API key and the
things you have set up:

```
codeaf serve [--workspace path] [--via https://…] [--take-tasks]
```

It prints, exactly:

```
  this machine is reachable as  otter-lamp-42
  let a device use this machine with code   715 302   (valid 10 minutes)
```

**The relay name is derived from that machine's own key.** Two words and two
digits, the same every time, on any relay. Nobody else can register under yours
while your machine is connected. A dev box also has the chosen alias saved on
the laptop that created it; `codeaf devices` shows that alias for `--at`.

`--workspace` is the directory a connection works in when it does not name one; empty
means the directory you ran the command in. For `codeaf serve`, the relay is, in order:
`--via`, the `CODEAF_RELAY` environment variable, `~/.codeaf/v3/remote/relay`, then the
relay this machine syncs through. For `codeaf chat --at`, it is `CODEAF_RELAY`, the relay
that machine was paired through, `~/.codeaf/v3/remote/relay`, then the relay this machine
syncs through.

Running it twice on one machine is refused, because there is only one of it:

```
this machine is already connected to sync as otter-lamp-42 — there is only one of it, so close the other `codeaf serve`
```

## codeaf serve when the network drops or the relay says no

If the network drops while it runs, it says once:

```
lost sync — trying again
```

and keeps trying, waiting a little longer each time up to a minute, resetting that wait
only after a connection has held for at least a minute. When it reconnects, it says once:

```
back — reachable as otter-lamp-42 again
```

It prints its name and code again only if the name or code changed. It does not need
restarting, and devices reach it as soon as it is back.

If the relay turns registrations away — it allows 10 registrations a minute from one
address — `codeaf serve` backs off the same way and says once:

```
the sync service is turning registrations away right now — waiting before trying again
```

A relay that answers but cannot connect machines, such as an older sync service, stops
it with:

```
the sync service at https://service.example.com syncs chats but does not connect machines — run your own with `codeaf relay` and set CODEAF_RELAY to it
```

A relay that cannot be reached when it starts stops it with the sentence in *When --at
will not open*. With sync turned off (`CODEAF_SYNC_URL=off`) and no `CODEAF_RELAY`, there
is no relay at all:

```
no sync service is set up on this machine, so there is nowhere to be reachable from — set CODEAF_RELAY to your sync service
```

Ctrl+c ends it, and the machine gives its name back on the way out:
`this machine is no longer reachable`.

## The pairing code, and typing it in

The first time a device reaches a machine it has to be let in, once. You read six digits
off the machine's own screen and type them into the device.

On the device:

```
codeaf chat --at otter-lamp-42
```

It prints, in this order:

```
pairing with otter-lamp-42
enter the code shown on otter-lamp-42:
```

Type `715 302` or `715302` — spaces and dashes are thrown away. Then this device shows three words and waits:

```
waiting for approval on otter-lamp-42; it should show: amber fox dune
```

## Approving remote work — the three words and y / n

The three words are the check. On the serving computer, codeaf first says `a paired device is a key to that machine: it opens conversations there, runs whatever that machine allows to run, and spends that machine's model key. it is the same weight as an ssh key.` Then it shows the same three words and asks the person at its terminal, which is you:

```
"laptop" wants to use this machine. Same three words on that screen: amber fox dune?  y / n
```

**There is no default.** Enter alone does nothing. Answer `y` only if both screens show the same three words. If they differ, answer `n`: somebody else may have used the code. On `n`, or with no answer for 2 minutes, this device says `refused on the other device` and the machine says `a device asked to pair and was not let in`.

**A machine with nobody at its terminal admits nobody.** When `codeaf serve` does not run in a terminal, there is no person to answer `y / n`, and the answer is no. Run `codeaf serve` in a terminal you watch while you pair.

On `y`, the device says:

```
paired. this device is now a key to otter-lamp-42.
```

and the chat opens. Every later `codeaf chat --at otter-lamp-42` from that device just
opens; the code is never asked for again.

**Once that machine says a device is paired, the very next connection is accepted.** The
machine writes the device into its list of devices *before* it says the pairing held, so a
first pairing never says `paired.` and then
`this device has been stopped on that machine — pair it again from there` a moment later.
That second sentence belongs to a device somebody deliberately stopped. A machine that
cannot write the pairing down does not say it held: it refuses the pairing instead, and
what you get is the sentence below for a code that did not work.

## How long a remote-access code lasts — one attempt and a wrong code

**A code is good for 10 minutes and for 1 attempt, whichever runs out first.** Any attempt
spends the code, right or wrong, and the machine shows a fresh one at once — you do not
press a key. A wrong code says:

```
that is not the code shown on otter-lamp-42 — read it again, and note that it is only good for 10 minutes
```

**Guessing a code from outside does not work.** Only the machine that minted it can spend
an attempt against it, and it allows 1 before throwing the code away. Then a person looks at
three words on two screens. A device that turns up when there is no live code at all is told
the same thing a wrong code is told, because from its side it is the same fact. The machine's
own screen is where the difference shows:

```
someone typed a wrong code, so that code is no longer good
a device tried to pair and there was no code to pair with
```

## Pairing another laptop, a desktop, or a phone

Each device pairs separately, with its own code and its own key. Pairing your laptop does
not pair your desktop.

1. On the machine with the work: `codeaf serve`. Read the code it shows.
2. On the other machine: `codeaf chat --at <name>`, and type the code.

The machine you are sitting at prints one line per arrival, so you can watch it happen:

```
laptop is now paired with this machine — it can open conversations here and run what this machine allows
laptop connected
laptop left
```

**A device names itself by its host name.** That is only a label for the list you read; a
connection is always checked against the device's key, never against its name. Two laptops
with the same host name are two entries.

**More than one device can be connected at once** — up to 16 through one machine's
connection to the relay. What that means for the conversation on the far machine is the
same as for any other connection; nothing about `--at` changes it.

**A phone or a tablet cannot do this yet.** There is no browser client and no phone app;
`--at` is a terminal command, so a phone reaches a machine only if you have a terminal on
it that can run codeaf.

## What a paired device is allowed to do

**A paired device is a key to that machine.** Not a viewer, not a read-only window. From
it, somebody can:

- open conversations on that machine, in that machine's workspace;
- do whatever that machine's approval settings allow — including, where that machine is
  set to allow everything, tools that write files and issue shell commands;
- spend that machine's model key, which is that machine's money.

The pairing screen says this before you type a code, in these words:

```
a paired device is a key to that machine: it opens conversations there, runs whatever that machine allows to run, and spends that machine's model key. it is the same weight as an ssh key.
```

Pair a device you would trust with a key to that machine, and no other.

**What a paired device cannot do:** it cannot pair another device, it cannot list or stop
the devices that machine has let in, and it cannot un-pair itself from there. Those are
all decisions of the machine that owns the work, made on that machine with
`codeaf devices`.

**What crosses the connection is exactly what crosses a `--host` connection.**
`codeaf serve` starts the very same `codeaf engine` on that machine, one per connection, so
everything this manual says about what does and does not work over a connection is true
here word for word. Read *Running on another machine* for that list.

## Is this safe — what the relay can see

The relay is a service in the middle that puts two connections next to each other. It
never sees a word of the conversation.

**What it can see:** the machine name, when a connection starts and stops, how many bytes
went each way, and the shape of the traffic. That is the complete list — the relay holds
no account, no password, and no record of a connection once it has ended.

**What it cannot see:** anything you type, anything the conversation answers, which files
were touched, what the tools did, or which of your devices is connected. All of it is
encrypted between the two ends.

**And it cannot get in the middle of a pairing, either**, which is the part that would
otherwise be easy for it. The six digits are exchanged with a password-authenticated key
exchange, so a listener — the relay included — learns nothing about the code and cannot
grind at it offline. The one way to test a guess is to run a whole attempt against the
machine that minted the code, and that machine allows 1 of them. A person then compares three
words on both screens before anything is let in.

After pairing, both ends have written down each other's key. Every later connection is a
handshake between those two keys, so a relay that pointed you at the wrong machine gets a
handshake that does not complete rather than a conversation it can read:

```
whatever is answering to otter-lamp-42 is not the machine this device paired with, so nothing was sent — pair again from that machine if it was rebuilt
```

## Someone else's computer, and borrowed machines

**Do not pair a machine you do not control.** A pairing does not expire, is not tied to a
session, and does not ask again — a borrowed laptop you paired stays a key to your machine
after you hand it back, until you stop it from the machine that owns the work.

If you have already done it: go to the machine with the work and run
`codeaf devices revoke <name>`. That takes effect on the next connection attempt, and the
device is told:

```
this device has been stopped on that machine — pair it again from there
```

There is no browser version of this, so "log in from a friend's computer" is not something
this door offers at all — it is a terminal command that installs a long-lived key on the
machine it is run on.

## Stop a device — codeaf devices

Run it on the machine that owns the work:

```
codeaf devices
```

It prints which machine this is, where its key is kept, and the devices it lets in:

```
this machine is reachable as otter-lamp-42
its key is kept in a file on this machine, readable only by you (~/.codeaf/v3/remote/device.key)

devices that can use this machine

  laptop   paired 3d ago  ·  last here 2h ago
  desktop  paired 12d ago

remove one with `codeaf devices revoke <name>` — it will need a new code to come back.
```

If you also paired computers with `/pair`, a second group under the heading `your devices` lists them, and `revoke <name>` works on both groups. With nothing of either kind it says:

```
nothing is paired yet — run `codeaf pair` to add another device, or `codeaf serve` to let a device use this machine.
```
 A device that has never
connected shows nothing where its last connection would be, rather than a zero.

To remove one:

```
codeaf devices revoke laptop
```

which answers
`laptop was removed — it can no longer open a chat here, and it needs a new pairing code to come back.`

A name nothing matches says:

```
no device called "phone" is paired with this machine — `codeaf devices` lists the ones that are
```

## Two devices with the same name — choose a suffix or remove all

A bare duplicate name is refused with the choices, such as `laptop #8381` and `laptop #9cf9`. Use `codeaf devices revoke laptop#8381` to choose one, or quote its printed name as `codeaf devices revoke "laptop #8381"`. `--all` removes every device answering to that name:

```
codeaf devices revoke laptop --all
codeaf devices revoke --all laptop
```

**Both spellings work.** `--all` is an ordinary flag and is read wherever you put it, before
the name or after it; `codeaf devices revoke --help` prints it. It used to be read only when
it came first, so `codeaf devices revoke laptop --all` was refused with a usage line that did
not mention `--all` at all.

`--all` reaches every device `codeaf devices` lists under that name, of either kind: a
computer that syncs your chats as well as a device that can use this machine. It used to look
only at devices that can use this machine, so `codeaf devices revoke <name> --all` said `no
device called "<name>" is paired with this machine` for a computer the list showed.

Removing one device with `--all` answers in the ordinary sentence — `laptop was removed — it
can no longer open a chat here, and it needs a new pairing code to come back.` Removing
several answers `2 devices called laptop were removed — each needs a new pairing code to come
back.`

**Revoking is always the decision of the machine that owns the work.** There is no way to
do it from the device, and no way for a device to remove another one.

## I lost my laptop

Go to the machine that owns the work — the one you ran `codeaf serve` on — and stop the
device:

```
codeaf devices revoke laptop
```

From then on that laptop opens nothing. It is told
`this device has been stopped on that machine — pair it again from there`, and no
conversation is opened.

**Nothing else has to be changed.** You do not have to rebuild the machine, change its
name, or re-pair your other devices — each device has its own key and stopping one has no
effect on the others.

**If you would rather start over completely**, delete the machine's own key file
(`~/.codeaf/v3/remote/device.key`) and run `codeaf serve` again. It comes back under a
*different name*, because the name is derived from the key — which un-pairs every device
at once, and means telling the ones you still want the new name and a new code.

The key on the lost laptop protects nothing by itself: it is only useful against a machine
that still has it in the list, which is what revoking removes.

## When --at will not open

There are five different reasons, and each says which one it is, in one sentence.

**No relay is set up on this machine** (sync is off and `CODEAF_RELAY` is empty):

```
no sync service is set up on this machine, so --at has nowhere to look for otter-lamp-42 — set CODEAF_RELAY to a sync service, or reach that machine with --host over ssh
```

**The relay is set up and not answering:**

```
nothing is answering at https://service.example.com — if that is your own sync service, start it there with codeaf relay
```

**The relay answers but cannot connect machines:**

```
the sync service at https://service.example.com syncs chats but does not connect machines — run your own with `codeaf relay` and set CODEAF_RELAY to it, or reach that machine with --host over ssh
```

**The relay is fine and that machine is not connected to it:**

```
otter-lamp-42 is not connected to the sync service right now — run `codeaf serve` on that machine
```

**This device has never been let in:**

```
this device is not paired with otter-lamp-42 — run `codeaf serve` on that machine, then run this command again and type the code it shows
```

Two more you may meet. A machine that does not answer the handshake at all:
`otter-lamp-42 did not answer this device's handshake, so nothing was sent — if that machine was rebuilt it has a new key and this device has to pair with it again`.
And a relay that is turning connections away:
`the sync service is turning connections away right now — try again in a minute` — it allows
30 connections a minute from one address. If a proxy or wrong path at the relay address
refuses the upgrade, it names the full address it tried:
`the sync service at https://service.example.com/wrong-prefix refused this connection (404) — check the address it was given`.

## The pairing code was right but approval failed — writing access on the serving computer

**A pairing that says the code was wrong when you are sure it was right** has one other
cause, and it is the machine's own disk. The machine writes a device into its list
*before* it tells the device the pairing held, so a write that fails refuses the pairing
outright rather than leaving a device that believes it is a key to a machine with no
record of it. It refuses it by hanging up, which from this end looks exactly like a code
that did not agree — the last message of a pairing carries a key and a name and has no
room for a reason. **The machine's own screen is where the difference shows**, in the same
place it says a code was wrong:

```
could not write down that pairing: <what the write said>
```

So if a code you read carefully keeps being refused, go and look at that machine.

A name of the wrong shape is caught before anything is dialled:

```
"devbox" is not the shape of a machine name — they look like otter-lamp-42, and `codeaf serve` prints the name of a machine
```

## How to type the --at target

The target is a machine name, optionally with a directory after a colon — the same shape
`--host` uses:

| What you type | What it means |
| --- | --- |
| `--at otter-lamp-42` | that machine, in the directory `codeaf serve` was started in |
| `--at otter-lamp-42:code/app` | a path relative to that machine's home directory |
| `--at otter-lamp-42:/srv/code/app` | an absolute path on that machine |

**The path is the far machine's path**, passed on as you typed it and resolved over there.
Tab completion in your own shell will not help you with it.

An empty target says:

```
--at needs a machine name: --at otter-lamp-42, or --at otter-lamp-42:code/app — `codeaf serve` prints the name of a machine
```

`--model`, `--reasoning`, `--session` and `--once` all work. `--no-compact` and `--yolo`
are refused rather than quietly ignored, because they build a session that is built over
there:

```
--no-compact and --yolo cannot travel over --at: the session is built on otter-lamp-42, so set it there — open the settings panel on that machine, or run `codeaf chat --no-compact --yolo` on it
```

## Where the device key is kept, and Touch ID

Each machine has one long-term key. Its name comes from that key, and every pairing is
written down against it.

**It is a file, not a keychain entry, and there is no Touch ID or fingerprint unlock in
this build.** The file is `~/.codeaf/v3/remote/device.key`, readable only by you, and
`codeaf devices` says so in as many words:

```
its key is kept in a file on this machine, readable only by you (~/.codeaf/v3/remote/device.key)
```

Putting the key in the operating system's keychain — so that a Mac could demand a
fingerprint before releasing it, and codeaf would never see the fingerprint itself — is
the intended design and is **not built**. Nothing in codeaf asks for a fingerprint today,
and any screen that appeared to would not be this.

What that means in practice: anybody who can read that file on your machine can be your
machine. It is exactly the exposure of `~/.ssh/id_ed25519` with no passphrase, and worth
the same care.

Deleting the file makes the machine come back under a **different name** and un-pairs
every device that knew the old one.

## No ssh, or ssh — --at and --host compared

Both run the conversation on another machine and keep the screen on the one you are
sitting at. They differ only in how the two halves reach each other.

| | `--host` | `--at` |
| --- | --- | --- |
| Needs | `ssh <machine>` already works | one pairing; the relay is the one sync uses |
| Set up on the far machine | nothing | `codeaf serve` running |
| Works through NAT and firewalls | only if ssh does | yes — that machine dials out |
| Anything in the middle | nothing of ours | a relay that cannot read the conversation |
| Available today | yes | yes |

**Prefer `--host` when ssh works.** It has nothing between the two machines, nothing to
set up, and no service to depend on. `--at` is for the machine ssh cannot reach.

Both machines must use the same relay. If one syncs through a relay of your own and the
other through the hosted one, set `CODEAF_RELAY` to the same address on both.

Both open the same surface with the same limits — what does and does not work over a
connection is a property of the connection, not of which of these two doors opened it.

## Running the relay yourself

The relay is a program, and `codeaf relay --listen :8787` runs it (`relay --listen :8787`
from the separate `relay` binary is the same thing). Left like that it is the blind pipe
this page describes: it forwards the conversation and cannot read it.

Given `--store <directory>` it also keeps the shared directory and blob store that lets one
person's machines see the same chats: `codeaf relay --listen :8787 --store /var/lib/codeaf`.
Every request must be signed by one of that person's devices, each person's data lives
apart from everyone else's, and a restart keeps who holds each chat. Its log lists the
first characters of an identity id, the kind of request, the status and the byte counts,
never what was sent. Stop it with SIGTERM and requests already under way finish first.

## The serving computer’s device name and the remote window bar

When this computer is paired for sync, `codeaf serve` and `codeaf serve --take-tasks` lead with the machine name and show the device name as a hint after it, for example `this machine is reachable as  seagull-lichen-63 (desk-b)`. Type the first name: `codeaf chat --at seagull-lichen-63`. The `--at` window’s bar shows `desk-b`. This build cannot look up a machine by its device name.

## Approve remote work from a device already paired for chats

Pairing for chats and allowing remote work are separate permissions. On the serving computer, before `y / n`, codeaf says `a paired device is a key to that machine: it opens conversations there, runs whatever that machine allows to run, and spends that machine's model key. it is the same weight as an ssh key.` The asking computer starts by asking for the displayed code.

The approver always asks `"laptop-a" wants to use this machine. Same three words on that screen: red green blue?`, followed by `y / n`. This question also appears when the asking computer is already paired for chats: this exchange cannot verify its chat pairing. Check the three words on both screens and explicitly approve before it gets access to work here.

## A served connection ended

The serving terminal says `the connection to laptop-a ended`. A known timeout adds `— it stopped answering`. Process and pipe details go to the local serving log, keeping the terminal free of raw process errors.


## Which computer name appears in an --at window and its connection errors

An `--at` window uses the serving computer's chosen device name in its bar,
connection errors, reconnecting notices and timeout messages. Its typed
rendezvous name still selects the machine to connect to. If an older engine
has not supplied a chosen name, the window uses the requested name.

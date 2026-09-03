# Reaching a machine with a pairing code

## Reaching a machine behind a router or a firewall

`--host` needs `ssh <machine>` to already work. When it does not — a home server behind a
router with no forwarded port, a work machine behind a firewall, a laptop on a café
network — there is a second door: one machine runs `aforge serve`, and you reach it by the
name that prints, with a pairing code instead of a key.

```
big-machine$ aforge serve
  this machine is reachable as  otter-lamp-42
  pair a new device with code   715 302   (valid 10 minutes)

laptop$ aforge chat --at otter-lamp-42
```

**Nothing is opened on the machine that runs the work.** No port, no inbound rule, no
router setting. That machine dials *out* to a relay and stays connected, which is why
firewalls and NAT stop mattering.

**This needs a relay service, and there is not one running yet.** Until one is switched
on, `--at` prints:

```
no relay is set up on this machine, so --at has nowhere to look for otter-lamp-42 — set AFORGE_RELAY to a relay's address, or reach that machine with --host over ssh
```

`--host` works today and is not going anywhere. If you can already reach the machine that
way, use that; the two doors are compared at the bottom of this page.

## aforge serve, and the name a machine gets

Run it on the machine that owns the work — the one with the files, the API key and the
things you have set up:

```
aforge serve [--workspace path] [--relay https://…]
```

It prints, exactly:

```
  this machine is reachable as  otter-lamp-42
  pair a new device with code   715 302   (valid 10 minutes)
```

**The name is not chosen, it is derived from that machine's own key.** Two words and two
digits, the same every time, on any relay. You cannot pick a nicer one, and nobody else
can register under yours while your machine is connected.

`--workspace` is the directory a connection works in when it does not name one; empty
means the directory you ran the command in. `--relay` beats the `AFORGE_RELAY`
environment variable.

Running it twice on one machine is refused, because there is only one of it:

```
this machine is already connected to the relay as otter-lamp-42 — there is only one of it, so close the other `aforge serve`
```

With no relay set up at all it says:

```
no relay is set up on this machine, so there is nowhere to be reachable from — set AFORGE_RELAY to a relay's address, or let people in over ssh with `aforge chat --host` from their side
```

Ctrl+c ends it, and the machine gives its name back on the way out:
`this machine is no longer reachable`.

## The pairing code, and typing it in

The first time a device reaches a machine it has to be let in, once. You read six digits
off the machine's own screen and type them into the device.

On the device:

```
aforge chat --at otter-lamp-42
```

It prints, in this order:

```
pairing with otter-lamp-42
a paired device is a key to that machine: it opens conversations there, runs whatever that machine allows to run, and spends that machine's model key. it is the same weight as an ssh key.
enter the code shown on otter-lamp-42:
```

Type `715 302` or `715302` — spaces and dashes are thrown away. Then:

```
paired. this device is now a key to otter-lamp-42.
```

and the chat opens. Every later `aforge chat --at otter-lamp-42` from that device just
opens; the code is never asked for again.

**Once that machine says a device is paired, the very next connection is accepted.** The
machine writes the device into its list of devices *before* it says the pairing held, so a
first pairing never says `paired.` and then
`this device has been stopped on that machine — pair it again from there` a moment later.
That second sentence belongs to a device somebody deliberately stopped. A machine that
cannot write the pairing down does not say it held: it refuses the pairing instead, and
what you get is the sentence below for a code that did not work.

**A code is good for 10 minutes and for 5 attempts, whichever runs out first**, and one
successful pairing spends it — the machine shows a fresh one for the next device. A wrong
code says:

```
that is not the code shown on otter-lamp-42 — read it again, and note that it is only good for 10 minutes
```

**Guessing a code from outside does not work.** Only the machine that minted it can spend
an attempt against it, and it allows 5 before throwing the code away. A device that turns
up when there is no live code at all is told the same thing a wrong code is told, because
from its side it is the same fact. The machine's own screen is where the difference shows:

```
a device tried to pair with the wrong code
a device tried to pair and there was no code to pair with
```

## Pairing another laptop, a desktop, or a phone

Each device pairs separately, with its own code and its own key. Pairing your laptop does
not pair your desktop.

1. On the machine with the work: `aforge serve`. Read the code it shows.
2. On the other machine: `aforge chat --at <name>`, and type the code.

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
it that can run aforge.

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
`aforge devices`.

**What crosses the connection is exactly what crosses a `--host` connection.**
`aforge serve` starts the very same `aforge engine` on that machine, one per connection, so
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
machine that minted the code, and that machine allows 5 of them.

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
`aforge devices revoke <name>`. That takes effect on the next connection attempt, and the
device is told:

```
this device has been stopped on that machine — pair it again from there
```

There is no browser version of this, so "log in from a friend's computer" is not something
this door offers at all — it is a terminal command that installs a long-lived key on the
machine it is run on.

## Stop a device — aforge devices

Run it on the machine that owns the work:

```
aforge devices
```

It prints which machine this is, where its key is kept, and the devices it lets in:

```
this machine is reachable as otter-lamp-42
its key is kept in a file on this machine, readable only by you (~/.aforge/v3/remote/device.key)

devices paired with this machine

  laptop   paired 3d ago  ·  last here 2h ago
  desktop  paired 12d ago

stop one with `aforge devices revoke <name>` — it will need a new code to come back.
```

With nothing paired it says
`no devices are paired with this machine.` and how to pair one. A device that has never
connected shows nothing where its last connection would be, rather than a zero.

To stop one:

```
aforge devices revoke laptop
```

which answers
`laptop has been stopped — it can no longer open a conversation here, and it will need a new pairing code to come back.`

A name nothing matches says:

```
no device called "phone" is paired with this machine — `aforge devices` lists the ones that are
```

Two devices with the same name are refused rather than guessed at, and `--all` stops every
device answering to that name:

```
aforge devices revoke laptop --all
aforge devices revoke --all laptop
```

**Both spellings work.** `--all` is an ordinary flag and is read wherever you put it, before
the name or after it; `aforge devices revoke --help` prints it. It used to be read only when
it came first, so `aforge devices revoke laptop --all` was refused with a usage line that did
not mention `--all` at all.

Stopping one device with `--all` answers in the ordinary sentence — `laptop has been stopped
— it can no longer open a conversation here, and it will need a new pairing code to come
back.` Stopping several answers `2 devices called laptop have been stopped — each needs a new
pairing code to come back.`

**Revoking is always the decision of the machine that owns the work.** There is no way to
do it from the device, and no way for a device to remove another one.

## I lost my laptop

Go to the machine that owns the work — the one you ran `aforge serve` on — and stop the
device:

```
aforge devices revoke laptop
```

From then on that laptop opens nothing. It is told
`this device has been stopped on that machine — pair it again from there`, and no
conversation is opened.

**Nothing else has to be changed.** You do not have to rebuild the machine, change its
name, or re-pair your other devices — each device has its own key and stopping one has no
effect on the others.

**If you would rather start over completely**, delete the machine's own key file
(`~/.aforge/v3/remote/device.key`) and run `aforge serve` again. It comes back under a
*different name*, because the name is derived from the key — which un-pairs every device
at once, and means telling the ones you still want the new name and a new code.

The key on the lost laptop protects nothing by itself: it is only useful against a machine
that still has it in the list, which is what revoking removes.

## When --at will not open

There are four different reasons, and each says which one it is, in one sentence.

**No relay is set up on this machine:**

```
no relay is set up on this machine, so --at has nowhere to look for otter-lamp-42 — set AFORGE_RELAY to a relay's address, or reach that machine with --host over ssh
```

**The relay is set up and not answering:**

```
the relay at https://relay.example.com cannot be reached from here — check this machine's network, or reach that machine with --host over ssh
```

**The relay is fine and that machine is not connected to it:**

```
otter-lamp-42 is not connected to the relay right now — run `aforge serve` on that machine
```

**This device has never been let in:**

```
this device is not paired with otter-lamp-42 — run `aforge serve` on that machine, then run this command again and type the code it shows
```

Two more you may meet. A machine that does not answer the handshake at all:
`otter-lamp-42 did not answer this device's handshake, so nothing was sent — if that machine was rebuilt it has a new key and this device has to pair with it again`.
And a relay that is turning connections away:
`the relay is turning connections away right now — try again in a minute` — it allows
30 connections a minute from one address.

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
"devbox" is not the shape of a machine name — they look like otter-lamp-42, and `aforge serve` prints the name of a machine
```

## How to type the --at target

The target is a machine name, optionally with a directory after a colon — the same shape
`--host` uses:

| What you type | What it means |
| --- | --- |
| `--at otter-lamp-42` | that machine, in the directory `aforge serve` was started in |
| `--at otter-lamp-42:code/app` | a path relative to that machine's home directory |
| `--at otter-lamp-42:/srv/code/app` | an absolute path on that machine |

**The path is the far machine's path**, passed on as you typed it and resolved over there.
Tab completion in your own shell will not help you with it.

An empty target says:

```
--at needs a machine name: --at otter-lamp-42, or --at otter-lamp-42:code/app — `aforge serve` prints the name of a machine
```

`--model`, `--reasoning`, `--session` and `--once` all work. `--no-compact` and `--yolo`
are refused rather than quietly ignored, because they build a session that is built over
there:

```
--no-compact and --yolo cannot travel over --at: the session is built on otter-lamp-42, so set it there — open the settings panel on that machine, or run `aforge chat --no-compact --yolo` on it
```

## Where the device key is kept, and Touch ID

Each machine has one long-term key. Its name comes from that key, and every pairing is
written down against it.

**It is a file, not a keychain entry, and there is no Touch ID or fingerprint unlock in
this build.** The file is `~/.aforge/v3/remote/device.key`, readable only by you, and
`aforge devices` says so in as many words:

```
its key is kept in a file on this machine, readable only by you (~/.aforge/v3/remote/device.key)
```

Putting the key in the operating system's keychain — so that a Mac could demand a
fingerprint before releasing it, and aforge would never see the fingerprint itself — is
the intended design and is **not built**. Nothing in aforge asks for a fingerprint today,
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
| Needs | `ssh <machine>` already works | a relay service, and one pairing |
| Set up on the far machine | nothing | `aforge serve` running |
| Works through NAT and firewalls | only if ssh does | yes — that machine dials out |
| Anything in the middle | nothing of ours | a relay that cannot read the conversation |
| Available today | yes | not until a relay is running |

**Prefer `--host` when ssh works.** It has nothing between the two machines, nothing to
set up, and no service to depend on. `--at` is for the machine ssh cannot reach.

Both open the same surface with the same limits — what does and does not work over a
connection is a property of the connection, not of which of these two doors opened it.

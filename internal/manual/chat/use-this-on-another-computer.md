# Pairing your chats with a second computer

## How do I use this on my laptop — pair a second computer with a code, and log in on a new machine

There is no account and no password to log in with. You pair: a code on one screen, typed on the other.

You learn one thing here: **a code shown on a screen you own, typed on the other computer.**

The computer that has your chats shows the code. The other computer types it. After that, both computers have the same chats.

There are two doors. Pick one.

1. On the computer that has your chats, type `/pair` in the chat. Or run `codeaf pair` in a terminal. It shows a code and how to use it:

```
  this shares your chats with the device you pair
  on it run  codeaf pair 42-715-302   (valid 10 minutes)
```

2. On the other computer, use the code:
   - in a terminal, run `codeaf pair 42-715-302`;
   - or, in the chat, type `/pair 42-715-302`;
   - or, on the first screen of a new codeaf, type the code in the field `have a code from another device?`.

Dashes and spaces in the code do not matter. `42-715-302`, `42 715 302` and `42715302` are the same code.

The code is good for 10 minutes. It works once.

`/pair` also answers to `/sync`, `/link` and `/laptop`. Type `/pair` with nothing after it to show a code. Type `/pair <code>` to use one. `codeaf pair` with nothing after it shows a code in a terminal. `codeaf pair <code>` uses one.

```
usage: codeaf pair [<code>] [--relay url] [--replace]
```

Press ctrl+c in the terminal to take a shown code back. In the chat, Esc stops it.

A computer that has no relay cannot pair. See "The relay" below. A chat that runs over `--host` or `--at` cannot pair: it says `pairing is not available on this connection`. Pair from a chat that runs on the computer itself.

## The three words — the check on the computer that has your chats

Both screens show the same three words. This is how you know the two computers are the ones you hold.

The computer that uses the code shows the words first and waits:

```
waiting for approval on your other device; it should show: amber fox dune
```

The computer that has your chats asks you. It shows the name of the other computer and the same words. You answer `y` or `n`:

```
"laptop" wants your chats. Same three words on that screen: amber fox dune?  y / n
```

**There is no default.** Enter alone does nothing. Only `y` lets the other computer in. In the chat the hint under the box reads `y / n · esc is n`: Esc counts as `n`.

Compare the words with your own eyes. Answer `y` only if both screens show the same three words. If the words are different, answer `n`. Somebody else may have used the code.

If you answer `n`, or you do not answer for 2 minutes, the other computer says:

```
refused on the other device
```

Nothing is sent. The code is gone. If you were not at the screen, run `/pair` again.

When you answer `y`, the computer that has your chats says:

```
paired: laptop
```

and the other computer says:

```
paired. this computer now has your chats.
```

The code is now dead. It cannot pair a third computer.

## The code did not work — one attempt for each code

**Each code takes 1 attempt.** A wrong code does not get a second try. The computer that shows the code throws it away and shows a new one at once. You do not press a key. It says:

```
someone typed a wrong code. New code:
```

and the new code appears under it. Use the new code.

This is why six digits are enough. An attacker gets one guess for each code. Each wrong guess shows on your screen. You then compare three words with your eyes.

The computer that typed the wrong code says:

```
that code did not work, or it timed out; ask for a new one
```

A code that ran out of time (10 minutes) says the same. Ask for a new code with `/pair` on the computer that has your chats.

Other things you can meet, each in one sentence:

A code with the wrong shape is refused before anything is sent:

```
a pairing code looks like 42-715-302: digits, with dashes or spaces if you like
```

The first number of the code is the place of the code on the relay. If nothing is there, you see this (the number is the one you typed):

```
no pairing is waiting under 42
```

This is also what you see when the code ran out. Ask for a new code.

Somebody else typed the code first:

```
someone else used that code
```

The computer that has your chats stopped, or the relay lost the pairing, before it finished:

```
the other device stopped pairing; run /pair there again
```

The pairing ran out of time in the middle:

```
the pairing did not finish; nothing was changed
```

A code is already on the screen of that computer, and you asked for a second one:

```
a pairing code is already showing on this computer
```

What the other computer sent could not be read:

```
what the other device sent could not be read; nothing was changed
```

A network asked for too many codes in an hour. The number is how long to wait:

```
too many pairings from this network; wait 7 min
```

## What the pairing shares — the other computer holds everything this one holds

**Pairing gives the other computer your whole identity.** Your chats, your saved keys and your sync are all part of it. The other computer now holds everything that this computer holds.

It is not a view of your chats. It is a copy of you. Pair only a computer you own and trust.

Pairing shares your chats. It does not share remote access. A paired computer is not a key to the tools on this machine. That is a different code, from `codeaf serve`. Read *Reaching a machine with a pairing code* for that door. The two doors are compared in a table on that page.

If the other computer already has chats of its own, it keeps them and nothing changes. It says:

```
this computer already has chats of its own, so they stay local and nothing was changed; to replace them with the other device's, run this again with --replace
```

Run `codeaf pair <code> --replace` if you want the chats of the other computer instead. Chats sealed under the old identity can no longer be read after that.

If the other computer already has the same chats, it says:

```
already paired; nothing changed
```

## I got a new laptop — move my chats to a new machine

1. On the old computer, type `/pair`.
2. On the new computer, run `codeaf pair <code>`, or type `/pair <code>` in the chat, or type the code on the first screen.
3. Compare the three words. Answer `y` on the old computer.

The new computer now has your chats. If both computers are on the same relay, chats sync between them. Read *Sync your chats between machines* in the terminal page for the sync settings.

There is no phone app in this stage. You cannot pair a phone, and there is no page you can open in a phone browser. You can pair only a computer that runs codeaf in a terminal.

## Devices and revoking — what you can stop, and what you cannot

`codeaf devices` lists every device linked to this computer, in up to two groups. A group with nothing in it does not show. With nothing of either kind it says:

```
no devices are paired yet — run `codeaf pair` to share your chats with another computer, or `codeaf serve` to let a device use this machine.
```

The group of computers you paired with `/pair` has this heading:

```
devices with your chats

  laptop  this computer
  desktop

stop one with `codeaf devices revoke <name>` — that cuts it off from your chats on the relay, and cannot take back what it already holds.
```

The group of computers you let in with `codeaf serve` has this heading, and its own list under it:

```
devices that can use this machine
```

To stop one, run `codeaf devices revoke <name>` with the name from the list. It works on both groups. When you stop a computer that holds your chats, it says:

```
desktop has been stopped — it can no longer sync your chats through the relay. it cannot take back what that computer already holds: it has your chats and keys, so if it was stolen, treat your chats as exposed.
```

You cannot stop the computer you are typing on. It says `that is this computer — stop it from another of your computers, so it is not the one cutting itself off`.

**Revoking has a limit, and you must know it.** Revoking cuts a device off from the relay. The relay stops serving it. Revoking does not take back what the device already holds. That device has your whole identity. It keeps every secret it already has.

So a computer that is lost or stolen must be treated as if your chats are exposed. Revoking it stops it from syncing. It does not make the chats on it unreadable to the person who has it.

To make revoking take effect for real, give all your other computers a new identity: run `codeaf identity rotate` on a computer you keep. Read *Locking out a lost computer for good*.

## Without a relay — identity export and import

You can move your identity by file. This needs no relay. It is the way to use when neither computer can reach a relay.

- `codeaf identity export` on the first computer asks for a passphrase and writes your identity under it.
- `codeaf identity import` on the other computer reads that file with the same passphrase.

**In this build, the `identity` verb is behind `CODEAF_CELLS=1`.** It is not in the help text. Without `CODEAF_CELLS=1` it stops with `codeaf identity needs CODEAF_CELLS=1`. Set `CODEAF_CELLS=1` on both computers to use it. The full description is under "Your identity" on the terminal page.

Keep the file and the passphrase safe. Anybody who has both has your chats.

## Where your chats are stored — the hosted relay, turning it off, your own relay, is my code private

Your chats live on your own computer first. They are also copied, sealed,
to a relay, so your other computers can open them. Which relay is one setting,
`CODEAF_SYNC_URL`, and there is no sign-in and no account:

- **Unset** uses codeaf's hosted relay when this build has one, and says so once, in one
  line, the first time your chats are copied: `Your chats sync end-to-end encrypted through
  codeaf's hosted relay, which only ever sees ciphertext; turn that off with
  CODEAF_SYNC_URL=off, or use your own relay with CODEAF_SYNC_URL=<url>.` A computer you
  paired with another one uses the relay that pairing saved instead. A build with no
  hosted relay has no default, and nothing is copied until you name a relay.
- **`CODEAF_SYNC_URL=off`** turns the copying off. Nothing leaves the computer, and `codeaf pair`
  says `sync is off (CODEAF_SYNC_URL=off); pairing needs a relay`.
- **`CODEAF_SYNC_URL=https://relay.example.com`** is your own relay. You run it with
  `codeaf relay --listen :8787 --store <directory>` (see "Running the relay yourself" on the page
  *Reaching a machine with a pairing code*). The hosted relay and your own speak the same way, so
  nothing else changes.

**Is my code private?** Everything a chat holds, its turns and the files it saved, is sealed
on your computer with a key only your own computers have, before it is sent. The relay keeps
ciphertext: it cannot read your messages, your code, or even the names of your chats. It can
see that an identity's devices talk to it, when, and how many bytes. Turning it off
stops new copies going, and a relay you run is yours to empty.

## The relay — what it sees, your own relay, and turning pairing off

A relay is a small service that passes bytes between the two computers. Your chats sync end to end encrypted. The relay only carries ciphertext. It never sees the code, the three words or your chats.

What the relay does see is short: the number at the start of the code, the time, how long each message is, and the network address of each computer. It never sees the six digits.

Pairing goes through the relay this computer syncs through: codeaf's hosted relay when this build has one and you have not chosen another, else the one `CODEAF_SYNC_URL` names, else the one an earlier pairing saved. A build with no hosted relay has no default, so there you must name one: set `CODEAF_SYNC_URL` to the address of a relay, or pass it in the command. With no relay at all, `/pair` and `codeaf pair` say:

```
no relay is set, so there is nowhere to pair through: set CODEAF_SYNC_URL to a relay's address, or pass --relay <address>
```

To use your own relay, name it on the computer that uses the code:

```
codeaf pair 42-715-302 --relay https://relay.example.com
```

When the computer that has your chats uses a relay you named, the code it shows carries that relay. Then the other computer does not have to guess:

```
  this shares your chats with the device you pair
  on it run  codeaf pair 42-715-302 --relay https://relay.example.com   (valid 10 minutes)
```

After the pairing, the other computer saves that relay and syncs through it; when it is the hosted relay there is nothing to save, because every computer reaches it without being told. The way to run a relay yourself is on the page *Reaching a machine with a pairing code*, under "Running the relay yourself".

`CODEAF_SYNC_URL=off` turns sync off. Then pairing is not available. Both commands say so before they send anything:

```
sync is off (CODEAF_SYNC_URL=off); pairing needs a relay
```

Other things the relay can say:

The relay does not answer. The name is the host you used:

```
cannot reach relay.example.com
```

The relay is full. Try again in a few minutes:

```
the relay is full right now; try again in a few minutes
```

The relay is an old one that does not know pairing:

```
that relay is too old for pairing
```

## I lost my laptop — what to do

1. On a computer you still have, run `codeaf devices`.
2. Run `codeaf devices revoke <name>` for the lost computer.
3. Treat your chats as exposed: the lost computer holds your whole identity, and revoking takes nothing back. `codeaf identity rotate` shuts it for good.

## Which door do I use — pairing, --at or --host

- **The same chats on both computers:** pair them with `/pair`. This page.
- **Work on one machine from the other, with no ssh:** `codeaf serve` on the machine that has the work, and `codeaf chat --at <name>` on the other. Read *Reaching a machine with a pairing code*.
- **Work on one machine from the other, with ssh:** `codeaf chat --host <machine>`. Read *Running on another machine*.
- **A phone:** there is no phone client in this stage.

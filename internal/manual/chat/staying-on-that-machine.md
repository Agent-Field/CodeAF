# Staying on that machine

## Staying on that machine

A conversation opened with `--host devbox` stays on **devbox** whether or not you are
attached to it. A small process over there holds it — the session, the turn in flight, and
any question that turn is waiting on — and your terminal attaches to that process rather
than being it.

That one fact is the whole of this page, and everything else here follows from it: closing
a lid costs nothing, coming back later lands you in the same conversation, a question
raised while you were away is still waiting, and two windows can watch one turn.

The shape where it is not true is a machine that cannot start a host — one where the
socket cannot be made, or an older build. There the ssh pipe is the conversation's whole
life, and losing the pipe ends the turn. aforge says which of the two you are on instead
of letting you guess.

## Does it keep running when I close my terminal

Yes, on a machine that holds sessions — which is every machine running a build with a
session host, and that is the ordinary case now.

A conversation opened with `--host devbox` does not live inside the ssh connection any
more. A small process on **devbox** holds it, and your terminal attaches to that process.
Closing the terminal, losing wifi or shutting the laptop takes away the attachment and
nothing else: the turn in flight keeps running, keeps writing the session file, and is
still going when you come back.

There is one shape where this is not true, and aforge tells you which one you are on
rather than letting you guess. If that machine cannot start a host — no room for its
socket, a state directory it cannot write, a build without one — the conversation is
served on the ssh pipe itself, exactly as older versions did, and then the pipe is the
conversation's whole life. On that shape a lost link ends the turn. The screen says so
when it matters:

```
the connection came back, but devbox does not keep a turn running while nothing is attached — that answer stopped when the link dropped, and asking again is the way back to it
```

Nothing is lost either way. The far machine is the only thing that writes the session
file and it writes it as the conversation happens.

## How quickly a dead ssh link is noticed and retried

With the defaults, an ssh connection that stops answering is noticed in about **9
seconds**: aforge asks after 3 seconds of silence and gives up after 3 unanswered asks.
The existing reconnect loop then keeps trying for up to 5 minutes. A cleanly closed link
is noticed immediately. During that gap the status line says
`reconnecting to devbox — trying for up to 5 minutes`; a message submitted in the gap is
refused visibly rather than lost.

A recent ssh connection is kept reusable for 300 seconds, so a new channel can avoid a
full handshake when the underlying ssh connection is still healthy. Its control socket
lives under this machine's aforge state directory at `~/.aforge/v3/ssh/` (moved by
`AFORGE_HOME`). A state path too long for a unix socket disables reuse only; the ordinary
ssh connection still opens.

These network-dependent defaults are editable on `/settings`' **Workspace** tab as `ssh
reuse` (`300s`), `ssh heartbeat` (`3s`), `ssh missed heartbeats` (`3`), and `ssh traffic`
(`lowdelay`). They were on the Session tab and moved, because every one of them lands on
the next launch rather than on the conversation in front of you; nothing you saved moved
with them. Setting the heartbeat to 0 turns
dead-link probes off; setting reuse to 0 stops keeping a connection after its channel
closes. Whole-stream ssh compression stays off because it usually slows a LAN attach;
large transcript frames compress themselves only when both aforge builds support it.

## I closed my laptop — did it keep going

If that machine holds sessions: yes, and you rejoin the turn part-way through.

Ask for a long piece of work from a café, shut the lid, sit down at a desk somewhere else,
run the same command. What you get is the same conversation, still going, with the part
you missed drawn in before the live tail catches up.

How much of the missed part you get depends on how long you were away. The machine keeps
the recent events of a **running** turn in memory so a returning window can be caught up —
several minutes of a long reply. If you were away longer than that, the oldest of it has
fallen away and you are shown what is still held. That is not a failure and nothing is
lost: the session file on that machine has the whole turn, and `/resume` or reopening the
conversation reads it.

If the turn **finished** while you were away, there is nothing to rejoin — the reply is in
the conversation, which is where you find it when you come back.

## Coming back later to the same conversation

Run the same command. `aforge chat --host devbox` opens the conversation that machine
already has for that workspace, whether it has been thirty seconds or a week.

There is nothing to reconnect to by hand and nothing to name. The machine keeps one
conversation per workspace open; a hello that names no session file asks for "the one for
this workspace", which is what your command means when you do not use `--session`.

**Conversations you finished with are let go of.** A conversation with nobody attached, no
turn running and no unanswered question is closed after 30 minutes, and the file is
flushed as it goes. Opening it again reads the same file back — a closed conversation is
not a lost one, it is one that is on disk instead of in memory.

To open a **different** one on that machine rather than the current one, `/resume` lists
what it has and opens the one you pick.

## It asked me something while I was away

It waits for you, and it is there when you come back.

A conversation can stop and ask: may this command run, should this reminder stand, should
this saved program take the turn, may this account be connected. Those questions used to
need somebody watching at the exact moment they were raised. On a machine that holds
sessions they do not: a question raised with no window attached is **kept**, the turn stays
stopped on it, and the next window to attach is handed it along with how long it has been
waiting.

So a long piece of work you left running does not fail four hours ago because nobody was
there to say yes. It is sitting where it stopped.

Four kinds of question wait this way: a permission question about a tool call, a reminder
or watch asking to stand, an offer to run a saved harness, and a request to connect an
account. Answer it exactly as you would have answered it live — it is not a different kind
of card, it is the card you would have seen, with the same keys and the same offer.

**It tells you how long it sat there**, on a line of its own just above the card:

```
this question has been waiting 4 hours
```

Minutes, hours or days, in words. A question raised in the last minute says nothing at all
about having waited, because "you were only just away" is not news.

One thing this cannot do: if the machine over there is a **newer build** holding a kind of
question this one has never drawn, that question is skipped rather than guessed at, and it
goes on waiting for a build that knows it. A card you could see and could not answer would
be worse than one you were never shown.

Two things are worth knowing. A question that was **on your screen** when you walked away
is kept too, and comes back the next time you attach. And a question waiting for you keeps
that conversation open — the machine does not let go of a conversation that is holding one.

A new window receives every unanswered question even if the old window is still
connected or its broken connection has not yet been noticed. The question appears once
in the new window, even when the running reply also replays it. Answering it resolves
the same question for the conversation.

## Two windows on one conversation — two terminals on the same chat

More than one window can be attached to the same conversation at once — a desk machine and
a laptop, two terminals on the same chat, or you and somebody else. Every window sees the
same turn as it happens.

**The keyboard follows the newest window.** Exactly one of them can type at a time, and it
is the one that opened the conversation most recently. Nothing is closed and nothing is
refused: the others stay attached, keep drawing every reply as it arrives, and get their
composer back the moment they ask for it. See *Someone else is typing — why can't I type*.

The rest of what you should know about sharing one:

- **A question is answered once.** Say yes to a permission card on the phone and the
  conversation has its answer. A second window may still have that card drawn, but
  pressing it decides nothing — a late answer to a question that has been settled is
  dropped, which is what the same card does locally when a turn has moved on.
- **Ending the conversation ends it for everybody.** Quitting deliberately closes the
  conversation and flushes the file, and that is a statement about the conversation rather
  than about your window. Simply closing a window — or losing its connection — leaves
  everything running for the others.
- **Only the machine holding the session knows how many of you there are**, so the count
  in the entry notice comes from over there. Opening the conversation here says, as you
  come in:

  ```
  another window is on this conversation — typing is here now
  ```

  or, for more than one, `2 other windows are on this conversation — typing is here now`.
  When you are alone — the ordinary case — nothing is said at all.

## Opening aforge in a second terminal here — two terminals on this machine

Sharing is not only for two machines. When a conversation on **this** machine is held by a
session host, `aforge` typed in a second terminal in the same workspace **joins it** rather
than opening one of its own — the same room, the same turn, one keyboard.

What the second terminal shows is exactly what a second machine shows. The newest window
gets the keyboard, so the one you just opened has the composer and the older one drops to
one dim line reading `typing from another window now · enter takes it back` — `another
window` rather than a machine name, because there is no other machine in it. The status
line grows no `via` segment for the same reason.

**A host is started for you.** `aforge chat` in a folder opens its conversation in this
machine's session host, and starts one if none is running. That is what makes the work
outlive the terminal: close the window mid-task and the task keeps going; open a terminal
here tomorrow and you are back in the same conversation rather than beside it. The host
retires itself when it is holding nothing.

**Four launches stay in this terminal instead**, each because something real about them
lives in this process:

- **the first run on a machine with no key.** Connecting a provider is a conversation with
  you, and a background host has no terminal to have it in. Once a key is set up, the next
  launch takes the host road.
- **`--once`**, which never starts a host — a resident process left behind by a headless
  command is a surprise — though it joins one that is already there.
- **`--debug`**, because the model-call record is written by the process making the calls.
- **`--no-host`**, the escape hatch, for the day the host is the thing that is wrong.

**The per-launch postures travel with the launch.** `--yolo`, `--no-compact`, `--one-model`,
`--max-hours` and `--max-cost` describe how a session is BUILT, and the host builds it that
way. A conversation that is already open keeps the shape it was opened with — nothing here
overwrites a session somebody else is in — so if you ask for one shape and this folder's
conversation is already running under another, aforge says so in one line and opens a
conversation in this terminal instead, where the flag is real. That one ends when the
terminal does.

## What typing from spark now means, and why my input box is one line

It means another window on this conversation has the keyboard, and this one is watching.
Where your input box was, there is one dim line instead:

```
typing from spark now                                     enter takes it back
```

`spark` is the machine the other window is on — the name that machine calls itself, with
any domain trimmed off it. A second window on **this** machine reads as `another window`
instead, which is what aforge calls a conversation open somewhere else everywhere. A
machine that could not say its own name gets `another window` too.

The line is the whole of the change. The transcript above it, the status line below it and
every other key on the screen are exactly as they were.

## Someone else is typing — why can't I type

Because another window on this conversation has the keyboard, and yours is watching. What
you are looking at is the line above.

**Nothing is lost and nothing is closed.** The window is still attached: replies arrive
live, the transcript is complete, you can scroll it, copy out of it, answer a permission
card, press `esc` to interrupt, and walk to any other place with the usual keys. What you
cannot do is send a message — and typing characters does nothing at all, because there is
no box on the screen to put them in.

**Your unsent draft is kept.** It is not cleared, not sent and not lost. It is exactly
where you left it when the box comes back.

## Take over the keyboard — enter takes it back

Press `enter`. The keyboard is yours in one round trip on the connection that is already
open — nothing reconnects, nothing restarts — and the other window becomes the watcher in
the same instant, told by the machine holding the session rather than finding out when
somebody types.

Why `enter` and not the first letter you type: taking the keyboard off another machine is
a thing to mean, and asking for it is one round trip, so the first letter of every sentence
would be racing a call over the wire. `enter` is already the key that means "my turn to
speak", and the line on screen says so.

There is no lock and nothing to release. Whoever pressed `enter` most recently has it, and
the window that lost it keeps its own draft, its own scroll position and the whole
conversation.

**Walking away instead:** two spaces in an empty box still open home, exactly as they do
when you are typing, so you can leave the conversation running in front of the other
window and get on with something else on this machine.

## If I type into the wrong window

You get one sentence back from the machine holding the conversation:

```
the keyboard is on spark right now — press enter here to take it back
```

or, for a window on this same machine, `the keyboard is in another window right now — press
enter here to take it back`.

**A message is never dropped in silence.** It either lands or it is answered — this is the
answer. It only comes up in the seconds where the keyboard moved while your message was
already on the wire, because a window that is watching has no send key to press: `enter`
there asks for the keyboard instead.

## Does the other window see the turn I started

Yes, as it happens. The message you send is drawn in the watching window above the reply,
and the reply arrives there token by token exactly as it does here — not on a refresh, not
when somebody touches it. That is the whole point of leaving the other window open: it
goes on showing the work.

A turn that was already running when a window attaches is picked up part-way through in the
same way, with the part it missed drawn in first.

And so is a turn nobody in the room started: a finished task's landing note starts a turn
of the conversation's own, and it streams to every attached window the same way — told the
moment it begins, drawn as it runs, closed when it ends. A window that attaches while it
runs picks it up part-way like any other running turn.

## Does my window come back and steal the keyboard after my wifi drops

No. A connection coming back is not somebody arriving.

If your link drops and the surface redials itself, it rejoins as it left — and if you have
walked to another machine and started typing there in the meantime, the returning window
comes back as the **watcher**. It takes the keyboard only if nothing else has it.

That is the one place "the newest window drives" is deliberately not literal, and the
reason is that a lid you closed in one city reconnecting half an hour later is not a person
sitting down.

## What happens to the keyboard when a window closes

It goes to the newest window still attached, so whoever is left can always type. Nobody has
to ask for it and no line appears — the watcher's line simply goes and the box comes back.

If the window that closed was the watcher, nothing moves at all.

## Nothing to set up on that machine

Nothing at all. There is no daemon to install, no port to open, no service to enable, no
configuration file.

The first time you connect, `aforge engine` looks for a host for that workspace and starts
one if there is none. That is the whole of the installation: the first attach is the
host's birth. It listens on a unix socket under that machine's own state directory —
`~/.aforge/v3/hosts/`, moved by `AFORGE_HOME` like everything else aforge keeps — and
never on a network port, so nothing about it is reachable from outside that machine. Your
ssh is still the only door in.

Two things are still required, and they are the same two `--host` has always needed:
aforge installed on that machine, and `ssh <machine>` already working from where you are
sitting.

If none of it can be set up — a socket path too long, a directory that cannot be written,
a spawn that fails — the connection is served on the ssh pipe instead and the conversation
works normally. It simply does not outlive the pipe.

## When does the thing holding my session go away

When it has no conversation left to hold.

A conversation with nobody attached, no turn in flight and no unanswered question is closed
half an hour later. Once the last of them is gone and no window is attached, the host
itself leaves two minutes afterwards. The next connection brings a fresh one up in about a
second, and you never see it.

**A turn still going is never idle**, and neither is a conversation holding a question for
you. That is the point of the whole arrangement: the long piece of work you left going
keeps that machine's attention until it is finished, and only then does the ordinary clock
begin.

**Reminders and watches are not affected by this.** They are not held by that process:
their pass is done by whichever aforge is up — any open window, or the timer on that
machine that calls `aforge tick` with nobody sitting anywhere. A host going away hands
their timing back to that timer exactly as closing a terminal always did.

**And it goes early when aforge on that machine is rebuilt under it.** It is a running copy
of the build that started it, so a new binary at the same path does not replace it; it
notices the file it was started from has been removed or rebuilt and retires the next time
it is holding nothing — no window attached, no turn running, no question waiting. What it
was holding is closed properly on the way out and every transcript is flushed. Nothing that
was still going is cut short for this.

## How do I stop the old engine holding my session — aforge engine --stop

Run it on the machine that is holding it:

```
aforge engine --stop
```

Its help text reads:

```
stop whatever is holding this workspace's conversations on this machine
```

It stops whatever holds that workspace, whichever build it is — including one too old to be
asked politely — after closing its conversations and flushing their transcripts. A turn it
catches stops where it is and keeps its partial reply, the same thing ctrl+c does locally.
Then it says one of these, naming the directory:

```
stopped holding /home/you/api — the next connection starts fresh from this build
nothing is holding /home/you/api here
```

`--workspace` picks which one; with no flag it means your home directory, exactly as it does
for `aforge engine` itself. The reason to type it is that a connection told you to: both
refusals about an older aforge still holding a machine name this command (*Running on
another machine*).

## What still does not work, even though the session stays open

Two things.

- **An adaptive run.** A hosted conversation is built with no adaptive runner. Its notes,
  its gauge and its spending gate arrive on a standing subscription that this wire does not
  yet carry, and unlike the harness lane that subscription replays nothing — so a gate
  raised while every window was away would be gone when you came back, and the run would sit
  at its cap for ever. Nothing is lost by the wait: no command, tool or sentence starts an
  adaptive run in this build anyway, here or on a far machine.
- **`/subharness`** lists what is saved on the machine you are sitting at, so over a
  connection it has nothing to show you and says so. The far session can still OFFER a
  saved program with an intake card, and answering that card runs it over there.

The list used to be three long. Harness building and subharness intake cards came off it
when the wire grew their subscription and their answer: a card raised while nobody is
attached is handed to the next window that arrives, once, and answering it takes it down.
Permission cards, reminders, harness offers and account connections wait for you as they
already did.

Nothing here half-works: a capability a road cannot carry is absent rather than present and
failing, which is why the model is not given a verb it could not finish.

## What does --no-host do — make one window not use the session host

There are two of it, one for each end, and they mean the same thing: do not look for a
session host, do not start one, open this conversation right here.

**`aforge chat --no-host`** and **`aforge resume --no-host`**, typed on the machine you are
sitting at. Its help text reads:

```
open this conversation in this process instead of attaching to this workspace's session host
```

Without it, `aforge chat` joins this workspace's host when one is already up. With it, the
conversation is built in this terminal's own process whatever is up. The reason to type it
is that something about the host itself is wrong and you want the floor rather than the
feature. It changes nothing in a workspace no host is holding, which is most of them.

It cannot be combined with `--host` or `--at`: over those the conversation is on the far
machine either way, and naming both is refused rather than ignored.

**`aforge engine --no-host`** serves one *connection* on the pipe, the old way. Its help
text reads:

```
serve this conversation on the pipe instead of attaching to a session host
```

You would type that one on the far machine, or put it in the command yourself, for the same
reason.

There is a second flag beside it, `--daemon`, whose help text reads:

```
hold this workspace's conversations and answer surfaces on a socket
```

That one is machinery: it is how a host is started, by the attaching process, and there is
nothing a person accomplishes by typing it. The third flag beside them is `--stop`, which is
the one a person really does type; it has its own section above. None of them appear in
`aforge`'s usage text, because `aforge engine` itself does not — it is the far half of
`--host` and a surface dials it.

## Background replies while another reply finishes

On a hosted conversation, a finished task or background command can start a reply
without another message from you. It appears as a new turn, preserving earlier
answers above it. If your window is still drawing the previous reply, it finishes
that stream before drawing the queued reply. Returning midway through a reply uses
the same stream and its recorded events.

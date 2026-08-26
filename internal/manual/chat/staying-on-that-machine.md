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

## What still does not work, even though the session stays open

Three things, and all three for the same reason: their questions do not travel this
connection at all — not the card, and not the answer.

- **Building a harness.** Asking for a harness to be designed is off over `--host`. The
  card that asks whether to keep the finished page is raised on a lane that has no door on
  this wire, so it would never reach you and holding it is not possible either. Running a
  harness that already exists works normally.
- **An adaptive run.** Its notes, its gauge and its spending gate all arrive on the same
  kind of lane. A run started over a connection would spend money and stop at its cap with
  nothing on your screen, so the model is not given the verb at all.
- **Offering a saved program with an intake card.** Same lane, same answer. `/subharness`
  is a command on the machine you are sitting at and has nothing to list over a
  connection.

This is a smaller list than it was — permission cards, reminders, harness offers and
account connections all wait for you now. What is left is not about anybody being in the
room; it is about a road that has not been built. Nothing here half-works: each one is
absent rather than present and failing.

## Make it not hold the session

`aforge engine --no-host` serves that one connection on the pipe, the old way, without
looking for or starting a host. Its help text reads:

```
serve this conversation on the pipe instead of attaching to a session host
```

You would type this on the far machine, or put it in the command yourself, and the only
reason to is that something about the host itself is wrong on that machine and you want
the floor rather than the feature.

There is a second flag beside it, `--daemon`, whose help text reads:

```
hold this workspace's conversations and answer surfaces on a socket
```

That one is machinery: it is how a host is started, by the attaching process, and there is
nothing a person accomplishes by typing it. Neither flag appears in `aforge`'s usage text,
because `aforge engine` itself does not — it is the far half of `--host` and a surface
dials it.

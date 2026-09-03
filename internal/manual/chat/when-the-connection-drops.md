# When the connection drops

## My wifi died in the middle of a reply

The surface redials the machine by itself. You do not have to do anything.

A conversation opened with `--host` runs on the other machine; the link between the two is
the only part a café's wifi, a VPN flap or a sleeping laptop can take away. When the link
dies without a goodbye, aforge opens another one, tells the far machine how far your
screen got, and the reply carries on from there. Text you had already been shown is not
drawn a second time, even when the far machine sends a little of it again.

**It keeps trying for 5 minutes.** The first retry is a second later, and the pause
doubles up to fifteen seconds between attempts. If the machine answers in that window, the
conversation continues as if nothing happened. If it does not, you see:

```
the connection to devbox is gone — run the same command to pick the conversation back up
```

Two things are true whatever happens. Your draft, your scrollback and your input history
are on the machine you are sitting at, so they are never at risk. And the conversation is
journaled on the far machine as it happens — the far machine is the only thing that writes
that file — so nothing that reached it is lost either way.

## I closed my laptop and opened it somewhere else

Under about five minutes, the surface will have redialled by itself and you are back in
the same conversation.

Longer than that, the redialling has given up and the window says the connection is gone.
**Run the same command again.** That is the whole recovery: the session file is on the far
machine's disk and the same command opens it again.

What you get back when you return depends on what is running over there. If that machine
keeps conversations alive between connections, a turn that was in flight is still going
and you rejoin it part-way through. If it does not — plain `aforge engine` on a pipe, one
conversation per connection — then the turn ended when the link did, and the reply it
was part-way through is not coming back. aforge says which of the two happened rather than
letting you guess; see *why did the reply not finish when it reconnected*.

## Did I lose my work

No. Nothing that reached the conversation is lost when a connection drops.

The far machine writes the session file, and it writes it as the conversation happens, not
at the end. Everything said before the link died is in it. When you open the conversation
again — by the redialling that happens for you, or by running the same command — it is all
there.

What can be lost is **the tail of a reply that was still being written**, and only
against a machine that does not keep a conversation running while nothing is attached. In
that case the turn ended where the link did. Ask again; nothing else about the
conversation moved.

Your draft is not at risk either: what you have typed and not sent lives on the machine
you are sitting at, and so do your scrollback and your input history.

## It says reconnecting

You see this when you try to send something while the link is being redialled:

```
reconnecting to devbox — try that again in a moment
```

It is not a failure. The surface is dialling the machine again and your message was not
sent, so press enter again once it is back — usually a second or two later. The status
line is saying the same thing at its other end while this is going on; see *how do I know
it is reconnecting*.

The redialling lasts 5 minutes. After that the connection is declared gone, in the one
sentence aforge has always used for that:

```
the connection to devbox is gone — run the same command to pick the conversation back up
```

**A file does not come across while the link is down either.** Clicking a path, `/files
<path>` and the browse page all fetch their bytes over this same connection, so during a
redial a click answers `reconnecting to devbox — try that again in a moment` rather than
opening. Nothing is broken by it: the addresses this window minted go on working once the
link is back, and a file already fetched opens from the copy on this machine without
asking that machine anything (*Opening files from that machine*).

## How do I know it is reconnecting — the segment on the status line

The status line says so, in one segment at its right-hand end, beside what the session is
doing:

```
reconnecting to devbox — trying for up to 5 minutes
```

It names the machine, because somebody with three windows open needs to know which one
lost its link, and it names how long it will keep trying, because that is the difference
between waiting and running the command again. **A narrow terminal never drops it**:
everything else on that end of the line is a number, and this is the reason none of those
numbers are moving.

**The reconnecting sentence replaces the healthy round-trip reading.** After a measurement
has answered, a working connection reads like `devbox · 3ms`; before the first answer it
draws nothing, never `0ms`. There is no badge, icon or "connected" word. While the link is
down no latency check is sent, and the reconnecting sentence takes the segment until the
link comes back.

On a narrow phone-width terminal the row has no room for a sentence that long, and it
moves into the status sheet with the rest of the numbers. `/status` prints it there too,
under `connection`.

## Why did the reply not finish when it reconnected

Because the machine you are working on does not keep a turn running while nothing is
attached. The connection came back; the work that was in flight did not. The turn says so
itself:

```
the connection came back, but devbox does not keep a turn running while nothing is attached — that answer stopped when the link dropped, and asking again is the way back to it
```

A line lands in the conversation beside it saying the same thing about the window rather
than about that one turn, and it is said once and not again:

```
devbox does not keep a turn running while nothing is attached, so the turn that was in flight did not survive the drop
```

That is the honest half of roaming. A machine running a persistent aforge holds your
session between connections, so a redial rejoins the turn where it was. A plain
`aforge engine` started by ssh for the duration of one connection cannot: when the pipe
died, so did it, and the redial reached a **new** one opened on the same session file.

The conversation itself is intact — the file is the same, everything already said is in
it, and the new connection works normally. Only the unfinished reply is gone. Ask again.

## It came back with a different conversation

If the far machine answers a redial with a different session file than the one this window
was in, aforge does not swap the conversation under you in silence. The turn that was
running ends with:

```
devbox opened a different conversation, so this turn is not coming back
```

and a line lands in the conversation, once, saying which half of the screen belongs to
which:

```
devbox came back with a different conversation open than the one this window left — what is above is the old one, and anything from here on belongs to the new one
```

This is rare, and it means the machine's idea of "the session for this workspace" moved
while you were away — most often because something else opened one there. What is on your
screen above belongs to the conversation you left. If you want it back, `/resume` lists
what that machine has and opens the one you name.

## Someone else is on this conversation

More than one window can be attached to the same conversation on the far machine — two
people, or you and a laptop you forgot to close. When there is, the entry notice says so
as you come in:

```
another window is on this conversation — typing is here now
```

or, for more than one, `2 other windows are on this conversation — typing is here now`.
The count comes from the far machine, because only the machine holding the session can
know it. When you are alone — the ordinary case — nothing is said at all.

The second half of that sentence is what opening it here just did to the other window:
**the keyboard follows the newest window.** That window is still attached and still drawing
every reply; it simply cannot type until somebody presses `enter` there. The whole of it —
the line it shows, how to take the keyboard back, and what a redial does — is on *Staying
on that machine*.

## My window came back and now I cannot type

Because you started typing somewhere else while it was away, and the keyboard went with
you.

A connection coming back is not somebody arriving, so a redial does **not** take the
keyboard off the machine you walked to. The returning window rejoins as a watcher and says

```
typing from spark now                                     enter takes it back
```

Press `enter` there if you want it back. If nothing else had the keyboard while you were
gone, the returning window simply has it and says nothing.

## Does quitting end what is running over there

Closing the surface deliberately and losing a connection are different events, and only
one of them is a person leaving.

- **You quit** (`ctrl+c` twice, or closing the window): the connection is closed on
  purpose and nothing is redialled, because you did not lose it. What becomes of the
  conversation is the far machine's answer — one that does not keep sessions alive stops
  the work and flushes the session file, one that does keeps going and the same command
  walks you back into it.
- **The link dies**: the surface redials for up to 5 minutes without telling the far
  machine anything, and the far machine is free to carry on with the turn.

A connection you closed yourself reads `this connection is closed` rather than the
sentence about a connection that is gone. That difference is deliberate: one of them is
news and the other one is you.

## Why does it take a moment before anything happens again

The first redial is a second after the link died, and the pauses double from there up to
fifteen seconds — so a link that stays down is tried roughly at one second, three, seven,
fifteen, thirty, and every fifteen seconds after that, for 5 minutes in total.

The pauses grow on purpose. Every attempt starts a fresh `ssh` on the machine you are
sitting at, and a hundred of those against a machine that is switched off is your laptop
working for nothing. A blip is caught by the first retry; a machine that is really gone is
not worth hammering.

Every call aforge makes over a connection already gives up after 10 seconds, so nothing
about a dead link can leave your terminal frozen while this is going on.

## It said something "fell over once and will be tried again"

That sentence is about one of the readings the surface keeps warm in the background over a
connection — the places behind home, what has been spent, what is remembered, the standing
items — and it means exactly what it says: the reading broke once, inside aforge, and it
will be asked for again on the next beat as if the far machine had simply not answered.
It is shown once, on the line where the connection's own one-off news appears, and never
repeated, even if the same reading keeps breaking. The full record of what broke goes to
the log file the surface writes beside the profile, `chat.log`.

```
reading what has been spent over this connection fell over once and will be tried again
```

The four readings are named `keeping the places current`, `reading what has been spent`,
`reading what is remembered` and `reading the standing items`. Nothing on the page is lost:
a page keeps drawing the last answer it was given until a new one arrives, and a page that
had never been answered draws nothing rather than a guess. A background reading is never
started when there is no connection to make it on, so a surface that has no machine behind
it is quiet rather than repeatedly failing.

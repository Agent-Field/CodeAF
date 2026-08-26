# Remote access — the architecture

How a v3 session runs on one machine and is used from another. Written as
decisions with the alternative that was rejected, in the manner of
`ARCHITECTURE.md` and `CHAT-V3.md`.

This is the **v3 surface's** remote story. The v1 resident (`internal/head`,
`internal/resident`) is a different product in the same binary and shares none
of this vocabulary — see CLAUDE.md. In particular the "session host" below is
**not** the resident: it is a lifetime for a conversation you sit in front of,
not an employee that works while you are away.

Product framing, the personas, and the road not taken live in
`docs/remote-access-design.md`; the build order lives in
`docs/remote-access-plan.md`. This document is what the code actually is.

## The one-sentence design

A conversation lives on the machine that owns the work; a surface **attaches**
to it over any transport that can carry a byte stream, and the same JSON-line
protocol runs inside every one of them.

---

## Decision 1 — The wire is an envelope, and the payloads are the session's own types

**Decision.** `internal/remote/wire.go` defines a `Frame` — kind, id, method,
payload, error, seq — and nothing else about meaning. Payloads are
`internal/session`'s own structs carried as JSON. Both halves compile against
`internal/session`, so a field added there travels with no wire change. The one
exception is `EventWire`, because `error` does not survive `encoding/json`.

**Why not a schema-first protocol (protobuf, an IDL).** The two halves are one
program, built from one tree, shipped as one binary. A schema layer would buy
cross-language compatibility we will never spend and cost a translation step on
every field anyone adds to a session event — which is the field churn that
actually happens. The version door (Decision 3) already covers the risk a
schema would manage.

**Why one JSON line per frame.** It is the journal's own framing, for the
journal's own reason: a torn write is one lost line, not a lost stream. It also
means the protocol can be read by a person with `head`, which has already paid
for itself in debugging. Payloads at least 32KB may be gzip-compressed inside
that envelope after the hello and welcome agree on it; the kind, id, method,
encoding and line boundary remain readable, and small interactive frames remain
plain JSON.

## Decision 2 — The transport is an `io.ReadWriteCloser`, and nothing in the protocol knows what it is

**Decision.** `remote.Dial` takes an `io.ReadWriteCloser`; `remote.Serve` takes
a reader and a writer. Spawning ssh belongs to the door in `cmd/aforge`, which
owns processes and flags. The protocol has no opinion about the pipe.

**Why.** This is the single most load-bearing decision in the remote lane, and
it was made before there was a second transport to justify it. Every carrier
added since — a unix socket to a local session host, an end-to-end encrypted
tunnel through a relay, an in-memory pipe for tests — plugs in without touching
a frame. A protocol that had reached for `exec.Cmd` or a socket type would have
had to be rewritten once per carrier.

**The corollary that keeps it true:** `internal/remote/loopback.go` fakes the
transport and *only* the transport. A test drives the real client against the
real server over `net.Pipe`. If a change ever makes the protocol care what it
is riding, that file stops compiling, which is the alarm we want.

## Decision 3 — A version mismatch is a refusal at the door

**Decision.** The hello and the welcome both carry `Version`. A mismatch is
refused during the handshake, before the surface takes the screen, with a
sentence naming the fix.

**Why not negotiate down.** Two builds that might disagree about a frame must
not find that out three turns into a conversation. Negotiation also multiplies
the states we have to be correct in by the number of versions in the wild,
and the fix — update the older machine — is one command the person can run.

**Capabilities are not versions.** Additive optimizations may be offered in the
hello and selected in the welcome when their absence preserves the old wire.
Frame gzip is that shape: an old engine ignores the offer and answers plain JSON;
a new engine sends plain JSON unless the surface offered gzip. This does not
negotiate protocol semantics down — a version mismatch is still refused.

**Why the handshake happens before the TUI starts.** Everything that might ask
the person a question happens on a plain terminal: ssh's passphrase prompt, its
unknown-host-key question, and this refusal. A TUI that came up first would
either eat those questions or draw a frame over them.

**Version 4 adds the empty `Ping` call.** The surface sends one only on its slow
connection clock and times the answer locally; no timestamp crosses between
machines whose clocks may disagree. This is still a protocol change, so the
version moved and the mismatch is still refused here, at the door, with the
same sentence naming the fix.

## Decision 4 — Version 2 separates a conversation's life from a pipe's

**Decision.** In version 1 the engine *was* the ssh command: it read frames on
stdin, and when the pipe died the turn died with it. Version 2 makes a session
something a surface **attaches to**, and adds exactly the four things that
separation requires:

| Addition | What it makes possible |
|---|---|
| `Frame.Seq` + `Hello.Resume` | a reattach replays the gap, not the conversation |
| `MethodDetach` | "this surface is leaving" stops being indistinguishable from a dead pipe |
| `SubmitFilesArgs` + `MethodFetchFile` | attachments travel both ways |
| `HeldQuestion` | a card raised with nobody attached waits instead of expiring |

`Welcome` gains `Live`, `Attached`, `Held` and `Persistent`.

**Why sequence numbers rather than replaying from the journal.** The journal is
the authority on a *finished* turn, and the surface reads it anyway. What it
cannot answer is the turn in flight: the journal does not hold a partial
reply's deltas, and the transcript would hand a surface the finished shape of
something it already half-drew. So a running stream's events are numbered and
kept in memory, the returning surface says how far it got, and the engine sends
what came after. Numbering from 1 makes zero mean "I have seen nothing of this
stream", which is the state of every first attach.

**Why `Detach` is worth a method of its own.** It is the fact version 1 could
not express. A closed window and a dropped connection were one event, so both
had to be read as an interrupt to be safe — which was right about the window
and wrong about the connection, and the person experienced it as losing a
running turn to a closed laptop lid. Three roads out now read as three
different things: `Close` ends the conversation, `Detach` leaves it running,
and a torn pipe means the surface will be back.

**Why every version-2 field is additive and `omitempty`.** So the file stayed a
superset rather than becoming a second protocol. This deliberately does *not*
make the versions compatible — the door still refuses a mismatch, and it must,
because a version-1 engine would silently interrupt a turn a version-2 surface
believed it had merely detached from.

## Decision 5 — `Persistent` is stated, never inferred from the transport

**Decision.** `Welcome.Persistent` says whether the far end is a session host
that outlives this connection, or a one-shot engine on a pipe. Both are
legitimate shapes and both speak this protocol.

**Why.** A SURFACE MUST NOT PROMISE A LIFETIME THE ENGINE DOES NOT HAVE.
"Close the lid, it keeps going" is true against a host and false against
`aforge engine` started by hand on a machine with no host — and the person
cannot see which they have. Inferring it from the carrier would be wrong in
both directions: a unix socket does not imply a host, and an ssh pipe does not
preclude one. So it is a fact the engine states about itself, and every screen
that makes a promise about detaching reads it.

## Decision 6 — The engine machine is the authority, always

**Decision.** The tool gate, the spend rail, the API key, the harness registry,
connected accounts, the session file, standing items, and every path on the
wire belong to the machine that runs the work. The surface owns the screen, the
keyboard, the draft, and the input history.

**Why it is stated as an architecture decision and not left to taste.** Every
honesty bug this lane has had came from a screen answering for the wrong
machine. The YOLO badge is drawn from the engine's own profile because a badge
read off the laptop would be a safety claim about a machine nobody consulted.
Standing items are counted on the engine's disk because they run there.
`/export` said `· on this machine` because that was the truth about where the
bytes landed. A surface that resolved any of these locally would be
comprehensible, fast, and wrong.

**The corollary for new capabilities:** when a capability cannot cross, it is
ABSENT rather than broken — the model is not given the verb, and the manual
says so in the refusal's exact words. What is *not* acceptable is a capability
that is present and fails every time it is called.

## Decision 7 — Attachments travel as bytes and are remade on arrival

**Decision.** A path typed on the surface means nothing on the engine, so
anything a person puts into the chat travels as bytes; the engine writes it
into that session's own folder and journals a path that is true on the machine
that owns the journal.

**Why.** `internal/session`'s image law came first and generalizes exactly: a
journal holds a *reference* to a file and never the bytes, so a transcript
stays readable and a resumed session is not re-sending base64 forever — and a
reference is only worth writing if it names a file that exists on the machine
that wrote it. Every other kind of attachment has the same problem and, before
version 2, had no answer at all.

**Why the model is told a path rather than the contents.** An attached file is
a file, and the session already has a `read` tool. Inlining a 4MB CSV would
spend the context window on bytes nothing has asked for yet.

**Why the reverse door is the engine's refusal to make.** A surface cannot know
the far machine's boundaries, so a client-side check on `Fetch.File` would be a
permission decision taken on the wrong machine.

## Decision 8 — The pairing crypto is borrowed, and the one piece nobody maintains is owned

**Decision.** Pairing runs CPace over the six-digit code; the pairing channel and every
later connection run Noise (`NNpsk0` then `IK`) between pinned device keys; the relay
proves possession of its registered key with stdlib ECDH and HMAC. None of it is written
here — except `filippo.io/cpace`, which is **vendored into `internal/pair/cpace`** with
its BSD licence and its own tests.

**Why vendor exactly that one.** It was the only dependency in the set that has never
been tagged: a 2021 snapshot with no release behind it and nobody promising to fix it.
Two hundred lines is small enough that one person can read all of it — which is more
assurance than an unowned dependency was giving — so the copy is a deliberate trade:
**the bug is ours now**, chosen rather than arrived at by drift. `flynn/noise` and
`gtank/ristretto255` stay as ordinary dependencies, because both are tagged releases in
wide use. The vendored file carries a header naming its exact upstream version, the
command to diff against it, and what reading it turned up.

**Why not write the PAKE.** For the same reason nothing else here is hand-rolled: a
plausible-looking PAKE that is subtly wrong is indistinguishable from a correct one until
somebody attacks it. The house rule for this lane was that a hand-rolled primitive which
*looks* like it works is the worst available outcome — worse than the capability being
absent.

**The property this buys, stated precisely.** The relay brokers an introduction it cannot
listen to. CPace means a wrong code yields a different key rather than an error, so there
is no oracle to guess against and no offline dictionary attack; the guess is spent before
the exchange runs. Noise `IK` then means every later connection authenticates the machine
before the surface sends anything, and the initiator's identity is encrypted — the relay
cannot even tell which of your devices connected.

## Decision 9 — The device key is a file, and the page says so

**Decision.** A machine's long-term key lives at `~/.aforge/v3/remote/device.key`, mode
0600. The OS keychain — and with it a Mac's fingerprint prompt — is a seam (`pair.Keeper`)
with no implementation.

**Why ship without it.** The file is exactly the exposure of an ssh key with no
passphrase, which is the credential this same person already trusts to reach the same
machine over `--host`. That is a true and familiar bargain. A keychain implementation is
Mac-only platform work needing cgo and a Mac to test on, with Linux and Windows each
wanting their own — a project, not a finishing touch.

**Why it is written down rather than left quiet.** Somebody who assumes a keychain will
protect a stolen laptop is worse off than somebody who knows it is a file. So
`aforge devices` names the path on screen, the manual has a section saying the keychain is
**not built** and what that means in practice, and a test fails the build if that page
ever starts promising otherwise. When the seam is filled, `Keeper.Where()` is the one
sentence that changes.

## Decision 10 — A place follows the machine the session is on, and one door serves five of them

**Decision.** The seven places are a listing of one machine's disk, and over a
connection that machine is the one the SESSION runs on. `Places.World` answers
`session.ReadWorld(session.PlacesRoot())` on the engine, and `Welcome` carries
the root it was walked under. The surface reads it through one seam
(`tui3.Options.World`), backed by a cache the door keeps warm
(`cmd/aforge`'s `hostWorld`), and **a hosted surface with no seam reads nothing
at all** rather than falling back to its own disk.

The engine door also adds that machine's deliverables index to the world. Home
draws those rows under the far conversation that made them and opens their paths
through the same fetch door as a path in a reply; it never joins a far session
id to the surface machine's index. `/export` remains deliberately local and
records into the surface machine's index because that is where its file lands.

**Why one door and not one per place.** Five of the seven are built from that
single walk — home lists it, tasks reads the task rows inside it, standing walks
its projects to ask the far store what else stands there, spend joins its titles
onto the ledger's ids, and search opens a hit's conversation out of it. A method
per place would have been five round trips answering one question, and five
chances for two screens to disagree about which machine they were describing.

**Why the seam answers whether it has an answer.** An empty list of standing
items and no answer yet are the same thing on a screen — the emptiness law draws
both as nothing. An empty WORLD is not: it is a machine with no projects, and
`nothing here yet — say something and this fills up` drawn over a server full of
work is the one wrong sentence home can say about somebody else's disk. So the
seam is `func() (session.World, bool)` and the surface draws nothing until the
far machine has spoken once.

**Why it is a cache and not a call.** A place may read on its open and on its
three-second beat, and over a wire both are moments a person is waiting through:
a call carries a ten-second deadline, so an open that made one would be a
terminal that stopped answering keys for as long as the far machine took. This
is `hostStanding`'s law applied to the reading five screens share.

**What is still local, and says so.** Spend reads the ledger file, search reads
the conversation index, memory reads the memory store — three files on the
machine this process runs on, with no door on the wire yet. Each place opens and
draws one dim line where its rows would be (`tui3`'s `place.remote`), which is
the corollary of Decision 6 stated for a screen rather than for a capability.

**And two things the surface must not ask its own disk.** Home's `readGone`
stats every project it draws; over a connection those paths are the engine's, so
the stat is not made and no row is marked `folder gone` — a stat here would
report every remote row as deleted. And the look stamps behind a tab's number
are kept per machine, in `~/.aforge/v3/looks/<machine>` on the SURFACE's disk:
what changed belongs to the far machine, when you last looked belongs to this
terminal, and one stamp answering for both would let a glance at the server clear
the badge over the laptop's own tab.

## Decision 11 — The room has one keyboard, and the engine says whose

**Decision.** A `Session` names one attached surface the **driver**. The
newest arrival takes it; a surface whose link merely dropped (`Hello.Back`)
takes it only if it is going spare; `MethodTake` moves it in one round trip; a
detach hands it to the newest surface still attached. `Submit`, `FollowUp`,
`SubmitImage` and `SubmitFiles` from a non-driver are refused with one sentence
that names the machine holding it and the key that takes it back. Every surface
learns who drives from a `driver` frame the engine sent, worded per recipient.
Protocol version 4.

**Why not lock-and-refuse.** A person who walked to another machine and opened
the conversation there is not an intruder. Refusing them would make the second
machine useless at exactly the moment they are standing in front of it, and the
only way out would be a gesture on a window in another building.

**Why not close the other window.** A forgotten window still showing the work is
a feature: it is the desk display, the phone on the side, the second monitor.
Closing it would throw away the one thing a second surface is for.

**Why the newest arrival rather than the first.** Because attaching is the
gesture a person makes with their hands, and the machine they are sitting at is
the machine they just typed a command on. Any other rule needs a second gesture
to say "no, really, me".

**Why `Hello.Back` is not optional.** A redial is an attach the person did not
make. Without it, a lid closed in a café reconnecting half an hour later would
pull the keyboard off the desk they are now sitting at, silently, and the
surface that stole it would be one nobody is looking at.

**Why the wording is per-recipient.** "The driver is macbook" is two different
sentences depending on who hears it: to the window beside it on the same
machine the honest word is `another window`, which is what aforge already says
at home; to a surface on another machine it is the machine's name. Only the
engine knows both names. The surface still owns the words — the engine sends
facts (`Driver{Yours, Machine, Here}`), except for the refusal, which is a
refusal and therefore the engine's to word (Decision 6).

## Decision 12 — A turn is announced to the surfaces that did not start it

**Decision.** When a turn opens, the engine sends every OTHER attached surface a
`turn` frame naming the stream and carrying the sentence that opened it.
`Welcome.Live` does the same job at attach time and carries no sentence.

**Why it was needed at all.** Events have fanned out to every attached surface
since version 2, and it made no difference: a surface only DRAWS a stream it
knows about — the one its own `Submit` named — so a turn started by another
window went past a watching one in silence. Two surfaces on one conversation
was true on the wire and false on the screen, and it stayed that way until
somebody sat in front of both at once.

**Why the engine sends it rather than the client inferring it.** The engine is
the only thing that knows who called `Submit`. A client that inferred "a turn I
did not start" from an event on an unfamiliar stream would race its own
submit's result and draw its own turn twice.

**Why the sentence rides the frame.** A reply with no question above it is a
screen that has lost the thread, and that message was typed on another machine.
A turn already running when a surface ATTACHES needs none of it — that message
is in the journal the surface reads on its way in — which is exactly why
`Welcome.Live` carries no sentence and this frame does.

## Decision 13 — Version 4: intent goes up, facts come down

**Decision.** The engine STATES its fact set — the model, the session's name,
what has been spent, what the conversation weighs, and the reasoning rung held
for every model anybody has dialled (`session.Facts`) — in the welcome, and
again on a `facts` frame whenever one of them moves: a turn ending, a name
settling, a compaction landing, somebody turning the model or the rung. The
surface keeps a replica (`internal/remote/replica.go`) and every read a frame or
a keystroke makes is a read of that. What still travels UP is intent — a
message, an answer, a key — because intent is the one thing the far end cannot
know on its own.

**Why not leave them as questions.** Because a question asked while drawing is a
question asked thirty times a second, and over an ssh pipe that is the repaint
rate of the terminal set to the round-trip time of the link. The owner met this
as "even hover seems to slow everything down": the status row asked the agent
for the reasoning rung while it was painting, and a pointer below the
conversation rebuilds the chrome per cell, so a two-hundred-cell sweep queued
seven seconds of keystrokes behind two hundred round trips. PERF.md's connection
laws pin the result at zero.

**Why the whole set and never a delta.** A push naming only what changed would be
smaller and would be wrong the first time one went missing: a surface that lost a
frame would carry a stale field for ever with nothing able to tell it so. The set
is five short fields and a small map — less than one line of a reply — so every
push is complete and the newest one is always the truth. A revision number minted
under the session's own lock is what makes "newest" well-defined when two facts
move in the same instant.

**Why a frame of its own rather than an event on a stream.** A fact moves between
turns as well as during one, and a stream only exists while a turn is running.
The `facts` frame belongs to the CONNECTION: no id, no seq, no stream. A build
that does not know the kind ignores it, which is what the client's reader already
does with every kind it has no case for.

**Why the surface may write to its own replica.** `SetModel` and `SetReasoningFor`
move it before the call goes out, because the person pressed a key and is looking
at the row it changed. That is optimism and not a second authority: the engine
announces the change to every surface on the conversation with a higher revision,
which lands over the top of the assumption. The assumption's whole life is one
round trip.

**And the same bargain on the way up.** A message typed over `--host` is drawn
the instant enter is pressed, in the place it will keep, marked one reading step
quieter until the engine has taken it (`internal/tui3/echo.go`). If the engine
refuses it — a turn already running, another window driving — the line comes
back off the page and the engine's own sentence is put where it was, because a
message the model never received must not sit in the only record of the
conversation looking as though it did.

---

## What runs where

```
        the machine you are sitting at            the machine that owns the work
        ─────────────────────────────             ──────────────────────────────
        internal/tui3   the screen                 internal/session   the agent
        internal/remote (Client, Agent)            internal/remote    (server)
        the draft, ↑-history, model list           the workspace, the key, the gate,
                                                   the journal, standing items,
        ── any io.ReadWriteCloser ─────────────►   the harness registry
           ssh pipe · unix socket · tunnel
```

## Map of the code

| Path | What it is |
| --- | --- |
| `internal/remote/wire.go` | the protocol: frames, methods, payloads, the version |
| `internal/remote/driver.go` | the room's one keyboard: who drives, who is told, and the refusal |
| `internal/remote/client.go` | the surface half — `Client`, and the `Agent` that satisfies `tui3.Agent` |
| `internal/remote/server.go` | the engine half — serves one conversation and the doors that replace it |
| `internal/remote/image.go` | the payload that cannot be handed on: a picture, remade on arrival |
| `internal/remote/replica.go` | the surface's copy of what the engine says about itself, kept fresh by push |
| `internal/remote/loopback.go` | the transport's test double: a real client, a real server, an in-memory pipe |
| `cmd/aforge/chatv3_host.go` | the `--host` door: parse the target, start ssh, hand the connection to the surface |
| `cmd/aforge/engine.go` | the far half ssh starts — machinery, not a command |

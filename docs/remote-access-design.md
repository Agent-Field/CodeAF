# Remote access — where v3 is, and where it should go

This document grooms the product design for reaching an aforge that lives on another
machine. It states where the `--host` lane is today (grounded in the code on
`chat-v3-task`), names the person this is actually for, and then walks the design space
from "what we have" to "no ssh at all", ranked by the experience a person gets and the
work each rung costs. It ends with a phased proposal.

The one-sentence thesis:

> **aforge on the remote machine should be a place you go back to, not a program you
> start.** Today `--host` gives us a faithful window onto a remote *launch*. The product
> wants a faithful window onto a remote *residence* — a session that is always there,
> reachable from wherever you happen to be sitting, with nothing to set up on the machine
> you are sitting at beyond proving you are you.

---

## 1. Where we are

`aforge chat --host devbox[:path]` works, and its bones are good:

- **The surface runs here, the engine runs there.** `cmd/aforge/chatv3_host.go` starts
  `ssh <dest> aforge engine` and speaks `internal/remote`'s protocol over the pipes.
  It is the person's own ssh — their config, aliases, agent, jump hosts. If `ssh devbox`
  works, `--host devbox` works. No daemon, no port, no key handling of ours.
- **The wire is an envelope, not a payload contract** (`internal/remote/wire.go`).
  Frames are JSON lines; payloads are `internal/session`'s own types, so a field added
  there travels without a wire change. Version mismatch is a refusal at the door.
- **Transport is already pluggable.** `remote.Dial` takes an `io.ReadWriteCloser` and
  `remote.Serve` takes a reader and a writer. Nothing in the protocol knows it is riding
  ssh. This is the load-bearing fact for everything below: every future transport —
  unix socket, relay tunnel, websocket — carries the *same frames*.
- **Pasted images already "just get there."** `SubmitImageArgs` carries bytes; the engine
  writes them into its own image store and journals a path that means something on the
  machine that owns the journal (`internal/remote/image.go`). This is the exact pattern
  to generalize for any local thing a person drops into the chat.
- **Truth about machines is taken seriously.** The YOLO badge names the engine's posture,
  standing items belong to the engine's disk, `/export` says `· on this machine`, paths
  are not drawn as links they cannot be. This honesty discipline is a real asset — every
  design below must keep it.
- **Reconnect is "run the same command."** The far machine is the only writer of the
  session file; on every road out it interrupts the in-flight turn (keeping the partial
  reply) and flushes the journal. Nothing that reached the file is lost.

And two structural limits, which are the whole reason this document exists:

1. **The engine dies with the pipe.** `aforge engine` reads frames on stdin; when ssh
   ends — closed lid, dropped wifi, killed terminal — the engine exits and the turn in
   flight is interrupted. The manual says it plainly: "Closing the terminal ends it;
   nothing keeps running on the far machine afterwards." For a person who runs aforge
   *constantly* on that machine, this is the wrong lifetime: the conversation's home is
   the far machine, but its heartbeat is a laptop's wifi.
2. **ssh is the only door.** Perfect when it is there; a wall when it is not — a machine
   behind NAT with no inbound port, a phone, a borrowed laptop, a browser. "Set up
   ssh to your home server" is a real onboarding cliff for exactly the person who most
   wants a persistent aforge.

There is also a known capability gap-list over `--host` (browser sign-ins, harness
building, adaptive runs, the task rail, home) — each one currently refused honestly
because its answer-channel would land in an empty room. Worth noticing: **most of these
are consequences of limit 1, not of remoteness.** A persistent engine that can hold a
question until *somebody* attaches dissolves several of them.

*(Vocabulary note: `internal/resident` — v1's "employee that keeps working while the
terminal is closed" — is a different product in the same binary. The persistent engine
proposed below shares its lifetime idea, not its design or its code. This document
deliberately does not call the new thing "the resident.")*

---

## 2. Who this is for — the groomed use case

**The persona:** one person, one always-on machine that owns the work (home server,
office workstation, cloud dev box), several places they sit (laptop, desktop,
occasionally a phone or a borrowed browser). The workspace, the files, the API key, the
standing orders — all live on the big machine. Local files on the access device are
explicitly *not* critical; the only local things that matter are **what they type, what
they paste, and what they are shown.**

Their day, as the product should serve it:

- Morning, desk: open a terminal, be *in* yesterday's conversation in one command —
  including anything a standing order did overnight.
- Lunch, café laptop: same conversation, mid-turn if a turn is running. Closing the lid
  mid-answer costs nothing; the turn finishes on the big machine.
- Evening, phone or a friend's browser: glance at what the long-running task did, answer
  a consent card it has been holding.
- Paste anything, anywhere: a screenshot, a log file, a CSV. It lands in the remote
  workspace as if it had always been there.

Requirements, ranked (each subsumes the ones above it):

| # | Requirement | Today |
| --- | --- | --- |
| R1 | Reattach to the same session, losing nothing that was journaled | ✅ works |
| R2 | A turn survives disconnection; the engine's lifetime is the machine's, not the pipe's | ❌ |
| R3 | Disconnection is *invisible*: the surface redials and resumes by itself (mosh-feel) | ❌ |
| R4 | Reachable without inbound network access to the machine (NAT, firewalls) | ❌ (ssh only) |
| R5 | Reachable from a device with no ssh setup — pairing instead of key management | ❌ |
| R6 | Reachable from a device with no aforge installed (browser) | ❌ |
| R7 | Anything pasted or attached in chat lands on the engine machine | ✅ images; ❌ other files |
| R8 | More than one surface attached at once (desk + phone), coherently | ❌ |

---

## 3. The design space, rung by rung

Each rung: the DX as a person would feel it, what it involves, and what it costs.
Every rung keeps the same wire protocol; only the *carrier* and the *lifetime* change.

### Rung 0 — today: `--host` over ssh, engine per connection

Kept as the floor and as the fallback forever: zero infrastructure, zero trust in any
third party, and the debugging story ("it is ssh") is unbeatable. Everything below is
additive.

### Rung 1 — the persistent engine: sessions that outlive the pipe

**DX:** identical command, different lifetime. `aforge chat --host devbox` attaches; the
turn keeps running when the connection drops; reattaching from anywhere shows the events
you missed and the live tail. Ask for a long refactor from the café, close the laptop,
open the desk machine, watch it still going.

**Design:**

- A per-machine **session host** process on the engine machine, listening on a unix
  socket under `AFORGE_HOME` (no TCP, no new network surface). It holds one engine per
  open session, exactly today's engine, just not married to a pipe.
- `aforge engine` (the thing ssh execs) becomes a thin **attach**: dial the socket, ask
  the host for the workspace's session, splice frames. If no host is running, spawn one
  (flock-guarded, the same discipline the standing tick lock already uses) — so there is
  still *nothing to set up*; the first attach is the daemon's birth. An idle host with no
  sessions and no standing work can simply exit.
- **Wire delta, deliberately tiny:** events need a per-stream **sequence number**, and
  `Hello` grows a resume cursor ("I have seen stream S through event N"). Reattach
  replays the journal for history (the transcript door already exists) and the event
  buffer for the live turn. The envelope law holds; this is protocol v2.
- **Detach semantics need one explicit choice:** today closing the surface *interrupts*
  the turn — that is the right meaning of ctrl+c, and the wrong meaning of a dropped
  pipe. The two must become distinguishable: an intentional close sends a frame; a torn
  pipe leaves the turn running. Consent cards and standing questions raised while nobody
  is attached are *held*, not expired — the empty-room problem becomes a
  waiting-room problem, which is exactly what dissolves several `--host` gaps.

**Cost:** medium. No infra, no accounts, no crypto beyond what ssh already gives.
Mostly lifetime/supervision engineering plus the seq/ack wire change. **This is the
highest leverage rung** — it converts "remote launch" into "residence" and R2 falls,
with R8 within reach (the host can fan events out to N attached surfaces; calls are
already serialized at the engine).

### Rung 2 — roaming: the surface redials by itself

**DX:** mosh-feel. Wifi blips, VPN flaps, laptop sleeps — the surface shows a quiet
`reconnecting…` in the status line and then doesn't need to say anything else. The
"connection is gone — run the same command" sentence becomes something the person reads
only when redialing has genuinely failed for a while.

**Design:** a redial loop with backoff around the ssh spawn; on success, the same
resume-cursor handshake as rung 1. The draft, the scrollback, the picker state are
already local, so nothing on screen is lost. The 10-second call deadline stays — calls
in flight during the gap fail into a retry queue for idempotent reads and an honest
error for writes.

**Cost:** small, *given rung 1*. Without rung 1 it is barely worth it (redialing
today's engine still means an interrupted turn and a cold session).

### Rung 3 — the relay: no ssh, a pairing code instead

This is the "hosted proxy on the aforge website" idea, done with the relay **blind**.

**DX:**

```
big-machine$ aforge serve
  this machine is reachable as  otter-lamp-42
  pair a new device with code   715 302   (valid 10 minutes)

laptop$ aforge chat --at otter-lamp-42
  pairing with otter-lamp-42 — enter the code shown there: ______
  paired. this laptop is now a key to otter-lamp-42.   [chat opens]
```

Thereafter `aforge chat --at otter-lamp-42` from that laptop just opens — over the
internet, through NAT, no ssh, no port, no account. Un-pair from the engine side with
`aforge devices` (list, revoke).

**Design:**

- **The engine machine only ever dials out.** `aforge serve` (or the rung-1 session
  host, given the flag) keeps an outbound websocket to `relay.aforge.dev`, registered
  under a name derived from its keypair. NAT and firewalls become irrelevant; there is
  nothing to open.
- **The relay is a dumb, blind pipe.** It matches a surface's dial to an engine's
  registration by name and forwards ciphertext. It can not read frames, because:
- **End-to-end encryption with pairing-code PAKE.** First contact runs a PAKE (e.g.
  SPAKE2) over the short code, so the relay cannot man-in-the-middle even though it
  brokered the introduction; the exchange pins long-term device keys on both ends
  (stored in the OS keychain where there is one — this is where "Mac fingerprint"
  belongs: the local keychain can demand Touch ID to release the device key, giving
  biometric unlock *without aforge ever seeing a biometric*). Every later connection is
  a Noise handshake between pinned keys. The relay's total knowledge: name, timing,
  bytes.
- **Same frames inside.** The tunnel presents as an `io.ReadWriteCloser`; `remote.Dial`
  neither knows nor cares. The whole gap-list honesty machinery carries over verbatim.
- **Security posture, stated plainly:** a paired device is a hand on that machine's
  tools — the same weight as an ssh key, and the product must say so at pairing time.
  Short-lived codes, engine-side revocation, and key-pinned transport make this
  defensible; "relax all constraints" is a fine brief for *friction*, not for this.

**Cost:** large-ish, honestly priced: a hosted service (dumb enough to be cheap — chat
is KB/s and the relay holds no plaintext and no state beyond registrations), the crypto
lane, pairing UX, abuse controls (rate limits, name squatting), and an operational
commitment. This is the rung where aforge grows a service dependency, so it must remain
*a* door, never *the* door — rungs 0–2 keep working with the relay down or shunned.

**Interim honesty:** until this rung exists, the README answer for "no inbound ssh" is
Tailscale/WireGuard — `--host` over a tailnet already gives R4 today for people willing
to install one, and rung 1 makes that genuinely good. The relay is for making it
*aforge's own* two-command story.

### Rung 4 — the web surface, and (maybe) accounts

**DX:** on any browser, `app.aforge.dev`, enter machine name + pair (or already-paired
browser keys in local storage): a read-mostly surface — transcript live-tail, consent
cards, small replies. Optionally, accounts so that paired machines are discoverable
("your machines") instead of remembered by name — accounts as a *directory*
convenience on top of the same device keys, never as the trust root.

**Cost:** a second surface is a serious product investment (tui3 is terminal-native);
scope it deliberately small — *glance and answer*, not a full second client. Ranked
last on purpose: everything before it multiplies the value of building it.

---

## 4. "Anything I add in chat should be there" — the attachment contract

Generalize the image precedent into a stated law:

> **Anything a person puts into the chat is the surface's to read and the engine's to
> keep.** Bytes travel with the message; the engine writes them under the session's own
> store and journals a path that is true on the machine that owns the journal.

Concretely:

- **Images:** done today. Keep.
- **Arbitrary files** (`/attach`, or `@` on a local path, or a drag onto the terminal):
  a `SubmitFileArgs` sibling — bytes + name + declared type, landed in a per-session
  `attachments/` dir on the engine, journaled by path, mentioned to the model as a path
  it can open with its ordinary tools. Same ceilings-with-honest-sentences discipline as
  images. This is small work on the existing pattern and closes R7.
- **Pasted text:** already just travels; a large paste is a message, not a file — keep.
- **The reverse direction** (engine → surface, "give me that file here") is the same
  frame walked backwards, and is what would make `/export` and "download this artifact"
  work onto the machine the person is sitting at. Worth doing at rung 1; the honesty
  suffix `· on this machine` then becomes a choice instead of a limitation.

---

## 5. The proposal

Phased so every phase ships a complete experience and no phase bets on infrastructure
before the product has earned it:

1. **Phase A — the persistent engine (rung 1).** Session host on a unix socket, attach
   via the same ssh command, seq/ack resume in protocol v2, held-not-expired cards,
   intentional-close vs torn-pipe. Ship with generic file attachments (§4) and the
   small `Hello` cleanups already stubbed in the code (`--model`/`--reasoning` belong in
   the hello). *This alone delivers the "running actual aforge constantly" story for
   everyone who has ssh.*
2. **Phase B — roaming reconnect (rung 2).** Redial loop + quiet status. Cheap after A;
   the moment the product starts feeling like a place.
3. **Phase C — the relay (rung 3).** `aforge serve`, blind relay, PAKE pairing codes,
   keychain-held device keys, `aforge devices`. The two-command, no-ssh story — and the
   first phase requiring hosted infrastructure, entered with rungs 0–2 as permanent
   fallbacks.
4. **Phase D — glance surface on the web (rung 4),** scoped to live-tail + cards +
   replies; accounts only if and when machine discovery proves to be the actual
   friction.

Decided-by-this-document defaults (revisit deliberately, not accidentally):

- The wire envelope stays; transports are carriers, not protocols.
- The relay never sees plaintext; pairing is PAKE over a short code; device keys pin.
- Trust roots on the engine machine, always: pairing approval, revocation, tool gate,
  spend rail. No surface, local or remote, ever answers for the machine that runs the
  tools.
- Rung 0 (plain ssh, per-connection engine) remains supported forever as the
  zero-dependency floor.

Open questions, for grooming rather than blocking:

1. Multi-attach (R8): mirror events to all surfaces (easy) — but who holds the draft,
   and does a consent card answered on the phone vanish from the desk mid-keystroke?
   Needs a small interaction design pass before rung 1 enables more than one attach.
2. Naming: `--host` (ssh) vs `--at` (relay) vs one flag that resolves either — one
   vocabulary for "where the work is" would be better if it can stay honest about
   transport.
3. Idle policy for the session host: exit-when-empty is clean, but standing orders
   already keep a machine warm — the host and the standing tick likely want to be the
   same lifetime eventually.
4. Browser sign-ins over a connection (the `/connect` gap): a persistent engine plus a
   relay could carry the OAuth loopback dance end-to-end; park until C.

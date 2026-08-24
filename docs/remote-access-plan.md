# Remote access — the build plan: one wave, parallel lanes

Companion to `docs/remote-access-design.md`. That document walked the design space as
rungs and proposed phases; this one replaces the phasing with what we actually intend to
do: **build everything in parallel lanes and ship it as one wave.** It also answers a
question the design doc did not: what `Agent-Field/furrow` gives us, and where it does
and does not belong in this picture.

---

## 1. furrow, assessed

`furrow` (Agent-Field, open, Apache-2.0, Rust) copy-on-write forks a whole workspace —
files, deps, `.env`, dev database, Git's own mutable state — into byte-exact "universes",
and continuously seals the workspace into an immutable content-addressed timeline. It
syncs that state between machines, encrypted below the transport (XChaCha20-Poly1305,
BLAKE3-verified chunks, opaque remote names), over SSH or any S3-compatible bucket, with
deduplicated deltas. `furrow sync --follow` keeps a warm two-way session;
`FURROW_RECOVERY_KEY=<key> furrow clone <url>` materializes the *current* working state —
not the last commit — on another machine. The CLI is agent-first: `--json` everywhere,
NDJSON event stream, an MCP server, destructive ops gated on IDs plus `--yes`.

### What it is for us, and what it is not

**furrow is a state-sync and isolation primitive, not a transport.** The remote-access
problem is an *interactive frame stream* — keystrokes out, events back, sub-second — and
furrow moves *sealed workspace snapshots*. Its S3 mailbox is a genuinely clever
no-hosted-service rendezvous, but for state, not for a conversation; polling a bucket is
not a chat wire. So: **the session host, the roaming reconnect, and the relay all stand
exactly as designed.** Furrow replaces none of them.

It also cannot be linked: furrow is Rust, aforge is Go. Any integration is the CLI —
which is fine, because the CLI *is* furrow's declared API — and therefore optional and
feature-detected. The codebase's own law applies: a capability that cannot work is
absent, not broken. No furrow on the machine → the seams below simply do not exist, and
nothing else in this plan notices.

### Where furrow genuinely helps — three seams, all off the critical path

1. **Folder sync as an opt-in lane, not the attachment contract.** The groomed persona
   says local files are *not* critical — work lives on the engine machine — so the
   in-chat attachment contract (bytes travel with the message) stays the default and is
   still built. But for the person who *does* want a local folder to exist over there —
   "I have this directory on my laptop, work on it from the big machine" — furrow is the
   right answer and we should not build a worse one: `aforge` detects furrow on both
   ends and offers the pairing (`furrow remote add` + `sync --follow`) instead of
   growing its own file-sync protocol. Divergence is furrow's honest edge (preserved and
   reported, not auto-merged; smoothest with one writer at a time) — which matches our
   trust-roots law anyway: the engine machine is the writer.
2. **Universes for multi-attach and for parallel sessions on the engine machine.** The
   session host can offer `furrow exec`-style isolation: each session (or each risky
   run) gets a warm CoW fork of the workspace, merged back with
   `furrow merge --check`. This is the same discipline this repo already uses for its
   own waves, done with state instead of git. Optional seam on the host, nothing
   depends on it.
3. **Workspace rewind aligned with conversation rewind.** v3 already has rewind points
   for the *conversation*; `furrow hook install` turns agent turns into attributed
   *workspace* restore points. Wiring the two — rewind the chat and offer the matching
   workspace state — is a killer capability and works locally as well as remotely.
   Again: optional, feature-detected, off the critical path.

**Decision:** furrow is a complement we integrate through three optional seams (lane F
below). It does not change the transport design, the relay, or the pairing model — though
its *recovery-key-entered-once-per-machine* UX is prior art in our own org for exactly
the pairing feel we want, and lane E should keep the two vocabularies compatible rather
than inventing a rival one.

---

## 2. Why one wave instead of phases

The design doc's phases A→D were sequenced by *value*, not by *dependency*. Looking at
actual dependencies, almost nothing serializes — and one thing actively argues for
shipping together:

**The protocol version is a door that slams.** The wire refuses a version mismatch
outright, so every ship that touches the envelope forces every user to update *both*
machines. Phased shipping means a v2 bump (persistent engine), then a v3 (attachments),
then a v4 (relay hello) — three forced double-updates. One wave means one bump:
**v1 → v2, once.** This is the strongest single argument for the user-visible release
being one event.

What still genuinely serializes:

- **The wire v2 contract itself** (a few days of design, not a phase): every lane codes
  against it, so it freezes first — lane 0.
- **The relay as an *operated service*** ships on a deploy cadence, not a binary
  release. The *code* rides the wave; the service goes live when ops is ready, and until
  then `--at` fails with one honest sentence. No other lane waits on it.
- **The web glance surface** is the one candidate to cut from the wave: nothing depends
  on it, it is the largest unknown, and it multiplies in value *after* the relay is
  live. It is in the plan as lane G, explicitly severable.

Everything else is parallel.

---

## 3. The lanes

Built the way this repo builds waves: one worktree per lane off `chat-v3-task`
(`git worktree add ~/af-<name> -b <branch>`), merged back, worktrees removed. Every lane
carries its own manual pages in the same change (THE MANUAL LAW); the three gates fail
the build otherwise.

### Lane 0 — the wire v2 contract *(freeze first; days; one author)*

The one true serialization point. Freezes, in `internal/remote/wire.go` terms:

- per-stream **sequence numbers** on event frames; a **resume cursor** in `Hello`
  ("stream S seen through N");
- an **intentional-close frame**, so a torn pipe (leave the turn running) and a chosen
  close (interrupt, flush) stop being the same event;
- `Model`/`Level` in `Hello` (retiring the `applyHostChoices` stub);
- **generic file payloads** — `SubmitFileArgs` beside `SubmitImageArgs`, and the reverse
  door (surface asks for bytes by engine path) for `/export`-to-here and downloads;
- **held-question semantics**: a consent/standing/design card raised with no surface
  attached is *held*, replayed on attach, with its held-since time — the frame shape for
  that;
- the relay's **rendezvous framing** (registration, dial-by-name, pairing envelope) —
  opaque to the relay, defined here so lanes D and E code against the same bytes.

Deliverable is the file plus a protocol fixture — a canned frame conversation both a
fake server and a fake client can replay — which is what lets lanes A and B proceed
without each other.

### Lane A — the session host *(the meat; largest lane)*

Per-machine host on a unix socket under `AFORGE_HOME`; engines outlive pipes;
`aforge engine` becomes attach-or-spawn (flock-guarded, the standing tick's discipline);
held cards; fan-out to N attached surfaces; idle policy. Includes the first honest pass
on **re-opening the gap list**: every `--host` refusal justified by "the card would land
in an empty room" (harness building, adaptive runs, consent "always") either lights up
under held-card semantics or gets a new true sentence — each one is a small scoped item
inside this lane, and the manual's `running-on-another-machine.md` is rewritten
accordingly.

### Lane B — surface resume + roaming *(medium; parallel with A via the fixture)*

Redial loop with backoff around the transport spawn; resume-cursor replay; quiet
`reconnecting…` status; idempotent-read retry queue and honest write errors; the
"connection is gone" sentence demoted to genuinely-failed-for-a-while. Codes against
lane 0's fixture; first integration with a real host is the merge wave.

### Lane C — attachments, both directions *(small; fully parallel)*

`/attach` + `@`-path upload landing in the session's `attachments/` dir (the
`image.go` pattern generalized), ceilings with honest sentences; engine→surface fetch
wired into `/export` and artifact download, retiring the ` · on this machine` suffix
where it stops being true.

### Lane D — the relay service *(parallel from day one; separate deployable)*

The dumb blind pipe: registration by key-derived name, dial matching, ciphertext
forwarding, rate limits, name-squatting policy. Speaks only lane 0's rendezvous
framing; never sees a session frame in plaintext. Lives as its own small deployable
(own repo or `cmd/relay` — decide at kickoff); runnable locally as a test binary, which
is what lane E develops against. Deployment to `relay.aforge.dev` is an ops task
decoupled from the code merge.

### Lane E — pairing + client crypto *(medium; parallel; depends only on lane 0's rendezvous framing)*

`aforge serve` (the host, given a flag, dialing out and holding the registration);
PAKE over the short pairing code; Noise between pinned device keys; keys in the OS
keychain (Touch ID gating where the platform offers it); `aforge devices`
list/revoke; `--at <name>` on the surface. Tunnel presents `io.ReadWriteCloser` —
`remote.Dial` never knows. Tested end-to-end against lane D's local binary. Keeps its
vocabulary compatible with furrow's recovery-key UX rather than rivaling it.

### Lane F — furrow seams *(small–medium; fully parallel; optional by construction)*

The three seams from §1, all feature-detected via the furrow CLI (`--json`):
folder-sync offer, universes on the host, rewind alignment. Works over today's
`--host` too — this lane does not even need lane 0. Its manual pages state plainly what
happens when furrow is absent (nothing, by design).

### Lane G — web glance surface *(severable; start last or not at all this wave)*

Browser client scoped to live-tail + cards + short replies, doing lane E's crypto in
WebCrypto against lane D's websocket door. Explicitly the first thing cut if the wave
needs slimming; nothing else references it.

### Lane H — the closing sweep *(short; serial at the end; one author)*

The integration wave: merge order, the one protocol-version bump, a hunt through the
manual corpus for every sentence the wave made false (the design doc's own
`generate_image` lesson), `system.md` re-checked against the actual belt over a
connection, the full `tui3` suite (~150s, with the two known flakes re-run in
isolation), and a live two-machine + relay smoke.

---

## 4. Dependency graph and staffing shape

```
lane 0 (wire v2 + fixture)  ──┬──► lane A (session host) ──────┐
                              ├──► lane B (resume/roaming) ────┤
                              ├──► lane C (attachments) ───────┼──► lane H (sweep, one ship)
                              └──► lane E (pairing/crypto) ◄───┤
lane D (relay, opaque bytes) ─────────────────────────────────┘
lane F (furrow seams) ────────────────────────────────────────┘
lane G (web glance) — severable, hangs off D+E when they exist
```

- **Critical path:** lane 0 → lane A → lane H. Everything else finishes inside lane A's
  shadow or waits harmlessly at the merge.
- **Maximum useful parallelism: six lanes** (A, B, C, D+E can pair, F) once lane 0
  freezes — which is also roughly the number of concurrent sessions this repo already
  runs comfortably in worktrees.
- **Merge order for lane H:** 0 is already in; then A, then B (first real host×surface
  integration), then C, then E with D's local binary in CI, then F; G whenever it
  exists. One version bump, one rebuild, one release.

## 5. What "ship everything" means, precisely

One binary release in which: `--host` attaches to a persistent engine and survives every
disconnect; the surface redials by itself; anything pasted or attached lands over there
and anything made over there can be fetched here; `aforge serve` + a pairing code +
`--at <name>` work with no ssh the moment the relay service is switched on (and fail in
one honest sentence until it is); furrow, where installed, offers folder sync, forked
universes, and aligned rewind; and the manual tells the truth about all of it. The relay
*service* going live and the web glance surface are the two consciously decoupled tails
— code in the wave, launch on their own clocks.

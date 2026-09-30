# Hosted relay

The relay of `internal/relayserve` (contract sections 3, 4.4, 6 and 18) as a Cloudflare Worker.
A client cannot tell the two apart: `internal/relayconf` is the judge, and this Worker passes it.
The relay sees only ciphertext and keeps no account table: an identity is the hash of its key.

## How it is built

```
request --> Worker (stateless) --+--> /v1/pair*  --> PairGate (one object) --> Mailbox (one per nameplate)
                                 |
                                 +--> /v1/store/*, /v1/dir/* --> IdentityDO (one per identity) --> R2
```

The Worker (`src/worker.js`) decides where a request goes and does nothing else. It reads the
identity from the unsigned `Codeaf-Identity` header, refuses an oversized `Content-Length` with 413
before waking anything, and forwards the request, body unread, to that identity's Durable Object.
Routing before reading is what keeps one identity's slow put from ever delaying another identity's
`Has`, and it lets the identity's object count its own puts.

`IdentityDO` (`src/identity.js`) proves the caller holds a cert of the identity (which needs no
body), counts a put as in flight, reads the body under its cap, verifies the request signature over
it, checks the device is not revoked, spends the caller's rate budget, and serves. Then:

- `Has` never answers no while a put for the same identity is still being received. It waits for the
  puts in flight (`src/flight.js`, a port of `internal/blobstore/flight.go`) and fails with 503 after
  30 s rather than say no. A caller with no cert of the identity cannot hold a put open.
- The directory lives in the object's SQLite (`src/directory.js`). The object is single-threaded and
  every operation is one synchronous turn, which is the compare-and-swap the lease rules need. The
  rules (`src/rules.js`) are a JS port of `internal/directory/rules.go`.
- Frames live in R2 at `<identity>/f/<FrameID>` (`src/store.js`), each validated (magics, header,
  `cell_key_id`) before it is stored. The index that finds an object inside its frame is rows in the
  object's SQLite (`objs`: rid -> frame, offset, length, primary key on rid), so it takes no memory
  and how many objects an identity holds is bounded by its byte quota. A frame is durable in R2
  before the index learns of it; a crash between the two leaves an unindexed frame, which the next
  put of that same frame indexes.
- A batch get, `POST /v1/store/objects` with `{"rids":[...]}` (1 to 256, none twice), answers a frame
  holding the objects of the longest prefix of the ids that fits in about 1 MiB, in request order,
  and stops at the first id the index does not hold (`404 not_found` only when the first is absent).
  It locates the ids in the object's SQLite and reads each frame it touches once, as one ranged R2
  read, so a takeover pays one Worker request and one R2 read per frame, not per object.
- A rid held with other bytes is `409 conflict`, and the frame stores none of its objects. Puts run
  one at a time inside the object, so two racing puts of one rid cannot both win.
- `/v1/store/stats` is kept across evictions and restarts: counts are batched in memory and written
  by an alarm a few seconds after the first change (`src/stats.js`). A platform crash can lose at
  most those few seconds.
- An identity is admitted on its first authenticated request, and that first sight is counted
  against the caller's IP in one `NewcomerGate` object (`src/newcomers.js`). Existing identities are
  never counted again, from any address, so the cap cannot affect the current client. Admission
  happens before the identity's tables exist, so a refused identity stores nothing.

Directory revocation is `POST /v1/dir/devices/{id}/revoke` (204; unknown id 404 `not_found`; the
caller itself 400 `self_revoke`; twice is fine). A revoked device is `401 revoked` on both wires, and
a device record cannot set or clear its own flag.

Directory watch (contract 21) is `GET /v1/dir/watch`, a signed WebSocket upgrade on the same object
(`src/watch.js`). It replaces the home screen's polling: the object keeps a durable version of the
directory (`dirver` in its SQLite), bumps it on every change a person could see (a heartbeat that only
moves a lease's expiry is not one), and sends `{"v":N}` to every socket. It accepts with the hibernation
API and answers the client's `ping` with the platform's auto-response, so an open, quiet socket wakes
nothing and bills nothing. `test/request-scoped.test.js` keeps the object free of timers (but `Has`'s
bounded wait) and of outbound connections. `maxWatchers` (default 1000) caps sockets per identity.

Rotation (contract 20) is `POST|GET /v1/identity/rotation` on the same object (`src/rotation.js`, a
port of `internal/directory/rotation.go`). The state is the `rotation` field of the identity record in
the object's SQLite. The first device to freeze owns the rotation, any device may thaw a frozen
identity, nothing thaws a retired one. Routes marked `write` in `src/routes.js` (every directory verb
that changes a record, and a frame put) are `410 rotated` once the identity is frozen or retired;
reads stay open. A retire arms the object's alarm at its deadline. The alarm (`src/erasure.js`) writes
the tombstone first (`gone` in the object's storage), deletes the identity's R2 prefix a page per turn,
then drops the SQLite tables, so a crash half way is finished by the next alarm; afterwards every
request is `410 gone` and makes no table and no R2 object. The one alarm slot is shared with the stats
flush, so `#arm` only ever moves it earlier and the alarm handler re-arms the deadline.

The pairing mailbox (`src/pair/`) is unauthenticated because the joining device has no identity yet,
so it cannot become free storage: every limit is by IP (`CF-Connecting-IP`), boxes live one TTL and
are deleted by an alarm, and `PairGate` caps the live boxes. Its counters are stored, so waiting for
the object to be evicted does not reset them.

The frozen Go code is not compiled into the Worker. The three pure parts (frame decode, request
verify, lease rules) are ported to JavaScript and pinned to Go by vectors that Go generates.

## Run it locally

```sh
cd relay/hosted
npm install
npm run dev                       # http://127.0.0.1:8787, R2 and Durable Objects simulated in workerd
```

`npm run dev` lifts the new-identity cap (the conformance suite makes one identity per case, all from
127.0.0.1); a bare `wrangler dev` keeps the default of 20 a day per address. Conformance, against
the running relay, from the repository root:

```sh
go test -tags relayurl ./internal/relayconf/ -relay-url=http://127.0.0.1:8787
```

`npm run dev` also trusts `X-Forwarded-For` and keeps pairing mailboxes 4 s, which the pairing suite
needs (each case is a network of its own; `ExpiryDeletes` skips above a 5 s TTL). `RelayFull` is
skipped unless a second relay capped at two mailboxes is given with `-relay-small-url`
(`npx wrangler dev --port 8788 --var TRUST_PROXY:1 --var 'CAF_LIMITS:{"pairMaxBoxes":2,"newIdentitiesPerIpPerDay":1000000}'`).

## Tests

```sh
npm test            # unit: the vectors, the rules, quotas, the store on a real R2 simulator, body caps
npm run e2e         # starts two local relays (default limits, short limits), runs every wire end to end
CONF=1 npm run e2e  # the same, plus the Go relayconf suite against the default relay
```

The Go vectors pin the JS ports. Any change to `internal/blobstore/frame.go`,
`internal/reqsign` or `internal/directory/rules.go` must add a vector in the same PR:

```sh
go run ./test/hostedvectors > /tmp/v.json && VECTORS=/tmp/v.json npm test
cp /tmp/v.json relay/hosted/vectors.json      # keys are fresh on every run; the file is a snapshot
```

One divergence is pinned on purpose: Go's `encoding/json` matches field names case-insensitively,
the port does not. Those frames carry `js_stricter` in the vectors; Go writers always emit the
canonical names, so the stricter side is the safe one.

## Bindings a deployment needs

| Binding | Kind | Purpose |
|---|---|---|
| `FRAMES` | R2 bucket | frames and the object index, under `<identity>/` |
| `IDENTITY` | Durable Object `IdentityDO` (SQLite) | directory, puts in flight, caps, one per identity |
| `NEWCOMERS` | Durable Object `NewcomerGate` (SQLite) | new identities per IP, one object |
| `PAIR_GATE` | Durable Object `PairGate` (SQLite) | nameplates, per-IP counters, one object |
| `MAILBOX` | Durable Object `Mailbox` | one pairing, one per nameplate |
| `LEASE_POLICY` | var | `amended` (default: 90 s, a publish renews; stage 1H decision 8.7) or `stage1` (the frozen 30 s law) |
| `CAF_LIMITS` | var, optional | JSON that overrides the numbers below |
| `TRUST_PROXY` | var, optional | `1` takes the caller's network from the first `X-Forwarded-For` address (the self-hosted relay's `--trust-proxy`); for test rigs only, never production, where `CF-Connecting-IP` is the one truth |

`wrangler.toml` declares all of them and the migration `v1` that creates the four classes as
SQLite-backed. The relay needs Workers Paid: the Free plan allows 10 ms of CPU a request and 100,000
requests a day.

## Deploy

Nothing here deploys by itself. To put it on an account (staging first):

```sh
cd relay/hosted
npx wrangler r2 bucket create <bucket>
# in a copy of wrangler.toml (or `-c staging.toml`): set `name`, and `bucket_name = "<bucket>"`
npx wrangler deploy
```

No route or domain is set: it answers on `<name>.<account>.workers.dev` until one is added. The
deployment runs the amended lease law (`LEASE_POLICY = "amended"`: 90 s, a publish renews), which is
what the client and the Go relay implement; `stage1` is the frozen 30 s law, kept as a named policy
only. Setting it back would make a takeover wait 30 s where the client expects 90 s of grace, so a
deployment never does. The conformance suite waits out the 90 s in real time (about 92 s in all,
its cases run in parallel); there is no short-TTL knob on either relay. The deployment keeps no
request log and enables no observability.

`staging.toml` already carries what the conformance suites need (a 4 s pairing TTL, `TRUST_PROXY`, a lifted
per-IP identity cap); production sets none of them. The pairing wire words are those of
`internal/pairbox` (`forbidden`, `gone`, `full`, `too big`, `slow down`).

## Limits (`src/limits.js`, one table)

| Limit | Default | Refusal |
|---|---|---|
| stored bytes per identity | 5 GiB | `507 full` |
| rotation grace: min / max / default (`minGraceMs`, `maxGraceMs`, `defaultGraceMs`; tests lower the min) | 1 h / 30 d / 7 d | `400 bad_grace` |
| stored objects per identity (the index is SQLite rows; the byte cap rules) | 5,000,000 | `507 full` |
| new identities per IP per day (first sight only; existing identities never counted) | 20 | `429 too_many_identities`, `Retry-After` |
| new frames per identity per day | 5,000 | `507 full` |
| requests per minute, per identity / per device | 1,200 / 600 | `429 rate_limited`, `Retry-After` |
| puts in flight per identity (a 16 MiB frame costs 2 to 3 times its size in memory) | 2 | `429 rate_limited` |
| frame body | 16 MiB (`MaxFrame`) | `413 bad_frame`, streamed or declared |
| any other body | 1 MiB | `413 too_large` (directory), `413 bad_frame` (store) |
| pairing: TTL, message, messages per side | 10 min, 4 KiB, 4 | `404`, `413`, `409 full` |
| pairing: creates per hour, writes per minute, open polls, per IP | 10, 30, 4 | `429 rate_limited`, `Retry-After` |
| pairing: live mailboxes in all (32 KiB each, so also 64 MiB) | 2,000 | `503 full` |

Stored quotas (bytes, objects, frames a day), the stats counts and the per-IP counters live in the identity object's SQLite and survive
eviction. A frame the store already holds costs no quota, so a client that resends is never refused
for it. The per-minute rates are kept in memory: an eviction can only forgive a burst.

## Cost note: Durable Object SQLite rows written

DO SQLite bills rows written at $1 per million after 50 million a month included (rows read are
25 billion included and not a concern here). Every table in the object is `WITHOUT ROWID` or keyed
by the rowid, so a row is one row written, not a row and an index entry (an assumption to confirm
against billed usage on a real account; the stage 1H staging read-back is the method).

| Operation | Rows written |
|---|---|
| put of a frame with n objects, new | n + 2 (the frame, each object, the usage row) |
| put of a frame already held | 0 |
| directory create, acquire, heartbeat, publish, release | 1 |
| stats | 1 per flush window (5 s) of store activity, not per request |
| `Has`, `Get`, list, cell read | 0 |
| an identity's first request | 1 (admitted) |

For the session shape measured in the stage 1H decision (232 one-object puts, 197 directory writes)
that is about 930 rows a session; 10,000 identities at 8 sessions a month is 74M rows, so about
$24 a month over the included 50M. With frames batched to about 12 puts of 20 objects a session it
is about 510 rows, 41M a month, inside the included amount. Object count no longer costs memory, so
the only price of a large repository is these rows: 170,000 objects is 170,000 rows once, about
$0.17.

## The other option

The R2-only directory (a Worker with no Durable Object) was built and measured in the stage 1H
spike and is kept in git history, not in this tree: `git log wip/layout-v0-hspike -- relay/hosted/src/r2dir.js`.
It costs 1.3 to 2.3 times as much and a directory write takes tens of times longer, and it cannot
keep the in-flight-put rule by identity.

# Hosted relay

The relay of `internal/relayserve` as a Cloudflare Worker.
The Go client holds both implementations to `internal/relayconf`; run the local matrix below
before claiming conformance for a build.
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
  read, so a takeover uses one Worker request and one R2 read per frame, not per object.
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

Directory watch is `GET /v1/dir/watch`, a signed WebSocket upgrade on the same object
(`src/watch.js`). It replaces the home screen's polling: the object keeps a durable version of the
directory (`dirver` in its SQLite), bumps it on every change a person could see (a heartbeat that only
moves a lease's expiry is not one), and sends `{"v":N}` to every socket. It accepts with the hibernation
API and answers the client's `ping` with the platform's auto-response, so an open, quiet socket wakes
nothing. `test/request-scoped.test.js` keeps the object free of timers (but `Has`'s
bounded wait) and of outbound connections. `maxWatchers` (default 1000) caps sockets per identity.

Rotation is `POST|GET /v1/identity/rotation` on the same object (`src/rotation.js`, a
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

Link pairing (`src/link/`, `docs/ux-pairing-contract.md`) is the sibling for a new device that asks a paired
device to approve it: `LinkGate` holds the per-IP decisions and allocates the 8-character code, one
`LinkRequest` object per code holds the record and moves it once (pending, then approved or denied), and the
signed approve and deny routes under `/v1/dir/requests/` are served by the identity's own object, which
writes the device in one synchronous turn. The relay stores the grant and the sealed name as opaque bytes.

The frozen Go code is not compiled into the Worker. The three pure parts (frame decode, request
verify, lease rules) are ported to JavaScript and pinned to Go by vectors that Go generates.

## Run it locally

```sh
cd relay/hosted
npm ci
npm run dev                       # http://127.0.0.1:8787, R2 and Durable Objects simulated in workerd
```

`npm run dev` lifts the new-identity cap (the conformance suite makes one identity per case, all from
127.0.0.1); a bare `wrangler dev` keeps the default of 200 a day per network. Conformance, against
the running relay, from the repository root:

```sh
go test -tags relayurl ./internal/relayconf/ -relay-url=http://127.0.0.1:8787
```

`npm run dev` also trusts `X-Forwarded-For` and keeps pairing mailboxes 4 s, which the pairing suite
needs (each case is a network of its own; `ExpiryDeletes` skips above a 5 s TTL). `RelayFull` is
skipped unless a second relay capped at two mailboxes is given with `-relay-small-url`
(`npx wrangler dev --port 8788 --var TRUST_PROXY:1 --var 'CAF_LIMITS:{"pairMaxBoxes":2,"newIdentitiesPerIpPerDay":1000000}'`).

## Tests

The repository's `scripts/relay-conformance.sh` runs its tracks sequentially on a shared
machine. `CONFORMANCE_PARALLEL=1` is an opt-in for isolated runners; concurrent package
builds and disk-heavy tests can otherwise consume a local Worker's request deadlines.

```sh
npm test            # hermetic unit: the vectors, the rules, quotas, the store on a real R2 simulator, body caps
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
| `LINK_GATE` | Durable Object `LinkGate` (SQLite) | link-request codes, per-IP counters, one object |
| `LINK_REQUEST` | Durable Object `LinkRequest` | one pending request, one per code |
| `LEASE_POLICY` | var | `amended` (default: 90 s, a publish renews) or `stage1` (30 s leases) |
| `CAF_LIMITS` | var, optional | JSON that overrides the numbers below |
| `BASE_PATH` | var, optional | a path prefix every route is served beneath (for example, `/fabric`); empty, the default, serves from the root. A request outside the prefix is `404 not_found` |
| `TRUST_PROXY` | var, optional | `1` takes the caller's network from the first `X-Forwarded-For` address (the self-hosted relay's `--trust-proxy`); for test rigs only, never production, where `CF-Connecting-IP` is the one truth |

`wrangler.toml` declares all of them and the migration `v1` that creates the four classes as
SQLite-backed; migration `v2` adds the link classes.

## Base path

A deployment can share a hostname by using a path prefix, such as
`https://relay.example.com/fabric`. `BASE_PATH = "/fabric"` makes the Worker serve every route
(store, directory, the `/v1/dir/watch` upgrade, pairing, link requests, rotation) beneath that prefix.
`src/base.js` holds the whole mechanism: the Worker strips the prefix once, before dispatch, and a
request outside it is the usual 404.

The request signature covers the path the client sent (`internal/reqsign` signs `RequestURI()`), so a
client signs `/fabric/v1/...` and the identity object checks the signature against the prefix put back
(`signedUri`), not against the stripped path. A client that signed the bare path is `401`. Every Go
client joins its routes to the relay address in one helper, `wireauth.Endpoint`, so an address with a
path (`https://host/fabric`, with or without the last slash) works for all of them.

The Go relay (`internal/relayserve`) stays at the root of its host: it has no base path. A self-hoster
who wants one puts their own proxy in front, and the proxy must pass the path on unchanged (the client
signed it), or serve the relay on a host of its own.

## Run your own

Copy `wrangler.toml` to a config of your own and choose a Worker name and R2 bucket name.
Create the bucket and deploy from `relay/hosted`:

```sh
cp wrangler.toml wrangler.custom.toml
# Edit wrangler.custom.toml to match the bucket name below.
npx wrangler r2 bucket create your-relay-frames
npx wrangler deploy --config wrangler.custom.toml
```

## Limits (`src/limits.js`, one table)

| Limit | Default | Refusal |
|---|---|---|
| stored bytes per identity | 5 GiB | `507 full` |
| rotation grace: min / max / default (`minGraceMs`, `maxGraceMs`, `defaultGraceMs`; tests lower the min) | 1 h / 30 d / 7 d | `400 bad_grace` |
| stored objects per identity (the index is SQLite rows; the byte cap rules) | 5,000,000 | `507 full` |
| new identities per network per day (first sight only; existing identities never counted; a network is an IPv4 address or an IPv6 /64, see `src/network.js`) | 200 | `429 too_many_identities`, `Retry-After` (seconds until that network's day ends, also `retry_after` in the body) |
| new frames per identity per day | 5,000 | `507 full` |
| requests per minute, per identity / per device | 1,200 / 600 | `429 rate_limited`, `Retry-After` |
| puts in flight per identity (a 16 MiB frame costs 2 to 3 times its size in memory) | 2 | `429 rate_limited` |
| frame body | 16 MiB (`MaxFrame`) | `413 bad_frame`, streamed or declared |
| any other body | 1 MiB | `413 too_large` (directory), `413 bad_frame` (store) |
| pairing: TTL, message, messages per side | 10 min, 4 KiB, 4 | `404`, `413`, `409 full` |
| pairing: creates per hour, writes per minute, open polls, per IP | 10, 30, 4 | `429 rate_limited`, `Retry-After` |
| pairing: live mailboxes in all (32 KiB each, so also 64 MiB) | 2,000 | `503 full` |
| link requests: TTL, create body, grant | 10 min, 1 KiB, 4 KiB | `404 gone`, `413 too_big` |
| link requests: creates an hour, pending at once, per IP | 10, 3 | `429 rate_limited`, `Retry-After` |
| link requests: reads a minute, misses a minute, open polls, per IP | 60, 20, 4 | `429 rate_limited` |
| link requests: live in all | 2,000 | `503 full` |

Stored quotas (bytes, objects, frames a day), the stats counts and the per-IP counters live in the identity object's SQLite and survive
eviction. A frame the store already holds costs no quota, so a client that resends is never refused
for it. The per-minute rates are kept in memory: an eviction can only forgive a burst.

## Deployment limits and data erasure

`CAF_LIMITS` optionally sets `globalNewIdentitiesPerDay` and `globalStoreBytes`.
Both are unlimited by default. Set either to a finite number to enable that limit;
`0` closes new admission or storage respectively. Per-identity and per-network limits
still apply.

A new identity over the daily admission limit gets `429 relay_admission_full` with
`Retry-After`. New identities at the storage limit, and new frames that would exceed
it, get `507 relay_storage_full`. Existing data stays readable, held frames can be
retried, and identity erasure remains available.

Every frame is under an identity prefix. Its owner can retire the identity through the
signed rotation route (normally `codeaf identity rotate`); after the grace period, an
alarm writes a tombstone, deletes the prefix a page at a time, releases its global capacity,
and drops its tables. Pairing and link data have bounded TTLs. There is no automatic
inactive-identity erasure: it is off, with no retention knob in this version. A device's
hibernating socket can stay live without requests, so a request-only last-seen timer would
be unsafe. If all identity keys are lost, owner erasure cannot be authenticated; the
operator's documented admission/storage brakes still bound new stored data.

`npm test` runs through `test/local.sh`: temporary config, cache, registry and log paths,
metrics and update prompts off, no dependency on a writable home. Use that wrapper around
local Wrangler commands too. The project `.npmrc` keeps npm's own installation cache and
logs below the ignored `.wrangler/npm-cache` directory, so `npm ci && npm test` also
works with a read-only home. `npm ci` installs only the lockfile's dependencies.

# Scoped clients, slots and queue

## Authentication and pairing

All routes use the existing `Codeaf-Identity`, `Codeaf-Cert`, `Codeaf-Time`
and `Codeaf-Sig` headers and `codeaf-req-v1` signing bytes (method, request
URI including query, request time, SHA-256 of the exact body). Identity namespaces
come exclusively from verified identity keys. Request skew is at most five minutes.
Errors are JSON `{"err":"code"}`. All replies include `Codeaf-Now` (Unix ms).
A store must be enabled. JSON bodies use UTF-8; sealed bytes use padded standard
base64. Unknown JSON fields and trailing JSON are rejected on fabric routes.

A full device pairs a client with `PUT /v1/clients/{device}` and body
`{"cert":<identity.Cert>,"methods":["method.name"],"name":"sealed name"}`.
The certificate must verify under the request's identity public key and name the
path device. The client generates its own Ed25519 key; the full device signs only
its public key with the existing certificate format. **Register before releasing
the certificate to the client**: legacy certificates carry no cryptographic scope.
The pairing grant contains only the certificate, identity public key and public
connection information; never cell keys, dedup secrets, identity private keys or
vault data. The relay accepts no grant or secret fields on this route.

Registration returns 204 and creates a directory device record with `kind: client`
and `methods`. At most 64 distinct method names, each 1–128 ASCII letters, digits,
underscore, dot or hyphen; an empty list is a read-only client. Repeat registration
returns 409 `exists`; scope changes require revocation and a new key. A full
existing device cannot be converted to a client. Old records without kind remain
full devices. `GET /v1/dir/list` by a full device includes the client records.
Clients cannot access directory, identity or blob-store routes (401 unauthorized),
including self-updates, approvals, leases and the vault. A client can read only its
own slots and append only its own queue. The method allowlist is checked at append;
the executing box must also check it before execution.

`POST /v1/dir/devices/{device}/revoke` is the existing full-device revoke.
A successful revoke deletes that client's slots, queue and sequence state. Its
certificate stays revoked across restarts; requests return 401 `revoked`.

## Slots

`PUT /v1/clients/{device}/slots/{name}` by any full device of the same identity
replaces one slot. Body: `{"sealed":"base64"}`. The receiver must be an active
client. Names are 1–128 ASCII letters, digits, underscore, dot or hyphen. Maximum
128 slots per client (a computer publishes up to twelve: a card, an outcome list and
up to ten read snapshots, so 128 serves ten computers) and 65536 decoded bytes per
slot (nonempty). Success: 204. The reply carries `Codeaf-Client-Watching: 1` while
the phone is watching (see "Watching"), and no such header otherwise. Writers are responsible for sealing to the current client key and
publishing only chats whose lease they hold; the relay cannot inspect sealed data.

`GET /v1/clients/{device}/slots/{name}` by that client returns 200
`{"sealed":"base64","writer":"dev_…","at":1700000000000,"version":7}`.
The relay sets writer, Unix-ms time and `version`, a per-client counter that rises
by one on every slot `PUT` (see "Slot wait"). A read counts as activity for the
watching state. Slots expire at age 24 hours, including
across restart, and expired slots free capacity. Full devices cannot read slots.

## Queue

`POST /v1/clients/{device}/queue` by that client accepts
`{"number":1,"at":1700000000000,"method":"method.name","target":"any","sealed":"base64"}`.
Numbers start at 1 and increase by exactly one per client, even after ack or
expiry. Integers must fit signed 64 bits. `at` is Unix ms, no more than five
minutes in the future and strictly younger than 24 hours. Each nonempty sealed
entry is at most 4096 decoded bytes; at most 128 unacknowledged entries per
client. Target is `any` or an active registered full device id in this identity.
The signed HTTP request authenticates all fields. Success: 204. Invalid numbers
(replay or gap) return 409 `sequence`; stale timestamps return 400 `stale`.
Denied methods return 403 `method_denied`. Expired entries free capacity but
never reset numbering.

`GET /v1/clients/{device}/queue` by a full device returns 200 `{"entries":[]}`,
ordered by number, with fields above plus `signature`, `cert`, `identity`,
`request_time`, `request_uri`, and `body` (base64 exact original JSON bytes).
These preserve the original request proof for independent box verification.
Only entries targeted at the caller, unclaimed `any` entries, or `any` entries
claimed by the caller are listed. A list does not claim or change a chat lease.

`POST /v1/clients/{device}/queue/{number}/claim` by a full device atomically
claims an `any` entry; success 204, another claimer 409 `claimed`. The body is
empty, or `{"token":"<64 lowercase hex>"}` (32 random bytes the claimer persists
before it claims): the entry is then owned by that caller and token, the same
caller and token may claim it again so a lost reply is recoverable without a double
execution, and a claim without the token on an entry that is already claimed is 409
`claimed`, even from the same caller. Listings show only the owning device id and
never the token. A targeted entry is claimable only by its target. Claims persist
until ack or expiry; the relay never automatically reassigns work after a crash.
An executor must claim before running. This prevents competing boxes executing
the same entry; durable executor-side idempotency is still required after a crash.

`DELETE /v1/clients/{device}/queue/{number}` by the claim owner deletes the entry
and returns 204. Ack without owning the claim returns 409 `claimed`. Missing or
expired entries return 404 `not_found`. A box must recheck age, signature, method
scope, routing and any required biometric flag inside the sealed call, and log
execution. The relay cannot verify encrypted call contents or biometric evidence.

## Enrollment: what admits a phone (identity-signed)

A phone is admitted by an **enrollment** the owner's identity key signs; any
computer can sign one, because every computer holds the identity key. The relay
verifies and stores it, and every computer reads and verifies it again before it
publishes anything to that phone.

`PUT /v1/clients/{device}/enroll` by a full device stores
`{"v":1,"device":"dev_…","sign_pub":b64,"box_pub":b64,"methods":[…],"reads":[…],"ver":1,"sig":b64}`
(exactly those keys; the key bytes are standard padded base64). The signature is
Ed25519 by the identity key over `codeaf-enroll-v1\n`, the device id, the hex of
both public keys, the sorted methods joined with commas, the sorted reads joined
with commas, and the decimal `ver`, each on its own line. `device` must be
`dev_` and the first 32 hex characters of the SHA-256 of `sign_pub`. Names follow
the method-name rules above, at most 64 in each list, no repeats. Success is 204
and the device does **not** have to be registered yet: the approving computer
enrolls first, then registers the client, then decides the link request, and each
step can be repeated. `ver` must be above the stored one (409 `version`); an
identical re-send is 204; at most 16 enrollments per identity (429 `full`); a
signature or device binding that does not verify is 400 `bad_request`; a device id
that is already a computer, or a revoked one, is 409 `exists`.

`GET /v1/clients` by a full device lists every stored enrollment as
`{"clients":[{"device":"dev_…","enroll":{…}}]}` sorted by device id, **including
the enrollments of revoked phones**: the relay keeps the row after a revoke so the
version cannot roll back there, and a computer cross-checks each entry against the
directory. The relay never decides whether an enrollment is current: computers pin
the highest version they have verified.

## Pairing a phone: the client-class link request

The link request a joining device makes (`POST /v1/link/requests`) takes two more
fields for a phone: `"class":"client"` and `"bind"`, the base64url of 16 bytes.
The relay gives such a request a **ten-character** code (a computer's stays eight,
which is how an older computer, whose parser accepts only eight, cannot approve a
phone), stores `class` and `bind` unchanged and serves them back on
`GET /v1/link/requests/{code}`. It never computes or checks `bind`: it is a MAC
over the phone's two public keys under the link key that only the phone and the
approving computer hold, so a relay that substitutes keys cannot make one.

For a client-class request the approve route, `POST /v1/dir/requests/{code}/approve`,
takes only `{"class":"client","grant":"<base64url sealed grant, at most 4096 bytes>"}`.
It answers 409 `not_enrolled` unless the phone is already registered as a client of
this identity and an enrollment is stored for it; otherwise it marks the request
approved and keeps the grant for the phone to read. It writes **no device row and
nothing to the directory or to Raft**: the grant is only a sealed message, and the
phone's registration is the earlier registration route. A repeat of the same
decision is 204; a different later decision is 409 `already_decided`. A
full-device approve body (`device`, `cert`, `grant`) for a client-class request is
400 `bad_request` and leaves it pending, so a phone can never be handed the
identity; a client body for a computer's request is also 400.

## Computers, serving and watching

`GET /v1/clients/{device}/computers`, by that client, answers
`{"now":ms,"computers":[{"device":"dev_…","serving":true,"at":ms}]}` for each
non-revoked computer of the identity. **Serving** means a runner is alive there: the
computer holds a directory watch socket opened with `serving=1`
(`GET /v1/dir/watch?events=1&beat=10&serving=1`; only the exact value `1` counts)
and the socket is live by the same rule as presence. `at` is the computer's
last-seen time as presence reports it. There are no names: they come from the
computers' own signed cards.

A phone is **watching** while a slot wait is parked for it, and for 30 seconds
after its last wait ended or slot was read (the `Codeaf-Client-Watching: 1` header
on a slot `PUT`) and for 45 seconds for the event. When the state changes the relay
sends `{"t":"watching","device":"dev_…","on":true,"at":ms}` to event sockets of the
directory watch, replays the current state to a socket when it connects, and sends
at most one frame per phone every two seconds (a faster flip is held and the latest
state goes out when the window has passed). A computer that learns a phone is
watching publishes every changed slot at once; otherwise it writes each slot at
most once per 30 seconds. The header is the fallback when its watch socket is down.

## Slot wait

`GET /v1/clients/{device}/slots?since=<uint64>&wait=<seconds>` by that client
answers 200 `{"version":N,"slots":["name",…]}` when the client's slot version is
greater than `since`: the names of the slots whose own version is greater, sorted.
Otherwise it holds up to `wait` seconds (0 to 25; 0 never waits) and answers 204.
`since` and `wait` are required; a body, a malformed value or a wait over 25 is 400.
A client has at most two waits parked (429 `full` for a third); the Go relay also
bounds the waits on its process at 1,024, and the Worker bounds them per identity
at 32, because each identity there is its own object. A revoke ends a parked wait
with 401 `revoked`. A wait whose caller has gone ends when the relay sees the
connection close, and in any case when its own wait runs out, so it can count
against the two for up to 25 seconds; a client that gets 429 reads once with
`wait=0` and asks again after a few seconds. (Under `wrangler dev --local` the
Worker does not see the close at all, and the live conformance case
`SlotWaitCancellation` fails its last step there; the Go relay passes it.) The
version is a per-client counter that rises by one on every
slot `PUT`; each slot records the counter value of its last write. These
semantics are fixed; how the Worker holds the request (a held request now, a
notification socket if the cost below says so) may change without changing them.

## What the hosted Worker's slot wait costs

This is a calculation from Cloudflare's published Durable Objects prices on
2026-11-27 (requests $0.15 per million; duration $12.50 per million GB-seconds,
billed at 128 MB; rows written $1.00 per million; incoming WebSocket messages count
at 20 to 1), **not a measurement on a deployed relay**: nothing was deployed to
measure. Treat each figure as a model until staging counters replace it.

- One identity's object held awake by a 25-second wait kept parked all day is about
  10,800 GB-seconds, **$0.135 per identity per day**, shared by that identity's
  phones. A phone in the foreground three hours a day costs about $0.017. A
  hibernating notification socket costs under $0.001 a day at rest. The wait is
  foreground-only, at most two per phone and 32 per identity, and the `slotWaitMax`
  limit turns waiting off (0 answers 204 at once), so the held request is the v1
  transport and the socket is the fallback if staging counts exceed the model.
- A slot `PUT` costs more than one write here: the request, the Raft reads for the
  caller and the target, and the rows written. The code's model is about $3.60 per
  million `PUT`s; four a second for a day on one chat is about $1.24 with no guard,
  against about $0.01 with the 30-second not-watching guard. The guard, not the
  transport, is what keeps the bill bounded.

## Refusals, persistence and implementations

Malformed bodies, names, targets or empty sealed values: 400 `bad_request`.
Oversized HTTP bodies (maximum 96 KiB), decoded blobs or method lists: 413
`too_large`. Slot/queue capacity: 429 `full`. Unknown client/slot/entry: 404
`not_found`. Wrong role or identity: 401 `unauthorized`; revoked certificate:
401 `revoked`; request skew: 401 `skew`; replaced identity: 410 `rotated`;
deleted identity: 410 `gone`. Unsupported methods: 405. Internal storage failure:
500 `internal`. Nonempty bodies on bodyless routes: 400 `bad_request`.

Records, sequence watermarks, proof and claims survive relay restarts. Fabric
mutations and revocation serialize within a relay process. One running Go relay
owns a store directory, as with its in-memory directory revocation feed.
Expiry is evaluated on reads and writes with the relay clock. Identity retirement
erases fabric data with the existing directory and blobs.

The Go relay and the Worker implement this contract.

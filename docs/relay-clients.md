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
64 slots per client and 65536 decoded bytes per slot (nonempty).
Success: 204. Writers are responsible for sealing to the current client key and
publishing only chats whose lease they hold; the relay cannot inspect sealed data.

`GET /v1/clients/{device}/slots/{name}` by that client returns 200
`{"sealed":"base64","writer":"dev_…","at":1700000000000}`.
The relay sets writer and Unix-ms time. Slots expire at age 24 hours, including
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
claims an `any` entry; success 204, same claimer repeat 204, another claimer
409 `claimed`. A targeted entry is claimable only by its target. Claims persist
until ack or expiry; the relay never automatically reassigns work after a crash.
An executor must claim before running. This prevents competing boxes executing
the same entry; durable executor-side idempotency is still required after a crash.

`DELETE /v1/clients/{device}/queue/{number}` by the claim owner deletes the entry
and returns 204. Ack without owning the claim returns 409 `claimed`. Missing or
expired entries return 404 `not_found`. A box must recheck age, signature, method
scope, routing and any required biometric flag inside the sealed call, and log
execution. The relay cannot verify encrypted call contents or biometric evidence.

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

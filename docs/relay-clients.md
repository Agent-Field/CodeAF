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
claims an `any` entry; success 204. An optional signed JSON body
`{"token":"64 lowercase hexadecimal characters"}` binds ownership to one process.
Only that same device/token can recover its claim (204); a fresh token or a
repeat without a token returns 409 `claimed`. Queue listings never disclose the
token. A targeted entry is claimable only by its target. Claims persist
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

## Full-device executor snapshots

The app backend keeps locally approved client encryption keys and requested
snapshot methods beside its own state. It dispatches reads through `appwire`;
chat results include only live leases held by this device. The v0 snapshot set is
`agents.list`, `asks.list`, `tasks.list`, `inbox.list`, and `spend.today`.
Unsupported read names are refused when pairing rather than invoked as reads.
The method allowlist remains separate from the snapshot subscription list.

One slot per method and writer is named `<method>.<writer-device-id>`. Its
NaCl anonymous-box plaintext is a JSON array of envelopes:
`[{"writer":"dev_…","at":"RFC3339Nano","agent":"chat-id","result":{…}}]`.
Global results omit `agent`; every chat result, including an empty list, includes
it. Client encryption keys are independent X25519 keys supplied during local
pairing. The grant includes this executor's public X25519 key for queued calls,
the writer id, relay URL, identity public key and certificate, never shared
identity secrets. Client keys and subscriptions enroll on each executor locally; a second trusted
executor can enroll the existing client only with the exact same relay method
scope. Each grant supplies that executor's distinct encryption public key. The
relay directory does not distribute encryption keys.

Completed sync publishes and app turn/ask/task events schedule coalesced refreshes.
The runner also refreshes every five seconds while active, backing off failures
to five minutes. Identical results retain their ciphertext and publication time;
unchanged slots refresh after twelve hours so they do not expire at the relay.
No active clients means no snapshot reads, relay requests or refresh timer.

## Executor queue payload and receipts

The queued ciphertext is a NaCl anonymous box sealed to the executor's public
box key from its grant. The plaintext is
`{"method":"asks.answer","params":{"agent":"chat-id","id":"ask-id","option":"option-id"},"face_id":true}`.
`face_id` is optional for reversible asks and mandatory for irreversible asks;
it is the client's signed assertion, not server-side biometric attestation.
The plaintext method must match the signed outer method exactly.

The v0 executor accepts `asks.answer`, `agents.send`, `agents.stop`, and
`agents.create` in a pairing's queue allowlist. Other mutation names are refused
at pairing because their routing and approval rules are not defined here.
Chat calls require this executor's current unexpired lease. Creation targets
`any` and is atomically claimed. The queue never takes a lease.

Executors independently verify the retained exact HTTP body and signature,
identity, client id, route, age, method scope, target, sequence and ask's current
irreversibility. A durable started receipt precedes dispatch, and a done or failed
receipt follows it. Restart never repeats a started call: its outcome is
uncertain. A lost acknowledgement is retried from the receipt, without dispatch.
Receipts include who, when, method and chat; the app exposes them in that chat's
history. The created chat receives the final receipt for new work.

Local sequence numbers start at one and must be contiguous unless the client
explicitly authorizes missing numbers. The relay filters calls targeted at other
boxes and omits expired calls. To resume after such a gap, the sealed call may
include `skip_through: number-1`. Its original client signature covers that
instruction. The executor records the skipped local range in its receipt and
chat entry. It never skips an earlier eligible entry still present in its queue
list; a refused pending entry stops the round. Without that explicit instruction,
gaps remain refused. Numbers already executed or recorded as started are never
executed again, including when a skip instruction is present. Uncertain calls
need a person to inspect the chat; they are acknowledged without an automatic
retry of their possible effect.

An `any` call can address several enrolled executors without sharing their
private keys. The sealed bytes start with ASCII `codeaf-client-boxes-v1` and a
newline, followed by a JSON object mapping executor device ids to base64 NaCl
anonymous boxes of the same call. A box opens only its own member, then competes
for the relay's atomic claim. `clientrun.SealForExecutors` builds this format;
the complete payload still fits the 4096-byte queue limit. Single-executor raw
anonymous boxes remain supported. Enroll the client on each eligible executor
and retain its public grant before constructing a multi-executor call.

## Settings over appwire

Engines implementing scoped-client management advertise `clients.manage` in
hello. `clients.list {}` returns `{clients:[{id,name,methods,reads,paused}]}`
with no secrets. `clients.pair {signing_key,box_key,methods,reads,name}` takes
base64 public keys, issues and registers the scoped certificate, and returns the
public grant described above. Pair only after the person has approved the
public keys and requested scope through a trusted local desktop connection.
The chat's ordinary full-device pairing command remains separate.

`clients.pause {id,paused:true}` stops this box's snapshots and queue execution
without revoking access to existing slots. `paused:false` resumes. Pause persists
across restart and waits for an in-flight round before returning.
`clients.unpair {id}` revokes the certificate at the relay, deleting slots and
queue, then removes the local enrollment; execution receipts remain in chat
history. Methods are available through the same table for every appwire client.
They add no desktop Settings widget by themselves; UI callers render the list
and controls using this interface. A scoped client's v0 queue does not expose
management mutations; those require a trusted live desktop connection.

## Snapshot capacity and independent failures

The complete encrypted method/writer snapshot must fit the relay's 64 KiB slot
limit. An oversized method is refused before upload, retaining its previous
snapshot and its original timestamp. The round reports the size refusal and
continues updating the client's other subscribed methods. Snapshots are not
silently truncated or split into an unadvertised paging format. Large result
pagination remains outside v0; clients must treat the retained timestamp as stale.
After the last unpair, startup does not open sync, start an executor or install
its publish watcher. Chat lines remain in each conversation’s history.

## Face ID for a whole consent batch

A queued answer requires the signed `face_id` assertion when any open ask it
resolves is irreversible. The check uses the fixed targets in the actual answer
path, after claiming, so a sibling arriving during the claim is covered too.
This includes a whole consent batch and a different
ask selected by `batch_item`. Without it the call is refused before any consent
is answered, and the paired-client refusal is kept in the chat with “Face ID”.

## One local runner at a time

The runner holds an exclusive OS lock beside its state file until it closes.
Another process using that home fails immediately with “client runner state
already open”. The runner persists a random ownership token in a `claiming`
receipt before requesting the claim. If the reply is lost, restart recovers
with that token; another process with separate state cannot reuse the claim.
After ownership is confirmed, a durable `started` receipt precedes dispatch.
Restart acknowledges a started call without executing it again.

## Interrupted new work keeps its chat receipt

For queued `agents.create`, the app reserves the generated chat id and transcript
path with the runner before creating the folder. That association reaches the
started receipt durably before minting. The paired-client line is written before
the first turn starts. Restart repairs the line if death happened immediately
after minting, even before the create result or derived session metadata exists.
If the reserved folder was never created, recovery invents no chat. Both cases
acknowledge the uncertain call without creating another chat.

## Receipt retention and unpair history

Acknowledgement keeps only the latest call’s proof and sequence watermark for
each paired client; older numbers still cannot run again. An in-flight call may
also retain the preceding watermark until acknowledgement. Lost acknowledgement
responses do not accumulate history. Unpair removes that client’s runner receipts.
Chat lines are saved separately in the conversation and survive both trimming
and unpair. Older state files are streamed one receipt at a time into chat logs,
then compacted, instead of loading their whole history into memory.

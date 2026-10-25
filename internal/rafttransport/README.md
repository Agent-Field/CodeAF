# Native peer transport

This is the authenticated peer bridge, separate from application ingress. The
persistent native relay assembles its router from committed membership and serves
personal admission, HTTP exchange and the outbound push socket on its ordinary
listener. `relayserve.Config.Cluster` is the internal custom-host handler seam.
The hosted relay serves the same personal admission, member transport and control status doors. A paired codeaf home runs its durable learner inside the terminal state-sync lifetime. No environment switch selects another writer.

## Host ordering and authorization

Create `NewRouter(DefaultLimits())`, then `Install(View)`. A view binds identity,
cluster UUID, epoch, protocol, revision, durable router generation and all member
IDs/incarnations/public transport keys. Install comes **after** the enrollment
transaction commits and **before** acknowledging that change or dispatching more
peer work. Serialize committed registry changes through this seam. It must never
be filled by an HTTP caller, a pairing nameplate, a socket, or a key presented
without committed enrollment. Anchors use protocol-only keys, never identity roots.

Every request, including an empty poll or acknowledgment, is signed. Every
outbound envelope is independently signed by its actual source. The router checks
sender and destination enrollment/incarnation, scope, digest, signature and the
inner Raft from/to/type. Local control, proposal forwarding and storage-completion
messages are refused. The core repeats its own peer-input checks. A view update
shares the routing lock, invalidates buffered traffic and notifies open sockets;
requests signed before a committed revocation are refused afterward. Retired node
records cannot disappear or regain authority. Key changes require a higher
incarnation. Host policy still owns owner consent, certificate verification,
configuration changes, tombstones and registry persistence.

The routing inbox is expendable memory. Before restoring service after loss of
that memory, increment and durably store the view's router `Generation`, then
install it. That fences all old delivery cursors and sockets. Member
incarnations also come from durable enrollment. Do not replace either number
with a random connection ID. A router has no automatic bootstrap, singleton
fallback or authority read. Membership policy must enforce a single durable
epoch for each identity; registering two unrelated scopes is not migration or
permission to run two writable clusters.

## Exchanges and sockets

`POST /v2/cluster/exchange` takes one canonical `Request` and returns `Response`.
`Client.Exchange` makes one bounded outbound request, refuses HTTP redirects and
starts no background work, listeners, retries or discovery. Hosts enforce paired
and sync-off policy **before** creating/using this client.
Use HTTPS with the configured relay's authenticated TLS endpoint (loopback HTTP is
used by the hermetic tests). Keep protocol admission separate from the public
application-command limiter, and configure an operator ingress rate/connection
budget in addition to these bounded transient queues.

`GET /v2/cluster/socket` upgrades an outbound connection. `Codeaf-Cluster-Auth`
contains raw-URL-base64 of a signed empty Request, with no outbound messages.
The first binary response is the current inbox. Subsequent signed binary Requests
use the same node/scope/incarnation/router generation and receive responses;
queued traffic also pushes a response without requiring a poll. Each frame and
push rechecks enrollment. The channel carries no lease or append acknowledgment.
Compression is disabled. Initial/body/frame/write/read bounds apply, and at most
eight sockets per member can be open. A delivery awaiting acknowledgment has a 30-second response deadline. An empty
subscription needs no keepalive: the socket stays passive until committed work
wakes it. Reconnect failures use HTTP fallback/backoff. No socket is required on a member.
Hibernation is a hosted-runtime responsibility; the Go server has ordinary sockets.

Responses are hints to transport delivery, not proof of persistence. Before core
dispatch, the receiving host checks the envelope signature using the **current**
committed peer key and incarnation, its expected identity/cluster/epoch/protocol,
and its own destination/incarnation. Remote hosts must not trust a stale cached
registry to authorize traffic. Recheck after asynchronous waits; discard callbacks
from an old actor generation. Only step a message once the receiver is available
for it. Keep/retry `ErrBusy` input. Keep all persistence/application boundaries:
`Persist -> Persisted -> Apply -> Applied`; a relay receipt completes none of them.

## Wire version 1

`envelope.go` and `exchange.go` define fixed-order JSON structs. All uint64 values,
including the three ack-array elements, are canonical decimal strings. Protocol
and lane are small JSON integers; bytes are padded standard base64. Scope strings
are 1–128 ASCII letters, digits, hyphen, underscore or dot. Digests are lowercase
SHA-256 hex. Strict decoding requires exact re-encoding: unknown/duplicate fields,
extra array elements, alternate numbers, whitespace and trailing bytes are refused.

Envelope field order is
`scope,from,to,incarnation,destination,sequence,chunk,digest,
payload,signature`; scope is `identity,cluster,epoch,protocol`; a non-null chunk
is `generation,offset,hash`. Request order is
`scope,node,incarnation,generation, messages,signature,ack`. Signing replaces
signature with JSON null and prepends `codeaf/raft/envelope/1\n` or
`codeaf/raft/exchange/1\n` respectively. No JSON object key sorting or JS Number
conversion is allowed. The committed independently Node-signed
`testdata/exchange-v1.json` is checked by Go and the targeted hosted Node test.
Hosted codecs must preserve that same wire representation.

## Bounds, priority and retry

Normal batches contain at most 64 messages and 64 KiB total raw Raft bytes. A
snapshot request contains exactly one chunk of at most 256 KiB. The encoded request
and response ceiling is 512 KiB. All signature/envelope overhead counts toward
queued memory. Each identity has at most 1 MiB of queued bytes: 384 KiB reserved
for voter-to-voter normal traffic, 128 KiB for traffic involving a learner, and
512 KiB for snapshot chunks. A member inbox has at most eight deliveries, reserved
as four voter, two learner and two snapshot slots. Reservations cannot be borrowed
by other classes. Class comes from committed enrollment, never a sender's claim.
Normal voter traffic drains before learner traffic and snapshot chunks. No messages
are coalesced, including heartbeat read contexts, votes, appends and read barriers.

`Delivery` has a separate monotonically increasing cursor per lane. Ack only the
contiguous delivered prefix of each lane after the host has accepted those inputs;
copy a cursor into the corresponding Request.Ack slot and sign it. Acks above the
highest actually issued cursor are refused. A lost response redelivers the same
bytes/cursor. Pending exact duplicates consume no extra slot; sequence conflicts
cannot replace a pending packet. After ack or expiry an old sequence may be
redelivered: Raft handles duplicates/reordering. There is no unbounded transport
replay table and no application request-dedup claim.

A batch with an invalid member/signature admits no outbound message. Backpressure
admits no part of an otherwise valid batch; valid incoming acks still drain that
caller's inbox to avoid a circular queue-full stall. Copy retained bytes before
returning. `ErrCapacity`/HTTP 429 means retain/retry or report failed transport to
the core, never a successful append. Socket responses carry optional `accepted`
and `rejected` arrays of canonical decimal envelope sequences. They describe
transient routing admission only, never Raft persistence. A rejected batch leaves
the socket usable for other voters. Senders retain at most eight outstanding
socket sends until that response and report failure to the core on rejection.
Optional `chunks` credit lists the target node and canonical envelope sequence
after that target acknowledges staging a chunk. A personal snapshot sender keeps
one chunk outstanding per target and advances on this pushed credit, so a large
snapshot cannot overrun the unchanged chunk reservation. Credit is transient
flow control; only the actual durable snapshot append reply completes Raft
snapshot delivery. A partially staged joining member keeps its actor while
subscribing passively for the next chunk; readiness still requires application.
Retry with host-owned exponential backoff,
seeded jitter and a 60-second ceiling. Buffers expire after 30 seconds of monotonic
sampled host time, lazily on activity, requiring no idle alarm. Raft retransmits;
lease renewal and snapshots retain their own durable semantics.

Default operator admission is 1,024 scopes, 4,096 retained enrollment records
per scope, at most 32 live learners and six live voter IDs (room for both sides
of a three-voter joint change). `Limits` makes scope/retained-record/voter
ceilings explicit operator choices; exceeding one refuses growth. Larger
topology policy belongs to membership policy, not transport. Tombstones and
application records are not deleted to regain admission. `Stats` reports
counts/bytes/socket usage only, never payloads.

## Snapshot host responsibilities

A `MsgSnap` contains a small portable manifest pointer. Chunk envelopes carry
that manifest's generation ID, zero-based chunk number and SHA-256. The receiver
must verify generation, offset, expected length and manifest totals before
`BeginSnapshot/PutSnapshotChunk/CompleteSnapshot`. A queued chunk or completed
stream is **not** installation: host success is reported through
`raftcore.TransportResult{Snapshot:true,Success:true}` only after the actual
snapshot Ready has installed durably. A timeout/interruption reports failure so
Raft can retry. Do not feed `MsgSnap` into the core before its chunks are ready.
Hosted actor/storage adapters own those operations and native/wasm lifecycle
verification.

The transport tests use real cores and three SQLite stores behind the actual
relay route, with only outbound member HTTP, intentional duplicate/reorder,
create, ReadIndex, leader death/restart, joint demotion and writes with an
absent learner. Transport unit tests pin auth, generation, socket revocation and
all queue reservations. The 2,000-seed simulator soak remains a required
regression gate. Full enrollment/worker journeys, churn/snapshot
interoperability, application revocation and full-state sentinel campaigns
require separate verification.


`CommittedView` derives protocol authority from the applied registry and
current device revocations. Hold the actor lock while reading and installing
that view, before acknowledging the command. Use the applied index as its
revision, so a device revoke updates routing even without a membership edit.
Retain retired entries; Router.Install refuses their resurrection. Increment
and persist router generation before admitting exchanges after inbox loss.
An authenticated source's three provisioned anchor keys may bootstrap protocol
traffic before registry commitment. A personal key requires committed enrollment;
never accept a caller's arbitrary view as an enrollment grant.

`CommittedViewAt` supports delegated worker learners. Commit current ingress
time and supply the applied configuration/read-set under the actor lock, then
Install before acknowledging the result. It checks the worker's explicit learner
consent, exclusive certificate expiry, revocation, rotation and current process
incarnation. The old metadata-only CommittedView marks workers revoked, so a
host that has not supplied these dependencies fails closed. Worker restarts
cannot resurrect a retired peer: remove/re-enroll with a fresh node ID.

## Native personal learners

The persistent native relay now owns its committed router and mounts enrollment,
exchange and socket routes on its ordinary listener. `Config.Cluster` remains the
internal custom-host assembly seam; the default native service installs its own
router. The hosted relay serves these member and control routes through its identity coordinator.

`POST /v2/cluster/enroll` uses the existing `reqsign` headers, original paired
personal certificate and body `{"store":"<64 lowercase hex characters>","protocol":1}`.
The client generates that random store token once when creating its SQLite store
and persists it alongside the store. Reopening it reuses the token. Wiping it
requires a new token: the old node is permanently retired, including its store
mapping and tombstone. Admission allocates from committed `NextNode`; caller IDs
and keys are never allocation authority. The original signed request is retained
and checked by the replicated evaluator against the committed root and revocation.
Worker certificates and retained worker/service roles cannot use personal admission.

The reply is `raftdirectory.PersonalEnrollment`: scope, durable router generation,
node, incarnation, advertised barrier and committed member public keys. The four
uint64 fields cross JSON as decimal strings. A first reply may have barrier `"0"`:
registry admission committed, and activation/maintenance completes the separate
learner configuration. Repeating the same request refreshes the grant without
allocating another node. A nonzero barrier is the committed settlement index;
readiness additionally requires actual learner-local application through it.

An exchange Request may include `applied` as a decimal string, signed with all its
other fields. The faithful host sets it only after its SQLite application commit.
The control Status `ready` list contains current learners whose signed durable
progress covers their advertised barrier. Leader replication Match and transport
acknowledgments alone never set ready. Router restart forgets this advisory evidence
and requires a new signed report. Registered keys and generations are checked on
HTTP and every socket frame/push. Ordinary public ingress or a returning peer may
activate an existing durable namespace; a peer cannot bootstrap an unknown one.

The router's Pump callback runs outside its lock. The native actor drains its
anchor inboxes through LocalExchange and completes persistence/application under
its queue, then makes bounded messages available to the outward-dialing learner.
It never dials or waits on that learner. Snapshot manifests precede ≤256 KiB chunks
on the separate chunk lane; the recipient stages all chunks before stepping the
snapshot into its core and reporting application. Retention or a lost transfer
reports failure to Raft for retry, while normal snapshots compact independently
of learner lag. All-voter quiescence discards expendable learner deliveries and
stops scheduling without learner acknowledgments. Fresh public read/write input
wakes the ordinary quorum path.

The codeaf client starts its personal learner only inside the existing terminal
state-sync lifetime, or a scoped status activation, after current quiet guards.
Open remains local. Hosted relays offer enrollment and control status. Worker
setup remains unavailable. Native personal leadership uses the separate application ingress below; hosted leadership remains unavailable.

The native host advances its checksummed `transport-generation` file with atomic
rename and directory fsync before opening its router; it never mutates the source
provision. Counter corruption refuses activation and retains evidence. During cold
voter activation external deliveries are unreachable until the committed router
is installed. A maximum-sized legal command can acquire protobuf framing larger
than the normal peer budget. In that case the host checkpoints only the applied
prefix and uses snapshot chunks for catch-up, keeping both existing byte limits
and successful voter operations intact.


## Native personal leaders and application forwarding

A native personal member also opens `/v2/cluster/forward` outward. This is a
separate application connection, never a Raft peer envelope. The upgrade uses
the same enrolled, signed scope/node/incarnation/router-generation proof. Every
application frame signs its scope, generation, source/destination, request ID,
action and complete body under `codeaf/cluster/forward/1`. Only the immutable
provisioned anchor keys route work; each reply uses the actual personal key.
Current committed revocation/generation is checked on every send and receive.
The existing eight-inflight and 512 KiB frame limits bound this channel, with
the ordinary 30-second control deadline and no idle keepalive.

The relay forwards the original signed command unchanged or a request-specific
ReadIndex operation. The elected personal core persists/applies it normally and
returns its actual admission/read index; the relay independently waits for its
own durable application through that index. A prior dedup result alone cannot
acknowledge a newly forwarded proposal. Public calls retain their operation
serialization but release the member queue while waiting, so authenticated peer
traffic and per-member persistence stay live. Calls canceled while still queued
are refused before admission; an admitted disk transaction finishes despite a
lost HTTP reply. A changed leader hint retries the
same original request, never a cached successful response.

Native leadership reconciles only committed owner intent through explicit joint
entry, durable leave, settlement and actual transfer. Target evidence comes from
its own durable store and actual recent reverse replication. Success follows a
current-term commit, fresh read, durable completion and another fresh receipt.
A+B restore C using the same planner after winning a new term; P is never asked
for removal consent and never automatically regains leadership. Disconnecting a
voting member wakes the ordinary scheduler, including after all-voter sleep.
Personal snapshot senders stream the same portable manifest/chunks to anchors
and learners; receivers stage and checksum chunks before stepping the pointer.
Committed revocation may precede removal from the Raft configuration. Senders
discard traffic lacking a currently authorized destination, preserving other
voters rather than failing the whole member.
The member and both connections share the existing terminal/scoped lifetime and
all use-time quiet guards. No content route or new daemon is involved.

A maximum-sized legal proposal can acquire protobuf framing larger than the
normal peer envelope. A personal leader sends that one `MsgApp` in the existing
chunk lane: offset zero and generation equal to the payload hash identify the
complete framed append, independently of any snapshot manifest. The envelope
checks its actual inner from/to/type and only permits `MsgApp` in this mode.
Snapshot generations remain hashes of manifests, not chunk payloads. Recipients
step the authenticated append normally, and only actual Raft persistence and
application acknowledgments count; routing/chunk credit cannot commit it. The
64 KiB proposal/normal-envelope, 256 KiB chunk and all queue caps are unchanged.

The personal store reserves an outbound sequence range in its existing durable
per-open identity write before any frame can escape. A process restart skips the
unused remainder, so an old unacknowledged learner delivery cannot collide with
new bytes under the same node/incarnation. No fsync per peer frame is needed.
Final sleep also waits for the member's bounded sends to be admitted. After the
all-voter marker, obsolete nonvoter append/heartbeat responses cannot wake the
leader; actual work, read requests and higher-term messages still do.

## Hosted personal learners

The hosted coordinator publishes routing only from committed anchor records.
Its generation is durably advanced before restoring expendable inboxes, so old
cursors and socket proofs cannot survive coordinator memory loss. Hibernating
peer and passive application sockets retain separate tags and attachments;
watch presence, broadcasts and watcher admission never count them. A stale
connection closes and the same durable home refreshes its enrollment grant.
The relay never opens an outbound connection to a personal member.

Admission and status use shared Go policy through the reactor. A committed
admission survives a lost reply before configuration; activation or maintenance
reconciles only pending intent. The original signed device request remains in
the enrollment command. A fresh quorum status reports readiness only after the
enrolled key signs durable application through its advertised join barrier.
Leadership remains unavailable on the hosted relay, and application sockets
stay passive. Peer ingress progresses independently of the public request queue.

Snapshots send a manifest followed by chunks on their reserved transport lane.
Each target has one outstanding chunk and advances only on signed delivery
credit; core snapshot success still waits for durable target application.
Learners never count toward quorum or hold back compaction. All-voter sleep
clears expendable learner deliveries and partial transfers; returning learners
catch up from the applied checkpoint and log.

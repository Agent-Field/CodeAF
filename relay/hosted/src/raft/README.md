# Hosted consensus actors

`RaftMemberDO` is exported by the existing Worker. The build entry
`src/raft-worker.js` statically imports the generated `core.wasm` and installs
one shared reactor per isolate. Wrangler's local build command compiles that
artifact from the pinned modified Go Raft source. The bindings add independent
SQL objects. The application adapter and irrevocable migration gate route
identity coordination to those members.

The internal namespace provisions an immutable cluster/node/bootstrap/seed
configuration before bootstrap writes. All anchors use the same initial voter
set. An owner/enrollment adapter must supply durable committed enrollment and
an application evaluator before there is an application proposal door. The
current production class has neither a public proposal endpoint nor a fallback
JavaScript lease writer. The test subclass explicitly supplies ProbeApply.

The actor queue spans every storage await and admits at most 64 callbacks.
Core calls synchronously copy/free their binary buffers. SQL uses
`transactionSync`; `storage.sync()` finishes before Persisted, Applied, any
reply or outbound dependent message. Failures stop participation and preserve
the database. Initialization/recovery is serialized too. Recovery starts as a
follower and never serves an authority read from remembered status.

Application writes and index/configuration/version/time commit atomically. The
evaluator is synchronous with an immutable applied view and bounded 1 MiB
read/write sets. It must implement committed policy, declared dependencies and
request/session semantics; storage supplies atomicity, not that policy. Rows
cover state, sessions, membership, enrollment, tombstones and outboxes. The
persisted application ceiling defaults to 8 MiB, including framed keys/values;
an explicit larger imported ceiling is preserved. Pressure refuses growth, never
deletes application rows. The migration host inventories and grandfathers
imported rows.

Snapshots use `internal/raftsnapshot` format 1 without a second JS schema.
Rows stream in SQL binary order into 256 KiB chunks. The shared Go codec
validates protobuf and manifest bytes and incrementally hashes the stream.
Each chunk, complete stream and keyed record aggregate is checked. Missing or
corrupt complete evidence refuses recovery; an incomplete stage has no
application authority. Two complete generations survive. Snapshot completion
is distinct from installation: send signed MsgSnap to stage its manifest,
stream signed chunks, then retry MsgSnap. Only its durable Ready install may
return `installed: true`. A transport sender must report failure/timeout to its
core if delivery is interrupted. The receiver can resume after eviction.

Every peer envelope uses the exact shared transport wire/signature domain.
Current committed source/destination incarnations and revocation are checked
after cryptographic awaits and again inside the actor queue. Queues reserve
voter/learner/snapshot slots (4/2/2 per member) and encoded bytes across an
identity (512/128/384 KiB). They do not coalesce votes, appends or read
barriers. There is no durable cursor in this direct internal delivery path: a
lost call is failed delivery and Raft retransmits. Durable enrollment supplies
peer authority; public outbound exchange/router assembly reuses the
authenticated generation-fenced bridge.

Each active handle reserves 6 MiB before creation; total reservations plus
observed reactor memory must fit 64 MiB. Retained core log cache is capped at 2
MiB, with lossless checkpoints at 1 MiB or 1,024 applied entries. This earlier
local checkpoint keeps the cache below the shared 4 MiB retained-log ceiling. A
leader is never evicted to meet admission. Only an explicitly quiescent,
nonleader, drained actor may close; the shared scheduler coordinates
quiescence/wake and alarm scheduling. `drainRecovery` exposes recovered
dependent effects for an internal router to deliver rather than silently
dropping them.

Run the bounded local proof with `sh relay/hosted/test/raft/actors-run.sh
/tmp/codeaf-actors`. It exercises real workerd actors and native SQLite peers,
signed messages, ReadIndex/minority refusal, both snapshot directions,
process death, full joint configuration restart and co-resident identities.
Unit tests cover durability waits, queue bounds, all snapshot namespaces,
partial/corrupt generations, revocation during an await and native wire bytes.
This is internal host proof, not the final client journeys or a deployment.

`LegacyAdmission` wraps full legacy IdentityDO requests, so freeze
waits for admitted pairing/blob awaits. `Tenant.migration` supplies a
`LegacySource`: freeze once, inventory original SQL rows through Go ABI op 12,
then stream one converted row at a time. Sampled watch renewals are stored apart
from original JSON; old watchers close and reread. A durable source gate blocks
old methods even through previously constructed Directory objects. An erased
source is refused, never enrolled as a fresh group.

`RaftMemberDO.legacyImport(config, manifest, row, finish)` stages bounded rows
and records `legacy-pending` before provisioning. Pending imports cannot
initialize/load a core after eviction. Exact retries survive interruption;
finish checks the complete digest, installs a three-voter genesis and portable
checkpoint, then removes only redundant staging rows and clears the pending
marker durably. The stored ceiling includes all grandfathered rows. One import
request per member can run at once, preventing page buffers stacking in memory.
After fresh quorum readback, `LegacySource.own` publishes `raft-owned`; `route`
then resolves only that UUID. There is no rollback to a legacy writer. The host
assembles the complete facade, receipt ingress, existing-client doors and
continuing watch renewal/scheduling. Source backup cleanup is explicit.

 Membership uses evaluation (ABI op 15), paged peer derivation (16), core
reconciliation (17), local learner catch-up (18) and provisioned-anchor manifest
validation (19). Original ABI operations remain unchanged. Native and hosted
application adapters supply the entry index and its exact configuration as
trusted host metadata. Operation 20 replays each committed configuration entry
with the pinned library changer, so batching cannot alter policy. No request may
supply those fields.

Provision `config.anchors` with the same three public protocol records and IDs
used by the source manifest/InitMembership. Only these anchors can bootstrap
peer traffic before the registry commits. Once present, every peer check derives
from the durable registry and current device revocations, including after awaits.
`enrollment(view, applied)` only publishes an identical committed view; it cannot
introduce a member. The old probe fixture's arbitrary registry is test-only.
`membership()` resumes the recorded intent, refreshes an obsolete checkpoint and
reports whether a signed settlement may be proposed. It never synthesizes that
settlement signature or declares a join successful. `joinStatus(barrier)` additionally
checks current authorization and the learner's own applied index.

The identity coordinator exposes personal learner admission, committed registry
routing and status through public authenticated doors. Voter swaps, personal
leadership and delegated learner activation are not available here yet.

Provisioned anchor keys receive restricted committed authority for membership
maintenance and request-session closure. Personal enrollment or retirement still
requires the original device admission proof, checked again by the replicated
evaluator. Anchors cannot authorize directory reads, chat mutations, pairing or
rotation. A crashed requester need not return for an already authorized admission
to finish; settlement requires the exact applied configuration and pending revision.


## Committed holds, timers and wake

ABI operations 23–26 supply scheduling, one-row activity checks, shared periods
and covered-renewal checks; step tag 15 is the internal idle ReadIndex. Copy all
ABI results before awaits. `CommittedHolds` samples authenticated watch evidence,
pages at most the Go batch limit per turn, skips publish-covered items after a
fresh authoritative read and emits exact-fence/index/hold-until receipts only
after application commit. `CommittedWatchers` supplies advisory presence without
the old socket-vouch header or lifted expiry. A failed renewal emits no receipt.

`MemberAlarms` holds the member queue across the timer decision, durable dispatch
and alarm update. Dispatch enqueues messages; it must never await their Raft
responses while holding that queue. `QuiescenceCoordinator` performs host
maintenance, internal safe read, bound marker commit, durable applied acks and
final sleep delivery. Lost final delivery leaves that member active. A wake or
new command invalidates the old generation, and recovered handles start awake.

The DO alarm handler retains at most one bounded durable transport batch, with a
monotonic batch ID. `scheduled` rereads that same batch after a lost reply;
`acknowledgeScheduled` removes only the matching ID. A stalled router backpressures
further turns with bounded retry delay. Draining this batch is transport delivery,
never append or application success. The sole namespace router must consume those
original peer bytes through the authenticated exchange bridge, propagate wake to
current voters, and supply queued assignment/outbox activity conservatively.
No host listener, provider service or alternate directory writer is added here.

## Hosted public assembly

`HostedIdentity` now runs at every authenticated public identity request. The
source object keeps blob/index/quota/socket admission and its irreversible
migration gate; all directory, authority, rotation and blob authorization goes
through three separately addressed `RaftMemberDO` actors. Imports advance
bounded pages, reuse source UUID/keys/ceiling and require an actual quorum
ReadIndex before ownership publication. After publication, only stores with
durable matching provenance can participate. A missing quorum refuses; it never
reimports a lost voter under its former ID or reopens the source writer.

ABI 30 prepares restricted effects using the source's protocol seed (never a
command field), 31 projects typed canonical rows to legacy numeric JSON, and 32
checks committed personal certificate/revocation/rotation. Step 16 requests an
explicit real campaign instead of fabricating ticks. Original v1 mutations still
use ABI 29 and the same deterministic evaluation. Empty legacy frame plans are
normalized before original-proof comparison, preserving replacement semantics.

Public source serialization spans coordination, while external blob I/O leaves
that queue and passes a new authority barrier afterward. Public watch sockets
carry presence without the legacy socket-vouch promise; v1 clients keep ordinary
beats. The hosted relay admits personal learners and serves their status, exchange
and hibernating sockets. Leadership requests remain unavailable; its committed
holds and idle maintenance follow the shared Go policy. Explicit retirement has its own retained
all-replica erasure journal and never serves as pressure cleanup.


## Public personal member routing

`POST /v2/cluster/enroll` retains the original personal certificate and signed
store token. Shared Go admission allocates from committed state; configuration
is a separate resumable stage. Status uses a fresh quorum read and Go projection.
An unsupported leadership request creates no intent. The application forwarding
upgrade is authenticated and passive until hosted leadership is available.

The router's durable generation advances before its volatile queues reopen.
Peer and application sockets use the hibernation API with a separate tag and
serialized attachment containing their signed connection proof. A cold presence
sweep preserves those connections without opening coordination or advancing the
generation. A real request reactivates the coordinator with fresh quorum reads,
fences the lost queues, and lets the client refresh the same store's grant. Watch presence and broadcasts filter these sockets.
Protocol queues use their own admission and reservations, outside public rates.

ABI 36 supplies admission plans, enrollment bytes and control status from an
immutable member view. ABI 37 checks native request/envelope signatures, cursors'
scope, registered keys and permitted inner message kinds; ABI 38 supplies the
transport limits and response encoding. Routing owns per-lane delivery cursors,
acks and chunk credits. One outstanding chunk per target bounds catch-up, and
only its signed durable applied evidence completes snapshot delivery. Public
coordination never waits for a learner; peer frames bypass its public queue.


## Sleeping accounts and advisory presence

The coordinator persists an outstanding-maintenance hint only while consensus
work is scheduled. All-voter sleep removes it and clears stale peer retention
deadlines before cancelling the maintenance alarm. Presence and explicit
retirement deadlines are then rearmed independently. An alarm on a cold sleeping
object reads socket attachments and that hint without constructing a tenant,
loading member handles, acquiring a read barrier or reopening peer queues.
Platform auto-response timestamps supply presence without data writes. Real
application input still activates coordination and takes a fresh quorum barrier;
a remembered leader never authorizes a reply. Watch hold renewals retain their
committed path and shared renewal period.

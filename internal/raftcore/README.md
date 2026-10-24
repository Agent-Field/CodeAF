# Deterministic consensus boundary

`raftcore` is an internal development seam shared by native and WASI hosts. It
has no production routing, goroutine, filesystem, timer, network, or ambient
random input. It owns RawNode and a copied in-memory view of recovered log
bytes. It imports P01's canonical command decoder, not directory/HTTP/SQLite.
Both targets must build through the root module's patched Raft replacement;
see `third_party/raft-patch/README.md` for pinned source and licenses.

## One call, explicit effects

Construct `New(Config, RecoveredState)`, then `Step(Poll{})` for initial work.
For fresh bootstrap every actor supplies the same voter set on empty storage;
this is not three singleton bootstraps. The host must authenticate the identity,
protocol, manifest, node ID and generation before constructing a member. IDs
cannot be reused. An empty bootstrap supports a not-yet-admitted learner; this
low-level mechanism does not grant enrollment or choose production topology.

Each Step returns diagnostic status and at most one new persistence batch.
Handle effects as follows:

1. Atomically persist all `Persist` entries, hard state and snapshot pointers,
   replacing conflicting suffixes. Await durability, then submit
   `Persisted{BatchID}`. Even commit-only hard state is durable. An empty batch
   is a dependency barrier and needs no physical transaction.
2. Only that completion releases `Send` messages and `Apply` work. Apply
   entries in index order. Configuration entries have already been passed to
   RawNode; the effect's complete ConfState must be persisted alongside the
   applied index, application rows, visible version, logical time and dedup
   results. A snapshot install must restore its application state before its
   suffix. Empty normal entries also advance the applied index.
3. After that application transaction is durable, submit `Applied{BatchID}`.
   The core advances RawNode only here (or immediately after persistence when
   there was no application work). The completion may return another Persist.
   Client mutation results belong to the durable application transaction, not
   successful proposal admission.
4. Serve an authority read only from a `Reads` barrier after the host's durable
   applied index reaches it. Reads use safe quorum ReadIndex, with a committed
   current-term entry, rather than a lease or remembered leader. Barriers that
   run ahead of a size-limited apply batch are retained. Cancel timed-out reads
   explicitly; there are at most 128 pending contexts per handle. A cancelled
   submitted context keeps its admission slot until its quorum round completes;
   pre-current-term reads stay in the bounded wrapper map until ready to send.

The host serializes calls. While a batch is pending, ordinary input returns
`ErrBusy` without being consumed: retain and retry it, with transport admission
bounds. Completions must match the pending batch and phase. Batch IDs belong
only to one handle lifetime; discard old-generation callbacks after recovery.
On storage failure send `StorageFailed{BatchID}` or drop the handle. No further
participation is permitted; retain the store and recover deliberately. Unknown
committed command schemas/operations stop participation rather than becoming
successful no-ops. Invariant panics are caught and stop the handle with a
constant diagnostic; no command bytes are logged.

## Host and later packet responsibilities

`PeerMessage` accepts only network Raft replication/election types. It rejects
local triggers, storage completions, forwarded proposals, peer authority reads,
and peer-initiated transfers. P07 authenticates original envelope signatures,
cluster/protocol, enrollment and revocation generations before this call. Local
`Propose` validates canonical schema/cluster/size but does not authenticate the
signature or apply policy. P09 must recheck committed authorization and produce
application results. Normal commands are bounded by the shared 64 KiB batch
budget; uncommitted payload is bounded at 1 MiB and inflight append batches at 8.
Authenticated transport and per-identity snapshot byte admission remain P07/P08/P09.

`Configure` and `Transfer` are mechanisms for authorized host intents, not
public APIs. Transfer requires a caught-up incoming voter outside joint state.
Request admission is not transfer completion: the host must observe the target
as leader with a current-term commit and a new read barrier. P12/P13 own the
registry, single membership intent, joint swaps, configuration revision,
owner/persistent-storage eligibility, fallback and policy. No topology or
leadership-reacquisition policy is implemented here.

P04 supplies actual durable storage. Recovered suffixes must be contiguous and
include uncommitted entries; ConfState is the most recent DURABLY APPLIED one.
The snapshot supplied at construction must already be fully installed in the
application (Applied >= its included index). If a crash left a snapshot pointer
without its application transaction, recovery must complete that installation
before constructing the core, or retain the preceding installed generation.
The host validates checksums and cluster/schema manifests; core structural
validation is not a substitute. A recovered core starts as a follower. Every
new handle receives explicit election seed input; it does not restore volatile
leadership, timers or RNG state. Replay records each such seed.

The current in-memory log view is not a persistent database or a complete
production memory policy. P05 must bound it through explicit snapshot/cache and
compaction effects, stream snapshot data and preserve two durable generations.
Do not use this package to load a full application identity into wasm: P01 rows
and P09 paged apply remain outside the consensus cache. Quiescence scheduling,
clock samples, retry deadlines and application logical time are explicit host
inputs in later packets. `NeedNextTick` currently means every non-stopped handle
needs ticks unless the explicit committed quiescence protocol below stops them.

## Replay contract

Election timing version 2 gives provisioned anchors A/B/C disjoint randomized
windows of 6–7/8–9/10–11 ticks; a personal voter uses 12–13. Rank is the index in
the immutable, sorted `Membership.Anchors` manifest, not current ConfState voter
order. Before registry initialization the same source-provisioned bootstrap
manifest supplies it. Native recovery supplies `RecoveredState.Anchors` from the
checksummed applied registry, including installed snapshots. Hosted create frames
carry that same durable list. Conflicting host/recovered manifests are refused.
An unprovisioned learner uses the personal window. Heartbeats remain every tick;
CheckQuorum and PreVote leader leases retain their six-tick interval. Hosts currently
schedule 5-second ticks (P14); no wall duration is read here. SplitMix64's exact
64-bit constants, overflow and rejection sampling are specified in random.go;
seed zero is valid. Every version-2 timeout reset consumes one bounded draw of width two and adds
twice the immutable rank through the existing provider. The library's separate
`ElectionRandomBound=8` admits these offsets without changing `ElectionTick`.
Version 1 remains available only as explicit internal replay compatibility for
old recorded inputs; production defaults and the ABI always use version 2.
The simulator writes its timing version and reads unversioned old recordings as
version 1, preserving their exact event digests. Every timeout reset uses this
per-node provider and
never upstream global randomness. RNG state/draws appear only in diagnostics.
Wire protobuf marshaling is deterministic. RawNode already orders meaningful
peer traversal; preserve its per-peer message order. Completed read contexts
sort before emission. P03 versions the binary ABI and checks actual wasm/native
parity; a JSON encoding of Go diagnostic structs is not that ABI.

Focused proof: `go test -p=1 ./internal/raftcore -run '^TestR' -count=1` at the
wave's nice/CPU/pressure limits. The tests cover R3/R4/R5/R6/R12/R14 mechanisms,
not the later simulator, persistent power-loss, joint-swap, cross-runtime or
real-client release proofs. The initial trace SHA-256 is a regression fixture;
SplitMix64 vectors are independently published values. Full fault checking is
P06 and the actual Worker stop/go measurement is P03.

## Applied checkpoints and bounded history

At 1,024 retained applied entries or 4 MiB of applied protobuf log, effects on an
idle completion boundary carry `SnapshotNeeded{Index, Term}`. This is a pure
maintenance hint; the host must service it before admitting further growth. It
does not wait for learners or change membership. Quotas/backpressure and durable
snapshot storage belong to the host. The core's separate uncommitted limit still
applies. See `internal/raftsnapshot/README.md` for portable format 1.

Serialize checkpoint creation with this actor. Create a durable checkpoint at the
current applied index, compact its covered durable prefix, and only then supply
`CompactSnapshot{Snapshot}`. The input refuses a pending Ready, wrong index/term,
cluster/protocol, noncanonical manifest or incomplete configuration. It copies
the manifest into MemoryStorage and compacts the in-memory prefix. A lagging
member then receives a manifest-only MsgSnap; the host streams chunks separately.
Never report successful snapshot transport until the destination has installed
its application and durable pointer. Chunk upload/completion alone is insufficient.

On incoming snapshots, install the application with the durable Raft batch before
`Persisted`; native raftstore does this atomically. This permits the core's
conservative persist-before-send protocol without sending an acknowledgment for
an uninstalled snapshot. The later Apply effect confirms that installed index
and applies any committed suffix before `Applied`. Recover the application before
constructing a core; no application rows or chunks belong in its log cache.


## Committed learner intents

P12's `PlanMembership` accepts only the durable `clusterstate.Membership` header
under the member serialization lock. `InitMembership` records exactly the three
already-applied anchors and their protocol public keys; source provisioning must
supply that SAME manifest to the bootstrap transport. Personal enrollment must
already have committed certificate authority. Metadata upserts grant none.

The header retains a single add/remove intent and configuration revision. Plan,
submit its deterministic ConfChangeV2, wait for durable application, then plan
again. A durable uncommitted configuration returns ErrBusy. Checkpoint when
requested before allowing compacted catch-up: a pre-admission snapshot cannot
restore a learner it excludes. Submit authorized SettleMembership only after the
configuration is applied. The evaluator checks the host's applied ConfState;
a request cannot supply it. `MembershipResult.Barrier` is zero on admission,
and the settlement entry index afterward. The learner must durably apply that
barrier before reporting ready. Current committed authorization must also hold.

A removal immediately retires transport authority, but its configuration change
still completes through Raft. IDs and tombstones survive snapshots and are never
reused. New IDs increase monotonically. Live learners are limited separately
from retained records; exhaustion refuses enrollment without deleting anything.
P13 owns promotion, joint swaps, transfer completion and anchor fallback. No
P12 policy command removes/promotes a voter. Configuration/provisioning APIs are
internal host seams, never unauthenticated client endpoints.

Provisioned anchor keys receive a restricted committed authority that can only
settle an existing intent or close their request session. They cannot enroll,
remove, read directory content, mutate chats, pair or rotate. Thus a crashed
requester need not return for an already authorized admission to finish. The
settlement still requires the exact applied configuration and pending revision.

P13 leadership hosts reconcile `Membership.Leadership` under their actor queue:
obtain authenticated target `LeadershipEvidence` from its durable store at the
intent barrier; call `PlanLeadership`; durably apply each explicit joint enter
and separate leave; commit `SettleLeadership`; then execute the planned transfer.
A zero-byte protobuf with `Configure=true` is the explicit leave, not no action.
Only the elected target can issue `LeadershipReceipt`, after a new current-term
ReadIndex response and durable apply. The read proof is consumed by that receipt;
starting another read invalidates any previous proof. Commit `CompleteLeadership`
through provisioned anchor authority after that observation, then obtain a new
read/receipt before returning success. A completed intent never drives another
automatic transfer after a later anchor election.

An elected anchor's `Fallback` plan authorizes only housekeeping intent admission,
not a voter mutation. Commit `RestoreAnchors` at the expected revision, obtain C's
actual durable evidence, reconcile the reverse joint enter/leave, and commit its
settlement. An interrupted joint state is finished before reversing. Concurrent
fallback requests conflict on the same revision. Returning P stays a learner;
only another personal request may promote it. Public handlers accept the typed
signed request and never expose Configure or host-supplied readiness evidence.
P14 schedules ticks/wake/housekeeping, and P17 assembles these host/control seams
with the personal member lifecycle and the one production directory route.

## Committed liveness and idle scheduling (P14)

`Schedule` takes monotonic elapsed milliseconds, conservative host activity,
its durable `clusterstate.Quiescence` row and authenticated applied voter
acknowledgments. It returns at most one tick, the next delay, wake destinations,
or a barrier/prepare plan. Run it under the member queue. The host includes
queued commands, outboxes, assignments, holds and membership/transfer work in
activity; `raftdirectory.Schedule` and `MemberActor.schedule` page the current
application's hold/intent dependencies using the same Go activity rule.

After sixty idle seconds the elected leader needs an internal `IdleRead` before
preparing a marker bound to its applied index, term and exact current voters.
`IdleRead` preserves elapsed idle time but still performs the ordinary safe
quorum read. Only a committed, durably applied anchor command creates the marker.
Every current voter must acknowledge that exact durable index/term before any
reached member can stop its timer. Unreached members remain active. Operations
cancel the generation; delayed final delivery cannot put it back to sleep.
Recovery always starts awake as a follower. An elapsed-hours sample yields one
tick, never hours of tick debt. A remembered leader never bypasses ReadIndex.

Application `RenewHolds` is restricted ingress authority, with exact device and
fence and a fresh ingress sample. Its expiry is sample + ordinary lease TTL,
never receipt-time + TTL. Released leases remain released. A recent publish can
cover a sample; the authoritative read plus shared `RenewalCovered` helper skips
that item before logging. Successful receipt indexes escape only after durable
application. Presence pongs alone do not suppress client heartbeats.

The router's timer driver must resume the existing P12/P13 committed workflow
and P10 outboxes before idle preparation. It must authenticate target evidence,
finish joint stages, settle, transfer, obtain the elected target's fresh read,
commit completion and obtain a new receipt. Returning completed intents never
regain leadership automatically. `QuiescenceCoordinator` sequences the idle
part without adding another coordination writer. P17 supplies the sole product
host and the outbound router assembly; no deployment or rollout switch exists.

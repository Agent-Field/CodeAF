# Worker reactor ABI, version 1

P03 is an internal feasibility boundary. Neither the local proof Worker nor the
stdio peer is a production directory or a new codeaf command. P08 owns the hosted
actor/queue/storage adapter; P09 owns complete paged application evaluation,
authorization, monotonic time, dedup and conditional transaction policy. No
JavaScript lease or consensus rules are added here.

Build/run the bounded local proof from the repository root:

```sh
sh relay/hosted/test/raft/run.sh /tmp/codeaf-p03
```

Install the hosted relay's pinned npm lockfile first if needed. It uses the installed
workerd binary/schema directly, with no retained Miniflare development SDK. The script waits
for I/O pressure before each heavy step, builds serially at nice 10 with two Go
processors and `-p=1`, and runs only the targeted files. It uses local workerd,
loopback inspector, a native stdio peer and disposable durable storage. The
Worker's outbound service throws. The existing local wrapper disables telemetry
and Request.cf downloads. It never deploys, signs in or reads a person's home.
The mixed-consensus and bulk-memory phases run sequentially: Node exits before
the Go streaming driver starts a fresh engine with six handles across two
identities. Both phases assert combined RSS at most 256 MiB. The wrapper limits
local allocator arenas and Node's test heap; it never forces Worker GC. Runtime
reports include sampled V8 heap/backing storage plus wasm linear-memory high
water, separately from process RSS. These local measurements are not billable
CPU or cloud duration. Artifacts and reports stay under the supplied /tmp directory; none is installed
as the person's codeaf binary.

## Buffers and lifetime

A precompiled module is imported by the Worker, with exactly the audited WASI
preview1 imports in `relay/hosted/src/raft/wasi.js`. One `Reactor` instance is
shared across all member handles in an isolate. Construction attaches exported
memory before `_initialize`. The exports are `codeaf_abi_version`,
`codeaf_alloc(n)`, `codeaf_call(n)`, `codeaf_result()` and `codeaf_free()`.
Allocation returns a wasm32 pointer; call returns result length; result returns
a pointer. Copy the result before freeing or calling again. No pointer survives
an await or memory growth. Calls are synchronous; the host serializes a member
across its asynchronous persistence and apply transactions. Input and output
buffers are bounded at 2 MiB. Input bytes are decoded completely before stepping
a core. Handles are monotonic uint32 values and are never reused within a
runtime; a new runtime discards all old completions.

The JS guard stops the whole instance if linear memory exceeds 64 MiB or a call
traps. This is a fail-closed guard, not a substitute for P08's preallocation
admission/cache/eviction policy. `GOMEMLIMIT=48MiB`, `GOGC=50` and
`GOMAXPROCS=1` are the entire runtime environment. Empty args, unsupported fd/
socket operations, runtime-only clock/entropy and discarded failure output are
the other imports. Poll/yield throw; a build that uses them cannot pass the
compatibility gate. Runtime entropy does not seed elections.

## Binary grammar

All lengths/counts, flags, tags and handles are little-endian uint32. Every
counter/node ID/index/seed is little-endian uint64 (BigInt in JS). A blob is
uint32 length followed by exact bytes; a string is a UTF-8 blob. A protobuf
field is an optional deterministic protobuf blob, with zero length for absent.
An entry list is count plus one blob per entry. Bytes cross this boundary
without JSON/base64 wrapping. Application command/row blobs retain P01's
canonical schema inside the binary envelope.

Every request starts with ABI version and operation. Every response starts
with ABI version and an error string; empty means success and the remaining
bytes are the operation result. Errors are bounded constant/structural messages;
the host must not log private request/row bytes. A Step error is not a successful
coordination response. Oversized effects require stopping/recovering, not retrying
a mutated core blindly.

| Operation | Request body | Result body |
| --- | --- | --- |
| 1 create/recover | cluster string, node/seed uint64, protocol uint32, bootstrap count/IDs, HardState/ConfState/Snapshot blobs, entries, applied uint64 | handle uint32 |
| 2 drop | handle | none |
| 3 step | handle, input tag, tag-specific data | effects |
| 4 lease feasibility probe | command blob, optional one-row blob | next canonical row blob |
| 5 row representation probe | canonical row blob | same canonical row blob |
| 6 snapshot maintenance query | handle | present flag, then index/term uint64 when present |

Step tags: 0 Poll, 1 Tick, 2 PeerMessage blob, 3 Propose blob, 4 Read string,
5 Persisted batch ID, 6 Applied batch ID, 7 Transfer target, 8 Configure blob,
9 StorageFailed batch ID, 10 CancelRead string, 11 CompactSnapshot protobuf blob. The core enforces the one-Ready
completion order, leader-only admission and peer-local-control refusal.

Effects are: optional persist flag and [ID, hard state, entries, snapshot];
send count and [destination, message blob] tuples; optional apply flag and
[ID, index, entries, snapshot, ConfState]; read count and [request string,
index] tuples; status [node, leader, term, commit, applied uint64, role uint32,
RNG state/draws uint64, stopped flag]; need-tick flag. Preserve send order.

The build manifest binds the ABI version, core protocol, row schema, source
inventory hash and wasm SHA-256. It also pins portable snapshot schema 1 and the
SHA-256 of `internal/raftsnapshot/format.go`. Operation 6 exposes the core's
SnapshotNeeded maintenance hint without changing ABI-1 effect framing; tag 11
completes a host's durable local checkpoint/compaction. Actual hosted chunk
storage and bidirectional native/wasm installation are P08's gate; the P03 opaque
checkpoint fixture alone does not establish that interoperability.

## Oversized legacy representation decision

Keep each existing canonical row in host storage and pass only one immutable
row (at most the existing 1 MiB legacy envelope) to Go. Inventory/import and
snapshot creation enumerate sorted rows at a fixed applied index, with no
whole-identity listing or JSON string in wasm. Persist the imported byte ceiling
`max(8 MiB, imported application bytes)` outside the reactor alongside the
applied metadata. Preserve every row, sealed title/key and frame plan. Do not
truncate to make import fit. P05 supplies chunk/checksum/install generations;
P09/P11 supply complete read-sets, atomic row/byte accounting and growth policy.

The proof uses 129 separately persisted rows totaling over 8 MiB, including one
near-limit row and 128 smaller pages. A small native driver compares every row
with wasm and verifies every stored byte on reread, then evaluates a real
release over the large row. Six production handles remain resident while the
engine is sampled through its loopback inspector. This establishes representation/runtime feasibility; it is
not a claim that migration, snapshots, complete policy or production admission
have already shipped. A legacy row over 1 MiB remains a migration refusal until
an owner-approved streaming row extension exists, as the design requires.


## P03 stop/go measurement

The final local proof on Go 1.26.5 and pinned workerd 1.20260926.1 passed. The
full existing relay plus reactor/shim/storage fixture/manifest is 2,248,258 bytes
compressed (3 MiB gate); wasm is 9,285,637 bytes raw. All 26 distinct imports are
covered by the closed shim. The mixed phase compared 743 effect responses
byte-for-byte, including RNG state/draws, committed a real create, passed a
quorum ReadIndex, refused a partitioned read, fell back to native peers and recovered
from an actual workerd process kill with disk-backed DO state.

The final mixed phase took 5.85 s, observed 230,334,464 bytes combined RSS and
12,949,979 bytes of isolate components, and cold first call was 32.33 ms.
The sequential six-handle/two-identity memory phase took 3.75 s, observed
207,298,560 bytes combined RSS and 39,200,796 bytes of isolate components,
with 17,301,504 bytes wasm linear-memory high water. Its cold call was 28.47 ms;
workerd startup was 145.90 ms. The 129 rows contain 9,205,245 bytes; largest row
is 786,665 bytes. These are observed local samples, not production allocation
admission guarantees or cloud percentiles.

This is a **go** for the measured shared-Go Worker boundary; no Go-host topology
alternative needs an owner decision at P03. P04/P05 still supply real member
stores and snapshot/compaction, P08 supplies production actors/admission and
serialization, and P09/P11 supply complete policy and migration. P08/P18 must
repeat the size/memory gate with those later components. Application rows are
paged; no production route or feature toggle was added by this proof.

P08 adds stateless host codecs without changing the existing operations: op 7
inspects Entry/HardState/Snapshot/peer Message (kind 1/2/3/4); op 8 hashes a
bounded block with an explicit serialized SHA-256 state and returns the next
state/digest; op 9 validates a canonical portable manifest and constructs its
protobuf snapshot pointer. Step tag 12 carries peer ID plus snapshot/success
flags for transport completion. These helpers provide framing and integrity,
never authorization or application policy. The same codecs run in native proof
peers and the bundled reactor.

P11 adds op 12 (canonical LegacyConfig, kind, ID, original v1 JSON and sampled
hold-until; returns canonical RecordWrite), op 13 (validated LegacyManifest;
returns the shared genesis Entry/HardState/ConfState protobuf bytes), and op 14
(validated public certificates/effects authority rows). These calls convert
bounded bytes and establish no request authentication or successful coordination
receipt. Source/target hosts own the irrevocable migration gate and durability.

P13 adds op 21 (node handle, canonical Membership header, canonical target
LeadershipEvidence; returns a canonical LeadershipPlan) and op 22 (node handle,
canonical Membership header; returns a consumed current-term LeadershipReceipt).
These are internal host calls, never public configuration or readiness inputs.
Plans distinguish Configure from empty bytes because the explicit leave-joint
protobuf has zero bytes. Operation 15 accepts an optional trailing entry term;
new hosts supply the actual committed entry term when evaluating leadership
intents, and old directory-only frames retain their prior representation.

The expanded mixed-runtime proof sets its Node coordinator old-generation limit to 16 MiB so
its retained request/report garbage does not consume the native/workerd share
of the unchanged 256 MiB combined RSS gate. The same six hosted handles, native
process death, both personal-leader roles and all snapshot/configuration steps
remain in the workload; production isolate/core ceilings are unchanged.

## Ranked election timing (R7/R14)

ABI 1 create optionally appends an anchor count (exactly three) and three uint64
IDs after the applied index. Old mechanism frames may omit it. Hosts obtain the
ordered IDs from the durably applied membership registry, or the same provisioned
bootstrap manifest before initialization. They must never use the current voter
list, which changes when C becomes a standby. The core uses election timing
version 2 in native and WASI. Rank survives snapshots and restarts as registry
state; absent provisioning identifies a personal learner, never an invented rank.

P15 adds operation 27: cluster/worker/cell strings, incarnation/committed-time
uint64s, then five blobs in WorkerReadKeys order (empty means absent). It returns
one canonical WorkerRead blob. The host must pass its fresh current-term quorum
barrier before returning these bytes; the ABI supplies policy, not consensus.
Operation 16 accepts an optional tail: committed time uint64, then identity,
authority and worker-record blobs. It is required for delegated peers; the old
metadata-only form returns them revoked. Personal/anchor encodings are unchanged.

P16 adds operation 28: cluster/worker strings, incarnation/committed-time uint64s,
canonical AssignmentRef blob, then six blobs in AssignmentReadKeys order. It
returns one canonical AssignmentRead blob. Like operation 27, it runs under the
member queue after current ingress time commits and a fresh quorum barrier
passes; the ABI never supplies a local success path. Assignments, per-cell active
slots and incarnation-bound holds use the existing state namespace and survive
ordinary native/hosted snapshots. No storage format or old ABI tag changes.

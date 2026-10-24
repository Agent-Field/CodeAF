# Hosted consensus actors

`RaftMemberDO` is exported by the existing Worker. The build entry
`src/raft-worker.js` statically imports the generated `core.wasm` and installs
one shared reactor per isolate. Wrangler's local build command compiles that
artifact from the pinned modified Go Raft source. The bindings add independent
SQL objects; they do not route the legacy identity directory to a new writer.
P09 owns the application adapter and P11 the irrevocable migration gate.

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

Application writes and index/configuration/version/time commit atomically.
The evaluator is synchronous with an immutable applied view and bounded 1 MiB
read/write sets. It must implement committed policy, declared dependencies and
request/session semantics; storage supplies atomicity, not that policy. Rows
cover state, sessions, membership, enrollment, tombstones and outboxes. The
persisted application ceiling defaults to 8 MiB, including framed keys/values;
an explicit larger imported ceiling is preserved. Pressure refuses growth,
never deletes application rows. P11 owns actual inventory/grandfathering.

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

Every peer envelope is the exact P07 wire/signature domain. Current committed
source/destination incarnations and revocation are checked after cryptographic
awaits and again inside the actor queue. Queues reserve voter/learner/snapshot
slots (4/2/2 per member) and encoded bytes across an identity (512/128/384 KiB).
They do not coalesce votes, appends or read barriers. There is no durable cursor
in this direct internal delivery path: a lost call is failed delivery and Raft
retransmits. P12 supplies durable enrollment; public outbound exchange/router
assembly reuses P07's authenticated generation-fenced bridge.

Each active handle reserves 6 MiB before creation; total reservations plus
observed reactor memory must fit 64 MiB. Retained core log cache is capped at
2 MiB, with lossless checkpoints at 1 MiB or 1,024 applied entries. This earlier
local checkpoint keeps the cache below the shared 4 MiB retained-log ceiling.
A leader is never evicted to meet admission. Only an explicitly quiescent,
nonleader, drained actor may close; P14 must coordinate actual quiescence/wake
and alarm scheduling. `drainRecovery` exposes recovered dependent effects for
an internal router to deliver rather than silently dropping them.

Run the bounded local proof with `sh relay/hosted/test/raft/actors-run.sh
/tmp/codeaf-p08`. It exercises real workerd actors and native SQLite peers,
signed messages, ReadIndex/minority refusal, both snapshot directions,
process death, full joint configuration restart and co-resident identities.
Unit tests cover durability waits, queue bounds, all snapshot namespaces,
partial/corrupt generations, revocation during an await and native wire bytes.
This is internal host proof, not the final client journeys or a deployment.

# Committed directory application

`clusterstate.Evaluate` is the single directory application evaluator for native and
WASI members. It takes a signed canonical command, immutable application metadata
and the complete declared dependency read-set. Missing and unread records differ.
The evaluator verifies the original signature against committed personal enrollment,
checks revocation at that log position, then applies the directory rules. Metadata
upserts cannot create signing authority. Delegated worker policy remains P15.

Every accepted session keeps its exact last result until an acknowledged sequence
advance or explicit close; closed IDs remain tombstoned. There are 128 live sessions
and 4,096 retained session IDs. Capacity refuses growth and deletes nothing. A
read-set/corrupt-store failure stops application; it is not a successful refusal.
Hosts must reject oversized ingress before proposing and reserve capacity for
coordination responses. If even a capacity response cannot fit, evaluation fails
closed rather than acknowledge an unrecorded result. This is backpressure, never
permission to retry a tool or erase application records.

Native `Apply` evaluates committed entries over an overlay in order and commits
rows, responses, version, monotonic time, applied index and complete configuration
in one FULL transaction. Serialize the actor across dependency reads and that
transaction. `ApplicationView` and `ScanPage` are local storage operations. Before
serving Listing/Cell/authority/rotation, commit current ingress time if needed,
pass a fresh current-term quorum ReadIndex, await durable application, and check
`AuthorizedRead` at that view. No local status or successful proposal admission is
an authority response. The integration harness proves this through `directory.Raft`
and its real HTTP handler; the backend interface is an assembly boundary.

The hosted member chooses `directoryEvaluator`, which calls Go through additive
ABI-1 operations 11 (dependencies) and 10 (evaluation). JS copies immutable bytes,
persists the returned writes and advances the core after durable completion. It
contains no lease, CAS, revocation, clock or sequence decisions. `RaftDirectoryFacade`
forwards original signed bytes to a host's commit and barrierRead operations. The
old hosted directory is still solely the pre-migration path; P11 must irrevocably
freeze it before routing a namespace to this facade. No production cutover or
feature toggle is introduced here.

The Client facade exposes all existing directory verbs. Its v1 acquire normalization
never manufactures an observed tuple: a missing expectation disables force, and a
required conditional take without an observation refuses. v1 fences cannot exceed
the exact JSON-integer limit; direct v2 commands retain full uint64 fences and refuse
overflow. Pure rules retain their legacy behavior for pre-cutover stores.

Pairing commands require an existing committed request dependency, atomically write
the device metadata and sealed decision, and retain the first decision. The native
and hosted tests provision genesis/request dependencies explicitly. P10 still owns
external request/certificate admission, authorization installation, outbox delivery,
blob receipts and upload-before-head. P11 owns actual genesis/import; P12 owns peer
registry; P14 owns watch presence and committed renewal scheduling. No socket can
extend a lease through this evaluator. P17 supplies original v1 authentication proof
and production client wiring. These boundaries are internal, not new manual claims.

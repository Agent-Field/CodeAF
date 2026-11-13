# Committed directory application

`clusterstate.Evaluate` is the single directory application evaluator for native
and WASI members. It takes a signed canonical command, immutable application
metadata and the complete declared dependency read-set. Missing and unread
records differ. The evaluator verifies the original signature against committed
personal enrollment, checks revocation at that log position, then applies the
directory rules. Metadata upserts cannot create signing authority. Delegated
worker policy uses a separate role and scope.

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
ABI-1 operations 11 (dependencies) and 10 (evaluation). JS copies immutable
bytes, persists the returned writes and advances the core after durable
completion. It contains no lease, CAS, revocation, clock or sequence decisions.
`RaftDirectoryFacade` forwards original signed bytes to a host's commit and
barrierRead operations. The old hosted directory is solely the pre-migration
path; migration must irrevocably freeze it before routing a namespace to this
facade. The facade itself neither performs cutover nor selects a feature toggle.

The Client facade exposes all existing directory verbs. Its v1 acquire normalization
never manufactures an observed tuple: a missing expectation disables force, and a
required conditional take without an observation refuses. v1 fences cannot exceed
the exact JSON-integer limit; direct v2 commands retain full uint64 fences and refuse
overflow. Pure rules retain their legacy behavior for pre-cutover stores.

Pairing commands require an existing committed request dependency, atomically
write the device metadata and sealed decision, and retain the first decision.
The native and hosted tests provision genesis/request dependencies explicitly.
The effects adapter below supplies external request/certificate admission,
authorization installation, outbox delivery, blob receipts and v2
upload-before-head. Separate host adapters supply genesis/import, peer registry,
watch presence and committed renewal scheduling. No socket can extend a lease
through this evaluator. The v1 adapter below retains original HTTP
authentication proof; production member/client routing requires the complete
namespace assembly.

`Effects` and the equivalent hosted `RaftEffectsFacade` bridge external
delivery. Provision a separate `Authority{Effects:true}` protocol key; it must
not be personal. It can only record upload evidence, reserve a join dependency,
or complete a delivery. These commands are idempotent by immutable effect keys
and do not consume client sessions. All effect rows count against the identity's
content ceiling; capacity refuses further growth without removing receipts,
decisions or blobs.

Before issuing a receipt, call `Effects.Upload` with the local head ID, its
**different encrypted remote object ID**, and the existing incremental frame
hint. Disk confirmation checks retrieval and flushes directory ancestry; hosted
confirmation awaits R2, its complete index and the supplied storage durability
boundary. A client still uploads its entire encrypted closure before requesting
this receipt: a relay cannot decrypt or traverse it, and an empty frame hint
never proves an empty closure. The receipt binds actor, both head IDs and the
exact frame hint. V2 Create/Publish require the committed receipt, and still
report stale fences/heads before missing upload evidence. The existing Client
facade explicitly identifies v1 Create/Publish, as it already identifies v1
Acquire. Old clients cannot supply the encrypted head RID; they retain the
existing upload-before-publish client ordering and their existing safe API
semantics. This is protocol compatibility in the same evaluator, never a second
writer. V1Directory selects this marker only from the original authenticated v1
ingress; ClientCommand rejects compatibility markers at a public v2 boundary. A
v2 body cannot become v1 merely by requesting it. Namespace migration and host
assembly must install the adapter before it serves production requests. No
internal facade activates that route by itself. Never derive an encrypted remote
object ID from a local turn ID. Receipts admit at most 512 declared frame hints;
a frame plan remains optional and partial.

Admit calls `LinkEffects.Bind` before logging the request. A reservation excludes
other identities and legacy decision paths. Failed admission retains it until
the ordinary public request expires. Approval verifies the original existing
certificate under `IdentityRec.SigningKey`, checks that its device key matches
the bound request, and installs personal authorization, device metadata and the
sealed decision atomically. Genesis/import must populate that **public** root key
from authenticated input. An upsert never installs authorization. Peer/node
membership and delegated workers use separate committed policies.

Replay scans at most 64 retained outbox rows per call, from a quorum-authorized
view. Deliver reads the decision through another fresh barrier, sends its hash
as the destination's idempotency key, then commits `CompleteJoin`. A crash at
any boundary repeats only that same decision. A public request that expired
records `gone`; a lost destination reply retries without changing its grant.
Restarting a scan at the beginning is safe. The host must schedule these bounded
pages on activation/wake; no periodic network loop starts here. Delivery of a
grant approved before revocation cannot restore authority after it: every
subsequent operation still checks the committed device record. Already delivered
sealed bytes cannot be recalled, as with existing personal pairing.

Assign/Reassign/ClaimAssignment/CancelAssignment/FinishAssignment run in
the shared Go evaluator. AssignmentRef declares every routing dependency before
application. Only an explicit current scoped worker/incarnation with recent
committed availability can claim; claim and ordinary fenced acquire commit in
one application transaction. Per-cell slots exclude overlapping active runs.
Finishing references an already published ordinary head and never writes a
parent. Assign/Reassign bind the existing child's ParentCell and refuse aliases.
Opaque payloads share the existing 4 KiB worker sealed-data limit. Admission
refuses growth at the persisted identity ceiling without deleting runs.

AuthorizedWorkerAssignment and hosted workerAssignment have the SAME
actor-queue/current-committed-time/fresh-quorum prerequisite as the selected chat
read. Recheck current target authorization at that view before execution; a saved
claim response alone cannot reopen a displaced or superseded run. A revoked
pending run is permanently unclaimable and its raw history remains retained.
An owner can explicitly cancel it to reuse the per-cell slot.

syncsetup.WorkerAdapter accepts scoped content dependencies and a selected
quorum-backed directory. It uses handoff.Taker for
stage/claim/reconcile/install, saves original signed claim/finish requests and
durably records execution intent before its callback. Completed attempts return
locally; a recorded finish retries only coordination. Interrupted attempts and
reassigned revisions return incomplete without invoking tools. Execute must use
the ordinary call recorder and cellsync.Publisher; it returns only an already
published head. The adapter never loads a personal identity, discovers a relay,
provisions a box or adds a public command. Public worker setup and assignment
execution commands are not available yet.

The native `V1Directory` implements `directory.RequestDirectory`. The HTTP
handler first authenticates, then binds the exact request to its client. The
relay has no personal signing key. `clusterstate.V1Command` translates the closed
v1 method/URI/body shapes; apply verifies the original certificate and HTTP
signature against the committed identity and rechecks revocation/rotation there.
Unsigned host time cannot freshen a request after committed time has advanced.
V1 uses its existing reconciliation behavior and does not consume v2 sessions.
ABI 29 and `src/raft/v1.js` expose that SAME translation to the WASI host.

`V1Host` must hold the actor queue across committed application and read
callbacks. GET first commits its ingress time; every read passes a new
current-term ReadIndex and durable applied index. A POST followed by its
rotation view passes another barrier without repeating the mutation. The
adapter checks current root/device authority inside that callback. Missing
request proof is refused; no in-process `For` call can bypass authentication.

The host supplies watches, personal-member lifecycle, worker/v2 HTTP ingress
and the wake/outbox coordinator around this translation adapter. Individual
commands remain bounded at 64 KiB. Legal original v1 bodies up to the 1 MiB HTTP
limit use bounded staged body pages; only the final application changes directory
rows, after rechecking the full original proof and current authority. Interrupted
staging can resume without exposing partial metadata. Both production relays use
this representation and the same consensus path. Capacity refuses retained content growth
without removing records or increasing the quorum.

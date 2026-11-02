# Pure coordination boundary

`clusterstate` is shared by native members and the WASI reactor. It has
no directory, HTTP, database, identity-key, wall-clock or random-source imports.
`directory` keeps aliases to its records/errors and forwards its existing rules.
The old v1 JSON representation and pure rule behavior remain unchanged.

Schema 1 covers commands, results and individual application rows.
An older member must reject unknown versions and variants and stop applying;
it cannot turn an unknown committed entry into a successful empty operation.

`EncodeCommand`/`DecodeCommand`, `EncodeResult`/`DecodeResult`, and
`EncodeRow`/`DecodeRow` validate their schema and closed unions. The underlying
`Encode`/`Decode` handle canonical typed values; they do not authorize an actor,
verify a signature, validate upload receipts, implement deduplication, or apply a
mutation. The host still authenticates the original signed request, and apply
must check committed enrollment, revocation, rotation, dependencies and scope.
`RequestDigest` and `Signature` are original authentication/session inputs;
ingress and apply supply their cryptographic and deduplication validation.

The v2 canonical encoding has these rules:

- Every Go `uint64`, including zero, becomes an unsigned decimal string without
  leading zeros. Values through `18446744073709551615` round trip exactly.
- Other integers remain JSON integers within ±(2^53−1). Millisecond times are
  integers, never floats. V1 fence admission must retain its 2^53−1 ceiling;
  the directory adapter must reject overflow before calling the legacy rule.
- Object keys sort by UTF-8 bytes, with no insignificant whitespace. Strings
  use Go JSON escaping, including HTML and U+2028/U+2029. Invalid UTF-8 is refused.
  Ciphertext strings and wrapped-key map values remain opaque strings even if
  a map key happens to say `fence` or `size`.
- Byte slices use padded standard base64. Nil and empty values retain the
  record's JSON semantics; fields marked `omitempty` are omitted when empty.
- Decode accepts exactly canonical bytes. Unknown, duplicate, missing required,
  noncanonical and trailing fields/data are refused without changing the target.
  API ingress must normalize external requests before signing/submitting them.

The hosted codec is a serialization boundary only. It preserves decimal strings
and checks canonical syntax; typed schema validation remains in Go. The shared
`testdata/canonical.json` goldens are independently specified wire values, also
read by the targeted hosted codec test. The existing relay rule vectors are
consumed unchanged by the pure Go test and the hosted vector test.

`Row` holds one record, so a host can supply bounded read-sets and stream sorted
rows at a fixed applied index. It is not a whole-identity snapshot format. Hosts
must atomically persist applied index, visible version, logical time, dedup and
membership metadata alongside rows. They must not use `Listing` JSON as a state
export: its visible version deliberately lives outside the v1 body.

`raftsim/model` is an independent sequential ownership/vault model. It imports
no production types or apply functions. The simulator uses Porcupine, unknown
timeout outcomes and replay/history machinery; a separate assignment model
extends the independent domain. Encoding tests alone do not prove consensus.

Worker delegation is a fourth exclusive Authority role. Personal pairing
Cert remains unchanged; WorkerCertificate uses its own signed domain and binds
the root public identity, cluster UUID, worker key, sorted selected cell IDs,
exclusive expiry and explicit learner consent. EnrollWorker commits only sealed
per-cell grants; it cannot replace personal authority or clear revocation.
WorkerRegistry retains at most 4,096 delegations independently of learner quota;
existing rows, grants and node tombstones are never deleted on pressure/expiry.

RegisterWorker requires a monotonic incarnation CAS. WorkerIncarnation is signed
and included in the request digest on every worker command. Authorization checks
it before dedup, with the narrow predecessor exception for an exact registration
retry. DrainWorker is sticky across registration and blocks WorkerCanClaim;
held work can save/release. WorkerBeatBatch retains original per-worker signed,
sequenced, fresh evidence, returns per-item codes and updates availability only.
Idle workers need no recurring beat. Availability and work hints do not acquire
or renew a chat. RenewHolds and upload receipts bind worker scopes/incarnations.

A delegated worker may beat/publish/release an already held selected chat, close
its request session, register, heartbeat or drain itself. It cannot use ordinary
Acquire, create/archive, pairing, vault, rotation or membership/leadership doors.
Conditional assignment claims require their own allowlist and declared
dependencies. They bind an assignment's claimant incarnation before granting
execution, even when a chat fence survives a worker restart.

ReadWorkerCell returns one selected chat and its sealed grant after the host
commits current ingress time and passes a fresh quorum ReadIndex barrier. The
native AuthorizedWorkerRead and hosted workerCell adapters have the SAME host
prerequisite, not a follower/local read path. Identity-wide AuthorizedRead still
refuses workers. Public worker setup and provisioning are not available yet.

Scoped API workers never receive the identity log. Explicit learner consent
permits its routing metadata, never content keys outside selected sealed grants.
MemberRecord binds worker expiry/incarnation; CommittedViewAt/MembershipViewAt
and hosted ABI-16's optional tail derive current revocation. Hosts MUST use the
time-aware view for worker learners; the older view fails closed for them. A new
worker incarnation retires the old peer view; removal/re-enrollment uses a fresh
node ID. Worker voters and promotion are refused. Rotation/removal cannot erase
bytes already downloaded by a worker.


## Large requests from older clients

The legacy HTTP body limit remains 1 MiB. A translated request fitting the
64 KiB command envelope keeps its ordinary representation. Larger requests
retain their original certificate, URI, timestamp, signature and body hash in
32 KiB body-page proposals. All pages use one replaceable staging header and
32 fixed page keys per identity. Abandonment cannot allocate more keys or more
than 1 MiB of body scratch. Replacing scratch never removes directory records.

Each committed page rechecks the original signed hash against the current root,
revocation and committed time. Pages change no directory row, visible version or
logical time. The final proposal requires every ordered page and the complete
body hash, decodes the closed legacy request again, and rechecks the original
proof and ordinary mutation rules. Its directory writes and scratch removal
commit in the same application transaction. A crash before this transaction
leaves the previous directory; recovery after it retains the complete change.

Large logical records and retained results use a tagged deterministic gzip
representation with compact HTML escaping when smaller, preserving all fields
inside the existing physical row envelope. Ordinary canonical wire bytes retain
their established HTML escaping. Canonical command bytes never accept this representation. The
shared decoder checks the complete compressed stream and bounds expansion by
the legacy JSON escaping envelope. Snapshots retain the physical bytes and
checksums; reads and public projections decode the original typed record.

The reactor pages immutable records and result bytes synchronously within the
member's application transaction. Each record page respects the 1 MiB evaluation
envelope and each result byte page is at most one quarter of it. Ordinary
commands retain their complete read-set cap. Staged final evaluation streams
its body and dependency pages without putting them in one ABI frame. The host
never yields or changes the applied view between these pages. The 64 KiB command,
2 MiB ABI buffer, 1 MiB physical row and content capacity caps are unchanged.
Imported source rows include the old relay's bounded added fields; rows above
the physical envelope use the same lossless compressed representation. Impossible
source rows still refuse migration without changing their originals.

Temporary request pages occupy one fixed slot outside retained application
capacity. Their extent is derived from the original HTTP body and command limits.
Snapshots preserve these pages and account for retained records separately;
abandoned requests cannot evict records or consume more slots.

Intermediate JSON and compressed physical records leave opaque HTML characters
unexpanded. Hashes and public canonical bytes keep their established escaping.
Comparison and compression consume final bytes directly, avoiding simultaneous
expanded copies of a legal request inside the bounded reactor.

Large retained cell and result records decode directly into their typed shape,
checking decimal counters and the complete canonical spelling before exposing
the value. A single-cell update reuses its validated cell and typed write for
visible-version comparison, preserving opaque metadata without repeatedly
decoding the complete frame plan.

## Capacity and retry bookkeeping

The content ceiling remains 8 MiB by default or the larger measured import.
Long retry keys hash both actor and session, so caller-selected names cannot
enlarge retained headers. Ordinary short retry keys keep their existing spelling. At most 512 bytes per retained session and its count row use
`CoordinationReserveBytes`; response bytes beyond that count against content.
One existing evaluation envelope also covers membership initialization. The live
and retained session limits and membership admission limits remain unchanged.
A capacity refusal saves its exact result and permits the next entry to apply.
The persisted accounting travels with snapshots; hosted storage obtains both its
charge and maximum from the same Go function used by native application.

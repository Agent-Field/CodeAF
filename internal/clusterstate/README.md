# Pure coordination boundary

`clusterstate` is shared by native members and the future WASI reactor. It has
no directory, HTTP, database, identity-key, wall-clock or random-source imports.
`directory` keeps aliases to its records/errors and forwards its existing rules.
The old v1 JSON representation and pure rule behavior remain unchanged.

P01 defines schema 1 for commands, results and individual application rows. This
is internal development scaffolding, not a production routing change. Later
packets add their typed operation/row variants before the single feature ships.
An older member must reject unknown versions and variants and stop applying;
it cannot turn an unknown committed entry into a successful empty operation.

`EncodeCommand`/`DecodeCommand`, `EncodeResult`/`DecodeResult`, and
`EncodeRow`/`DecodeRow` validate their schema and closed unions. The underlying
`Encode`/`Decode` handle canonical typed values; they do not authorize an actor,
verify a signature, validate upload receipts, implement deduplication, or apply a
mutation. The host still authenticates the original signed request, and apply
must check committed enrollment, revocation, rotation, dependencies and scope.
`RequestDigest` and `Signature` are original authentication/session inputs; P07
and P09 supply their cryptographic and deduplication validation.

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
rows at a fixed applied index. It is not a whole-identity snapshot format. P04/P05
must atomically persist applied index, visible version, logical time, dedup and
membership metadata alongside rows. They must not use `Listing` JSON as a state
export: its visible version deliberately lives outside the v1 body.

`raftsim/model` is an independent sequential ownership/vault skeleton. It imports
no production types or apply functions. P06 adds the Porcupine adapter, unknown
timeout outcomes and replay/history machinery; later assignment packets extend
the independent domain. P01 does not claim simulator or consensus proof.

Worker delegation (P15) is a fourth exclusive Authority role. Personal pairing
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
P16 supplies the conditional assignment claim and must extend the allowlist and
assignment dependencies there; it must bind an assignment's claimant incarnation
before granting execution, even when a chat fence survives a worker restart.

ReadWorkerCell returns one selected chat and its sealed grant after the host
commits current ingress time and passes a fresh quorum ReadIndex barrier. The
native AuthorizedWorkerRead and hosted workerCell adapters have the SAME host
prerequisite, not a follower/local read path. Identity-wide AuthorizedRead still
refuses workers. The public setup/client/provisioning lifecycle remains P17.

Scoped API workers never receive the identity log. Explicit learner consent
permits its routing metadata, never content keys outside selected sealed grants.
MemberRecord binds worker expiry/incarnation; CommittedViewAt/MembershipViewAt
and hosted ABI-16's optional tail derive current revocation. Hosts MUST use the
time-aware view for worker learners; the older view fails closed for them. A new
worker incarnation retires the old peer view; removal/re-enrollment uses a fresh
node ID. Worker voters and promotion are refused. Rotation/removal cannot erase
bytes already downloaded by a worker.

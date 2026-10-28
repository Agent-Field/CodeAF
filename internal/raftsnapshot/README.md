# Portable coordination snapshot, format 1

This pure package is shared by native Go and the WASI core. SQLite and DO hosts
own chunk staging, durable generation publication and installation. The format
contains coordination only, with no plaintext chat content or private keys. It
copies every opaque application row without interpreting its policy. The
application supplies complete authorization/evaluation; host adapters supply
storage.

`raftpb.Snapshot.Data` is a canonical manifest, at most 16 KiB. Its metadata index,
term and complete ConfState must match that manifest. It never embeds chunks or
a whole identity. Its generation ID is lowercase SHA-256 hex of those exact
manifest bytes. The manifest is compact UTF-8 JSON, fields in the following order,
with no extra fields, whitespace, duplicate keys or alternate encodings:

| Field | Encoding and meaning |
| --- | --- |
| format | decimal string `"1"` |
| cluster | cluster UUID string |
| protocol | decimal string of the core protocol |
| index, term | nonzero uint64 decimal strings |
| conf | standard padded base64 of deterministic raftpb.ConfState protobuf |
| version | uint64 decimal string of the visible application version |
| now | int64 decimal string of committed logical milliseconds |
| records | uint64 decimal string, complete application record count |
| records_hash | lowercase SHA-256-sized hex, XOR of keyed application row hashes |
| bytes, chunks | uint64 decimal strings, total stream length and ceiling(bytes / 262144) |
| digest | lowercase SHA-256 hex of the concatenated stream bytes |

Unsigned strings have no leading zero except `"0"`. Signed time uses ordinary
base-ten with no plus or negative zero. JSON string escapes follow Go's compact
encoding/json spelling (including HTML escapes), and the ConfState wire bytes
use deterministic protobuf. Decode re-encodes and compares exact bytes. This
makes noncanonical or unknown schemas fail closed. Every ConfState field,
including outgoing voters, learners-next and AutoLeave presence, is preserved.
The Worker build manifest pins format version and the format source hash beside
ABI/core/source/wasm hashes. The native format tests supply independent framing
and greater-than-2^53 integer vectors.

The stream contains rows sorted ascending by namespace bytes, then key bytes.
The six namespaces are state, sessions, membership, enrollment, tombstones and
outbox. Every record is three unsigned big-endian uint32 lengths (namespace,
key, value), followed by those exact byte strings. Namespace is ASCII. Key is
nonempty and at most 1 MiB; value is opaque, may be empty, and is at most 1 MiB.
No record may be filtered. Framing may cross any chunk boundary, including within
a length field or a near-limit row. A reader retains at most one row and one
chunk. Duplicate/out-of-order records, bad lengths, incomplete headers/values,
wrong count, wrong keyed-row aggregate and extra trailing bytes are refused.

Chunks are numbered from zero. Every chunk except the last is exactly 256 KiB;
the last has bytes - number * 262144 bytes. Empty application state has zero
chunks and the SHA-256 digest of an empty stream. Each chunk has its own SHA-256
for transport; native storage additionally binds its checksum to generation and
chunk number. The final manifest digest detects reordered or substituted chunks.
The native keyed checksum is SHA-256 of a sequence of byte parts, each prefixed
by its eight-byte unsigned big-endian length. An application row's parts are
`records`, namespace, key, value. The count plus XOR of those hashes is the
native application's missing-row detector; the ordered stream digest separately
binds the exact complete portable representation.

Hosts inventory and admit a transfer against their identity's content ceiling
and bounded bookkeeping allowances before staging; the format does not silently impose an 8 MiB ceiling on
grandfathered legacy data. Only one incomplete generation may stage. Verify a
new complete generation before retiring redundant consensus bytes. Keep two
complete generations, including the installed one while another awaits install.
A completed upload is not a successful Raft snapshot delivery. Atomically install
application rows, application metadata, complete configuration, included index /
term and durable Raft pointer before releasing the dependent acknowledgment.
An interrupted installation keeps its prior complete generation. Never erase
corrupt authoritative evidence to manufacture a fresh member.

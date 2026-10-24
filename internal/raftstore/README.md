# Native member durability

This is P04's internal host adapter, with no production directory routing or new
person-facing door. Each member has its own SQLite file, permanently bound to
`Identity{ClusterUUID, NodeID, Protocol}` and `FormatVersion`. The same identity's
three anchors must use three distinct files. The caller creates the parent
directory; `Open` creates only an absent file with mode 0600 and syncs the parent
directory after initialization. Existing empty, corrupt, mismatched or incomplete
files are refused and retained. They are never repaired into new empty members.

SQLite uses WAL, `synchronous=FULL`, immediate transactions and one connection.
The connection pragmas are checked at startup and applied to any replacement
connection by the driver DSN. There is no weaker durability option. A transaction
error stops the handle; close it, retain the file and recover before participating
again. A validation refusal (`ErrBatch`, `ErrConflict`, `ErrSnapshot`) rolls back
without poisoning otherwise valid storage. A COMMIT error is an unknown outcome,
never permission to acknowledge a client or send a Raft append response.

## Driving the core

1. `Open(path, identity)`, then `Recover(ctx)`. Supply its `Core` to
   `raftcore.New` with explicit seed/protocol/member inputs. Supply a shared
   bootstrap voter set only when `Recovery.Fresh` is true. Persisted but unapplied
   bootstrap entries are recovered without bootstrapping a second time.
2. On `Effects.Persist`, call `Persist(ctx, *batch)`. Only after success submit
   `raftcore.Persisted{BatchID: batch.ID}`. This flushes the entire Ready, including
   commit-only hard state, before the core releases any messages. An empty Ready
   is a no-write barrier. Batch IDs belong to one core lifetime and are not durable
   sequence numbers; discard old-handle completions after restart.
3. Evaluate `Effects.Apply` against immutable rows returned by `Read` at the
   current applied index. Pass its complete ConfState, entries and index unchanged
   to `Apply`, with a host-evaluated `Update`. Apply compares `ExpectedApplied`,
   checks contiguous entries against the durable committed log, and commits the
   write-set with visible version, monotonic logical time, complete ConfState and
   applied index. Empty normal and configuration entries also advance that index.
4. Only after Apply succeeds may client results escape and the host submit
   `raftcore.Applied{BatchID: batch.ID}`. Recovery skips already-applied entries and
   replays committed entries above Applied. Results in `Sessions` share the same
   transaction, so a dropped reply does not lose its stored result.
5. A local `Read` is a storage dependency read, not an authority response. Serve a
   public read only after a fresh core quorum ReadIndex barrier and durable
   Applied reaching that barrier. The host serializes all core work and storage
   completions; the database mutex alone does not serialize a whole actor.

P09 owns signatures, authorization, declared read dependencies, deterministic
evaluation, response encoding, dedup sequencing and admission policy. This adapter
does not infer these from stored bytes. It accepts opaque records with a 1 MiB
per-record and aggregate read-set envelope. A missing key is absent from the map;
an existing empty record remains present. `Update.Delete` is for an explicitly
authorized application change; no consensus maintenance path deletes records.

## Format and corruption checks

`meta` holds checksummed manifest, hard state, log tip, application header and
complete protobuf ConfState. `raft_log` holds protobuf entries by eight-byte
big-endian index, covering the full uint64 range with correct SQLite ordering.
`records` is keyed by namespace and opaque record key. Namespaces separate state,
sessions/dedup results, membership intents, enrollments, tombstones and durable
outboxes while keeping them in one transaction.

SHA-256 checksums bind payloads to their table and key. The application header
also holds record count and an XOR accumulator of the keyed row hashes to detect
missing rows without loading every row on each write. The log tip detects a
missing uncommitted tail as well as holes. Recovery streams all application rows
through checksum validation, checks SQLite integrity and index/term/commit/applied
invariants, then validates the supplied recovery with the production core. These
checks detect accidental corruption, not a malicious storage operator or rollback
of an entire consistent database to an old backup.

`ErrCorrupt` stops that member and retains evidence. Recovery from a healthy group
uses a newly enrolled replacement ID, not deletion of the file. There is no path
here that deletes chats, frames, branches, saved heads or legacy original stores.

## P05 boundary

Snapshot input explicitly returns `ErrSnapshot`; no snapshot acknowledgment may
escape. P05 must add streamed manifest/chunk generations, checksums, two retained
complete generations, atomic installation and compaction. Its recovered snapshot
must already be installed in the application before constructing the core. The
current schema retains the whole log and recovery materializes that suffix; this
is not the completed bounded log/cache policy. P05 must update the format and
recovery checks together, preserving all application namespaces and exact results.

## Focused proof

Run through the wave's pressure/CPU wrapper:

```sh
sh scripts/raft-proof-footprint.sh go test -p=1 ./internal/raftstore ./internal/raftcore -run '^TestR' -count=1 -timeout=120s -v
```

The R3/R12/R17 transaction tests terminate subprocesses without Close/rollback at
every Ready/application statement and both sides of COMMIT. They recover copies
of only the database and WAL, excluding process memory and the shared-memory index.
The Linux `strace` probe asserts successful WAL fsync/fdatasync between invocation
and return for term/vote, commit-only and application commits. This observes the
production SQLite VFS rather than substituting a fake persistence completion.
Physical power cuts or hardware that lies about flushes are not simulated.

The bounded three-native-node fixture runs real cores/stores and real create/
publish lease rules, then loses a leader and recovers another member from durable
bytes before a quorum read. It also recovers and leaves real joint membership.
Its one-cell evaluator is test-only, not P09's authorized directory writer or the
independent P06 history oracle. Full fault schedules, snapshots, transport,
membership policy, cross-runtime storage and real-client journeys remain later
packets; these mechanism tests do not claim complete R1–R30 release proof.

Build `tinylog`, a crash-safe append-only key–value store, as a Python package
in this workspace. Start from an empty directory; there is no existing code.

Create `tinylog/__init__.py` (further modules are yours to choose) exporting a
class `LogStore`. Standard library only — no third-party dependencies.

## On-disk format — implement exactly this

The store is one file. It is a sequence of records, each framed as:

| bytes | field |
|---|---|
| 4 | payload length `N`, unsigned 32-bit **little-endian** |
| `N` | payload |
| 4 | `zlib.crc32` of **the payload only**, unsigned 32-bit little-endian |

The payload is:

| bytes | field |
|---|---|
| 1 | flags; bit 0 set means this record is a tombstone (a delete) |
| 2 | key length `K`, unsigned 16-bit little-endian |
| `K` | the key |
| `N - 3 - K` | the value |

Three things about this frame are deliberate and are checked:

- The crc covers the payload and **not** the length prefix.
- A record's total size on disk is exactly `4 + N + 4`.
- A tombstone is a real record appended to the log, carrying its key and an
  empty value. Deleting does not rewrite or erase anything.

Keys and values are arbitrary `bytes` — any byte value, including NUL and
`0xff`, and values may be empty. **A `put` with an empty value is a live
record, not a delete.**

## API

```python
class LogStore:
    def __init__(self, path: str)           # opens or creates; recovers first
    def put(self, key: bytes, value: bytes) -> None
    def delete(self, key: bytes) -> None
    def get(self, key: bytes) -> bytes | None
    def scan(self, lo: bytes | None = None, hi: bytes | None = None)
    def compact(self) -> int
    def close(self) -> None
    @property
    def live_keys(self) -> int
```

- **`get`** returns the value of the latest live record for the key, or `None`
  if the key was never written or its latest record is a tombstone.
- **`scan`** yields `(key, value)` pairs for live keys with `lo <= key < hi` —
  **half-open**, so `hi` is excluded and `scan(b"c", b"c")` yields nothing.
  `lo=None` means unbounded below, `hi=None` unbounded above. Order is
  ascending **byte-lexicographic** on the raw key bytes, so `b"\x80"` sorts
  after `b"z"` and `b"A"` before `b"a"`. Deleted keys never appear.
- **`live_keys`** is the number of keys with a live latest record.
- **`compact`** rewrites the file keeping exactly one record per live key, in
  ascending byte-lexicographic key order, with no tombstones and no superseded
  versions. A key whose latest record is a tombstone disappears entirely —
  the tombstone goes too. It returns the number of bytes reclaimed
  (size before minus size after). The store must remain usable afterwards, and
  the compacted file must reopen to the same contents.

## Recovery — the part that is easy to get wrong

`__init__` reads the whole log and rebuilds the live view before accepting any
write. While reading, a record is trustworthy only if the frame fits inside the
file **and** the stored crc matches the payload.

At the first record that is not trustworthy — a length that runs past the end
of the file, a crc that does not match, a frame too short to hold a header —
reading stops, and **everything from that record to the end of the file is
discarded**: the file is truncated to the end of the last good record. This is
an append-only log, so bytes following an unverifiable record cannot be assumed
to be a record at all. A file that is entirely garbage opens as an empty store.
After recovery the store must accept new writes normally, and those writes must
survive the next reopen.

Recovery is not optional or best-effort: a torn tail must actually be truncated
off the file, not merely skipped in memory.

## How this is graded

By a hidden test suite you will not see, grouped by capability: the API and
binary safety, durability across reopen, `scan` semantics and ordering,
recovery from torn and corrupt tails, compaction, and conformance of the bytes
on disk to the frame specified above. The format group is checked by a parser
written independently of your code, so a self-consistent format of your own
invention will fail it.

Your score is the fraction of groups in which **every** test passes; partial
work inside a group scores nothing for that group. A package that does not
import scores zero.

Write real tests of your own as you go — they are not graded and will not be
read, but nothing here is checkable by inspection. Leave the package on disk;
there is nothing to submit and no report to write.

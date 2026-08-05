Build `tinylog`, a crash-safe segmented key–value store, as a Python package in
this workspace. Start from an empty directory; there is no existing code.

Create a `tinylog` package exporting a class `LogStore`. How you split it across
modules is yours. Standard library only — no third-party dependencies.

## Layout on disk

A store lives at `path` but **`path` itself is never written**. Instead:

- **Segments** are named `path.NNNNNN` where `NNNNNN` is the segment number,
  decimal, zero-padded to exactly six digits. The first segment is `000001`.
  Numbers are sequential with no gaps.
- **The manifest** is `path.manifest`, a text file listing the live segment
  numbers in ascending order, one per line, decimal, no padding required.

Writes always append to the **highest-numbered** segment. When that segment
reaches `segment_bytes`, the store starts the next one and appends there. A
single record is never split across segments, so a record larger than
`segment_bytes` simply makes an oversized segment.

## Record format — implement exactly this

Each segment is a sequence of records, each framed as:

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
    def __init__(self, path: str, segment_bytes: int = 65536)
    def put(self, key: bytes, value: bytes) -> None
    def delete(self, key: bytes) -> None
    def get(self, key: bytes) -> bytes | None
    def scan(self, lo: bytes | None = None, hi: bytes | None = None)
    def compact(self) -> int
    def close(self) -> None
    @property
    def live_keys(self) -> int
    @property
    def segments(self) -> list[int]      # live segment numbers, ascending
```

- **`get`** returns the value of the latest live record for the key, or `None`
  if the key was never written or its latest record is a tombstone. It must be
  a lookup, not a search: a store holding 100,000 keys has to answer tens of
  thousands of gets in under a second, which means an in-memory index built at
  open time. An implementation that re-reads the log per `get` will not finish.
- **`scan`** yields `(key, value)` pairs for live keys with `lo <= key < hi` —
  **half-open**, so `hi` is excluded and `scan(b"c", b"c")` yields nothing.
  `lo=None` means unbounded below, `hi=None` unbounded above. Order is
  ascending **byte-lexicographic** on the raw key bytes, so `b"\x80"` sorts
  after `b"z"` and `b"A"` before `b"a"`. Deleted keys never appear, and keys
  from every segment appear in one merged order. **`scan` must be a generator**
  — a caller taking the first pair from a 50,000-key store must not pay for all
  50,000.
- **`live_keys`** is the number of keys with a live latest record.
- **`compact`** merges **every** segment into a single new one, keeping exactly
  one record per live key, in ascending byte-lexicographic key order, with no
  tombstones and no superseded versions. A key whose latest record is a
  tombstone disappears entirely — the tombstone goes too. The old segment files
  are removed and the manifest is rewritten. It returns the number of bytes
  reclaimed (total segment bytes before minus after). The store must remain
  usable afterwards and reopen to the same contents.

## Recovery — the part that is easy to get wrong

`__init__` rebuilds the live view before accepting any write.

**Finding the segments.** Read the manifest if it is there and readable. But the
manifest is a cache of the directory, not the truth: if it is **missing**, if it
is **unparseable**, or if it names a segment that is **not on disk**, fall back
to discovering `path.NNNNNN` files in the directory. A segment file that exists
but is not named in the manifest must still be read — losing a segment because a
manifest forgot it is the worst possible outcome. Write a correct manifest back
out before returning.

**Reading a segment.** A record is trustworthy only if its frame fits inside the
file **and** the stored crc matches the payload. At the first record that is not
trustworthy, reading that segment stops and **everything from that record to the
end of that file is discarded** — the file is truncated to the end of the last
good record. A segment is append-only, so bytes following an unverifiable record
cannot be assumed to be a record at all. Damage to one segment must not discard
any other segment. A file that is entirely garbage contributes nothing and is
not an error.

Recovery is not best-effort: a torn tail must actually be truncated off the
file, not merely skipped in memory. After recovery the store accepts new writes
normally and those writes survive the next reopen.

## How this is graded

By a hidden test suite you will not see, grouped by capability: the API and
binary safety; durability across reopen; `scan` semantics, ordering and
laziness; crash recovery; compaction; conformance of the bytes on disk to the
frame above; segment rolling and naming; manifest loss and repair; and a
performance floor at 100,000 keys. The format group is checked by a parser
written independently of your code, so a self-consistent format of your own
invention will fail it.

Your score is the fraction of groups in which **every** test passes; partial
work inside a group scores nothing for that group. A package that does not
import scores zero.

Write real tests of your own as you go — they are not graded and will not be
read, but almost nothing here is checkable by inspection, and the parts that
look simplest (the roll boundary, the half-open bound, the tombstone that must
not survive compaction) are where this goes wrong. Leave the package on disk;
there is nothing to submit and no report to write.

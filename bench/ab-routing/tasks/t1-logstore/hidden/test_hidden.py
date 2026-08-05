"""Hidden grading suite for tinylog, round 2. Never shown to the agent.

Round 1 specified the frame byte for byte over a single file, and arm A wrote a
fully correct store in one turn -- a transcription exercise, not a design one.
Round 2 keeps groups 1-6 (the format group still has to pin the frame) and adds
the parts that cannot be transcribed: segments, a manifest that recovery must
not trust, and a size the answer has to be fast enough for.

Each group runs in its own pytest process (see grade.py) so a store left open
by one test cannot affect another.

Groups:
  1  api          put/get/delete round-trip, binary safety, empty value != delete
  2  durability   data survives close and reopen
  3  scan         half-open [lo, hi), byte-lexicographic order, tombstones excluded
  4  recovery     a torn or corrupt tail is discarded and truncated off
  5  compact      merges every segment into one, drops tombstoned keys
  6  format       the bytes on disk match the specified frame, checked by a
                  parser written here rather than by calling into the submission
  7  segments     the active segment rolls at the threshold and data spans them
  8  manifest     a missing or corrupt manifest is rebuilt from the directory
  9  performance  100k keys are indexed, not rescanned, and scan is lazy
"""

import os
import struct
import time
import zlib

import pytest

from tinylog import LogStore


@pytest.fixture
def path(tmp_path):
    return str(tmp_path / "store.log")


# ---------------------------------------------------------------------------
# An independent parser: group 6's point is that the submission wrote the format
# in the brief, not a format of its own that its own reader happens to accept.

LEN = struct.Struct("<I")
CRC = struct.Struct("<I")
HDR = struct.Struct("<BH")


def parse(blob):
    out, offset = [], 0
    while offset < len(blob):
        assert len(blob) - offset >= 8, "trailing bytes are not a frame"
        (length,) = LEN.unpack_from(blob, offset)
        end = offset + 4 + length + 4
        assert end <= len(blob), "frame runs past end of file"
        payload = blob[offset + 4:offset + 4 + length]
        (stored,) = CRC.unpack_from(blob, offset + 4 + length)
        assert stored == (zlib.crc32(payload) & 0xFFFFFFFF), "crc mismatch"
        flags, key_length = HDR.unpack_from(payload, 0)
        out.append((payload[3:3 + key_length], payload[3 + key_length:],
                    bool(flags & 1)))
        offset = end
    return out


def frame(key, value, tombstone=False):
    payload = HDR.pack(1 if tombstone else 0, len(key)) + key + value
    return LEN.pack(len(payload)) + payload + CRC.pack(zlib.crc32(payload) & 0xFFFFFFFF)


def segment_files(path):
    """Segment files on disk, in order, found without asking the store."""
    directory = os.path.dirname(os.path.abspath(path)) or "."
    base = os.path.basename(path) + "."
    found = []
    for name in os.listdir(directory):
        tail = name[len(base):] if name.startswith(base) else ""
        if len(tail) == 6 and tail.isdigit():
            found.append((int(tail), os.path.join(directory, name)))
    return [p for _n, p in sorted(found)]


def active_segment(path):
    files = segment_files(path)
    assert files, f"no segment files were created next to {path}"
    return files[-1]


def all_records(path):
    out = []
    for segment in segment_files(path):
        with open(segment, "rb") as f:
            out.extend(parse(f.read()))
    return out


# ---- group 1: the API ------------------------------------------------------

def test_GROUP_1_put_get_round_trip(path):
    s = LogStore(path)
    s.put(b"alpha", b"one")
    s.put(b"beta", b"two")
    assert s.get(b"alpha") == b"one"
    assert s.get(b"beta") == b"two"
    assert s.get(b"missing") is None
    s.close()


def test_GROUP_1_last_write_wins(path):
    s = LogStore(path)
    for v in (b"v1", b"v2", b"v3"):
        s.put(b"k", v)
    assert s.get(b"k") == b"v3"
    assert s.live_keys == 1
    s.close()


def test_GROUP_1_delete_removes(path):
    s = LogStore(path)
    s.put(b"k", b"v")
    s.delete(b"k")
    assert s.get(b"k") is None
    assert s.live_keys == 0
    s.close()


def test_GROUP_1_empty_value_is_not_a_delete(path):
    s = LogStore(path)
    s.put(b"k", b"")
    assert s.get(b"k") == b""
    assert s.get(b"k") is not None
    assert s.live_keys == 1
    s.close()


def test_GROUP_1_binary_safe(path):
    s = LogStore(path)
    keys = [b"\x00\x01", b"\xff\xfe", b"with\x00nul", bytes(range(256))]
    for n, k in enumerate(keys):
        s.put(k, bytes([n]) * 300 + b"\x00\xff")
    for n, k in enumerate(keys):
        assert s.get(k) == bytes([n]) * 300 + b"\x00\xff"
    s.close()


def test_GROUP_1_delete_of_absent_key_is_harmless(path):
    s = LogStore(path)
    s.delete(b"never-existed")
    assert s.live_keys == 0
    s.put(b"k", b"v")
    assert s.get(b"k") == b"v"
    s.close()


# ---- group 2: durability ---------------------------------------------------

def test_GROUP_2_reopen_sees_everything(path):
    s = LogStore(path)
    for n in range(50):
        s.put(f"k{n:03d}".encode(), f"v{n}".encode())
    s.close()
    again = LogStore(path)
    assert again.live_keys == 50
    assert again.get(b"k017") == b"v17"
    again.close()


def test_GROUP_2_reopen_respects_deletes_and_overwrites(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.put(b"b", b"1")
    s.put(b"a", b"2")
    s.delete(b"b")
    s.close()
    again = LogStore(path)
    assert again.get(b"a") == b"2"
    assert again.get(b"b") is None
    assert again.live_keys == 1
    again.close()


def test_GROUP_2_reopen_and_append(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    s = LogStore(path)
    s.put(b"b", b"2")
    s.close()
    s = LogStore(path)
    assert (s.get(b"a"), s.get(b"b")) == (b"1", b"2")
    s.close()


def test_GROUP_2_fresh_path_starts_empty(path):
    s = LogStore(path)
    assert s.live_keys == 0
    assert list(s.scan()) == []
    s.close()


def test_GROUP_2_reopen_across_a_roll(path):
    s = LogStore(path, segment_bytes=1024)
    for n in range(400):
        s.put(f"k{n:04d}".encode(), b"v" * 40)
    s.close()
    again = LogStore(path, segment_bytes=1024)
    assert again.live_keys == 400
    assert again.get(b"k0000") == b"v" * 40
    assert again.get(b"k0399") == b"v" * 40
    again.close()


# ---- group 3: scan ---------------------------------------------------------

def test_GROUP_3_scan_is_sorted_and_complete(path):
    s = LogStore(path)
    for k in [b"d", b"a", b"c", b"b"]:
        s.put(k, k * 2)
    assert list(s.scan()) == [(b"a", b"aa"), (b"b", b"bb"),
                              (b"c", b"cc"), (b"d", b"dd")]
    s.close()


def test_GROUP_3_scan_is_byte_lexicographic_not_text(path):
    s = LogStore(path)
    for k in [b"z", b"\x80", b"A", b"\xff", b"a"]:
        s.put(k, b"x")
    assert [k for k, _ in s.scan()] == [b"A", b"a", b"z", b"\x80", b"\xff"]
    s.close()


def test_GROUP_3_scan_bounds_are_half_open(path):
    s = LogStore(path)
    for k in [b"a", b"b", b"c", b"d", b"e"]:
        s.put(k, b"x")
    assert [k for k, _ in s.scan(b"b", b"d")] == [b"b", b"c"]
    assert [k for k, _ in s.scan(b"b", None)] == [b"b", b"c", b"d", b"e"]
    assert [k for k, _ in s.scan(None, b"c")] == [b"a", b"b"]
    assert [k for k, _ in s.scan(b"c", b"c")] == []
    assert [k for k, _ in s.scan(b"d", b"b")] == []
    s.close()


def test_GROUP_3_scan_excludes_deleted(path):
    s = LogStore(path)
    for k in [b"a", b"b", b"c"]:
        s.put(k, b"x")
    s.delete(b"b")
    assert [k for k, _ in s.scan()] == [b"a", b"c"]
    assert [k for k, _ in s.scan(b"a", b"c")] == [b"a"]
    s.close()


def test_GROUP_3_scan_spans_segments_in_order(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    assert len(segment_files(path)) > 1, "the roll never happened"
    keys = [k for k, _ in s.scan()]
    assert keys == sorted(keys)
    assert len(keys) == 200
    assert [k for k, _ in s.scan(b"k0100", b"k0103")] == \
        [b"k0100", b"k0101", b"k0102"]
    s.close()


# ---- group 4: recovery -----------------------------------------------------

def test_GROUP_4_truncated_tail_is_dropped(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.put(b"b", b"2")
    s.close()
    segment = active_segment(path)
    good = os.path.getsize(segment)
    with open(segment, "ab") as f:
        f.write(frame(b"c", b"3")[:7])

    s = LogStore(path)
    assert s.get(b"a") == b"1" and s.get(b"b") == b"2"
    assert s.get(b"c") is None
    assert s.live_keys == 2
    s.close()
    assert os.path.getsize(segment) == good, "the torn tail should be truncated away"


def test_GROUP_4_corrupt_crc_is_dropped(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    segment = active_segment(path)
    good = os.path.getsize(segment)
    with open(segment, "ab") as f:
        f.write(frame(b"b", b"2"))
    with open(segment, "r+b") as f:
        f.seek(good + 5)
        original = f.read(1)
        f.seek(good + 5)
        f.write(bytes([original[0] ^ 0xFF]))

    s = LogStore(path)
    assert s.get(b"a") == b"1"
    assert s.get(b"b") is None
    s.close()
    assert os.path.getsize(segment) == good


def test_GROUP_4_everything_after_a_bad_record_is_discarded(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    segment = active_segment(path)
    good = os.path.getsize(segment)
    with open(segment, "ab") as f:
        f.write(frame(b"b", b"2"))
        f.write(frame(b"c", b"3"))
    with open(segment, "r+b") as f:
        f.seek(good + 5)
        original = f.read(1)
        f.seek(good + 5)
        f.write(bytes([original[0] ^ 0xFF]))

    s = LogStore(path)
    assert s.get(b"b") is None
    assert s.get(b"c") is None
    assert s.live_keys == 1
    s.close()


def test_GROUP_4_store_is_usable_after_recovery(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    with open(active_segment(path), "ab") as f:
        f.write(frame(b"z", b"9")[:6])

    s = LogStore(path)
    s.put(b"b", b"2")
    s.close()
    s = LogStore(path)
    assert s.get(b"a") == b"1" and s.get(b"b") == b"2"
    assert s.get(b"z") is None
    s.close()


def test_GROUP_4_garbage_only_segment_opens_empty(path):
    s = LogStore(path)
    s.close()
    with open(active_segment(path), "wb") as f:
        f.write(b"\xff" * 64)
    s = LogStore(path)
    assert s.live_keys == 0
    s.put(b"a", b"1")
    s.close()
    s = LogStore(path)
    assert s.get(b"a") == b"1"
    s.close()


def test_GROUP_4_a_torn_earlier_segment_does_not_lose_later_ones(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    s.close()
    files = segment_files(path)
    assert len(files) >= 3
    with open(files[0], "ab") as f:            # damage the first segment's tail
        f.write(b"\x00" * 9)

    s = LogStore(path, segment_bytes=512)
    # whatever the first segment lost, the later segments are intact and their
    # keys must still be there
    assert s.get(b"k0199") == b"v" * 20
    assert s.live_keys > 100
    s.close()


# ---- group 5: compaction ---------------------------------------------------

def test_GROUP_5_compact_reclaims_and_preserves(path):
    s = LogStore(path)
    for n in range(40):
        s.put(b"hot", f"v{n}".encode())
    s.put(b"cold", b"c")
    reclaimed = s.compact()
    assert reclaimed > 0
    assert s.get(b"hot") == b"v39"
    assert s.get(b"cold") == b"c"
    assert s.live_keys == 2
    s.close()


def test_GROUP_5_compact_merges_every_segment_into_one(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    assert len(segment_files(path)) > 1
    s.compact()
    assert len(segment_files(path)) == 1, \
        "compaction must leave exactly one segment and remove the rest"
    assert s.live_keys == 200
    s.close()


def test_GROUP_5_compact_drops_tombstoned_keys(path):
    s = LogStore(path)
    s.put(b"gone", b"x" * 200)
    s.put(b"stay", b"y")
    s.delete(b"gone")
    s.compact()
    s.close()
    records = all_records(path)
    assert [k for k, _v, _t in records] == [b"stay"]
    assert not any(t for _k, _v, t in records), "no tombstones survive compaction"


def test_GROUP_5_compact_output_reopens(path):
    s = LogStore(path, segment_bytes=1024)
    for n in range(60):
        s.put(f"k{n:02d}".encode(), b"v" * 50)
    s.delete(b"k00")
    s.put(b"k01", b"final")
    s.compact()
    s.close()
    s = LogStore(path, segment_bytes=1024)
    assert s.live_keys == 59
    assert s.get(b"k00") is None
    assert s.get(b"k01") == b"final"
    s.close()


def test_GROUP_5_compact_of_empty_store_is_safe(path):
    s = LogStore(path)
    s.compact()
    assert s.live_keys == 0
    s.put(b"a", b"1")
    s.close()
    s = LogStore(path)
    assert s.get(b"a") == b"1"
    s.close()


def test_GROUP_5_compact_writes_keys_in_order(path):
    s = LogStore(path)
    for k in [b"z", b"\x80", b"a", b"A"]:
        s.put(k, b"v")
    s.compact()
    s.close()
    assert [k for k, _v, _t in all_records(path)] == [b"A", b"a", b"z", b"\x80"]


# ---- group 6: the on-disk format -------------------------------------------

def test_GROUP_6_records_parse_independently(path):
    s = LogStore(path)
    s.put(b"alpha", b"one")
    s.put(b"beta", b"two")
    s.close()
    assert all_records(path) == [(b"alpha", b"one", False), (b"beta", b"two", False)]


def test_GROUP_6_delete_is_a_tombstone_record(path):
    s = LogStore(path)
    s.put(b"k", b"v")
    s.delete(b"k")
    s.close()
    records = all_records(path)
    assert len(records) == 2
    assert records[0] == (b"k", b"v", False)
    assert records[1][0] == b"k" and records[1][2] is True


def test_GROUP_6_frame_size_is_exact(path):
    s = LogStore(path)
    s.put(b"key", b"value")
    s.close()
    # 4 length + (1 flags + 2 keylen + 3 key + 5 value) + 4 crc
    assert os.path.getsize(active_segment(path)) == 4 + (1 + 2 + 3 + 5) + 4


def test_GROUP_6_a_hand_written_segment_is_readable(path):
    s = LogStore(path)
    segment = active_segment(path)
    s.close()
    with open(segment, "wb") as f:
        f.write(frame(b"a", b"1"))
        f.write(frame(b"b", b"2"))
        f.write(frame(b"a", b"", tombstone=True))
    s = LogStore(path)
    assert s.get(b"a") is None
    assert s.get(b"b") == b"2"
    assert s.live_keys == 1
    s.close()


def test_GROUP_6_crc_covers_payload_not_length(path):
    s = LogStore(path)
    segment = active_segment(path)
    s.close()
    with open(segment, "wb") as f:
        f.write(frame(b"only", b"record"))
    s = LogStore(path)
    assert s.get(b"only") == b"record"
    s.close()


# ---- group 7: segments -----------------------------------------------------

def test_GROUP_7_first_segment_is_numbered_one(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    assert os.path.exists(path + ".000001")


def test_GROUP_7_active_segment_rolls_at_the_threshold(path):
    s = LogStore(path, segment_bytes=1024)
    for n in range(300):
        s.put(f"k{n:04d}".encode(), b"v" * 40)
    s.close()
    files = segment_files(path)
    assert len(files) > 1, "no roll happened at a 1 KiB threshold"
    # every full segment must be at least the threshold; only the last may be
    # short, and none may run away far past it
    for f in files[:-1]:
        assert os.path.getsize(f) >= 1024, f"{f} rolled before the threshold"
        assert os.path.getsize(f) < 1024 * 4, f"{f} rolled long after the threshold"


def test_GROUP_7_segment_numbers_are_sequential_and_zero_padded(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    s.close()
    names = sorted(os.path.basename(f) for f in segment_files(path))
    base = os.path.basename(path)
    numbers = [int(n[len(base) + 1:]) for n in names]
    assert all(len(n[len(base) + 1:]) == 6 for n in names), "not zero-padded to six"
    assert numbers == list(range(numbers[0], numbers[0] + len(numbers)))


def test_GROUP_7_a_big_value_still_lands_in_one_segment(path):
    s = LogStore(path, segment_bytes=1024)
    big = b"x" * 5000
    s.put(b"big", big)
    s.put(b"after", b"y")
    s.close()
    s = LogStore(path, segment_bytes=1024)
    assert s.get(b"big") == big
    assert s.get(b"after") == b"y"
    s.close()


def test_GROUP_7_segments_property_lists_them(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    assert list(s.segments) == sorted(s.segments)
    assert len(s.segments) == len(segment_files(path))
    s.close()


# ---- group 8: the manifest -------------------------------------------------

def test_GROUP_8_manifest_exists_and_lists_the_segments(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    s.close()
    with open(path + ".manifest") as f:
        listed = [int(l) for l in f.read().split()]
    base = os.path.basename(path)
    on_disk = sorted(int(os.path.basename(p)[len(base) + 1:])
                     for p in segment_files(path))
    assert listed == on_disk


def test_GROUP_8_a_deleted_manifest_is_rebuilt(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    s.close()
    os.remove(path + ".manifest")

    s = LogStore(path, segment_bytes=512)
    assert s.live_keys == 200, "the store did not rediscover its segments"
    assert s.get(b"k0000") == b"v" * 20
    assert s.get(b"k0199") == b"v" * 20
    s.close()
    assert os.path.exists(path + ".manifest"), "the manifest was not rewritten"


def test_GROUP_8_a_corrupt_manifest_is_rebuilt(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    s.close()
    with open(path + ".manifest", "w") as f:
        f.write("this is not a manifest\n\x00\x01\n")

    s = LogStore(path, segment_bytes=512)
    assert s.live_keys == 200
    s.close()


def test_GROUP_8_a_manifest_missing_a_segment_does_not_lose_it(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    s.close()
    with open(path + ".manifest") as f:
        listed = [l for l in f.read().split() if l]
    with open(path + ".manifest", "w") as f:
        f.write("\n".join(listed[:-1]) + "\n")   # forget the newest segment

    s = LogStore(path, segment_bytes=512)
    assert s.get(b"k0199") == b"v" * 20, \
        "a segment the manifest forgot is still on disk and must be read"
    s.close()


def test_GROUP_8_manifest_after_compaction_lists_one_segment(path):
    s = LogStore(path, segment_bytes=512)
    for n in range(200):
        s.put(f"k{n:04d}".encode(), b"v" * 20)
    s.compact()
    s.close()
    with open(path + ".manifest") as f:
        listed = [int(l) for l in f.read().split()]
    assert len(listed) == 1
    assert os.path.exists(path + f".{listed[0]:06d}")


# ---- group 9: it has to be fast enough to be a store -----------------------

def test_GROUP_9_a_hundred_thousand_gets_are_indexed_not_rescanned(path):
    s = LogStore(path, segment_bytes=1 << 20)
    n = 100_000
    for i in range(n):
        s.put(f"k{i:06d}".encode(), b"v" * 16)
    started = time.monotonic()
    for i in range(0, n, 7):
        assert s.get(f"k{i:06d}".encode()) == b"v" * 16
    elapsed = time.monotonic() - started
    # ~14k lookups. An in-memory index does this in well under a second; a
    # implementation that re-reads the log per get cannot finish at all.
    assert elapsed < 5.0, f"{elapsed:.1f}s for 14k gets — get is not O(1)"
    s.close()


def test_GROUP_9_reopen_of_a_large_store_is_bounded(path):
    s = LogStore(path, segment_bytes=1 << 20)
    for i in range(60_000):
        s.put(f"k{i:06d}".encode(), b"v" * 16)
    s.close()
    started = time.monotonic()
    s = LogStore(path, segment_bytes=1 << 20)
    elapsed = time.monotonic() - started
    assert s.live_keys == 60_000
    assert elapsed < 30.0, f"{elapsed:.1f}s to reopen 60k keys"
    s.close()


def test_GROUP_9_scan_is_lazy(path):
    s = LogStore(path, segment_bytes=1 << 20)
    for i in range(50_000):
        s.put(f"k{i:06d}".encode(), b"v" * 16)
    started = time.monotonic()
    iterator = s.scan()
    first = next(iterator)
    elapsed = time.monotonic() - started
    assert first[0] == b"k000000"
    # scan returns an iterator, so asking for one item must not cost what
    # asking for fifty thousand costs
    assert elapsed < 3.0, f"{elapsed:.1f}s to take the first item of a scan"
    s.close()


def test_GROUP_9_scan_returns_an_iterator_not_a_list(path):
    s = LogStore(path)
    for i in range(10):
        s.put(f"k{i}".encode(), b"v")
    result = s.scan()
    assert not isinstance(result, (list, tuple)), \
        "scan must yield, so a caller can stop early"
    assert next(iter(result))[0] == b"k0"
    s.close()

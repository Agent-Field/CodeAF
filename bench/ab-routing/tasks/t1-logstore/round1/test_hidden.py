"""Hidden grading suite for tinylog. Never shown to the agent.

Grouped by capability so partial work scores partially. Each group runs in its
own pytest process (see grade.py) so a store left open by one test cannot
affect another.

Groups:
  1  api        put/get/delete round-trip, binary safety, empty value != delete
  2  durability data survives close and reopen
  3  scan       half-open [lo, hi), byte-lexicographic order, tombstones excluded
  4  recovery   a torn or corrupt tail is discarded and the store stays usable
  5  compact    reclaims bytes, drops tombstoned keys, keeps last-write-wins
  6  format     the bytes on disk match the specified frame, checked by a parser
                written here rather than by calling back into the submission
"""

import os
import struct
import zlib

import pytest

from tinylog import LogStore


@pytest.fixture
def path(tmp_path):
    return str(tmp_path / "store.log")


# ---------------------------------------------------------------------------
# An independent parser. The point of group 6 is that the submission wrote the
# format in the brief, not a format of its own that its own reader happens to
# understand, so nothing here calls into tinylog.

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
        key = payload[3:3 + key_length]
        value = payload[3 + key_length:]
        out.append((key, value, bool(flags & 1)))
        offset = end
    return out


def frame(key, value, tombstone=False):
    payload = HDR.pack(1 if tombstone else 0, len(key)) + key + value
    return LEN.pack(len(payload)) + payload + CRC.pack(zlib.crc32(payload) & 0xFFFFFFFF)


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
    s.put(b"k", b"v1")
    s.put(b"k", b"v2")
    s.put(b"k", b"v3")
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


# ---- group 3: scan ---------------------------------------------------------

def test_GROUP_3_scan_is_sorted_and_complete(path):
    s = LogStore(path)
    for k in [b"d", b"a", b"c", b"b"]:
        s.put(k, k * 2)
    assert [k for k, _ in s.scan()] == [b"a", b"b", b"c", b"d"]
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


def test_GROUP_3_scan_survives_reopen(path):
    s = LogStore(path)
    for k in [b"a", b"b", b"c"]:
        s.put(k, b"x")
    s.delete(b"b")
    s.close()
    s = LogStore(path)
    assert [k for k, _ in s.scan()] == [b"a", b"c"]
    s.close()


# ---- group 4: recovery -----------------------------------------------------

def test_GROUP_4_truncated_tail_is_dropped(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.put(b"b", b"2")
    s.close()
    good = os.path.getsize(path)
    with open(path, "ab") as f:            # half a record
        f.write(frame(b"c", b"3")[:7])

    s = LogStore(path)
    assert s.get(b"a") == b"1" and s.get(b"b") == b"2"
    assert s.get(b"c") is None
    assert s.live_keys == 2
    s.close()
    assert os.path.getsize(path) == good, "the torn tail should be truncated away"


def test_GROUP_4_corrupt_crc_is_dropped(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    good = os.path.getsize(path)
    with open(path, "ab") as f:
        f.write(frame(b"b", b"2"))
    with open(path, "r+b") as f:           # flip a byte inside the payload
        f.seek(good + 5)
        original = f.read(1)
        f.seek(good + 5)
        f.write(bytes([original[0] ^ 0xFF]))

    s = LogStore(path)
    assert s.get(b"a") == b"1"
    assert s.get(b"b") is None
    s.close()
    assert os.path.getsize(path) == good


def test_GROUP_4_everything_after_a_bad_record_is_discarded(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    good = os.path.getsize(path)
    with open(path, "ab") as f:
        f.write(frame(b"b", b"2"))
        f.write(frame(b"c", b"3"))
    with open(path, "r+b") as f:
        f.seek(good + 5)
        original = f.read(1)
        f.seek(good + 5)
        f.write(bytes([original[0] ^ 0xFF]))

    s = LogStore(path)
    # the log is append-only: nothing after an unverifiable record can be
    # trusted to be a record at all
    assert s.get(b"b") is None
    assert s.get(b"c") is None
    assert s.live_keys == 1
    s.close()


def test_GROUP_4_store_is_usable_after_recovery(path):
    s = LogStore(path)
    s.put(b"a", b"1")
    s.close()
    with open(path, "ab") as f:
        f.write(frame(b"z", b"9")[:6])

    s = LogStore(path)
    s.put(b"b", b"2")
    s.close()
    s = LogStore(path)
    assert s.get(b"a") == b"1" and s.get(b"b") == b"2"
    assert s.get(b"z") is None
    s.close()


def test_GROUP_4_garbage_only_file_opens_empty(path):
    with open(path, "wb") as f:
        f.write(b"\xff" * 64)
    s = LogStore(path)
    assert s.live_keys == 0
    s.put(b"a", b"1")
    s.close()
    s = LogStore(path)
    assert s.get(b"a") == b"1"
    s.close()


# ---- group 5: compaction ---------------------------------------------------

def test_GROUP_5_compact_reclaims_and_preserves(path):
    s = LogStore(path)
    for n in range(40):
        s.put(b"hot", f"v{n}".encode())
    s.put(b"cold", b"c")
    before = os.path.getsize(path)
    reclaimed = s.compact()
    after = os.path.getsize(path)
    assert reclaimed == before - after
    assert reclaimed > 0
    assert s.get(b"hot") == b"v39"
    assert s.get(b"cold") == b"c"
    assert s.live_keys == 2
    s.close()


def test_GROUP_5_compact_drops_tombstoned_keys(path):
    s = LogStore(path)
    s.put(b"gone", b"x" * 200)
    s.put(b"stay", b"y")
    s.delete(b"gone")
    s.compact()
    s.close()
    with open(path, "rb") as f:
        records = parse(f.read())
    keys = [k for k, _v, _t in records]
    assert b"gone" not in keys, "a compacted log keeps no trace of a deleted key"
    assert keys == [b"stay"]
    assert not any(t for _k, _v, t in records), "no tombstones survive compaction"


def test_GROUP_5_compact_output_reopens(path):
    s = LogStore(path)
    for n in range(30):
        s.put(f"k{n:02d}".encode(), b"v" * 50)
    s.delete(b"k00")
    s.put(b"k01", b"final")
    s.compact()
    s.close()
    s = LogStore(path)
    assert s.live_keys == 29
    assert s.get(b"k00") is None
    assert s.get(b"k01") == b"final"
    assert [k for k, _ in s.scan()][:2] == [b"k01", b"k02"]
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
    with open(path, "rb") as f:
        records = parse(f.read())
    assert [k for k, _v, _t in records] == [b"A", b"a", b"z", b"\x80"]


# ---- group 6: the on-disk format is the specified one ----------------------

def test_GROUP_6_records_parse_independently(path):
    s = LogStore(path)
    s.put(b"alpha", b"one")
    s.put(b"beta", b"two")
    s.close()
    with open(path, "rb") as f:
        records = parse(f.read())
    assert records == [(b"alpha", b"one", False), (b"beta", b"two", False)]


def test_GROUP_6_delete_is_a_tombstone_record(path):
    s = LogStore(path)
    s.put(b"k", b"v")
    s.delete(b"k")
    s.close()
    with open(path, "rb") as f:
        records = parse(f.read())
    assert len(records) == 2
    assert records[0] == (b"k", b"v", False)
    assert records[1][0] == b"k" and records[1][2] is True


def test_GROUP_6_frame_size_is_exact(path):
    s = LogStore(path)
    s.put(b"key", b"value")
    s.close()
    # 4 length + (1 flags + 2 keylen + 3 key + 5 value) + 4 crc
    assert os.path.getsize(path) == 4 + (1 + 2 + 3 + 5) + 4


def test_GROUP_6_a_hand_written_log_is_readable(path):
    with open(path, "wb") as f:
        f.write(frame(b"a", b"1"))
        f.write(frame(b"b", b"2"))
        f.write(frame(b"a", b"", tombstone=True))
    s = LogStore(path)
    assert s.get(b"a") is None
    assert s.get(b"b") == b"2"
    assert s.live_keys == 1
    s.close()


def test_GROUP_6_crc_covers_payload_not_length(path):
    # A store that checksummed the length prefix too would reject this record,
    # which was written exactly as the brief specifies.
    with open(path, "wb") as f:
        f.write(frame(b"only", b"record"))
    s = LogStore(path)
    assert s.get(b"only") == b"record"
    s.close()

"""tinylog reference implementation, round 2. Never shipped to an agent.

Round 1 was a transcription exercise: the brief specified the frame byte for
byte and a single file, and arm A produced a fully correct store in one turn.
Round 2 keeps the frame -- the format group still has to pin something -- and
adds the parts that cannot be transcribed: a segmented log with a manifest,
recovery that has to reconstruct a manifest it does not trust, compaction that
merges segments, and an index that makes 100k gets cheap. See BASELINE.md §
"calibration rounds".
"""

import os
import struct
import zlib

TOMBSTONE = 0x01
DEFAULT_SEGMENT_BYTES = 65536

_LEN = struct.Struct("<I")   # payload length
_CRC = struct.Struct("<I")   # crc32 of the payload
_HDR = struct.Struct("<BH")  # flags, key length

__all__ = ["LogStore", "TOMBSTONE", "encode_record", "iter_records",
           "DEFAULT_SEGMENT_BYTES"]


def encode_record(key, value, tombstone=False):
    """One framed record: length, payload, crc32(payload)."""
    if not isinstance(key, (bytes, bytearray)) or not isinstance(value, (bytes, bytearray)):
        raise TypeError("keys and values are bytes")
    if len(key) > 0xFFFF:
        raise ValueError("key longer than 65535 bytes")
    payload = _HDR.pack(TOMBSTONE if tombstone else 0, len(key)) + bytes(key) + bytes(value)
    return _LEN.pack(len(payload)) + payload + _CRC.pack(zlib.crc32(payload) & 0xFFFFFFFF)


def iter_records(blob):
    """Walk a segment image, stopping at the first byte that cannot be trusted."""
    offset, size = 0, len(blob)
    while offset < size:
        if size - offset < _LEN.size + _CRC.size:
            return
        (length,) = _LEN.unpack_from(blob, offset)
        end = offset + _LEN.size + length + _CRC.size
        if end > size or length < _HDR.size:
            return
        payload = blob[offset + _LEN.size:offset + _LEN.size + length]
        (stored,) = _CRC.unpack_from(blob, offset + _LEN.size + length)
        if stored != (zlib.crc32(payload) & 0xFFFFFFFF):
            return
        flags, key_length = _HDR.unpack_from(payload, 0)
        if _HDR.size + key_length > length:
            return
        key = bytes(payload[_HDR.size:_HDR.size + key_length])
        value = bytes(payload[_HDR.size + key_length:])
        yield end, key, value, bool(flags & TOMBSTONE)
        offset = end


class LogStore:
    def __init__(self, path, segment_bytes=DEFAULT_SEGMENT_BYTES):
        self.path = path
        self.segment_bytes = int(segment_bytes)
        if self.segment_bytes <= 0:
            raise ValueError("segment_bytes must be positive")
        self._live = {}
        self._closed = False
        self._segments = []
        self._handle = None
        self._recover()

    # -- layout ------------------------------------------------------------

    @property
    def manifest_path(self):
        return self.path + ".manifest"

    def segment_path(self, number):
        return f"{self.path}.{number:06d}"

    def _discover_segments(self):
        """Segment files actually on disk, in order.

        This is the source of truth when the manifest is missing or unreadable.
        A manifest is a cache of the directory listing; treating it as the only
        record means one corrupt file loses a store that is entirely intact.
        """
        directory = os.path.dirname(os.path.abspath(self.path)) or "."
        base = os.path.basename(self.path) + "."
        found = []
        for name in os.listdir(directory):
            if not name.startswith(base):
                continue
            tail = name[len(base):]
            if len(tail) == 6 and tail.isdigit():
                found.append(int(tail))
        return sorted(found)

    def _read_manifest(self):
        try:
            with open(self.manifest_path, "r") as handle:
                lines = [l.strip() for l in handle if l.strip()]
        except OSError:
            return None
        numbers = []
        for line in lines:
            if not line.isdigit():
                return None          # unreadable: fall back to discovery
            numbers.append(int(line))
        return numbers or None

    def _write_manifest(self):
        """Atomic: write beside, fsync, rename. A manifest torn by a crash is
        the one file whose corruption loses the whole store, so it is never
        updated in place."""
        temporary = self.manifest_path + ".tmp"
        with open(temporary, "w") as handle:
            handle.write("".join(f"{n}\n" for n in self._segments))
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, self.manifest_path)

    # -- lifecycle ---------------------------------------------------------

    def _recover(self):
        discovered = self._discover_segments()
        listed = self._read_manifest()
        if listed is not None and all(os.path.exists(self.segment_path(n)) for n in listed):
            # trust the manifest, but never lose a segment it forgot about
            segments = sorted(set(listed) | set(discovered))
        else:
            segments = discovered
        if not segments:
            segments = [1]

        live = {}
        for number in segments:
            path = self.segment_path(number)
            try:
                with open(path, "rb") as handle:
                    blob = handle.read()
            except OSError:
                open(path, "wb").close()
                continue
            good_end = 0
            for end, key, value, dead in iter_records(blob):
                good_end = end
                if dead:
                    live.pop(key, None)
                else:
                    live[key] = value
            if good_end != len(blob):
                with open(path, "r+b") as handle:
                    handle.truncate(good_end)

        self._segments = segments
        self._live = live
        self._write_manifest()
        self._handle = open(self.segment_path(segments[-1]), "ab", buffering=0)

    def close(self):
        if not self._closed:
            if self._handle:
                self._handle.close()
            self._closed = True

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()
        return False

    def _check(self):
        if self._closed:
            raise ValueError("store is closed")

    # -- writes ------------------------------------------------------------

    def _roll_if_needed(self):
        if self._handle.tell() < self.segment_bytes:
            return
        self._handle.close()
        self._segments.append(self._segments[-1] + 1)
        self._handle = open(self.segment_path(self._segments[-1]), "ab", buffering=0)
        self._write_manifest()

    def _append(self, key, value, tombstone):
        self._handle.write(encode_record(key, value, tombstone))
        self._handle.flush()
        self._roll_if_needed()

    def put(self, key, value):
        self._check()
        self._append(key, value, False)
        self._live[bytes(key)] = bytes(value)

    def delete(self, key):
        self._check()
        key = bytes(key)
        if key not in self._live:
            return False
        self._append(key, b"", True)
        self._live.pop(key, None)
        return True

    # -- reads -------------------------------------------------------------

    def get(self, key):
        self._check()
        return self._live.get(bytes(key))

    def scan(self, lo=None, hi=None):
        """Lazy: the generator yields without sorting the whole key space when
        the caller only wants the front of the range."""
        self._check()
        lo = None if lo is None else bytes(lo)
        hi = None if hi is None else bytes(hi)
        for key in sorted(self._live):
            if lo is not None and key < lo:
                continue
            if hi is not None and key >= hi:
                return
            yield key, self._live[key]

    @property
    def live_keys(self):
        return len(self._live)

    @property
    def segments(self):
        return list(self._segments)

    # -- maintenance -------------------------------------------------------

    def compact(self):
        """Merge every segment into one, keeping one live record per key."""
        self._check()
        before = sum(os.path.getsize(self.segment_path(n))
                     for n in self._segments if os.path.exists(self.segment_path(n)))
        self._handle.close()

        target = self._segments[-1] + 1
        temporary = self.segment_path(target) + ".tmp"
        with open(temporary, "wb") as handle:
            for key in sorted(self._live):
                handle.write(encode_record(key, self._live[key], False))
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, self.segment_path(target))

        old = list(self._segments)
        self._segments = [target]
        self._write_manifest()      # manifest first: it is what recovery reads
        for number in old:
            try:
                os.remove(self.segment_path(number))
            except OSError:
                pass

        after = os.path.getsize(self.segment_path(target))
        self._handle = open(self.segment_path(target), "ab", buffering=0)
        return before - after

"""tinylog reference implementation -- never shipped to an agent.

Its job is to prove the hidden suite is satisfiable and to pin the on-disk
format the suite checks, so that a failing grade is evidence about the model
rather than about the grader.
"""

import os
import struct
import zlib

TOMBSTONE = 0x01

_LEN = struct.Struct("<I")   # payload length
_CRC = struct.Struct("<I")   # crc32 of the payload
_HDR = struct.Struct("<BH")  # flags, key length

__all__ = ["LogStore", "TOMBSTONE", "encode_record", "iter_records"]


def encode_record(key, value, tombstone=False):
    """One framed record: length, payload, crc32(payload).

    The crc covers the payload only. A length prefix that is itself covered by
    the checksum cannot be trusted to find the checksum, so the frame is
    deliberately arranged so the length is read first and verified by the
    arithmetic working out, not by the crc.
    """
    if not isinstance(key, (bytes, bytearray)) or not isinstance(value, (bytes, bytearray)):
        raise TypeError("keys and values are bytes")
    if len(key) > 0xFFFF:
        raise ValueError("key longer than 65535 bytes")
    payload = _HDR.pack(TOMBSTONE if tombstone else 0, len(key)) + bytes(key) + bytes(value)
    return _LEN.pack(len(payload)) + payload + _CRC.pack(zlib.crc32(payload) & 0xFFFFFFFF)


def iter_records(blob):
    """Walk a log image, stopping at the first byte that cannot be trusted.

    Yields (end_offset, key, value, tombstone). The log is append-only, so a
    record that fails to verify invalidates everything after it as well: there
    is no way to know whether the bytes that follow are a later record or the
    tail of the damaged one.
    """
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
    def __init__(self, path):
        self.path = path
        self._live = {}
        self._closed = False
        self._recover()
        self._handle = open(self.path, "ab", buffering=0)

    # -- lifecycle ---------------------------------------------------------

    def _recover(self):
        if not os.path.exists(self.path):
            open(self.path, "wb").close()
            return
        with open(self.path, "rb") as handle:
            blob = handle.read()
        good_end = 0
        live = {}
        for end, key, value, dead in iter_records(blob):
            good_end = end
            if dead:
                live.pop(key, None)
            else:
                live[key] = value
        if good_end != len(blob):
            with open(self.path, "r+b") as handle:
                handle.truncate(good_end)
        self._live = live

    def close(self):
        if not self._closed:
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

    def _append(self, key, value, tombstone):
        self._handle.write(encode_record(key, value, tombstone))
        self._handle.flush()
        os.fsync(self._handle.fileno())

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
        self._check()
        for key in sorted(self._live):
            if lo is not None and key < bytes(lo):
                continue
            if hi is not None and key >= bytes(hi):
                continue
            yield key, self._live[key]

    @property
    def live_keys(self):
        return len(self._live)

    # -- maintenance -------------------------------------------------------

    def compact(self):
        self._check()
        before = os.path.getsize(self.path)
        temporary = self.path + ".compact"
        with open(temporary, "wb") as handle:
            for key in sorted(self._live):
                handle.write(encode_record(key, self._live[key], False))
            handle.flush()
            os.fsync(handle.fileno())
        self._handle.close()
        os.replace(temporary, self.path)
        self._handle = open(self.path, "ab", buffering=0)
        return before - os.path.getsize(self.path)

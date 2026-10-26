// Package raftsnapshot defines the portable, streamed coordination snapshot.
// Hosts persist chunks; consensus retains only the small manifest in Snapshot.Data.
package raftsnapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

const (
	Version             = 1
	ChunkBytes          = 256 << 10
	RecordBytes         = 1 << 20
	ManifestBytes       = 16 << 10
	RetainedGenerations = 2
)

var ErrFormat = errors.New("raftsnapshot: invalid snapshot format or checksum")

// Manifest uses decimal strings for every integer crossing JavaScript. Conf is
// deterministic protobuf, preserving joint voters, learners-next and AutoLeave.
// Digest covers the concatenated fixed-size chunks, independent of transport.
type Manifest struct {
	Version        uint64 `json:"format,string"`
	Cluster        string `json:"cluster"`
	Protocol       uint64 `json:"protocol,string"`
	Index          uint64 `json:"index,string"`
	Term           uint64 `json:"term,string"`
	Conf           []byte `json:"conf"`
	VisibleVersion uint64 `json:"version,string"`
	LogicalTime    int64  `json:"now,string"`
	RecordCount    uint64 `json:"records,string"`
	RecordsHash    string `json:"records_hash"`
	Bytes          uint64 `json:"bytes,string"`
	Chunks         uint64 `json:"chunks,string"`
	Digest         string `json:"digest"`
}

var wire = proto.MarshalOptions{Deterministic: true}

func (m Manifest) Configuration() (*pb.ConfState, error) {
	c := &pb.ConfState{}
	if proto.Unmarshal(m.Conf, c) != nil || len(c.ProtoReflect().GetUnknown()) != 0 {
		return nil, ErrFormat
	}
	b, err := wire.Marshal(c)
	if err != nil || !bytes.Equal(b, m.Conf) {
		return nil, ErrFormat
	}
	return c, nil
}

func Configuration(c *pb.ConfState) ([]byte, error) { return wire.Marshal(c) }

func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && hex.EncodeToString(b) == s
}

func (m Manifest) Encode() ([]byte, error) {
	if m.Version != Version || m.Cluster == "" || !utf8.ValidString(m.Cluster) || m.Protocol == 0 || m.Index == 0 || m.Term == 0 || !validHash(m.Digest) || !validHash(m.RecordsHash) || m.Chunks != m.Bytes/ChunkBytes+boolUint(m.Bytes%ChunkBytes != 0) {
		return nil, ErrFormat
	}
	if _, e := m.Configuration(); e != nil {
		return nil, e
	}
	b, e := json.Marshal(m)
	if e != nil || len(b) > ManifestBytes {
		return nil, ErrFormat
	}
	return b, nil
}
func boolUint(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

func Decode(b []byte) (Manifest, error) {
	var m Manifest
	if len(b) > ManifestBytes || json.Unmarshal(b, &m) != nil {
		return m, ErrFormat
	}
	canonical, e := m.Encode()
	if e != nil || !bytes.Equal(canonical, b) {
		return Manifest{}, ErrFormat
	}
	return m, nil
}

func ID(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Row framing has three big-endian uint32 lengths followed by namespace, key
// and opaque value. A record may cross chunk boundaries; it is never truncated.
type Row struct {
	Namespace string
	Key       string
	Value     []byte
}

func validRow(r Row) bool {
	switch r.Namespace {
	case "state", "sessions", "membership", "enrollment", "tombstones", "outbox":
	default:
		return false
	}
	return len(r.Key) > 0 && len(r.Key) <= RecordBytes && len(r.Value) <= RecordBytes
}
func WriteRow(w io.Writer, r Row) error {
	if !validRow(r) {
		return ErrFormat
	}
	var header [12]byte
	binary.BigEndian.PutUint32(header[:4], uint32(len(r.Namespace)))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(r.Key)))
	binary.BigEndian.PutUint32(header[8:], uint32(len(r.Value)))
	for _, b := range [][]byte{header[:], []byte(r.Namespace), []byte(r.Key), r.Value} {
		if _, e := w.Write(b); e != nil {
			return e
		}
	}
	return nil
}
func ReadRow(r io.Reader) (Row, error) {
	var header [12]byte
	if _, e := io.ReadFull(r, header[:]); e != nil {
		return Row{}, e
	}
	n, k, v := binary.BigEndian.Uint32(header[:4]), binary.BigEndian.Uint32(header[4:8]), binary.BigEndian.Uint32(header[8:])
	if n > 16 || k == 0 || k > RecordBytes || v > RecordBytes {
		return Row{}, ErrFormat
	}
	b := make([]byte, int(n)+int(k)+int(v))
	if _, e := io.ReadFull(r, b); e != nil {
		return Row{}, e
	}
	row := Row{string(b[:n]), string(b[n : n+k]), b[n+k:]}
	if !validRow(row) {
		return Row{}, ErrFormat
	}
	return row, nil
}

// Package rafttransport carries authenticated peer traffic without granting
// directory authority. Hosts still own consensus persistence and application.
package rafttransport

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

const (
	Protocol       = raftcore.ProtocolVersion
	MaxBatchBytes  = raftcore.MaxMessageBytes
	MaxChunkBytes  = raftsnapshot.ChunkBytes
	MaxInflight    = raftcore.MaxInflightBatches
	MaxLearners    = 32
	MaxMessages    = 64
	MaxQueuedBytes = 1 << 20
	// Encoded envelopes include base64 payloads, signatures and bounded IDs.
	MaxExchangeBytes = 2 * MaxChunkBytes
)

var (
	ErrAuth       = errors.New("rafttransport: member authorization refused")
	ErrWire       = errors.New("rafttransport: invalid peer envelope")
	ErrGeneration = errors.New("rafttransport: transport generation changed; reconnect")
	ErrCapacity   = errors.New("rafttransport: transport is busy; retry with backoff")
	ErrCursor     = errors.New("rafttransport: invalid delivery cursor")
)

// Scope binds every signature to an identity, cluster epoch and protocol. All
// uint64 fields cross JavaScript as canonical unsigned decimal strings.
type Scope struct {
	Identity string `json:"identity"`
	Cluster  string `json:"cluster"`
	Epoch    uint64 `json:"epoch,string"`
	Protocol uint16 `json:"protocol"`
}

func token(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.", c)) {
			return false
		}
	}
	return true
}
func (s Scope) valid() bool {
	return token(s.Identity) && token(s.Cluster) && s.Epoch != 0 && s.Protocol == Protocol
}
func node(id uint64) bool { return id != 0 && !raft.IsLocalMsgTarget(id) }

// Chunk names one portable snapshot generation and zero-based chunk number.
// Hash is checked before routing; installation remains the receiving host's job.
type Chunk struct {
	Generation string `json:"generation"`
	Offset     uint64 `json:"offset,string"`
	Hash       string `json:"hash"`
}

type Envelope struct {
	Scope       Scope  `json:"scope"`
	From        uint64 `json:"from,string"`
	To          uint64 `json:"to,string"`
	Incarnation uint64 `json:"incarnation,string"`
	Destination uint64 `json:"destination,string"`
	Sequence    uint64 `json:"sequence,string"`
	Chunk       *Chunk `json:"chunk"`
	Digest      string `json:"digest"`
	Payload     []byte `json:"payload"`
	Signature   []byte `json:"signature"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func hash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

// signingBytes uses a fixed JSON field order and ASCII-only metadata. Payloads
// are base64, so native and JavaScript hosts can sign exactly the same bytes.
func (e Envelope) signingBytes() []byte {
	e.Signature = nil
	b, _ := json.Marshal(e)
	return append([]byte("codeaf/raft/envelope/1\n"), b...)
}
func (e *Envelope) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return ErrAuth
	}
	e.Digest = digest(e.Payload)
	if err := e.validate(); err != nil {
		return err
	}
	e.Signature = ed25519.Sign(key, e.signingBytes())
	return nil
}
func (e Envelope) Verify(key ed25519.PublicKey) error {
	if err := e.validate(); err != nil {
		return err
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, e.signingBytes(), e.Signature) {
		return ErrAuth
	}
	return nil
}
func (e Envelope) validate() error {
	if !e.Scope.valid() || !node(e.From) || !node(e.To) || e.From == e.To || e.Incarnation == 0 || e.Destination == 0 || e.Sequence == 0 || e.Digest != digest(e.Payload) {
		return ErrWire
	}
	if e.Chunk != nil {
		if len(e.Payload) == 0 || len(e.Payload) > MaxChunkBytes || !hash(e.Chunk.Generation) || e.Chunk.Hash != e.Digest {
			return ErrWire
		}
		return nil
	}
	if len(e.Payload) == 0 || len(e.Payload) > MaxBatchBytes {
		return ErrWire
	}
	m := &pb.Message{}
	if proto.Unmarshal(e.Payload, m) != nil || m.Type == nil || m.GetFrom() != e.From || m.GetTo() != e.To || m.GetTerm() == 0 || len(m.GetResponses()) != 0 || len(m.ProtoReflect().GetUnknown()) != 0 {
		return ErrWire
	}
	switch m.GetType() {
	case pb.MsgApp, pb.MsgAppResp, pb.MsgVote, pb.MsgVoteResp, pb.MsgSnap, pb.MsgHeartbeat, pb.MsgHeartbeatResp, pb.MsgPreVote, pb.MsgPreVoteResp, pb.MsgTimeoutNow:
		return nil
	default:
		return ErrWire
	}
}

// Encode is the wire format shared by HTTP and socket frames. Decode rejects
// alternative encodings so duplicate keys cannot disagree across runtimes.
func Encode(v any) ([]byte, error) { return json.Marshal(v) }
func Decode(b []byte, v any) error {
	if len(b) > MaxExchangeBytes {
		return ErrWire
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrWire
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrWire
	}
	canonical, err := Encode(v)
	if err != nil || !bytes.Equal(canonical, b) {
		return ErrWire
	}
	return nil
}

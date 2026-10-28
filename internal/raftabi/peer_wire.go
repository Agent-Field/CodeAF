package raftabi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

const peerLaneCount = 3
const (
	peerProtocol       = raftcore.ProtocolVersion
	peerMaxBatchBytes  = raftcore.MaxMessageBytes
	peerMaxChunkBytes  = raftsnapshot.ChunkBytes
	peerMaxInflight    = raftcore.MaxInflightBatches
	peerMaxLearners    = clusterstate.MaxLearners
	peerMaxMessages    = clusterstate.MaxPeerMessages
	peerMaxQueuedBytes = clusterstate.MaxPeerQueuedBytes
	// Encoded envelopes include base64 payloads, signatures and bounded IDs.
	peerMaxExchangeBytes = 2 * peerMaxChunkBytes
)

var (
	peerErrAuth       = errors.New("rafttransport: member authorization refused")
	peerErrWire       = errors.New("rafttransport: invalid peer envelope")
	peerErrGeneration = errors.New("rafttransport: transport generation changed; reconnect")
	peerErrCapacity   = errors.New("rafttransport: transport is busy; retry with backoff")
	peerErrCursor     = errors.New("rafttransport: invalid delivery cursor")
)

// peerScope binds every signature to an identity, cluster epoch and protocol. All
// uint64 fields cross JavaScript as canonical unsigned decimal strings.
type peerScope struct {
	Identity string `json:"identity"`
	Cluster  string `json:"cluster"`
	Epoch    uint64 `json:"epoch,string"`
	Protocol uint16 `json:"protocol"`
}

func peerToken(s string) bool {
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
func (s peerScope) valid() bool {
	return peerToken(s.Identity) && peerToken(s.Cluster) && s.Epoch != 0 && s.Protocol == peerProtocol
}
func peerNode(id uint64) bool { return id != 0 && !raft.IsLocalMsgTarget(id) }

// peerChunk names one portable snapshot generation and zero-based chunk number.
// Hash is checked before routing; installation remains the receiving host's job.
type peerChunk struct {
	Generation string `json:"generation"`
	Offset     uint64 `json:"offset,string"`
	Hash       string `json:"hash"`
}

type peerEnvelope struct {
	Scope       peerScope  `json:"scope"`
	From        uint64     `json:"from,string"`
	To          uint64     `json:"to,string"`
	Incarnation uint64     `json:"incarnation,string"`
	Destination uint64     `json:"destination,string"`
	Sequence    uint64     `json:"sequence,string"`
	Chunk       *peerChunk `json:"chunk"`
	Digest      string     `json:"digest"`
	Payload     []byte     `json:"payload"`
	Signature   []byte     `json:"signature"`
}

func peerDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func peerHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

// signingBytes uses a fixed JSON field order and ASCII-only metadata. Payloads
// are base64, so native and JavaScript hosts can sign exactly the same bytes.
func (e peerEnvelope) signingBytes() []byte {
	e.Signature = nil
	b, _ := json.Marshal(e)
	return append([]byte("codeaf/raft/envelope/1\n"), b...)
}
func (e *peerEnvelope) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return peerErrAuth
	}
	return e.SignWith(func(b []byte) []byte { return ed25519.Sign(key, b) })
}

// SignWith keeps device private keys behind their existing signing boundary.
func (e *peerEnvelope) SignWith(sign func([]byte) []byte) error {
	if sign == nil {
		return peerErrAuth
	}
	e.Digest = peerDigest(e.Payload)
	if err := e.validate(); err != nil {
		return err
	}
	e.Signature = sign(e.signingBytes())
	if len(e.Signature) != ed25519.SignatureSize {
		return peerErrAuth
	}
	return nil
}
func (e peerEnvelope) Verify(key ed25519.PublicKey) error {
	if err := e.validate(); err != nil {
		return err
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, e.signingBytes(), e.Signature) {
		return peerErrAuth
	}
	return nil
}

// AppendChunk carries one framed append whose command already fits the normal
// proposal budget. The payload peerHash as generation distinguishes it from a
// snapshot manifest; both retain the existing chunk byte and queue limits.
func (e peerEnvelope) AppendChunk() bool {
	return e.Chunk != nil && e.Chunk.Offset == 0 && e.Chunk.Generation == e.Chunk.Hash
}

func (e peerEnvelope) validate() error {
	if !e.Scope.valid() || !peerNode(e.From) || !peerNode(e.To) || e.From == e.To || e.Incarnation == 0 || e.Destination == 0 || e.Sequence == 0 || e.Digest != peerDigest(e.Payload) {
		return peerErrWire
	}
	if e.Chunk != nil {
		if len(e.Payload) == 0 || len(e.Payload) > peerMaxChunkBytes || !peerHash(e.Chunk.Generation) || e.Chunk.Hash != e.Digest {
			return peerErrWire
		}
		if !e.AppendChunk() {
			return nil
		}
		if len(e.Payload) <= peerMaxBatchBytes {
			return peerErrWire
		}
	} else if len(e.Payload) == 0 || len(e.Payload) > peerMaxBatchBytes {
		return peerErrWire
	}
	m := &pb.Message{}
	if proto.Unmarshal(e.Payload, m) != nil || m.Type == nil || m.GetFrom() != e.From || m.GetTo() != e.To || m.GetTerm() == 0 || len(m.GetResponses()) != 0 || len(m.ProtoReflect().GetUnknown()) != 0 {
		return peerErrWire
	}
	if e.AppendChunk() && m.GetType() != pb.MsgApp {
		return peerErrWire
	}
	switch m.GetType() {
	case pb.MsgApp, pb.MsgAppResp, pb.MsgVote, pb.MsgVoteResp, pb.MsgSnap, pb.MsgHeartbeat, pb.MsgHeartbeatResp, pb.MsgPreVote, pb.MsgPreVoteResp, pb.MsgTimeoutNow:
		return nil
	default:
		return peerErrWire
	}
}

// peerEncode is the wire format shared by HTTP and socket frames. peerDecode rejects
// alternative encodings so duplicate keys cannot disagree across runtimes.
func peerEncode(v any) ([]byte, error) { return json.Marshal(v) }
func peerDecode(b []byte, v any) error {
	if len(b) > peerMaxExchangeBytes {
		return peerErrWire
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return peerErrWire
	}
	if d.Decode(new(any)) != io.EOF {
		return peerErrWire
	}
	canonical, err := peerEncode(v)
	if err != nil || !bytes.Equal(canonical, b) {
		return peerErrWire
	}
	return nil
}

// peerRequest signs polls and acknowledgments as well as outbound digests. A
// stolen unsigned cursor must not allow a third party to drain a peer's inbox.
type peerRequest struct {
	// Applied is signed evidence from the member after its application transaction.
	// A transport acknowledgment or leader Match alone never makes it ready.
	Applied     uint64                `json:"applied,string,omitempty"`
	Scope       peerScope             `json:"scope"`
	Node        uint64                `json:"node,string"`
	Incarnation uint64                `json:"incarnation,string"`
	Generation  uint64                `json:"generation,string"`
	Ack         [peerLaneCount]uint64 `json:"ack"`
	Messages    []peerEnvelope        `json:"messages"`
	Signature   []byte                `json:"signature"`
}

func peerDecimal(n uint64) string { return strconv.FormatUint(n, 10) }
func peerParseDecimal(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || peerDecimal(n) != s {
		return 0, peerErrWire
	}
	return n, nil
}

// Ack is encoded through peerDecimal strings even though it is a fixed array.
func (r peerRequest) MarshalJSON() ([]byte, error) {
	type raw peerRequest
	return json.Marshal(struct {
		raw
		Ack [peerLaneCount]string `json:"ack"`
	}{raw(r), [peerLaneCount]string{peerDecimal(r.Ack[0]), peerDecimal(r.Ack[1]), peerDecimal(r.Ack[2])}})
}
func (r *peerRequest) UnmarshalJSON(b []byte) error {
	type raw peerRequest
	var v struct {
		raw
		Ack [peerLaneCount]string `json:"ack"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil {
		return peerErrWire
	}
	*r = peerRequest(v.raw)
	for i, s := range v.Ack {
		n, err := peerParseDecimal(s)
		if err != nil {
			return err
		}
		r.Ack[i] = n
	}
	return nil
}
func (r peerRequest) signingBytes() []byte {
	r.Signature = nil
	b, _ := peerEncode(r)
	return append([]byte("codeaf/raft/exchange/1\n"), b...)
}
func (r *peerRequest) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return peerErrAuth
	}
	return r.SignWith(func(b []byte) []byte { return ed25519.Sign(key, b) })
}

// SignWith uses the same canonical bytes without exporting a device seed.
func (r *peerRequest) SignWith(sign func([]byte) []byte) error {
	if sign == nil {
		return peerErrAuth
	}
	if err := r.validate(); err != nil {
		return err
	}
	r.Signature = sign(r.signingBytes())
	if len(r.Signature) != ed25519.SignatureSize {
		return peerErrAuth
	}
	return nil
}
func (r peerRequest) Verify(key ed25519.PublicKey) error {
	if err := r.validate(); err != nil {
		return err
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, r.signingBytes(), r.Signature) {
		return peerErrAuth
	}
	return nil
}
func (r peerRequest) validate() error {
	if !r.Scope.valid() || !peerNode(r.Node) || r.Incarnation == 0 || r.Generation == 0 || len(r.Messages) > peerMaxMessages {
		return peerErrWire
	}
	normal, chunks := 0, 0
	for _, e := range r.Messages {
		if e.Chunk != nil {
			chunks++
		} else {
			normal += len(e.Payload)
		}
	}
	if normal > peerMaxBatchBytes || chunks > 1 || chunks != 0 && len(r.Messages) != 1 {
		return peerErrWire
	}
	b, err := peerEncode(r)
	if err != nil || len(b) > peerMaxExchangeBytes {
		return peerErrWire
	}
	return nil
}

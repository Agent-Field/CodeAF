package clusterstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// MaxV1BodyBytes preserves the legacy HTTP envelope independently of the
// smaller consensus command envelope. Staging reuses a single bounded slot.
const MaxV1BodyBytes = 1 << 20
const V1PageBytes = MaxCommandBytes / 2
const MaxV1StagingBytes = MaxV1BodyBytes + MaxCommandBytes
const V1StagePages = (MaxV1BodyBytes + V1PageBytes - 1) / V1PageBytes

type V1Stage struct {
	Hash   string `json:"hash"`
	Total  uint32 `json:"total"`
	Offset uint32 `json:"offset"`
	Data   []byte `json:"data"`
	Commit bool   `json:"commit,omitempty"`
}
type v1Staging struct {
	ID   string `json:"id"`
	Next uint32 `json:"next"`
}

func V1StageKey() RecordKey { return RecordKey{"outbox", "v1-stage/header"} }
func V1PageKey(page int) RecordKey {
	return RecordKey{"outbox", fmt.Sprintf("v1-stage/page/%02d", page)}
}

// V1StagingKeys fixes the temporary slot's complete physical extent. Retained
// capacity excludes this bounded command bookkeeping, including in snapshots.
func V1StagingKeys() []RecordKey {
	keys := []RecordKey{V1StageKey()}
	for page := 0; page < V1StagePages; page++ {
		keys = append(keys, V1PageKey(page))
	}
	return keys
}

// V1StagingBytes derives temporary accounting from immutable records rather
// than maintaining a second durable counter that could disagree after a crash.
func V1StagingBytes(read ReadRecord) (uint64, error) {
	var size uint64
	for i, key := range V1StagingKeys() {
		raw, err := read(key)
		if err != nil {
			return 0, err
		}
		if i == 0 && raw == nil {
			return 0, nil
		}
		limit := V1PageBytes
		if i == 0 {
			limit = MaxCommandBytes
		}
		if len(raw) > limit {
			return 0, ErrDependency
		}
		if raw != nil {
			size += RecordBytes(key, raw)
		}
	}
	if size > MaxV1StagingBytes {
		return 0, ErrDependency
	}
	return size, nil
}

// V1RequestCommands translates a legacy body directly to supported proposals.
// A proof that cannot fit one command never allocates that unused command's
// expanded digest representation before producing its bounded body pages.
func V1RequestCommands(cluster string, now int64, proof V1Request) ([]Command, error) {
	c, err := translateV1(cluster, now, proof)
	if err != nil {
		return nil, err
	}
	if base64.StdEncoding.EncodedLen(len(proof.Body)) <= MaxCommandBytes {
		c.RequestDigest, err = Digest(c)
		if err != nil {
			return nil, err
		}
	}
	return V1Commands(c)
}

// V1Commands preserves the original proof while each proposal remains within
// the command envelope. Only the final proposal can change directory records.
func V1Commands(c Command) ([]Command, error) {
	var err error
	// The proof's base64 body alone can establish that a command cannot fit.
	// Avoid materializing an oversized duplicate of the decoded mutation.
	if c.V1 == nil || base64.StdEncoding.EncodedLen(len(c.V1.Body)) <= MaxCommandBytes {
		raw, e := EncodeCommand(c)
		if e != nil {
			return nil, e
		}
		if len(raw) <= MaxCommandBytes {
			return []Command{c}, nil
		}
	}
	if c.V1 == nil || len(c.V1.Body) > MaxV1BodyBytes || c.Change.StageV1 != nil {
		return nil, ErrCodec
	}
	proof := *c.V1
	body := proof.Body
	proof.Body = nil
	hash := sha256.Sum256(body)
	part := func(offset int, data []byte, commit bool) (Command, error) {
		v := c
		v.V1 = &proof
		v.Change = Mutation{StageV1: &V1Stage{Hash: hex.EncodeToString(hash[:]), Total: uint32(len(body)), Offset: uint32(offset), Data: bytes.Clone(data), Commit: commit}}
		v.RequestDigest, err = Digest(v)
		if err != nil {
			return Command{}, err
		}
		b, e := EncodeCommand(v)
		if e != nil || len(b) > MaxCommandBytes {
			return Command{}, ErrCodec
		}
		return v, nil
	}
	out := []Command{}
	for offset := 0; offset < len(body); offset += V1PageBytes {
		v, e := part(offset, body[offset:min(len(body), offset+V1PageBytes)], false)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	v, e := part(len(body), nil, true)
	if e != nil {
		return nil, e
	}
	return append(out, v), nil
}
func v1StageID(c Command) string {
	v := c.Change.StageV1
	raw, _ := Encode(struct {
		Proof *V1Request `json:"proof"`
		Hash  string     `json:"hash"`
		Total uint32     `json:"total"`
	}{c.V1, v.Hash, v.Total})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func evaluateV1Stage(c Command, meta ApplicationMeta, read ReadRecord) (Evaluation, error) {
	out := Evaluation{Now: meta.Now, Version: meta.Version, Writes: []RecordWrite{}, Result: Result{Schema: SchemaVersion, Code: Unauthorized, Now: meta.Now, Version: meta.Version}}
	v := c.Change.StageV1
	hash, err := hex.DecodeString(v.Hash)
	digest, e := Digest(c)
	if err != nil || len(hash) != sha256.Size || e != nil || digest != c.RequestDigest || c.Cluster != meta.Cluster || c.V1 == nil || len(c.V1.Body) != 0 || v.Total > MaxV1BodyBytes || v.Total == 0 || v.Offset > v.Total || c.Certificate != "" {
		return out, nil
	}
	var identity IdentityRec
	raw, err := read(IdentityKey())
	if err != nil {
		return out, err
	}
	if Decode(raw, &identity) != nil {
		return out, ErrDependency
	}
	if _, ok := v1ProofAuthority(c, identity, meta.Now, v.Hash); !ok {
		return out, nil
	}
	raw, err = read(DeviceKey(c.Actor))
	if err != nil {
		return out, err
	}
	if raw != nil {
		var device Device
		if Decode(raw, &device) != nil {
			return out, ErrDependency
		}
		if device.Revoked {
			out.Result.Code = Revoked
			return out, nil
		}
	}
	if identity.Rotation != nil && !(c.V1.Method == "POST" && strings.TrimPrefix(c.V1.URI, "/fabric") == "/v1/identity/rotation") {
		out.Result.Code = Rotated
		return out, nil
	}
	raw, err = read(V1StageKey())
	if err != nil {
		return out, err
	}
	var stage v1Staging
	if raw != nil && Decode(raw, &stage) != nil {
		return out, ErrDependency
	}
	id := v1StageID(c)
	conflict := func() (Evaluation, error) { out.Result.Code = Conflict; return out, nil }
	if !v.Commit {
		if v.Offset%V1PageBytes != 0 || len(v.Data) != min(V1PageBytes, int(v.Total-v.Offset)) || len(v.Data) == 0 {
			return out, nil
		}
		if v.Offset != 0 && (stage.ID != id || stage.Next != v.Offset) {
			return conflict()
		}
		stage = v1Staging{ID: id, Next: v.Offset + uint32(len(v.Data))}
		header, _ := Encode(stage)
		writes := []RecordWrite{{RecordKey: V1StageKey(), Data: header}, {RecordKey: V1PageKey(int(v.Offset) / V1PageBytes), Data: bytes.Clone(v.Data)}}
		out.Result.Code = OK
		out.Writes = writes
		return out, nil
	}
	if len(v.Data) != 0 || v.Offset != v.Total || stage.ID != id || stage.Next != v.Total {
		return conflict()
	}
	body := make([]byte, 0, int(v.Total))
	scratch := RecordBytes(V1StageKey(), raw)
	cleanup := []RecordWrite{{RecordKey: V1StageKey(), Delete: true}}
	for page := 0; page < V1StagePages; page++ {
		key := V1PageKey(page)
		b, err := read(key)
		if err != nil {
			return out, err
		}
		if len(b) > V1PageBytes {
			return out, ErrDependency
		}
		if b != nil {
			scratch += RecordBytes(key, b)
			cleanup = append(cleanup, RecordWrite{RecordKey: key, Delete: true})
		}
		if page*V1PageBytes < int(v.Total) {
			want := min(V1PageBytes, int(v.Total)-page*V1PageBytes)
			if len(b) != want {
				return conflict()
			}
			body = append(body, b...)
		}
	}
	actual := sha256.Sum256(body)
	if !bytes.Equal(actual[:], hash) {
		return conflict()
	}
	proof := *c.V1
	proof.Body = body
	original, err := translateV1(c.Cluster, c.Now, proof)
	if err != nil {
		out.Result.Code = BadRequest
		out.Writes = cleanup
		return out, nil
	}
	// Resolve immutable dependencies before computing the expanded operation
	// digest. A paged host may retry for each missing record; those retries must
	// not retain several unused expanded command representations.
	for _, key := range Dependencies(original) {
		if _, err := read(key); err != nil {
			return out, err
		}
	}
	original.RequestDigest, err = Digest(original)
	if err != nil {
		return out, err
	}
	if scratch > meta.Bytes {
		return out, ErrDependency
	}
	meta.Bytes -= scratch
	evaluated, err := evaluateCommand(original, meta, read, true)
	if err != nil {
		return out, err
	}
	evaluated.Writes = append(evaluated.Writes, cleanup...)
	return evaluated, nil
}

// BoundedV1Result forwards a durable apply receipt when the original view
// cannot fit a normal response. The ingress resolves the exact result from its
// own application of that same entry, rather than reading a newer cell.
func BoundedV1Result(command []byte, result Result, index uint64) Result {
	c, err := DecodeCommand(command)
	if err != nil || c.V1 == nil || result.View == nil || index == 0 {
		return result
	}
	raw, err := EncodeResult(result)
	if err == nil && len(raw) > MaxCommandBytes {
		result.View = nil
		result.ViewIndex = index
	}
	return result
}

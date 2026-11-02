package clusterstate_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func largeV1(t *testing.T) (cs.Command, map[cs.RecordKey][]byte, cs.ApplicationMeta) {
	t.Helper()
	raw, err := os.ReadFile("../raftdirectory/testdata/v1-large-requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Now   int64
		Cases []struct {
			Method, URI, Body string
			Headers           map[string]string
		}
	}
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixture decode")
	}
	one := f.Cases[1]
	h := one.Headers
	root, err := base64.RawURLEncoding.DecodeString(h["Codeaf-Identity"])
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(h["Codeaf-Sig"])
	if err != nil {
		t.Fatal(err)
	}
	c, err := cs.V1Command("large", f.Now, cs.V1Request{Method: one.Method, URI: one.URI, Body: []byte(one.Body), At: h["Codeaf-Time"], Certificate: h["Codeaf-Cert"], IdentityKey: root, Signature: sig})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := cs.Encode(cs.IdentityRec{V: 1, Identity: cs.IdentityID(root), SigningKey: root})
	if err != nil {
		t.Fatal(err)
	}
	rows := map[cs.RecordKey][]byte{cs.IdentityKey(): identity}
	meta := cs.ApplicationMeta{Cluster: "large", Now: f.Now, Ceiling: cs.DefaultApplicationBytes, Bytes: cs.RecordBytes(cs.IdentityKey(), identity)}
	return c, rows, meta
}
func applyV1Page(t *testing.T, c cs.Command, rows map[cs.RecordKey][]byte, meta *cs.ApplicationMeta) cs.Result {
	t.Helper()
	raw, err := cs.EncodeCommand(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > cs.MaxCommandBytes {
		t.Fatal("proposal exceeds command cap")
	}
	out, err := cs.Evaluate(raw, *meta, func(k cs.RecordKey) ([]byte, error) { return rows[k], nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range out.Writes {
		if old := rows[w.RecordKey]; old != nil {
			meta.Bytes -= cs.RecordBytes(w.RecordKey, old)
		}
		if w.Delete {
			delete(rows, w.RecordKey)
		} else {
			if len(w.Data) > cs.MaxEvaluationBytes {
				t.Fatal("physical record exceeds evaluation cap")
			}
			rows[w.RecordKey] = w.Data
			meta.Bytes += cs.RecordBytes(w.RecordKey, w.Data)
		}
	}
	meta.Now, meta.Version = out.Now, out.Version
	meta.Index++
	return out.Result
}

// R25/R16 require the complete original proof at final commitment, including
// unchanged time, current revocation and ordered, hash-matching body pages.
func TestR25V1StagingProofAndAtomicity(t *testing.T) {
	for _, fault := range []string{"none", "changed-page", "missing-page", "reordered", "stale-proof", "revoked", "wrong-cluster", "wrong-root", "public-v2"} {
		t.Run(fault, func(t *testing.T) {
			c, rows, meta := largeV1(t)
			parts, err := cs.V1Commands(c)
			if err != nil {
				t.Fatal(err)
			}
			if cs.ClientCommand(parts[0]) == nil {
				t.Fatal("public v2 accepted staging")
			}
			if fault == "public-v2" {
				return
			}
			for i, part := range parts[:len(parts)-1] {
				if fault == "missing-page" && i == len(parts)-2 {
					continue
				}
				if fault == "reordered" && i == 1 {
					continue
				}
				if fault == "changed-page" && i == 1 {
					part.Change.StageV1.Data = bytes.Clone(part.Change.StageV1.Data)
					part.Change.StageV1.Data[0] ^= 1
					part.RequestDigest, _ = cs.Digest(part)
				}
				r := applyV1Page(t, part, rows, &meta)
				if fault != "reordered" && r.Code != cs.OK {
					t.Fatal(r.Code)
				}
				if rows[cs.CellKey(c.Change.Create.Cell)] != nil || meta.Version != 0 {
					t.Fatal("partial request became visible")
				}
			}
			switch fault {
			case "stale-proof":
				meta.Now += cs.V1SkewMillis + 1
			case "revoked":
				b, _ := cs.Encode(cs.Device{V: 1, Revoked: true})
				rows[cs.DeviceKey(c.Actor)] = b
				meta.Bytes += cs.RecordBytes(cs.DeviceKey(c.Actor), b)
			case "wrong-cluster":
				meta.Cluster = "other"
			case "wrong-root":
				seed := sha256.Sum256([]byte("different root"))
				root := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
				b, _ := cs.Encode(cs.IdentityRec{V: 1, Identity: cs.IdentityID(root), SigningKey: root})
				meta.Bytes -= cs.RecordBytes(cs.IdentityKey(), rows[cs.IdentityKey()])
				rows[cs.IdentityKey()] = b
				meta.Bytes += cs.RecordBytes(cs.IdentityKey(), b)
			}
			r := applyV1Page(t, parts[len(parts)-1], rows, &meta)
			if fault == "none" {
				if r.Code != cs.OK {
					t.Fatal(r.Code)
				}
				var cell cs.Cell
				if cs.Decode(rows[cs.CellKey(c.Change.Create.Cell)], &cell) != nil || cell.Title != c.Change.Create.Init.Title || cell.Head != c.Change.Create.Init.Head || cell.Lease.Fence != 1 || !reflect.DeepEqual(cell.Frames, c.Change.Create.Init.Frames) || !reflect.DeepEqual(cell.Keys, c.Change.Create.Init.Keys) {
					t.Fatal("large request lost fields")
				}
				if meta.Version != 1 {
					t.Fatal("final request did not publish exactly once")
				}
				for k := range rows {
					if k == cs.V1StageKey() || bytes.HasPrefix([]byte(k.Key), []byte("v1-stage/page/")) {
						t.Fatal("scratch survived commit")
					}
				}
			} else if r.Code == cs.OK || rows[cs.CellKey(c.Change.Create.Cell)] != nil {
				t.Fatal("invalid staged proof changed directory")
			}
		})
	}
}

// R25 keeps the old HTTP refusal boundary rather than raising a command cap.
func TestR25V1BodyLimitStillRefusesOversize(t *testing.T) {
	c, _, _ := largeV1(t)
	p := *c.V1
	p.Body = append(bytes.Clone(p.Body), []byte("  ")...)
	if len(p.Body) <= cs.MaxV1BodyBytes {
		t.Fatal("fixture not above old limit")
	}
	if _, err := cs.V1Command(c.Cluster, c.Now, p); err == nil {
		t.Fatal("request above old limit admitted")
	}
}

// R25/R27 keep retained application growth a refusal without losing records.
func TestR25V1StagingCapacityPreservesRecords(t *testing.T) {
	c, rows, meta := largeV1(t)
	parts, err := cs.V1Commands(c)
	if err != nil {
		t.Fatal(err)
	}
	meta.Ceiling = meta.Bytes
	for _, part := range parts[:len(parts)-1] {
		if r := applyV1Page(t, part, rows, &meta); r.Code != cs.OK {
			t.Fatal(r.Code)
		}
	}
	r := applyV1Page(t, parts[len(parts)-1], rows, &meta)
	if r.Code != cs.Capacity || len(rows) != 1 || rows[cs.V1StageKey()] != nil {
		t.Fatal("capacity changed committed records or retained temporary pages")
	}
}

// R25/R21 preserve legal rotation requests even while ordinary writes freeze.
func TestR25V1StagingAllowsLargeThaw(t *testing.T) {
	c, rows, meta := largeV1(t)
	var identity cs.IdentityRec
	if cs.Decode(rows[cs.IdentityKey()], &identity) != nil {
		t.Fatal("identity")
	}
	identity.Rotation = &cs.Rotation{State: "frozen", By: c.Actor, At: c.Now}
	raw, _ := cs.Encode(identity)
	meta.Bytes -= cs.RecordBytes(cs.IdentityKey(), rows[cs.IdentityKey()])
	rows[cs.IdentityKey()] = raw
	meta.Bytes += cs.RecordBytes(cs.IdentityKey(), raw)
	proof := *c.V1
	proof.URI = "/v1/identity/rotation"
	proof.Body = append([]byte(`{"op":"thaw"}`), bytes.Repeat([]byte(" "), cs.MaxV1BodyBytes-14)...)
	seed := sha256.Sum256([]byte("p17-a"))
	proof.Signature = ed25519.Sign(ed25519.NewKeyFromSeed(seed[:]), cs.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
	c, err := cs.V1Command(c.Cluster, c.Now, proof)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := cs.V1Commands(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if r := applyV1Page(t, part, rows, &meta); r.Code != cs.OK {
			t.Fatal(r.Code)
		}
	}
	if cs.Decode(rows[cs.IdentityKey()], &identity) != nil || identity.Rotation != nil {
		t.Fatal("legal thaw did not commit")
	}
}

// R25/R14 preserve every logical field while malformed compressed storage
// remains an error, rather than a missing record or a successful empty result.
func TestR25LargeResultRepresentationIsLossless(t *testing.T) {
	want := cs.Result{Schema: cs.SchemaVersion, Code: cs.OK, Version: ^uint64(0), View: &cs.CellView{Cell: cs.Cell{Lease: cs.Lease{Fence: ^uint64(0)}, Keys: map[string]map[string]string{"wrapped": {"fence": "0001", "size": "1e3"}}, Title: string(bytes.Repeat([]byte("<"), cs.MaxV1BodyBytes-1024))}}}
	raw, err := cs.EncodeApplicationResult(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > cs.MaxEvaluationBytes {
		t.Fatal("physical result exceeds existing envelope")
	}
	var got cs.Result
	if cs.Decode(raw, &got) != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("logical result was truncated")
	}
	for _, bad := range [][]byte{raw[:len(raw)-1], append(bytes.Clone(raw), 1), func() []byte { b := bytes.Clone(raw); b[len(b)-5] ^= 1; return b }()} {
		if cs.Decode(bad, &got) == nil {
			t.Fatal("corrupt compressed result accepted")
		}
	}
}

// R25/R27 keep a retained physical row at the legacy limit usable through
// paged evaluation, without dropping its opaque metadata or raising the limit.
func TestR25PagedV1UpdatesLegacyLimitRow(t *testing.T) {
	c, rows, meta := largeV1(t)
	cell := cs.Cell{V: 1, Head: "saved", Lease: cs.Lease{Device: c.Actor, Fence: 7, Expires: c.Now + cs.LeaseTTLMillis}, Title: "x"}
	raw, _ := cs.Encode(cell)
	cell.Title = string(bytes.Repeat([]byte("e"), cs.MaxEvaluationBytes-len(raw)+1))
	raw, _ = cs.Encode(cell)
	if len(raw) != cs.MaxEvaluationBytes {
		t.Fatal("retained row is not at limit")
	}
	k := cs.CellKey(c.Change.Create.Cell)
	rows[k] = raw
	meta.Bytes += cs.RecordBytes(k, raw)
	proof := *c.V1
	proof.URI += "/acquire"
	proof.Body = append([]byte(`{}`), bytes.Repeat([]byte(" "), 65535)...)
	seed := sha256.Sum256([]byte("p17-a"))
	proof.Signature = ed25519.Sign(ed25519.NewKeyFromSeed(seed[:]), cs.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
	c, err := cs.V1Command(c.Cluster, c.Now, proof)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := cs.V1Commands(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if r := applyV1Page(t, part, rows, &meta); r.Code != cs.OK {
			t.Fatal(r.Code)
		}
	}
	var got cs.Cell
	if cs.Decode(rows[k], &got) != nil || got.Title != cell.Title || got.Head != cell.Head || got.Lease.Fence != 8 {
		t.Fatal("retained row was lost or stranded")
	}
}

// R25/R6 keep large original views off the bounded forwarding wire. The
// receipt names the exact durable application result, never a newer cell read.
func TestR25ForwardedLargeViewsUseAppliedReceipts(t *testing.T) {
	c, _, _ := largeV1(t)
	parts, err := cs.V1Commands(c)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cs.EncodeCommand(parts[len(parts)-1])
	if err != nil {
		t.Fatal(err)
	}
	original := cs.Result{Schema: cs.SchemaVersion, Code: cs.OK, Now: c.Now, Version: 7, View: &cs.CellView{Now: c.Now, Cell: cs.Created(c.Change.Create.Init, c.Actor, c.Now)}}
	receipt := cs.BoundedV1Result(raw, original, 42)
	wire, err := cs.EncodeResult(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.View != nil || receipt.ViewIndex != 42 || receipt.Code != original.Code || receipt.Now != original.Now || receipt.Version != original.Version || len(wire) > cs.MaxCommandBytes {
		t.Fatal("large view exceeded the forwarding envelope or lost its receipt")
	}
	if original.View == nil || !reflect.DeepEqual(original.View.Cell.Frames, c.Change.Create.Init.Frames) {
		t.Fatal("receipt creation changed the original result")
	}
	small := cs.Result{Schema: cs.SchemaVersion, Code: cs.OK, View: &cs.CellView{Cell: cs.Cell{Head: "saved"}}}
	kept := cs.BoundedV1Result(raw, small, 43)
	if kept.View == nil || kept.ViewIndex != 0 {
		t.Fatal("small result no longer crosses directly")
	}
	receipt.View = original.View
	if _, err = cs.EncodeResult(receipt); err == nil {
		t.Fatal("ambiguous full view and receipt accepted")
	}
}

// R25/R27 allow a same-size or shrinking legacy update at the retained ceiling.
// Temporary command staging cannot consume the capacity of existing records.
func TestR25V1StagingAtRetainedCapacity(t *testing.T) {
	c, rows, meta := largeV1(t)
	init := c.Change.Create.Init
	init.Frames = init.Frames[:len(init.Frames)-8]
	cell := cs.Created(init, c.Actor, c.Now)
	raw, err := cs.Encode(cell)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > cs.MaxEvaluationBytes {
		t.Fatal("legacy row exceeds old limit")
	}
	k := cs.CellKey(c.Change.Create.Cell)
	rows[k] = raw
	meta.Bytes += cs.RecordBytes(k, raw)
	meta.Ceiling = meta.Bytes
	proof := *c.V1
	proof.URI += "/acquire"
	proof.Body = append([]byte(`{}`), bytes.Repeat([]byte(" "), cs.MaxV1BodyBytes-3)...)
	seed := sha256.Sum256([]byte("p17-a"))
	proof.Signature = ed25519.Sign(ed25519.NewKeyFromSeed(seed[:]), cs.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
	c, err = cs.V1Command(c.Cluster, c.Now, proof)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := cs.V1Commands(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if r := applyV1Page(t, part, rows, &meta); r.Code != cs.OK {
			t.Fatalf("legal update at retained ceiling: %s", r.Code)
		}
	}
	var got cs.Cell
	if cs.Decode(rows[k], &got) != nil || got.Lease.Fence != 2 || !reflect.DeepEqual(got.Frames, cell.Frames) || meta.Bytes > meta.Ceiling {
		t.Fatal("retained update lost metadata or raised its ceiling")
	}
}

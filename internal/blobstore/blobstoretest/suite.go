// Package blobstoretest is the conformance suite every blobstore.Store must
// pass. The fake and each real store run the same cases, so the fake can stand
// in for any of them in other packages' tests without changing what they prove.
package blobstoretest

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// Factory returns a fresh, empty store for one test.
type Factory func(t *testing.T) blobstore.Store

// Run runs the whole suite against stores made by factory.
func Run(t *testing.T, factory Factory) {
	cases := map[string]func(*testing.T, blobstore.Store){
		"round trip":             roundTrip,
		"put answers frame id":   putAnswersFrameID,
		"idempotent put":         idempotentPut,
		"same object two frames": sameObjectTwoFrames,
		"conflict":               conflict,
		"ConflictStoresNothing":  conflictStoresNone,
		"FullLeavesNoPointer":    fullLeavesNoPointer,
		"not found":              notFound,
		"bad rid":                badRID,
		"GetManyInOrder":         getManyInOrder,
		"GetManyStopsAtAbsent":   getManyStopsAtAbsent,
		"GetManyFirstAbsent":     getManyFirstAbsent,
		"GetManyFitsOneFrame":    getManyFitsOneFrame,
		"GetManyRefusals":        getManyRefusals,
		"has order":              hasOrder,
		"has bound":              hasBound,
		"bad frames":             badFrames,
		"RefusesPlaintext":       plaintextRefused,
		"vault object accepted":  vaultObjectAccepted,
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) { run(t, factory(t)) })
	}
}

var ctx = context.Background()

func roundTrip(t *testing.T, s blobstore.Store) {
	a, b := object("a"), object("b")
	put(t, s, frameOf(t, a, b))
	for _, o := range []blobstore.Object{a, b} {
		got, err := s.Get(ctx, o.RID)
		if err != nil || string(got) != string(o.Bytes) {
			t.Fatalf("Get(%s) = %q, %v; want %q", o.RID[:8], got, err, o.Bytes)
		}
	}
}

func putAnswersFrameID(t *testing.T, s blobstore.Store) {
	frame := frameOf(t, object("a"))
	if got := put(t, s, frame); got != blobstore.IDOf(frame) {
		t.Fatalf("PutFrame id = %s, want %s", got, blobstore.IDOf(frame))
	}
}

func idempotentPut(t *testing.T, s blobstore.Store) {
	frame := frameOf(t, object("a"), object("b"))
	first := put(t, s, frame)
	if second := put(t, s, frame); second != first {
		t.Fatalf("second put answered %s, first %s", second, first)
	}
}

func sameObjectTwoFrames(t *testing.T, s blobstore.Store) {
	a := object("a")
	put(t, s, frameOf(t, a))
	put(t, s, frameOf(t, a, object("b")))
	if got, err := s.Get(ctx, a.RID); err != nil || string(got) != string(a.Bytes) {
		t.Fatalf("Get after re-put = %q, %v", got, err)
	}
}

func conflict(t *testing.T, s blobstore.Store) {
	a := object("a")
	put(t, s, frameOf(t, a))
	other := blobstore.Object{RID: a.RID, Bytes: sealed("different")}
	if _, err := s.PutFrame(ctx, frameOf(t, other)); !errors.Is(err, blobstore.ErrConflict) {
		t.Fatalf("PutFrame with the same rid and other bytes = %v, want ErrConflict", err)
	}
	if got, _ := s.Get(ctx, a.RID); string(got) != string(a.Bytes) {
		t.Fatalf("a conflicting put changed the stored bytes to %q", got)
	}
}

// A frame is all or nothing: one conflicting object must keep the frame's
// other objects out, or a refused frame would still leave part of itself behind.
func conflictStoresNone(t *testing.T, s blobstore.Store) {
	a, fresh := object("a"), object("fresh")
	put(t, s, frameOf(t, a))
	other := blobstore.Object{RID: a.RID, Bytes: sealed("different")}
	if _, err := s.PutFrame(ctx, frameOf(t, fresh, other)); !errors.Is(err, blobstore.ErrConflict) {
		t.Fatalf("PutFrame = %v, want ErrConflict", err)
	}
	if _, err := s.Get(ctx, fresh.RID); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("Get of an object from a refused frame = %v, want ErrNotFound", err)
	}
}

// failer is what a fake offers so the suite can make it report a full store;
// a real store cannot be filled on demand and has its own test instead.
type failer interface{ FailAfter(n int, err error) }

func fullLeavesNoPointer(t *testing.T, s blobstore.Store) {
	f, ok := s.(failer)
	if !ok {
		t.Skip("store cannot be told to fail; its own test covers a full store")
	}
	a := object("a")
	f.FailAfter(0, blobstore.ErrFull)
	if _, err := s.PutFrame(ctx, frameOf(t, a)); !errors.Is(err, blobstore.ErrFull) {
		t.Fatalf("PutFrame on a full store = %v, want ErrFull", err)
	}
	if _, err := s.Get(ctx, a.RID); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("Get after a full store refused the put = %v, want ErrNotFound", err)
	}
	put(t, s, frameOf(t, a)) // the failure was one call; the store carries on
}

func notFound(t *testing.T, s blobstore.Store) {
	if _, err := s.Get(ctx, object("absent").RID); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("Get of an absent object = %v, want ErrNotFound", err)
	}
}

func badRID(t *testing.T, s blobstore.Store) {
	for _, rid := range []string{"", "abc", strings.Repeat("A", 64), strings.Repeat("g", 64), "../" + strings.Repeat("a", 61)} {
		if _, err := s.Get(ctx, rid); !errors.Is(err, blobstore.ErrBadRID) {
			t.Errorf("Get(%q) = %v, want ErrBadRID", rid, err)
		}
		if _, err := s.Has(ctx, []string{rid}); !errors.Is(err, blobstore.ErrBadRID) {
			t.Errorf("Has(%q) = %v, want ErrBadRID", rid, err)
		}
	}
}

// getManyInOrder: every object asked for comes back in the order asked, which is
// not the order the store holds them in.
func getManyInOrder(t *testing.T, s blobstore.Store) {
	a, b, c := object("a"), object("b"), object("c")
	put(t, s, frameOf(t, a, b))
	put(t, s, frameOf(t, c))
	got, err := s.GetMany(ctx, []string{c.RID, a.RID, b.RID})
	must(t, err)
	wantObjects(t, got, c, a, b)
}

// getManyStopsAtAbsent: an absent object ends the answer, with what came before
// it, so the caller learns which rid is missing by asking for the rest.
func getManyStopsAtAbsent(t *testing.T, s blobstore.Store) {
	a, b := object("a"), object("b")
	put(t, s, frameOf(t, a, b))
	got, err := s.GetMany(ctx, []string{a.RID, object("absent").RID, b.RID})
	must(t, err)
	wantObjects(t, got, a)
}

func getManyFirstAbsent(t *testing.T, s blobstore.Store) {
	if _, err := s.GetMany(ctx, []string{object("absent").RID, object("a").RID}); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("GetMany with the first object absent = %v, want ErrNotFound", err)
	}
}

// getManyFitsOneFrame: the answer is about a frame's worth of bytes, so a caller
// that asked for more asks again, and the pieces add up to what it asked for.
func getManyFitsOneFrame(t *testing.T, s blobstore.Store) {
	big := make([]blobstore.Object, 4)
	rids := make([]string, len(big))
	for i := range big {
		big[i] = blobstore.Object{RID: object(string(rune('p' + i))).RID, Bytes: sealed(strings.Repeat("x", blobstore.TargetFrame/3))}
		rids[i] = big[i].RID
	}
	put(t, s, frameOf(t, big...))
	first, err := s.GetMany(ctx, rids)
	must(t, err)
	if len(first) == 0 || len(first) >= len(big) {
		t.Fatalf("GetMany of four thirds of a frame answered %d objects, want a proper prefix", len(first))
	}
	rest, err := s.GetMany(ctx, rids[len(first):])
	must(t, err)
	got := append(first, rest...)
	wantObjects(t, got, big[:len(got)]...)
}

func getManyRefusals(t *testing.T, s blobstore.Store) {
	rids := make([]string, blobstore.MaxGetMany+1)
	for i := range rids {
		rids[i] = object(strings.Repeat("y", i+1)).RID
	}
	if _, err := s.GetMany(ctx, rids); !errors.Is(err, blobstore.ErrTooMany) {
		t.Errorf("GetMany of MaxGetMany+1 ids = %v, want ErrTooMany", err)
	}
	if _, err := s.GetMany(ctx, []string{object("a").RID, "abc"}); !errors.Is(err, blobstore.ErrBadRID) {
		t.Errorf("GetMany with a bad id = %v, want ErrBadRID", err)
	}
	if got, err := s.GetMany(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("GetMany of no ids = %v, %v; want empty, nil", got, err)
	}
}

func wantObjects(t *testing.T, got []blobstore.Object, want ...blobstore.Object) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("GetMany answered %d objects, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].RID != want[i].RID || string(got[i].Bytes) != string(want[i].Bytes) {
			t.Fatalf("object %d is %s, want %s", i, got[i].RID[:8], want[i].RID[:8])
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func hasOrder(t *testing.T, s blobstore.Store) {
	a, b, c := object("a"), object("b"), object("c")
	put(t, s, frameOf(t, a, c))
	got, err := s.Has(ctx, []string{c.RID, b.RID, a.RID, c.RID})
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, true, true}
	if len(got) != len(want) {
		t.Fatalf("Has answered %d values, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Has = %v, want %v", got, want)
		}
	}
}

func hasBound(t *testing.T, s blobstore.Store) {
	rids := make([]string, blobstore.MaxHas)
	for i := range rids {
		rids[i] = object(string(rune('a'+i%26)) + strings.Repeat("x", i)).RID
	}
	if got, err := s.Has(ctx, rids); err != nil || len(got) != blobstore.MaxHas {
		t.Fatalf("Has of MaxHas ids = %d values, %v; want %d, nil", len(got), err, blobstore.MaxHas)
	}
	if _, err := s.Has(ctx, append(rids, rids[0])); !errors.Is(err, blobstore.ErrTooMany) {
		t.Fatalf("Has of MaxHas+1 ids = %v, want ErrTooMany", err)
	}
	if got, err := s.Has(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("Has of no ids = %v, %v; want empty, nil", got, err)
	}
}

// badFrames gives every rule of Decode its own row. Each frame is valid except
// for that one rule, so a row that fails to fail proves that rule is unchecked.
func badFrames(t *testing.T, s blobstore.Store) {
	a, b := object("a"), object("b")
	for name, frame := range badFrameTable(a, b) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.PutFrame(ctx, frame); !errors.Is(err, blobstore.ErrBadFrame) {
				t.Fatalf("PutFrame = %v, want ErrBadFrame", err)
			}
			if _, err := s.Get(ctx, a.RID); !errors.Is(err, blobstore.ErrNotFound) {
				t.Fatalf("a refused frame left an object behind: Get = %v", err)
			}
		})
	}
}

func badFrameTable(a, b blobstore.Object) map[string][]byte {
	good := func(refs ...blobstore.ObjectRef) []byte { return frameWith(header(refs...), a.Bytes, b.Bytes) }
	ref := func(o blobstore.Object, off uint64) blobstore.ObjectRef {
		return blobstore.ObjectRef{RID: o.RID, Off: off, Len: uint32(len(o.Bytes))}
	}
	la := uint64(len(a.Bytes))
	pad := make([]byte, blobstore.MaxFrame)
	return map[string][]byte{
		"empty":                    nil,
		"shorter than the prefix":  []byte("AGEF\x01\x00"),
		"wrong magic":              append([]byte("XXXX\x01"), good(ref(a, 0), ref(b, la))[5:]...),
		"wrong magic version":      append([]byte("AGEF\x02"), good(ref(a, 0), ref(b, la))[5:]...),
		"header longer than limit": binary.LittleEndian.AppendUint32([]byte("AGEF\x01"), blobstore.MaxHeader+1),
		"header runs past frame":   binary.LittleEndian.AppendUint32([]byte("AGEF\x01"), 100),
		"header not json":          frameWith([]byte("not json"), a.Bytes),
		"header version 2":         frameWith(mustJSON(blobstore.Header{V: 2, Objects: []blobstore.ObjectRef{ref(a, 0)}}), a.Bytes),
		"cell key id too short":    frameWith(mustJSON(blobstore.Header{V: 1, CellKeyID: "abc", Objects: []blobstore.ObjectRef{ref(a, 0)}}), a.Bytes),
		"cell key id uppercase":    frameWith(mustJSON(blobstore.Header{V: 1, CellKeyID: strings.Repeat("A", 32), Objects: []blobstore.ObjectRef{ref(a, 0)}}), a.Bytes),
		"cell key id empty":        frameWith(mustJSON(blobstore.Header{V: 1, Objects: []blobstore.ObjectRef{ref(a, 0)}}), a.Bytes),
		"header version missing":   frameWith([]byte(`{"objects":[]}`), a.Bytes),
		"no objects":               frameWith(mustJSON(blobstore.Header{V: 1, Objects: []blobstore.ObjectRef{}}), nil),
		"first offset not zero":    frameWith(header(ref(a, 1)), []byte("x"), a.Bytes),
		"gap between objects":      frameWith(header(ref(a, 0), ref(b, la+1)), a.Bytes, []byte("x"), b.Bytes),
		"objects overlap":          frameWith(header(ref(a, 0), ref(b, la-1)), a.Bytes, b.Bytes),
		"object past payload":      frameWith(header(ref(a, 0), ref(b, la)), a.Bytes),
		"payload has extra bytes":  frameWith(header(ref(a, 0)), a.Bytes, b.Bytes),
		"rid too short":            frameWith(header(blobstore.ObjectRef{RID: "abc", Len: uint32(len(a.Bytes))}), a.Bytes),
		"rid uppercase":            frameWith(header(blobstore.ObjectRef{RID: strings.ToUpper(a.RID), Len: uint32(len(a.Bytes))}), a.Bytes),
		"rid not hex":              frameWith(header(blobstore.ObjectRef{RID: strings.Repeat("z", 64), Len: uint32(len(a.Bytes))}), a.Bytes),
		"duplicate rid":            frameWith(header(ref(a, 0), blobstore.ObjectRef{RID: a.RID, Off: la, Len: uint32(len(a.Bytes))}), a.Bytes, a.Bytes),
		"object is plaintext":      frameWith(header(blobstore.ObjectRef{RID: a.RID, Len: 5}), []byte("hello")),
		"object has empty body":    frameWith(header(blobstore.ObjectRef{RID: a.RID, Len: 0}), nil),
		"object magic version":     frameWith(header(blobstore.ObjectRef{RID: a.RID, Len: 6}), []byte("AGEO\x02x")),
		"object magic cut short":   frameWith(header(blobstore.ObjectRef{RID: a.RID, Len: 4}), []byte("AGEO")),
		"second object is plain":   frameWith(header(ref(a, 0), blobstore.ObjectRef{RID: b.RID, Off: la, Len: 5}), a.Bytes, []byte("plain")),
		"frame larger than limit":  oversized(a, pad),
	}
}

// oversized builds a frame whose only fault is its total size: one valid
// object padded past MaxFrame.
func oversized(a blobstore.Object, pad []byte) []byte {
	body := append(append([]byte(nil), a.Bytes...), pad...)
	return frameWith(header(blobstore.ObjectRef{RID: a.RID, Len: uint32(len(body))}), body)
}

// L6: a store must never hold bytes it cannot tell are sealed.
func plaintextRefused(t *testing.T, s blobstore.Store) {
	plain := blobstore.Object{RID: hash("plain"), Bytes: []byte("just some readable text")}
	frame := frameWith(header(blobstore.ObjectRef{RID: plain.RID, Len: uint32(len(plain.Bytes))}), plain.Bytes)
	if _, err := s.PutFrame(ctx, frame); !errors.Is(err, blobstore.ErrBadFrame) {
		t.Fatalf("PutFrame of a plaintext object = %v, want ErrBadFrame", err)
	}
	if _, err := s.Get(ctx, plain.RID); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("plaintext object was stored: Get = %v", err)
	}
}

func vaultObjectAccepted(t *testing.T, s blobstore.Store) {
	v := blobstore.Object{RID: hash("vault"), Bytes: []byte("AGEV\x01envelope")}
	put(t, s, frameOf(t, v))
	if got, err := s.Get(ctx, v.RID); err != nil || string(got) != string(v.Bytes) {
		t.Fatalf("Get of a vault object = %q, %v", got, err)
	}
}

func put(t *testing.T, s blobstore.Store, frame []byte) blobstore.FrameID {
	t.Helper()
	id, err := s.PutFrame(ctx, frame)
	if err != nil {
		t.Fatalf("PutFrame: %v", err)
	}
	return id
}

func frameOf(t *testing.T, objects ...blobstore.Object) []byte {
	t.Helper()
	frame, err := blobstore.Encode(strings.Repeat("0", 32), objects)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return frame
}

// object makes a sealed-looking object whose id is the hash of its name.
func object(name string) blobstore.Object {
	return blobstore.Object{RID: hash(name), Bytes: sealed(name)}
}

func sealed(body string) []byte { return append([]byte("AGEO\x01"), body...) }

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func header(refs ...blobstore.ObjectRef) []byte {
	return mustJSON(blobstore.Header{V: 1, CellKeyID: strings.Repeat("0", 32), Objects: refs})
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// frameWith assembles a frame by hand so a case can break exactly one rule,
// which Encode would refuse to do.
func frameWith(header []byte, payload ...[]byte) []byte {
	out := binary.LittleEndian.AppendUint32([]byte("AGEF\x01"), uint32(len(header)))
	out = append(out, header...)
	for _, p := range payload {
		out = append(out, p...)
	}
	return out
}

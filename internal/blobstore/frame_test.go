package blobstore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

func obj(name, body string) blobstore.Object {
	sum := sha256.Sum256([]byte(name))
	return blobstore.Object{RID: hex.EncodeToString(sum[:]), Bytes: []byte("AGEO\x01" + body)}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := []blobstore.Object{obj("a", "one"), obj("b", ""), obj("c", "three")}
	frame, err := blobstore.Encode(strings.Repeat("0", 32), in)
	if err != nil {
		t.Fatal(err)
	}
	h, out, err := blobstore.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	if h.V != 1 || h.CellKeyID != strings.Repeat("0", 32) || len(h.Objects) != 3 {
		t.Fatalf("header = %+v", h)
	}
	for i := range in {
		if out[i].RID != in[i].RID || string(out[i].Bytes) != string(in[i].Bytes) {
			t.Fatalf("object %d = %q, want %q", i, out[i].Bytes, in[i].Bytes)
		}
	}
}

func TestDecodeAppendCannotReachNextObject(t *testing.T) {
	frame, _ := blobstore.Encode("", []blobstore.Object{obj("a", "one"), obj("b", "two")})
	_, out, err := blobstore.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	_ = append(out[0].Bytes, "XXXX"...)
	if string(out[1].Bytes) != "AGEO\x01two" {
		t.Fatalf("append through the first object changed the second: %q", out[1].Bytes)
	}
}

func TestEncodeRefusesWhatDecodeRefuses(t *testing.T) {
	cases := map[string][]blobstore.Object{
		"no objects": nil,
		"plaintext":  {{RID: obj("a", "").RID, Bytes: []byte("hello")}},
		"bad rid":    {{RID: "nope", Bytes: []byte("AGEO\x01x")}},
		"duplicate":  {obj("a", "x"), obj("a", "x")},
	}
	for name, objects := range cases {
		if _, err := blobstore.Encode("", objects); !errors.Is(err, blobstore.ErrBadFrame) {
			t.Errorf("%s: Encode = %v, want ErrBadFrame", name, err)
		}
	}
}

func TestIDOfIsSHA256OfTheFrame(t *testing.T) {
	frame, _ := blobstore.Encode("", []blobstore.Object{obj("a", "x")})
	sum := sha256.Sum256(frame)
	if got := blobstore.IDOf(frame); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("IDOf = %s", got)
	}
}

func TestValidRID(t *testing.T) {
	good := strings.Repeat("0123456789abcdef", 4)
	cases := map[string]bool{
		good:                          true,
		good[:63]:                     false,
		good + "0":                    false,
		strings.ToUpper(good):         false,
		strings.Repeat("g", 64):       false,
		"":                            false,
		strings.Repeat("a", 63) + "/": false,
	}
	for s, want := range cases {
		if got := blobstore.ValidRID(s); got != want {
			t.Errorf("ValidRID(%q) = %v, want %v", s, got, want)
		}
	}
}

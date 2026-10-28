package clusterstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

type golden struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Canonical string `json:"canonical"`
}

func goldens(t *testing.T) []golden {
	t.Helper()
	b, err := os.ReadFile("testdata/canonical.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []golden
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestR14CanonicalGoldenVectors(t *testing.T) {
	for _, tc := range goldens(t) {
		t.Run(tc.Name, func(t *testing.T) {
			var b []byte
			var err error
			switch tc.Kind {
			case "command":
				var c Command
				c, err = DecodeCommand([]byte(tc.Canonical))
				if err == nil {
					b, err = EncodeCommand(c)
				}
			case "row":
				var r Row
				r, err = DecodeRow([]byte(tc.Canonical))
				if err == nil {
					b, err = EncodeRow(r)
				}
			case "result":
				var r Result
				r, err = DecodeResult([]byte(tc.Canonical))
				if err == nil {
					b, err = EncodeResult(r)
				}
			default:
				t.Fatalf("unknown fixture kind %s", tc.Kind)
			}
			if err != nil || string(b) != tc.Canonical {
				t.Fatalf("round trip: %v\ngot %s\nwant %s", err, b, tc.Canonical)
			}
		})
	}
}

func TestR14Uint64BoundariesAndLegacyJSONStayDistinct(t *testing.T) {
	for _, n := range []uint64{0, 1, MaxSafeInteger, MaxSafeInteger + 1, math.MaxUint64} {
		in := Lease{Fence: n}
		b, err := Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		var out Lease
		if err := Decode(b, &out); err != nil || in != out {
			t.Fatalf("%d: %s, %+v, %v", n, b, out, err)
		}
		legacy, _ := json.Marshal(in)
		if bytes.Equal(legacy, b) || bytes.Contains(legacy, []byte(`"fence":"`)) {
			t.Fatalf("v1 numeric JSON changed: %s", legacy)
		}
	}
	for _, n := range []int64{-int64(MaxSafeInteger), int64(MaxSafeInteger)} {
		in := CellView{Now: n}
		b, err := Encode(in)
		var out CellView
		if err != nil || Decode(b, &out) != nil || out.Now != n {
			t.Fatalf("safe timestamp %d: %s, %v", n, b, err)
		}
	}
	if _, err := Encode(CellView{Now: int64(MaxSafeInteger) + 1}); !errors.Is(err, ErrCodec) {
		t.Fatalf("rounded timestamp accepted: %v", err)
	}
}

func TestR14RejectAmbiguousCountersAndJSON(t *testing.T) {
	base := goldens(t)[0].Canonical
	for name, b := range map[string]string{
		"numeric uint64":     strings.Replace(base, `"9007199254740992"`, `9007199254740992`, 1),
		"overflow":           strings.Replace(base, `"18446744073709551615"`, `"18446744073709551616"`, 1),
		"leading zero":       strings.Replace(base, `"9007199254740992"`, `"09007199254740992"`, 1),
		"negative":           strings.Replace(base, `"9007199254740992"`, `"-1"`, 1),
		"plus":               strings.Replace(base, `"9007199254740992"`, `"+1"`, 1),
		"exponent":           strings.Replace(base, `"9007199254740992"`, `"1e3"`, 1),
		"fraction":           strings.Replace(base, `1700000000000`, `1700000000000.0`, 1),
		"unsafe time":        strings.Replace(base, `1700000000000`, `9007199254740992`, 1),
		"unknown":            strings.Replace(base, `"actor":`, `"unknown":0,"actor":`, 1),
		"duplicate":          strings.Replace(base, `"actor":`, `"actor":"other","actor":`, 1),
		"trailing":           base + `{}`,
		"whitespace":         base + "\n",
		"null scalar":        strings.Replace(base, `"actor":"dev_a"`, `"actor":null`, 1),
		"unpaired surrogate": strings.Replace(base, `"dev_a"`, `"\ud800"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCommand([]byte(b)); err == nil {
				t.Fatalf("accepted %s", b)
			}
		})
	}
}

func TestR14UnknownSchemaAndOperationsStopDecoding(t *testing.T) {
	base := goldens(t)[0].Canonical
	if _, err := DecodeCommand([]byte(strings.Replace(base, `"schema":1`, `"schema":2`, 1))); !errors.Is(err, ErrSchema) {
		t.Fatalf("unknown version: %v", err)
	}
	c, err := DecodeCommand([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	c.Change.AdvanceTime = &AdvanceTime{}
	if _, err := EncodeCommand(c); err == nil {
		t.Fatal("two operations accepted")
	}
	c.Change = Mutation{}
	if _, err := EncodeCommand(c); err == nil {
		t.Fatal("no operation accepted")
	}
	unknown := strings.Replace(base, `"acquire":`, `"unknown_operation":`, 1)
	if _, err := DecodeCommand([]byte(unknown)); err == nil {
		t.Fatal("unknown operation accepted")
	}
}

func TestR14EveryInitialCommandVariantRoundTrips(t *testing.T) {
	for _, change := range []Mutation{
		{Create: &CreateCell{Cell: "c", Init: CellInit{Head: "h", Frames: []string{"f"}}, Receipt: "r"}},
		{Acquire: &AcquireCell{Cell: "c", Expect: LeaseExpectation{Device: "a", Fence: 1}}},
		{Beat: &BeatCell{Cell: "c", Beat: Beat{Fence: 1, Pending: 2}}},
		{BeatBatch: &BeatBatch{Items: []BeatCell{{Cell: "c", Beat: Beat{Fence: 1}}, {Cell: "d", Beat: Beat{Fence: 2}}}}},
		{BeatBatch: &BeatBatch{}},
		{BeatBatch: &BeatBatch{Items: []BeatCell{}}},
		{Publish: &PublishCell{Cell: "c", Publish: Publish{Fence: 1, OldHead: "h", Head: "h2"}, Receipt: "r"}},
		{Release: &ReleaseCell{Cell: "c", Fence: 1, Quiet: true}},
		{Archive: &CellID{Cell: "c"}},
		{PutDevice: &PutDevice{ID: "d", Device: Device{Name: "sealed"}}},
		{Revoke: &DeviceID{Device: "d"}},
		{SetVault: &VaultCAS{Old: "v", New: "v2"}},
		{AdvanceTime: &AdvanceTime{}},
		{DeviceSeen: &DeviceSeen{Device: "d"}},
	} {
		c := Command{Schema: SchemaVersion, Cluster: "cluster", Actor: "a", Session: "s", Sequence: 1, RequestDigest: "digest", Change: change}
		b, err := EncodeCommand(c)
		if err != nil {
			t.Fatal(err)
		}
		out, err := DecodeCommand(b)
		if err != nil || !reflect.DeepEqual(c, out) {
			t.Fatalf("command round trip: %s, %v", b, err)
		}
	}
}

func TestR15SealedBytesAndOpaqueMapKeysRemainExact(t *testing.T) {
	if _, err := Encode(Cell{Title: string([]byte{0xff})}); !errors.Is(err, ErrCodec) {
		t.Fatalf("lossy opaque string accepted: %v", err)
	}
	c, err := DecodeCommand([]byte(goldens(t)[0].Canonical))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		if len(c.Signature) != 256 || c.Signature[i] != byte(i) {
			t.Fatal("binary payload changed")
		}
	}
	r, err := DecodeRow([]byte(goldens(t)[1].Canonical))
	if err != nil {
		t.Fatal(err)
	}
	if r.Cell.Size != math.MaxUint64 || r.Cell.Lease.Fence != MaxSafeInteger || r.Cell.Keys["key"]["fence"] != "9007199254740993" {
		t.Fatalf("typed or opaque counter changed: %+v", r.Cell)
	}
	if r.Cell.Title != jsonBase64(t, c.Signature) || r.Cell.Keys["key"]["id"] != r.Cell.Title {
		t.Fatal("sealed fields changed")
	}
}

func jsonBase64(t *testing.T, b []byte) string {
	t.Helper()
	encoded, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if err := json.Unmarshal(encoded, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestR13CanonicalRowsRetainAllApplicationFields(t *testing.T) {
	for _, tc := range goldens(t) {
		if tc.Kind != "row" {
			continue
		}
		r, err := DecodeRow([]byte(tc.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var restored Row
		if err := json.Unmarshal(legacy, &restored); err != nil || !reflect.DeepEqual(r, restored) {
			t.Fatalf("legacy record fields changed: %v", err)
		}
	}
}

func TestR14DecodeFailureDoesNotChangeDestination(t *testing.T) {
	in := Lease{Device: "keep", Fence: 5}
	out := in
	if err := Decode([]byte(`{"fence":"6","unknown":0}`), &out); err == nil || out != in {
		t.Fatalf("invalid decode changed destination: %+v, %v", out, err)
	}
}

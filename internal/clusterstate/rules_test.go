package clusterstate

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// These are the existing cross-relay contract vectors, consumed unchanged.
// R1/R2/R22/R25 require extraction to preserve their observable decisions.
func TestR25ExistingPureRuleVectorsRemainIdentical(t *testing.T) {
	b, err := os.ReadFile("../../relay/hosted/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Rules []struct {
			Name   string          `json:"name"`
			Op     string          `json:"op"`
			Device string          `json:"device"`
			Now    int64           `json:"now"`
			Cell   Cell            `json:"cell"`
			Arg    json.RawMessage `json:"arg"`
			Want   Cell            `json:"want"`
			Err    string          `json:"err"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(b, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors.Rules) == 0 {
		t.Fatal("no contract vectors found")
	}
	for _, tc := range vectors.Rules {
		t.Run(tc.Name, func(t *testing.T) {
			arg := func(v any) {
				if len(tc.Arg) > 0 {
					if err := json.Unmarshal(tc.Arg, v); err != nil {
						t.Fatal(err)
					}
				}
			}
			var got Cell
			var err error
			switch tc.Op {
			case "acquire":
				var in struct{ Force bool }
				arg(&in)
				got, err = Acquire(tc.Cell, tc.Device, tc.Now, in.Force)
			case "heartbeat":
				var in Beat
				arg(&in)
				got, err = Heartbeat(tc.Cell, tc.Device, in, tc.Now)
			case "publish":
				var in Publish
				arg(&in)
				got, err = PublishTo(tc.Cell, tc.Device, in, tc.Now)
			case "release":
				var in Beat
				arg(&in)
				got, err = ReleaseOf(tc.Cell, tc.Device, in.Fence)
			case "create":
				var in CellInit
				arg(&in)
				got = Created(in, tc.Device, tc.Now)
			default:
				t.Fatalf("unknown operation %s", tc.Op)
			}
			wantErr, known := map[string]error{"": nil, "lease_held": ErrLeaseHeld, "fence_stale": ErrFenceStale, "head_moved": ErrHeadMoved}[tc.Err]
			if !known || err != wantErr {
				t.Fatalf("error = %v, want %q", err, tc.Err)
			}
			if err == nil && !reflect.DeepEqual(got, tc.Want) {
				t.Fatalf("got %+v, want %+v", got, tc.Want)
			}
			if err != nil && !reflect.DeepEqual(got, tc.Cell) {
				t.Fatal("a refusal changed the cell")
			}
		})
	}
}

func TestR1ConditionalAcquireComparesBeforeLivenessAndForce(t *testing.T) {
	c := Cell{Lease: Lease{Device: "a", Fence: 7, Expires: 100}}
	expect := &LeaseExpectation{Device: "a", Fence: 7}
	for _, force := range []bool{false, true} {
		taken, err := Acquire(c, "b", 101, force, expect)
		if err != nil || taken.Lease.Fence != 8 {
			t.Fatalf("first take force=%v: %+v %v", force, taken, err)
		}
		refused, err := Acquire(taken, "c", 1_000_000, force, expect)
		if err != ErrLeaseHeld || !reflect.DeepEqual(taken, refused) {
			t.Fatalf("changed observation force=%v: %+v %v", force, refused, err)
		}
	}
}

func TestR16DeviceStampCannotUndoRevocationOrTrustClientTimes(t *testing.T) {
	old := Device{Revoked: true, Created: 10, LastSeen: 20}
	got := Stamped(old, true, Device{Created: 100, LastSeen: 200, Platform: "arbitrary"}, 300)
	if !got.Revoked || got.Created != 10 || got.LastSeen != 20 || got.Platform != "other" {
		t.Fatalf("stamp changed authority: %+v", got)
	}
	if _, err := RevokeOf(old, "a", "a"); err != ErrSelfRevoke {
		t.Fatalf("self revoke: %v", err)
	}
}

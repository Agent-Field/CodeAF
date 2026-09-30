package directory

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestHoldQueryRoundTripsThroughParse(t *testing.T) {
	in := []Hold{{"a:b", 3}, {"c", 0}, {strings.Repeat("x", MaxHoldCellBytes), maxHoldFence}}
	q, err := url.ParseQuery(HoldQuery(in))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseHolds(q["hold"])
	if err != nil || !reflect.DeepEqual(got, in) {
		t.Fatalf("ParseHolds = %v, %v; want %v", got, err, in)
	}
	if HoldQuery(nil) != "" {
		t.Fatal("no holds must give an empty query")
	}
}

func TestParseHoldsRefusesWhatTheContractRefuses(t *testing.T) {
	for _, v := range []string{
		"nocolon", ":5", "c:", "c:-1", "c:+1", "c: 1", "c:1x", "c:9007199254740992",
		"c:12345678901234567", strings.Repeat("c", MaxHoldCellBytes+1) + ":1", "",
	} {
		if _, err := ParseHolds([]string{v}); err != errBadRequest {
			t.Errorf("hold %q: err %v, want bad request", v, err)
		}
	}
	many := make([]string, MaxHolds+1)
	for i := range many {
		many[i] = "c:1"
	}
	if _, err := ParseHolds(many); err != errBadRequest {
		t.Errorf("%d holds: err %v, want bad request", len(many), err)
	}
}

func TestParseHoldsCountsARepeatedHoldOnce(t *testing.T) {
	got, err := ParseHolds([]string{"c:1", "c:2", "c:1"})
	want := []Hold{{"c", 1}, {"c", 2}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseHolds = %v, %v; want %v", got, err, want)
	}
}

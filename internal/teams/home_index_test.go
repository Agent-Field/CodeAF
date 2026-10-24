package teams

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// The existing single-conversation policy remains the reference, including
// untidied duplicate flags, closed parents, cycles and independent members.
func TestBatchedHomesAndRepairPreserveReportingPolicy(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 1000; trial++ {
		var teams []Team
		for i := 0; i < 7; i++ {
			tm := Team{ID: fmt.Sprint(i), Manager: fmt.Sprint(rng.Intn(6))}
			if rng.Intn(4) == 0 {
				tm.ClosedAt = time.Now()
			}
			if rng.Intn(2) == 0 {
				tm.Parent = fmt.Sprint(rng.Intn(9))
			}
			for j := 0; j < 10; j++ {
				tm.Members = append(tm.Members, Member{Key: fmt.Sprint(rng.Intn(8)), Home: rng.Intn(3) == 0, Independent: rng.Intn(6) == 0, Started: rng.Intn(2) == 0})
			}
			teams = append(teams, tm)
		}
		f := &File{Teams: teams}
		homes := f.Homes()
		for key := 0; key < 9; key++ {
			want, wantOK := f.Home(fmt.Sprint(key))
			got, gotOK := homes[fmt.Sprint(key)]
			if got != want || gotOK != wantOK {
				t.Fatalf("trial %d key %d: batch=%+v/%v single=%+v/%v", trial, key, got, gotOK, want, wantOK)
			}
		}
		want := make([]Team, len(teams))
		copy(want, teams)
		for i := range want {
			want[i].Members = append([]Member(nil), teams[i].Members...)
		}
		changed := legacyAssignHomesForTest(want)
		if got := assignHomes(teams); got != changed || !reflect.DeepEqual(teams, want) {
			t.Fatalf("trial %d changed=%v want=%v: indexed repair changed reporting policy", trial, got, changed)
		}
	}
}

func legacyAssignHomesForTest(teams []Team) bool {
	changed, done := false, map[string]bool{}
	for _, tm := range teams {
		for _, m := range tm.Members {
			if done[m.Key] {
				continue
			}
			done[m.Key] = true
			if independent(teams, m.Key) {
				changed = clearOrSet(teams, m.Key, "") || changed
				continue
			}
			if homeHolds(teams, m.Key) {
				continue
			}
			want := ""
			if cs := candidates(teams, m.Key); len(cs) > 0 {
				want = cs[0].Via
			}
			changed = clearOrSet(teams, m.Key, want) || changed
		}
	}
	return changed
}

package service

import (
	"strconv"
	"testing"
)

// A PAGE IS ONE OWNER'S SERVICES IN ID ORDER, AFTER A GIVEN ID, NO MORE THAN
// ASKED, AND ITS REVISION MOVES WITH MEMBERSHIP ONLY.
func TestPageIsOneOwnersServicesInOrderWithAMembershipRevision(t *testing.T) {
	s := &Supervisor{dir: t.TempDir(), entries: map[string]*entry{}, clock: newFakeClock()}
	for n := 1; n <= 25; n++ {
		id := "s" + strconv.Itoa(n)
		owner := "a"
		if n%5 == 0 {
			owner = "b"
		}
		s.entries[id] = &entry{rec: record{ID: id, Owner: owner}}
	}
	var got []string
	after, rev := "", ""
	for pages := 0; pages < 10; pages++ {
		rows, more, r := s.Page("a", after, 7)
		if rev != "" && r != rev {
			t.Fatal("the revision moved without a membership change")
		}
		rev = r
		if len(rows) > 7 {
			t.Fatalf("a page of %d", len(rows))
		}
		for _, row := range rows {
			got = append(got, row.ID)
			after = row.ID
		}
		if !more {
			break
		}
	}
	if len(got) != 20 || got[0] != "s1" || got[8] != "s11" || got[19] != "s24" {
		t.Fatalf("pages: %v", got)
	}
	s.entries["s7"].rec.Policy = "always"
	if _, _, r := s.Page("a", "", 1); r != rev {
		t.Fatal("a change of state moved the membership revision")
	}
	s.entries["s99"] = &entry{rec: record{ID: "s99", Owner: "a"}}
	if _, _, r := s.Page("a", "", 1); r == rev {
		t.Fatal("a new service did not move the membership revision")
	}
}

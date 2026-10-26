package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE FACTORY'S RULES ARE KEPT BESIDE ITS SETTINGS: none until given, saved
// cleaned, never read back as a job, and a file that cannot be read is an
// error, not an empty set that would let a held job start.
func TestHouseRulesAreKeptCleanedAndNeverForgottenQuietly(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	none, err := st.HouseRules()
	if err != nil || !none.Empty() || none.Rules == nil {
		t.Fatalf("a new factory has rules %#v, %v", none, err)
	}
	saved, err := st.ChangeHouseRules(func(h *factory.HouseRules) error {
		h.Rules = []string{" no new dependencies without asking ", "No new dependencies without asking"}
		h.AskFirst = []string{"touch auth/"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := factory.HouseRules{Rules: []string{"no new dependencies without asking"}, AskFirst: []string{"touch auth/"}, Never: []string{}}
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved %#v, want %#v", saved, want)
	}
	if again, err := st.HouseRules(); err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("read back %#v, %v", again, err)
	}
	if items, err := st.List(); err != nil || len(items) != 0 {
		t.Fatalf("the rules file was listed as a job: %v, %v", items, err)
	}
	if _, err := st.ChangeHouseRules(func(h *factory.HouseRules) error { h.Rules = []string{"a\nb"}; return nil }); err == nil {
		t.Fatal("a two-line rule was saved")
	}
	if again, _ := st.HouseRules(); !reflect.DeepEqual(again, want) {
		t.Fatalf("a refused change altered the rules: %#v", again)
	}
	if err := os.WriteFile(filepath.Join(st.Root(), houseRulesFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.HouseRules(); err == nil {
		t.Fatal("an unreadable rules file read as no rules")
	}
}

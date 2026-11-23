package store

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestTakesArePerHandleAndSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, set, err := st.Takes("sam")
	if err != nil || set || saved != nil {
		t.Fatalf("unset: %v set=%v %v", saved, set, err)
	}
	if err := st.SetTakes(" @Sam ", map[string]bool{"owner/repo": true, "other": false}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTakes("priya", map[string]bool{"owner/web": true}); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, set, err = again.Takes("sam")
	if err != nil || !set || !saved["owner/repo"] || saved["other"] || saved["owner/web"] {
		t.Fatalf("sam: %v set=%v %v", saved, set, err)
	}
	if err := again.SetTakes("sam", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	saved, set, err = again.Takes(factory.NormalizeHandle("sam"))
	if err != nil || !set || len(saved) != 0 {
		t.Fatalf("cleared: %v set=%v %v", saved, set, err)
	}
	if _, set, err = again.Takes("priya"); err != nil || !set {
		t.Fatalf("priya was replaced: set=%v %v", set, err)
	}
	if err := again.SetTakes("sam", map[string]bool{" ": true}); err == nil {
		t.Fatal("blank repo was stored")
	}
}

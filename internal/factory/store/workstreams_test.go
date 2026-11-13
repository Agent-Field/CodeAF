package store

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// AN ORGANIZATION'S WORKSTREAMS ARE KEPT IN ONE SAVE: only the ones an edit
// names count their rev up, a refused edit writes nothing, a team that owns
// one is known, and the document is never read as a job.
func TestOrgWorkstreamsAreSavedTogether(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if all, err := st.Workstreams(); err != nil || len(all) != 0 {
		t.Fatalf("a fresh server keeps %v %v", all, err)
	}
	all, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		all["ws_a"] = factory.OrgWorkstream{Name: "A", Owner: factory.WorkstreamOwner{Kind: "team", ID: "tm_x"}}
		all["ws_b"] = factory.OrgWorkstream{Name: "B", Owner: factory.WorkstreamOwner{Kind: "person", ID: "abir"}}
		return []string{"ws_a", "ws_b"}, nil
	})
	if err != nil || all["ws_a"].Rev != 1 || all["ws_b"].ID != "ws_b" {
		t.Fatalf("created %+v %v", all, err)
	}
	all, err = st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		a := all["ws_a"]
		a.Name = "A2"
		all["ws_a"] = a
		return []string{"ws_a"}, nil
	})
	if err != nil || all["ws_a"].Rev != 2 || all["ws_b"].Rev != 1 {
		t.Fatalf("one edit moved %+v %v", all, err)
	}
	refused := errors.New("no")
	if _, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		delete(all, "ws_a")
		return nil, refused
	}); !errors.Is(err, refused) {
		t.Fatal(err)
	}
	st, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if w, ok, err := st.Workstream("ws_a"); !ok || err != nil || w.Name != "A2" {
		t.Fatalf("after reopening: %+v %v %v", w, ok, err)
	}
	if owns, err := st.TeamOwnsWorkstream("tm_x"); !owns || err != nil {
		t.Fatal("tm_x owns ws_a")
	}
	if owns, _ := st.TeamOwnsWorkstream("tm_y"); owns {
		t.Fatal("tm_y owns nothing")
	}
	if items, err := st.List(); err != nil || len(items) != 0 {
		t.Fatalf("the workstreams file was read as a job: %+v %v", items, err)
	}
}

package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// THE JSON DOCUMENT IS MIGRATED ONCE: a server that kept its workstreams in
// the old file finds every one in the database, pages with a rev, and the file
// renamed aside so a later start never reads it again.
func TestOrgWorkstreamsMigrateFromTheOldDocumentOnce(t *testing.T) {
	root := t.TempDir()
	old := `{"ws_a":{"id":"ws_a","name":"A","stage":"shaping","owner":{"kind":"person","id":"santosh"},"pages":[{"id":"pg_1","title":"One","body_md":"hi","kind":"page"}],"rev":4}}`
	if err := os.WriteFile(filepath.Join(root, WorkstreamsFile), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	w, ok, err := st.Workstream("ws_a")
	if err != nil || !ok || w.Rev != 4 || len(w.Pages) != 1 || w.Pages[0].Rev != 1 || w.Pages[0].BodyMD != "hi" || w.Jobs == nil {
		t.Fatalf("migrated %+v %v %v", w, ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, WorkstreamsFile)); !os.IsNotExist(err) {
		t.Fatal("the old document is still where a start would read it")
	}
	if _, err := os.Stat(filepath.Join(root, WorkstreamsFile+".migrated")); err != nil {
		t.Fatal("the old document was not kept aside")
	}
}

// A STORE THAT NEVER KEPT A WORKSTREAM LEAVES NO DATABASE BEHIND A READ.
func TestOrgWorkstreamsReadCreatesNothing(t *testing.T) {
	root := t.TempDir()
	st, _ := Open(root)
	if all, err := st.Workstreams(); err != nil || len(all) != 0 {
		t.Fatal(all, err)
	}
	if v, err := st.PageVersions("ws_a", "pg_1"); err != nil || len(v) != 0 {
		t.Fatal(v, err)
	}
	if _, err := os.Stat(filepath.Join(root, WorkstreamsDB)); !os.IsNotExist(err) {
		t.Fatal("a read made the database")
	}
}

// A MIRROR SAVE DOES NOT MOVE THE REV a writer read; an edit does; a purge
// takes the workstream's versions, parts and spend with it.
func TestOrgWorkstreamMirrorPartsSpendAndPurge(t *testing.T) {
	st, _ := Open(t.TempDir())
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	if _, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		all["ws_a"] = factory.OrgWorkstream{Name: "A", Pages: []factory.WorkstreamPage{{ID: "pg_1", Title: "One"}}}
		return []string{"ws_a"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := st.MirrorWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		w := all["ws_a"]
		w.Pages[0].BodyMD = "typed"
		all["ws_a"] = w
		return []string{"ws_a"}, nil
	})
	if err != nil || saved["ws_a"].Rev != 1 || saved["ws_a"].Pages[0].BodyMD != "typed" {
		t.Fatalf("mirror %+v %v", saved["ws_a"], err)
	}

	v1, added, err := st.AddPageVersion("ws_a", "pg_1", PageVersion{At: "t1", By: "santosh", Title: "One", Reason: "idle", BodyMD: "a", State: []byte{1}})
	if err != nil || !added || v1.Version != 1 {
		t.Fatal(v1, added, err)
	}
	again, added, _ := st.AddPageVersion("ws_a", "pg_1", PageVersion{At: "t2", By: "abir", Title: "One", Reason: "idle", BodyMD: "a", State: []byte{1}})
	if added || again.Version != 1 || again.By != "santosh" {
		t.Fatalf("the same words saved twice: %+v %v", again, added)
	}
	v2, _, _ := st.AddPageVersion("ws_a", "pg_1", PageVersion{At: "t3", By: "abir", Title: "One", Reason: "manual", BodyMD: "ab"})
	list, _ := st.PageVersions("ws_a", "pg_1")
	if v2.Version != 2 || len(list) != 2 || list[0].Version != 2 || list[0].By != "abir" || list[0].BodyMD != "" || list[0].Bytes != 2 {
		t.Fatalf("versions %+v", list)
	}
	whole, ok, _ := st.PageVersionAt("ws_a", "pg_1", 1)
	if !ok || whole.BodyMD != "a" || len(whole.State) != 1 {
		t.Fatalf("version 1 %+v", whole)
	}

	p, err := st.PutPart("ws_a", PartPlan, "plan", 0, "santosh", now, json.RawMessage(`{"items":[]}`))
	if err != nil || p.Rev != 1 || p.By != "santosh" {
		t.Fatal(p, err)
	}
	if _, err := st.PutPart("ws_a", PartPlan, "plan", 0, "abir", now, json.RawMessage(`{}`)); !errors.Is(err, ErrPartStale) {
		t.Fatal("a second first write was kept:", err)
	}
	if p, err = st.PutPart("ws_a", PartPlan, "plan", 1, "abir", now, json.RawMessage(`{"items":[1]}`)); err != nil || p.Rev != 2 || p.By != "santosh" || p.UpdatedBy != "abir" {
		t.Fatal(p, err)
	}

	if total, _ := st.AddSpend("ws_a", Spend{At: "t", By: "abir", USD: 1.5, Key: "k1"}); total != 1.5 {
		t.Fatal(total)
	}
	if total, _ := st.AddSpend("ws_a", Spend{At: "t", By: "abir", USD: 1.5, Key: "k1"}); total != 1.5 {
		t.Fatal("one key was counted twice:", total)
	}
	if total, _ := st.AddSpend("ws_a", Spend{At: "t", By: "santosh", USD: 0.5}); total != 2 {
		t.Fatal(total)
	}

	if _, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		delete(all, "ws_a")
		return []string{"ws_a"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	list, _ = st.PageVersions("ws_a", "pg_1")
	parts, _ := st.Parts("ws_a", PartPlan)
	spend, _ := st.WorkstreamSpend()
	if len(list) != 0 || len(parts) != 0 || len(spend) != 0 {
		t.Fatalf("a purge left %v %v %v", list, parts, spend)
	}
}

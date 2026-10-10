package desktopbridge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func lifecycleSnapshot(t *testing.T, rig *placesRig) *placegraph.Snapshot {
	t.Helper()
	snapshot, err := rig.p.Store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func lifecycleDelete(t *testing.T, rig *placesRig, id string) Mutation {
	t.Helper()
	var result Mutation
	if code := rig.do("POST", "/places/"+id+"/delete", nil, &result); code != 200 {
		t.Fatalf("delete status %d", code)
	}
	if result.Receipt == nil || len(result.Undo) != 1 || result.Receipt.ID != result.Undo[0] {
		t.Fatalf("missing undo receipt: %+v", result)
	}
	return result
}

func TestDeletingMovesChildrenUpToTheDeletedPlacesParents(t *testing.T) {
	for _, topLevel := range []bool{false, true} {
		t.Run(map[bool]string{false: "multiple parents", true: "top level"}[topLevel], func(t *testing.T) {
			rig := newPlacesRig(t)
			var parents []string
			if !topLevel {
				parents = []string{rig.mk("Parent A"), rig.mk("Parent B")}
			}
			victim := rig.mk("Victim", parents...)
			childParents := append([]string{victim}, parents...)
			child := rig.mk("Child", childParents...)
			grandchild := rig.mk("Grandchild", child)
			before := lifecycleSnapshot(t, rig).Revision
			result := lifecycleDelete(t, rig, victim)
			snapshot := lifecycleSnapshot(t, rig)
			got, _ := snapshot.Place(child)
			if !reflect.DeepEqual(got.Parents, append([]string{}, parents...)) {
				t.Fatalf("parents %v, want %v", got.Parents, parents)
			}
			grand, _ := snapshot.Place(grandchild)
			if !reflect.DeepEqual(grand.Parents, []string{child}) || snapshot.Revision != before+1 || *result.Children != 1 {
				t.Fatalf("delete must be one write moving only direct children: %+v", result)
			}
		})
	}
}

func TestDeletingKeepsChatsOtherPlacesOrUnplacesThem(t *testing.T) {
	rig := newPlacesRig(t)
	victim, other := rig.mk("Victim"), rig.mk("Other")
	for _, chat := range []string{"only", "shared"} {
		if _, _, err := rig.p.Store.AddChat(chat, victim, placegraph.AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := rig.p.Store.AddChat("shared", other, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(filepath.Dir(rig.path), "transcript.jsonl")
	const content = "{\"text\":\"Keep this chat\"}\n"
	if err := os.WriteFile(transcript, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	result := lifecycleDelete(t, rig, victim)
	snapshot := lifecycleSnapshot(t, rig)
	if len(snapshot.PlacesOf("only")) != 0 || len(snapshot.PlacesOf("shared")) != 1 || snapshot.PlacesOf("shared")[0].PlaceID != other || *result.Chats != 2 {
		t.Fatalf("memberships changed incorrectly: %+v", snapshot.Memberships)
	}
	if got, err := os.ReadFile(transcript); err != nil || string(got) != content {
		t.Fatalf("transcript changed: %q, %v", got, err)
	}
}

func TestDeleteUndoRestoresEverything(t *testing.T) {
	for _, route := range []string{"token", "path", "receipts"} {
		t.Run(route, func(t *testing.T) {
			rig := newPlacesRig(t)
			parent := rig.mk("Parent")
			victim := rig.mk("Victim", parent)
			rig.mk("Child", victim)
			if _, _, err := rig.p.Store.AddChat("chat", victim, placegraph.AddedByAI); err != nil {
				t.Fatal(err)
			}
			if _, err := rig.p.Store.Pin(victim, 0); err != nil {
				t.Fatal(err)
			}
			if err := rig.p.Store.Visit(victim); err != nil {
				t.Fatal(err)
			}
			before := lifecycleSnapshot(t, rig).State
			result := lifecycleDelete(t, rig, victim)
			path := "/places/undo"
			var body any = map[string]any{"token": result.Receipt.ID}
			if route == "path" {
				path += "/" + result.Receipt.ID
				body = nil
			}
			if route == "receipts" {
				body = map[string]any{"receipts": result.Undo}
			}
			if code := rig.do("POST", path, body, nil); code != 200 {
				t.Fatalf("undo status %d", code)
			}
			after := lifecycleSnapshot(t, rig).State
			after.Revision = before.Revision
			// Undo preserves the current working set; it restores graph structure and filings.
			before.Open = after.Open
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("undo did not restore exact graph\nbefore: %+v\nafter: %+v", before, after)
			}
		})
	}
}

func TestArchivedPlaceLeavesTheRailAndContext(t *testing.T) {
	rig := newPlacesRig(t)
	victim := rig.mk("Victim")
	if _, _, err := rig.p.Store.AddChat("chat", victim, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.p.Store.Pin(victim, 0); err != nil {
		t.Fatal(err)
	}
	if err := rig.p.Store.Visit(victim); err != nil {
		t.Fatal(err)
	}
	var result Mutation
	if code := rig.do("POST", "/places/"+victim+"/archive", map[string]bool{"archived": true}, &result); code != 200 {
		t.Fatalf("archive status %d", code)
	}
	snapshot := lifecycleSnapshot(t, rig)
	place, found := snapshot.Place(victim)
	if !found || !place.Archived || place.ArchivedAt.IsZero() || len(snapshot.Pinned) != 0 || len(snapshot.Open) != 0 || len(snapshot.Resolve("chat", placegraph.ResolveOptions{}).Places) != 0 {
		t.Fatalf("archived place still active: %+v", snapshot.State)
	}
	var graph graphResponse
	if code := rig.do("GET", "/places?archived=1", nil, &graph); code != 200 || len(graph.Places) != 1 || !graph.Places[0].Archived {
		t.Fatalf("archived place missing from archived list: %+v", graph)
	}
	if result.Receipt == nil || *result.Chats != 1 || *result.Children != 0 {
		t.Fatalf("archive result: %+v", result)
	}
	if code := rig.do("POST", "/places/"+victim+"/archive", map[string]bool{"archived": false}, &result); code != 200 {
		t.Fatalf("restore status %d", code)
	}
	if len(lifecycleSnapshot(t, rig).Resolve("chat", placegraph.ResolveOptions{}).Places) != 1 {
		t.Fatal("restored membership did not restore context")
	}
}

func TestImpactCountsForTheConfirm(t *testing.T) {
	rig := newPlacesRig(t)
	victim := rig.mk("Victim")
	child := rig.mk("Child", victim)
	rig.mk("Grandchild", child)
	archived := rig.mk("Archived child", victim)
	if _, err := rig.p.Store.Archive(archived); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"direct", victim}, {"shared", victim}, {"shared", child}, {"descendant", child}} {
		if _, _, err := rig.p.Store.AddChat(pair[0], pair[1], placegraph.AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	before := lifecycleSnapshot(t, rig).State
	var impact struct {
		Chats    int `json:"chats"`
		Children int `json:"children"`
	}
	if code := rig.do("GET", "/places/"+victim+"/impact", nil, &impact); code != 200 || impact.Chats != 2 || impact.Children != 2 {
		t.Fatalf("impact: %+v", impact)
	}
	if !reflect.DeepEqual(before, lifecycleSnapshot(t, rig).State) {
		t.Fatal("impact changed graph")
	}
	result := lifecycleDelete(t, rig, victim)
	if *result.Chats != impact.Chats || *result.Children != impact.Children {
		t.Fatal("confirmation and deletion counts disagree")
	}
	if code := rig.do("GET", "/places/missing/impact", nil, nil); code != 404 {
		t.Fatalf("missing impact: %d", code)
	}
	if code := rig.do("POST", "/places/"+child+"/impact", nil, nil); code != 404 {
		t.Fatalf("wrong method: %d", code)
	}
}

func TestLifecycleUndoRejectsStaleAndAmbiguousTokens(t *testing.T) {
	rig := newPlacesRig(t)
	victim := rig.mk("Victim")
	result := lifecycleDelete(t, rig, victim)
	rig.mk("Later")
	for _, path := range []string{"/places/undo", "/places/undo/" + result.Receipt.ID} {
		var body any
		if path == "/places/undo" {
			body = map[string]string{"token": result.Receipt.ID}
		}
		if code := rig.do("POST", path, body, nil); code != 409 {
			t.Fatalf("stale undo %s: %d", path, code)
		}
	}
	if code := rig.do("POST", "/places/undo", map[string]any{"token": result.Receipt.ID, "receipts": result.Undo}, nil); code != 400 {
		t.Fatalf("ambiguous undo: %d", code)
	}
	if code := rig.do("POST", "/places/undo", map[string]string{"token": "missing"}, nil); code != 410 {
		t.Fatalf("missing undo: %d", code)
	}
}

func TestLifecycleMergeCountsAndUndoPublishTheRestoredGraph(t *testing.T) {
	rig := newPlacesRig(t)
	source, target := rig.mk("Source"), rig.mk("Target")
	rig.mk("Child", source)
	if _, _, err := rig.p.Store.AddChat("chat", source, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	var records []placesRecord
	rig.p.publish = func(kind string, payload any) {
		if kind != "places" {
			t.Fatalf("unexpected event %s", kind)
		}
		records = append(records, payload.(placesRecord))
	}
	var result Mutation
	if code := rig.do("POST", "/places/"+source+"/merge", map[string]string{"into": target}, &result); code != 200 || result.Receipt == nil || *result.Chats != 1 || *result.Children != 1 {
		t.Fatalf("merge: %d %+v", code, result)
	}
	if code := rig.do("POST", "/places/undo", map[string]string{"token": result.Receipt.ID}, nil); code != 200 {
		t.Fatalf("undo: %d", code)
	}
	if len(records) != 2 || len(records[0].Nodes) != 2 || len(records[1].Nodes) != 3 || records[1].Generation != lifecycleSnapshot(t, rig).Revision {
		t.Fatalf("windows missed restored graph: %+v", records)
	}
}

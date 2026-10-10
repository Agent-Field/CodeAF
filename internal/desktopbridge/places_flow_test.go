package desktopbridge

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// TestPlacesFlow follows one conversation through the real HTTP doors so a
// collection of independently correct handlers cannot hide a broken journey.
func TestPlacesFlow(t *testing.T) {
	rig := newUsingRig(t)
	t.Cleanup(rig.b.Close)

	readGraph := func() graphResponse {
		t.Helper()
		var graph graphResponse
		if code := rig.do(http.MethodGet, "/places?archived=1", nil, &graph); code != http.StatusOK {
			t.Fatalf("graph: status %d", code)
		}
		return graph
	}
	assertRecord := func(before int, revision uint64) {
		t.Helper()
		records := placesRecords(rig.placesRig)
		if len(records) != before+1 {
			t.Fatalf("write emitted %d places records, want exactly one", len(records)-before)
		}
		var record placesRecord
		if err := json.Unmarshal(records[before].Payload, &record); err != nil {
			t.Fatal(err)
		}
		graph := readGraph()
		if record.Generation != revision || record.Generation != graph.Generation ||
			!reflect.DeepEqual(record.Nodes, graph.Nodes) || !reflect.DeepEqual(record.Rail, graph.Rail) {
			t.Fatalf("world record does not describe committed graph: record=%+v graph=%+v", record, graph)
		}
	}
	writePlace := func(path string, body any) Mutation {
		t.Helper()
		before := len(placesRecords(rig.placesRig))
		var result Mutation
		if code := rig.do(http.MethodPost, path, body, &result); code != http.StatusOK || result.Noop {
			t.Fatalf("POST %s: status %d, mutation %+v", path, code, result)
		}
		assertRecord(before, result.Revision)
		return result
	}
	create := func(name string, parents ...string) string {
		t.Helper()
		result := writePlace("/places", map[string]any{"name": name, "parents": parents})
		if result.Place == nil || result.Place.Name != name {
			t.Fatalf("create %s: %+v", name, result)
		}
		return result.Place.ID
	}
	var release string
	assertMembership := func(archived bool) {
		t.Helper()
		var members chatPlacesResponse
		if code := rig.do(http.MethodGet, "/chats/"+rig.chat+"/places", nil, &members); code != http.StatusOK ||
			!members.Known || len(members.Places) != 1 || members.Places[0].ID != release || members.Places[0].Archived != archived {
			t.Fatalf("chat must stay in Release: status %d, %+v", code, members)
		}
	}

	root := create("codeaf")
	software := create("Software", root)
	marketing := create("Marketing", root)
	release = create("Release", software, marketing)
	for _, policy := range []struct{ id, model string }{
		{root, "place/pro"}, {software, "place/software"}, {marketing, "place/marketing"},
	} {
		writePlace("/places/"+policy.id, map[string]any{"policy": map[string]string{"model": policy.model}})
	}
	token, code, refusal := rig.open(map[string]any{})
	if code != http.StatusOK || token == "" {
		t.Fatalf("open fake engine: status %d, %+v", code, refusal)
	}
	filed := writePlace("/places/"+release+"/members", map[string]any{"chats": []string{rig.chat}, "addedBy": "you"})
	if len(filed.Memberships) != 1 || filed.Memberships[0].PlaceID != release || filed.Memberships[0].ChatID != rig.chat {
		t.Fatalf("filing receipt: %+v", filed)
	}
	assertMembership(false)
	using := rig.using(token)
	if using.Bundle == nil || !using.Engine.Places || using.ChatID != rig.chat {
		t.Fatalf("Using must describe this chat's engine context: %+v", using)
	}
	var decision placegraph.PolicyDecision
	for _, candidate := range using.Bundle.Policy {
		if candidate.Field == placegraph.PolicyModel {
			decision = candidate
		}
	}
	if decision.Outcome != placegraph.PolicyDecided || decision.DecidedBy != root || decision.Value != "place/pro" {
		t.Fatalf("Software and Marketing must defer to codeaf: %+v", decision)
	}
	if setting := setting(using, placegraph.PolicyModel); setting.DecidedBy != root || setting.Value != "place/pro" {
		t.Fatalf("Using must expose the ancestor's decision: %+v", setting)
	}

	beforeDelete := readGraph()
	deleted := writePlace("/places/"+software+"/delete", nil)
	if deleted.Receipt == nil || deleted.Children == nil || *deleted.Children != 1 || deleted.Chats == nil || *deleted.Chats != 0 {
		t.Fatalf("delete Software receipt: %+v", deleted)
	}
	afterDelete := readGraph()
	foundRelease := false
	for _, node := range afterDelete.Nodes {
		if node.ID == software {
			t.Fatal("deleted Software still appears in the graph")
		}
		if node.ID == release {
			foundRelease = true
			if !reflect.DeepEqual(node.Parents, []string{root, marketing}) {
				t.Fatalf("Release must move up and retain Marketing: %v", node.Parents)
			}
		}
	}
	if !foundRelease || len(afterDelete.Nodes) != len(beforeDelete.Nodes)-1 {
		t.Fatalf("delete must preserve every other place: %+v", afterDelete.Nodes)
	}
	assertMembership(false)
	beforeUndo := len(placesRecords(rig.placesRig))
	var undone struct {
		Revision uint64 `json:"revision"`
		Undone   int    `json:"undone"`
	}
	if code := rig.do(http.MethodPost, "/places/undo", map[string]string{"token": deleted.Receipt.ID}, &undone); code != http.StatusOK || undone.Undone != 1 {
		t.Fatalf("undo delete: status %d, %+v", code, undone)
	}
	assertRecord(beforeUndo, undone.Revision)
	if after := readGraph(); !reflect.DeepEqual(after.Nodes, beforeDelete.Nodes) || !reflect.DeepEqual(after.Rail, beforeDelete.Rail) {
		t.Fatalf("undo must restore the graph: before=%+v after=%+v", beforeDelete, after)
	}
	assertMembership(false)

	writePlace("/places/rail", map[string]string{"op": "pin", "place": release})
	var rail RailView
	if code := rig.do(http.MethodGet, "/places/rail", nil, &rail); code != http.StatusOK || len(rail.Pinned) != 1 || rail.Pinned[0].ID != release {
		t.Fatalf("Release must be visible before archive: status %d, %+v", code, rail)
	}
	writePlace("/places/"+release+"/archive", map[string]bool{"archived": true})
	if code := rig.do(http.MethodGet, "/places/rail", nil, &rail); code != http.StatusOK {
		t.Fatalf("archived rail: status %d", code)
	}
	for _, section := range [][]PlaceView{rail.Pinned, rail.Open} {
		for _, node := range section {
			if node.ID == release {
				t.Fatal("archived Release still appears in the rail")
			}
		}
	}
	assertMembership(true)

	// Both requests carry the same fresh generation. Exactly one may commit;
	// the other becomes stale while waiting for the handler's write lock.
	beforeRace := readGraph()
	beforeRecords := len(placesRecords(rig.placesRig))
	var results [2]struct {
		status int
		body   struct {
			Mutation
			apiError
		}
	}
	start := make(chan struct{})
	var writers sync.WaitGroup
	for i, name := range []string{"codeaf first", "codeaf second"} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			results[i].status = rig.do(http.MethodPost, "/places/"+root,
				map[string]any{"name": name, "ifGeneration": beforeRace.Generation}, &results[i].body)
		}()
	}
	close(start)
	writers.Wait()
	winner := -1
	for i, result := range results {
		switch result.status {
		case http.StatusOK:
			if winner != -1 {
				t.Fatal("both writers committed the same generation")
			}
			winner = i
		case http.StatusConflict:
			if result.body.Code != "stale" {
				t.Fatalf("stale writer refusal: %+v", result.body.apiError)
			}
		default:
			t.Fatalf("writer %d: status %d, %+v", i, result.status, result.body)
		}
	}
	if winner == -1 || results[winner].body.Generation != beforeRace.Generation+1 || results[winner].body.Place == nil {
		t.Fatalf("want one new generation: %+v", results)
	}
	assertRecord(beforeRecords, results[winner].body.Generation)
	var home digestResponse
	if code := rig.do(http.MethodGet, "/places/"+root, nil, &home); code != http.StatusOK || home.Title != results[winner].body.Place.Name {
		t.Fatalf("losing writer changed the committed name: status %d, %+v", code, home)
	}
	assertMembership(true)
	if count := len(placesRecords(rig.placesRig)); count != beforeRecords+1 {
		t.Fatalf("reads or the stale writer emitted extra records: got %d, want %d", count, beforeRecords+1)
	}
}

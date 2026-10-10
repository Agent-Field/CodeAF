package desktopbridge

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

func addKnows(t *testing.T, rig *placesRig, place, text string) knowsMutation {
	t.Helper()
	var out knowsMutation
	if code := rig.do("POST", "/places/"+place+"/knows", map[string]any{"text": text}, &out); code != 200 || out.Line == nil || len(out.Undo) != 1 {
		t.Fatalf("add: %d %+v", code, out)
	}
	return out
}

func listKnows(t *testing.T, rig *placesRig, place string) knowsList {
	t.Helper()
	var out knowsList
	if code := rig.do("GET", "/places/"+place+"/knows", nil, &out); code != 200 {
		t.Fatalf("list: %d", code)
	}
	return out
}

func TestKnowsCRUDAndDeleteUndo(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Release")
	base := "/places/" + id + "/knows"
	if got := listKnows(t, rig, id); len(got.Lines) != 0 || got.Lines == nil || got.StillTrue == nil {
		t.Fatalf("empty: %+v", got)
	}
	added := addKnows(t, rig, id, "Use tabs")
	if added.Line.SourceWords != "You wrote" || added.Line.Source.Kind != placegraph.LineYouWrote {
		t.Fatalf("source: %+v", added.Line)
	}
	rig.advance(24 * time.Hour)
	var edited knowsMutation
	if code := rig.do("PATCH", base+"/"+added.Line.ID, map[string]any{"text": "Use spaces"}, &edited); code != 200 {
		t.Fatalf("edit: %d", code)
	}
	if edited.Line.ID != added.Line.ID || edited.Line.Text != "Use spaces" || !edited.Line.CreatedAt.Equal(added.Line.CreatedAt) || edited.Line.EditedAt.IsZero() {
		t.Fatalf("edit: %+v", edited)
	}
	var removed knowsMutation
	if code := rig.do("DELETE", base+"/"+added.Line.ID, nil, &removed); code != 200 || len(removed.Undo) != 1 {
		t.Fatalf("delete: %d %+v", code, removed)
	}
	if len(listKnows(t, rig, id).Lines) != 0 {
		t.Fatal("delete kept the line")
	}
	if code := rig.do("POST", "/places/undo/"+removed.Undo[0], nil, nil); code != 200 {
		t.Fatalf("undo: %d", code)
	}
	if got := listKnows(t, rig, id); len(got.Lines) != 1 || got.Lines[0].Text != "Use spaces" {
		t.Fatalf("restored: %+v", got)
	}
}

func TestKnowsPatchIsNewerAndSupersedesAtomically(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Pricing")
	edited := addKnows(t, rig, id, "Prefer flat rates")
	rig.advance(24 * time.Hour)
	older := addKnows(t, rig, id, "Lead with usage pricing")
	rig.advance(24 * time.Hour)
	var out knowsMutation
	if code := rig.do("PATCH", "/places/"+id+"/knows/"+edited.Line.ID, map[string]any{"text": "Lead with free seat", "supersedes": older.Line.ID}, &out); code != 200 || out.Ask != nil || len(out.Undo) != 1 {
		t.Fatalf("replace: %d %+v", code, out)
	}
	got := listKnows(t, rig, id)
	if got.Lines[1].ReplacedBy != edited.Line.ID || got.Lines[1].ReplacedAt.IsZero() {
		t.Fatalf("not replaced: %+v", got)
	}
	if code := rig.do("POST", "/places/undo/"+out.Undo[0], nil, nil); code != 200 {
		t.Fatalf("undo: %d", code)
	}
	got = listKnows(t, rig, id)
	if got.Lines[0].Text != "Prefer flat rates" || got.Lines[1].ReplacedBy != "" {
		t.Fatalf("partial undo: %+v", got)
	}
}

func TestKnowsSameDayConflictAsksThroughPostAndPatch(t *testing.T) {
	for _, method := range []string{"POST", "PATCH"} {
		t.Run(method, func(t *testing.T) {
			rig := newPlacesRig(t)
			id := rig.mk("Pricing")
			earlier := addKnows(t, rig, id, "Lead with usage pricing")
			path := "/places/" + id + "/knows"
			if method == "PATCH" {
				fresh := addKnows(t, rig, id, "Prefer flat rates")
				path += "/" + fresh.Line.ID
			}
			var out knowsMutation
			if code := rig.do(method, path, map[string]any{"text": "Lead with free seat", "supersedes": earlier.Line.ID}, &out); code != 200 || out.Ask == nil {
				t.Fatalf("conflict: %d %+v", code, out)
			}
			if out.Ask.A.ID != earlier.Line.ID || out.Ask.B.Text != "Lead with free seat" {
				t.Fatalf("ask: %+v", out.Ask)
			}
			for _, line := range listKnows(t, rig, id).Lines {
				if line.ReplacedBy != "" {
					t.Fatalf("guessed: %+v", line)
				}
			}
		})
	}
}

func TestKnowsStillTrueResetsLastUse(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Release")
	added := addKnows(t, rig, id, "Run the suite before launch")
	rig.advance(placegraph.StillTrueAfter)
	if got := listKnows(t, rig, id); len(got.StillTrue) != 1 {
		t.Fatalf("due: %+v", got)
	}
	path := "/places/" + id + "/knows/" + added.Line.ID + "/still-true"
	if code := rig.do("POST", path, map[string]any{"yes": false}, nil); code != 400 {
		t.Fatalf("false: %d", code)
	}
	var out knowsMutation
	if code := rig.do("POST", path, map[string]any{"yes": true}, &out); code != 200 || !out.Line.LastUsedAt.Equal(rig.clock) {
		t.Fatalf("confirm: %d %+v", code, out)
	}
	if len(listKnows(t, rig, id).StillTrue) != 0 {
		t.Fatal("confirmation remained due")
	}
}

func TestKnowsListsRealSourceWordsAndRejectsForeignWrites(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Release")
	rig.setRows(row("chat-launch", "Launch post", rig.clock))
	for _, src := range []placegraph.LineSource{
		{Kind: placegraph.LineSaidInChat, ChatID: "chat-launch", At: placesEpoch},
		{Kind: placegraph.LineLearned, Answers: 3},
		{Kind: placegraph.LineFile, Path: "brand-voice.md"},
	} {
		if _, _, err := rig.p.Store.AddLine(placegraph.Line{PlaceID: id, Text: "Run tests", Source: src}); err != nil {
			t.Fatal(err)
		}
	}
	got := listKnows(t, rig, id)
	for i, words := range []string{"You said in Launch post", "Learned from 3 of your answers", "From brand-voice.md"} {
		if got.Lines[i].SourceWords != words {
			t.Fatalf("source %d: %+v", i, got.Lines[i])
		}
	}
	other := rig.mk("Other")
	var e apiError
	for _, method := range []string{"PATCH", "DELETE"} {
		if code := rig.do(method, "/places/"+other+"/knows/"+got.Lines[0].ID, map[string]any{"text": "Changed"}, &e); code != 404 {
			t.Fatalf("foreign %s: %d %+v", method, code, e)
		}
	}
	rev, _ := rig.p.Store.Revision()
	if code := rig.do("POST", "/places/"+id+"/knows", map[string]any{"text": "Changed", "ifGeneration": rev - 1}, &e); code != 409 {
		t.Fatalf("stale: %d %+v", code, e)
	}
	if code := rig.do("POST", "/places/"+id+"/knows", map[string]any{"text": "Changed", "supersedes": got.Lines[0].ID + "missing"}, &e); code != 400 {
		t.Fatalf("target: %d %+v", code, e)
	}
	if len(listKnows(t, rig, id).Lines) != 3 {
		t.Fatal("failed replacement wrote partial data")
	}
}

func TestKnowsUsingReadsLiveLinesWithPlaceSourcesAndPolicy(t *testing.T) {
	rig := newUsingRig(t)
	id := rig.policyPlace("Release", placegraph.Policy{Model: "place/flash"})
	rig.fileChat(id)
	older := addKnows(t, rig.placesRig, id, "Lead with usage pricing")
	rig.advance(24 * time.Hour)
	var out knowsMutation
	if code := rig.do("POST", "/places/"+id+"/knows", map[string]any{"text": "Lead with free seat", "supersedes": older.Line.ID}, &out); code != 200 {
		t.Fatalf("add: %d", code)
	}
	if _, err := rig.p.Store.SetContext(id, placegraph.Context{Sources: []placegraph.Source{{Kind: placegraph.SourceURL, Ref: "https://codeaf.dev/docs"}}}); err != nil {
		t.Fatal(err)
	}
	token, _, _ := rig.open(map[string]any{})
	got := rig.using(token).Bundle
	if len(got.Instructions) != 1 || got.Instructions[0].PlaceID != id || got.Instructions[0].Text != "Lead with free seat" {
		t.Fatalf("instructions: %+v", got.Instructions)
	}
	if len(got.Sources) != 1 || got.Sources[0].From[0].PlaceID != id || len(got.Policy) != 1 || got.Policy[0].Wanted[0].PlaceID != id {
		t.Fatalf("origins: %+v", got)
	}
	// A plain resolver read proves the engine and the popover receive one truth.
	snap, _ := rig.p.Store.Snapshot()
	engine := session.PlaceGraphUsing(snap, rig.chat, nil, rig.door.Sources)
	if engine.Instructions[0].Text != got.Instructions[0].Text {
		t.Fatal("engine and Using diverged")
	}
}

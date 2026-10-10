package desktopbridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixtures are the REAL answers of the real handlers over a deterministic
// graph and world. The TypeScript client's tests parse these same files, so a
// wire change cannot pass one language and break the other: it fails here until
// the fixtures are regenerated (UPDATE_PLACES_FIXTURES=1 go test -run
// TestPlacesWireFixtures ./internal/desktopbridge/), and the regenerated files
// then have to satisfy the client's validators.
const placesFixtureDir = "../../desktop/src/features/places/fixtures"

func TestPlacesWireFixtures(t *testing.T) {
	rig := newPlacesRig(t)
	work := t.TempDir()
	if real, err := filepath.EvalSymlinks(work); err == nil {
		work = real
	}
	repo := filepath.Join(work, "repo")
	initGitFixture(t, repo)
	got := map[string][]byte{}
	capture := func(name, method, path string, body any, wantStatus int) json.RawMessage {
		t.Helper()
		var raw json.RawMessage
		if code := rig.do(method, path, body, &raw); code != wantStatus {
			t.Fatalf("%s: %s %s answered %d, want %d: %s", name, method, path, code, wantStatus, raw)
		}
		text := strings.ReplaceAll(string(raw), work, "/work")
		var tree any
		if err := json.Unmarshal([]byte(text), &tree); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pretty, _ := json.MarshalIndent(tree, "", "  ")
		got[name] = append(pretty, '\n')
		return raw
	}

	t0 := placesEpoch
	rig.setRows(
		waiting(row("sess-need", "Port fix to v1", t0.Add(-time.Hour)), "Allow the v1 branch push?"),
		working(row("sess-run", "Update fixtures", t0.Add(-time.Minute)), "7", "Update fixtures", t0.Add(-2*time.Minute)),
		row("sess-idle", "Naming the product", t0.Add(-26*time.Hour)),
		row("sess-loose", "Pricing for teams", t0.Add(-72*time.Hour)),
	)
	var created Mutation
	if err := json.Unmarshal(capture("create", "POST", "/places", map[string]any{"name": "Software", "tint": "iris", "instructions": "Keep strict mode the default."}, 200), &created); err != nil {
		t.Fatal(err)
	}
	software := created.Place.ID
	docs := rig.mk("Docs")
	cfg := rig.mk("Config parser", software)
	rel := rig.mk("Release", software)
	rig.do("POST", "/places/"+rel+"/parents", map[string]any{"add": docs}, nil) // a diamond
	capture("error-name-taken", "POST", "/places", map[string]any{"name": "software"}, 409)
	capture("members", "POST", "/places/"+cfg+"/members", map[string]any{"chats": []string{"sess-need", "sess-run"}}, 200)
	rig.do("POST", "/places/"+rel+"/members", map[string]any{"chats": []string{"sess-idle"}}, nil)
	capture("sources", "POST", "/places/"+cfg+"/sources", map[string]any{"kind": "repo", "ref": repo}, 200)
	rig.do("POST", "/places/"+software+"/pin", nil, nil)
	rig.do("POST", "/places/"+cfg+"/visit", nil, nil)
	capture("graph", "GET", "/places", nil, 200)
	capture("status", "GET", "/places/status", nil, 200)
	capture("rail", "GET", "/places/rail", nil, 200)
	capture("home-place", "GET", "/places/"+cfg, nil, 200)
	capture("home-parent", "GET", "/places/"+software, nil, 200)
	capture("home-root", "GET", "/places/root", nil, 200)
	capture("home-now", "GET", "/places/now", nil, 200)
	capture("chat-places", "GET", "/chats/sess-need/places", nil, 200)
	capture("delete-preview", "GET", "/places/"+cfg+"/delete-preview", nil, 200)
	capture("visit", "POST", "/places/"+rel+"/visit", nil, 200)
	var archived Mutation
	rig.do("POST", "/places/"+rel+"/archive", nil, &archived)
	capture("undo", "POST", "/places/undo", map[string]any{"receipts": archived.Undo}, 200)
	capture("error-unknown-chat", "POST", "/places/"+cfg+"/members", map[string]any{"chats": []string{"ghost"}}, 404)

	update := os.Getenv("UPDATE_PLACES_FIXTURES") == "1"
	if update {
		if err := os.MkdirAll(placesFixtureDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range got {
		file := filepath.Join(placesFixtureDir, name+".json")
		if update {
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("fixture %s is missing; regenerate with UPDATE_PLACES_FIXTURES=1: %v", name, err)
		}
		if !bytes.Equal(want, data) {
			t.Errorf("the wire answer %q changed; regenerate the fixtures (UPDATE_PLACES_FIXTURES=1) and check the TypeScript client still parses them\n--- committed\n%s\n--- now\n%s", name, want, data)
		}
	}
}

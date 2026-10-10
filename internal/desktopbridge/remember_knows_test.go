package desktopbridge

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func TestRememberKnowsDeleteCrossesTheEngineProcessBoundary(t *testing.T) {
	for _, route := range []string{"saved", "edited", "invalid"} {
		t.Run(route, func(t *testing.T) {
			rig := newPlacesRig(t)
			place := rig.mk("Release")
			child, err := placegraph.Open(placegraph.Options{Path: rig.path})
			if err != nil {
				t.Fatal(err)
			}
			line, _, err := child.AddLine(placegraph.Line{PlaceID: place, Text: "Run tests before launch", Source: placegraph.LineSource{Kind: placegraph.LineSaidInChat, ChatID: "chat"}})
			if err != nil {
				t.Fatal(err)
			}
			token := placegraph.RememberUndoToken(line)
			want := 200
			if route == "edited" {
				line.Text = "Changed by another window"
				if _, err := child.UpdateLine(line); err != nil {
					t.Fatal(err)
				}
				want = 409
			}
			if route == "invalid" {
				token = "invalid"
				want = 404
			}
			path, body := "/places/knows", map[string]any{"token": token}
			var result map[string]any
			if status := rig.do("DELETE", path, body, &result); status != want {
				t.Fatalf("Undo status %d: %+v", status, result)
			}
			snap, _ := rig.p.Store.Snapshot()
			if (len(snap.Knowledge(place)) == 0) != (want == 200) {
				t.Fatal("Undo left the line")
			}
		})
	}
}

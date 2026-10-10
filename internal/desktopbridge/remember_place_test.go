package desktopbridge

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func TestRememberPlaceUndoRoutesCrossTheEngineProcessBoundary(t *testing.T) {
	for _, route := range []string{"token", "receipts", "path"} {
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
			path, body := "/places/undo", map[string]any{"token": token, "ifGeneration": 0}
			if route == "receipts" {
				body = map[string]any{"receipts": []string{token}, "ifGeneration": 0}
			}
			if route == "path" {
				path, body = "/places/undo/"+token, map[string]any{"ifGeneration": 0}
			}
			var result map[string]any
			if status := rig.do("POST", path, body, &result); status != 200 {
				t.Fatalf("Undo status %d: %+v", status, result)
			}
			snap, _ := rig.p.Store.Snapshot()
			if len(snap.Knowledge(place)) != 0 {
				t.Fatal("Undo left the line")
			}
		})
	}
}

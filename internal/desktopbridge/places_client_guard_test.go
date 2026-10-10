package desktopbridge

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Guarded client requests must reach the real handlers rather than fail decoding.
func TestPlacesClientGenerationGuards(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Client guard")
	for _, path := range []string{"/places/undo", "/places/undo/missing", "/sessions"} {
		body := map[string]any{"ifGeneration": 0}
		if path == "/places/undo" {
			body["receipts"] = []string{"missing"}
		}
		if path == "/sessions" {
			body["placeId"] = id
		}
		var refusal placesError
		if code := rig.do("POST", path, body, &refusal); code != 409 || refusal.Error != "Your places changed in another window. Reload and try again." {
			t.Fatalf("%s = %d, %+v", path, code, refusal)
		}
	}
	// Stale choices stop before dereferencing the conversation or writing a pick.
	for _, apply := range []bool{false, true} {
		req := httptest.NewRequest("POST", "/using", strings.NewReader(`{"field":"model","placeId":"a","ifGeneration":0}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if apply {
			rig.b.places.apply(w, req, nil, "chat")
		} else {
			rig.b.places.choose(w, req, nil, "chat")
		}
		if w.Code != 409 {
			t.Fatalf("choice apply=%v = %d: %s", apply, w.Code, w.Body.String())
		}
	}
	agent := &referringAgent{}
	s := &conversation{conn: Connection{Agent: agent}}
	if got := dropOn(s, `{"path":"/work","ifGeneration":1}`); got.Code != 200 {
		t.Fatalf("source = %d: %s", got.Code, got.Body.String())
	}
}

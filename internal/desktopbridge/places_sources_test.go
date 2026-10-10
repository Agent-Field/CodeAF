package desktopbridge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// referringAgent records ReferPlace and nothing else; the embedded nil
// interface is never touched because chatSource asserts only the one door.
type referringAgent struct {
	Agent
	asked   []string
	arrival session.PlaceArrival
	err     error
}

func (a *referringAgent) ReferPlace(path string, arrival session.PlaceArrival) (session.PlaceRef, error) {
	a.asked = append(a.asked, path)
	a.arrival = arrival
	return session.PlaceRef{Path: path, Arrival: arrival}, a.err
}

func dropOn(s *conversation, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/sessions/x/sources", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.chatSource(w, r)
	return w
}

func TestChatOnlyDropRefersTheFolderToThatChat(t *testing.T) {
	agent := &referringAgent{}
	s := &conversation{conn: Connection{Agent: agent}}
	w := dropOn(s, `{"path":"/work/app"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if len(agent.asked) != 1 || agent.asked[0] != "/work/app" || agent.arrival != session.PlaceSaid {
		t.Fatalf("ReferPlace got %v %q, want /work/app said", agent.asked, agent.arrival)
	}
}

func TestChatOnlyDropRefusesAnEmptyPathAndAnAgentWithoutTheDoor(t *testing.T) {
	agent := &referringAgent{}
	if w := dropOn(&conversation{conn: Connection{Agent: agent}}, `{"path":" "}`); w.Code != 400 || len(agent.asked) != 0 {
		t.Fatalf("empty path: %d, asked %v", w.Code, agent.asked)
	}
	if w := dropOn(&conversation{conn: Connection{Agent: nil}}, `{"path":"/x"}`); w.Code != 409 {
		t.Fatalf("no door: %d", w.Code)
	}
	failing := &referringAgent{err: errors.New("not a folder")}
	if w := dropOn(&conversation{conn: Connection{Agent: failing}}, `{"path":"/x"}`); w.Code != 422 {
		t.Fatalf("refused: %d", w.Code)
	}
}

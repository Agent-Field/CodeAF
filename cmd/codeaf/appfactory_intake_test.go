package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// THE DESK'S DOOR IS IN-PROCESS: factory.intake_add files on the factory this
// engine hosts through inbox.Add, with no HTTP intake behind it. A repeat of
// the same (source, id) is the same job, a factory elsewhere is refused in
// words that say where work goes instead, a repository that is off is
// refused before anything is filed, and the factory itself answers no
// /api/intake route at all.
func TestTheDesksIntakeFilesOnThisMachinesFactoryWithoutHTTP(t *testing.T) {
	s := newLocalSetup(t)
	entry := func(title string) map[string]any {
		return map[string]any{"factory": appLocalID, "source": "inbox:ops", "external_id": "A1", "title": title, "priority": "p1"}
	}
	s.mustFail("factory.intake_add", entry("Fix the login timeout"), appwire.CodeNotFound)
	s.start()

	var first, again inbox.Result
	s.must("factory.intake_add", entry("Fix the login timeout"), &first)
	if first.ID < 1 || !first.Created {
		t.Fatalf("first filing = %+v, want a new job", first)
	}
	s.must("factory.intake_add", entry("Fix the login timeout on mobile"), &again)
	if again.ID != first.ID || again.Created {
		t.Fatalf("repeat = %+v, want {id:%d created:false}", again, first.ID)
	}
	host, err := s.backend.localHostOf(appLocalID)
	if err != nil {
		t.Fatal(err)
	}
	it, err := host.floor.Get(first.ID)
	if err != nil || it.SourceRef != "inbox:ops:A1" || it.Title != "Fix the login timeout on mobile" {
		t.Fatalf("job %d = %+v, %v", first.ID, it, err)
	}

	elsewhere := entry("Fix it elsewhere")
	elsewhere["factory"] = "factory_elsewhere"
	if msg := s.mustFail("factory.intake_add", elsewhere, appwire.CodeNotFound); !strings.Contains(msg, "through AgentField") {
		t.Fatalf("a factory elsewhere said %q", msg)
	}

	if err := host.floor.SetRepos([]string{"acme/web"}); err != nil {
		t.Fatal(err)
	}
	off := entry("Fix the API")
	off["external_id"], off["repo"] = "A2", "acme/api"
	if msg := s.mustFail("factory.intake_add", off, appwire.CodeConflict); msg != factory.RepoOffWords("acme/api") {
		t.Fatalf("a repository that is off said %q", msg)
	}

	handler, err := host.solo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/intake", "/api/intake/inbox:ops/A1", "/api/intake/inbox:ops/A1/messages", "/v1/webhooks"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"source":"inbox:ops","external_id":"A3","title":"x"}`))
		r.Header.Set("Authorization", "Bearer "+host.token)
		handler.ServeHTTP(w, r)
		// No route takes it: an unknown path is 404, and one under the
		// page's catch-all is 405 for a POST.
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s with the access token answered %d, want 404 or 405: the factory has no HTTP intake", path, w.Code)
		}
	}
	if all, err := host.floor.ListSaved(); err != nil || len(all) != 1 {
		t.Fatalf("the floor holds %d jobs (%v) after the HTTP posts, want only the desk's one", len(all), err)
	}
}

// The removed commands say where the work goes now rather than falling
// through to `codeaf factory serve`.
func TestTheRemovedFactoryCommandsSayWhereWorkGoes(t *testing.T) {
	old := usageErr
	t.Cleanup(func() { usageErr = old })
	for _, verb := range []string{"add", "webhook"} {
		var said strings.Builder
		usageErr = &said
		err := runFactory([]string{verb, "--source", "inbox:ops"})
		var status exitStatus
		if !errors.As(err, &status) || strings.TrimSpace(said.String()) != factoryRemovedDoor {
			t.Fatalf("codeaf factory %s = %v, said %q; want %q", verb, err, said.String(), factoryRemovedDoor)
		}
	}
}

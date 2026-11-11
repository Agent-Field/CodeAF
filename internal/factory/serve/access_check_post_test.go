package serve

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestAboutAdvertisesAccessCheckPost(t *testing.T) {
	f, _ := linkFixture(t)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/about", nil))
	var about map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &about); err != nil || about["access_check_post"] != true {
		t.Fatalf("about = %d %s", w.Code, w.Body)
	}
	// An optional capability: the global contract does not move.
	if int(about["contract"].(float64)) != api.Contract {
		t.Fatalf("contract = %v", about["contract"])
	}
}

// A POST check is a read: with no flow it starts no sign-in, mints no cookie and keeps no state.
func TestPostCheckNeverStartsAuthOrMintsACookie(t *testing.T) {
	f, _ := linkFixture(t)
	made, _ := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	w, out := joinLinkRequest(t, f.h, "POST", made.URL, "check", nil)
	if out.State != "ready" {
		t.Fatalf("check = %+v", out)
	}
	if len(w.Result().Cookies()) != 0 || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("a check minted a cookie: %v", w.Header())
	}
	f.h.accessMu.Lock()
	flows := len(f.h.accessFlows)
	f.h.accessMu.Unlock()
	f.h.auth.mu.Lock()
	devices := len(f.h.auth.pending)
	f.h.auth.mu.Unlock()
	if flows != 0 || devices != 0 || out.SignInURL != "" || out.UserCode != "" {
		t.Fatalf("a check started a sign-in: flows=%d devices=%d out=%+v", flows, devices, out)
	}
	if got := f.links(t)[made.ID]; got.Used != 0 {
		t.Fatalf("a check spent a use: %+v", got)
	}
}

// The check polls the caller's own flow by cookie and neither restarts nor resets it.
func TestPostCheckPollsOnlyTheCallersFlow(t *testing.T) {
	f, g := linkFixture(t)
	made, _ := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	g.login = "mona"
	w, out := joinLinkRequest(t, f.h, "POST", made.URL, "sign_in", nil)
	if out.State != "waiting" {
		t.Fatalf("sign_in = %+v", out)
	}
	cookies := w.Result().Cookies()
	// Another caller, no cookie: not joined, and nothing is started for it.
	_, other := joinLinkRequest(t, f.h, "POST", made.URL, "check", nil)
	if other.State != "ready" {
		t.Fatalf("another caller = %+v", other)
	}
	// Before GitHub answers, the owner's check keeps waiting on the same code.
	_, again := joinLinkRequest(t, f.h, "POST", made.URL, "check", cookies)
	if again.State != "waiting" || again.UserCode != out.UserCode {
		t.Fatalf("poll reset the flow: %+v vs %+v", again, out)
	}
	f.now = f.now.Add(10 * time.Second)
	_, joined := joinLinkRequest(t, f.h, "POST", made.URL, "check", cookies)
	if joined.State != "joined" || joined.AccessToken == "" {
		t.Fatalf("owner poll = %+v", joined)
	}
}

func TestAccessVerbEnumIsStrict(t *testing.T) {
	f, _ := linkFixture(t)
	made, _ := f.makeLink(t, api.CreateAccessLink{})
	for _, verb := range []string{"", "poll", "CHECK", "check ", "sign-in"} {
		r := httptest.NewRequest("POST", "/v1/factory-access", strings.NewReader(`{"link":"`+made.URL+`","verb":"`+verb+`"}`))
		r.RemoteAddr = "203.0.113.12:4567"
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 422 {
			t.Errorf("verb %q = %d %s", verb, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), made.URL) {
			t.Errorf("verb %q echoes the link", verb)
		}
	}
}

// Legacy GET still answers, and still polls, for factories that older apps talk to.
func TestLegacyGetCheckStillPolls(t *testing.T) {
	f, g := linkFixture(t)
	made, _ := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	g.login = "sam"
	if out := f.cardSignIn(t, made.URL); out.State != "joined" {
		t.Fatalf("legacy poll = %+v", out)
	}
}

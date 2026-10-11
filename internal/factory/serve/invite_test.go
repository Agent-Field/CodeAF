package serve

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

func TestInviteTokenSessionShowsWhoInvitedAndDiesWithTheRow(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "tokens.json")
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	row.InvitedBy = "sam"
	row.Expires = now.Add(time.Hour)
	if err := tokens.Save(path, []tokens.Token{row}); err != nil {
		t.Fatal(err)
	}
	h := New(factory.FixtureSeam(now), Config{AccessToken: "floor-secret", TokensPath: path})
	defer h.Close()
	h.sessions.now = func() time.Time { return now }

	login := accessRequest(h, "POST", "/access", row.Token, nil, false)
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("invite login = %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	me := accessRequest(h, "GET", "/v1/me", "", cookie, false)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"invited_by":"sam"`) {
		t.Fatalf("session me = %d %s", me.Code, me.Body.String())
	}
	bearer := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	bearer.RemoteAddr = "192.0.2.1:9"
	bearer.Header.Set("Authorization", "Bearer "+row.Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearer)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"invited_by":"sam"`) {
		t.Fatalf("bearer me = %d %s", w.Code, w.Body.String())
	}
	floor := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	floor.RemoteAddr = "192.0.2.1:9"
	floor.Header.Set("Authorization", "Bearer floor-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, floor)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "invited_by") {
		t.Fatalf("floor token named an inviter: %d %s", w.Code, w.Body.String())
	}
	revoke := httptest.NewRequest(http.MethodPost, "/v1/access/revoke-sessions", nil)
	revoke.RemoteAddr = "192.0.2.1:9"
	revoke.Header.Set("Authorization", "Bearer "+row.Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, revoke)
	if w.Code != http.StatusUnauthorized || len(h.sessions.ids) != 1 {
		t.Fatalf("invite token revoked sessions: %d, sessions %d", w.Code, len(h.sessions.ids))
	}

	now = now.Add(time.Hour)
	if w := accessRequest(h, "GET", "/v1/queue", "", cookie, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired invite session = %d", w.Code)
	}
	now = now.Add(-time.Hour)
	// The expired-session check above deleted the cookie. Sign in again, then
	// revoke the row out from under the new session.
	login = accessRequest(h, "POST", "/access", row.Token, nil, false)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("relogin after moving the clock back = %d %s", login.Code, login.Body.String())
	}
	cookie = login.Result().Cookies()[0]
	if err := tokens.Save(path, nil); err != nil {
		t.Fatal(err)
	}
	if w := accessRequest(h, "GET", "/v1/me", "", cookie, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked invite session = %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, bearer)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked invite bearer = %d", w.Code)
	}
}

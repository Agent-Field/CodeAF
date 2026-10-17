package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

func TestAccessTokenSessionDiesWithTheRow(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "tokens.json")
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	row.Expires = now.Add(time.Hour)
	if err := tokens.Save(path, []tokens.Token{row}); err != nil {
		t.Fatal(err)
	}
	h := New(factory.FixtureSeam(now), Config{AccessToken: "floor-secret", TokensPath: path})
	defer h.Close()
	h.sessions.now = func() time.Time { return now }

	login := accessRequest(h, "POST", "/access", row.Token, nil, false)
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("token login = %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	me := accessRequest(h, "GET", "/v1/me", "", cookie, false)
	if me.Code != http.StatusOK {
		t.Fatalf("session me = %d %s", me.Code, me.Body.String())
	}
	bearer := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	bearer.RemoteAddr = "192.0.2.1:9"
	bearer.Header.Set("Authorization", "Bearer "+row.Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearer)
	if w.Code != http.StatusOK {
		t.Fatalf("bearer me = %d %s", w.Code, w.Body.String())
	}
	floor := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	floor.RemoteAddr = "192.0.2.1:9"
	floor.Header.Set("Authorization", "Bearer floor-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, floor)
	if w.Code != http.StatusOK {
		t.Fatalf("floor me = %d %s", w.Code, w.Body.String())
	}
	revoke := httptest.NewRequest(http.MethodPost, "/v1/access/revoke-sessions", nil)
	revoke.RemoteAddr = "192.0.2.1:9"
	revoke.Header.Set("Authorization", "Bearer "+row.Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, revoke)
	if w.Code != http.StatusUnauthorized || len(h.sessions.ids) != 1 {
		t.Fatalf("access token revoked sessions: %d, sessions %d", w.Code, len(h.sessions.ids))
	}

	now = now.Add(time.Hour)
	if w := accessRequest(h, "GET", "/v1/queue", "", cookie, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token session = %d", w.Code)
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
		t.Fatalf("revoked token session = %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, bearer)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token bearer = %d", w.Code)
	}
}

func TestRefusedLinkNamesWhy(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "tokens.json")
	expired, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	expired.InvitedBy = "santosh"
	expired.Created = now.Add(-48 * time.Hour)
	expired.Expires = now.Add(-time.Hour)
	revoked, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	revoked.InvitedBy = "@santosh"
	revoked.Created = now.Add(-time.Hour)
	if err := tokens.Save(path, []tokens.Token{expired, revoked}); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Remove(path, revoked.ID); err != nil {
		t.Fatal(err)
	}
	h := New(factory.FixtureSeam(now), Config{AccessToken: "floor-secret", TokensPath: path})
	defer h.Close()
	h.sessions.now = func() time.Time { return now }

	check := func(secret, reason, invited string, created time.Time) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		r.RemoteAddr = "192.0.2.1:9"
		if secret != "" {
			r.Header.Set("Authorization", "Bearer "+secret)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s = %d %s", reason, w.Code, w.Body.String())
		}
		var body struct {
			Error     string     `json:"error"`
			Reason    string     `json:"reason"`
			InvitedBy string     `json:"invited_by"`
			CreatedAt *time.Time `json:"created_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Reason != reason || body.InvitedBy != invited {
			t.Fatalf("%s body = %+v", reason, body)
		}
		if created.IsZero() {
			if body.CreatedAt != nil {
				t.Fatalf("%s created = %v", reason, body.CreatedAt)
			}
			return
		}
		if body.CreatedAt == nil || !body.CreatedAt.Equal(created) {
			t.Fatalf("%s created = %v", reason, body.CreatedAt)
		}
	}
	check(expired.Token, "expired", "@santosh", expired.Created)
	check(revoked.Token, "revoked", "@santosh", revoked.Created)
	check("not-a-token", "unknown", "", time.Time{})
	plain := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	plain.RemoteAddr = "192.0.2.1:9"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, plain)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "a bearer access token or live session is required") || strings.Contains(w.Body.String(), `"reason"`) {
		t.Fatalf("no secret = %d %s", w.Code, w.Body.String())
	}
	form := httptest.NewRequest(http.MethodPost, "/access", strings.NewReader("token="+expired.Token))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form.RemoteAddr = "192.0.2.1:9"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, form)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"reason":"expired"`) {
		t.Fatalf("access form = %d %s", w.Code, w.Body.String())
	}
}

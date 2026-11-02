package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/scout"
)

func scoutFloor(t *testing.T, look func(ctx context.Context, id int, refresh bool) (factory.Scout, bool, error)) *Handler {
	t.Helper()
	seam := factory.Seam{Load: func() (factory.Snapshot, error) {
		return factory.Snapshot{Items: []factory.Item{{ID: 5, Repo: "acme/api", Kind: factory.KindIssue, Title: "retry", State: factory.StateNew}},
			Repos: []factory.Repo{{Name: "acme/api"}}}, nil
	}}
	h := New(seam, Config{Address: "http://factory", Version: "test", Scout: look})
	t.Cleanup(h.Close)
	return h
}

func postScout(h *Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs/5/scout", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLookCloserAnswersTheScoutAndWhetherItWasKept(t *testing.T) {
	var asked []bool
	h := scoutFloor(t, func(_ context.Context, id int, refresh bool) (factory.Scout, bool, error) {
		asked = append(asked, refresh)
		return factory.Scout{Plan: []string{"a"}, Calls: 2, Head: "abc"}, !refresh, nil
	})
	w := postScout(h, `{"refresh": true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	var got api.ScoutLook
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Job != 5 || got.Cached || got.Scout.Calls != 2 || len(asked) != 1 || !asked[0] {
		t.Fatalf("look = %+v asked %v", got, asked)
	}
}

func TestLookCloserSaysItsLimitInWords(t *testing.T) {
	h := scoutFloor(t, func(context.Context, int, bool) (factory.Scout, bool, error) {
		return factory.Scout{}, false, scout.ErrDayLimit
	})
	w := postScout(h, `{}`)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "Daily limit for this repo reached") {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
}

func TestAFactoryWithoutAScoutSaysSo(t *testing.T) {
	h := scoutFloor(t, nil)
	if w := postScout(h, `{}`); w.Code == http.StatusOK {
		t.Fatalf("a factory with no scout answered %d", w.Code)
	}
}

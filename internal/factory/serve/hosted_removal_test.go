package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/coder/websocket"
)

// VALIDATION CONTRACT, written before the code it checks:
//   - When the operator removes a person, the model door refuses the bearer of
//     every chat that person owned, at once: a removed person's chat spends
//     nothing and reaches no git host as them.
//   - Their chats are gone from the chats' host and from the registry, so a
//     guest cannot wake one, and their Linux user and folder are freed.
//   - Their stored keys are forgotten.
//   - Nobody else is touched: another person's chat keeps its model access.
//   - The chats' host being down never blocks a removal, and the bearers are
//     refused anyway.
//   - A chat the host could not delete is closed instead: a person it was
//     shared with can neither read it nor attach to it, because waking it
//     would hand its removed owner a fresh model bearer.

func TestRemovingAPersonEndsTheirHostedChatsAndKeys(t *testing.T) {
	rg := newHostedRig(t)
	rg.withServerKey(t, rigServerKey)
	first, second := rg.create(t, "mia"), rg.create(t, "mia")
	kept := rg.create(t, "santosh")
	miaToken, secondToken, keptToken := rg.host.tokenOf(first), rg.host.tokenOf(second), rg.host.tokenOf(kept)
	if w := rg.putKey("mia", "openrouter", "sk-mia-own-key-0001"); w.Code != 204 {
		t.Fatalf("storing mia's key = %d %s", w.Code, w.Body)
	}

	// Controls: before the removal every one of these works, so a refusal after it is the removal's doing.
	for name, token := range map[string]string{"mia first": miaToken, "mia second": secondToken, "santosh": keptToken} {
		if code := rg.model(token); code != http.StatusOK {
			t.Fatalf("before the removal, %s's chat model call = %d", name, code)
		}
	}
	if key, own, err := rg.h.hostedKeyFor("mia", "openrouter"); key == "" || !own || err != nil {
		t.Fatalf("before the removal, mia's key = %q, %v, %v", key, own, err)
	}

	if w := rg.send("owner", "DELETE", "/v1/people/mia/sessions", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("removing mia = %d %s", w.Code, w.Body)
	}

	for name, token := range map[string]string{"mia first": miaToken, "mia second": secondToken} {
		if code := rg.model(token); code != http.StatusUnauthorized {
			t.Errorf("after the removal, %s's chat model call = %d, want 401", name, code)
		}
	}
	if code := rg.model(keptToken); code != http.StatusOK {
		t.Errorf("another person's chat model call after mia's removal = %d, want 200", code)
	}
	for _, id := range []string{first, second} {
		if owner := rg.host.ownerOf(id); owner != "" {
			t.Errorf("chat %s is still on the chats' host, owned by %q", id, owner)
		}
		if _, found, err := rg.st.Resource(store.SessionKind, id); err != nil || found {
			t.Errorf("chat %s is still in the registry (found=%v, err=%v)", id, found, err)
		}
	}
	if owner := rg.host.ownerOf(kept); owner != "santosh" {
		t.Errorf("santosh's chat after mia's removal is owned by %q", owner)
	}
	if key, own, _ := rg.h.hostedKeyFor("mia", "openrouter"); key != "" || own {
		t.Errorf("mia's stored key survived her removal: %q, %v", key, own)
	}
}

func TestRemovingAPersonWorksWhileTheChatsHostIsDown(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "mia")
	token := rg.host.tokenOf(chat)
	if code := rg.model(token); code != http.StatusOK {
		t.Fatalf("before the removal, the chat's model call = %d", code)
	}
	rg.hostSrv.Close()
	if w := rg.send("owner", "DELETE", "/v1/people/mia/sessions", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("removing mia with the chats' host down = %d %s", w.Code, w.Body)
	}
	if code := rg.model(token); code != http.StatusUnauthorized {
		t.Errorf("after a removal made while the host was down, the chat's model call = %d, want 401", code)
	}
}

func TestAGuestCannotWakeARemovedPersonsChatThatTheHostCouldNotDelete(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "mia")
	hostedGrant(t, rg, "mia", chat, "santosh", "edit")
	kept := rg.create(t, "santosh")
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	attach := func(who, id string) (int, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		conn, res, err := websocket.Dial(ctx, wireURL(srv, id), &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": {"Bearer " + rg.tokens[who]}},
		})
		if conn != nil {
			_ = conn.CloseNow()
		}
		if res == nil {
			return 0, err
		}
		return res.StatusCode, err
	}

	// Control: before the removal the guest reads the chat and attaches to it, so a refusal after is the removal's doing.
	if w := rg.send("santosh", http.MethodGet, hostedSessionsPath+"/"+chat, nil, nil); w.Code != http.StatusOK {
		t.Fatalf("before the removal, the guest's read of the chat = %d %s", w.Code, w.Body)
	}
	if code, err := attach("santosh", chat); code != http.StatusSwitchingProtocols {
		t.Fatalf("before the removal, the guest's attach = %d, %v", code, err)
	}

	rg.hostSrv.Close()
	if w := rg.send("owner", "DELETE", "/v1/people/mia/sessions", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("removing mia with the chats' host down = %d %s", w.Code, w.Body)
	}

	if w := rg.send("santosh", http.MethodGet, hostedSessionsPath+"/"+chat, nil, nil); w.Code != http.StatusNotFound || w.Body.String() != hostNoSuchChat {
		t.Errorf("after the removal, the guest's read of the chat = %d %s, want 404 %q", w.Code, w.Body, hostNoSuchChat)
	}
	if code, _ := attach("santosh", chat); code != http.StatusNotFound {
		t.Errorf("after the removal, the guest's attach = %d, want 404", code)
	}
	row, found, err := rg.st.Resource(store.SessionKind, chat)
	if err != nil || !found {
		t.Fatalf("the chat the host could not delete must stay tracked in the registry: found=%v, err=%v", found, err)
	}
	if row.Visibility != acl.Private {
		t.Errorf("the closed chat's visibility = %q, want %q", row.Visibility, acl.Private)
	}
	if grants, err := rg.st.GrantsOn(store.SessionKind, chat); err != nil || len(grants) != 0 {
		t.Errorf("the closed chat still has shares: %+v, %v", grants, err)
	}
	if _, found, err := rg.st.Resource(store.SessionKind, kept); err != nil || !found {
		t.Errorf("another person's chat was touched by mia's removal: found=%v, err=%v", found, err)
	}
}

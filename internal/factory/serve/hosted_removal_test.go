package serve

import (
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/store"
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

package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// P2-8
// The latest guest asker appears on the owner's spend rows, and the owner's own turns leave asker empty.
func TestP2_8_HostedSpendRecordsGuestWithoutMovingTheCharge(t *testing.T) {
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		return meteredAnswer(ctx, model, "paid", 0.25), nil
	})
	chat := rg.create(t, "santosh")
	state := rg.h.config.Hosted.state
	for _, asker := range []string{"", "abir", "santosh"} {
		if asker != "" {
			state.noteAsker(chat, asker)
		}
		if w := callHostedModel(rg, chat, testHostedBody); w.Code != http.StatusOK {
			t.Fatalf("model call as asker %q = %d %s", asker, w.Code, w.Body)
		}
	}
	space := myHostedSpace(t, rg, "santosh")
	rows := hostedSpendRows(t, rg, space.Workstream.ID)
	if len(rows) != 3 || space.SpentUSD != 0.75 {
		t.Fatalf("owner's space spent $%g in %d rows", space.SpentUSD, len(rows))
	}
	for i, want := range []string{"", "abir", ""} {
		if row := rows[i]; row.By != "santosh" || row.Asker != want || row.Chat != "hosted:"+chat || row.USD != 0.25 {
			t.Errorf("ledger row %d = %+v, want asker %q", i, row, want)
		}
	}
	if guest := myHostedSpace(t, rg, "abir"); guest.SpentUSD != 0 || len(hostedSpendRows(t, rg, guest.Workstream.ID)) != 0 {
		t.Fatalf("guest's space spent $%g", guest.SpentUSD)
	}
}

// P2-8
// Pushing a chat's model access on behalf of a guest keeps that guest's GitHub credential out of the owner's keys.
func TestP2_8_HostedPushKeepsOnlyTheOwnerSignInGitHub(t *testing.T) {
	rg := newHostedRig(t)
	rg.withServerKey(t, rigServerKey)
	chat := rg.create(t, "santosh")
	rg.host.adopt(chat, "santosh")
	push := func(who string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, hostedSessionsPath+"/"+chat+"/wire", nil)
		r.Header.Set("Authorization", "Bearer "+rg.tokens[who])
		w := httptest.NewRecorder()
		if !rg.h.hostedPush(w, r, rg.h.config.Hosted.state, chat, "santosh") {
			t.Fatalf("push as %s = %d %s", who, w.Code, w.Body)
		}
	}
	guestRequest := httptest.NewRequest(http.MethodGet, hostedSessionsPath, nil)
	guestRequest.Header.Set("Authorization", "Bearer "+rg.tokens["abir"])
	guestKey, signIn := rg.h.personGitHubToken(guestRequest)
	if !signIn || len(guestKey) < 8 {
		t.Fatal("guest fixture has no sign-in credential")
	}
	push("abir")
	if key, exists, err := rg.h.hostedKeyFor("santosh", "github"); err != nil || exists || key != "" {
		t.Fatalf("guest push stored a GitHub credential for the owner, present=%v, error=%v", exists, err)
	}
	pushes := rg.host.pushList()
	if len(pushes) != 2 || pushes[1].person != "santosh" || pushes[1].id != chat || !strings.HasPrefix(pushes[1].token, "cmt_") {
		t.Fatalf("guest push did not restore model access for the owner: %d pushes", len(pushes))
	}
	push("santosh")
	ownerRequest := httptest.NewRequest(http.MethodGet, hostedSessionsPath, nil)
	ownerRequest.Header.Set("Authorization", "Bearer "+rg.tokens["santosh"])
	ownerKey, signIn := rg.h.personGitHubToken(ownerRequest)
	if key, exists, err := rg.h.hostedKeyFor("santosh", "github"); err != nil || !exists || !signIn || key != ownerKey || key == "" {
		t.Fatalf("owner push did not keep the owner's sign-in credential, present=%v, error=%v", exists, err)
	}
}

// P2-9
// An organization setting takes precedence over the configured cap at the model door.
func TestP2_9_PolicyOverridesHostedSessionCap(t *testing.T) {
	calls := 0
	rg := newMeteredRig(t, 9, func(ctx context.Context, _, model string) (*ai.Response, error) {
		calls++
		return meteredAnswer(ctx, model, "paid", 0.25), nil
	})
	chat := rg.create(t, "santosh")
	policy := acl.DefaultPolicy()
	policy.Hosting.MaxSessionCostUSD = 0.25
	rg.do(t, "owner", http.MethodPut, "/v1/org/policy", 0, policy, http.StatusOK, nil)
	if w := callHostedModel(rg, chat, testHostedBody); w.Code != http.StatusOK {
		t.Fatalf("first call = %d %s", w.Code, w.Body)
	}
	hostedModelErrorIs(t, callHostedModel(rg, chat, testHostedBody), 402, "insufficient_quota",
		"This chat has reached its spending limit of $0.25, so it is paused. Start a new chat to carry on.")
	rg.setAccessPolicy(t, func(p *acl.Policy) { p.Hosting.MaxSessionCostUSD = 0.50 })
	if w := callHostedModel(rg, chat, testHostedBody); w.Code != http.StatusOK {
		t.Fatalf("call after raising the cap = %d %s", w.Code, w.Body)
	}
	hostedModelErrorIs(t, callHostedModel(rg, chat, testHostedBody), 402, "insufficient_quota",
		"This chat has reached its spending limit of $0.50, so it is paused. Start a new chat to carry on.")
	if calls != 2 {
		t.Fatalf("made %d upstream calls", calls)
	}
}

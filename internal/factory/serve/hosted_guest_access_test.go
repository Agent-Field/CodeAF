package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/coder/websocket"
)

// P2-6
// A bot's chats are its creator's; its own scope and the creator's current rights govern the hosted routes.
func TestP2_6_BotsUseCurrentHostedRights(t *testing.T) {
	rg := newHostedRig(t)
	full := rg.contractBot(t, "abir", "Full bot", "full")
	view := rg.contractBot(t, "abir", "View bot", "view")
	for _, path := range []string{"/v1/hosted/keys", "/v1/hosted/git", "/v1/hosted/keys/github"} {
		w := rg.botDo(t, full.Token, http.MethodGet, path, -1, nil, http.StatusForbidden, nil)
		assertBotRefusal(t, w, "a bot cannot use this yet")
	}
	actualGitPath := "/v1/hosted/git/github.com/org/repo/info/refs?service=git-upload-pack"
	wantGitSentence(t, rg.botDo(t, full.Token, http.MethodGet, actualGitPath, -1, nil, http.StatusUnauthorized, nil),
		http.StatusUnauthorized, "Sign in to use git here.")

	var made struct{ ID string }
	rg.botDo(t, full.Token, http.MethodPost, hostedSessionsPath, -1, nil, http.StatusCreated, &made)
	row, found, err := rg.st.Resource(store.SessionKind, made.ID)
	if err != nil || !found || row.Owner != (acl.Subject{Kind: "person", ID: "abir"}) ||
		row.CreatedBy != (acl.Principal{Kind: "bot", ID: full.Bot.ID, Sponsor: "abir"}) ||
		row.Visibility != acl.Private || rg.host.ownerOf(made.ID) != "abir" {
		t.Fatalf("registered bot chat = %+v, found %v, host owner %q, %v", row, found, rg.host.ownerOf(made.ID), err)
	}
	for _, person := range []string{"abir", "santosh", "mia", "owner"} {
		var listed struct{ Sessions, Shared []struct{ ID string } }
		rg.do(t, person, http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &listed)
		if person == "abir" {
			if len(listed.Sessions) != 1 || listed.Sessions[0].ID != made.ID {
				t.Fatalf("creator did not list the chat: %+v", listed)
			}
		} else if len(listed.Sessions)+len(listed.Shared) != 0 {
			t.Errorf("%s listed a private bot chat: %+v", person, listed)
		}
	}
	madePath := hostedSessionsPath + "/" + made.ID
	rg.botDo(t, view.Token, http.MethodGet, madePath, -1, nil, http.StatusOK, nil)
	rg.botDo(t, view.Token, http.MethodPost, madePath+"/stop", -1, nil, http.StatusForbidden, nil)
	rg.botDo(t, view.Token, http.MethodDelete, madePath, -1, nil, http.StatusForbidden, nil)

	chat := rg.create(t, "mia")
	grant := hostedGrant(t, rg, "mia", chat, "abir", "edit")
	var listed struct{ Sessions, Shared []struct{ ID string } }
	rg.botDo(t, view.Token, http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != made.ID ||
		len(listed.Shared) != 1 || listed.Shared[0].ID != chat {
		t.Fatalf("scoped bot list = %+v", listed)
	}

	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	ownClient, _ := guestConnect(t, srv, opened, view.Token, made.ID)
	guestWrite(t, ownClient, guestCall(1, "Submit"))
	guestRefusal(t, ownClient, 1, guestRunWords)
	sharedClient, sharedHost := guestConnect(t, srv, opened, full.Token, chat)
	guestWrite(t, sharedClient, guestCall(2, "Steer"))
	if got := guestHostLine(t, sharedHost); got != guestCall(2, "Steer") {
		t.Fatalf("shared bot did not get its creator's rights: %q", got)
	}

	rg.do(t, "mia", http.MethodDelete, "/v1/resources/session/"+chat+"/grants/"+grant.ID, -1, nil, http.StatusNoContent, nil)
	guestAccessRemoved(t, sharedClient, sharedHost)
	w := rg.botDo(t, full.Token, http.MethodGet, hostedSessionsPath+"/"+chat, -1, nil, http.StatusNotFound, nil)
	if w.Body.String() != hostNoSuchChat {
		t.Fatalf("bot read after revocation = %q", w.Body)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, res, err := websocket.Dial(ctx, wireURL(srv, chat), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + full.Token}},
	})
	if err == nil || res == nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("bot attach after revocation = %v, %v", res, err)
	}
	_ = ownClient.CloseNow()

	rg.botDo(t, full.Token, http.MethodPost, madePath+"/stop", -1, nil, http.StatusNoContent, nil)
	rg.botDo(t, full.Token, http.MethodDelete, madePath, -1, nil, http.StatusNoContent, nil)
	rg.setAccessPolicy(t, func(p *acl.Policy) { p.Agents.MayCreate = false })
	before := rg.host.count()
	w = rg.botDo(t, full.Token, http.MethodPost, hostedSessionsPath, -1, nil, http.StatusForbidden, nil)
	assertBotRefusal(t, w, "agents cannot create resources under the organization policy")
	if got := rg.host.count(); got != before {
		t.Fatalf("refused bot create reached the host: %d requests", got-before)
	}
}

// P2-6
// A bot's open guest wire follows its current scope and ends when its credential is revoked.
func TestP2_6_BotWireRefreshesScopeAndToken(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "full")
	bot := rg.contractBot(t, "abir", "Editor", "edit")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client, host := guestConnect(t, srv, opened, bot.Token, chat)
	guestWrite(t, client, guestCall(1, "Submit"))
	if got := guestHostLine(t, host); got != guestCall(1, "Submit") {
		t.Fatalf("host received %q for the bot's first message", got)
	}
	rg.setAccessPolicy(t, func(p *acl.Policy) {
		for i := range p.Roles {
			if p.Roles[i].Name == "edit" {
				p.Roles[i].Verbs = slices.DeleteFunc(p.Roles[i].Verbs, func(v string) bool { return v == "run" })
			}
		}
	})
	time.Sleep(2 * guestTestHeartbeat)
	guestWrite(t, client, guestCall(2, "Submit"))
	guestRefusal(t, client, 2, guestRunWords)

	path := "/v1/bots/" + bot.Bot.ID + "/tokens/" + bot.Bot.Tokens[0].ID
	rg.do(t, "abir", http.MethodDelete, path, -1, nil, http.StatusOK, nil)
	guestAccessRemoved(t, client, host)
}

// P2-5
// A signed-out person loses their open guest wire even when they still have a grant.
func TestP2_5_SignOutClosesGuestWire(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client, host := guestConnect(t, srv, opened, rg.tokens["abir"], chat)
	var sessions struct {
		Sessions []struct {
			ID      string
			Current bool
		}
	}
	rg.do(t, "abir", http.MethodGet, "/v1/me/sessions", -1, nil, http.StatusOK, &sessions)
	for _, session := range sessions.Sessions {
		if session.Current {
			rg.do(t, "abir", http.MethodDelete, "/v1/me/sessions/"+session.ID, -1, nil, http.StatusOK, nil)
			guestAccessRemoved(t, client, host)
			return
		}
	}
	t.Fatal("could not find the guest's sign-in session")
}

// P2-8
// A guest's allowed calls record who asked while model spend and git credentials stay with the chat's owner.
func TestP2_8_HostedWireStampsSpendAndAudit(t *testing.T) {
	rg := newMeteredRig(t, 5, func(ctx context.Context, key, model string) (*ai.Response, error) {
		if key != testHostedOrgKey {
			t.Errorf("model used a different credential: %q", key)
		}
		return meteredAnswer(ctx, model, "answer", 0.05), nil
	})
	chat := rg.create(t, "santosh")
	rg.host.adopt(chat, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "edit")
	hostedGrant(t, rg, "santosh", chat, "mia", "view")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	rg.up.mu.Lock()
	rg.up.handler = func(w http.ResponseWriter, r *http.Request) {
		if _, key, ok := r.BasicAuth(); !ok || key != personGitKey {
			t.Error("git did not use the owner's credential")
		}
		_, _ = io.WriteString(w, "0000")
	}
	rg.up.mu.Unlock()
	rg.h.config.Hosted.state.gitHosts = []string{rg.upSrv.URL}
	previous := hostedKeyForHook
	hostedKeyForHook = func(person, provider string) (string, bool, error) {
		if person == "santosh" && provider == "github" {
			return personGitKey, true, nil
		}
		return "", false, nil
	}
	t.Cleanup(func() { hostedKeyForHook = previous })
	guest, host := guestConnect(t, srv, opened, rg.tokens["abir"], chat)
	pushes := rg.host.pushList()
	if len(pushes) != 2 || pushes[1].id != chat || pushes[1].person != "santosh" ||
		pushes[1].token == pushes[0].token || pushes[1].token != rg.host.tokenOf(chat) {
		t.Fatal("guest attach did not renew the owner's model access")
	}
	space := myHostedSpace(t, rg, "santosh")
	charge := func() {
		t.Helper()
		if w := callHostedModel(rg, chat, testHostedBody); w.Code != http.StatusOK {
			t.Fatalf("model charge: %d %s", w.Code, w.Body)
		}
	}
	git := func() {
		t.Helper()
		host := strings.TrimPrefix(rg.upSrv.URL, "http://")
		res := rg.send("", http.MethodGet, "/v1/hosted/git/"+host+"/org/repo/info/refs?service=git-upload-pack",
			nil, map[string]string{"Authorization": gitBasic(rg.host.tokenOf(chat))})
		if res.Code != http.StatusOK {
			t.Fatalf("git = %d %s", res.Code, res.Body)
		}
	}

	viewer, _ := guestConnect(t, srv, opened, rg.tokens["mia"], chat)
	guestWrite(t, viewer, guestCall(1, "Steer"))
	guestRefusal(t, viewer, 1, guestSteerWords)
	charge()
	git()

	const privateText = "a prompt that must stay out of the audit file"
	prompts := []string{"Steer", "Submit", "SubmitBash", "SubmitImage", "SubmitFiles", "FollowUp"}
	for i, method := range prompts {
		line := strings.TrimSuffix(guestCall(i+2, method), "}\n") + `,"payload":{"text":"` + privateText + `"}}` + "\n"
		guestWrite(t, guest, line)
		if got := guestHostLine(t, host); got != line {
			t.Fatalf("host received %q, want %q", got, line)
		}
		if method == "Steer" {
			charge()
			git()
		}
	}
	owner := guestDial(t, srv, rg.tokens["santosh"], chat)
	ownerHost, _ := guestReceive(t, opened)
	const ownerBytes = " { \"kind\":\"call\", \"method\":\"Close\" }\n"
	guestWrite(t, owner, ownerBytes)
	if got := guestHostLine(t, ownerHost); got != ownerBytes {
		t.Fatalf("owner used the guest pump: %q", got)
	}
	charge()
	git()
	spends := hostedSpendRows(t, rg, space.Workstream.ID)
	if len(spends) != 3 {
		t.Fatalf("spend rows = %+v", spends)
	}
	for i, asker := range []string{"", "abir", ""} {
		if spends[i].By != "santosh" || spends[i].Asker != asker {
			t.Errorf("spend %d = %+v, want santosh / %q", i, spends[i], asker)
		}
	}

	var audit struct{ Entries []map[string]string }
	rg.do(t, "santosh", http.MethodGet, "/v1/hosted/audit", -1, nil, http.StatusOK, &audit)
	promptCount := 0
	var gitAskers []string
	for _, entry := range audit.Entries {
		if entry["person"] != "santosh" || entry["chat"] != chat {
			t.Fatalf("wrong audit owner or chat: %+v", entry)
		}
		switch entry["kind"] {
		case "prompt":
			promptCount++
			if entry["asker"] != "abir" {
				t.Errorf("prompt audit = %+v", entry)
			}
		case "git":
			gitAskers = append(gitAskers, entry["asker"])
			if asker, present := entry["asker"]; asker == "" && present {
				t.Errorf("P1 git entry gained a field: %+v", entry)
			}
		default:
			t.Errorf("unexpected audit entry: %+v", entry)
		}
	}
	if promptCount != len(prompts) || !reflect.DeepEqual(gitAskers, []string{"", "abir", ""}) {
		t.Fatalf("audit rows = %+v", audit.Entries)
	}
	rg.do(t, "abir", http.MethodGet, "/v1/hosted/audit", -1, nil, http.StatusOK, &audit)
	if len(audit.Entries) != 0 || strings.Contains(rg.readFile(t, "hosted-audit.jsonl"), privateText) {
		t.Fatal("the audit exposed the owner's entries to the guest or logged the text")
	}
}

// P2-9b
// The hosted list carries the effective policy words and omits them when local chats are allowed.
func TestP2_9_HostedListIncludesPolicyWords(t *testing.T) {
	rg := newHostedRig(t)
	const refuse = "This organization runs your chats on its own server, so they do not start on this computer. To work here instead, switch to your personal account, which does not see the organization's chats or files."
	const warn = "This organization keeps your chats on its own server. A chat started on this computer is not part of the organization's account."
	for _, tc := range []struct{ setting, effective, words string }{
		{"", "refuse", refuse}, {"refuse", "refuse", refuse}, {"warn", "warn", warn}, {"allow", "allow", ""},
	} {
		t.Run(tc.effective+"/"+tc.setting, func(t *testing.T) {
			if tc.setting != "" {
				rg.setAccessPolicy(t, func(p *acl.Policy) {
					p.Hosting = acl.Hosting{LocalSessions: tc.setting, MaxSessionCostUSD: 40}
				})
			}
			var result struct{ Hosting map[string]json.RawMessage }
			rg.do(t, "abir", http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &result)
			want := map[string]json.RawMessage{"local_sessions": json.RawMessage(`"` + tc.effective + `"`)}
			if tc.words != "" {
				want["words"], _ = json.Marshal(tc.words)
			}
			if !reflect.DeepEqual(result.Hosting, want) {
				t.Fatalf("hosting = %+v, want %+v", result.Hosting, want)
			}
		})
	}
}

package serve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/canvasrpc"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func contractBotRequest(t *testing.T, h *Handler, method, path, peer, token string, body any, rev int, headers map[string]string, status int, out any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.RemoteAddr = peer
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	if rev >= 0 {
		r.Header.Set("If-Match", strconv.Itoa(rev))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: %d %s, want %d", method, path, w.Code, w.Body, status)
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (o *wsOrg) contractBot(t *testing.T, creator, name, scope string) api.BotCreated {
	t.Helper()
	var created api.BotCreated
	o.do(t, creator, "POST", "/v1/bots", -1, api.BotNew{Name: name, Scope: scope}, 201, &created)
	return created
}

func (o *wsOrg) botDo(t *testing.T, token, method, path string, rev int, body any, status int, out any) *httptest.ResponseRecorder {
	t.Helper()
	return contractBotRequest(t, o.h, method, path, "192.0.2.1:1234", token, body, rev, nil, status, out)
}

func assertBotRefusal(t *testing.T, w *httptest.ResponseRecorder, words string) {
	t.Helper()
	var body struct{ Error string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !strings.Contains(body.Error, words) {
		t.Fatalf("wanted %q in the refusal, got %s", words, w.Body)
	}
	if w.Code == 401 && w.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing bearer challenge")
	}
}

func TestContractC5BotSecretAppearsOnlyOnce(t *testing.T) {
	o := newWSOrg(t)
	const path, name, creator = "/v1/bots", "Build helper", "santosh"
	key := map[string]string{"Idempotency-Key": "a-mint-attempt"}
	body := api.BotNew{Name: name}
	var created api.BotCreated
	contractBotRequest(t, o.h, "POST", path, "192.0.2.1:1234", o.tokens[creator], body, -1, key, 201, &created)
	if !regexp.MustCompile(`^bt_[0-9A-HJKMNP-TV-Z]{26}$`).MatchString(created.Bot.ID) ||
		!regexp.MustCompile(`^cbt_[A-Za-z0-9_-]{43}$`).MatchString(created.Token) ||
		created.Bot.Name != name || created.Bot.Creator != creator || len(created.Bot.Tokens) != 1 {
		t.Fatalf("invalid bot or credential shape for %q", created.Bot.ID)
	}
	duplicate := contractBotRequest(t, o.h, "POST", path, "192.0.2.1:1234", o.tokens[creator], body, -1, key, 409, nil)
	if strings.Contains(duplicate.Body.String(), created.Token) {
		t.Fatal("the original secret was replayed")
	}
	o.do(t, creator, "POST", path, -1, api.BotNew{Name: strings.ToUpper(name)}, 409, nil)
	o.do(t, creator, "POST", path, -1, api.BotNew{Name: strings.Repeat("a", 61)}, 422, nil)
	o.do(t, creator, "POST", path, -1, api.BotNew{Name: "two\nlines"}, 422, nil)
	o.contractBot(t, "mia", name, "")
	mintPath := path + "/" + created.Bot.ID + "/tokens"
	var rotated api.BotTokenCreated
	contractBotRequest(t, o.h, "POST", mintPath, "192.0.2.1:1234", o.tokens[creator], api.BotTokenNew{Label: "rotation"}, -1, key, 201, &rotated)
	if rotated.Token == created.Token || rotated.Label != "rotation" || rotated.ID == "" {
		t.Fatal("rotation did not issue its own credential")
	}
	var listed api.BotList
	reply := o.botDo(t, o.tokens[creator], "GET", path, -1, nil, 200, &listed)
	if len(listed.Bots) != 1 || len(listed.Bots[0].Tokens) != 2 {
		t.Fatalf("list = %+v", listed.Bots)
	}
	for _, secret := range []string{created.Token, rotated.Token} {
		hint := strings.TrimPrefix(secret, "cbt_")[:8]
		if !slices.ContainsFunc(listed.Bots[0].Tokens, func(tok store.BotToken) bool { return tok.Hint == hint }) {
			t.Fatal("the token hint is missing")
		}
		sum := sha256.Sum256([]byte(secret))
		hash := hex.EncodeToString(sum[:])
		if strings.Contains(reply.Body.String(), secret) || strings.Contains(reply.Body.String(), hash) {
			t.Fatal("the list contains a credential or its hash")
		}
		db := filepath.Join(o.st.Root(), store.WorkstreamsDB)
		data, err := os.ReadFile(db)
		if err != nil || !bytes.Contains(data, []byte(hash)) {
			t.Fatalf("the SQLite file does not hold the token hash: %v", err)
		}
		if err := filepath.WalkDir(o.st.Root(), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || !entry.Type().IsRegular() {
				return err
			}
			data, err := os.ReadFile(path)
			if err == nil && bytes.Contains(data, []byte(secret)) {
				t.Errorf("a plain credential was saved in %s", path)
			}
			if err == nil && path != db && bytes.Contains(data, []byte(hash)) {
				t.Errorf("a credential hash was saved outside SQLite in %s", path)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := o.h.config.Record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for _, event := range events {
		if event.Object == "bot:"+created.Bot.ID && event.Actor.Kind == "person" && event.Handle == creator {
			details = append(details, event.Detail)
		}
	}
	for _, want := range []string{"created bot " + name, "minted bot token " + name} {
		if !slices.Contains(details, want) {
			t.Fatalf("record is missing %q: %v", want, details)
		}
	}
	for n := 1; n < 20; n++ {
		o.contractBot(t, creator, "Helper "+strconv.Itoa(n), "")
	}
	o.do(t, creator, "POST", path, -1, api.BotNew{Name: "One too many"}, 409, nil)
}

func TestContractC6BotRightsFollowTheCreatorAndCurrentPolicy(t *testing.T) {
	o := newWSOrg(t)
	o.h.config.WorkstreamAgent = func(context.Context, AgentTurn) (AgentTurnEnd, error) {
		t.Error("creating a chat started an agent")
		return AgentTurnEnd{}, nil
	}
	ws, chats := chatsSharedWorkstream(t, o)
	path := "/v1/workstreams/" + ws
	bot := o.contractBot(t, "abir", "Writer", "")
	hidden := o.create(t, "santosh", api.WorkstreamNew{Name: "Hidden", Private: true})
	o.botDo(t, bot.Token, "GET", "/v1/workstreams/"+hidden.ID, -1, nil, 404, nil)
	var current api.Workstream
	o.botDo(t, bot.Token, "GET", path, -1, nil, 200, &current)
	edit := map[string]any{"purpose": "A scoped edit"}
	o.botDo(t, bot.Token, "PATCH", path, current.Rev, edit, 200, &current)
	key := map[string]string{"Idempotency-Key": "a-previously-authorized-request"}
	chat := api.AgentChatNew{Title: "Scoped draft"}
	contractBotRequest(t, o.h, "POST", chats, "192.0.2.1:1234", bot.Token, chat, -1, key, 201, nil)
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Agents.Scopes = map[string]string{"bot:" + bot.Bot.ID: "view"} })
	o.botDo(t, bot.Token, "PATCH", path, current.Rev, edit, 403, nil)
	o.do(t, "abir", "PATCH", path, current.Rev, edit, 200, &current)
	o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Agents.Scopes = nil
		p.Agents.MayCreate = false
	})
	w := o.botDo(t, bot.Token, "POST", chats, -1, api.AgentChatNew{Title: "No chat"}, 403, nil)
	assertBotRefusal(t, w, "agents cannot create resources")
	w = o.botDo(t, bot.Token, "POST", path+"/agent/messages", -1, nil, 403, nil)
	assertBotRefusal(t, w, "a bot cannot start an agent run yet")
	shares := []factory.Share{}
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &shares}, 200, nil)
	o.botDo(t, bot.Token, "GET", path, -1, nil, 404, nil)
	o.botDo(t, bot.Token, "PATCH", path, current.Rev, edit, 404, nil)
	contractBotRequest(t, o.h, "POST", chats, "192.0.2.1:1234", bot.Token, chat, -1, key, 404, nil)
}

func TestContractC6bBotOwnScopeFailsClosed(t *testing.T) {
	o := newWSOrg(t)
	contractSpace(t, o, "abir")
	ws := o.create(t, "abir", api.WorkstreamNew{Name: "Sponsor owned"})
	path := "/v1/workstreams/" + ws.ID
	o.do(t, "abir", "POST", "/v1/bots", -1, api.BotNew{Name: "Invalid", Scope: "missing"}, 422, nil)
	view := o.contractBot(t, "abir", "Viewer", "view")
	edit := map[string]any{"purpose": "Only the creator can change this"}
	o.botDo(t, view.Token, "GET", path, -1, nil, 200, nil)
	o.botDo(t, view.Token, "PATCH", path, ws.Rev, edit, 403, nil)
	o.do(t, "abir", "PATCH", path, ws.Rev, edit, 200, &ws)
	o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Roles = append(p.Roles, acl.Role{Name: "reader", Verbs: []string{"view"}})
	})
	bot := o.contractBot(t, "abir", "Custom viewer", "reader")
	o.botDo(t, bot.Token, "GET", path, -1, nil, 200, nil)
	o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Roles = slices.DeleteFunc(p.Roles, func(role acl.Role) bool { return role.Name == "reader" })
	})
	o.botDo(t, bot.Token, "GET", path, -1, nil, 404, nil)
	o.botDo(t, bot.Token, "GET", "/v1/myspace", -1, nil, 404, nil)
}

func TestContractC6NamedAgentIDsCannotClaimABotID(t *testing.T) {
	o := newWSOrg(t)
	bot := o.contractBot(t, "abir", "Bot", "")
	body := map[string]string{"name": "Named agent", "instructions": "Write summaries", "id": "bot:" + bot.Bot.ID}
	o.do(t, "abir", "POST", "/v1/agents", -1, body, 422, nil)
	delete(body, "id")
	var agent api.OrgAgent
	o.do(t, "abir", "POST", "/v1/agents", -1, body, 201, &agent)
	if !regexp.MustCompile(`^ag_[0-9A-HJKMNP-TV-Z]{26}$`).MatchString(agent.ID) {
		t.Fatalf("named agent has unexpected id %q", agent.ID)
	}
}

func TestContractC7BotChatKeepsCreatorOwnershipAndIsPrivate(t *testing.T) {
	o := newWSOrg(t)
	o.h.config.WorkstreamAgent = func(context.Context, AgentTurn) (AgentTurnEnd, error) {
		t.Error("creating a chat started an agent")
		return AgentTurnEnd{}, nil
	}
	_, list := chatsSharedWorkstream(t, o)
	bot := o.contractBot(t, "abir", "Chat maker", "")
	var chat api.AgentChat
	o.botDo(t, bot.Token, "POST", list, -1, api.AgentChatNew{Title: "Private draft"}, 201, &chat)
	path := "/v1/resources/chat/" + chat.ID
	var resource api.OrgResource
	o.do(t, "abir", "GET", path, -1, nil, 200, &resource)
	want := acl.Principal{Kind: "bot", ID: bot.Bot.ID, Sponsor: "abir"}
	if resource.Owner != (acl.Subject{Kind: "person", ID: "abir"}) ||
		resource.Visibility != "private" || resource.CreatedBy == nil || *resource.CreatedBy != want {
		t.Fatalf("chat resource = %+v, created_by = %+v", resource, resource.CreatedBy)
	}
	for _, who := range []string{"santosh", "mia", "owner"} {
		if others := othersOf(t, o, who, list); len(others) != 0 {
			t.Fatalf("%s was shown a private chat: %+v", who, others)
		}
		o.do(t, who, "GET", path, -1, nil, 404, nil)
	}
	var mine api.AgentChatList
	o.do(t, "abir", "GET", list, -1, nil, 200, &mine)
	if len(mine.Chats) != 1 || mine.Chats[0].ID != chat.ID {
		t.Fatalf("creator's chat list = %+v", mine)
	}
	o.do(t, "abir", "DELETE", "/v1/bots/"+bot.Bot.ID, -1, nil, 200, nil)
	o.do(t, "abir", "GET", path, -1, nil, 200, nil)
}

func TestContractC8BotTokenLifecycle(t *testing.T) {
	o := newWSOrg(t)
	const about = "/v1/about"
	bot := o.contractBot(t, "abir", "Rotator", "")
	path := "/v1/bots/" + bot.Bot.ID
	var expiring, replacement api.BotTokenCreated
	o.do(t, "abir", "POST", path+"/tokens", -1, api.BotTokenNew{ExpiresAt: o.now.Add(time.Minute).Format(time.RFC3339Nano)}, 201, &expiring)
	o.do(t, "abir", "POST", path+"/tokens", -1, nil, 201, &replacement)
	o.do(t, "abir", "POST", path+"/tokens", -1, nil, 409, nil)
	o.botDo(t, bot.Token, "GET", about, -1, nil, 200, nil)
	lastUsed := func() string {
		t.Helper()
		var list api.BotList
		o.do(t, "abir", "GET", "/v1/bots", -1, nil, 200, &list)
		for _, token := range list.Bots[0].Tokens {
			if token.ID == bot.Bot.Tokens[0].ID && token.LastUsedAt != nil {
				return *token.LastUsedAt
			}
		}
		t.Fatal("the token has no last-used time")
		return ""
	}
	firstUse := lastUsed()
	o.now = o.now.Add(time.Second)
	o.botDo(t, bot.Token, "GET", about, -1, nil, 200, nil)
	if lastUsed() != firstUse {
		t.Fatal("a second use inside a minute rewrote last-used time")
	}
	o.now = o.now.Add(time.Minute)
	o.botDo(t, bot.Token, "GET", about, -1, nil, 200, nil)
	if lastUsed() <= firstUse {
		t.Fatal("last-used time did not move after a minute")
	}
	assertBotRefusal(t, o.botDo(t, expiring.Token, "GET", about, -1, nil, 401, nil), "has expired")
	o.do(t, "abir", "POST", path+"/tokens", -1, nil, 201, nil)
	o.do(t, "owner", "DELETE", path+"/tokens/"+bot.Bot.Tokens[0].ID, -1, nil, 200, nil)
	assertBotRefusal(t, o.botDo(t, bot.Token, "GET", about, -1, nil, 401, nil), "was revoked")
	o.do(t, "abir", "DELETE", path, -1, nil, 200, nil)
	assertBotRefusal(t, o.botDo(t, replacement.Token, "GET", about, -1, nil, 401, nil), "bot was removed")

	leaving := o.contractBot(t, "mia", "Mia bot", "")
	o.h.config.Team = []string{"owner"}
	assertBotRefusal(t, o.botDo(t, leaving.Token, "GET", about, -1, nil, 403, nil), "is not on the factory team")
	o.h.config.Team = nil
	o.botDo(t, leaving.Token, "GET", about, -1, nil, 200, nil)
	o.do(t, "owner", "DELETE", "/v1/people/mia/sessions", -1, nil, 200, nil)
	assertBotRefusal(t, o.botDo(t, leaving.Token, "GET", about, -1, nil, 401, nil), "switched off")
	var all api.BotList
	o.do(t, "owner", "GET", "/v1/bots", -1, nil, 200, &all)
	if len(all.Bots) != 1 || all.Bots[0].DisabledAt == nil || all.Bots[0].Tokens[0].RevokedAt == nil {
		t.Fatalf("disabled bot metadata = %+v", all.Bots)
	}
	remaining := o.contractBot(t, "abir", "After the reset", "")
	o.do(t, "owner", "POST", "/v1/access/revoke-sessions", -1, nil, 200, nil)
	o.botDo(t, remaining.Token, "GET", about, -1, nil, 200, nil)
}

func TestContractC8bBotBearerNeverFallsBack(t *testing.T) {
	const configured = "cbt_configured-host-token"
	for _, servedBy := range []string{"factory", "app"} {
		t.Run(servedBy, func(t *testing.T) {
			h := New(factory.Seam{}, Config{ServedBy: servedBy, AccessToken: configured, LoopbackTrust: boolPtr(true)})
			t.Cleanup(h.Close)
			for _, token := range []string{"cbt_unknown", configured} {
				w := contractBotRequest(t, h, "GET", "/v1/me", "127.0.0.1:1234", token, nil, -1, nil, 401, nil)
				words := "not recognized"
				if servedBy == "app" {
					words = "the app bearer token is required"
				}
				assertBotRefusal(t, w, words)
			}
		})
	}
}

func TestContractC9BotManagementAndRouteAllowlist(t *testing.T) {
	o := newWSOrg(t)
	o.h.config.Canvas = canvasrpc.New(filepath.Join(t.TempDir(), "unused-sidecar"), o.st.CanvasRoot())
	o.h.canvasRoutes()
	bot := o.contractBot(t, "abir", "Abir bot", "")
	other := o.contractBot(t, "mia", "Mia bot", "")
	path := "/v1/bots/" + bot.Bot.ID
	tokensPath := path + "/tokens"
	tokenPath := tokensPath + "/" + bot.Bot.Tokens[0].ID
	ws := o.create(t, "abir", api.WorkstreamNew{Name: "Bot work"})
	workstream := "/v1/workstreams/" + ws.ID
	contractSpace(t, o, "abir")
	for _, path := range []string{"/v1/workstreams", workstream, "/v1/canvases", "/v1/resources",
		"/v1/myspace", "/v1/org/policy", "/v1/members", "/v1/teams", "/v1/about"} {
		t.Run(path, func(t *testing.T) { o.botDo(t, bot.Token, "GET", path, -1, nil, 200, nil) })
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", "/v1/bots"}, {"POST", "/v1/bots"}, {"POST", tokensPath}, {"DELETE", tokenPath}, {"DELETE", path},
		{"GET", "/v1/me"}, {"POST", "/v1/auth/password"}, {"GET", "/v1/invites"},
		{"POST", "/v1/access/revoke-sessions"}, {"GET", "/v1/events"}, {"GET", "/v1/jobs/1"},
		{"GET", "/v1/runners"}, {"GET", "/v1/repos"}, {"GET", "/v1/people"}, {"GET", "/v1/people/abir"},
		{"DELETE", "/v1/people/mia/sessions"}, {"PUT", "/v1/org/policy"}, {"POST", "/v1/teams"}, {"PUT", "/v1/myspace/limit"},
		{"GET", "/v1/workstreams/requestable"}, {"POST", workstream + "/requests"}, {"PUT", workstream + "/driver"},
		{"PUT", workstream + "/jobs/1"}, {"POST", workstream + "/tasks/" + factory.JobTaskID(1) + "/answer"},
		{"GET", "/v1/workstreams-extra"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			assertBotRefusal(t, o.botDo(t, bot.Token, tc.method, tc.path, -1, nil, 403, nil), "a bot cannot use this yet")
		})
	}
	for _, path := range []string{workstream + "/agent", workstream + "/%61gent/messages", workstream + "/agent/stop"} {
		assertBotRefusal(t, o.botDo(t, bot.Token, "GET", path, -1, nil, 403, nil), "a bot cannot start an agent run yet")
	}
	cookie := map[string]string{"Cookie": accessCookieName + "=" + o.tokens["abir"]}
	w := contractBotRequest(t, o.h, "POST", "/v1/bots", "192.0.2.1:1234", bot.Token, api.BotNew{Name: "No"}, -1, cookie, 400, nil)
	assertBotRefusal(t, w, "send one credential")
	o.do(t, "", "POST", "/v1/bots", -1, api.BotNew{Name: "No"}, 401, nil)
	var mine, all api.BotList
	o.do(t, "mia", "GET", "/v1/bots", -1, nil, 200, &mine)
	if len(mine.Bots) != 1 || mine.Bots[0].ID != other.Bot.ID {
		t.Fatalf("member list = %+v", mine.Bots)
	}
	o.do(t, "mia", "POST", tokensPath, -1, nil, 404, nil)
	o.do(t, "mia", "DELETE", tokenPath, -1, nil, 404, nil)
	o.do(t, "mia", "DELETE", path, -1, nil, 404, nil)
	history := o.botDo(t, o.tokens["mia"], "GET", "/v1/audit", -1, nil, 200, nil)
	if strings.Contains(history.Body.String(), bot.Bot.ID) || strings.Contains(history.Body.String(), bot.Bot.Name) {
		t.Fatal("another person's bot appeared in organization activity")
	}
	o.do(t, "owner", "GET", "/v1/bots", -1, nil, 200, &all)
	if len(all.Bots) != 2 {
		t.Fatalf("operator list = %+v", all.Bots)
	}
	o.do(t, "owner", "POST", tokensPath, -1, nil, 403, nil)
	o.do(t, "owner", "DELETE", path, -1, nil, 200, nil)
}

func TestContractC11BotTokenCannotUseAWorkerDoor(t *testing.T) {
	o := newWSOrg(t)
	bot := o.contractBot(t, "abir", "Worker candidate", "")
	for _, path := range []string{"/v1/workers/pull", "/v1/runners/pull"} {
		w := o.botDo(t, bot.Token, "GET", path, -1, nil, 401, nil)
		assertBotRefusal(t, w, "a bearer worker credential is required")
	}
	var me api.Me
	o.do(t, "abir", "GET", "/v1/me", -1, nil, 200, &me)
	if me.Profile.Handle != "abir" || me.Operator {
		t.Fatalf("person session no longer has its own identity: %+v", me.Profile)
	}
	o.botDo(t, o.cfg.AccessToken, "GET", "/v1/me", -1, nil, 200, &me)
}

func TestContractC6BotCannotUseAfiExceptionsOrStartTaskRuns(t *testing.T) {
	o, path, page := commentOrg(t, "edit")
	o.h.config.WorkstreamAgent = func(context.Context, AgentTurn) (AgentTurnEnd, error) {
		t.Error("the bot started an agent")
		return AgentTurnEnd{}, nil
	}
	bot := o.contractBot(t, "santosh", "Editor", "")
	commentPath := path + "/pages/" + page + "/comments"
	missingBlock := launchPassage
	missingBlock.Block = nil
	o.botDo(t, bot.Token, "POST", commentPath, -1, missingBlock, 422, nil)
	o.botDo(t, bot.Token, "POST", commentPath, -1, launchPassage, 201, nil)

	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Afi task", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	taskPath := path + "/tasks/" + task.ID
	w := o.botDo(t, bot.Token, "PATCH", taskPath, task.Rev, map[string]any{"state": factory.TaskRunning}, 403, nil)
	assertBotRefusal(t, w, "a bot cannot start an agent run yet")
	o.botDo(t, bot.Token, "DELETE", taskPath, task.Rev, nil, 403, nil)
}

func TestContractC6BotTaskReadsDoNotInheritHostRepositoryAccess(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "abir", api.WorkstreamNew{Name: "Job visibility"})
	path := "/v1/workstreams/" + ws.ID
	if err := o.st.SetRepos([]string{"org/open"}); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"org/open", "private/repo"} {
		_, err := o.st.Create(factory.Item{Title: repo, Repo: repo, PolicyGroup: ws.ID, Kind: factory.KindIssue, State: factory.StateQueued})
		if err != nil {
			t.Fatal(err)
		}
	}
	bot := o.contractBot(t, "abir", "Task reader", "")
	var tasks api.WorkstreamTasks
	o.botDo(t, bot.Token, "GET", path+"/tasks", -1, nil, 200, &tasks)
	if len(tasks.Tasks) != 1 || tasks.Tasks[0].Title != "org/open" {
		t.Fatalf("bot's task list includes jobs outside the organization: %+v", tasks.Tasks)
	}
}

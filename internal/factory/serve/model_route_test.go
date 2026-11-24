package serve

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

const (
	testHostedOrgKey = "sk-test-org-0001"
	testHostedOwnKey = "sk-test-own-0001"
	testHostedModel  = "stub/scripted"
	testHostedBody   = `{"model":"stub/scripted","messages":[{"role":"user","content":"Hi"}]}`
	testHostedStream = `{"model":"stub/scripted","messages":[{"role":"user","content":"Hi"}],"stream":true}`
)

type hostedTestAnswer func(context.Context, string, string) (*ai.Response, error)

func newMeteredRig(t *testing.T, capUSD float64, answer hostedTestAnswer) *hostedRig {
	t.Helper()
	rg := newHostedRig(t)
	rg.configure(t, func(c *HostedConfig) {
		c.SessionCapUSD = capUSD
		c.ModelUpstreamKey = testHostedOrgKey
		c.CompleterFor = func(_ string, key string) func(string) modelapi.Completer {
			return func(model string) modelapi.Completer {
				return hostedModelCompleter(func(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
					return answer(ctx, key, model)
				})
			}
		}
	})
	t.Cleanup(rg.h.closeHostedModels)
	return rg
}

func callHostedModel(rg *hostedRig, chat, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, modelapi.ChatURL("/v1/model"), strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+rg.host.tokenOf(chat))
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, r)
	return w
}

func meteredAnswer(ctx context.Context, model, text string, cost float64) *ai.Response {
	provider.BillingSinkFrom(ctx)(provider.Billed{Model: model, PromptTokens: 11, CompletionTokens: 7, Cost: cost})
	return &ai.Response{ID: "gen-test", Model: model, Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}}, FinishReason: "stop",
	}}}
}

func myHostedSpace(t *testing.T, rg *hostedRig, person string) api.MySpace {
	t.Helper()
	var space api.MySpace
	rg.do(t, person, http.MethodGet, "/v1/myspace", -1, nil, http.StatusOK, &space)
	return space
}

func hostedSpendRows(t *testing.T, rg *hostedRig, ws string) []store.Spend {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(rg.st.Root(), store.WorkstreamsDB))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT at, by, usd, chat, key, asker FROM spend WHERE ws = ? ORDER BY rowid", ws)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var spends []store.Spend
	for rows.Next() {
		var spend store.Spend
		if err := rows.Scan(&spend.At, &spend.By, &spend.USD, &spend.Chat, &spend.Key, &spend.Asker); err != nil {
			t.Fatal(err)
		}
		spends = append(spends, spend)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return spends
}

func hostedModelErrorIs(t *testing.T, w *httptest.ResponseRecorder, status int, kind, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Message string
			Type    string
			Code    int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != status ||
		body.Error.Code != status || body.Error.Type != kind || body.Error.Message != message {
		t.Fatalf("response = %d %s, want %d, %s, %q", w.Code, w.Body, status, kind, message)
	}
}

func TestP1_2_EveryCallIsPostedToThePersonsSpendToTheCent(t *testing.T) {
	var calls int
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		calls++
		switch calls {
		case 4:
			return nil, errors.New("the model failed")
		case 5:
			return meteredAnswer(ctx, model, "free", 0), nil
		default:
			return meteredAnswer(ctx, model, "paid", 0.0123), nil
		}
	})
	chat := rg.create(t, "santosh")
	for i, status := range []int{200, 200, 200, 502, 200} {
		body := testHostedBody
		if i == 1 {
			body = testHostedStream
		}
		w := callHostedModel(rg, chat, body)
		if w.Code != status || strings.Contains(w.Body.String(), `"cost"`) {
			t.Fatalf("call %d = %d %s", i, w.Code, w.Body)
		}
		if i == 1 && (!w.Flushed || !strings.Contains(w.Body.String(), `"total_tokens":18`) || !strings.Contains(w.Body.String(), "data: [DONE]")) {
			t.Fatalf("stream = %s, flushed=%v", w.Body, w.Flushed)
		}
	}
	space := myHostedSpace(t, rg, "santosh")
	if space.SpentUSD != 0.04 {
		t.Fatalf("My space spent $%g", space.SpentUSD)
	}
	other := myHostedSpace(t, rg, "mia")
	if other.SpentUSD != 0 || len(hostedSpendRows(t, rg, other.Workstream.ID)) != 0 {
		t.Fatalf("another person's space changed: %+v", other)
	}
	rows := hostedSpendRows(t, rg, space.Workstream.ID)
	var total float64
	keys := make(map[string]bool)
	for _, row := range rows {
		total += row.USD
		if row.By != "santosh" || row.Chat != "hosted:"+chat || row.USD != 0.0123 ||
			!strings.HasPrefix(row.Key, row.Chat+":") || keys[row.Key] {
			t.Fatalf("unexpected or repeated charge: %+v", row)
		}
		keys[row.Key] = true
	}
	if len(rows) != 3 || math.Abs(total-0.0369) > 1e-12 {
		t.Fatalf("posted %d rows totaling %g", len(rows), total)
	}
}

func TestP1_3_ChatCapRefusesInAPlainSentenceAndMakesNoUpstreamCall(t *testing.T) {
	var calls int
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		calls++
		return meteredAnswer(ctx, model, "paid", 5), nil
	})
	chat := rg.create(t, "santosh")
	if w := callHostedModel(rg, chat, testHostedBody); w.Code != 200 {
		t.Fatalf("first call = %d %s", w.Code, w.Body)
	}
	hostedModelErrorIs(t, callHostedModel(rg, chat, testHostedBody), 402, "insufficient_quota",
		"This chat has reached its spending limit of $5.00, so it is paused. Start a new chat to carry on.")
	if calls != 1 {
		t.Fatalf("made %d upstream calls", calls)
	}
}

func TestP1_3_MonthlyLimitRefuses(t *testing.T) {
	var calls atomic.Int64
	rg := newMeteredRig(t, 0, func(context.Context, string, string) (*ai.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected model call")
	})
	space := myHostedSpace(t, rg, "santosh")
	if _, err := rg.st.AddSpend(space.Workstream.ID, store.Spend{
		At: rg.h.clock().Format(time.RFC3339Nano), USD: 20, Chat: "agent", By: "santosh",
	}); err != nil {
		t.Fatal(err)
	}
	id := rg.create(t, "santosh")
	hostedModelErrorIs(t, callHostedModel(rg, id, testHostedBody), 402, "insufficient_quota",
		"You have reached your monthly limit of $20.00, so this chat is paused. The organization's operator can raise it in Settings.")
	if calls.Load() != 0 || len(rg.h.config.Hosted.state.models.entries) != 0 {
		t.Fatal("the monthly refusal created a server or called upstream")
	}
}

func TestP1_3_RefusalRepeatsWithoutSpending(t *testing.T) {
	var calls int
	rg := newMeteredRig(t, 0.25, func(ctx context.Context, _, model string) (*ai.Response, error) {
		calls++
		return meteredAnswer(ctx, model, "paid", 0.25), nil
	})
	id := rg.create(t, "santosh")
	if w := callHostedModel(rg, id, testHostedBody); w.Code != 200 {
		t.Fatalf("first call = %d %s", w.Code, w.Body)
	}
	// A replacement must use the same persisted cap.
	rg.h.config.Hosted.state.models.forget(id)
	for range 3 {
		hostedModelErrorIs(t, callHostedModel(rg, id, testHostedBody), 402, "insufficient_quota",
			"This chat has reached its spending limit of $0.25, so it is paused. Start a new chat to carry on.")
	}
	if got := myHostedSpace(t, rg, "santosh").SpentUSD; got != 0.25 || calls != 1 {
		t.Fatalf("after refusals: spent $%g across %d upstream calls", got, calls)
	}
}

type blockedModelBody struct {
	*strings.Reader
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (b *blockedModelBody) Read(p []byte) (int, error) {
	b.once.Do(func() {
		b.started <- struct{}{}
		<-b.release
	})
	return b.Reader.Read(p)
}

func TestP1_3_FifthSimultaneousCallIsBusy(t *testing.T) {
	var calls atomic.Int64
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		calls.Add(1)
		return meteredAnswer(ctx, model, "free", 0), nil
	})
	id := rg.create(t, "santosh")
	started, release := make(chan struct{}, 4), make(chan struct{})
	var active sync.WaitGroup
	defer func() {
		close(release)
		active.Wait()
	}()
	for range 4 {
		active.Go(func() {
			body := &blockedModelBody{Reader: strings.NewReader(testHostedBody), started: started, release: release}
			r := httptest.NewRequest(http.MethodPost, modelapi.ChatURL("/v1/model"), body)
			r.Header.Set("Authorization", "Bearer "+rg.host.tokenOf(id))
			rg.h.ServeHTTP(httptest.NewRecorder(), r)
		})
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("four calls never reached request parsing")
		}
	}
	hostedModelErrorIs(t, callHostedModel(rg, id, testHostedBody), 429, "rate_limit",
		"This chat is already making as many requests as it may at once. Try again in a moment.")
	if calls.Load() != 0 {
		t.Fatal("a refused call reached the model")
	}
}

func TestP1_3_RefusalBodyIsAnOpenAIErrorAndStatus(t *testing.T) {
	call := &hostedModelCall{}
	for _, tc := range []struct {
		status int
		header bool
		kind   string
		words  string
	}{
		{402, false, "insufficient_quota", "This chat has reached its spending limit of $5.00, so it is paused. Start a new chat to carry on."},
		{429, true, "rate_limit", "This chat is already making as many requests as it may at once. Try again in a moment."},
	} {
		rec := httptest.NewRecorder()
		w := &hostedModelWriter{ResponseWriter: rec, call: call, capUSD: 5}
		if tc.header {
			w.Header().Set(modelapi.HeldHeader, "ceiling")
		}
		w.WriteHeader(tc.status)
		_, _ = io.WriteString(w, `{"error":{"message":"the internal limit","code":402}}`)
		hostedModelErrorIs(t, rec, tc.status, tc.kind, tc.words)
	}
}

func TestP1_4_StoppedChatsBearerIsRefusedOnTheNextCall(t *testing.T) {
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		return meteredAnswer(ctx, model, "hi", 0), nil
	})
	for _, stop := range []struct{ method, suffix string }{{"POST", "/stop"}, {"DELETE", ""}} {
		id := rg.create(t, "santosh")
		if w := callHostedModel(rg, id, testHostedBody); w.Code != 200 {
			t.Fatalf("before stop: %d %s", w.Code, w.Body)
		}
		registry := &rg.h.config.Hosted.state.models
		entry := registry.entries[id]
		key := rg.host.tokenOf(id)
		w := rg.send("santosh", stop.method, "/v1/hosted/sessions/"+id+stop.suffix, nil, nil)
		if w.Code != http.StatusNoContent || registry.entries[id] != nil || entry.server.API().Token != "" {
			t.Fatalf("%s %s = %d, model server still alive=%v", stop.method, stop.suffix, w.Code, entry.server.API().Token != "")
		}
		ended := rg.bearerCall("POST", modelapi.ChatURL("/v1/model"), key, nil)
		hostedModelErrorIs(t, ended, 401, "invalid_request_error", "This chat's model access has ended.")
	}
}

func TestP1_4_BearerOfAnotherChatCannotUseThisChatsServer(t *testing.T) {
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		return meteredAnswer(ctx, model, "hi", 0.01), nil
	})
	a := rg.create(t, "santosh")
	b := rg.create(t, "mia")
	if w := callHostedModel(rg, a, testHostedBody); w.Code != 200 {
		t.Fatalf("first chat = %d %s", w.Code, w.Body)
	}
	entry := rg.h.config.Hosted.state.models.entries[a]
	stop := rg.send("mia", "POST", "/v1/hosted/sessions/"+a+"/stop", nil, nil)
	if stop.Code != http.StatusNotFound || entry.server.API().Token == "" {
		t.Fatalf("another person's stop returned %d or ended the server", stop.Code)
	}
	r := httptest.NewRequest("POST", modelapi.ChatURL("/v1/model")+"?chat="+a, strings.NewReader(testHostedBody))
	r.Header.Set("Authorization", "Bearer "+rg.host.tokenOf(b))
	r.Header.Set("X-Codeaf-Person", "santosh")
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, r)
	entries := rg.h.config.Hosted.state.models.entries
	if w.Code != 200 || entries[a] != entry || entries[b] == nil || entries[b] == entry {
		t.Fatalf("second chat = %d %s, with %d registered servers", w.Code, w.Body, len(entries))
	}
	for person, id := range map[string]string{"santosh": a, "mia": b} {
		space := myHostedSpace(t, rg, person)
		rows := hostedSpendRows(t, rg, space.Workstream.ID)
		if len(rows) != 1 || rows[0].Chat != "hosted:"+id {
			t.Fatalf("%s's spend: %+v", person, rows)
		}
	}
}

func TestP1_1_UpstreamSeesOnlyTheOrgKeyAndTheChatSeesNone(t *testing.T) {
	installHostedKeyHook(t, func(person, _ string) (string, bool, error) {
		if person == "mia" {
			return testHostedOwnKey, true, nil
		}
		return "", false, nil
	})
	rg := newHostedRig(t)
	var reject atomic.Bool
	rg.up.mu.Lock()
	rg.up.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("X-Test-Upstream-Key", key)
		if reject.Load() || key != testHostedOrgKey && key != testHostedOwnKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":{"message":%q,"code":401}}`, "invalid key "+key)
			return
		}
		if r.URL.Path == modelapi.ChatURL("/api/v1") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"gen-test\",\"model\":\"stub/scripted\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"cost\":0.0123}}\n\ndata: [DONE]\n\n", "hello "+key)
		}
	}
	rg.up.mu.Unlock()
	rg.configure(t, func(c *HostedConfig) {
		c.ModelUpstreamKey = testHostedOrgKey
		c.CompleterFor = func(base, key string) func(string) modelapi.Completer {
			return func(model string) modelapi.Completer {
				client, err := provider.NewClient(provider.Config{BaseURL: base, APIKey: key, Model: model})
				if err != nil {
					return hostedModelCompleter(func(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) { return nil, err })
				}
				return client
			}
		}
	})
	t.Cleanup(rg.h.closeHostedModels)
	var ownChat string
	for _, person := range []string{"santosh", "mia"} {
		id := rg.create(t, person)
		if person == "mia" {
			ownChat = id
		}
		w := callHostedModel(rg, id, testHostedBody)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") {
			t.Fatalf("%s's reply = %d %s", person, w.Code, w.Body)
		}
		for _, key := range []string{testHostedOrgKey, testHostedOwnKey} {
			if strings.Contains(w.Body.String(), key) || strings.Contains(fmt.Sprint(w.Header()), key) {
				t.Fatalf("%s's reply exposed a key: %v %s", person, w.Header(), w.Body)
			}
		}
	}
	reject.Store(true)
	refused := callHostedModel(rg, ownChat, testHostedBody)
	// Item 0: a key the provider refuses is a plain sentence worded as the person's own, never the provider's words.
	if refused.Code != 401 || !strings.Contains(refused.Body.String(), hostedOwnKeyRefused) || strings.Contains(refused.Body.String(), "invalid key") ||
		strings.Contains(refused.Body.String(), testHostedOwnKey) || strings.Contains(fmt.Sprint(refused.Header()), testHostedOwnKey) {
		t.Fatalf("upstream refusal = %d %v %s", refused.Code, refused.Header(), refused.Body)
	}
	var keys []string
	for _, request := range rg.up.requests() {
		if request.method == http.MethodPost {
			keys = append(keys, request.header.Get("Authorization"))
		}
	}
	if !slices.Equal(keys, []string{"Bearer " + testHostedOrgKey, "Bearer " + testHostedOwnKey, "Bearer " + testHostedOwnKey}) {
		t.Fatalf("upstream saw %v", keys)
	}
}

func installHostedKeyHook(t *testing.T, hook func(string, string) (string, bool, error)) {
	t.Helper()
	hostedKeyForHook = hook
	t.Cleanup(func() { hostedKeyForHook = nil })
}

func TestP1_5_OwnKeyIsUsedForItsPersonOnly(t *testing.T) {
	own := true
	installHostedKeyHook(t, func(person, provider string) (string, bool, error) {
		if own && person == "santosh" && provider == "openrouter" {
			return testHostedOwnKey, true, nil
		}
		return "", false, nil
	})
	var seen []string
	rg := newMeteredRig(t, 0, func(ctx context.Context, key, model string) (*ai.Response, error) {
		seen = append(seen, key)
		return meteredAnswer(ctx, model, "hi", 0.0123), nil
	})
	chat := rg.create(t, "santosh")
	b := rg.create(t, "mia")
	for _, id := range []string{chat, b, chat} {
		if w := callHostedModel(rg, id, testHostedBody); w.Code != 200 {
			t.Fatalf("call = %d %s", w.Code, w.Body)
		}
		if id == b {
			own = false
		}
	}
	if !slices.Equal(seen, []string{testHostedOwnKey, testHostedOrgKey, testHostedOrgKey}) {
		t.Fatalf("selected %v", seen)
	}
	space := myHostedSpace(t, rg, "santosh")
	rows := hostedSpendRows(t, rg, space.Workstream.ID)
	if len(rows) != 2 || rows[0].Chat != "hosted:"+chat+":own" || rows[1].Chat != "hosted:"+chat {
		t.Fatalf("own-key spend = %+v", rows)
	}
}

func TestP1_5_OwnKeyThatCannotBeOpenedRefusesNeverFallsBack(t *testing.T) {
	installHostedKeyHook(t, func(string, string) (string, bool, error) {
		return "", true, errors.New("test key could not be opened")
	})
	var calls atomic.Int64
	rg := newMeteredRig(t, 0, func(context.Context, string, string) (*ai.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected model call")
	})
	hostedModelErrorIs(t, callHostedModel(rg, rg.create(t, "santosh"), testHostedBody), 402, "insufficient_quota",
		"Your own model key could not be opened on this server, so this chat paused instead of using the organization's.")
	if calls.Load() != 0 {
		t.Fatal("the model was called")
	}
}

func TestP1_5_UpstreamRejectingAnOwnKeyIsReturnedNotRetriedWithTheOrgKey(t *testing.T) {
	installHostedKeyHook(t, func(string, string) (string, bool, error) {
		return testHostedOwnKey, true, nil
	})
	var seen []string
	rg := newMeteredRig(t, 0, func(_ context.Context, key, _ string) (*ai.Response, error) {
		seen = append(seen, key)
		return nil, &provider.APIError{Status: 401, Message: "invalid key"}
	})
	w := callHostedModel(rg, rg.create(t, "santosh"), testHostedBody)
	// Item 0 changed the words, never the refusal: the person's key is named as
	// theirs and the provider's own words are not passed on.
	hostedModelErrorIs(t, w, 401, "", hostedOwnKeyRefused)
	if !slices.Equal(seen, []string{testHostedOwnKey}) {
		t.Fatalf("upstream got %v", seen)
	}
}

func TestP1_1_OnlyModelRoutesReachUpstream(t *testing.T) {
	installHostedKeyHook(t, func(string, string) (string, bool, error) {
		t.Error("a listing or refused path checked a personal key")
		return testHostedOwnKey, true, nil
	})
	rg := newMeteredRig(t, 0, func(context.Context, string, string) (*ai.Response, error) {
		t.Error("a listing or refused path used a completer")
		return nil, errors.New("unexpected completion")
	})
	key := rg.host.tokenOf(rg.create(t, "santosh"))
	for _, path := range []string{"models", "models/vendor/model"} {
		if w := rg.bearerCall("GET", "/v1/model/"+path, key, nil); w.Code != 200 {
			t.Fatalf("GET %s = %d %s", path, w.Code, w.Body)
		}
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "key"}, {"GET", "credits"}, {"GET", "generation"}, {"GET", "auth/keys"},
		{"POST", "completions"}, {"GET", "chat/completions"}, {"GET", "models/../key"},
	} {
		w := rg.bearerCall(route.method, "/v1/model/"+route.path, key, nil)
		if w.Code != 404 || errorOf(t, w.Body.Bytes()) != "There is nothing at that address." {
			t.Fatalf("%s %s = %d %s", route.method, route.path, w.Code, w.Body)
		}
	}
	requests := rg.up.requests()
	if len(requests) != 2 || requests[0].header.Get("Authorization") != "Bearer "+testHostedOrgKey || requests[1].header.Get("Authorization") != "Bearer "+testHostedOrgKey {
		t.Fatalf("listings and refusals made upstream requests %+v", requests)
	}
}

func TestP1_2_IdleCloseSettlesTheOriginalKeysReceiptBeforeReopening(t *testing.T) {
	own := true
	installHostedKeyHook(t, func(string, string) (string, bool, error) {
		if own {
			return testHostedOwnKey, true, nil
		}
		return "", false, nil
	})
	settle := make(chan func(), 1)
	var calls atomic.Int64
	rg := newMeteredRig(t, 0.25, func(ctx context.Context, _, model string) (*ai.Response, error) {
		calls.Add(1)
		done := provider.ReceiptPendingFrom(ctx)()
		settle <- func() {
			defer done()
			provider.ReconcileSinkFrom(ctx)(provider.Reconciled{
				Found:  true,
				Billed: provider.Billed{Model: model, Cost: 0.25, PromptTokens: 11, CompletionTokens: 7},
			})
		}
		return &ai.Response{Model: model, Choices: []ai.Choice{{FinishReason: "stop"}}}, nil
	})
	id := rg.create(t, "santosh")
	if w := callHostedModel(rg, id, testHostedBody); w.Code != 200 {
		t.Fatalf("before idle: %d %s", w.Code, w.Body)
	}
	finish := sync.OnceFunc(<-settle)
	defer finish()
	own = false

	registry := &rg.h.config.Hosted.state.models
	registry.mu.Lock()
	entry := registry.entries[id]
	entry.idle.Reset(0)
	registry.mu.Unlock()

	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		registry.mu.Lock()
		closing := entry.closing != nil
		registry.mu.Unlock()
		if closing {
			break
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("idle entry did not start closing")
		}
	}
	waiting := make(chan *httptest.ResponseRecorder, 1)
	go func() { waiting <- callHostedModel(rg, id, testHostedBody) }()
	select {
	case w := <-waiting:
		t.Fatalf("a call answered before settlement: %d %s", w.Code, w.Body)
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	select {
	case w := <-waiting:
		hostedModelErrorIs(t, w, 402, "insufficient_quota",
			"This chat has reached its spending limit of $0.25, so it is paused. Start a new chat to carry on.")
	case <-deadline:
		t.Fatal("a call did not resume after settlement")
	}
	space := myHostedSpace(t, rg, "santosh")
	rows := hostedSpendRows(t, rg, space.Workstream.ID)
	if calls.Load() != 1 || len(rows) != 1 || rows[0].Chat != "hosted:"+id+":own" || rows[0].USD != 0.25 {
		t.Fatalf("%d upstream calls, rows %+v", calls.Load(), rows)
	}
}

func TestP1_4_HandlerCloseDrainsTheChatModels(t *testing.T) {
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		return meteredAnswer(ctx, model, "hi", 0), nil
	})
	id := rg.create(t, "santosh")
	if w := callHostedModel(rg, id, testHostedBody); w.Code != 200 {
		t.Fatalf("before close: %d %s", w.Code, w.Body)
	}
	registry := &rg.h.config.Hosted.state.models
	entry := registry.entries[id]
	rg.h.Close()
	if len(registry.entries) != 0 || entry.server.API().Token != "" {
		t.Fatal("the handler left its chat model server open")
	}
}

func TestP1_1_DefaultBaseAndKeylessUpstream(t *testing.T) {
	for _, tc := range []struct {
		name              string
		explicitBase      bool
		storedKey, useKey string
	}{
		{name: "default base and stored organization key", storedKey: testHostedOrgKey, useKey: testHostedOrgKey},
		{name: "explicit keyless upstream", explicitBase: true, useKey: "keyless"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rg := newHostedRig(t)
			var base, key string
			if tc.storedKey != "" {
				if _, err := rg.st.SetOrgKey("openrouter", tc.storedKey, "owner", rg.h.clock().Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
			}
			rg.configure(t, func(cfg *HostedConfig) {
				if !tc.explicitBase {
					cfg.ModelUpstream = ""
				}
				cfg.CompleterFor = func(givenBase, givenKey string) func(string) modelapi.Completer {
					base, key = givenBase, givenKey
					return func(model string) modelapi.Completer {
						return hostedModelCompleter(func(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
							return meteredAnswer(ctx, model, "hi", 0), nil
						})
					}
				}
			})
			wantBase := ""
			if tc.explicitBase {
				wantBase = rg.upSrv.URL + "/api/v1"
			}
			w := callHostedModel(rg, rg.create(t, "santosh"), testHostedBody)
			if w.Code != 200 || base != wantBase || key != tc.useKey {
				t.Fatalf("call = %d %s, configured base = %q, chosen key matched = %v", w.Code, w.Body, base, key == tc.useKey)
			}
		})
	}
}

// operatorLog replaces the key-refusal log with a recorder and returns what it
// has been told so far.
func operatorLog(t *testing.T, rg *hostedRig) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	rg.h.config.Hosted.state.refusals.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(lines)
	}
}

// Item 0: when the provider rejects the ORGANIZATION's key (401 or 403) the chat
// gets a plain refusal that is not a bad gateway (the engine retries those
// quietly, about once a minute), the person-facing words carry neither the key
// nor the provider's text, the upstream is asked once per call, and the
// operator's log gets ONE line naming the cause, not one per call.
func TestItem0_OrgKeyRefusedByTheProviderIsAPlainRefusalAndOneOperatorLine(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int64
			rg := newMeteredRig(t, 0, func(_ context.Context, key, _ string) (*ai.Response, error) {
				calls.Add(1)
				return nil, &provider.APIError{Status: status, Message: "User not found. " + key}
			})
			lines := operatorLog(t, rg)
			chat := rg.create(t, "santosh")
			for i := 1; i <= 3; i++ {
				w := callHostedModel(rg, chat, testHostedBody)
				hostedModelErrorIs(t, w, 401, "", hostedOrgKeyRefused)
				if strings.Contains(w.Body.String(), testHostedOrgKey) || strings.Contains(w.Body.String(), "User not found") {
					t.Fatalf("the refusal carries the key or the provider's words: %s", w.Body)
				}
				if w.Code >= 500 {
					t.Fatalf("a refused key must not look like a gateway fault the engine retries: %d", w.Code)
				}
				if got := calls.Load(); got != int64(i) {
					t.Fatalf("after %d calls the provider was asked %d times: the door must not retry", i, got)
				}
			}
			got := lines()
			if len(got) != 1 || !strings.Contains(got[0], "organization's model key") || !strings.Contains(got[0], fmt.Sprint(status)) ||
				strings.Contains(got[0], testHostedOrgKey) {
				t.Fatalf("operator lines = %q, want exactly one naming the organization's key and no key", got)
			}
		})
	}
}

// Item 0: a refused key that later works is a new outage if it is refused
// again, so the operator hears of it; and the quiet period ends by itself.
func TestItem0_ARecoveredKeyThatFailsAgainIsLoggedAgain(t *testing.T) {
	var refuse atomic.Bool
	refuse.Store(true)
	rg := newMeteredRig(t, 0, func(ctx context.Context, _, model string) (*ai.Response, error) {
		if refuse.Load() {
			return nil, &provider.APIError{Status: 401, Message: "no"}
		}
		return meteredAnswer(ctx, model, "ok", 0), nil
	})
	lines := operatorLog(t, rg)
	chat := rg.create(t, "santosh")
	callHostedModel(rg, chat, testHostedBody)
	callHostedModel(rg, chat, testHostedBody)
	refuse.Store(false)
	if w := callHostedModel(rg, chat, testHostedBody); w.Code != 200 {
		t.Fatalf("a working key = %d %s", w.Code, w.Body)
	}
	refuse.Store(true)
	callHostedModel(rg, chat, testHostedBody)
	if got := lines(); len(got) != 2 {
		t.Fatalf("operator lines = %q, want one per outage (2)", got)
	}
	// The same outage is named again only after the quiet period.
	state := &rg.h.config.Hosted.state.refusals
	state.refused(time.Now().Add(hostedRefusalEvery+time.Minute), false, "santosh", 401)
	if got := lines(); len(got) != 3 {
		t.Fatalf("after the quiet period the line repeats: %q", got)
	}
}

// Item 0: a person's own sealed key that the provider rejects gets the same
// treatment, worded as their key, with one operator line that names the person
// and never the key, and the organization's key is neither used nor named.
func TestItem0_OwnKeyRefusedByTheProviderIsWordedAsTheirKey(t *testing.T) {
	installHostedKeyHook(t, func(string, string) (string, bool, error) {
		return testHostedOwnKey, true, nil
	})
	var seen []string
	rg := newMeteredRig(t, 0, func(_ context.Context, key, _ string) (*ai.Response, error) {
		seen = append(seen, key)
		return nil, &provider.APIError{Status: 403, Message: "forbidden " + key}
	})
	lines := operatorLog(t, rg)
	chat := rg.create(t, "santosh")
	for range 2 {
		w := callHostedModel(rg, chat, testHostedBody)
		hostedModelErrorIs(t, w, 401, "", hostedOwnKeyRefused)
		if strings.Contains(w.Body.String(), testHostedOwnKey) || strings.Contains(w.Body.String(), "forbidden") {
			t.Fatalf("the refusal carries the key or the provider's words: %s", w.Body)
		}
	}
	if !slices.Equal(seen, []string{testHostedOwnKey, testHostedOwnKey}) {
		t.Fatalf("upstream got %v", seen)
	}
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "own model key") || !strings.Contains(got[0], "santosh") ||
		strings.Contains(got[0], testHostedOwnKey) || strings.Contains(got[0], "organization") {
		t.Fatalf("operator lines = %q, want one naming the person's own key", got)
	}
	if strings.Contains(hostedOwnKeyRefused+hostedOrgKeyRefused, "bearer") {
		t.Fatal("plain words")
	}
}

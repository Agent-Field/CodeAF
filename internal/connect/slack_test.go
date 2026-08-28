package connect

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// fakeSlack points every Slack address except the public door at one local
// stand-in. The door is kept real because its exact spelling is one of the
// things these tests prove.
func fakeSlack(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	auth, token, api := slackAuthURL, slackTokenURL, slackAPIURL
	t.Cleanup(func() { slackAuthURL, slackTokenURL, slackAPIURL = auth, token, api })
	slackAuthURL = server.URL + "/auth"
	slackTokenURL = server.URL + "/token"
	slackAPIURL = server.URL + "/api"
	return server
}

func slackManager(t *testing.T) *Manager {
	t.Helper()
	withPlugs(t, slack{})
	manager, err := NewManager(t.TempDir(), map[string]ClientCredential{
		"slack": {ID: "slack-client", Public: true},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager
}

func TestSlackPublicApplicationIsOfferedWithoutASecret(t *testing.T) {
	manager := slackManager(t)
	services := manager.Services()
	if len(services) != 1 || services[0].ID != "slack" || services[0].Auth != AuthBrowser {
		t.Fatalf("Services = %+v", services)
	}
}

func TestSlackExchangeSendsAUserKeyAsBearer(t *testing.T) {
	const key = "xoxp-test-user-key"
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.Form.Get("client_id") != "slack-client" || r.Form.Get("code_verifier") != "one-verifier" {
			t.Errorf("exchange form = %v", r.Form)
		}
		if _, present := r.Form["client_secret"]; present {
			t.Errorf("exchange carries a secret field: %v", r.Form)
		}
		writeJSON(t, w, map[string]any{
			"ok": true, "access_token": key, "token_type": "user",
			"scope": "search:read,chat:write",
		})
	})
	mux.HandleFunc("/api/auth.test", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+key {
			t.Errorf("Authorization = %q, want the user key sent as Bearer", got)
		}
		writeJSON(t, w, map[string]any{"ok": true, "user": "abir", "team": "AgentField"})
	})
	server := fakeSlack(t, mux)

	config := oauth2.Config{ClientID: "slack-client", Endpoint: (slack{}).Endpoint()}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: (slack{}).Transport(server.Client().Transport),
	})
	keys, err := config.Exchange(ctx, "one-code", oauth2.VerifierOption("one-verifier"))
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	account, err := (slack{}).Account(context.Background(), staticClient(context.Background(), keys))
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if account != "abir · AgentField" {
		t.Errorf("Account = %q", account)
	}
	if keys.TokenType != "user" {
		t.Errorf("the stored answer was rewritten to %q", keys.TokenType)
	}
}

func TestSlackExchangeKeepsItsRefusalWord(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"ok": false, "error": "invalid_code"})
	})
	server := fakeSlack(t, mux)
	config := oauth2.Config{ClientID: "slack-client", Endpoint: (slack{}).Endpoint()}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: (slack{}).Transport(server.Client().Transport),
	})
	_, err := config.Exchange(ctx, "refused-code", oauth2.VerifierOption("one-verifier"))
	if err == nil || !strings.Contains(err.Error(), "invalid_code") {
		t.Fatalf("Exchange refusal = %v, want invalid_code", err)
	}
	if strings.Contains(err.Error(), "xoxp") {
		t.Fatalf("the refusal contains a key: %v", err)
	}
}

func TestSlackCallRefusesOKFalse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"ok": false, "error": "not_in_channel"})
	})
	server := fakeSlack(t, mux)
	err := slackCall(context.Background(), server.Client(), "chat.postMessage", nil, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "not_in_channel") {
		t.Fatalf("slackCall refusal = %v", err)
	}
}

func TestSlackCallSaysHowLongARateLimitLasts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "59")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"ok":false,"error":"ratelimited"}`)
	})
	server := fakeSlack(t, mux)
	err := slackCall(context.Background(), server.Client(), "conversations.replies", nil, &struct{}{})
	if err == nil || err.Error() != "Slack is rate-limiting this for another 59 seconds" {
		t.Fatalf("rate-limit refusal = %v", err)
	}
}

func TestSlackSearchFormatsEveryHitForTheThreadReader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search.messages", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.Form.Get("count") != "2" || r.Form.Get("sort") != "timestamp" || r.Form.Get("sort_dir") != "desc" {
			t.Errorf("search form = %v", r.Form)
		}
		writeJSON(t, w, map[string]any{"ok": true, "messages": map[string]any{"matches": []any{
			map[string]any{
				"channel": map[string]any{"id": "C12345678", "name": "general", "is_private": false},
				"user":    "U2U85N1RV", "username": "roach", "text": "first hit", "ts": "1508284197.000015",
				"permalink": "https://example.slack.com/archives/C12345678/p1508284197000015", "team": "T111", "type": "message",
			},
			map[string]any{
				"channel": map[string]any{"id": "C222", "name": "launch", "is_private": true},
				"user":    "U222", "username": "bob", "text": "second hit", "ts": "1720000000.200",
				"permalink": "https://example.slack.com/archives/C222/p1720000000200", "team": "T111", "type": "message",
			},
		}}})
	})
	server := fakeSlack(t, mux)
	text, err := SlackSearch(context.Background(), server.Client(), "launch", 2)
	if err != nil {
		t.Fatalf("SlackSearch: %v", err)
	}
	for _, want := range []string{
		"#general · roach", "first hit",
		"https://example.slack.com/archives/C12345678/p1508284197000015",
		"channel C12345678 · ts 1508284197.000015",
		"#launch · bob", "second hit", "channel C222 · ts 1720000000.200",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("search answer is missing %q:\n%s", want, text)
		}
	}
}

func TestSlackHelpersExposeDeadKeysAsAReconnect(t *testing.T) {
	for _, testCase := range []struct {
		name string
		path string
		word string
		call func(context.Context, *http.Client) (string, error)
	}{
		{
			name: "search",
			path: "/api/search.messages",
			word: "token_revoked",
			call: func(ctx context.Context, client *http.Client) (string, error) {
				return SlackSearch(ctx, client, "launch", 10)
			},
		},
		{
			name: "send",
			path: "/api/chat.postMessage",
			word: "invalid_auth",
			call: func(ctx context.Context, client *http.Client) (string, error) {
				return SlackSend(ctx, client, "#general", "we ship Friday", "")
			},
		},
		{
			name: "list channels",
			path: "/api/conversations.list",
			word: "account_inactive",
			call: func(ctx context.Context, client *http.Client) (string, error) {
				return SlackListChannels(ctx, client, "", 50)
			},
		},
		{
			name: "read thread",
			path: "/api/conversations.replies",
			word: "invalid_auth",
			call: func(ctx context.Context, client *http.Client) (string, error) {
				return SlackReadThread(ctx, client, "C111", "1710000000.100")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(testCase.path, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, map[string]any{"ok": false, "error": testCase.word})
			})
			server := fakeSlack(t, mux)
			_, err := testCase.call(context.Background(), server.Client())
			if err == nil || err.Error() != "Slack has to be connected again" {
				t.Fatalf("%s refusal = %v", testCase.word, err)
			}
		})
	}
}

func TestSlackSendKeepsOtherSlackRefusalWords(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"ok": false, "error": "not_in_channel"})
	})
	server := fakeSlack(t, mux)
	_, err := SlackSend(context.Background(), server.Client(), "#general", "we ship Friday", "")
	want := "Slack refused chat.postMessage: not_in_channel"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("SlackSend refusal = %v, want it to contain %q", err, want)
	}
}

func TestSlackReadThreadMakesOneBoundedHistoryCall(t *testing.T) {
	var replies atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		replies.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.Form.Get("channel") != "C111" || r.Form.Get("ts") != "1710000000.100" ||
			r.Form.Get("limit") != "15" || r.Form.Get("inclusive") != "true" {
			t.Errorf("thread form = %v", r.Form)
		}
		writeJSON(t, w, map[string]any{"ok": true, "messages": []any{
			map[string]any{"username": "alice", "ts": "1710000000.100", "text": "root"},
			map[string]any{"username": "bob", "ts": "1710000001.200", "text": "reply"},
		}})
	})
	server := fakeSlack(t, mux)
	text, err := SlackReadThread(context.Background(), server.Client(), "C111", "1710000000.100")
	if err != nil {
		t.Fatalf("SlackReadThread: %v", err)
	}
	if replies.Load() != 1 {
		t.Fatalf("conversations.replies calls = %d, want 1", replies.Load())
	}
	if !strings.Contains(text, "alice") || !strings.Contains(text, "reply") {
		t.Errorf("thread answer = %q", text)
	}
}

func TestSlackListChannelsFiltersAndClamps(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.Form.Get("types") != "public_channel,private_channel" ||
			r.Form.Get("exclude_archived") != "true" || r.Form.Get("limit") != "200" {
			t.Errorf("channel form = %v", r.Form)
		}
		writeJSON(t, w, map[string]any{"ok": true, "channels": []any{
			map[string]any{"id": "C111", "name": "general", "num_members": 12, "purpose": map[string]any{"value": "Company news"}},
			map[string]any{"id": "C222", "name": "launch", "num_members": 4, "purpose": map[string]any{"value": "Ship it"}},
		}})
	})
	server := fakeSlack(t, mux)
	text, err := SlackListChannels(context.Background(), server.Client(), "launch", 999)
	if err != nil {
		t.Fatalf("SlackListChannels: %v", err)
	}
	if text != "#launch · C222 · 4 members · Ship it" {
		t.Errorf("channels = %q", text)
	}
}

func TestSlackSendCarriesTheOptionalThread(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.Form.Get("channel") != "#launch" || r.Form.Get("text") != "ready to ship" ||
			r.Form.Get("thread_ts") != "1710000000.100" {
			t.Errorf("send form = %v", r.Form)
		}
		writeJSON(t, w, map[string]any{"ok": true, "ts": "1710000001.200"})
	})
	server := fakeSlack(t, mux)
	text, err := SlackSend(context.Background(), server.Client(), "#launch", "ready to ship", "1710000000.100")
	if err != nil {
		t.Fatalf("SlackSend: %v", err)
	}
	if text != "Sent to #launch (ts 1710000001.200)" {
		t.Errorf("send answer = %q", text)
	}
	if _, err := SlackSend(context.Background(), server.Client(), "#launch", "   ", ""); err == nil {
		t.Fatal("an empty message was sent")
	}
	if calls.Load() != 1 {
		t.Errorf("post calls = %d, want 1", calls.Load())
	}
}

func TestSlackAccountDropsNoIdentityPieces(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth.test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"ok": true, "user": "abir", "team": "AgentField"})
	})
	server := fakeSlack(t, mux)
	got, err := (slack{}).Account(context.Background(), server.Client())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if got != "abir · AgentField" {
		t.Errorf("Account = %q", got)
	}
}

func TestSlackDoorNamesTheFirstRegisteredDoor(t *testing.T) {
	if got := (slack{}).Door(8765); got != "https://agentfield.ai/connect/slack/8765" {
		t.Errorf("Door(8765) = %q", got)
	}
}

func TestGrantedReadsSlackCommaSeparatedScopes(t *testing.T) {
	keys := (&oauth2.Token{}).WithExtra(map[string]any{"scope": "a:b,c:d"})
	got := granted(keys, nil)
	if strings.Join(got, "|") != "a:b|c:d" {
		t.Errorf("granted = %v", got)
	}
}

func TestSlackBeginAuthUsesTheSecondDoorWhenTheFirstIsBusy(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:8765")
	if err != nil {
		t.Fatalf("hold the first Slack address: %v", err)
	}
	defer func() { _ = first.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {})
	fakeSlack(t, mux)
	manager := slackManager(t)
	flow, err := manager.BeginAuth(context.Background(), "slack", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	if flow.URL() != "http://localhost:18765/" {
		t.Fatalf("flow URL = %q", flow.URL())
	}

	stop := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	hop, err := stop.Get(loopback(t, flow.URL()).String())
	if err != nil {
		t.Fatalf("open loopback index: %v", err)
	}
	_ = hop.Body.Close()
	if hop.StatusCode != http.StatusFound {
		t.Fatalf("loopback index = %s", hop.Status)
	}
	outgoing, err := url.Parse(hop.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize address: %v", err)
	}
	query := outgoing.Query()
	if query.Get("redirect_uri") != "https://agentfield.ai/connect/slack/18765" {
		t.Errorf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("client_id") != "slack-client" || query.Get("code_challenge") == "" {
		t.Errorf("authorize query = %v", query)
	}
	if got := strings.Fields(query.Get("scope")); len(got) != len(slackScopes) {
		t.Errorf("authorize scope = %v, want the %d user permissions", got, len(slackScopes))
	}
	if _, present := query["client_secret"]; present {
		t.Errorf("authorize query carries a secret field: %v", query)
	}
	flow.Cancel()
	select {
	case <-flow.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled Slack listener did not stop")
	}
}

func TestSlackBeginAuthSaysBothRegisteredAddressesAreBusy(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:8765")
	if err != nil {
		t.Fatalf("hold the first Slack address: %v", err)
	}
	defer func() { _ = first.Close() }()
	second, err := net.Listen("tcp", "127.0.0.1:18765")
	if err != nil {
		t.Fatalf("hold the second Slack address: %v", err)
	}
	defer func() { _ = second.Close() }()

	manager := slackManager(t)
	_, err = manager.BeginAuth(context.Background(), "slack", "")
	want := "connect Slack: both of the addresses Slack can send you back to are busy on this machine — finish or cancel the other sign-in and try again"
	if err == nil || err.Error() != want {
		t.Fatalf("BeginAuth = %v, want %q", err, want)
	}
}

// fakeDoored is a doored plug that is not Slack, so the shared sign-in path
// can be proved to speak the service's own name and not Slack's.
type fakeDoored struct{ fakePlug }

func (fakeDoored) Door(port int) string { return "https://example.test/door/" + strconv.Itoa(port) }

// THE BUSY SENTENCE NAMES THE SERVICE, not Slack: the door is a law of auth.go,
// and the next vendor to need one must not be told about Slack.
func TestADooredPlugsBusySentenceNamesTheService(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:8765")
	if err != nil {
		t.Fatalf("hold the first address: %v", err)
	}
	defer func() { _ = first.Close() }()
	second, err := net.Listen("tcp", "127.0.0.1:18765")
	if err != nil {
		t.Fatalf("hold the second address: %v", err)
	}
	defer func() { _ = second.Close() }()

	withPlugs(t, fakeDoored{fakePlug{service: Service{ID: "acme", Name: "Acme", Auth: AuthBrowser, Category: categoryCommunication}}})
	manager, err := NewManager(t.TempDir(), map[string]ClientCredential{"acme": {ID: "acme-id", Public: true}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	_, err = manager.BeginAuth(context.Background(), "acme", "")
	want := "connect Acme: both of the addresses Acme can send you back to are busy on this machine — finish or cancel the other sign-in and try again"
	if err == nil || err.Error() != want {
		t.Fatalf("BeginAuth = %v, want %q", err, want)
	}
}

func TestSlackBrowserRefusalEndsTheFlow(t *testing.T) {
	manager := slackManager(t)
	flow, err := manager.BeginAuth(context.Background(), "slack", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	back := loopback(t, flow.URL())
	back.RawQuery = url.Values{"error": {"access_denied"}}.Encode()
	response, err := http.Get(back.String())
	if err != nil {
		flow.Cancel()
		t.Fatalf("return the refusal: %v", err)
	}
	_ = response.Body.Close()
	waiting, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := flow.Wait(waiting)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("Wait refusal = %v", err)
	}
	if status.Connected {
		t.Fatal("a refused Slack sign-in reads as connected")
	}
}

func TestExpiredKeyWithNoRenewalAsksToConnectAgain(t *testing.T) {
	manager := slackManager(t)
	if err := manager.store.put("slack", stored{
		Scopes: append([]string(nil), slackScopes...),
		Keys: &oauth2.Token{
			AccessToken: "expired-user-key",
			TokenType:   "user",
			Expiry:      time.Now().Add(-time.Minute),
		},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, err := manager.Client(context.Background(), "slack")
	if err == nil || err.Error() != "Slack has to be connected again" {
		t.Fatalf("Client = %v", err)
	}
}

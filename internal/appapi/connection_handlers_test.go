package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func accountFixture(t *testing.T) (*Server, *config.AppCredentials, string) {
	t.Helper()
	profile := t.TempDir()
	source := config.NewAppCredentials()
	s := New(testToken, Options{ProfileDir: profile, Credentials: source})
	t.Cleanup(s.Close)
	return s, source, profile
}
func settingsRequest(t *testing.T, s *Server, method, path, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := request(t, s, method, path, body, bearer(), "127.0.0.1:4000")
	if w.Code != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, w.Code, w.Body, want)
	}
	return w
}
func TestAccountRoutesReadPatchActionsPollDisconnect(t *testing.T) {
	s, source, profile := accountFixture(t)
	source.Set("google", "launch-google-secret")
	source.Set("github", "launch-github-secret")
	rows := settingsRequest(t, s, "GET", "/app/v1/accounts", "", 200)
	var accounts AccountsResponse
	if err := json.Unmarshal(rows.Body.Bytes(), &accounts); err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 3 || accounts[0].State != "connected" || accounts[1].State != "connected" || len(accounts[1].Actions) != 4 || accounts[2].State != "disconnected" {
		t.Fatalf("%s", rows.Body)
	}
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/google", `{"access":"yes"}`, 200)
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/google/actions/mail-read", `{"access":"off"}`, 200)
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/google/actions/mail-send", `{"access":"yes"}`, 422)
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/google/actions/calendar-write", `{"access":"off"}`, 422)
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/google/actions/calendar-write", `{"access":"ask_first"}`, 200)
	manager := s.accountManager()
	if manager.CapabilityState("google", "mail-read") != "off" || manager.CapabilityState("google", "calendar-write") != "ask" {
		t.Fatal("native policy did not follow Settings")
	}
	settingsRequest(t, s, "GET", "/app/v1/accounts/google/connect", "", 200)
	settingsRequest(t, s, "POST", "/app/v1/accounts/github/connect", `{}`, 200)
	settingsRequest(t, s, "DELETE", "/app/v1/accounts/google", "", 200)
	if token, _ := source.Key("google"); token != "" {
		t.Fatal("disconnect retained the grant")
	}
	poll := settingsRequest(t, s, "GET", "/app/v1/accounts/google/connect", "", 200)
	if !strings.Contains(poll.Body.String(), `"state":"disconnected"`) {
		t.Fatal(poll.Body)
	}
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/slack", `{"access":"yes"}`, 409)
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/missing", `{"access":"off"}`, 404)
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/github", `{"access":null}`, 422)
	settingsRequest(t, s, "PATCH", "/app/v1/accounts/github", `{"access":"yes","token":"secret"}`, 422)
	raw, _ := os.ReadFile(config.BudgetConfigPath(profile))
	if strings.Contains(string(raw), "launch-") {
		t.Fatal("grant reached disk")
	}
	if _, err := os.Stat(filepath.Join(profile, "credentials.json")); !os.IsNotExist(err) {
		t.Fatal("app opened a credential file", err)
	}
}
func TestAccountBrowserRoutesReturnARealCallbackAndCancelOnDisconnect(t *testing.T) {
	for _, id := range []string{"google", "slack"} {
		t.Run(id, func(t *testing.T) {
			s, _, _ := accountFixture(t)
			w := settingsRequest(t, s, "POST", "/app/v1/accounts/"+id+"/connect", `{}`, 200)
			var connected ServiceConnectResponse
			if err := json.Unmarshal(w.Body.Bytes(), &connected); err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(connected.ConnectURL)
			if err != nil || u.Scheme != "http" || u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
				t.Fatal("not a local OAuth callback", connected)
			}
			settingsRequest(t, s, "GET", "/app/v1/accounts/"+id+"/connect", "", 200)
			settingsRequest(t, s, "DELETE", "/app/v1/accounts/"+id, "", 200)
			s.accounts.Lock()
			defer s.accounts.Unlock()
			if len(s.accounts.flows) != 0 {
				t.Fatal("disconnect kept its browser callback")
			}
		})
	}
}
func TestMcpRoutesDiscoverToolsPersistPolicyAndRemove(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "lookup", Description: "Read a fixture"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture result"}}}, struct{}{}, nil
	})
	host := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer host.Close()
	s, source, profile := accountFixture(t)
	settingsRequest(t, s, "GET", "/app/v1/mcp", "", 200)
	w := settingsRequest(t, s, "POST", "/app/v1/mcp", `{"address":"`+host.URL+`"}`, 201)
	var row McpServer
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.State != "connected" || row.Tools != 1 || row.Access != ServiceAsk {
		t.Fatalf("discovery: %+v", row)
	}
	manager := s.accountManager()
	tools, err := manager.MCPTools(context.Background(), "mcp:"+row.ID)
	if err != nil || len(tools) != 1 || tools[0].Name != "lookup" {
		t.Fatal("saved server did not reach the native belt", tools, err)
	}
	answer, err := manager.MCPCall(context.Background(), "mcp:"+row.ID, "lookup", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(answer, "fixture result") {
		t.Fatal("native server call failed", answer, err)
	}
	source.Set("mcp:"+row.ID, "mcp-launch-secret")
	settingsRequest(t, s, "PATCH", "/app/v1/mcp/"+row.ID, `{"access":"off"}`, 200)
	if manager.CapabilityState("mcp:"+row.ID, "read") != "off" {
		t.Fatal("server Off did not reach the native gate")
	}
	next := New(testToken, Options{ProfileDir: profile, Credentials: source})
	defer next.Close()
	w = settingsRequest(t, next, "GET", "/app/v1/mcp", "", 200)
	if !strings.Contains(w.Body.String(), `"access":"off"`) {
		t.Fatal("restart lost policy", w.Body)
	}
	settingsRequest(t, s, "PATCH", "/app/v1/mcp/"+row.ID, `{"access":"allow"}`, 422)
	settingsRequest(t, s, "POST", "/app/v1/mcp", `{"address":"https://name:secret@example.com/mcp"}`, 422)
	settingsRequest(t, s, "POST", "/app/v1/mcp", `{"address":"https://example.com/mcp?token=secret"}`, 422)
	settingsRequest(t, s, "DELETE", "/app/v1/mcp/"+row.ID, "", 200)
	if _, err := manager.MCPTools(context.Background(), "mcp:"+row.ID); err == nil {
		t.Fatal("removed server stayed armed")
	}
	settingsRequest(t, s, "PATCH", "/app/v1/mcp/"+row.ID, `{"access":"off"}`, 404)
	w = settingsRequest(t, s, "GET", "/app/v1/mcp", "", 200)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Body)
	}
	if token, _ := source.Key("mcp:" + row.ID); token != "" {
		t.Fatal("removed server kept its token")
	}
	raw, _ := os.ReadFile(config.BudgetConfigPath(profile))
	if strings.Contains(string(raw), "mcp-launch-secret") {
		t.Fatal("server secret persisted")
	}
}
func TestAdvancedCrewAndFactorySettingsRoundTripIsAtomic(t *testing.T) {
	s, _, profile := accountFixture(t)
	body := `{"advanced.context_tokens":80000,"advanced.work_folder":"/tmp/codeaf-work","advanced.proposal_s":24,"advanced.idle_days":9,"crews.depth":4,"crews.share_pct":60,"crews.wake":false,"factory.polling_s":90,"factory.runner_timeout_s":120,"factory.keep_output_days":14,"factory.keep_output_mb":20}`
	settingsRequest(t, s, "PATCH", "/app/v1/settings", body, 200)
	w := settingsRequest(t, s, "GET", "/app/v1/settings", "", 200)
	var values map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &values)
	if values["advanced.context_tokens"] != float64(80000) || values["crews.wake"] != false || values["factory.keep_output_mb"] != float64(20) {
		t.Fatal(w.Body)
	}
	before, _ := os.ReadFile(config.BudgetConfigPath(profile))
	settingsRequest(t, s, "PATCH", "/app/v1/settings", `{"crews.wake":true,"factory.runner_timeout_s":0}`, 422)
	after, _ := os.ReadFile(config.BudgetConfigPath(profile))
	if string(before) != string(after) {
		t.Fatal("partial settings write")
	}
}
func TestConnectionRoutesUseTheTransportGuard(t *testing.T) {
	s, _, _ := accountFixture(t)
	for _, route := range connectionContractRoutes {
		path := contractPathVariable.ReplaceAllString(route.Path, "fixture")
		w := request(t, s, route.Method, path, "{}", nil, "127.0.0.1:4000")
		if w.Code != 401 {
			t.Fatal(route.Path, w.Code)
		}
	}
}

func TestAccountConnectPollHandsNewGrantsToKeychainOnce(t *testing.T) {
	s, source, _ := accountFixture(t)
	source.Set("google", "new-browser-grant-secret")
	_ = s.accountManager()
	s.accounts.Lock()
	s.accounts.issued["google"] = "new-browser-grant-secret"
	s.accounts.Unlock()
	w := settingsRequest(t, s, "GET", "/app/v1/accounts", "", 200)
	if strings.Contains(w.Body.String(), "grant-secret") {
		t.Fatal("account read exposed a credential")
	}
	w = request(t, s, "GET", "/app/v1/accounts/google/connect", "", nil, "127.0.0.1:4000")
	if w.Code != 401 {
		t.Fatal("unguarded receipt")
	}
	w = settingsRequest(t, s, "GET", "/app/v1/accounts/google/connect", "", 200)
	var poll AccountConnectResponse
	if err := json.Unmarshal(w.Body.Bytes(), &poll); err != nil {
		t.Fatal(err)
	}
	if poll.Credential != "new-browser-grant-secret" || poll.State != "connected" {
		t.Fatal("missing Keychain handoff", w.Body)
	}
	w = settingsRequest(t, s, "GET", "/app/v1/accounts/google/connect", "", 200)
	if strings.Contains(w.Body.String(), "credential") {
		t.Fatal("receipt was exported twice", w.Body)
	}
	source.Set("google", "launch-grant-secret")
	w = settingsRequest(t, s, "GET", "/app/v1/accounts/google/connect", "", 200)
	if strings.Contains(w.Body.String(), "credential") {
		t.Fatal("poll exported a launch credential")
	}
}

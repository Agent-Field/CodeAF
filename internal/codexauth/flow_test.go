package codexauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/trace"
)

func jwt(t *testing.T, claims any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(raw) + ".signature"
}

func testListener(_ string, _ string) (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

func callbackAddress(flow *Flow, query url.Values) string {
	return "http://" + flow.listener.Addr().String() + callbackPath + "?" + query.Encode()
}

func TestC2AuthorizeAddressCarriesOnlyTheCodexCLIContract(t *testing.T) {
	// C2: the authorization address has the exact fixed fields, fresh S256 proof and state.
	flow, err := Begin(context.Background(), Options{Issuer: "https://issuer.example", Random: strings.NewReader(strings.Repeat("a", 32) + strings.Repeat("b", 32)), Listen: testListener})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Cancel()
	parsed, _ := url.Parse(flow.URL())
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != "https://issuer.example/oauth/authorize" {
		t.Fatalf("authorize address = %q", flow.URL())
	}
	want := map[string]string{
		"response_type": "code", "client_id": ClientID,
		"redirect_uri": "http://localhost:1455/auth/callback", "scope": callbackScope,
		"code_challenge_method": "S256", "state": base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("b", 32))),
		"id_token_add_organizations": "true", "codex_cli_simplified_flow": "true", "originator": Originator,
	}
	challenge := sha256Text(base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 32))))
	want["code_challenge"] = challenge
	if len(parsed.Query()) != len(want) {
		t.Fatalf("query fields = %v", parsed.Query())
	}
	for key, value := range want {
		if got := parsed.Query().Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestC2ListenerRequestsTheRegisteredPortsInOrder(t *testing.T) {
	// C2: the listener seam receives the actual registered addresses. It may
	// supply an ephemeral loopback socket for the callback drive, but it cannot
	// hide a production change from 1455 followed by 1457.
	var requested []string
	listen := func(network, address string) (net.Listener, error) {
		if network != "tcp" {
			t.Fatalf("listener network = %q", network)
		}
		requested = append(requested, address)
		if len(requested) == 1 {
			return nil, errors.New("primary port busy")
		}
		return net.Listen("tcp", "127.0.0.1:0")
	}
	flow, err := Begin(context.Background(), Options{
		Issuer: "https://issuer.example", Random: strings.NewReader(strings.Repeat("p", 64)), Listen: listen,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Cancel()
	if strings.Join(requested, ",") != "127.0.0.1:1455,127.0.0.1:1457" {
		t.Fatalf("requested listener addresses = %v", requested)
	}
	parsed, _ := url.Parse(flow.URL())
	if got := parsed.Query().Get("redirect_uri"); got != "http://localhost:1457/auth/callback" {
		t.Fatalf("fallback redirect = %q", got)
	}
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestC3CallbackRejectsWrongStateAndASecondReturn(t *testing.T) {
	// C3: only the first callback carrying this flow's state may spend the code.
	flow, err := Begin(context.Background(), Options{Issuer: "https://issuer.example", Random: strings.NewReader(strings.Repeat("x", 64)), Listen: testListener})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Cancel()
	response, err := http.Get(callbackAddress(flow, url.Values{"state": {"wrong"}, "code": {"one"}}))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong state status = %d", response.StatusCode)
	}
	response, err = http.Get(callbackAddress(flow, url.Values{"state": {flow.state}, "code": {"two"}}))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("second callback status = %d", response.StatusCode)
	}
	if _, err := flow.Wait(context.Background()); err == nil || !strings.Contains(err.Error(), "different sign-in") {
		t.Fatalf("wait error = %v", err)
	}
}

func TestC3CallbackRefusalAndMissingCodeArePlainFailures(t *testing.T) {
	// C3: an issuer error or a callback without a code connects nothing.
	for _, query := range []url.Values{{"error": {"denied"}}, {}} {
		flow, err := Begin(context.Background(), Options{Issuer: "https://issuer.example", Random: strings.NewReader(strings.Repeat("q", 64)), Listen: testListener})
		if err != nil {
			t.Fatal(err)
		}
		query.Set("state", flow.state)
		response, err := http.Get(callbackAddress(flow, query))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if _, err := flow.Wait(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "connect Codex: ") {
			t.Fatalf("plain callback error = %v", err)
		}
		flow.Cancel()
	}
}

func TestC4CodeExchangeKeepsTokensAndReadsAccountClaims(t *testing.T) {
	// C4: the authorization code form is exact and no API-key exchange occurs.
	now := time.Unix(1_800_000_000, 0)
	access := jwt(t, map[string]any{"exp": now.Add(time.Hour).Unix()})
	idToken := jwt(t, map[string]any{"email": "person@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-1", "chatgpt_plan_type": "pro"}})
	var form url.Values
	issuer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != tokenPath {
			t.Fatalf("unexpected exchange path %q", request.URL.Path)
		}
		_ = request.ParseForm()
		form = request.Form
		_ = json.NewEncoder(writer).Encode(map[string]string{"access_token": access, "refresh_token": "refresh-secret", "id_token": idToken})
	}))
	defer issuer.Close()
	flow, err := Begin(context.Background(), Options{Issuer: issuer.URL, HTTPClient: issuer.Client(), Random: strings.NewReader(strings.Repeat("z", 64)), Listen: testListener, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Cancel()
	response, err := http.Get(callbackAddress(flow, url.Values{"state": {flow.state}, "code": {"the-code"}}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	tokens, err := flow.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != access || tokens.RefreshToken != "refresh-secret" || tokens.IDToken != idToken || tokens.AccountID != "acct-1" || tokens.Email != "person@example.com" || tokens.Plan != "pro" {
		t.Fatalf("tokens = %+v", tokens)
	}
	want := map[string]string{"grant_type": "authorization_code", "client_id": ClientID, "code": "the-code", "redirect_uri": "http://localhost:1455/auth/callback", "code_verifier": flow.verifier}
	if len(form) != len(want) {
		t.Fatalf("exchange fields = %v", form)
	}
	for key, value := range want {
		if form.Get(key) != value {
			t.Errorf("%s = %q, want %q", key, form.Get(key), value)
		}
	}
	second, err := http.Get(callbackAddress(flow, url.Values{"state": {flow.state}, "code": {"again"}}))
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("second successful callback status = %d", second.StatusCode)
	}
}

func TestC5BothRegisteredPortsBusySaysHowToRecover(t *testing.T) {
	// C5: two occupied callback ports produce the fact and the next action.
	_, err := Begin(context.Background(), Options{Random: strings.NewReader(strings.Repeat("r", 64)), Listen: func(string, string) (net.Listener, error) { return nil, os.ErrExist }})
	if err == nil || !strings.Contains(err.Error(), "both browser return ports are busy") || !strings.Contains(err.Error(), "finish or cancel") {
		t.Fatalf("busy-port error = %v", err)
	}
}

func TestC6TokenFileIsOwnerOnlyAndEveryTokenIsScrubbedOnLoad(t *testing.T) {
	// C6: codex.json is 0600 and loading it registers all three credentials.
	dir := t.TempDir()
	tokens := Tokens{AccessToken: "access-secret-value", RefreshToken: "refresh-secret-value", IDToken: "identity-secret-value"}
	if err := Save(dir, tokens); err != nil {
		t.Fatal(err)
	}
	mode, _ := os.Stat(Path(dir))
	if mode.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %o", mode.Mode().Perm())
	}
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	clean := string(trace.Scrub([]byte(tokens.AccessToken + " " + tokens.RefreshToken + " " + tokens.IDToken)))
	if strings.Contains(clean, "secret-value") {
		t.Fatalf("tokens survived scrub: %q", clean)
	}
}

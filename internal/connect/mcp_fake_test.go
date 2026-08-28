package connect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NO TEST IN THIS FILE TOUCHES THE NETWORK, the same law fake_test.go states.
// What is different is how much of a service has to be stood in for: a tool
// server is asked five separate questions before a person is ever sent to a
// browser, so the stand-in below answers all five — where its description lives,
// where its sign-in is, how a program introduces itself there, and the two ends
// of the browser trip — and then speaks the protocol itself.
//
// THE PROTOCOL HALF IS THE SDK'S OWN SERVER. Hand-writing an answer to
// "tools/list" would be testing this package's ability to imitate a protocol
// rather than its ability to speak it; the real server implementation is one
// import away and every message then crosses a real transport.

// fakeShape is what one test wants its stand-in to be, in the few ways they
// differ from each other.
type fakeShape struct {
	// blank makes the stand-in ask for one closed-list address answer. "here"
	// and "elsewhere" are both live paths so a test may move between them.
	blank bool
	// refuseIntroductions is a sign-in that will not let a program register
	// itself, which is the one thing that cannot be worked around.
	refuseIntroductions bool
	// stampsIssuer is a sign-in that promises to name itself on the way back.
	stampsIssuer bool
	// briefKeys is a service whose keys are stale the moment they are issued,
	// which is how the renewal path is reached without waiting an hour.
	briefKeys bool
}

// fakeToolServer is a stand-in for one service that brings its own tools.
type fakeToolServer struct {
	*httptest.Server
	fakeShape

	mu sync.Mutex
	// what it was asked, for the tests that are about the asking
	introductions int
	toolLists     int
	renewals      int
	askedResource []string
	askedProof    bool
	askedScope    string
	requests      []string
	// what it has issued
	clientID     string
	clientSecret string
	challenges   map[string]string
	live         map[string]bool
	refresh      string
	issued       int
}

// startFakeToolServer stands one up for the duration of a test.
func startFakeToolServer(t *testing.T, shape fakeShape) *fakeToolServer {
	t.Helper()
	fake := &fakeToolServer{fakeShape: shape}
	fake.challenges = map[string]string{}
	fake.live = map[string]bool{}
	fake.clientID = "issued-client"
	fake.clientSecret = "issued-secret"
	fake.refresh = "renewal-key"

	// The handlers go on before the server does, so that nothing is registered
	// on a mux that is already serving.
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource/here", fake.describe)
	mux.HandleFunc("/.well-known/oauth-protected-resource/elsewhere", fake.describe)
	mux.HandleFunc("/.well-known/oauth-authorization-server", fake.describeSignIn)
	mux.HandleFunc("/register", fake.introduce)
	mux.HandleFunc("/authorize", fake.authorize)
	mux.HandleFunc("/token", fake.issue)
	toolHandler := fake.guard(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return fake.tools(t) },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	))
	mux.Handle("/here", toolHandler)
	mux.Handle("/elsewhere", toolHandler)

	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.requests = append(fake.requests, r.URL.Path)
		fake.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(fake.Server.Close)
	return fake
}

// address is where the service answers, which is the one fact a plug for it
// carries.
func (f *fakeToolServer) address() string { return f.URL + "/here" }

// describe is the service's own description of itself (RFC 9728).
func (f *fakeToolServer) describe(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/.well-known/oauth-protected-resource")
	writeFakeJSON(w, map[string]any{
		"resource":              f.URL + path,
		"authorization_servers": []string{f.URL},
		"scopes_supported":      []string{"read", "write"},
	})
}

// describeSignIn is the sign-in describing itself (RFC 8414).
func (f *fakeToolServer) describeSignIn(w http.ResponseWriter, r *http.Request) {
	meta := map[string]any{
		"issuer":                                f.URL,
		"authorization_endpoint":                f.URL + "/authorize",
		"token_endpoint":                        f.URL + "/token",
		"jwks_uri":                              f.URL + "/keys",
		"response_types_supported":              []string{"code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
		"scopes_supported":                      []string{"read", "write", "offline_access"},
	}
	if !f.refuseIntroductions {
		meta["registration_endpoint"] = f.URL + "/register"
	}
	if f.stampsIssuer {
		meta["authorization_response_iss_parameter_supported"] = true
	}
	writeFakeJSON(w, meta)
}

// introduce is where a program registers itself (RFC 7591).
func (f *fakeToolServer) introduce(w http.ResponseWriter, r *http.Request) {
	var asked struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
		Scope        string   `json:"scope"`
	}
	_ = json.NewDecoder(r.Body).Decode(&asked)

	f.mu.Lock()
	f.introductions++
	f.askedScope = asked.Scope
	f.mu.Unlock()

	w.WriteHeader(http.StatusCreated)
	writeFakeJSON(w, map[string]any{
		"client_id":                  f.clientID,
		"client_secret":              f.clientSecret,
		"redirect_uris":              asked.RedirectURIs,
		"client_name":                asked.ClientName,
		"token_endpoint_auth_method": "client_secret_post",
	})
}

// authorize is the sign-in page, which agrees at once and sends the browser
// back where it came from.
func (f *fakeToolServer) authorize(w http.ResponseWriter, r *http.Request) {
	asked := r.URL.Query()
	code := fmt.Sprintf("code-%d", len(asked))

	f.mu.Lock()
	f.askedProof = asked.Get("code_challenge_method") == "S256" && asked.Get("code_challenge") != ""
	f.askedResource = append(f.askedResource, asked.Get("resource"))
	f.challenges[code] = asked.Get("code_challenge")
	f.mu.Unlock()

	back, err := url.Parse(asked.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "no way back", http.StatusBadRequest)
		return
	}
	answer := url.Values{"code": {code}, "state": {asked.Get("state")}}
	if f.stampsIssuer {
		answer.Set("iss", f.URL)
	}
	back.RawQuery = answer.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

// issue is the token address: the exchange, and every later renewal.
func (f *fakeToolServer) issue(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unreadable", http.StatusBadRequest)
		return
	}
	if r.Form.Get("client_id") != f.clientID {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		challenge, known := f.challenges[r.Form.Get("code")]
		if !known {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		if proof(r.Form.Get("code_verifier")) != challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		f.askedResource = append(f.askedResource, r.Form.Get("resource"))
	case "refresh_token":
		if r.Form.Get("refresh_token") != f.refresh {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		f.renewals++
	default:
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		return
	}

	f.issued++
	key := fmt.Sprintf("access-key-%d", f.issued)
	f.live[key] = true
	answer := map[string]any{
		"access_token":  key,
		"token_type":    "Bearer",
		"refresh_token": f.refresh,
		"scope":         "read write",
	}
	// A key that is already stale is how a test asks for the renewal path
	// without waiting an hour for it.
	answer["expires_in"] = 3600
	if f.briefKeys {
		answer["expires_in"] = -60
	}
	writeFakeJSON(w, answer)
}

// guard is the service refusing anybody without keys, and saying where its
// description lives while it does so.
func (f *fakeToolServer) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		known := f.live[key]
		f.mu.Unlock()
		f.count(r)
		if key == "" || !known {
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource%s"`, f.URL, r.URL.Path))
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// count notices what the service was asked for, which is how a test proves that
// a tool list is fetched once and not once per question.
func (f *fakeToolServer) count(r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return
	}
	if !strings.Contains(string(body), `"tools/list"`) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolLists++
}

// tools is the service's own tool list: one that only looks, one that acts, one
// that refuses, and one that says far more than anybody can read.
func (f *fakeToolServer) tools(t *testing.T) *mcp.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "example", Version: "1"}, nil)

	server.AddTool(&mcp.Tool{
		Name:        "find_pages",
		Description: "Search the pages in this account.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
	}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return said("Found: " + string(request.Params.Arguments)), nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "write_page",
		Description: "Add a page to this account.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
	}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return said("Wrote " + string(request.Params.Arguments)), nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "refuse",
		Description: "Always refuses.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		answer := said("that page is not yours to read")
		answer.IsError = true
		return answer, nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "flood",
		Description: "Says more than anybody can read.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return said(strings.Repeat("a line of an answer nobody asked for\n", 2000)), nil
	})

	return server
}

// counted answers how many times the stand-in was asked for each of the things
// a test cares about.
func (f *fakeToolServer) counted() (introductions, lists, renewals int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.introductions, f.toolLists, f.renewals
}

// resources is every value the resource indicator carried, in order.
func (f *fakeToolServer) resources() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.askedResource...)
}

// paths is every address the stand-in was asked, in order.
func (f *fakeToolServer) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// said is one plain answer from a tool.
func said(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// proof is the challenge a verifier makes, as the specification computes it.
func proof(verifier string) string {
	if verifier == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// writeFakeJSON answers a request with an encoded value, the way the real
// services do.
func writeFakeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// withToolServer puts one stand-in on the registry as the only service this
// build has, and hands back a manager over a fresh profile directory with NO
// client credentials at all — which is the point: a tool server is offered in a
// build that was configured with nothing.
func withToolServer(t *testing.T, fake *fakeToolServer) (*Manager, *toolServer) {
	t.Helper()
	plug := &toolServer{
		service: fakeToolService(fake.blank),
		address: fake.address(),
	}
	if fake.blank {
		plug.address = fake.URL + "/{{.site}}"
		plug.blank = blank{name: "site", label: "Site"}
		plug.answers = []string{"here", "elsewhere"}
	}
	withPlugs(t, plug)

	toolServerMu.Lock()
	toolServerIDs[plug.service.ID] = true
	toolServerMu.Unlock()
	t.Cleanup(func() {
		toolServerMu.Lock()
		delete(toolServerIDs, plug.service.ID)
		toolServerMu.Unlock()
	})
	t.Cleanup(func() { forgetTools(plug.service.ID) })
	forgetTools(plug.service.ID)

	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, plug
}

// fakeToolService is the menu line the stand-in uses. Keeping it whole lets the
// catalog's vocabulary law read the same words these tests register.
func fakeToolService(asks bool) Service {
	service := Service{
		ID:       "example",
		Name:     "Example",
		Category: categoryProductivity,
		Blurb:    "Example's own tools, signed in in your browser.",
		Auth:     AuthBrowser,
	}
	if asks {
		service.Blank = "Site"
		service.Answers = []string{"here", "elsewhere"}
		service.KeyAsk = "Which Example site is your account on? Choose here or elsewhere."
	}
	return service
}

// openInBrowser does what a person does: opens the address they were given, and
// lets the browser follow wherever it is sent.
func openInBrowser(t *testing.T, flow *Flow) {
	t.Helper()
	response, err := http.Get(flow.URL())
	if err != nil {
		t.Fatalf("open %q: %v", flow.URL(), err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the browser trip ended at %s", response.Status)
	}
}

package serve

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The contract items these tests are written from are P1-4 (a chat's model
// access survives a restart and an ended one stays ended), P1-5 (a person's own
// keys), P1-7 (a restart breaks no attached chat) and P1-9 (plain words), in
// the lane's NOTES.md. Each test says which it covers.

const (
	// rigServerKey is a server key as an operator writes it: 64 hex characters.
	rigServerKey      = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	rigOtherServerKey = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
)

func (rg *hostedRig) withServerKey(t *testing.T, key string) {
	t.Helper()
	rg.configure(t, func(c *HostedConfig) { c.ServerKey = key })
}

func (rg *hostedRig) file(name string) string { return filepath.Join(rg.st.Root(), name) }

func (rg *hostedRig) readFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(rg.file(name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return string(raw)
}

// model makes one model call with a chat's bearer and answers the status.
func (rg *hostedRig) model(token string) int {
	return rg.send("", "GET", "/v1/model/models", nil, map[string]string{"Authorization": "Bearer " + token}).Code
}

// putKey saves a person's own key through the route and answers the status.
func (rg *hostedRig) putKey(who, provider, key string) *httptest.ResponseRecorder {
	return rg.send(who, "PUT", "/v1/hosted/keys/"+provider, map[string]string{"key": key}, nil)
}

// dial opens one window's wire to a chat on a real server.
func dial(t *testing.T, srv *httptest.Server, bearerOfPerson, id string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, wireURL(srv, id), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + bearerOfPerson}}})
	if err != nil {
		t.Fatalf("the wire did not open: %v %v", resp, err)
	}
	c.SetReadLimit(1 << 20)
	if err := c.Write(ctx, websocket.MessageBinary, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, got, err := c.Read(ctx); err != nil || string(got) != "hello" {
		t.Fatalf("the wire did not carry a frame: %q %v", got, err)
	}
	return c
}

// ── P1-4: THE MODEL ACCESS OF A CHAT SURVIVES A RESTART AND AN ENDED ONE STAYS ENDED ──

// A bearer minted before the organization's server restarted still works after
// it; a stopped chat's bearer stays dead across a restart; one that has run out
// is gone, and its row leaves the saved file.
func TestP1_4_BearerSurvivesARestartOfTheServer(t *testing.T) {
	rg := newHostedRig(t)
	id := rg.create(t, "santosh")
	token := rg.host.tokenOf(id)
	if code := rg.model(token); code != 200 {
		t.Fatalf("a fresh bearer = %d", code)
	}
	rg.configure(t, func(*HostedConfig) {})
	if code := rg.model(token); code != 200 {
		t.Fatalf("the same bearer after a restart = %d; the chat's engine kept it and must still be answered", code)
	}

	// Stop ends it at once, and it stays ended after another restart.
	if w := rg.send("santosh", "POST", "/v1/hosted/sessions/"+id+"/stop", nil, nil); w.Code != 204 {
		t.Fatalf("stop = %d %s", w.Code, w.Body)
	}
	if code := rg.model(token); code != 401 {
		t.Fatalf("a stopped chat's bearer = %d; want 401 on its very next call", code)
	}
	rg.configure(t, func(*HostedConfig) {})
	if code := rg.model(token); code != 401 {
		t.Fatalf("a stopped chat's bearer after a restart = %d; want 401", code)
	}
	if strings.Contains(rg.readFile(t, hostedBearersFileName), bearerHash(token)) {
		t.Fatal("the saved file still holds the stopped chat's bearer")
	}

	// A deleted chat likewise.
	gone := rg.create(t, "santosh")
	goneToken := rg.host.tokenOf(gone)
	if w := rg.send("santosh", "DELETE", "/v1/hosted/sessions/"+gone, nil, nil); w.Code != 204 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	rg.configure(t, func(*HostedConfig) {})
	if code := rg.model(goneToken); code != 401 {
		t.Fatalf("a deleted chat's bearer after a restart = %d", code)
	}

	// Seven days is how long one lasts: alive a minute before, gone a minute
	// after, and gone from the file once the server has started again.
	aging := rg.create(t, "santosh")
	agingToken := rg.host.tokenOf(aging)
	rg.now = rg.now.Add(mintedBearerTTL - time.Minute)
	rg.configure(t, func(*HostedConfig) {})
	if code := rg.model(agingToken); code != 200 {
		t.Fatalf("a bearer a minute before its end = %d", code)
	}
	rg.now = rg.now.Add(2 * time.Minute)
	if code := rg.model(agingToken); code != 401 {
		t.Fatalf("a bearer a minute past its end = %d", code)
	}
	rg.configure(t, func(*HostedConfig) {})
	if code := rg.model(agingToken); code != 401 {
		t.Fatalf("an expired bearer after a restart = %d", code)
	}
	if strings.Contains(rg.readFile(t, hostedBearersFileName), bearerHash(agingToken)) {
		t.Fatal("the saved file still holds a row that ran out")
	}
}

// A bearer that cannot be saved is not issued: the chat is not started, the
// person is told so in a plain sentence, and nothing was asked of the host.
func TestP1_4_ABearerThatCannotBeSavedIsNotIssued(t *testing.T) {
	rg := newHostedRig(t)
	// A directory where the file belongs makes every save fail without
	// depending on who the tests run as.
	if err := os.Mkdir(rg.file(hostedBearersFileName), 0o700); err != nil {
		t.Fatal(err)
	}
	before := rg.host.count()
	w := rg.send("santosh", "POST", "/v1/hosted/sessions", nil, nil)
	if w.Code != 500 || errorOf(t, w.Body.Bytes()) != hostedNotStarted {
		t.Fatalf("create with the save failing = %d %s", w.Code, w.Body)
	}
	if rg.host.count() != before {
		t.Fatalf("the host was asked about a chat whose model access could not be kept: %+v", rg.host.requests()[before:])
	}
	if err := os.Remove(rg.file(hostedBearersFileName)); err != nil {
		t.Fatal(err)
	}
	if id := rg.create(t, "santosh"); rg.model(rg.host.tokenOf(id)) != 200 {
		t.Fatal("after the save works again a chat starts and is answered")
	}
}

// The file that keeps bearers across a restart holds their hashes and never
// one of them.
func TestP1_4_PersistedFileHoldsNoToken(t *testing.T) {
	rg := newHostedRig(t)
	id := rg.create(t, "santosh")
	token := rg.host.tokenOf(id)
	raw := rg.readFile(t, hostedBearersFileName)
	if strings.Contains(raw, token) || strings.Contains(raw, strings.TrimPrefix(token, mintedBearerPrefix)) || strings.Contains(raw, mintedBearerPrefix) {
		t.Fatalf("the saved file holds the bearer: %s", raw)
	}
	var doc struct {
		Rows map[string]struct{ Session, Person string }
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	row, ok := doc.Rows[bearerHash(token)]
	if !ok || row.Session != id || row.Person != "santosh" || len(doc.Rows) != 1 {
		t.Fatalf("the saved rows = %+v; want only the hash of the chat's bearer", doc.Rows)
	}
	if info, err := os.Stat(rg.file(hostedBearersFileName)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the saved file's mode = %v, %v; want 0600", info, err)
	}
}

const hostedBearersFileName = "hosted-bearers.json"

// ── P1-5: A PERSON'S OWN KEYS ──

// A key a person stores is sealed on disk, listed by its last four characters,
// returned to that person's chat only, and forgotten on delete.
func TestP1_5_OwnKeyIsStoredSealedAndOnlyForItsPerson(t *testing.T) {
	rg := newHostedRig(t)
	rg.withServerKey(t, rigServerKey)
	santoshKey, abirKey, santoshGitHub := "sk-test-santosh-aaaaaaaa-0001", "sk-test-abir-bbbbbbbbbb-0002", "ghp_test_santosh_0003"
	for _, put := range []struct{ who, provider, key string }{
		{"santosh", "openrouter", santoshKey}, {"santosh", "github", santoshGitHub}, {"abir", "openrouter", abirKey}} {
		if w := rg.putKey(put.who, put.provider, "  "+put.key+"\n"); w.Code != 204 || w.Body.Len() != 0 {
			t.Fatalf("%s stores %s = %d %s", put.who, put.provider, w.Code, w.Body)
		}
	}
	file := rg.readFile(t, "person-keys.json")
	for _, secret := range []string{santoshKey, abirKey, santoshGitHub, "aaaaaaaa", "bbbbbbbbbb", rigServerKey} {
		if strings.Contains(file, secret) {
			t.Fatalf("the stored file holds %q: %s", secret, file)
		}
	}
	// Whose key goes out with whose call.
	for _, want := range []struct {
		who, provider, key string
		own                bool
	}{
		{"santosh", "openrouter", santoshKey, true}, {"santosh", "github", santoshGitHub, true}, {"abir", "openrouter", abirKey, true},
		{"abir", "github", "", false}, {"mia", "openrouter", "", false}, {"mia", "github", "", false}, {"santos", "openrouter", "", false}, {"", "openrouter", "", false},
	} {
		key, own, err := rg.h.hostedKeyFor(want.who, want.provider)
		if key != want.key || own != want.own || err != nil {
			t.Fatalf("%q's %s key = %q, %v, %v; want %q, %v", want.who, want.provider, key, own, err, want.key, want.own)
		}
	}
	// A handle's display forms are the same person.
	if key, own, err := rg.h.hostedKeyFor("@Santosh", "openrouter"); key != santoshKey || !own || err != nil {
		t.Fatalf("the display form of a handle = %q, %v, %v", key, own, err)
	}
	// The list: only the caller's, last four only, never null.
	type listed struct {
		Keys []struct{ Provider, Last4, At, Source string } `json:"keys"`
	}
	list := func(who string) (listed, string) {
		t.Helper()
		w := rg.send(who, "GET", "/v1/hosted/keys", nil, nil)
		var out listed
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("list as %s = %d %s", who, w.Code, w.Body)
		}
		return out, w.Body.String()
	}
	got, body := list("santosh")
	if len(got.Keys) != 2 || got.Keys[0].Provider != "github" || got.Keys[0].Last4 != "0003" || got.Keys[0].Source != "own" ||
		got.Keys[1].Provider != "openrouter" || got.Keys[1].Last4 != "0001" || got.Keys[1].At == "" {
		t.Fatalf("santosh's list = %s", body)
	}
	for _, secret := range []string{santoshKey, santoshGitHub, "sk-test", "ghp_", "aaaaaaaa", abirKey} {
		if strings.Contains(body, secret) {
			t.Fatalf("the list holds %q: %s", secret, body)
		}
	}
	if got, body := list("abir"); len(got.Keys) != 1 || got.Keys[0].Last4 != "0002" || strings.Contains(body, "0001") {
		t.Fatalf("abir's list = %s", body)
	}
	if _, body := list("mia"); strings.TrimSpace(body) != `{"keys":[]}` {
		t.Fatalf("a person with no keys lists %s; want an empty list", body)
	}
	// What was stored is still there after the server restarts.
	rg.withServerKey(t, rigServerKey)
	if key, own, err := rg.h.hostedKeyFor("santosh", "openrouter"); key != santoshKey || !own || err != nil {
		t.Fatalf("after a restart = %q, %v, %v", key, own, err)
	}
	// Delete answers only about the caller: abir deleting "github" (which only
	// santosh has) touches nothing, and santosh's delete forgets santosh's.
	if w := rg.send("abir", "DELETE", "/v1/hosted/keys/github", nil, nil); w.Code != 204 {
		t.Fatalf("abir's delete of a key he does not have = %d", w.Code)
	}
	if key, _, _ := rg.h.hostedKeyFor("santosh", "github"); key != santoshGitHub {
		t.Fatal("another person's delete removed santosh's key")
	}
	for i := 0; i < 2; i++ {
		if w := rg.send("santosh", "DELETE", "/v1/hosted/keys/openrouter", nil, nil); w.Code != 204 {
			t.Fatalf("delete #%d = %d %s", i+1, w.Code, w.Body)
		}
	}
	if key, own, err := rg.h.hostedKeyFor("santosh", "openrouter"); key != "" || own || err != nil {
		t.Fatalf("after the delete = %q, %v, %v", key, own, err)
	}
	if key, _, _ := rg.h.hostedKeyFor("abir", "openrouter"); key != abirKey {
		t.Fatal("santosh's delete removed abir's key")
	}
}

// A person's own save replaces what their sign-in brought, a sign-in never
// replaces what the person saved, and a wire attach keeps the sign-in's GitHub
// credential for a person who has none.
func TestP1_5_SignInGitHubCredentialIsKeptForAChatButNeverOverANewerOwnKey(t *testing.T) {
	rg := newHostedRig(t)
	rg.withServerKey(t, rigServerKey)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	id := rg.create(t, "santosh")
	if key, own, _ := rg.h.hostedKeyFor("santosh", "github"); own || key != "" {
		t.Fatalf("a chat's creation stored a credential: %q", key)
	}
	dial(t, srv, rg.tokens["santosh"], id).Close(websocket.StatusNormalClosure, "")
	signedIn, own, err := rg.h.hostedKeyFor("santosh", "github")
	if !own || err != nil || len(signedIn) < 8 {
		t.Fatalf("after an attach the sign-in's credential = %q, %v, %v", signedIn, own, err)
	}
	var listed struct {
		Keys []struct{ Provider, Source string }
	}
	w := rg.send("santosh", "GET", "/v1/hosted/keys", nil, nil)
	if json.Unmarshal(w.Body.Bytes(), &listed) != nil || len(listed.Keys) != 1 || listed.Keys[0].Source != "sign-in" {
		t.Fatalf("the kept credential's source = %s", w.Body)
	}
	if strings.Contains(w.Body.String(), signedIn) || strings.Contains(rg.readFile(t, "person-keys.json"), signedIn) {
		t.Fatal("the sign-in's credential is in the clear")
	}
	if key, _, _ := rg.h.hostedKeyFor("abir", "github"); key != "" {
		t.Fatalf("santosh's sign-in credential answered for abir: %q", key)
	}
	// The person's own key replaces it and survives the next attach.
	if w := rg.putKey("santosh", "github", "ghp_test_santosh_own_0007"); w.Code != 204 {
		t.Fatalf("own save = %d %s", w.Code, w.Body)
	}
	dial(t, srv, rg.tokens["santosh"], id).Close(websocket.StatusNormalClosure, "")
	if key, _, _ := rg.h.hostedKeyFor("santosh", "github"); key != "ghp_test_santosh_own_0007" {
		t.Fatalf("an attach replaced the person's own key with %q", key)
	}
	w = rg.send("santosh", "GET", "/v1/hosted/keys", nil, nil)
	if json.Unmarshal(w.Body.Bytes(), &listed) != nil || len(listed.Keys) != 1 || listed.Keys[0].Source != "own" {
		t.Fatalf("the own key's source = %s", w.Body)
	}
}

// A server key that is not the one the stored keys were sealed with refuses to
// start the server, names the setting, and leaves the stored keys untouched; a
// running handler with the wrong key never opens a key and never falls back.
func TestP1_5_WrongServerKeyRefusesToStart(t *testing.T) {
	rg := newHostedRig(t)
	rg.withServerKey(t, rigServerKey)
	if w := rg.putKey("santosh", "openrouter", "sk-test-santosh-0001-aaaa"); w.Code != 204 {
		t.Fatalf("store = %d %s", w.Code, w.Body)
	}
	before := rg.readFile(t, "person-keys.json")
	cfg := &HostedConfig{ServerKey: rigOtherServerKey}
	err := cfg.Check(rg.st)
	if err == nil || err.Error() != serverKeyWrong || !strings.Contains(err.Error(), "CODEAF_SERVER_KEY") ||
		strings.Contains(err.Error(), rigOtherServerKey) || strings.Contains(err.Error(), rigServerKey) {
		t.Fatalf("a wrong server key checked as %v", err)
	}
	if rg.readFile(t, "person-keys.json") != before {
		t.Fatal("refusing changed the stored keys")
	}
	if err := (&HostedConfig{ServerKey: rigServerKey}).Check(rg.st); err != nil {
		t.Fatalf("the right server key = %v", err)
	}
	// A handler that was started anyway never opens a key and never falls back
	// to the organization's: santosh's call is an error, a person with no key
	// is still no key at all, and nothing can be stored.
	rg.withServerKey(t, rigOtherServerKey)
	if key, own, err := rg.h.hostedKeyFor("santosh", "openrouter"); key != "" || own || err == nil {
		t.Fatalf("with the wrong key santosh's key = %q, %v, %v; want an error", key, own, err)
	}
	if key, own, err := rg.h.hostedKeyFor("abir", "openrouter"); key != "" || own || err != nil {
		t.Fatalf("with the wrong key a person with none = %q, %v, %v", key, own, err)
	}
	if w := rg.putKey("abir", "openrouter", "sk-test-abir-000000-bbbb"); w.Code != 409 || errorOf(t, w.Body.Bytes()) != hostedKeysCannotKeep {
		t.Fatalf("a save with the wrong key = %d %s", w.Code, w.Body)
	}
	if rg.readFile(t, "person-keys.json") != before {
		t.Fatal("a handler with the wrong key changed the stored keys")
	}
}

// Stored keys with no server key set refuse to start; with none stored, no
// server key is no trouble.
func TestP1_5_StoredKeysWithNoServerKeyRefuseToStart(t *testing.T) {
	rg := newHostedRig(t)
	if err := (&HostedConfig{}).Check(rg.st); err != nil {
		t.Fatalf("no server key and nothing stored = %v", err)
	}
	rg.withServerKey(t, rigServerKey)
	if w := rg.putKey("santosh", "openrouter", "sk-test-santosh-0001-aaaa"); w.Code != 204 {
		t.Fatalf("store = %d %s", w.Code, w.Body)
	}
	before := rg.readFile(t, "person-keys.json")
	for _, blank := range []string{"", "  \n"} {
		err := (&HostedConfig{ServerKey: blank}).Check(rg.st)
		if err == nil || err.Error() != serverKeyMissing || !strings.Contains(err.Error(), "CODEAF_SERVER_KEY") {
			t.Fatalf("stored keys with the server key %q checked as %v", blank, err)
		}
	}
	if rg.readFile(t, "person-keys.json") != before {
		t.Fatal("refusing changed the stored keys")
	}
	// A handler started anyway refuses santosh's calls instead of using the
	// organization's key.
	rg.configure(t, func(*HostedConfig) {})
	if key, own, err := rg.h.hostedKeyFor("santosh", "openrouter"); key != "" || own || err == nil {
		t.Fatalf("with no server key santosh's key = %q, %v, %v; want an error", key, own, err)
	}
	if _, _, err := rg.h.hostedKeyFor("abir", "openrouter"); err != nil {
		t.Fatalf("a person with none, no server key: %v", err)
	}
	// A server key that is not a key at all.
	for _, bad := range []string{"short", strings.Repeat("g", 64), strings.Repeat("ab", 31), "!!!!"} {
		err := (&HostedConfig{ServerKey: bad}).Check(rg.st)
		if err == nil || err.Error() != serverKeyForm || strings.Contains(err.Error(), bad) {
			t.Fatalf("the server key %q checked as %v", bad, err)
		}
	}
}

// With no server key at all, hosted chats still run on the organization's key,
// and saving an own key is refused with a plain sentence and stores nothing.
func TestP1_5_NoServerKeyMeansOwnKeysAreRefusedWithASentence(t *testing.T) {
	rg := newHostedRig(t)
	w := rg.putKey("santosh", "openrouter", "sk-test-santosh-0001-aaaa")
	if w.Code != 409 || errorOf(t, w.Body.Bytes()) != "This server cannot keep personal keys safe yet, so it did not store yours. Its operator can set that up." {
		t.Fatalf("a save with no server key = %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(rg.file("person-keys.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused key left a file behind: %v", err)
	}
	if w := rg.send("santosh", "GET", "/v1/hosted/keys", nil, nil); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"keys":[]}` {
		t.Fatalf("list with no server key = %d %s", w.Code, w.Body)
	}
	if w := rg.send("santosh", "DELETE", "/v1/hosted/keys/openrouter", nil, nil); w.Code != 204 {
		t.Fatalf("delete with no server key = %d", w.Code)
	}
	if key, own, err := rg.h.hostedKeyFor("santosh", "openrouter"); key != "" || own || err != nil {
		t.Fatalf("with no server key a call goes out with %q, %v, %v; want the organization's (none, no error)", key, own, err)
	}
	id := rg.create(t, "santosh")
	if code := rg.model(rg.host.tokenOf(id)); code != 200 {
		t.Fatalf("a chat with no server key = %d", code)
	}
}

// Every key route needs a signed-in person, and a stranger, this machine and
// the operator's own access token all get the sign-in sentence and change
// nothing.
func TestP1_5_KeyRoutesNeedASignIn(t *testing.T) {
	rg := newHostedRig(t)
	rg.withServerKey(t, rigServerKey)
	// Since the organization server stopped trusting its own computer by default (loopback_trust), a request from this machine with no
	// credential is refused before it reaches the key routes. The test turns that trust on so it still checks what it is about: a caller the
	// server lets in without a verified person gets the sign-in sentence. The refusal without the trust is pinned just below.
	trust := true
	rg.h.config.LoopbackTrust = &trust
	as := func(remote, auth, method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"key":"sk-test-santosh-0001-aaaa"}`))
		r.RemoteAddr = remote
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		rg.h.ServeHTTP(w, r)
		return w
	}
	for _, step := range []struct{ method, path string }{
		{"GET", "/v1/hosted/keys"}, {"PUT", "/v1/hosted/keys/openrouter"}, {"PUT", "/v1/hosted/keys/github"}, {"DELETE", "/v1/hosted/keys/openrouter"}} {
		for name, w := range map[string]*httptest.ResponseRecorder{
			"stranger":       as("192.0.2.1:9", "", step.method, step.path),
			"this machine":   as("127.0.0.1:9", "", step.method, step.path),
			"operator token": as("192.0.2.1:9", "operator-secret", step.method, step.path),
		} {
			if w.Code != 401 || (name != "stranger" && errorOf(t, w.Body.Bytes()) != hostedSignIn) {
				t.Fatalf("%s %s as %s = %d %s; want 401 and the sign-in sentence", step.method, step.path, name, w.Code, w.Body)
			}
		}
	}
	trust = false
	if w := as("127.0.0.1:9", "", "GET", "/v1/hosted/keys"); w.Code != 401 {
		t.Fatalf("this machine without loopback trust = %d %s; want 401", w.Code, w.Body)
	}
	if _, err := os.Stat(rg.file("person-keys.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused request stored something: %v", err)
	}
}

// What a key route accepts and what it refuses.
func TestP1_5_KeyRoutesCheckWhatTheyAreGiven(t *testing.T) {
	rg := newHostedRig(t)
	rg.withServerKey(t, rigServerKey)
	raw := func(who, method, path, body string, length int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+rg.tokens[who])
		if length != 0 {
			r.ContentLength = length
		}
		w := httptest.NewRecorder()
		rg.h.ServeHTTP(w, r)
		return w
	}
	for _, step := range []struct {
		name, method, path, body string
		length                   int64
		status                   int
		words                    string
	}{
		{"unknown provider", "PUT", "/v1/hosted/keys/anthropic", `{"key":"sk-test-santosh-0001-aaaa"}`, 0, 404, hostedKeysNoSuchKey},
		{"unknown provider on delete", "DELETE", "/v1/hosted/keys/anthropic", "", 0, 404, hostedKeysNoSuchKey},
		{"too short for a model key", "PUT", "/v1/hosted/keys/openrouter", `{"key":"sk-short"}`, 0, 422, "That does not look like a valid key: one word of 12 to 512 characters."},
		{"too short for a GitHub key", "PUT", "/v1/hosted/keys/github", `{"key":"short"}`, 0, 422, "That does not look like a valid key: one word of 8 to 512 characters."},
		{"a space inside", "PUT", "/v1/hosted/keys/openrouter", `{"key":"sk-test with-a-space-0001"}`, 0, 422, "That does not look like a valid key: one word of 12 to 512 characters."},
		{"too long", "PUT", "/v1/hosted/keys/openrouter", `{"key":"` + strings.Repeat("k", 513) + `"}`, 0, 422, "That does not look like a valid key: one word of 12 to 512 characters."},
		{"not an object", "PUT", "/v1/hosted/keys/openrouter", `"sk-test-santosh-0001-aaaa"`, 0, 422, hostedKeysBadRequest},
		{"an extra field", "PUT", "/v1/hosted/keys/openrouter", `{"key":"sk-test-santosh-0001-aaaa","owner":"abir"}`, 0, 422, hostedKeysBadRequest},
		{"no body", "PUT", "/v1/hosted/keys/openrouter", ``, 0, 422, hostedKeysBadRequest},
		{"announced as large", "PUT", "/v1/hosted/keys/openrouter", `{"key":"sk-test-santosh-0001-aaaa"}`, 5000, 413, hostedKeysTooLarge},
		{"sent as large with no length", "PUT", "/v1/hosted/keys/openrouter", `{"key":"` + strings.Repeat("k", 5000) + `"}`, -1, 413, hostedKeysTooLarge},
	} {
		w := raw("santosh", step.method, step.path, step.body, step.length)
		if w.Code != step.status || errorOf(t, w.Body.Bytes()) != step.words {
			t.Fatalf("%s: %d %s; want %d %q", step.name, w.Code, w.Body, step.status, step.words)
		}
	}
	if _, err := os.Stat(rg.file("person-keys.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused request stored something: %v", err)
	}
	// The shortest and the longest that are allowed.
	if w := raw("santosh", "PUT", "/v1/hosted/keys/github", `{"key":"12345678"}`, 0); w.Code != 204 {
		t.Fatalf("an eight-character GitHub key = %d %s", w.Code, w.Body)
	}
	if w := raw("santosh", "PUT", "/v1/hosted/keys/openrouter", `{"key":"`+strings.Repeat("k", 512)+`"}`, 0); w.Code != 204 {
		t.Fatalf("a 512-character model key = %d %s", w.Code, w.Body)
	}
}

// parseServerKey reads every form an operator is likely to write, and nothing
// that is not 32 bytes.
func TestP1_5_ServerKeyForms(t *testing.T) {
	want := make([]byte, 32)
	for i := range want {
		want[i] = byte(i * 7)
	}
	// The first bytes make the standard and the URL alphabets differ.
	want[0], want[1] = 0xfb, 0xff
	forms := map[string]string{
		"hex":                 hex.EncodeToString(want),
		"hex with newline":    hex.EncodeToString(want) + "\n",
		"upper hex":           strings.ToUpper(hex.EncodeToString(want)),
		"std base64":          base64.StdEncoding.EncodeToString(want),
		"std base64 unpadded": base64.RawStdEncoding.EncodeToString(want),
		"url base64":          base64.URLEncoding.EncodeToString(want),
		"url base64 unpadded": base64.RawURLEncoding.EncodeToString(want),
	}
	if forms["std base64"] == forms["url base64"] {
		t.Fatal("the test key does not tell the two alphabets apart")
	}
	for name, form := range forms {
		got, err := parseServerKey(form)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: %x, %v", name, got, err)
		}
	}
	for _, bad := range []string{"", "   ", "abc", strings.Repeat("0", 63), strings.Repeat("0", 66), strings.Repeat("z", 64),
		base64.StdEncoding.EncodeToString(want[:30]), base64.StdEncoding.EncodeToString(append(want, 1))} {
		if got, err := parseServerKey(bad); err == nil || got != nil || err.Error() != serverKeyForm {
			t.Fatalf("%q parsed as %x, %v", bad, got, err)
		}
	}
}

// ── P1-7: A RESTART DOES NOT BREAK ATTACHED CHATS ──

// A host that restarted and forgot its credentials is given a new bearer
// before the wire opens: the window sees no refusal, the host holds a new
// bearer, and the old one is ended.
func TestP1_7_AttachAfterHostRestartPushesBeforeTheWireOpens(t *testing.T) {
	rg := newHostedRig(t)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	id := rg.create(t, "santosh")
	old := rg.host.tokenOf(id)
	rg.host.adopt(id, "santosh") // the chats' host restarted and holds nothing
	before := rg.host.count()
	dial(t, srv, rg.tokens["santosh"], id).Close(websocket.StatusNormalClosure, "")
	fresh := rg.host.tokenOf(id)
	if fresh == "" || fresh == old || !strings.HasPrefix(fresh, mintedBearerPrefix) {
		t.Fatalf("the host holds %q after the attach; want a new bearer (was %q)", fresh, old)
	}
	var order []string
	for _, seen := range rg.host.requests()[before:] {
		order = append(order, seen.method+" "+strings.TrimPrefix(seen.path, "/v1/sessions/"+id))
	}
	if strings.Join(order, ",") != "GET ,PUT /credentials,GET /wire" {
		t.Fatalf("the host was asked %v; want its row read, then the bearer pushed, then the wire", order)
	}
	if rg.model(old) != 401 || rg.model(fresh) != 200 {
		t.Fatalf("the old bearer = %d and the new one = %d; want 401 and 200", rg.model(old), rg.model(fresh))
	}
}

// A host that holds the organization's current bearer is pushed nothing.
func TestP1_7_AttachWithCurrentHostPushesNothing(t *testing.T) {
	rg := newHostedRig(t)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	id := rg.create(t, "santosh")
	token := rg.host.tokenOf(id)
	before := rg.host.count()
	for i := 0; i < 3; i++ {
		dial(t, srv, rg.tokens["santosh"], id).Close(websocket.StatusNormalClosure, "")
	}
	if len(rg.host.pushList()) != 1 || rg.host.tokenOf(id) != token {
		t.Fatalf("an attach with a current host pushed: %+v", rg.host.pushList())
	}
	for _, seen := range rg.host.requests()[before:] {
		if seen.method == "PUT" || seen.method == "POST" {
			t.Fatalf("an attach with a current host made a %s %s", seen.method, seen.path)
		}
	}
	if rg.model(token) != 200 {
		t.Fatal("the bearer stopped working")
	}
}

// The organization's server restarting changes nothing for an attached chat: the
// bearer the engine already holds is the one the server still recognises, and
// the attach pushes nothing.
func TestP1_7_AttachAfterOrgRestartKeepsTheEnginesToken(t *testing.T) {
	rg := newHostedRig(t)
	id := rg.create(t, "santosh")
	token := rg.host.tokenOf(id)
	rg.configure(t, func(*HostedConfig) {})
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	dial(t, srv, rg.tokens["santosh"], id).Close(websocket.StatusNormalClosure, "")
	if len(rg.host.pushList()) != 1 || rg.host.tokenOf(id) != token {
		t.Fatalf("after the server restarted the attach pushed: %+v", rg.host.pushList())
	}
	if rg.model(token) != 200 {
		t.Fatal("the engine's bearer stopped working after the restart")
	}
}

// An organization that lost its saved table (the file is gone) still puts a
// chat right at its next attach: the host's bearer is one the server no longer
// knows, so it is replaced before the wire opens.
func TestP1_7_AttachAfterALostTableReplacesTheBearer(t *testing.T) {
	rg := newHostedRig(t)
	id := rg.create(t, "santosh")
	old := rg.host.tokenOf(id)
	if err := os.Remove(rg.file(hostedBearersFileName)); err != nil {
		t.Fatal(err)
	}
	rg.configure(t, func(*HostedConfig) {})
	if rg.model(old) != 401 {
		t.Fatal("a bearer the server has no row for was answered")
	}
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	dial(t, srv, rg.tokens["santosh"], id).Close(websocket.StatusNormalClosure, "")
	fresh := rg.host.tokenOf(id)
	if fresh == old || rg.model(fresh) != 200 || len(rg.host.pushList()) != 2 {
		t.Fatalf("after a lost table: host holds %q (was %q), pushes %+v", fresh, old, rg.host.pushList())
	}
}

// What the org tells a chat's host about git rides the same body as the model
// access, only when the model address allows it to be derived, and carries no
// secret.
func TestP1_CredentialBodyCarriesGitOnlyWhenTheAddressAllowsIt(t *testing.T) {
	fieldsOf := func(seen hostSeen) map[string]json.RawMessage {
		t.Helper()
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(seen.body), &fields); err != nil {
			t.Fatalf("a push body that is not an object: %q", seen.body)
		}
		return fields
	}
	rg := newHostedRig(t)
	id := rg.create(t, "santosh")
	created := fieldsOf(rg.host.requests()[0])
	if string(created["id"]) != `"`+id+`"` || string(created["model_url"]) != `"`+rigModelURL+`"` ||
		string(created["git_url"]) != `"http://codeaf.test/v1/hosted/git"` || string(created["git_hosts"]) != `["https://github.com"]` ||
		!strings.HasPrefix(string(created["model_token"]), `"cmt_`) {
		t.Fatalf("the create body = %s", rg.host.requests()[0].body)
	}
	// The operator's list is normalized: lower case, no slash, no repeats.
	rg.configure(t, func(c *HostedConfig) {
		c.GitHosts = []string{"HTTPS://GitHub.com/", "https://git.example.org:8443", "https://github.com", "ftp://nope", "https://x.test/path"}
	})
	rg.create(t, "santosh")
	last := rg.host.requests()
	fields := fieldsOf(last[len(last)-1])
	if string(fields["git_hosts"]) != `["https://github.com","https://git.example.org:8443"]` {
		t.Fatalf("git_hosts = %s", fields["git_hosts"])
	}
	// A model address that does not end in /v1/model leaves git out entirely.
	rg.configure(t, func(c *HostedConfig) { c.ModelURL = "http://codeaf.test/models" })
	rg.create(t, "santosh")
	last = rg.host.requests()
	fields = fieldsOf(last[len(last)-1])
	if _, ok := fields["git_url"]; ok {
		t.Fatalf("git was offered with no address to derive it from: %s", last[len(last)-1].body)
	}
	if _, ok := fields["git_hosts"]; ok {
		t.Fatalf("git hosts were offered with no address to derive it from: %s", last[len(last)-1].body)
	}
	// An origin that is not one is refused at start.
	if err := (&HostedConfig{GitHosts: []string{"https://github.com/org"}}).Check(nil); err == nil || !strings.Contains(err.Error(), "CODEAF_HOSTED_GIT_HOSTS") {
		t.Fatalf("a git host with a path checked as %v", err)
	}
	if err := (&HostedConfig{GitHosts: []string{"https://github.com", "http://localhost:3000"}}).Check(nil); err != nil {
		t.Fatalf("two good origins checked as %v", err)
	}
}

// ── P1-9: PLAIN WORDS ──

// Every sentence the key routes and the start of a chat say on their own is one
// plain sentence with none of the words a person has no use for.
func TestP1_9_PlainWordsOfTheKeyRoutes(t *testing.T) {
	banned := []string{"uid", "sandbox", "sessionhost", "fence", "landlock", "daemon", "bearer", "token", "kek", "dek", "proxy"}
	plain := func(name, words string) {
		t.Helper()
		if !strings.HasSuffix(words, ".") || strings.Contains(words, "\n") {
			t.Fatalf("%s: %q is not one plain sentence", name, words)
		}
		for _, word := range banned {
			if strings.Contains(strings.ToLower(words), word) {
				t.Fatalf("%s says %q, which has %q in it", name, words, word)
			}
		}
	}
	for name, words := range map[string]string{
		"cannot keep": hostedKeysCannotKeep, "no such key": hostedKeysNoSuchKey, "too large": hostedKeysTooLarge,
		"bad request": hostedKeysBadRequest, "not stored": hostedKeysNotStored, "not started": hostedNotStarted, "sign in": hostedSignIn,
	} {
		plain(name, words)
	}
	// And through the real handler, with a server key set and without.
	rg := newHostedRig(t)
	drive := func(label string) {
		t.Helper()
		for _, step := range []struct {
			name, who, method, path, body string
		}{
			{"unknown provider", "santosh", "PUT", "/v1/hosted/keys/nope", `{"key":"sk-test-santosh-0001-aaaa"}`},
			{"save", "santosh", "PUT", "/v1/hosted/keys/openrouter", `{"key":"sk-test-santosh-0001-aaaa"}`},
			{"short key", "santosh", "PUT", "/v1/hosted/keys/openrouter", `{"key":"short"}`},
			{"bad body", "santosh", "PUT", "/v1/hosted/keys/openrouter", `nope`},
		} {
			r := httptest.NewRequest(step.method, step.path, strings.NewReader(step.body))
			r.RemoteAddr = "192.0.2.1:1234"
			if step.who != "" {
				r.Header.Set("Authorization", "Bearer "+rg.tokens[step.who])
			}
			w := httptest.NewRecorder()
			rg.h.ServeHTTP(w, r)
			if w.Code == 204 {
				continue
			}
			plain(label+": "+step.name, errorOf(t, w.Body.Bytes()))
		}
	}
	drive("no server key")
	rg.withServerKey(t, rigServerKey)
	drive("server key")
	// A chat that cannot be given model access.
	_ = os.Remove(rg.file(hostedBearersFileName))
	if err := os.Mkdir(rg.file(hostedBearersFileName), 0o700); err != nil {
		t.Fatal(err)
	}
	w := rg.send("santosh", "POST", "/v1/hosted/sessions", nil, nil)
	if w.Code != 500 {
		t.Fatalf("create with the save failing = %d %s", w.Code, w.Body)
	}
	plain("not started", errorOf(t, w.Body.Bytes()))
}

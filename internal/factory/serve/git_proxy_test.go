package serve

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const personGitKey = "sk-test-github-0001"

type gitRig struct {
	*hostedRig
	host, chat, access string
}

func newGitRig(t *testing.T, upstream http.HandlerFunc) *gitRig {
	t.Helper()
	rg := newHostedRig(t)
	if upstream == nil {
		upstream = func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "0000")
		}
	}
	rg.up.mu.Lock()
	rg.up.handler = upstream
	rg.up.mu.Unlock()
	rg.h.config.Hosted.state.gitHosts = []string{rg.upSrv.URL}
	previous := hostedKeyForHook
	hostedKeyForHook = func(person, provider string) (string, bool, error) {
		if person == "abir" && provider == "github" {
			return personGitKey, true, nil
		}
		return "", false, nil
	}
	t.Cleanup(func() { hostedKeyForHook = previous })
	chat := rg.create(t, "abir")
	return &gitRig{rg, strings.TrimPrefix(rg.upSrv.URL, "http://"), chat, rg.host.tokenOf(chat)}
}

func gitBasic(password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("codeaf:"+password))
}

func (g *gitRig) git(method, suffix, password string) *httptest.ResponseRecorder {
	return g.send("", method, "/v1/hosted/git/"+g.host+"/"+suffix, nil,
		map[string]string{"Authorization": gitBasic(password)})
}

func TestP1_6_CloneAndPushCarryThePersonsTokenAndTheChatNever(t *testing.T) {
	g := newGitRig(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-"+strings.TrimPrefix(r.URL.Path, "/org/repo.git/")+"-result")
		w.Header().Set("Set-Cookie", "credential="+personGitKey)
		_, _ = io.WriteString(w, "0000")
	})
	const base = "https://org.test/v1/hosted/git/"
	for _, service := range []string{"git-upload-pack", "git-receive-pack"} {
		for _, step := range []struct{ method, path, uri, body string }{
			{"GET", "/info/refs?service=" + service, "/org/repo.git/info/refs?service=" + service, ""},
			{"POST", "/" + service, "/org/repo.git/" + service, "0000"},
		} {
			req := httptest.NewRequest(step.method, base+g.host+"/org/repo"+step.path, strings.NewReader(step.body))
			req.SetBasicAuth("any-user", g.access)
			req.Header.Set("Cookie", "my=sign-in")
			req.Header.Set("Proxy-Authorization", "private")
			req.Header.Set("Forwarded", "for=192.0.2.1")
			req.Header.Set("X-Forwarded-Host", "wrong.test")
			req.Header.Set("X-Real-Ip", "192.0.2.1")
			req.Header.Set("X-Codeaf-Person", "mia")
			req.Header.Set("Git-Protocol", "version=2")
			req.Header.Set("Content-Type", "application/x-"+service+"-request")

			w := httptest.NewRecorder()
			g.h.ServeHTTP(w, req)
			if w.Code != 200 || w.Body.String() != "0000" || w.Header().Get("Set-Cookie") != "" {
				t.Fatalf("git response: %d %v %q", w.Code, w.Header(), w.Body)
			}
			for _, values := range w.Header() {
				for _, value := range values {
					if strings.Contains(value, personGitKey) {
						t.Fatal("upstream credential reached the chat")
					}
				}
			}
			seen := g.up.requests()
			got := seen[len(seen)-1]
			if got.method != step.method || got.uri != step.uri || got.body != step.body ||
				got.header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+personGitKey)) ||
				got.header.Get("Git-Protocol") != "version=2" || got.header.Get("Content-Type") != req.Header.Get("Content-Type") {
				t.Fatalf("git upstream request differs: %#v", got)
			}
			for _, header := range []string{"Cookie", "Proxy-Authorization", "Forwarded", "X-Forwarded-Host", "X-Real-Ip", "X-Codeaf-Person"} {
				if got.header.Get(header) != "" {
					t.Errorf("identity header %s reached upstream", header)
				}
			}
		}
	}
	if got := len(g.up.requests()); got != 4 {
		t.Fatalf("upstream calls = %d, want 4", got)
	}
}

func TestP1_6_RefusalsAreSentencesAndMakeNoUpstreamCall(t *testing.T) {
	g := newGitRig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/org/redirect.git/info/refs" {
			http.Redirect(w, r, "/out-of-scope", http.StatusFound)
			return
		}
		w.Header().Set("Content-Encoding", "legacy")
		w.Header().Set("Set-Cookie", "credential="+personGitKey)
		http.Error(w, personGitKey, 403)
	})
	for _, tc := range []struct{ method, path string }{
		{"GET", "org/repo/info/refs"},
		{"GET", "org/repo/info/refs?service=git-other"},
		{"GET", "org/repo/info/refs?service=git-upload-pack&service=git-receive-pack"},
		{"GET", "org/repo/info/refs?service=git-upload-pack&"},
		{"GET", "org/repo/info/refs/?service=git-upload-pack"},
		{"GET", "org/repo/git-receive-pack"},
		{"POST", "org/repo/git-upload-pack?service=git-upload-pack"},
		{"POST", "org/repo/info/refs?service=git-receive-pack"},
		{"GET", "org/repo/../repo/info/refs?service=git-upload-pack"},
		{"GET", "org//repo/info/refs?service=git-upload-pack"},
		{"GET", "org%2frepo/info/refs?service=git-upload-pack"},
		{"GET", "org/repo/info%2Frefs?service=git-upload-pack"},
		{"GET", "org/repo%5C/info/refs?service=git-upload-pack"},
		{"GET", "org/repo%00/info/refs?service=git-upload-pack"},
		{"GET", "org/repo..git/info/refs?service=git-upload-pack"},
		{"GET", "org/.git/info/refs?service=git-upload-pack"},
		{"GET", "-org/repo/info/refs?service=git-upload-pack"},
	} {
		wantGitSentence(t, g.git(tc.method, tc.path, g.access), 404, "There is nothing at that address.")
	}
	auth := map[string]string{"Authorization": gitBasic(g.access)}
	wantGitSentence(t, g.send("", "GET", "/v1/hosted/git/other.test/org/repo/info/refs?service=git-upload-pack", nil, auth),
		403, "This server does not reach that git host for chats.")
	hostedKeyForHook = func(string, string) (string, bool, error) {
		return "", true, errors.New("fake stored credential could not be opened")
	}
	wantGitSentence(t, g.git("GET", "org/repo/info/refs?service=git-upload-pack", g.access),
		403, "Your GitHub access could not be opened on this server.")
	if got := len(g.up.requests()); got != 0 {
		t.Fatalf("refused requests made %d upstream calls", got)
	}
	if entries := gitAuditEntries(t, g, "abir"); len(entries) != 0 {
		t.Fatalf("refused requests wrote audit entries: %#v", entries)
	}

	hostedKeyForHook = func(string, string) (string, bool, error) { return personGitKey, true, nil }
	w := g.git("GET", "org/redirect/info/refs?service=git-upload-pack", g.access)
	wantGitSentence(t, w, 502, "That git host is not answering.")
	if w.Header().Get("Location") != "" || len(g.up.requests()) != 1 {
		t.Fatal("upstream redirect escaped")
	}
	w = g.git("GET", "org/denied/info/refs?service=git-upload-pack", g.access)
	wantGitSentence(t, w, 403, "That git host did not accept your GitHub access.")
	if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("replacement response kept upstream headers: %v", w.Header())
	}
}

func TestP1_6_AnotherPersonsTokenIsNeverUsed(t *testing.T) {
	g := newGitRig(t, nil)
	chat := g.create(t, "mia")
	wantGitSentence(t, g.git("GET", "org/repo/info/refs?service=git-upload-pack", g.hostedRig.host.tokenOf(chat)),
		403, "You have no GitHub access stored on this server yet. Add it, then try again.")
	if len(g.up.requests()) != 0 {
		t.Fatal("another person's access was tried upstream")
	}
}

func TestP1_6_RevokedBearerEndsGitAtOnce(t *testing.T) {
	g := newGitRig(t, nil)
	target := "org/repo/info/refs?service=git-upload-pack"
	if w := g.git("GET", target, g.access); w.Code != 200 {
		t.Fatalf("before stop: %d %s", w.Code, w.Body)
	}
	if w := g.send("abir", "POST", "/v1/hosted/sessions/"+g.chat+"/stop", nil, nil); w.Code != 204 {
		t.Fatalf("stop: %d %s", w.Code, w.Body)
	}
	w := g.git("GET", target, g.access)
	wantGitSentence(t, w, 401, "Sign in to use git here.")
	if len(g.up.requests()) != 1 {
		t.Fatal("the stopped chat reached upstream")
	}
}

func TestP1_6_NoBearerGetsAChallenge(t *testing.T) {
	g := newGitRig(t, nil)
	target := "/v1/hosted/git/" + g.host + "/org/repo/info/refs?service=git-upload-pack"
	for _, header := range []string{"", "Bearer " + g.access, gitBasic("unknown")} {
		w := g.send("abir", "GET", target, nil, map[string]string{"Authorization": header})
		wantGitSentence(t, w, 401, "Sign in to use git here.")
		if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="codeaf"` {
			t.Errorf("challenge = %q", got)
		}
	}
	if len(g.up.requests()) != 0 {
		t.Fatal("an unauthenticated request reached upstream")
	}
}

func TestP1_6_HostedOffAnswersAsAnyUnknownPath(t *testing.T) {
	g := newGitRig(t, nil)
	g.cfg.Hosted = nil
	restartAccessOrg(t, g.wsOrg)
	for _, who := range []string{"", "abir"} {
		unknown := g.send(who, "GET", "/v1/not-a-known-path", nil, nil)
		for _, target := range []string{"/v1/hosted/git/git.test/org/repo/info/refs?service=git-upload-pack", "/v1/hosted/audit"} {
			got := g.send(who, "GET", target, nil, nil)
			if got.Code != unknown.Code || got.Header().Get("WWW-Authenticate") != unknown.Header().Get("WWW-Authenticate") ||
				errorOf(t, got.Body.Bytes()) != errorOf(t, unknown.Body.Bytes()) {
				t.Fatalf("unknown path gives %d %s while %s gives %d %s", unknown.Code, unknown.Body, target, got.Code, got.Body)
			}
		}
	}
}

func TestP1_9_GitSentencesArePlain(t *testing.T) {
	for _, words := range []string{gitSignIn, gitNoSuchPlace, gitHostDenied, gitNoAccess, gitAccessUnreadable, gitNotAnswering, gitAccessRejected, gitTooLarge} {
		if !strings.HasSuffix(words, ".") {
			t.Errorf("missing sentence punctuation: %q", words)
		}
		for _, term := range strings.Fields("uid sandbox sessionhost fence landlock daemon bearer token kek dek proxy") {
			if strings.Contains(strings.ToLower(words), term) {
				t.Errorf("%q contains %q", words, term)
			}
		}
	}
}

func wantGitSentence(t *testing.T, w *httptest.ResponseRecorder, status int, sentence string) {
	t.Helper()
	if w.Code != status || w.Body.String() != sentence+"\n" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("git response = %d %q %q, want %d %q", w.Code, w.Header().Get("Content-Type"), w.Body, status, sentence)
	}
}

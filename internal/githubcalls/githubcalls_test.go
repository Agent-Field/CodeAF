package githubcalls

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeNext func(*http.Request) (*http.Response, error)

func (f fakeNext) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answering(status int, headers map[string]string) fakeNext {
	return func(*http.Request) (*http.Response, error) {
		h := make(http.Header)
		for k, v := range headers {
			h.Set(k, v)
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader("body"))}, nil
	}
}

func logTo(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "github.jsonl")
	t.Setenv(EnvVar, path)
	return path
}

func lines(t *testing.T, path string) []Record {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

func call(t *testing.T, next http.RoundTripper, ctx context.Context, method, url string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer ghp_secret_token_value")
	resp, err := Transport(next).RoundTrip(req)
	if err == nil {
		resp.Body.Close()
	}
}

func TestOffHandsBackTheSameTransport(t *testing.T) {
	t.Setenv(EnvVar, "")
	next := answering(200, nil)
	got := Transport(next)
	if _, same := got.(fakeNext); !same {
		t.Fatalf("logging off must not wrap, got %T", got)
	}
	if Transport(nil) != nil {
		t.Fatal("logging off must keep a nil transport nil")
	}
}

func TestACallIsLoggedByEndpointWithItsRateLimit(t *testing.T) {
	path := logTo(t)
	call(t, answering(200, map[string]string{
		"X-RateLimit-Resource":  "core",
		"X-RateLimit-Remaining": "4321",
		"X-RateLimit-Used":      "679",
	}), WithCaller(context.Background(), "GET /v1/queue"), "GET", "https://api.github.com/repos/secret-org/secret-repo/issues/42/comments?per_page=100&access_token=zzz")
	got := lines(t, path)
	if len(got) != 1 {
		t.Fatalf("records %d", len(got))
	}
	r := got[0]
	if r.Method != "GET" || r.Endpoint != "/repos/:owner/:repo/issues/:n/comments" || r.Status != 200 {
		t.Fatalf("%+v", r)
	}
	if r.Resource != "core" || r.Remaining != 4321 || r.Used != 679 || r.Caller != "GET /v1/queue" {
		t.Fatalf("%+v", r)
	}
}

func TestTheLogHoldsNoPrivateDataOrToken(t *testing.T) {
	path := logTo(t)
	next := answering(200, nil)
	for _, url := range []string{
		"https://api.github.com/repos/secret-org/secret-repo/contents/secret-dir/secret-file.go?ref=secret-branch",
		"https://api.github.com/repos/secret-org/secret-repo/git/refs/heads/secret-branch",
		"https://api.github.com/repos/secret-org/secret-repo/commits/abcdef0123456789abcdef0123456789abcdef01",
		"https://api.github.com/users/secret-person/repos",
		"https://api.github.com/search/issues?q=secret",
	} {
		call(t, next, context.Background(), "GET", url)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"secret", "ghp_", "abcdef", "zzz"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("log holds %q:\n%s", leak, raw)
		}
	}
	got := lines(t, path)
	want := []string{
		"/repos/:owner/:repo/contents/:x",
		"/repos/:owner/:repo/git/refs/:x",
		"/repos/:owner/:repo/commits/:x",
		"/users/:x/repos",
		"/search/issues",
	}
	for i, w := range want {
		if got[i].Endpoint != w {
			t.Errorf("%d: %s, want %s", i, got[i].Endpoint, w)
		}
	}
}

func TestA304IsLoggedAsTheFreeAnswerItIs(t *testing.T) {
	path := logTo(t)
	call(t, answering(304, nil), context.Background(), "GET", "https://api.github.com/repos/o/r/issues")
	if r := lines(t, path)[0]; r.Status != 304 || r.Caller != "" {
		t.Fatalf("%+v", r)
	}
}

func TestAFailedCallIsLoggedAndItsErrorPassesThrough(t *testing.T) {
	path := logTo(t)
	boom := errors.New("connection refused to secret-host")
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	_, err := Transport(fakeNext(func(*http.Request) (*http.Response, error) { return nil, boom })).RoundTrip(req)
	if !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if r := lines(t, path)[0]; r.Status != 0 || r.Endpoint != "/user" {
		t.Fatalf("%+v", r)
	}
}

func TestAnUnwritableLogNeverBreaksTheCall(t *testing.T) {
	t.Setenv(EnvVar, filepath.Join(t.TempDir(), "no", "such", "dir", "log"))
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	resp, err := Transport(answering(200, nil)).RoundTrip(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
}

func TestAGhSubprocessIsLoggedByItsVerbOnly(t *testing.T) {
	path := logTo(t)
	NoteGH(WithCaller(context.Background(), "GET /v1/setup"), "auth status")
	r := lines(t, path)[0]
	if r.Method != "GH" || r.Endpoint != "gh auth status" || r.Caller != "GET /v1/setup" {
		t.Fatalf("%+v", r)
	}
}

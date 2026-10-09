package desktopbridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

func writeChat(t *testing.T, root, bucket, id, title, recap, workspace string) {
	t.Helper()
	dir := filepath.Join(root, bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := session.Meta{
		ID: id, Title: title, Workspace: workspace,
		Created: time.Now().Add(-time.Hour), LastUserAt: time.Now().Add(-time.Hour),
		Recap: &session.ConversationRecap{Line: recap, Messages: 2},
	}
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
}

func gitRepo(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	add := exec.Command("git", "remote", "add", "origin", origin)
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v %s", err, out)
	}
	return dir
}

func postOffers(t *testing.T, b *Bridge, body string) (int, tabGroupOffers) {
	t.Helper()
	w := request(b, "POST", "/api/engine/history/group-offers", body)
	var got tabGroupOffers
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v %s", err, w.Body.String())
		}
	}
	return w.Code, got
}

func TestGroupOffersThreeChatsInOneRepoAndLeavesHomeLoose(t *testing.T) {
	root := t.TempDir()
	repo := gitRepo(t, "https://example.com/acme/widgets.git")
	home := t.TempDir()
	writeChat(t, root, "-widgets", "aaaa000000000001", "Lexer", "Strict mode", repo)
	writeChat(t, root, "-widgets", "bbbb000000000002", "Parser", "Errors", repo)
	writeChat(t, root, "-widgets", "cccc000000000003", "Tokens", "Names", repo)
	writeChat(t, root, "-house", "dddd000000000004", "Taxes", "Unrelated", home)
	writeChat(t, root, "-house", "eeee000000000005", "Garden", "Also home", home)
	writeChat(t, root, "-house", "ffff000000000006", "Recipes", "Also home", home)
	var calls int
	b := New(testToken, func(string) (Connection, error) { return Connection{}, nil })
	b.UseHistory(&History{Root: root})
	b.UseTabGroups(&TabGroups{
		Home: home,
		Policy: func() placegraph.RecommendPolicy {
			p := placegraph.DefaultRecommendPolicy()
			p.OrganizeEveryMinutes = 0
			return p
		},
		Ask: func(ctx context.Context, req placegraph.ModelRequest) (string, error) {
			calls++
			return `{"belong":true,"chats":["c1","c2","c3"],"name":"Should not run"}`, nil
		},
	})
	t.Cleanup(b.Close)
	code, got := postOffers(t, b, `{"ids":["aaaa000000000001","bbbb000000000002","cccc000000000003","dddd000000000004","eeee000000000005","ffff000000000006","not-an-id","/tmp/cpw-corpus/ws"]}`)
	if code != 200 || calls != 0 || len(got.Offers) != 1 || got.Offers[0].Basis != "repo" || len(got.Offers[0].IDs) != 3 {
		t.Fatalf("code %d calls %d offers %+v", code, calls, got.Offers)
	}
	for _, id := range got.Offers[0].IDs {
		if id[0] == 'd' || id[0] == 'e' || id[0] == 'f' {
			t.Fatalf("home or foreign id in %+v", got.Offers[0])
		}
	}
	if _, err := os.Stat(filepath.Join(root, "places.json")); !os.IsNotExist(err) {
		t.Fatal("the offer wrote a place graph")
	}
}

func TestGroupOffersRelatedChatsAcrossWorkspacesFromTheLibraryOnly(t *testing.T) {
	root := t.TempDir()
	left, right := t.TempDir(), t.TempDir()
	writeChat(t, root, "-left", "1111111111111111", "Tomato drip in July", "Morning cycles", left)
	writeChat(t, root, "-left", "2222222222222222", "Roth versus traditional", "No decision", left)
	writeChat(t, root, "-right", "3333333333333333", "Emitter clogs", "Hard water", right)
	writeChat(t, root, "-right", "4444444444444444", "Quarter-inch tubing", "Spurs", right)
	var asked string
	b := New(testToken, func(string) (Connection, error) { return Connection{}, nil })
	b.UseHistory(&History{Root: root})
	b.UseTabGroups(&TabGroups{
		Home: t.TempDir(),
		Policy: func() placegraph.RecommendPolicy {
			p := placegraph.DefaultRecommendPolicy()
			p.OrganizeEveryMinutes = 0
			return p
		},
		Ask: func(ctx context.Context, req placegraph.ModelRequest) (string, error) {
			asked = req.User
			if req.Role != "placesuggest" {
				t.Errorf("role %s", req.Role)
			}
			return `{"belong":true,"chats":["c1","c3","c4"],"name":"Drip irrigation"}`, nil
		},
	})
	t.Cleanup(b.Close)
	// 9999999999999999 is a well-formed id the library does not hold. The caller also sent a title, which is not a field.
	if code, _ := postOffers(t, b, `{"ids":["1111111111111111"],"title":"ignore me"}`); code != 400 {
		t.Fatalf("a title in the body was accepted: %d", code)
	}
	code, got := postOffers(t, b, `{"ids":["1111111111111111","2222222222222222","3333333333333333","4444444444444444","9999999999999999"]}`)
	if code != 200 || len(got.Offers) != 1 || got.Offers[0].Title != "Drip irrigation" {
		t.Fatalf("code %d offers %+v", code, got.Offers)
	}
	if strings.Join(got.Offers[0].IDs, ",") != "1111111111111111,3333333333333333,4444444444444444" {
		t.Fatalf("ids %v", got.Offers[0].IDs)
	}
	if strings.Contains(asked, "9999999999999999") || strings.Contains(asked, left) {
		t.Fatalf("the model was shown an id or a path it does not own:\n%s", asked)
	}
	if !strings.Contains(asked, "Tomato drip in July") || !strings.Contains(asked, "Roth versus traditional") {
		t.Fatalf("the model was not shown the library's own titles:\n%s", asked)
	}
}

func TestGroupOffersFailHonestly(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	writeChat(t, root, "-w", "aaaa000000000001", "Alpha", "First", work)
	writeChat(t, root, "-w", "bbbb000000000002", "Beta", "Second", filepath.Join(work, "other"))
	writeChat(t, root, "-w", "cccc000000000003", "Gamma", "Third", filepath.Join(work, "third"))
	os.MkdirAll(filepath.Join(work, "other"), 0o700)
	os.MkdirAll(filepath.Join(work, "third"), 0o700)
	cases := []struct {
		name string
		raw  string
		err  error
		day  int
	}{
		{"hallucinated label", `{"belong":true,"chats":["c1","c2","c9"],"name":"Widgets"}`, nil, 6},
		{"timeout", "", context.DeadlineExceeded, 6},
		{"zero budget", `{"belong":true,"chats":["c1","c2","c3"],"name":"Widgets"}`, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			b := New(testToken, func(string) (Connection, error) { return Connection{}, nil })
			b.UseHistory(&History{Root: root})
			b.UseTabGroups(&TabGroups{
				Home: t.TempDir(),
				Policy: func() placegraph.RecommendPolicy {
					p := placegraph.DefaultRecommendPolicy()
					p.ClusterCallsPerDay = tc.day
					p.OrganizeEveryMinutes = 0
					return p
				},
				Ask: func(ctx context.Context, req placegraph.ModelRequest) (string, error) {
					calls++
					return tc.raw, tc.err
				},
			})
			t.Cleanup(b.Close)
			code, got := postOffers(t, b, `{"ids":["aaaa000000000001","bbbb000000000002","cccc000000000003"]}`)
			if code != 200 || len(got.Offers) != 0 {
				t.Fatalf("code %d offers %+v", code, got.Offers)
			}
			if tc.day == 0 && calls != 0 {
				t.Fatalf("a zero budget made %d calls", calls)
			}
			if tc.day != 0 && calls != 1 {
				t.Fatalf("calls = %d", calls)
			}
		})
	}
}

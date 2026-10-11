package github_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestPeopleProfileTTLAndETagAndOutsideAuthor(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/outside" {
			t.Errorf("path = %s", r.URL.Path)
		}
		reads++
		if reads > 1 {
			if r.Header.Get("If-None-Match") != `"user-v1"` {
				t.Error("missing ETag")
			}
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"user-v1"`)
		fmt.Fprint(w, `{"name":"Outside Author","avatar_url":"https://avatar/outside","html_url":"https://github.com/outside"}`)
	}))
	defer srv.Close()
	loader := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI { return forge.NewClient("").WithBaseURL(srv.URL) }, func() time.Time { return now })
	first, err := loader.Profile(context.Background(), "@Outside")
	if err != nil || first.Name != "Outside Author" || first.HTMLURL == "" {
		t.Fatalf("profile = %+v %v", first, err)
	}
	now = now.Add(factorygithub.ProfileTTL - time.Second)
	if got, err := loader.Profile(context.Background(), "outside"); err != nil || got != first || reads != 1 {
		t.Fatalf("cached profile = %+v %v reads %d", got, err, reads)
	}
	now = now.Add(time.Second)
	if got, err := loader.Profile(context.Background(), "outside"); err != nil || got != first || reads != 2 {
		t.Fatalf("304 profile = %+v %v reads %d", got, err, reads)
	}
}

func TestCodeOwnersDefaultBranchPrecedenceAndNextPoll(t *testing.T) {
	path, text := "CODEOWNERS", "/api/ @outside\n"
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.URL.Query().Get("ref") != "" {
			t.Error("ownership read from job branch")
		}
		if r.URL.Path != "/repos/acme/api/contents/"+path {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"type": "file", "encoding": "base64", "sha": "owners", "content": base64.StdEncoding.EncodeToString([]byte(text))})
	}))
	defer srv.Close()
	loader := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI { return forge.NewClient("stub").WithBaseURL(srv.URL) }, nil)
	ctx := context.Background()
	rules, err := loader.Owners(ctx, "acme/api")
	if err != nil || len(rules) != 1 || rules[0].Handles[0] != "outside" || reads != 2 {
		t.Fatalf("rules = %+v %v reads=%d", rules, err, reads)
	}
	rules[0].Handles[0] = "mutated"
	if rules, _ := loader.Owners(ctx, "acme/api"); rules[0].Handles[0] != "outside" || reads != 2 {
		t.Fatal("cache mutated or re-read")
	}
	path, text = ".github/CODEOWNERS", "* @mei\n"
	loader.RefreshOwners(ctx, "acme/api")
	if rules, _ := loader.Owners(ctx, "acme/api"); len(rules) != 1 || rules[0].Handles[0] != "mei" || reads != 3 {
		t.Fatalf("next poll = %+v reads %d", rules, reads)
	}
	path = "missing"
	loader.RefreshOwners(ctx, "acme/api")
	if rules, err := loader.Owners(ctx, "acme/api"); err != nil || len(rules) != 0 {
		t.Fatalf("deleted file = %+v %v", rules, err)
	}
}

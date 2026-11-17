package serve

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// The join preview, the runner directory and the activity feed each name
// handles; the app draws a GitHub photo only for handles the response vouches
// for, so these tests pin who is vouched for and what never rides along.

func TestJoinPreviewCarriesTheOperatorsRecordedGitHubFace(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "owner"}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	var created struct {
		Link api.AccessLink `json:"link"`
	}
	f.request(t, "POST", "/v1/access/links", "operator-secret", api.CreateAccessLink{Login: "priya", ExpiresAt: f.now.Add(time.Hour)}, 201, &created)

	_, before := joinLinkRequest(t, f.h, "GET", created.Link.URL, "", nil)
	if before.Factory.Operator != "owner" || before.Factory.OperatorName != "" || before.Factory.OperatorAvatarURL != "" {
		t.Fatalf("no sign-in recorded yet, so only the handle: %+v", before.Factory)
	}

	signInHandle(t, f.h, g, &f.now, "owner", "operator-secret")
	w, after := joinLinkRequest(t, f.h, "GET", created.Link.URL, "", nil)
	if after.Factory.OperatorName != "Priya Nair" || after.Factory.OperatorAvatarURL != "https://avatars.githubusercontent.com/u/42" {
		t.Fatalf("recorded profile missing: %+v", after.Factory)
	}
	for _, secret := range []string{"github-access-secret", "operator-secret", "scopes"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("preview leaked %q: %s", secret, w.Body)
		}
	}

	// A link that matches nothing earns no face, only the handle it always had.
	bogus := "codeaf://factory/join?" + url.Values{"url": {"https://factory.test"}}.Encode() + "#token=nope"
	_, none := joinLinkRequest(t, f.h, "GET", bogus, "", nil)
	if none.Factory.OperatorName != "" || none.Factory.OperatorAvatarURL != "" {
		t.Fatalf("unmatched link got a profile: %+v", none.Factory)
	}
}

func TestRunnersAndActivityNameOnlyTheirOwnSignedInPeople(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "priya"}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	const gate = "operator-secret"
	priya := signInHandle(t, f.h, g, &f.now, "priya", gate)
	signInHandle(t, f.h, g, &f.now, "raj", gate) // signed in, but owns no runner and acts in no event
	f.request(t, "POST", "/v1/runners/attach", priya, api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "priya", Kind: api.RunnerMac}, 201, nil)
	if _, err := f.cfg.Record.Append(t.Context(), record.Event{Kind: record.LinkCreated, Handle: "priya", Object: "x", Actor: record.Actor{Kind: "person"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.cfg.Record.Append(t.Context(), record.Event{Kind: record.LinkCreated, Handle: "stranger", Object: "y", Actor: record.Actor{Kind: "person"}}); err != nil {
		t.Fatal(err)
	}

	var runners api.Runners
	raw := f.request(t, "GET", "/v1/runners", priya, nil, 200, &runners)
	if len(runners.Runners) != 1 || len(runners.People) != 1 || runners.People["priya"].Name != "Priya Nair" || runners.People["priya"].AvatarURL == "" {
		t.Fatalf("runners people = %+v", runners.People)
	}
	var feed struct {
		Events []record.Event               `json:"events"`
		People map[string]map[string]string `json:"people"`
	}
	rawFeed := f.request(t, "GET", "/v1/activity", gate, nil, 200, &feed)
	if feed.People["priya"]["avatar_url"] == "" {
		t.Fatalf("activity people = %+v", feed.People)
	}
	if _, ok := feed.People["stranger"]; ok {
		t.Fatalf("a handle nobody signed in as was given a profile: %+v", feed.People)
	}
	if _, ok := feed.People["raj"]; ok {
		t.Fatalf("a member with no event in the feed rode along: %+v", feed.People)
	}
	for _, body := range []string{raw, rawFeed} {
		for _, secret := range []string{"github-access-secret", "scopes", "html_url"} {
			if strings.Contains(body, secret) {
				t.Fatalf("response leaked %q: %s", secret, body)
			}
		}
	}
}

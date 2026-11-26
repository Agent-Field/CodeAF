package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// VALIDATION CONTRACT, written before the code it checks (found on a production run of the hosted organization:
// a person who joined by an anonymous access link and was then removed stayed in People as "away" and kept the
// grants others had given them):
//   - Removing a person drops them from People and from the member list, and drops every grant they hold on
//     anyone's chat, whichever door they joined by: a named invite, an anonymous link, or a GitHub sign-in.
//   - Nobody else is touched: another person's grants on the same chat, and a joiner of the same multi-use link,
//     stay exactly as they were.
//   - A removed person's spent use stays spent: the link they came in by does not reopen for anyone else. Joining
//     again, by a new link or by the old one, brings no old grants back.
//   - A person who joined by a named invite is removed exactly as before: gone from People, the invite closed.

// removalDoor is one way a person joins the organization.
type removalDoor struct {
	name string
	// link makes a fresh link as the operator and answers it.
	link func(t *testing.T, rg *hostedRig) string
	// after is the state the answer to a second sign-up through the same link names once its person is gone.
	after string
}

var removalDoors = []removalDoor{
	{"named invite", func(t *testing.T, rg *hostedRig) string {
		t.Helper()
		var made api.InviteMade
		rg.do(t, "owner", http.MethodPost, "/v1/invites", -1, api.InviteNew{Email: "bob@acme.example"}, http.StatusCreated, &made)
		return made.URL
	}, "revoked"},
	{"anonymous link", func(t *testing.T, rg *hostedRig) string { return rg.anonymousLink(t, 1) }, "used_up"},
}

// anonymousLink is the README's own recipe, POST /v1/access/links, for up to uses people.
func (rg *hostedRig) anonymousLink(t *testing.T, uses int) string {
	t.Helper()
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	rg.do(t, "owner", http.MethodPost, "/v1/access/links", -1, api.CreateAccessLink{MaxUses: uses}, http.StatusCreated, &made)
	if made.Link.URL == "" {
		t.Fatal("the link carries no address")
	}
	return made.Link.URL
}

// join signs username up through link and keeps their token.
func (rg *hostedRig) join(t *testing.T, link, username string) {
	t.Helper()
	out, code := rg.signUpAs(t, link, username)
	if code != http.StatusOK || out.State != "joined" || out.AccessToken == "" {
		t.Fatalf("%s did not join: %d %+v", username, code, out)
	}
	rg.tokens[username] = out.AccessToken
}

// handlesOf reads who the organization lists, through the two routes that list people.
func (rg *hostedRig) handlesOf(t *testing.T) (people, members []string) {
	t.Helper()
	var list api.People
	rg.do(t, "owner", http.MethodGet, "/v1/people", -1, nil, http.StatusOK, &list)
	for _, p := range list.People {
		people = append(people, p.Handle)
	}
	var all api.Members
	rg.do(t, "owner", http.MethodGet, "/v1/members", -1, nil, http.StatusOK, &all)
	for _, m := range all.Members {
		members = append(members, m.Handle)
	}
	return people, members
}

// holders lists who holds a grant on one chat, by handle and role, in the store.
func (rg *hostedRig) holders(t *testing.T, chat string) []string {
	t.Helper()
	grants, err := rg.st.GrantsOn(store.SessionKind, chat)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, g := range grants {
		out = append(out, g.Subject.ID+":"+g.Role)
	}
	slices.Sort(out)
	return out
}

func (rg *hostedRig) remove(t *testing.T, handle string) {
	t.Helper()
	if w := rg.send("owner", http.MethodDelete, "/v1/people/"+handle+"/sessions", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("removing %s = %d %s", handle, w.Code, w.Body)
	}
}

// Contract items 1, 2, 3 and 4: through the real handlers and a real store, on both password doors.
func TestRemovingAJoinerEndsTheirPlaceAndEveryGrantWhicheverDoorTheyUsed(t *testing.T) {
	for _, door := range removalDoors {
		t.Run(door.name, func(t *testing.T) {
			rg := newHostedRig(t)
			link := door.link(t, rg)
			rg.join(t, link, "bob")
			chat := rg.create(t, "santosh")
			other := rg.create(t, "mia")
			hostedGrant(t, rg, "santosh", chat, "bob", "edit")
			hostedGrant(t, rg, "santosh", chat, "bob", "view")
			hostedGrant(t, rg, "santosh", chat, "abir", "view")
			hostedGrant(t, rg, "mia", other, "bob", "view")

			// Controls: before the removal bob is listed, holds the grants and reads the chat, so what is gone
			// afterwards is the removal's doing.
			if people, members := rg.handlesOf(t); !slices.Contains(people, "bob") || !slices.Contains(members, "bob") {
				t.Fatalf("before the removal, bob is not listed: people %v, members %v", people, members)
			}
			if w := rg.send("bob", http.MethodGet, hostedSessionsPath+"/"+chat, nil, nil); w.Code != http.StatusOK {
				t.Fatalf("before the removal, bob's read of the chat = %d %s", w.Code, w.Body)
			}
			if got, want := rg.holders(t, chat), []string{"abir:view", "bob:edit", "bob:view"}; !slices.Equal(got, want) {
				t.Fatalf("before the removal, the chat's grants = %v, want %v", got, want)
			}

			rg.remove(t, "bob")

			people, members := rg.handlesOf(t)
			if slices.Contains(people, "bob") || slices.Contains(members, "bob") {
				t.Errorf("after the removal, bob is still listed: people %v, members %v", people, members)
			}
			if held, err := rg.st.GrantsOfSubject("person", "bob"); err != nil || len(held) != 0 {
				t.Errorf("after the removal, bob still holds grants: %+v, %v", held, err)
			}
			if got, want := rg.holders(t, chat), []string{"abir:view"}; !slices.Equal(got, want) {
				t.Errorf("the chat's grants after the removal = %v, want %v", got, want)
			}
			if got := rg.holders(t, other); len(got) != 0 {
				t.Errorf("mia's chat's grants after the removal = %v, want none", got)
			}
			if w := rg.send("abir", http.MethodGet, hostedSessionsPath+"/"+chat, nil, nil); w.Code != http.StatusOK {
				t.Errorf("another guest's read of the chat after bob's removal = %d, want 200", w.Code)
			}
			// What the removal already did stays done.
			if w := rg.send("bob", http.MethodGet, "/v1/me", nil, nil); w.Code != http.StatusUnauthorized {
				t.Errorf("bob's token after the removal = %d, want 401", w.Code)
			}
			if w := rg.signIn(t, "bob"); w.Code != http.StatusUnauthorized {
				t.Errorf("bob's password sign-in after the removal = %d, want 401", w.Code)
			}
			// The link does not reopen, and its answer to a second person is what it was.
			if out, _ := rg.signUpAs(t, link, "carol"); out.State != door.after {
				t.Errorf("a second sign-up through bob's link says %q, want %q", out.State, door.after)
			}
		})
	}
}

// signUpAs joins by password through link, from loopback because the password door takes plain HTTP only there;
// the answer may be a refusal.
func (rg *hostedRig) signUpAs(t *testing.T, link, username string) (api.FactoryAccess, int) {
	t.Helper()
	body, _ := json.Marshal(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: username, Password: pwAbir})
	r := httptest.NewRequest(http.MethodPost, "/v1/factory-access", strings.NewReader(string(body)))
	r.RemoteAddr = "127.0.0.1:4000"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, r)
	var out api.FactoryAccess
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out, w.Code
}

func (rg *hostedRig) signIn(t *testing.T, username string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(api.PasswordSignIn{Username: username, Password: pwAbir})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/password", strings.NewReader(string(body)))
	r.RemoteAddr = "127.0.0.1:4000"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, r)
	return w
}

// Contract item 2: a link that admits several people loses only the one removed.
func TestRemovingOneJoinerOfAMultiUseLinkLeavesTheOthersAndTheirGrants(t *testing.T) {
	rg := newHostedRig(t)
	link := rg.anonymousLink(t, 2)
	rg.join(t, link, "bob")
	rg.join(t, link, "carol")
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "bob", "edit")
	hostedGrant(t, rg, "santosh", chat, "carol", "edit")

	rg.remove(t, "bob")

	if people, members := rg.handlesOf(t); slices.Contains(people, "bob") || slices.Contains(members, "bob") ||
		!slices.Contains(people, "carol") || !slices.Contains(members, "carol") {
		t.Errorf("after bob's removal: people %v, members %v; want carol listed and bob not", people, members)
	}
	if got, want := rg.holders(t, chat), []string{"carol:edit"}; !slices.Equal(got, want) {
		t.Errorf("the chat's grants after bob's removal = %v, want %v", got, want)
	}
	if w := rg.send("carol", http.MethodGet, hostedSessionsPath+"/"+chat, nil, nil); w.Code != http.StatusOK {
		t.Errorf("carol's read of the chat after bob's removal = %d, want 200", w.Code)
	}
	// Bob's use stays spent: the link did not reopen for a third person.
	if out, _ := rg.signUpAs(t, link, "dave"); out.State != "used_up" {
		t.Errorf("a third sign-up through the two-person link says %q, want used_up", out.State)
	}
}

// Contract item 3, on the GitHub door: joining again by a new link brings no old grant back.
func TestARemovedGitHubJoinerDoesNotComeBackWithTheirGrants(t *testing.T) {
	rg, g, _, chat := removedGitHubJoiner(t)

	// A new link joins him again, and he holds nothing he held before.
	fresh := rg.anonymousLink(t, 1)
	rg.tokens["bob"] = signInHandle(t, rg.h, g, &rg.now, "bob", gateOf(t, fresh))
	rg.wantBackWithoutGrants(t, chat)
}

// Contract item 3, same door: the link they came in by still admits someone it already counted, as it always has,
// and that brings him back into People without any old grant.
func TestARemovedGitHubJoinerUsingTheOldLinkIsBackInPeopleWithoutGrants(t *testing.T) {
	rg, g, gate, chat := removedGitHubJoiner(t)

	rg.tokens["bob"] = signInHandle(t, rg.h, g, &rg.now, "bob", gate)
	rg.wantBackWithoutGrants(t, chat)
}

// removedGitHubJoiner joins bob through an anonymous link by GitHub, gives him a grant on santosh's chat, removes him,
// and answers the scene.
func removedGitHubJoiner(t *testing.T) (*hostedRig, *fakeGitHub, string, string) {
	t.Helper()
	rg := newHostedRig(t)
	g := &fakeGitHub{t: t}
	rg.h.config.GitHubClientID, rg.h.config.GitHubHTTPClient = "client", g.client()
	gate := gateOf(t, rg.anonymousLink(t, 3))
	rg.tokens["bob"] = signInHandle(t, rg.h, g, &rg.now, "bob", gate)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "bob", "edit")
	if w := rg.send("bob", http.MethodGet, hostedSessionsPath+"/"+chat, nil, nil); w.Code != http.StatusOK {
		t.Fatalf("before the removal, bob's read of the chat = %d %s", w.Code, w.Body)
	}
	rg.remove(t, "bob")
	if people, _ := rg.handlesOf(t); slices.Contains(people, "bob") {
		t.Fatalf("after the removal, bob is still listed: %v", people)
	}
	return rg, g, gate, chat
}

func (rg *hostedRig) wantBackWithoutGrants(t *testing.T, chat string) {
	t.Helper()
	if people, _ := rg.handlesOf(t); !slices.Contains(people, "bob") {
		t.Errorf("after joining again, bob is not listed: %v", people)
	}
	if held, err := rg.st.GrantsOfSubject("person", "bob"); err != nil || len(held) != 0 {
		t.Errorf("joining again brought old grants back: %+v, %v", held, err)
	}
	if w := rg.send("bob", http.MethodGet, hostedSessionsPath+"/"+chat, nil, nil); w.Code != http.StatusNotFound || w.Body.String() != hostNoSuchChat {
		t.Errorf("after joining again, bob's read of the old chat = %d %s, want 404 %q", w.Code, w.Body, hostNoSuchChat)
	}
}

// gateOf is the secret in a link's address, which GitHub sign-in presents as its gate.
func gateOf(t *testing.T, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	fragment, _ := url.ParseQuery(parsed.Fragment)
	return fragment.Get("token")
}

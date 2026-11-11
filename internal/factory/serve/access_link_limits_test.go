package serve

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// linkFixture is a team factory with GitHub stubbed and one watched repo.
func linkFixture(t *testing.T) (*peopleV2Fixture, *fakeGitHub) {
	t.Helper()
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	f.request(t, "PUT", "/v1/repos", "operator-secret", map[string]any{"watched": []string{"acme/api"}}, 200, nil)
	return f, g
}

func (f *peopleV2Fixture) makeLink(t *testing.T, body api.CreateAccessLink) (api.AccessLink, string) {
	t.Helper()
	var created struct {
		Link api.AccessLink `json:"link"`
	}
	f.request(t, "POST", "/v1/access/links", "operator-secret", body, 201, &created)
	parsed, err := url.Parse(created.Link.URL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, _ := url.ParseQuery(parsed.Fragment)
	return created.Link, fragment.Get("token")
}

func (f *peopleV2Fixture) links(t *testing.T) map[string]api.AccessLink {
	t.Helper()
	var list api.AccessLinks
	raw := f.request(t, "GET", "/v1/access/links", "operator-secret", nil, 200, &list)
	if strings.Contains(raw, `"url"`) || strings.Contains(raw, "github-access-secret") {
		t.Fatalf("list exposes a secret: %s", raw)
	}
	out := map[string]api.AccessLink{}
	for _, l := range list.Links {
		out[l.ID] = l
	}
	return out
}

func TestNewHumanLinksLastSevenDaysAndBoundPeople(t *testing.T) {
	f, _ := linkFixture(t)
	one, secretOne := f.makeLink(t, api.CreateAccessLink{Name: "For Mona"})
	if !one.ExpiresAt.Equal(f.now.Add(7*24*time.Hour)) || one.MaxUses != 1 || one.Used != 0 || one.Remaining != 1 || one.Status != "open" {
		t.Fatalf("default link = %+v", one)
	}
	if strings.Contains(f.links(t)[one.ID].URL, secretOne) {
		t.Fatal("secret survived creation")
	}
	many, _ := f.makeLink(t, api.CreateAccessLink{MaxUses: 10, Name: "Team link"})
	if many.MaxUses != 10 || many.Remaining != 10 {
		t.Fatalf("up-to link = %+v", many)
	}
	for _, bad := range []api.CreateAccessLink{
		{MaxUses: -1}, {MaxUses: 51}, {MaxUses: 3, Login: "priya"},
		{ExpiresAt: f.now.Add(8 * 24 * time.Hour)},
	} {
		f.request(t, "POST", "/v1/access/links", "operator-secret", bad, 422, nil)
	}
	f.makeLink(t, api.CreateAccessLink{MaxUses: 50, ExpiresAt: f.now.Add(time.Hour)})
}

func TestOnlyTheOperatorMakesOrRevokesLinks(t *testing.T) {
	f, g := linkFixture(t)
	link, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	member := signInHandle(t, f.h, g, &f.now, "priya", gate)
	f.request(t, "POST", "/v1/access/links", member, api.CreateAccessLink{}, 403, nil)
	f.request(t, "GET", "/v1/access/links", member, nil, 403, nil)
	f.request(t, "DELETE", "/v1/access/links/"+link.ID, member, nil, 403, nil)
	f.request(t, "DELETE", "/v1/access/links/"+link.ID, gate, nil, 401, nil)
}

func TestUpToNLinkSpendsOneUsePerPersonAndNeverMore(t *testing.T) {
	f, g := linkFixture(t)
	link, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	priya := signInHandle(t, f.h, g, &f.now, "priya", gate)
	if got := f.links(t)[link.ID]; got.Used != 1 || got.Remaining != 1 || got.Status != "open" {
		t.Fatalf("after one = %+v", got)
	}
	// Priya coming back on another device spends nothing.
	signInHandle(t, f.h, g, &f.now, "priya", gate)
	if got := f.links(t)[link.ID]; got.Used != 1 {
		t.Fatalf("repeat consumed: %+v", got)
	}
	sam := signInHandle(t, f.h, g, &f.now, "sam", gate)
	got := f.links(t)[link.ID]
	if got.Used != 2 || got.Remaining != 0 || got.Status != "used_up" || got.EndedAt == nil || strings.Join(got.UsedBy, ",") != "priya,sam" {
		t.Fatalf("after two = %+v", got)
	}
	// The third person is refused after GitHub sign-in and gets no session.
	g.login = "jo"
	device := startIdentity(t, f.h, gate, nil)
	f.now = f.now.Add(5 * time.Second)
	if w := pollIdentity(f.h, device, gate, nil); w.Code != 403 || !strings.Contains(w.Body.String(), linkUsedUpWords) {
		t.Fatalf("third = %d %s", w.Code, w.Body)
	}
	if f.links(t)[link.ID].Used != 2 {
		t.Fatal("refused person consumed a use")
	}
	// Sessions of people who are in are separate from the shared secret.
	f.request(t, "GET", "/v1/queue", priya, nil, 200, nil)
	f.request(t, "GET", "/v1/queue", sam, nil, 200, nil)
	f.request(t, "GET", "/v1/queue", gate, nil, 401, nil)
	var me api.Me
	f.request(t, "GET", "/v1/me", gate, nil, 401, &me)
}

func TestRevokingAHumanLinkStopsNewPeopleAndKeepsThoseWhoJoined(t *testing.T) {
	f, g := linkFixture(t)
	link, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 5})
	priya := signInHandle(t, f.h, g, &f.now, "priya", gate)
	f.request(t, "DELETE", "/v1/access/links/"+link.ID, "operator-secret", nil, 200, nil)
	f.request(t, "DELETE", "/v1/access/links/"+link.ID, "operator-secret", nil, 404, nil)
	got := f.links(t)[link.ID]
	if got.Status != "revoked" || !got.Revoked || got.EndedAt == nil || !got.EndedAt.Equal(f.now) {
		t.Fatalf("revoked = %+v", got)
	}
	f.request(t, "GET", "/v1/queue", priya, nil, 200, nil)
	g.login = "sam"
	f.request(t, "POST", "/v1/auth/device", gate, nil, 401, nil)
	// The ended link stays listed greyed for thirty days, then drops out.
	f.now = f.now.Add(31 * 24 * time.Hour)
	if _, ok := f.links(t)[link.ID]; ok {
		t.Fatal("ended link listed past thirty days")
	}
}

func TestNoVisibleRepoDeniedAndCancelledSignInsSpendNothing(t *testing.T) {
	f, g := linkFixture(t)
	link, gate := f.makeLink(t, api.CreateAccessLink{})
	g.login, g.repoStatus = "mona", 404
	device := startIdentity(t, f.h, gate, nil)
	f.now = f.now.Add(5 * time.Second)
	if w := pollIdentity(f.h, device, gate, nil); w.Code != 403 || !strings.Contains(w.Body.String(), linkNoRepoWords) {
		t.Fatalf("no repo = %d %s", w.Code, w.Body)
	}
	g.repoStatus, g.exchange = 0, `{"error":"access_denied"}`
	device = startIdentity(t, f.h, gate, nil)
	f.now = f.now.Add(5 * time.Second)
	if w := pollIdentity(f.h, device, gate, nil); w.Code != 403 {
		t.Fatalf("denied = %d %s", w.Code, w.Body)
	}
	g.exchange = ""
	if got := f.links(t)[link.ID]; got.Used != 0 || got.Status != "open" {
		t.Fatalf("link spent by a failure: %+v", got)
	}
	signInHandle(t, f.h, g, &f.now, "mona", gate)
	if got := f.links(t)[link.ID]; got.Used != 1 || got.Status != "used_up" {
		t.Fatalf("after join: %+v", got)
	}
}

func (f *peopleV2Fixture) access(t *testing.T, link, token string) api.FactoryAccess {
	t.Helper()
	var out api.FactoryAccess
	f.request(t, "GET", "/v1/factory-access?link="+url.QueryEscape(link), token, nil, 200, &out)
	return out
}

func TestLinkCardPrecedenceAndAlreadyIn(t *testing.T) {
	f, g := linkFixture(t)
	address := f.h.factory.Address
	row := func(mutate func(*tokens.Token)) string {
		tok, err := tokens.Mint()
		if err != nil {
			t.Fatal(err)
		}
		tok.AccessLink, tok.MaxUses, tok.Created, tok.Expires = true, 1, f.now, f.now.Add(tokens.HumanLinkTTL)
		mutate(&tok)
		link := accessLinkURL(address, tok)
		if err := tokens.Add(f.cfg.TokensPath, tok); err != nil {
			t.Fatal(err)
		}
		if tok.Revoked {
			t.Fatal("add revoked rows through RemoveAt")
		}
		return link
	}
	spent := func(tok *tokens.Token) {
		tok.Profile = &tokens.Profile{Handle: "raj"}
		tok.Joined = f.now
		tok.Joiners = []tokens.Joiner{{Profile: *tok.Profile, Joined: f.now}}
	}
	// revoked wins over expired and used up.
	revokedLink := row(func(tok *tokens.Token) { spent(tok); tok.Expires = f.now.Add(-time.Hour) })
	rows, _ := tokens.Load(f.cfg.TokensPath)
	if err := tokens.RemoveAt(f.cfg.TokensPath, rows[len(rows)-1].ID, f.now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := f.access(t, revokedLink, ""); got.State != "revoked" || got.EndedAt == nil {
		t.Fatalf("revoked = %+v", got)
	}
	// expired wins over used up.
	expired := row(func(tok *tokens.Token) { spent(tok); tok.Expires = f.now.Add(-time.Hour) })
	if got := f.access(t, expired, ""); got.State != "expired" {
		t.Fatalf("expired = %+v", got)
	}
	used := row(spent)
	if got := f.access(t, used, ""); got.State != "used_up" || got.Used != 1 || got.Remaining != 0 || got.MaxUses != 1 {
		t.Fatalf("used = %+v", got)
	}
	// An open up-to link says how many places are left.
	open, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 4})
	if got := f.access(t, open.URL, ""); got.State != "ready" || got.Remaining != 4 || got.ExpiresAt == nil || !strings.Contains(got.Detail, "up to 4 people · 4 left") {
		t.Fatalf("open = %+v", got)
	}
	// Somebody already in is told so, and the link stays unused.
	member := signInHandle(t, f.h, g, &f.now, "priya", gate)
	if got := f.access(t, open.URL, member); got.State != "already_joined" {
		t.Fatalf("already in = %+v", got)
	}
	fresh, _ := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	if got := f.access(t, fresh.URL, member); got.State != "already_joined" || got.Remaining != 3 {
		t.Fatalf("already in fresh = %+v", got)
	}
	if got := f.links(t)[fresh.ID]; got.Used != 0 {
		t.Fatalf("already in consumed: %+v", got)
	}
}

func TestLegacyLinkRowsKeepTheirMeaning(t *testing.T) {
	f, _ := linkFixture(t)
	explicit := f.now.Add(36 * time.Hour)
	// Written by a build with no limit, no joiners and no revoke instant.
	old := `{"tokens":[
	 {"access_link":true,"id":"tok_open","token":"legacy-open","created":"2026-10-01T00:00:00Z","expires":"2026-10-12T00:00:00Z"},
	 {"access_link":true,"id":"tok_spent","token":"legacy-spent","created":"2026-10-01T00:00:00Z","joined":"2026-10-02T00:00:00Z","profile":{"handle":"raj"}},
	 {"access_link":true,"id":"tok_gone","token_hash":"` + strings.Repeat("a", 64) + `","revoked":true,"created":"2026-10-01T00:00:00Z","expires":"2026-10-12T00:00:00Z"}]}`
	if err := os.WriteFile(f.cfg.TokensPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	links := f.links(t)
	if o := links["tok_open"]; o.MaxUses != 1 || o.Used != 0 || o.Status != "open" || !o.ExpiresAt.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("legacy open = %+v", o)
	}
	if s := links["tok_spent"]; s.MaxUses != 1 || s.Used != 1 || s.Remaining != 0 || s.Status != "used_up" || strings.Join(s.UsedBy, ",") != "raj" {
		t.Fatalf("legacy spent = %+v", s)
	}
	// A legacy revoked row without a revoke instant falls back to when it was made.
	if g := links["tok_gone"]; g.Status != "revoked" || g.EndedAt == nil {
		t.Fatalf("legacy revoked = %+v", g)
	}
	// 2026-10-12 has passed at 2026-10-11 12:00 + 36h, so the old expiry is honoured.
	f.now = explicit
	if o := f.links(t)["tok_open"]; o.Status != "expired" {
		t.Fatalf("legacy expiry ignored: %+v", o)
	}
	access := f.access(t, accessLinkURL(f.h.factory.Address, tokens.Token{Token: "legacy-spent"}), "")
	if access.State != "used_up" {
		t.Fatalf("legacy spent card = %+v", access)
	}
}

// cardSignIn drives the real link card to its end with the given cookie jar.
func (f *peopleV2Fixture) cardSignIn(t *testing.T, link string) api.FactoryAccess {
	t.Helper()
	w, out := joinLinkRequest(t, f.h, "POST", link, "sign_in", nil)
	if out.State != "waiting" {
		t.Fatalf("sign in = %+v", out)
	}
	f.now = f.now.Add(10 * time.Second)
	_, out = joinLinkRequest(t, f.h, "GET", link, "", w.Result().Cookies())
	return out
}

func TestLateRepoDisagreementNeverSpendsAUseOrLeavesASession(t *testing.T) {
	f, g := linkFixture(t)
	made, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	link := made.URL
	g.login = "mona"
	// The check before the join says yes; the one on the new session says no.
	g.repoSeq = []int{200, 404}
	out := f.cardSignIn(t, link)
	if out.State != "no_access" || out.AccessToken != "" || out.Message != linkNoRepoWords {
		t.Fatalf("disagreeing checks = %+v", out)
	}
	if got := f.links(t)[made.ID]; got.Used != 0 || got.Status != "open" || len(got.UsedBy) != 0 {
		t.Fatalf("use spent by a refused join: %+v", got)
	}
	f.h.sessions.mu.Lock()
	for _, s := range f.h.sessions.ids {
		if s.profile != nil && s.profile.Handle == "mona" {
			t.Error("refused join left a usable session")
		}
	}
	f.h.sessions.mu.Unlock()

	// A person who is already in keeps their place and session when a later
	// sign-in of theirs is downgraded; nothing is deleted from the row.
	// The fake gives every person the same GitHub token, so forget the
	// cached "no" the refusal above left behind.
	f.h.viewer.mu.Lock()
	clear(f.h.viewer.grants)
	f.h.viewer.mu.Unlock()
	priya := signInHandle(t, f.h, g, &f.now, "priya", gate)
	g.repoSeq = []int{200, 404}
	f.h.viewer.mu.Lock()
	clear(f.h.viewer.grants)
	f.h.viewer.mu.Unlock()
	if out := f.cardSignIn(t, link); out.State != "no_access" {
		t.Fatalf("priya again = %+v", out)
	}
	if got := f.links(t)[made.ID]; got.Used != 1 || strings.Join(got.UsedBy, ",") != "priya" {
		t.Fatalf("existing member uncounted: %+v", got)
	}
	f.request(t, "GET", "/v1/queue", priya, nil, 200, nil)
}

func TestGivingBackAUseKeepsAnotherLiveSessionOfTheSamePerson(t *testing.T) {
	f, g := linkFixture(t)
	made, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	signInHandle(t, f.h, g, &f.now, "sam", gate)
	// A failed second sign-in of Sam's is rolled back; the first still counts.
	f.h.joinMu.Lock()
	f.h.giveBackUse(made.ID, "sam")
	f.h.joinMu.Unlock()
	if got := f.links(t)[made.ID]; got.Used != 1 {
		t.Fatalf("rollback uncounted a successful session: %+v", got)
	}
	// With no session left for a person, the use does go back.
	f.h.sessions.mu.Lock()
	f.h.sessions.ids = map[string]accessSession{}
	f.h.sessions.mu.Unlock()
	f.h.giveBackUse(made.ID, "sam")
	if got := f.links(t)[made.ID]; got.Used != 0 {
		t.Fatalf("orphaned use kept: %+v", got)
	}
}

func TestConcurrentSamePersonJoinsCountOnce(t *testing.T) {
	f, g := linkFixture(t)
	made, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	g.login = "sam"
	var wg sync.WaitGroup
	var devices []api.Device
	for i := 0; i < 6; i++ {
		devices = append(devices, startIdentity(t, f.h, gate, nil))
	}
	f.now = f.now.Add(10 * time.Second)
	for _, device := range devices {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pollIdentity(f.h, device, gate, nil)
		}()
	}
	wg.Wait()
	if got := f.links(t)[made.ID]; got.Used != 1 || strings.Join(got.UsedBy, ",") != "sam" {
		t.Fatalf("same person counted wrongly: %+v", got)
	}
}

// onDisk reads the handles of the sessions the private snapshot holds.
func onDisk(t *testing.T, h *Handler) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(h.sessions.path)
	if err != nil {
		t.Fatal(err)
	}
	var doc sessionDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, row := range doc.Sessions {
		if row.Profile != nil {
			out[row.Profile.Handle] = true
		}
	}
	return out
}

func TestRefusedLateRepoCheckNeverReachesTheDisk(t *testing.T) {
	f, g := linkFixture(t)
	made, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	signInHandle(t, f.h, g, &f.now, "priya", gate)
	before, err := os.ReadFile(f.h.sessions.path)
	if err != nil {
		t.Fatal(err)
	}
	g.login = "mona"
	g.repoSeq = []int{200, 404}
	f.h.viewer.mu.Lock()
	clear(f.h.viewer.grants)
	f.h.viewer.mu.Unlock()
	if out := f.cardSignIn(t, made.URL); out.State != "no_access" {
		t.Fatalf("late refusal = %+v", out)
	}
	if disk := onDisk(t, f.h); disk["mona"] || !disk["priya"] {
		t.Fatalf("sessions on disk after a refusal = %v", disk)
	}
	after, _ := os.ReadFile(f.h.sessions.path)
	if string(before) != string(after) {
		t.Fatal("a refused sign-in rewrote the session file")
	}
}

func TestSessionSaveFailureFailsClosedAndKeepsEveryoneElse(t *testing.T) {
	f, g := linkFixture(t)
	made, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	priya := signInHandle(t, f.h, g, &f.now, "priya", gate)
	// The snapshot can no longer be replaced: its folder is now a file.
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.h.sessions.mu.Lock()
	f.h.sessions.path = filepath.Join(blocker, "sessions.json")
	f.h.sessions.mu.Unlock()

	g.login = "mona"
	device := startIdentity(t, f.h, gate, nil)
	f.now = f.now.Add(10 * time.Second)
	w := pollIdentity(f.h, device, gate, nil)
	if w.Code < 500 || strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("save failure = %d %s", w.Code, w.Body)
	}
	if got := f.links(t)[made.ID]; got.Used != 1 || strings.Join(got.UsedBy, ",") != "priya" {
		t.Fatalf("failed sign-in kept its use: %+v", got)
	}
	f.h.sessions.mu.Lock()
	for _, s := range f.h.sessions.ids {
		if s.profile != nil && s.profile.Handle == "mona" {
			t.Error("unsaved session stayed in memory")
		}
	}
	f.h.sessions.mu.Unlock()
	f.request(t, "GET", "/v1/queue", priya, nil, 200, nil)
}

func TestGivingBackAUseIgnoresAnExpiredSessionOfTheSamePerson(t *testing.T) {
	f, g := linkFixture(t)
	made, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	signInHandle(t, f.h, g, &f.now, "sam", gate)
	f.h.sessions.mu.Lock()
	for key, s := range f.h.sessions.ids {
		s.expires = f.now.Add(-time.Second)
		f.h.sessions.ids[key] = s
	}
	f.h.sessions.mu.Unlock()
	f.h.giveBackUse(made.ID, "sam")
	if got := f.links(t)[made.ID]; got.Used != 0 {
		t.Fatalf("an expired session kept the use: %+v", got)
	}
}

func TestSessionOfAPersonGivenBackIsNotUsable(t *testing.T) {
	f, g := linkFixture(t)
	made, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	token := signInHandle(t, f.h, g, &f.now, "sam", gate)
	if err := tokens.Unjoin(f.cfg.TokensPath, made.ID, "sam"); err != nil {
		t.Fatal(err)
	}
	f.request(t, "GET", "/v1/queue", token, nil, 401, nil)
}

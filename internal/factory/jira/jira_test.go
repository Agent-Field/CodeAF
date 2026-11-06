package jira_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/jira"
	"github.com/Agent-Field/codeaf/internal/factory/jira/jirastub"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const (
	stubEmail = "ada@example.com"
	stubToken = "tok-1234567890"
)

func stub(t *testing.T) *jirastub.Server {
	t.Helper()
	s := jirastub.New(stubEmail, stubToken)
	t.Cleanup(s.Close)
	return s
}

func clientOf(s *jirastub.Server) *jira.Client {
	return &jira.Client{Site: s.URL, Email: stubEmail, Token: stubToken}
}

// A description in ADF reads as plain text a model can read: paragraphs
// apart, lists as lines, mentions and links by their words, nulls as nothing.
func TestADFDescriptionReadsAsPlainText(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","version":1,"content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Steps"}]},
		{"type":"paragraph","content":[{"type":"text","text":"Ask "},{"type":"mention","attrs":{"id":"1","text":"@Grace"}},{"type":"text","text":" first."},{"type":"hardBreak"},{"type":"text","text":"Then "},{"type":"text","text":"ship","marks":[{"type":"strong"}]},{"type":"text","text":" "},{"type":"emoji","attrs":{"shortName":":rocket:","text":"🚀"}}]},
		{"type":"orderedList","attrs":{"order":3},"content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"open"}]}]},
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"close"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]}]},
		{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1"}]},
		{"type":"paragraph","content":[{"type":"inlineCard","attrs":{"url":"https://example.com/a"}}]},
		{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
		{"type":"rule"},
		{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"x"}}]}
	]}`)
	want := "Steps\n\nAsk @Grace first.\nThen ship 🚀\n\n3. open\n4. close\n  - nested\n\nx := 1\n\nhttps://example.com/a\n\n> quoted\n\n---"
	if got := jira.PlainText(raw); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	for _, empty := range []string{``, `null`, `{"nope`, `{"type":"doc","version":1,"content":[]}`} {
		if got := jira.PlainText(json.RawMessage(empty)); got != "" {
			t.Errorf("%s read as %q", empty, got)
		}
	}
	if got := jira.PlainText(json.RawMessage(`"  a v2 string  "`)); got != "a v2 string" {
		t.Errorf("a string description read as %q", got)
	}
}

// A comment is a valid ADF document whose links are real links, and it reads
// back as the text it was made from.
func TestDocIsADFWithLinksAndReadsBack(t *testing.T) {
	text := "codeaf opened a pull request: https://github.com/o/r/pull/7\n\nsecond line"
	raw, _ := json.Marshal(jira.Doc(text))
	if got := jira.PlainText(raw); got != text {
		t.Fatalf("round trip: %q", got)
	}
	if !strings.Contains(string(raw), `"marks":[{"type":"link","attrs":{"href":"https://github.com/o/r/pull/7"}}]`) {
		t.Fatalf("the link is not marked: %s", raw)
	}
	if !strings.HasPrefix(string(raw), `{"type":"doc","version":1,`) {
		t.Fatalf("not a doc: %s", raw)
	}
}

// THE ONE PRIORITY TABLE.
func TestPriorityTable(t *testing.T) {
	for name, want := range map[string]string{
		"Highest": "p0", "High": "p1", "Medium": "p2", "Low": "p3", "Lowest": "",
		" highest ": "p0", "Blocker": "", "": "",
	} {
		if got := jira.Priority(name); got != want {
			t.Errorf("%q → %q, want %q", name, got, want)
		}
	}
}

// The client checks the account, finds tickets page by page and posts a
// comment, and a refused credential is said in words that carry no token.
func TestClientAgainstTheStub(t *testing.T) {
	s := stub(t)
	ctx := context.Background()
	c := clientOf(s)
	me, err := c.Myself(ctx)
	if err != nil || me.EmailAddress != stubEmail || me.TimeZone != "UTC" {
		t.Fatalf("myself: %+v %v", me, err)
	}
	for range 3 {
		s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "x"})
	}
	page, err := c.Search(ctx, `(project = FL) ORDER BY updated ASC`, "")
	if err != nil || len(page.Issues) != 3 || !page.IsLast || page.Issues[0].Key != "FL-1" {
		t.Fatalf("search: %+v %v", page, err)
	}
	if err := c.Comment(ctx, "FL-2", jira.Doc("hello")); err != nil {
		t.Fatal(err)
	}
	if got := s.Comments("FL-2"); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("comments: %v", got)
	}
	if err := c.Comment(ctx, "FL-99", jira.Doc("x")); err == nil || !strings.Contains(err.Error(), "no such ticket") {
		t.Fatalf("a missing ticket: %v", err)
	}
	if err := c.Comment(ctx, "../../x", jira.Doc("x")); err == nil {
		t.Fatal("a key that is not a key reached a path")
	}
	bad := &jira.Client{Site: s.URL, Email: stubEmail, Token: "wrong-secret"}
	_, err = bad.Myself(ctx)
	if !jira.IsAuth(err) || strings.Contains(err.Error(), "wrong-secret") {
		t.Fatalf("a refused token: %v", err)
	}
	if _, err := c.Search(ctx, `project = FL`, ""); err == nil || !strings.Contains(err.Error(), "refused the search") {
		t.Fatalf("a JQL the stub cannot read was not refused in words: %v", err)
	}
}

// The feed sends the person's conditions, then narrows by its cursor; each
// ticket maps to one entry; the next cursor is the newest change less a
// minute, in the account's zone.
func TestFeedMapsTicketsAndMovesTheCursor(t *testing.T) {
	s := stub(t)
	s.SetTimeZone("America/Toronto")
	at := time.Date(2026, 11, 6, 15, 30, 45, 0, time.UTC)
	s.SetNow(func() time.Time { return at })
	key := s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "  Fix the login  ", Description: "It fails.\n\nOn Safari.", Labels: []string{"bug"}, Priority: "High", Reporter: "Grace Hopper", ReporterEmail: "grace@example.com"})
	s.CreateIssue(jirastub.Issue{Project: "OPS", Summary: "not mine"})
	zone, _ := time.LoadLocation("America/Toronto")
	f := &jira.Feed{Client: clientOf(s), JQL: "project = FL order by created DESC", Zone: zone}
	if f.Name() != "jira" {
		t.Fatalf("name %q", f.Name())
	}
	entries, next, err := f.Fetch(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := inbox.Entry{Source: "jira", ExternalID: key, URL: s.URL + "/browse/" + key, Title: "Fix the login", Body: "It fails.\n\nOn Safari.", Author: "Grace Hopper <grace@example.com>", Labels: []string{"bug", "jira-project:FL"}, Priority: "p1", Trust: factory.TierCollab}
	if len(entries) != 1 || !equalEntry(entries[0], want) {
		t.Fatalf("entries %+v", entries)
	}
	// 15:30 UTC is 10:30 in Toronto that day; less a minute.
	if next != "2026-11-06 10:29" {
		t.Fatalf("next cursor %q", next)
	}
	if got := s.Searches(); got[len(got)-1] != `(project = FL) ORDER BY updated ASC` {
		t.Fatalf("first query %q", got[len(got)-1])
	}
	// Nothing changed: the same cursor comes back and the query carries it.
	entries, again, err := f.Fetch(context.Background(), next)
	if err != nil || again != next || len(entries) != 1 {
		t.Fatalf("second read: %d %q %v", len(entries), again, err)
	}
	if got := s.Searches(); got[len(got)-1] != `(project = FL) AND updated >= "2026-11-06 10:29" ORDER BY updated ASC` {
		t.Fatalf("narrowed query %q", got[len(got)-1])
	}
	// A later cursor finds nothing and keeps itself.
	entries, kept, err := f.Fetch(context.Background(), "2026-11-06 11:00")
	if err != nil || len(entries) != 0 || kept != "2026-11-06 11:00" {
		t.Fatalf("a later cursor: %d %q %v", len(entries), kept, err)
	}
}

func equalEntry(a, b inbox.Entry) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// A search larger than a page is read to the end.
func TestFeedReadsEveryPage(t *testing.T) {
	s := stub(t)
	for range jira.PageSize + 20 {
		s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "x"})
	}
	entries, _, err := (&jira.Feed{Client: clientOf(s), JQL: "project = FL"}).Fetch(context.Background(), "")
	if err != nil || len(entries) != jira.PageSize+20 {
		t.Fatalf("%d entries, %v", len(entries), err)
	}
}

// A query with no conditions is refused before any call; a failed read keeps
// the cursor.
func TestFeedRefusals(t *testing.T) {
	if _, err := jira.Query("ORDER BY created", ""); err == nil {
		t.Fatal("an unbounded query was accepted")
	}
	if _, err := jira.Query("project = FL", "yesterday"); err == nil {
		t.Fatal("an unreadable cursor was accepted")
	}
	s := stub(t)
	f := &jira.Feed{Client: clientOf(s), JQL: "project = FL"}
	s.FailNextSearch(http.StatusServiceUnavailable)
	_, next, err := f.Fetch(context.Background(), "2026-01-01 00:00")
	if err == nil || next != "2026-01-01 00:00" {
		t.Fatalf("a failed read: %q %v", next, err)
	}
}

// The replier comments the pull request's link when it opens and merges, the
// job's words when it is stuck, and nothing for a moment with nothing to say.
func TestReplierCommentsEachMoment(t *testing.T) {
	s := stub(t)
	key := s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "x"})
	r := jira.Replier{Client: clientOf(s)}
	ctx := context.Background()
	url := "https://github.com/o/r/pull/9"
	for _, ev := range []inbox.Event{
		{Kind: inbox.EventPROpened, JobID: 1, URL: url},
		{Kind: inbox.EventMerged, JobID: 1, URL: url},
		{Kind: inbox.EventStuck, JobID: 1, Text: "The checks failed twice; it needs a person."},
		{Kind: inbox.EventPROpened, JobID: 1},
		{Kind: "unknown", JobID: 1, URL: url},
	} {
		if err := r.Reply(ctx, key, ev); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"codeaf opened a pull request: " + url, "Merged: " + url, "The checks failed twice; it needs a person."}
	if got := s.Comments(key); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("comments %q", got)
	}
}

// A connection checks before it keeps anything, keeps the token in the
// profile and never beside the floor, says its status without the token,
// feeds and answers while connected, and after a disconnect does neither.
func TestConnectionConnectsStatusesAndDisconnects(t *testing.T) {
	s := stub(t)
	ctx := context.Background()
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &jira.Connection{Store: floor, Profile: t.TempDir()}
	if _, ok := c.Feed(); ok || c.Status().Connected {
		t.Fatal("a fresh factory is connected")
	}
	if _, err := c.Connect(ctx, s.URL, stubEmail, "wrong", "project = FL"); !jira.IsAuth(err) {
		t.Fatalf("a wrong token: %v", err)
	}
	if _, err := c.Connect(ctx, s.URL, stubEmail, stubToken, "project ~ FL"); err == nil {
		t.Fatal("a JQL Jira refuses connected")
	}
	if _, err := os.Stat(filepath.Join(floor.Root(), "jira.json")); !os.IsNotExist(err) || config.JiraTokenAt(c.Profile) != "" {
		t.Fatal("a refused connect kept something")
	}
	st, err := c.Connect(ctx, s.URL+"/jira/your-work", stubEmail, stubToken, "project = FL")
	if err != nil || !st.Connected || st.Site != s.URL || st.JQL != "project = FL" || st.Zone != "UTC" {
		t.Fatalf("connect: %+v %v", st, err)
	}
	if config.JiraTokenAt(c.Profile) != stubToken {
		t.Fatal("the token was not kept in the profile")
	}
	kept, _ := os.ReadFile(filepath.Join(floor.Root(), "jira.json"))
	if strings.Contains(string(kept), stubToken) {
		t.Fatal("the token is in the factory's folder")
	}
	key := s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "x"})
	f, ok := c.Feed()
	if !ok {
		t.Fatal("no feed while connected")
	}
	// One pass of the inbox's own poll files the ticket once, however often
	// it is read, and the status reads the poll's record of it.
	for range 2 {
		if _, err := inbox.PollOnce(ctx, f, floor, "", time.Now); err != nil {
			t.Fatal(err)
		}
	}
	items, _ := floor.List()
	if len(items) != 1 || items[0].SourceRef != inbox.Ref("jira", key) || items[0].Title != "x" {
		t.Fatalf("jobs after two reads: %+v", items)
	}
	if st := c.Status(); st.LastRead.IsZero() || st.LastError != "" {
		t.Fatalf("after a read: %+v", st)
	}
	s.FailNextSearch(http.StatusInternalServerError)
	_, _ = inbox.PollOnce(ctx, f, floor, "", time.Now)
	if st := c.Status(); st.LastRead.IsZero() || !strings.Contains(st.LastError, "500") {
		t.Fatalf("after a failed read: %+v", st)
	}
	if err := c.Replier().Reply(ctx, key, inbox.Event{Kind: inbox.EventMerged, URL: "https://x.test/1"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if st := c.Status(); st.Connected || st.LastError != "" || !st.LastRead.IsZero() || config.JiraTokenAt(c.Profile) != "" {
		t.Fatalf("after a disconnect: %+v", st)
	}
	if _, ok := c.Feed(); ok {
		t.Fatal("still connected after a disconnect")
	}
	if err := c.Replier().Reply(ctx, key, inbox.Event{Kind: inbox.EventMerged, URL: "https://x.test/2"}); err == nil {
		t.Fatal("a disconnected factory commented")
	}
	if got := s.Comments(key); len(got) != 1 {
		t.Fatalf("comments %v", got)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatalf("a second disconnect: %v", err)
	}
}

// A site is its scheme and host; http is refused except on this machine.
func TestNormalizeSite(t *testing.T) {
	for in, want := range map[string]string{
		"acme.atlassian.net":                       "https://acme.atlassian.net",
		"https://acme.atlassian.net/jira/for-you/": "https://acme.atlassian.net",
		"http://127.0.0.1:4242":                    "http://127.0.0.1:4242",
	} {
		if got, err := jira.NormalizeSite(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "http://acme.atlassian.net", "ftp://x", "https://u:p@acme.atlassian.net"} {
		if _, err := jira.NormalizeSite(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// With the account's zone unknown, the cursor is written in UTC and starts
// early enough for any zone, so no ticket is skipped.
func TestAnUnknownZoneWidensTheCursor(t *testing.T) {
	s := stub(t)
	s.SetNow(func() time.Time { return time.Date(2026, 11, 6, 15, 30, 0, 0, time.UTC) })
	s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "x"})
	_, next, err := (&jira.Feed{Client: clientOf(s), JQL: "project = FL"}).Fetch(context.Background(), "")
	if err != nil || next != "2026-11-06 01:29" {
		t.Fatalf("%q %v", next, err)
	}
}

// THE ROUTING SEAM: a ticket filed through the inbox is a job a rule naming
// source jira and its project's label matches, and one naming another
// project does not. The connector picks no repository; the rule does.
func TestAJiraJobIsMatchedBySourceAndProjectLabel(t *testing.T) {
	s := stub(t)
	key := s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "x", Priority: "High"})
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &jira.Feed{Client: clientOf(s), JQL: "project = FL"}
	if _, err := inbox.PollOnce(context.Background(), f, floor, "", time.Now); err != nil {
		t.Fatal(err)
	}
	items, _ := floor.List()
	if len(items) != 1 || items[0].SourceRef != inbox.Ref("jira", key) {
		t.Fatalf("jobs %+v", items)
	}
	job := items[0]
	if !route.Matches(route.Match{Sources: []string{"jira"}, Labels: []string{"jira-project:FL"}}, job) {
		t.Fatalf("the FL rule does not match the job (origin %q, labels %v)", job.Origin, job.Labels)
	}
	if route.Matches(route.Match{Sources: []string{"jira"}, Labels: []string{"jira-project:OPS"}}, job) {
		t.Fatal("another project's rule matched")
	}
	if route.Matches(route.Match{Sources: []string{"github"}}, job) {
		t.Fatal("a GitHub rule matched a Jira job")
	}
}

// A JIRA REPORTER IS NEVER THE OPERATOR: a person with a seat, or a reporter
// the account cannot see, is a collaborator; a service-desk customer is a
// stranger, whose ticket no AI yes starts and whose job never writes.
func TestAReporterIsTrustedAsACollaboratorAndACustomerIsAStranger(t *testing.T) {
	site := "https://acme.atlassian.net"
	for _, c := range []struct {
		name     string
		reporter *jira.User
		want     factory.Tier
	}{
		{"seat", &jira.User{DisplayName: "Ada", AccountType: "atlassian"}, factory.TierCollab},
		{"app", &jira.User{DisplayName: "Automation", AccountType: "app"}, factory.TierCollab},
		{"hidden", nil, factory.TierCollab},
		{"customer", &jira.User{DisplayName: "Pat", AccountType: "Customer"}, factory.TierStranger},
	} {
		e := jira.EntryOf(site, jira.Issue{Key: "FL-1", Fields: jira.IssueFields{Summary: "x", Reporter: c.reporter}})
		if e.Trust != c.want {
			t.Errorf("%s: trust %q, want %q", c.name, e.Trust, c.want)
		}
	}
	s := stub(t)
	s.CreateIssue(jirastub.Issue{Project: "FL", Summary: "from the portal", Reporter: "Pat", ReporterType: "customer"})
	entries, _, err := (&jira.Feed{Client: clientOf(s), JQL: "project = FL"}).Fetch(context.Background(), "")
	if err != nil || len(entries) != 1 || entries[0].Trust != factory.TierStranger {
		t.Fatalf("stub customer: %+v, %v", entries, err)
	}
}

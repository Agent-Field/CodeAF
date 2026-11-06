// Package jirastub is an in-process fake of exactly the three Jira Cloud REST
// v3 endpoints the factory's connector calls, for tests and the end-to-end
// suite:
//
//	GET  /rest/api/3/myself
//	GET  /rest/api/3/search/jql?jql=…&fields=…&maxResults=…&nextPageToken=…
//	POST /rest/api/3/issue/{key}/comment
//
// It checks basic auth on every call and reads JQL MINIMALLY: only the shape
// the connector sends, `(<conditions>) [AND updated >= "<yyyy-MM-dd HH:mm>"]
// ORDER BY updated ASC`, where the conditions are `project = KEY`,
// `project in (A, B)`, `labels = x`, `status = x`, `status != x` and
// `statusCategory != Done`, joined by AND. Anything else is refused with a
// 400, the way Jira refuses a query it cannot parse, so a connector that
// started sending something new fails a test rather than passing on a fake
// that guessed.
//
// A test creates tickets with [Server.CreateIssue], changes them with
// [Server.UpdateIssue], and reads back what was posted with
// [Server.Comments].
package jirastub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/jira"
)

// Issue is a ticket as a test describes it.
type Issue struct {
	// Project is the project key; the ticket's key is Project-N.
	Project string
	Summary string
	// Description is plain text; the stub serves it as ADF paragraphs.
	Description   string
	Labels        []string
	Priority      string // Highest, High, Medium, Low, Lowest or ""
	Reporter      string // display name
	ReporterEmail string
	Status        string // "" is "To Do"
}

// Server is the fake. URL is its site, the value a connection's site is
// set to.
type Server struct {
	URL string

	email, token string
	srv          *httptest.Server

	mu       sync.Mutex
	now      func() time.Time
	zone     string
	issues   []*stored
	seq      map[string]int
	comments map[string][]json.RawMessage
	searches []string
	fail     []int // statuses the next searches answer, in order
}

type stored struct {
	key     string
	issue   Issue
	updated time.Time
}

// New starts a stub that accepts exactly email and token. Close it when the
// test ends.
func New(email, token string) *Server {
	s := &Server{email: email, token: token, now: time.Now, zone: "UTC", seq: map[string]int{}, comments: map[string][]json.RawMessage{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/myself", s.authed(s.myself))
	mux.HandleFunc("GET /rest/api/3/search/jql", s.authed(s.search))
	mux.HandleFunc("POST /rest/api/3/issue/{key}/comment", s.authed(s.comment))
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	return s
}

// Close stops the stub.
func (s *Server) Close() { s.srv.Close() }

// SetNow replaces the stub's clock, which stamps every created or updated
// ticket.
func (s *Server) SetNow(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// SetTimeZone sets the account's zone /myself answers, which the stub also
// reads the JQL cursor in.
func (s *Server) SetTimeZone(zone string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zone = zone
}

// CreateIssue files a ticket and answers its key, such as FL-1.
func (s *Server) CreateIssue(i Issue) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq[i.Project]++
	key := fmt.Sprintf("%s-%d", i.Project, s.seq[i.Project])
	s.issues = append(s.issues, &stored{key: key, issue: i, updated: s.now()})
	return key
}

// UpdateIssue changes a ticket and stamps it updated now. It answers false
// for a key the stub does not have.
func (s *Server) UpdateIssue(key string, change func(*Issue)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.issues {
		if st.key == key {
			change(&st.issue)
			st.updated = s.now()
			return true
		}
	}
	return false
}

// Comments are the comments posted on key, oldest first, each as plain text.
func (s *Server) Comments(key string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.comments[key]))
	for _, body := range s.comments[key] {
		out = append(out, jira.PlainText(body))
	}
	return out
}

// CommentBodies are the comments posted on key as the ADF documents sent.
func (s *Server) CommentBodies(key string) []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]json.RawMessage(nil), s.comments[key]...)
}

// Searches are the JQL queries the stub received, oldest first.
func (s *Server) Searches() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.searches...)
}

// FailNextSearch makes the next search answer status (one per call).
func (s *Server) FailNextSearch(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = append(s.fail, status)
}

func (s *Server) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, token, ok := r.BasicAuth()
		if !ok || email != s.email || token != s.token {
			refuse(w, http.StatusUnauthorized, "Client must be authenticated to access this resource.")
			return
		}
		h(w, r)
	}
}

func (s *Server) myself(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	zone := s.zone
	s.mu.Unlock()
	answer(w, http.StatusOK, jira.User{AccountID: "stub-account", DisplayName: "Stub Person", EmailAddress: s.email, TimeZone: zone})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	jql := q.Get("jql")
	s.mu.Lock()
	s.searches = append(s.searches, jql)
	if len(s.fail) > 0 {
		status := s.fail[0]
		s.fail = s.fail[1:]
		s.mu.Unlock()
		refuse(w, status, "the stub was told to fail this search")
		return
	}
	zone := s.zone
	s.mu.Unlock()
	if q.Get("fields") != jira.SearchFields {
		refuse(w, http.StatusBadRequest, "the stub expects fields="+jira.SearchFields)
		return
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	match, err := parseJQL(jql, loc)
	if err != nil {
		refuse(w, http.StatusBadRequest, "Error in the JQL Query: "+err.Error())
		return
	}
	size := jira.PageSize
	if n, err := strconv.Atoi(q.Get("maxResults")); err == nil && n > 0 && n < size {
		size = n
	}
	from := 0
	if tok := q.Get("nextPageToken"); tok != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(tok, "page:"))
		if err != nil || !strings.HasPrefix(tok, "page:") {
			refuse(w, http.StatusBadRequest, "The provided next page token is invalid or expired.")
			return
		}
		from = n
	}
	s.mu.Lock()
	var found []stored
	for _, st := range s.issues {
		if match(st) {
			found = append(found, *st)
		}
	}
	s.mu.Unlock()
	sort.SliceStable(found, func(i, j int) bool { return found[i].updated.Before(found[j].updated) })
	page := jira.SearchPage{Issues: []jira.Issue{}}
	for i := from; i < len(found) && i < from+size; i++ {
		page.Issues = append(page.Issues, issueOf(found[i]))
	}
	if from+size < len(found) {
		page.NextPageToken = "page:" + strconv.Itoa(from+size)
	} else {
		page.IsLast = true
	}
	answer(w, http.StatusOK, page)
}

func (s *Server) comment(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	s.mu.Lock()
	known := false
	for _, st := range s.issues {
		known = known || st.key == key
	}
	s.mu.Unlock()
	if !known {
		refuse(w, http.StatusNotFound, "Issue does not exist or you do not have permission to see it.")
		return
	}
	var body struct {
		Body json.RawMessage `json:"body"`
	}
	var doc struct {
		Type    string            `json:"type"`
		Version int               `json:"version"`
		Content []json.RawMessage `json:"content"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || json.Unmarshal(body.Body, &doc) != nil || doc.Type != "doc" || doc.Version != 1 || len(doc.Content) == 0 {
		refuse(w, http.StatusBadRequest, "Comment body is not valid ADF.")
		return
	}
	s.mu.Lock()
	s.comments[key] = append(s.comments[key], body.Body)
	id := len(s.comments[key])
	s.mu.Unlock()
	answer(w, http.StatusCreated, map[string]any{"id": strconv.Itoa(id), "body": body.Body})
}

func issueOf(st stored) jira.Issue {
	f := jira.IssueFields{Summary: st.issue.Summary, Labels: st.issue.Labels, Updated: jira.Time{Time: st.updated}}
	if st.issue.Description != "" {
		f.Description, _ = json.Marshal(jira.Doc(st.issue.Description))
	}
	if st.issue.Priority != "" {
		f.Priority = &jira.Named{Name: st.issue.Priority}
	}
	if st.issue.Reporter != "" || st.issue.ReporterEmail != "" {
		f.Reporter = &jira.User{DisplayName: st.issue.Reporter, EmailAddress: st.issue.ReporterEmail}
	}
	f.Status = &jira.Named{Name: statusOf(st.issue)}
	return jira.Issue{ID: st.key, Key: st.key, Fields: f}
}

func statusOf(i Issue) string {
	if i.Status == "" {
		return "To Do"
	}
	return i.Status
}

var (
	reQuery = regexp.MustCompile(`^\((.+)\)(?: AND updated >= "(\d{4}-\d{2}-\d{2} \d{2}:\d{2})")? ORDER BY updated ASC$`)
	reAnd   = regexp.MustCompile(`(?i)\s+AND\s+`)
	reEq    = regexp.MustCompile(`(?i)^(project|labels|status|statuscategory)\s*(=|!=)\s*"?([^"]+?)"?$`)
	reIn    = regexp.MustCompile(`(?i)^project\s+in\s*\(([^)]*)\)$`)
)

// parseJQL reads the one shape the connector sends.
func parseJQL(jql string, loc *time.Location) (func(*stored) bool, error) {
	m := reQuery.FindStringSubmatch(jql)
	if m == nil {
		return nil, fmt.Errorf("the stub reads only (<conditions>) [AND updated >= \"<date>\"] ORDER BY updated ASC, not %q", jql)
	}
	var tests []func(*stored) bool
	for _, clause := range reAnd.Split(strings.TrimSpace(m[1]), -1) {
		clause = strings.TrimSpace(clause)
		if in := reIn.FindStringSubmatch(clause); in != nil {
			want := map[string]bool{}
			for _, p := range strings.Split(in[1], ",") {
				want[strings.ToUpper(strings.Trim(strings.TrimSpace(p), `"`))] = true
			}
			tests = append(tests, func(st *stored) bool { return want[strings.ToUpper(st.issue.Project)] })
			continue
		}
		eq := reEq.FindStringSubmatch(clause)
		if eq == nil {
			return nil, fmt.Errorf("the stub cannot read %q", clause)
		}
		field, negate, value := strings.ToLower(eq[1]), eq[2] == "!=", eq[3]
		var test func(*stored) bool
		switch field {
		case "project":
			test = func(st *stored) bool { return strings.EqualFold(st.issue.Project, value) }
		case "labels":
			test = func(st *stored) bool {
				for _, l := range st.issue.Labels {
					if l == value {
						return true
					}
				}
				return false
			}
		case "status":
			test = func(st *stored) bool { return strings.EqualFold(statusOf(st.issue), value) }
		case "statuscategory":
			if !strings.EqualFold(value, "Done") {
				return nil, fmt.Errorf("the stub knows only statusCategory Done")
			}
			test = func(st *stored) bool { return strings.EqualFold(statusOf(st.issue), "Done") }
		}
		if negate {
			inner := test
			test = func(st *stored) bool { return !inner(st) }
		}
		tests = append(tests, test)
	}
	var since time.Time
	if m[2] != "" {
		t, err := time.ParseInLocation(jira.CursorLayout, m[2], loc)
		if err != nil {
			return nil, err
		}
		since = t
	}
	return func(st *stored) bool {
		if !since.IsZero() && st.updated.Before(since) {
			return false
		}
		for _, t := range tests {
			if !t(st) {
				return false
			}
		}
		return true
	}, nil
}

func answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func refuse(w http.ResponseWriter, status int, msg string) {
	answer(w, status, map[string]any{"errorMessages": []string{msg}, "errors": map[string]string{}})
}

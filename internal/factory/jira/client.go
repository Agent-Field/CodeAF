// Package jira is the factory's Jira Cloud connector. It TRANSLATES and does
// nothing else: [Feed] turns the issues a person's JQL finds into
// [inbox.Entry] values, and [Replier] turns a job's moments ([inbox.Event])
// into comments on the ticket. Which repository, which pipeline and when to
// start are the routing rules' to decide (internal/factory/route), and
// filing a job is [inbox.Add]'s; this package never touches the store.
//
// [Client] is a small typed client for exactly the three Jira Cloud REST v3
// calls the connector makes, and jirastub is an in-process fake of exactly
// those three. docs/design/factory-general/DESIGN.md is the contract.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SearchFields are the issue fields a search asks for: what an entry is made
// of, and nothing else, so a large site's tickets stay small on the wire.
const SearchFields = "summary,description,labels,components,priority,reporter,updated,status"

// PageSize is how many issues one search page asks for.
const PageSize = 100

// callTimeout bounds one Jira call when the caller's context has no sooner
// deadline, so a site that hangs never holds the poll.
const callTimeout = 30 * time.Second

// maxBody is the most of one answer the client reads.
const maxBody = 8 << 20

// Client calls one Jira Cloud site as one account, with basic auth (the
// account's email and an API token). Site is the site's base URL, such as
// https://acme.atlassian.net, with no trailing slash; tests point it at
// jirastub. THE TOKEN GOES INTO THE AUTHORIZATION HEADER AND NOWHERE ELSE: no
// error, log line or answer carries it.
type Client struct {
	Site  string
	Email string
	Token string
	// HTTP is the transport; nil is http.DefaultClient.
	HTTP *http.Client
}

// User is the account an answer names: the connected one (/myself) or a
// ticket's reporter.
type User struct {
	AccountID    string `json:"accountId,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	// AccountType is "atlassian" for a person with a seat on the site, "app"
	// for an integration, and "customer" for a service-desk customer who
	// raised the ticket from a portal and has no seat at all.
	AccountType string `json:"accountType,omitempty"`
	// TimeZone is the account's zone, such as America/Toronto. JQL reads a
	// date literal in it, so the feed writes its cursor in it.
	TimeZone string `json:"timeZone,omitempty"`
}

// Named is a field Jira answers as an object with a name: priority, status.
type Named struct {
	Name string `json:"name"`
}

// Time is a Jira timestamp, which is not RFC 3339: 2026-11-06T10:15:30.000+0000.
type Time struct{ time.Time }

// TimeLayout is how Jira spells a timestamp in an answer.
const TimeLayout = "2006-01-02T15:04:05.000-0700"

func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil || s == "" {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{TimeLayout, time.RFC3339Nano, "2006-01-02T15:04:05-0700"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("jira: unreadable time %q", s)
}

func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`null`), nil
	}
	return json.Marshal(t.Format(TimeLayout))
}

// IssueFields are the fields [SearchFields] asks for.
type IssueFields struct {
	Summary string `json:"summary"`
	// Description is ADF (see [PlainText]); null when the ticket has none.
	Description json.RawMessage `json:"description,omitempty"`
	Labels      []string        `json:"labels,omitempty"`
	Components  []Named         `json:"components,omitempty"`
	Priority    *Named          `json:"priority,omitempty"`
	Reporter    *User           `json:"reporter,omitempty"`
	Updated     Time            `json:"updated"`
	Status      *Named          `json:"status,omitempty"`
}

// Issue is one ticket a search found.
type Issue struct {
	ID     string      `json:"id,omitempty"`
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
}

// SearchPage is one page of GET /rest/api/3/search/jql. The next page is
// asked for with NextPageToken until IsLast.
type SearchPage struct {
	Issues        []Issue `json:"issues"`
	NextPageToken string  `json:"nextPageToken,omitempty"`
	IsLast        bool    `json:"isLast"`
}

// Reason is what a failed answer means, decided ONCE, where the answer
// arrives ([reasonOf]), and carried on the [Error] as a fact. Nothing else in
// codeaf reads a Jira status to decide what to do; it reads this.
type Reason int

const (
	ReasonOther       Reason = iota // any other failure
	ReasonCredentials               // the email and API token were refused
	ReasonForbidden                 // the account may not do this
	ReasonMissing                   // no such ticket, account or endpoint
	ReasonRefused                   // Jira refused the request itself (a bad JQL)
	ReasonSlowDown                  // Jira asked for fewer calls
)

// reasonOf is THE ONE PLACE a Jira status becomes a reason.
func reasonOf(status int) Reason {
	switch status {
	case http.StatusUnauthorized:
		return ReasonCredentials
	case http.StatusForbidden:
		return ReasonForbidden
	case http.StatusNotFound:
		return ReasonMissing
	case http.StatusBadRequest:
		return ReasonRefused
	case http.StatusTooManyRequests:
		return ReasonSlowDown
	}
	return ReasonOther
}

// Error is a call Jira answered with a failure. Its text is in a person's
// words and carries Jira's own messages, never the request's credentials.
type Error struct {
	Status   int
	Reason   Reason
	Op       string // "search", "comment", "myself"
	Messages []string
}

func (e *Error) Error() string {
	var lead string
	switch e.Reason {
	case ReasonCredentials:
		lead = "Jira did not accept that email and API token"
	case ReasonForbidden:
		lead = "this Jira account is not allowed to " + verb(e.Op)
	case ReasonMissing:
		lead = "Jira has no such " + noun(e.Op)
	case ReasonRefused:
		lead = "Jira refused the " + noun(e.Op)
	case ReasonSlowDown:
		lead = "Jira asked codeaf to slow down"
	default:
		lead = fmt.Sprintf("Jira answered %d to the %s", e.Status, noun(e.Op))
	}
	if len(e.Messages) > 0 {
		return lead + ": " + strings.Join(e.Messages, "; ")
	}
	return lead
}

func verb(op string) string {
	switch op {
	case "comment":
		return "comment on that ticket"
	case "search":
		return "search"
	}
	return "sign in"
}

func noun(op string) string {
	switch op {
	case "comment":
		return "ticket"
	case "search":
		return "search"
	}
	return "account"
}

// ErrUnreachable is what every call that never got an answer from Jira is
// (errors.Is): a site that does not resolve, refuses, or does not answer in
// time.
var ErrUnreachable = errors.New("Jira could not be reached")

type unreachable struct{ msg string }

func (e *unreachable) Error() string        { return e.msg }
func (e *unreachable) Is(target error) bool { return target == ErrUnreachable }

// IsAuth says err is Jira refusing the credentials themselves.
func IsAuth(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Reason == ReasonCredentials
}

// reKey is a ticket's key: a project key, a dash and a number. A key that is
// not one never reaches a URL path.
var reKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

// ValidKey says key reads as a Jira issue key, such as ENG-142.
func ValidKey(key string) bool { return reKey.MatchString(key) }

// Myself is GET /rest/api/3/myself: who the credentials belong to. It is the
// connect check.
func (c *Client) Myself(ctx context.Context) (User, error) {
	var me User
	err := c.do(ctx, "myself", http.MethodGet, "/rest/api/3/myself", nil, nil, &me)
	return me, err
}

// Search is one page of GET /rest/api/3/search/jql, asking for
// [SearchFields]; pageToken is "" for the first page.
func (c *Client) Search(ctx context.Context, jql, pageToken string) (SearchPage, error) {
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("fields", SearchFields)
	q.Set("maxResults", fmt.Sprint(PageSize))
	if pageToken != "" {
		q.Set("nextPageToken", pageToken)
	}
	var page SearchPage
	err := c.do(ctx, "search", http.MethodGet, "/rest/api/3/search/jql", q, nil, &page)
	return page, err
}

// Comment is POST /rest/api/3/issue/{key}/comment with body, an ADF document
// ([Doc]).
func (c *Client) Comment(ctx context.Context, key string, body any) error {
	if !ValidKey(key) {
		return fmt.Errorf("%q is not a Jira ticket key", key)
	}
	return c.do(ctx, "comment", http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", nil, struct {
		Body any `json:"body"`
	}{body}, nil)
}

// do makes one call and decodes a success into out (nil reads nothing).
func (c *Client) do(ctx context.Context, op, method, path string, q url.Values, in, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, callTimeout)
		defer cancel()
	}
	target := strings.TrimRight(c.Site, "/") + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return errors.New("that Jira site is not a web address codeaf can reach")
	}
	req.SetBasicAuth(c.Email, c.Token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = http.DefaultClient
	}
	resp, err := httpc.Do(req)
	if err != nil {
		// A read the caller called off is not Jira's failure: it is said as
		// the cancellation it is, and only a deadline reads as Jira's silence.
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		if ctx.Err() != nil {
			return &unreachable{"Jira did not answer in time"}
		}
		// The transport's own error names the URL, which carries no secret.
		return &unreachable{"Jira could not be reached: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("Jira's answer was cut off: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Error{Status: resp.StatusCode, Reason: reasonOf(resp.StatusCode), Op: op, Messages: jiraMessages(raw)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Jira's answer to the %s could not be read", noun(op))
	}
	return nil
}

// jiraMessages reads Jira's error body ({"errorMessages":[…],"errors":{…}}),
// keeping at most a few short lines.
func jiraMessages(raw []byte) []string {
	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	out := append([]string{}, body.ErrorMessages...)
	fields := make([]string, 0, len(body.Errors))
	for field := range body.Errors {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		out = append(out, field+": "+body.Errors[field])
	}
	if len(out) > 3 {
		out = out[:3]
	}
	for i, m := range out {
		if len(m) > 300 {
			out[i] = m[:300] + "…"
		}
	}
	return out
}

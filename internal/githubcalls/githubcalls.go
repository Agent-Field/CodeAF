// Package githubcalls is an opt-in, per-endpoint log of what codeaf asks GitHub,
// for finding out what drains an account's rate limit.
//
// With CODEAF_GITHUB_LOG unset nothing is wrapped and nothing is written. Set
// to a file, every GitHub HTTP client appends one JSON line per request. The
// file is append-only and each line is one write, so the several processes of
// a factory can share it.
//
// THE LOG HOLDS THE SHAPE OF A CALL AND NOTHING PRIVATE. A path is written
// through a default-deny template: only GitHub's own API words survive, so an
// owner, a repository, a branch, a file path or a login never reaches the file.
// No query, header, body or host is read.
//
// THERE IS NO SINGLE DOOR TO GITHUB, so this is worn at each one: the forge
// client, the factory server's client, the job writes, the release client, and
// [NoteGH] where the engine runs gh. A gh run by an agent's own shell command
// is outside all of them; compare Used across lines to see what else spent.
package githubcalls

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// EnvVar names the file the log goes to. Unset, empty or "off" logs nothing.
const EnvVar = "CODEAF_GITHUB_LOG"

// Record is one line of the log. Status 304 is an answer GitHub does not count
// against the limit, and 0 is a call that got no answer. Method "GH" is a gh
// subprocess: one or more requests the engine cannot see into.
type Record struct {
	Time      time.Time `json:"t"`
	Caller    string    `json:"caller,omitempty"`
	Method    string    `json:"m"`
	Endpoint  string    `json:"ep"`
	Status    int       `json:"st"`
	Resource  string    `json:"res,omitempty"`
	Remaining int       `json:"rem,omitempty"`
	Used      int       `json:"used,omitempty"`
	Millis    int64     `json:"ms"`
	PID       int       `json:"pid"`
}

func logPath() string {
	path := env.Get(EnvVar)
	if path == "off" {
		return ""
	}
	return path
}

type callerKey struct{}

// WithCaller names what ctx's GitHub calls are done for: a factory route, or
// a background loop. Logging off, ctx comes back as it was.
func WithCaller(ctx context.Context, caller string) context.Context {
	if logPath() == "" {
		return ctx
	}
	return context.WithValue(ctx, callerKey{}, caller)
}

func callerOf(ctx context.Context) string {
	caller, _ := ctx.Value(callerKey{}).(string)
	return caller
}

// Transport wears the log around next, or hands next back untouched when the
// log is off. A nil next means http.DefaultTransport, as it does on a client.
func Transport(next http.RoundTripper) http.RoundTripper {
	path := logPath()
	if path == "" {
		return next
	}
	if next == nil {
		next = http.DefaultTransport
	}
	return logged{next: next, path: path}
}

type logged struct {
	next http.RoundTripper
	path string
}

func (l logged) RoundTrip(req *http.Request) (*http.Response, error) {
	started := time.Now()
	resp, err := l.next.RoundTrip(req)
	r := Record{Time: started.UTC(), Caller: callerOf(req.Context()), Method: req.Method, Endpoint: Template(req.URL.Path), Millis: time.Since(started).Milliseconds(), PID: os.Getpid()}
	if err == nil {
		r.Status = resp.StatusCode
		r.Resource = resp.Header.Get("X-RateLimit-Resource")
		r.Remaining, _ = strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
		r.Used, _ = strconv.Atoi(resp.Header.Get("X-RateLimit-Used"))
	}
	write(l.path, r)
	return resp, err
}

// NoteGH logs that the engine ran gh with verb, a fixed phrase such as
// "auth status". Say it from the call site; never pass the command's arguments.
func NoteGH(ctx context.Context, verb string) {
	path := logPath()
	if path == "" {
		return
	}
	write(path, Record{Time: time.Now().UTC(), Caller: callerOf(ctx), Method: "GH", Endpoint: "gh " + verb, PID: os.Getpid()})
}

var files sync.Map // path -> *os.File, nil when it would not open

// write drops the line rather than trouble the call when the file is unusable.
func write(path string, r Record) {
	line, err := json.Marshal(r)
	if err != nil {
		return
	}
	file, known := files.Load(path)
	if !known {
		opened, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			opened = nil
		}
		var raced bool
		file, raced = files.LoadOrStore(path, opened)
		if raced && opened != nil {
			_ = opened.Close()
		}
	}
	if f, _ := file.(*os.File); f != nil {
		_, _ = f.Write(append(line, '\n'))
	}
}

// apiWords are the path segments GitHub itself defines. Anything else in a path
// may be someone's name, so it is not written.
var apiWords = func() map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.Fields(`actions annotations artifacts assignees blobs branches
		check-runs check-suites collaborators comments commits compare contents events files
		git graphql installation installations issues jobs labels latest logs members
		memberships merge merges mergeable orgs permission protection pulls rate_limit readme
		ref refs releases repos repositories requested_reviewers reviews rulesets runs search
		status statuses tags teams timeline trees update-branch user users workflows`) {
		words[w] = true
	}
	return words
}()

// Template is path with everything that is not GitHub's own word taken out:
// numbers become :n, and a run of anything else one :x.
func Template(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	var out strings.Builder
	last := ""
	for i, segment := range segments {
		shape := ""
		switch {
		case segment == "":
			continue
		case segments[0] == "repos" && i == 1:
			shape = ":owner"
		case segments[0] == "repos" && i == 2:
			shape = ":repo"
		case apiWords[segment]:
			shape = segment
		case isNumber(segment):
			shape = ":n"
		default:
			shape = ":x"
		}
		if shape == ":x" && last == ":x" {
			continue
		}
		out.WriteString("/" + shape)
		last = shape
	}
	if out.Len() == 0 {
		return "/"
	}
	return out.String()
}

func isNumber(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

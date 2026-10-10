package desktopbridge

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// extra serves the routes under /sessions/{id}/ that carry a sub-path or an
// action of their own. It reports whether it answered the request.
func (s *conversation) extra(w http.ResponseWriter, r *http.Request, parts []string) bool {
	switch {
	case len(parts) == 5 && parts[2] == "tasks":
		s.taskAction(w, r, parts[3], parts[4])
	case len(parts) == 3 && parts[2] == "files":
		s.readFile(w, r)
	case len(parts) == 4 && parts[2] == "files" && parts[3] == "stat":
		s.statFiles(w, r)
	case len(parts) == 4 && parts[2] == "files" && parts[3] == "list":
		s.listFiles(w, r)
	case len(parts) >= 3 && parts[2] == "jobs":
		s.jobsRoute(w, r, parts[3:])
	case len(parts) == 3 && parts[2] == "detach":
		s.bridge.detach(w, r, s)
	case len(parts) == 4 && parts[2] == "files" && parts[3] == "text":
		s.fileText(w, r)
	case len(parts) == 4 && parts[2] == "files" && parts[3] == "find":
		s.fileFind(w, r)
	case len(parts) == 4 && parts[2] == "files" && parts[3] == "locate":
		s.fileLocate(w, r)
	case len(parts) == 3 && parts[2] == "editors":
		s.editors(w, r)
	case len(parts) == 4 && parts[2] == "editors" && parts[3] == "open":
		s.openEditor(w, r)
	case len(parts) == 3 && parts[2] == "changes":
		s.changes(w, r)
	case len(parts) == 3 && parts[2] == "diff":
		s.fileDiff(w, r)
	case len(parts) == 4 && parts[2] == "questions" && parts[3] == "hold":
		s.holdQuestion(w, r)
	case len(parts) >= 3 && parts[2] == "terminals":
		s.terminalRoute(w, r, parts[3:])
	case len(parts) == 3 && parts[2] == "sources":
		s.chatSource(w, r)
	case len(parts) == 3 && parts[2] == "favicon":
		s.favicon(w, r)
	default:
		return false
	}
	return true
}

func needPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	fail(w, 405, "POST required")
	return false
}

func needGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	fail(w, 405, "GET required")
	return false
}

// publishSnapshot tells every view the engine state moved.
func (s *conversation) publishSnapshot() {
	snapshot := s.snapshot()
	s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
}

// taskAction is E1: talk to or control one task. The engine's refusal
// sentences pass through as 409.
func (s *conversation) taskAction(w http.ResponseWriter, r *http.Request, taskID, action string) {
	if !needPost(w, r) {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	door, ok := s.conn.Agent.(interface {
		PlanNote(id, text string) error
		PlanAmend(id, text string) error
		PlanPause(id string) error
		PlanResume(id string) error
		PlanCancel(id string) error
	})
	if !ok {
		fail(w, 409, "this engine cannot steer tasks")
		return
	}
	text := strings.TrimSpace(body.Text)
	var run func() error
	switch action {
	case "note", "amend":
		if text == "" {
			fail(w, 400, "words required")
			return
		}
		run = func() error { return door.PlanNote(taskID, text) }
		if action == "amend" {
			run = func() error { return door.PlanAmend(taskID, text) }
		}
	case "pause":
		run = func() error { return door.PlanPause(taskID) }
	case "resume":
		run = func() error { return door.PlanResume(taskID) }
	case "cancel":
		run = func() error { return door.PlanCancel(taskID) }
	default:
		fail(w, 404, "unknown task action")
		return
	}
	if s.conn.Take != nil {
		if err := s.conn.Take(); err != nil {
			fail(w, 409, err.Error())
			return
		}
	}
	if err := run(); err != nil {
		fail(w, 409, err.Error())
		return
	}
	s.publishSnapshot()
	write(w, map[string]bool{"accepted": true})
}

// holdQuestion is E4: stop one open question's clock while the person reads.
func (s *conversation) holdQuestion(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var ask struct {
		Kind string `json:"kind"`
		ID   uint64 `json:"id"`
		Ref  string `json:"ref"`
	}
	if !decode(w, r, &ask) {
		return
	}
	door, ok := s.conn.Agent.(interface {
		OpenQuestions() []session.Question
		HoldQuestion(session.QuestionKind, string)
	})
	if !ok {
		fail(w, 409, "this engine cannot hold questions")
		return
	}
	for _, q := range door.OpenQuestions() {
		if string(q.Kind) == ask.Kind && q.ID == ask.ID && q.Ref == ask.Ref {
			door.HoldQuestion(q.Kind, q.Token())
			write(w, map[string]bool{"accepted": true})
			return
		}
	}
	fail(w, 409, "this question is no longer waiting")
}

// seamNotImplemented is the sentence an empty slot returns. One constant so
// the table and the test cannot drift to two different 501 bodies.
const seamNotImplemented = "not implemented"

// seamHandler is the real door a later file registers. ids are the {name}
// captures with the braces removed, so "{id}" arrives as ids["id"]. The
// handler owns every status from here; an empty slot is always 501.
type seamHandler func(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string)

// seamRoute is one path the desktop has promised. pattern is a string literal
// on the pattern key on purpose: the security scan reads `pattern: "..."` and
// requires that shape on bridgeRoutes. A constant or a concatenation would
// hide the route from that scan.
//
// PUT /places/{id}/decide is the place's threshold and alwaysAsk. The empty
// slot does not read the body; the handler that lands here does.
type seamRoute struct {
	method   string
	pattern  string
	segments []string
	handle   seamHandler
}

// seamTable is the shared route table for decisions, knows, councils and plan
// cards. A later task adds a file and calls registerSeamRoute from init. It
// does not edit this list. An empty handle answers 501, so the path is claimed
// before that file exists.
//
// Written from init only, then read without a lock. Tests that install a
// handler put the slot back before they return, and this package's tests do
// not run in parallel.
var seamTable = []seamRoute{
	{method: http.MethodGet, pattern: "/places/{id}/decisions"},
	{method: http.MethodGet, pattern: "/places/{id}/decide-status"},
	{method: http.MethodGet, pattern: "/decisions/{id}"},
	{method: http.MethodPost, pattern: "/decisions/{id}/overturn"},
	{method: http.MethodPut, pattern: "/places/{id}/decide"},
	{method: http.MethodGet, pattern: "/places/{id}/knows"},
	{method: http.MethodPost, pattern: "/places/{id}/knows"},
	{method: http.MethodPatch, pattern: "/places/{id}/knows/{line}"},
	{method: http.MethodDelete, pattern: "/places/{id}/knows/{line}"},
	{method: http.MethodPost, pattern: "/places/{id}/knows/{line}/still-true"},
	{method: http.MethodPost, pattern: "/sessions/{id}/plan/{plan}/go"},
	{method: http.MethodPost, pattern: "/sessions/{id}/plan/{plan}/edit"},
	{method: http.MethodPost, pattern: "/sessions/{id}/plan/{plan}/cancel"},
	{method: http.MethodGet, pattern: "/councils"},
	{method: http.MethodPost, pattern: "/councils/{id}/steer"},
}

func init() {
	seen := map[string]bool{}
	for i := range seamTable {
		route := &seamTable[i]
		key := route.method + " " + route.pattern
		if route.pattern == "" || !strings.HasPrefix(route.pattern, "/") || strings.Contains(route.pattern, "//") || seen[key] {
			panic("desktopbridge: bad seam route " + key)
		}
		seen[key] = true
		route.segments = strings.Split(strings.Trim(route.pattern, "/"), "/")
		for _, seg := range route.segments {
			if _, hole := seamHole(seg); hole {
				continue
			}
			if seg == "" || strings.ContainsAny(seg, "{}") {
				panic("desktopbridge: bad seam route " + key)
			}
		}
		for j := range i {
			other := &seamTable[j]
			if other.pattern != route.pattern && seamOverlaps(other.segments, route.segments) {
				panic("desktopbridge: seam routes overlap: " + other.pattern + " and " + route.pattern)
			}
		}
	}
}

// registerSeamRoute installs the handler for one row. The method and pattern
// must already be on seamTable; a typo panics at startup rather than 404ing
// in production. A second file claiming the same row panics too.
func registerSeamRoute(method, pattern string, h seamHandler) {
	if h == nil {
		panic("desktopbridge: nil seam handler for " + method + " " + pattern)
	}
	for i := range seamTable {
		if seamTable[i].method == method && seamTable[i].pattern == pattern {
			if seamTable[i].handle != nil {
				panic("desktopbridge: seam route registered twice: " + method + " " + pattern)
			}
			seamTable[i].handle = h
			return
		}
	}
	panic("desktopbridge: seam route is not on the table: " + method + " " + pattern)
}

// clearSeamRoute puts an empty slot back. Tests use it after installing a
// handler; production code registers once and leaves the row.
func clearSeamRoute(method, pattern string) {
	for i := range seamTable {
		if seamTable[i].method == method && seamTable[i].pattern == pattern {
			seamTable[i].handle = nil
			return
		}
	}
	panic("desktopbridge: seam route is not on the table: " + method + " " + pattern)
}

func seamHole(seg string) (string, bool) {
	if len(seg) < 3 || seg[0] != '{' || seg[len(seg)-1] != '}' {
		return "", false
	}
	name := seg[1 : len(seg)-1]
	if name == "" || strings.ContainsAny(name, "{}") {
		return "", false
	}
	return name, true
}

// seamOverlaps reports whether two patterns can match one concrete path. A
// hole matches any segment, so a later "{id}" beside an existing literal would
// shadow it. The same pattern on two methods is not an overlap.
func seamOverlaps(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		_, aHole := seamHole(a[i])
		_, bHole := seamHole(b[i])
		if aHole || bHole {
			continue
		}
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func matchSeam(segments, parts []string) (map[string]string, bool) {
	if len(segments) != len(parts) || len(segments) == 0 {
		return nil, false
	}
	ids := map[string]string{}
	for i, seg := range segments {
		name, hole := seamHole(seg)
		if hole {
			if parts[i] == "" {
				return nil, false
			}
			ids[name] = parts[i]
			continue
		}
		if seg != parts[i] {
			return nil, false
		}
	}
	return ids, true
}

// seamRoutes serves the shared table. It reports whether the path belonged to
// the table, including a wrong method, so the caller does not turn it into a
// 404. An empty slot is 501 until registerSeamRoute fills it.
func (b *Bridge) seamRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var allowed []string
	for i := range seamTable {
		route := &seamTable[i]
		ids, ok := matchSeam(route.segments, parts)
		if !ok {
			continue
		}
		if route.method != r.Method {
			allowed = append(allowed, route.method)
			continue
		}
		if route.handle == nil {
			fail(w, 501, seamNotImplemented)
			return true
		}
		route.handle(b, w, r, ids)
		return true
	}
	if len(allowed) == 0 {
		return false
	}
	fail(w, 405, strings.Join(allowed, " or ")+" required")
	return true
}

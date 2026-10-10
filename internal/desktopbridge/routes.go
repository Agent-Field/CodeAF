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

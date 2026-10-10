package desktopbridge

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Engine background jobs (Shell §3c, Places §6a and §8a): the list a conversation
// already holds, a stop that is Cancel("job:N"), and a log tail read from that
// conversation's own folder. The route table wires this as
//
//	case len(parts) >= 3 && parts[2] == "jobs":
//	    s.jobsRoute(w, r, parts[3:])
//
// A jobUpdate on an attached chat also publishes one `jobs` record on the
// engine-wide feed, so a window can see another conversation's running work
// without attaching to it.

// jobLogCap is the most of a log one read returns. A pane shows a tail; the
// rest stays in the file the engine holds. Same ceiling as a tool result.
const jobLogCap = maxToolDisplayBytes

// jobWire is one job as the desktop reads it. Empty facts are omitted: a name
// that has not arrived, a detail only a watch has, an exit code only a finished
// command has. The id is the job's own number, the same one Cancel takes.
type jobWire struct {
	ID        int    `json:"id"`
	Name      string `json:"name,omitempty"`
	Command   string `json:"command,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Kind      string `json:"kind,omitempty"`
	State     string `json:"state,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	ElapsedMs int64  `json:"elapsedMs,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Ticks     int    `json:"ticks,omitempty"`
	LogPath   string `json:"logPath,omitempty"`
}

// jobsRollup is the payload of a `jobs` world record: one attached chat, how
// many of its jobs are still running, and the jobs themselves.
type jobsRollup struct {
	ChatID  string    `json:"chatId"`
	Running int       `json:"running"`
	Jobs    []jobWire `json:"jobs"`
}

func jobWireOf(n session.JobNotice) jobWire {
	w := jobWire{
		ID: n.ID, Name: n.Name, Command: n.Command, Detail: n.Detail,
		Kind: string(n.Kind), State: string(n.State), Ticks: n.Ticks, LogPath: n.LogPath,
	}
	if !n.Started.IsZero() {
		w.StartedAt = n.Started.UTC().Format(time.RFC3339)
	}
	if n.Elapsed > 0 {
		w.ElapsedMs = n.Elapsed.Milliseconds()
	}
	// ExitCode is a real zero when a command exited 0, and a meaningless zero
	// while the job is still running or was stopped before it exited. Only the
	// first of those is sent.
	if n.State == session.JobDone || n.State == session.JobFailed {
		code := n.ExitCode
		w.ExitCode = &code
	}
	return w
}

func jobWires(rows []session.JobNotice) []jobWire {
	out := make([]jobWire, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobWireOf(row))
	}
	return out
}

// jobNotices reads the engine's job shelf. ok is false when this engine has
// no such door: the remote agent returns a list and an error, an in-process
// agent returns the list alone, and anything else cannot list jobs.
func jobNotices(agent any) (rows []session.JobNotice, err error, ok bool) {
	if door, has := agent.(interface {
		JobNotices() ([]session.JobNotice, error)
	}); has {
		rows, err = door.JobNotices()
		return rows, err, true
	}
	if door, has := agent.(interface {
		JobNotices() []session.JobNotice
	}); has {
		return door.JobNotices(), nil, true
	}
	return nil, nil, false
}

// jobsRoute serves /sessions/{id}/jobs[/{jobId}/{stop|log}].
func (s *conversation) jobsRoute(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	case len(rest) == 0:
		if needGet(w, r) {
			s.listJobs(w)
		}
	case len(rest) == 2 && rest[1] == "stop":
		if needPost(w, r) {
			s.stopJob(w, rest[0])
		}
	case len(rest) == 2 && rest[1] == "log":
		if needGet(w, r) {
			s.jobLog(w, r, rest[0])
		}
	default:
		fail(w, 404, "unknown job action")
	}
}

func (s *conversation) listJobs(w http.ResponseWriter) {
	rows, err, ok := jobNotices(s.conn.Agent)
	if !ok {
		fail(w, 409, "this engine cannot list its jobs")
		return
	}
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	write(w, jobWires(rows))
}

func (s *conversation) stopJob(w http.ResponseWriter, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		fail(w, 400, "job id required")
		return
	}
	door, ok := s.conn.Agent.(interface {
		Cancel(id string) (string, error)
	})
	if !ok {
		fail(w, 409, "this engine cannot stop a job")
		return
	}
	// The prefix is the whole of the address. A bare number would stop a task.
	line, err := door.Cancel("job:" + id)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	body := map[string]any{"accepted": true}
	if strings.TrimSpace(line) != "" {
		body["line"] = line
	}
	write(w, body)
}

func (s *conversation) jobLog(w http.ResponseWriter, r *http.Request, id string) {
	rows, err, ok := jobNotices(s.conn.Agent)
	if !ok {
		fail(w, 409, "this engine cannot list its jobs")
		return
	}
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	job, found := findJob(rows, id)
	if !found {
		fail(w, 404, fmt.Sprintf("there is no job %s in this conversation", id))
		return
	}
	// The path comes off the job the engine listed, never off the query. A
	// client that named a different file would be asking this route to read
	// something the conversation did not spool.
	path, err := sessionLogPath(s.conn.Welcome.SessionFile, job.LogPath)
	if err != nil {
		status := 404
		if errors.Is(err, errJobLogOutside) {
			status = 403
		}
		fail(w, status, err.Error())
		return
	}
	if s.conn.FetchFile == nil {
		fail(w, 409, "this engine cannot hand over files")
		return
	}
	file, err := s.conn.FetchFile(path)
	if err != nil {
		fail(w, refusalStatus(err), err.Error())
		return
	}
	raw, truncated, err := tailBytes(file.Bytes, r.URL.Query().Get("tail"))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	write(w, map[string]any{"text": plainOutput(raw), "truncated": truncated})
}

func findJob(rows []session.JobNotice, id string) (session.JobNotice, bool) {
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return session.JobNotice{}, false
	}
	for _, row := range rows {
		if row.ID == n {
			return row, true
		}
	}
	return session.JobNotice{}, false
}

var (
	errJobLogOutside = errors.New("that log is outside this conversation")
	errJobLogMissing = errors.New("this job has no log")
)

// sessionLogPath is the job's log only when it sits inside the conversation's
// own folder. The workspace is not a second root here: a job log is a spool
// the session wrote, and a path that merely lives beside the project is not one.
func sessionLogPath(sessionFile, logPath string) (string, error) {
	if strings.TrimSpace(sessionFile) == "" || strings.TrimSpace(logPath) == "" {
		return "", errJobLogMissing
	}
	root := filepath.Clean(filepath.Dir(sessionFile))
	path := logPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !pathWithin(root, path) {
		return "", errJobLogOutside
	}
	// A symlink that walks out is outside even when the spelling looks inside.
	// A file that is not there yet fails to resolve; the lexical check stands,
	// and FetchFile is the one that says the file is missing.
	realRoot, errRoot := filepath.EvalSymlinks(root)
	realPath, errPath := filepath.EvalSymlinks(path)
	if errRoot == nil && errPath == nil && !pathWithin(realRoot, realPath) {
		return "", errJobLogOutside
	}
	return path, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// tailBytes keeps the last n bytes, and never more than jobLogCap. An empty
// tail query means the cap. The cut is moved forward to a rune boundary so the
// text that follows is valid UTF-8.
func tailBytes(raw []byte, query string) ([]byte, bool, error) {
	limit := jobLogCap
	if query != "" {
		n, err := strconv.Atoi(query)
		if err != nil || n < 0 {
			return nil, false, errors.New("tail must be a number of bytes")
		}
		if n < limit {
			limit = n
		}
	}
	if len(raw) <= limit {
		return raw, false, nil
	}
	out := raw[len(raw)-limit:]
	for len(out) > 0 && !utf8.RuneStart(out[0]) {
		out = out[1:]
	}
	return out, true, nil
}

// noteJob folds one jobUpdate into this chat's shelf and, when the shelf
// moved, publishes the roll-up. The event wins for its own id: a list read in
// the same instant can still show the previous state.
func (s *conversation) noteJob(n session.JobNotice) {
	rows := s.absorbJob(n)
	if s.jobsWorld == nil {
		return
	}
	payload := jobsRollup{ChatID: s.jobsChatID(), Running: runningJobs(rows), Jobs: jobWires(rows)}
	sig := jobsSigOf(payload)
	s.jobsMu.Lock()
	if s.jobsSig == sig {
		s.jobsMu.Unlock()
		return
	}
	s.jobsSig = sig
	publish := s.jobsWorld
	s.jobsMu.Unlock()
	publish(jobsWorldKind, payload)
}

const jobsWorldKind = "jobs"

func (s *conversation) absorbJob(n session.JobNotice) []session.JobNotice {
	listed, err, ok := jobNotices(s.conn.Agent)
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	if s.jobsSeen == nil {
		s.jobsSeen = map[int]session.JobNotice{}
	}
	if ok && err == nil {
		s.jobsSeen = map[int]session.JobNotice{}
		for _, row := range listed {
			s.jobsSeen[row.ID] = row
		}
	}
	s.jobsSeen[n.ID] = n
	out := make([]session.JobNotice, 0, len(s.jobsSeen))
	for _, row := range s.jobsSeen {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func runningJobs(rows []session.JobNotice) int {
	n := 0
	for _, row := range rows {
		if row.State == session.JobRunning {
			n++
		}
	}
	return n
}

// jobsChatID is the conversation folder's name, the same id the world rows
// use. A session that has no folder yet is named by this bridge's handle, so
// the record still says which chat it is.
func (s *conversation) jobsChatID() string {
	file := s.conn.Welcome.SessionFile
	if file != "" {
		id := filepath.Base(filepath.Dir(file))
		if id != "" && id != "." && id != string(filepath.Separator) {
			return id
		}
	}
	return s.id
}

func jobsSigOf(payload jobsRollup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d", payload.ChatID, payload.Running)
	for _, job := range payload.Jobs {
		code := ""
		if job.ExitCode != nil {
			code = strconv.Itoa(*job.ExitCode)
		}
		fmt.Fprintf(&b, "|%d|%s|%s|%s|%s|%s|%s|%d|%s|%d|%s", job.ID, job.Name, job.Command, job.Detail, job.Kind, job.State, job.StartedAt, job.ElapsedMs, code, job.Ticks, job.LogPath)
	}
	return b.String()
}

// recordWorld appends one record to the engine-wide feed. It is what an
// attached chat's jobUpdate publishes through.
func (b *Bridge) recordWorld(kind string, payload any) {
	b.worldFeed().record(kind, payload)
}

// record appends one record. A feed that has been closed drops it: nothing is
// left listening, and a late job must not reopen the ring.
func (f *WorldFeed) record(kind string, payload any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.appendLocked(kind, payload)
}

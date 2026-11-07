package agentfield

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── THE NODE'S HTTP SURFACE ─────────────────────────────────────────────────
//
// Everything is under `/n/<secret>` (see connection.go for why that is the
// node's authentication), and everything else is 404 — a caller without the
// secret cannot even learn that a codeaf node answers here:
//
//	POST /n/<secret>/reasoners/intake   a trigger's {event, _meta}, or an entry
//	POST /n/<secret>/reasoners/message  {source, id, text} or {job, text}
//	POST /n/<secret>/reasoners/status   {source, id} or {job}
//	GET  /n/<secret>/health             {status: healthy, node_id}
//	GET  /n/<secret>/status             the control plane's agent status shape
//	GET  /n/<secret>/discover           the node id, version and reasoners
//
// THE INTAKE ANSWERS 202 when it filed a job for a dispatched execution: the
// control plane leaves the execution running, and the node reports on it
// until the job opens its pull request, ends or stops (sink.go). Anything
// that files nothing — a github nudge, a Linear comment, a cron tick with no
// entry — answers 200 with what happened, which completes the execution at
// once with that answer as its result. An entry the inbox refuses answers
// 422 with the inbox's own sentence, which the control plane records as the
// execution's and the event's error.

// maxCall is the most of one call's body the node reads.
const maxCall = 1 << 20

func (n *Node) handler(secret string, r *running) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /reasoners/"+ReasonerIntake, n.intake)
	mux.HandleFunc("POST /reasoners/"+ReasonerMessage, n.message)
	mux.HandleFunc("POST /reasoners/"+ReasonerStatus, n.status)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "healthy", "node_id": r.settings.NodeID, "version": n.Version})
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		up := n.now().Sub(r.began)
		h := n.Health()
		last := h.LastEvent
		if last.IsZero() {
			last = r.began
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "running", "uptime": up.Round(time.Second).String(), "uptime_seconds": int(up.Seconds()), "pid": os.Getpid(), "version": n.Version, "node_id": r.settings.NodeID, "last_activity": last.UTC().Format(time.RFC3339), "resources": map[string]any{}})
	})
	mux.HandleFunc("GET /discover", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"node_id": r.settings.NodeID, "version": n.Version, "deployment_type": "long_running", "reasoners": Reasoners(), "skills": []any{}})
	})
	return Guarded(secret, mux)
}

// Guarded answers next only under /n/<secret>, with that prefix taken off,
// and 404 to everything else. The secret is compared in constant time.
func Guarded(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, SecretPrefix)
		if !ok || secret == "" {
			http.NotFound(w, r)
			return
		}
		got, tail, _ := strings.Cut(rest, "/")
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			http.NotFound(w, r)
			return
		}
		inner := r.Clone(r.Context())
		inner.URL.Path = "/" + tail
		inner.URL.RawPath = ""
		next.ServeHTTP(w, inner)
	})
}

// call is one reasoner call's body, read whole.
func readCall(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCall))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "a call is at most 1 MiB"})
		return nil, false
	}
	return raw, true
}

// dispatch is the intake's input read apart: a trigger's {event, _meta}, or
// an entry from a direct call.
func dispatchOf(raw []byte, r *http.Request) (Meta, json.RawMessage, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return Meta{}, nil, errors.New("the intake takes a JSON object: a trigger's {event, _meta}, or an entry")
	}
	if rawMeta, ok := body["_meta"]; ok {
		var meta Meta
		if err := json.Unmarshal(rawMeta, &meta); err != nil {
			return Meta{}, nil, errors.New("_meta is not the control plane's dispatch metadata")
		}
		if meta.Source == "" {
			meta.Source = r.Header.Get("X-Source-Name")
		}
		if meta.EventType == "" {
			meta.EventType = r.Header.Get("X-Event-Type")
		}
		if meta.EventID == "" {
			meta.EventID = r.Header.Get("X-Event-ID")
		}
		if meta.TriggerID == "" {
			meta.TriggerID = r.Header.Get("X-Trigger-ID")
		}
		return meta, body["event"], nil
	}
	// A DIRECT CALL (POST /api/v1/execute/<node>.intake): the body is the input.
	return Meta{Source: ExecuteSource, EventID: r.Header.Get("X-Execution-ID")}, raw, nil
}

func (n *Node) intake(w http.ResponseWriter, r *http.Request) {
	raw, ok := readCall(w, r)
	if !ok {
		return
	}
	meta, event, err := dispatchOf(raw, r)
	if err != nil {
		n.heard("", err.Error())
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	source := SourceName(meta.Source)
	s, _, _, _ := n.Conn.Kept()
	t, err := Translate(meta, event, s.Trust)
	if err != nil {
		n.heard(source, "refused: "+err.Error())
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	switch {
	case t.Nudge != "":
		writeJSON(w, http.StatusOK, n.nudge(t.Nudge))
		return
	case t.File == nil:
		n.heard(source, "filed nothing: "+t.Ignored)
		writeJSON(w, http.StatusOK, map[string]any{"filed": false, "reason": t.Ignored})
		return
	}
	var opts []inbox.Option
	if n.Options != nil {
		opts = n.Options()
	}
	res, err := inbox.Add(r.Context(), n.Store, *t.File, n.now(), opts...)
	if err != nil {
		msg := redact.Secrets(err.Error())
		n.heard(source, "refused: "+msg)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": msg})
		return
	}
	what := "updated job"
	if res.Created {
		what = "filed job"
	}
	n.heard(source, what+" "+strconv.Itoa(res.ID)+" ("+t.File.ExternalID+")")
	out := map[string]any{"job": res.ID, "created": res.Created, "source": t.File.Source, "external_id": t.File.ExternalID}
	execution := strings.TrimSpace(r.Header.Get("X-Execution-ID"))
	status := http.StatusOK
	if execution != "" {
		if err := n.table().add(execution, res.ID, n.now()); err != nil {
			n.trouble("keeping the execution for job " + strconv.Itoa(res.ID) + ": " + err.Error())
		} else {
			status = http.StatusAccepted
		}
	}
	writeJSON(w, status, out)
	n.afterFiling(res.ID)
}

// afterFiling runs the factory's intake pass, so a routed job starts now, and
// then tells the job's executions where it stands. It runs on its own
// goroutine: the control plane waits thirty seconds for an answer, and a
// routing rule may ask a model.
func (n *Node) afterFiling(job int) {
	n.mu.Lock()
	r := n.running
	n.mu.Unlock()
	ctx := context.Background()
	if r != nil {
		ctx = r.ctx
	}
	guard.Go("factory/agentfield after intake", func() {
		if n.After != nil && ctx.Err() == nil {
			if err := n.After(ctx); err != nil {
				n.trouble("the intake pass after job " + strconv.Itoa(job) + ": " + err.Error())
			}
		}
		if r != nil && ctx.Err() == nil {
			n.reportJob(ctx, r.client, job)
		}
	})
}

// nudge is a github event: the factory's own GitHub read is asked to read now
// when the repository is one it watches.
func (n *Node) nudge(repo string) map[string]any {
	watched, _ := n.Store.Repos()
	for _, w := range watched {
		if strings.EqualFold(w, repo) {
			if n.Nudge != nil {
				n.Nudge()
				n.heard(SourceName("github"), "asked the GitHub read to read "+repo+" now")
				return map[string]any{"filed": false, "nudged": w, "reason": "GitHub work reaches the factory through its own GitHub read; it was asked to read " + w + " now"}
			}
			break
		}
	}
	reason := "the factory does not watch " + repo + " on GitHub"
	if n.Nudge == nil {
		reason = "this factory has no GitHub read to wake"
	}
	n.heard(SourceName("github"), "filed nothing: "+reason)
	return map[string]any{"filed": false, "reason": reason}
}

// jobRef is the message and status reasoners' way of naming a job: by the
// (source, id) it was filed under, or by its number.
type jobRef struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Job    int    `json:"job"`
	Text   string `json:"text"`
	Author string `json:"author"`
}

func (n *Node) readRef(w http.ResponseWriter, r *http.Request) (jobRef, bool) {
	raw, ok := readCall(w, r)
	if !ok {
		return jobRef{}, false
	}
	var ref jobRef
	if err := json.Unmarshal(raw, &ref); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "the call is a JSON object: {source, id} or {job}"})
		return jobRef{}, false
	}
	ref.Source, ref.ID = strings.TrimSpace(ref.Source), strings.TrimSpace(ref.ID)
	if ref.Job <= 0 && (ref.Source == "" || ref.ID == "") {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "name the job: {source, id} as it was filed, or {job}"})
		return jobRef{}, false
	}
	return ref, true
}

// filedAs is the (source, id) ref names: its own, or the one its job was
// filed under. A job the inbox did not file is [inbox.ErrNoJob].
func (n *Node) filedAs(ref jobRef) (string, string, error) {
	if ref.Job <= 0 {
		return ref.Source, ref.ID, nil
	}
	it, err := n.Store.GetSaved(ref.Job)
	if err != nil {
		return "", "", inbox.ErrNoJob
	}
	source, id, ok := inbox.Split(it)
	if !ok {
		return "", "", inbox.ErrNoJob
	}
	return source, id, nil
}

// message is the Trigger API's message door ([trigger.Message]): the words
// are a comment on the job, read the way every comment is, HELD TO THE LOWER
// of the job's trust and the control plane's.
func (n *Node) message(w http.ResponseWriter, r *http.Request) {
	ref, ok := n.readRef(w, r)
	if !ok {
		return
	}
	source, id, err := n.filedAs(ref)
	if err == nil {
		s, _, _, _ := n.Conn.Kept()
		var got trigger.Accepted
		got, err = trigger.Message(r.Context(), n.Store, trigger.Said{Source: source, ID: id, Author: ref.Author, Text: ref.Text, Trust: capTrust("", s.Trust)}, n.now())
		if err == nil {
			n.heard(SourceName(ExecuteSource), "a message for job "+strconv.Itoa(got.Job)+": "+got.Line)
			writeJSON(w, http.StatusOK, got)
			return
		}
	}
	writeDoorError(w, err)
}

// status is the Trigger API's status door ([trigger.Status]).
func (n *Node) status(w http.ResponseWriter, r *http.Request) {
	ref, ok := n.readRef(w, r)
	if !ok {
		return
	}
	var got api.IntakeStatus
	var err error
	if ref.Job > 0 {
		got, err = trigger.StatusOfJob(n.Store, ref.Job)
	} else {
		got, err = trigger.Status(n.Store, ref.Source, ref.ID)
	}
	if err != nil {
		writeDoorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

// writeDoorError answers a door's refusal: 404 for a job never filed, 422 for
// words it cannot take, 500 for anything else.
func writeDoorError(w http.ResponseWriter, err error) {
	var refused inbox.Refused
	switch {
	case errors.Is(err, inbox.ErrNoJob):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	case errors.As(err, &refused):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": refused.Words})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": redact.Secrets(err.Error())})
	}
}

// Facts are what every execution result says about a job.
func Facts(it factory.Item) map[string]any {
	done, total := factory.StepCount(it)
	out := map[string]any{"job": it.ID, "title": it.Title, "state": factory.JobState(it), "steps_done": done, "steps_total": total}
	if repo := factory.RepoOf(it); repo != "" {
		out["repo"] = repo
	}
	if it.Route != "" {
		out["route"] = it.Route
	}
	if pr := it.PullRequest; pr != nil && pr.URL != "" {
		out["pr_url"] = pr.URL
	}
	if it.State == factory.StateNeedsYou && it.Question != "" {
		out["question"] = it.Question
	}
	if source, id, ok := inbox.Split(it); ok {
		out["source"], out["external_id"] = source, id
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

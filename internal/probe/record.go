package probe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Record is one step-indexed line of a session's evidence file. It is a
// record of what WAS observed and what WAS sent — evidence, never a script
// to re-execute: a live CodeAF session is nondeterministic, so a playback
// reads these lines, it does not replay them against a real session.
type Record struct {
	Step        int             `json:"step"`
	TS          string          `json:"ts"`                // RFC3339, UTC
	Verb        string          `json:"verb"`              // observe | act | wait | outcome
	Request     json.RawMessage `json:"request,omitempty"` // the request as the caller gave it
	Observation *ObserveData    `json:"observation,omitempty"`
	Revision    int             `json:"revision,omitempty"` // session revision at the time of the step

	// Outcome is set only on terminal records (Verb == "outcome"): how the
	// journey ended. "ok" for a completed journey, "failed" or "error" for
	// an incomplete one — failures must land here, never silently pass.
	Outcome string `json:"outcome,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// RecordingPath is where a session's evidence file lives: under the probe
// root's recordings directory, one JSONL file per session, so Finish (which
// removes the session's home) does not destroy the evidence.
func RecordingPath(m *Manager, sessionID string) string {
	return filepath.Join(m.Root, "recordings", sanitizeName(sessionID)+".jsonl")
}

// Recorder appends step-indexed records to a session's evidence file.
// The file is private by default (0600, directory 0700) — recordings carry
// screen content and are nobody else's business.
type Recorder struct {
	mu   sync.Mutex
	path string
	f    *os.File
	w    *bufio.Writer
	step int
}

// OpenRecording opens (or creates) the session's evidence file for
// appending and returns a Recorder. Steps number from 1.
func OpenRecording(m *Manager, sessionID string) (*Recorder, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("recording: session id required")
	}
	dir := filepath.Dir(RecordingPath(m, sessionID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(RecordingPath(m, sessionID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	// Steps continue across separate CLI invocations: count the lines already
	// there so the index stays stable over a multi-process session.
	step := 0
	if rf, err := os.Open(RecordingPath(m, sessionID)); err == nil {
		sc := bufio.NewScanner(rf)
		for sc.Scan() {
			step++
		}
		rf.Close()
	}
	return &Recorder{path: RecordingPath(m, sessionID), f: f, w: bufio.NewWriter(f), step: step}, nil
}

// Append writes one record and flushes it: a crash must not lose the last
// observation, because the last observation is often the interesting one.
func (r *Recorder) Append(rec Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.step++
	rec.Step = r.step
	if rec.TS == "" {
		rec.TS = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("recording: marshal: %w", err)
	}
	if _, err := r.w.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := r.w.Flush(); err != nil {
		return err
	}
	return r.f.Sync()
}

// RecordRequest marshals request as the request field.
func RecordRequest(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{"unmarshalable":true}`)
	}
	return b
}

// Close closes the underlying file. Safe to call twice.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	_ = r.w.Flush()
	err := r.f.Close()
	r.f = nil
	return err
}

// ReadRecords reads a session's evidence file back, whole, in order.
func ReadRecords(m *Manager, sessionID string) ([]Record, error) {
	f, err := os.Open(RecordingPath(m, sessionID))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return out, fmt.Errorf("recording line corrupt: %w", err)
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// RecordedAct performs one act and appends its record: the request as given,
// the observation the act returned, and the revision after the act. It is the
// evidence-bearing form of Manager.Act.
func RecordedAct(m *Manager, sessionID string, req ActRequest) (ActData, ObserveData, error) {
	data, err := m.Act(sessionID, req)
	if err != nil {
		appendOutcome(m, sessionID, "error", "act refused: "+err.Error())
		return data, ObserveData{}, err
	}
	if data.Stale {
		// A stale act sent nothing: there is no observation to record, only
		// the refusal. Record the honest outcome so the evidence file shows
		// the journey hit a wall.
		appendOutcome(m, sessionID, "error", CodeStaleRevision+": revision did not match; nothing was sent")
		return data, ObserveData{}, nil
	}
	obs, oerr := m.ObserveWithWait(sessionID, waitOf(req))
	rec := Record{Verb: "act", Request: RecordRequest(req), Revision: data.RevisionAfter}
	if oerr == nil {
		o := obs
		rec.Observation = &o
	}
	if err := appendRec(m, sessionID, rec); err != nil {
		return data, obs, err
	}
	if oerr != nil {
		return data, obs, oerr
	}
	return data, obs, nil
}

// RecordedObserve performs one observe and appends its record.
func RecordedObserve(m *Manager, sessionID string, wantDiff bool, prev string) (ObserveData, error) {
	obs, err := m.Observe(sessionID)
	if err != nil {
		appendOutcome(m, sessionID, "error", "observe refused: "+err.Error())
		return obs, err
	}
	if wantDiff {
		obs.Diff = diffOf(prev, obs.Snapshot)
	}
	rec := Record{Verb: "observe", Request: RecordRequest(map[string]any{"diff": wantDiff}), Revision: obs.Revision}
	o := obs
	rec.Observation = &o
	if err := appendRec(m, sessionID, rec); err != nil {
		return obs, err
	}
	return obs, nil
}

// RecordedOutcome appends the journey's terminal record. ok=false is an
// honest incomplete outcome: the journey failed and the recording says so.
func RecordedOutcome(m *Manager, sessionID string, ok bool, reason string) error {
	outcome := "ok"
	if !ok {
		outcome = "failed"
	}
	return appendOutcome(m, sessionID, outcome, reason)
}

func appendRec(m *Manager, sessionID string, rec Record) error {
	r, err := OpenRecording(m, sessionID)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Append(rec)
}

func appendOutcome(m *Manager, sessionID, outcome, reason string) error {
	return appendRec(m, sessionID, Record{Verb: "outcome", Outcome: outcome, Reason: reason})
}

// RecordOutcome appends a journey-terminal record with an explicit outcome
// ("ok", "failed" or "error") and the reason it ended that way.
func RecordOutcome(m *Manager, sessionID, outcome, reason string) error {
	return appendOutcome(m, sessionID, outcome, reason)
}

func waitOf(req ActRequest) ActWait {
	if req.Wait != nil {
		return *req.Wait
	}
	return ActWait{QuietMs: 300, TimeoutMs: 10000}
}

// diffOf is a line-oriented +/- diff between two snapshots, used to fill a
// recorded observation's diff the same way the CLI's --diff does.
func diffOf(old, cur string) string {
	oldL, curL := splitLines(old), splitLines(cur)
	var b []byte
	oi, ci := 0, 0
	for oi < len(oldL) || ci < len(curL) {
		switch {
		case oi < len(oldL) && ci < len(curL) && oldL[oi] == curL[ci]:
			oi++
			ci++
		case oi < len(oldL) && !containsLine(curL[ci:], oldL[oi]):
			b = append(b, '-')
			b = append(b, oldL[oi]...)
			b = append(b, '\n')
			oi++
		default:
			b = append(b, '+')
			b = append(b, curL[ci]...)
			b = append(b, '\n')
			ci++
		}
	}
	return string(b)
}

func containsLine(lines []string, s string) bool {
	for _, l := range lines {
		if l == s {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

package run

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Diff is what a worker reports after its runner moved a job from prev to
// cur: THE RUNNER'S WRITES, AS A REPORT. The runner writes a local copy of the
// job; Diff reads what changed there in the vocabulary [ApplyReport] takes, so
// the server's copy follows without the runner knowing it is remote. ok is
// false when nothing worth sending changed.
//
// Two things are never reported, because they are a person's on the server:
// the job's state (the server moves it on the person's verbs, and on done) and
// an approve step passing (only the person's answer passes it).
func Diff(prev, cur factory.Item) (factory.RunReport, bool) {
	ps, cs := streamOf(prev), streamOf(cur)
	var r factory.RunReport
	if pipelineMoved(prev, cur) {
		r.Pipeline = &factory.PipelineChange{Stages: cur.Stages, Phases: cs.Phases}
	}
	if !reflect.DeepEqual(prev.Change, cur.Change) {
		r.Change = cur.Change
	}
	if prev.ChangeBase != cur.ChangeBase {
		r.ChangeBase = cur.ChangeBase
	}
	r.Stages = stepsMoved(ps.Phases, cs.Phases)
	r.Log = newLines(ps.Log, cs.Log)
	if cur.State == factory.StateLanded || cur.State == factory.StateShipped {
		r.Log = unlanded(r.Log)
	}
	if spent := cs.Spent - ps.Spent; spent > 0 {
		r.Cost = spent
	}
	r.Claims = append(claimsMoved(prev.Proof, cur.Proof), claimsMoved(prev.Policy, cur.Policy)...)
	r.Question, r.QuestionKind = asked(prev, cur)
	if cur.PullRequest != nil && !reflect.DeepEqual(prev.PullRequest, cur.PullRequest) {
		pr := *cur.PullRequest
		r.PR = &pr
	}
	if cur.Publish != prev.Publish {
		r.Publish = cur.Publish
	}
	if cur.CommentURL != prev.CommentURL {
		r.CommentURL = cur.CommentURL
	}
	if strings.TrimSpace(cur.Talk) != strings.TrimSpace(prev.Talk) {
		r.Talk = cur.Talk
	}
	if len(cur.Notes) > len(prev.Notes) && reflect.DeepEqual(cur.Notes[:len(prev.Notes)], prev.Notes) {
		r.Notes = append([]string(nil), cur.Notes[len(prev.Notes):]...)
	}
	r.Log = without(r.Log, echoes(prev, r))
	return r, !reflect.DeepEqual(r, factory.RunReport{})
}

// echoes are the log lines the server writes itself when it applies r (a
// step's first line, its claims, the question), found by applying r to a copy
// of prev. The runner wrote the same lines here; sending them too would say
// each twice.
func echoes(prev factory.Item, r factory.RunReport) []factory.LogLine {
	raw, err := json.Marshal(prev)
	if err != nil {
		return nil
	}
	var sim factory.Item
	if json.Unmarshal(raw, &sim) != nil {
		return nil
	}
	sim.State = factory.StateRunning
	r.Log = nil
	if ApplyReport(&sim, r, time.Time{}) != nil {
		return nil
	}
	return newLines(streamOf(prev).Log, streamOf(sim).Log)
}

// without is lines less one line of the same tone and words for each echo.
func without(lines, echoes []factory.LogLine) []factory.LogLine {
	var kept []factory.LogLine
	for _, l := range lines {
		i := slices.IndexFunc(echoes, func(e factory.LogLine) bool { return e.Tone == l.Tone && e.Text == l.Text })
		if i >= 0 {
			echoes = slices.Delete(echoes, i, i+1)
			continue
		}
		kept = append(kept, l)
	}
	return kept
}

func streamOf(it factory.Item) factory.Stream {
	if it.Stream == nil {
		return factory.Stream{}
	}
	return *it.Stream
}

// pipelineMoved says the runner reshaped the job: its stages, or the names of
// its run's phases in order.
func pipelineMoved(prev, cur factory.Item) bool {
	if !reflect.DeepEqual(prev.Stages, cur.Stages) {
		return true
	}
	p, c := streamOf(prev).Phases, streamOf(cur).Phases
	if len(p) != len(c) {
		return true
	}
	for i := range p {
		if p[i].Name != c[i].Name {
			return true
		}
	}
	return false
}

// unlanded leaves the landing's own line out: done lands the job on the
// server ([FinishReport]), which says it there, and a line said twice is a lie
// about what happened.
func unlanded(lines []factory.LogLine) []factory.LogLine {
	var kept []factory.LogLine
	for _, l := range lines {
		if l.Text != sayLanded && l.Text != logShippedHabit {
			kept = append(kept, l)
		}
	}
	return kept
}

// stepsMoved is each phase whose state, round or result changed. An approve
// step is never reported: its question puts it waiting, and the person's
// answer on the server passes it or sends it back.
func stepsMoved(prev, cur []factory.Phase) []factory.StageReport {
	was := map[string]factory.Phase{}
	for _, ph := range prev {
		was[ph.Name] = ph
	}
	var out []factory.StageReport
	for _, ph := range cur {
		old, seen := was[ph.Name]
		if seen && old.State == ph.State && old.Round == ph.Round && reflect.DeepEqual(old.Result, ph.Result) {
			continue
		}
		if ph.Kind == factory.StageGate {
			continue
		}
		step := factory.StageReport{Name: ph.Name, State: ph.State, Round: ph.Round}
		if ph.Result != nil && (!seen || !reflect.DeepEqual(old.Result, ph.Result)) {
			res := *ph.Result
			res.Edit = nil
			step.Result = &res
		}
		out = append(out, step)
	}
	return out
}

// newLines is the log lines cur added after the last line prev had. The log
// is bounded, so the old tail is found by its last line rather than by count;
// a tail that scrolled away entirely sends the whole of cur.
func newLines(prev, cur []factory.LogLine) []factory.LogLine {
	if len(prev) == 0 {
		return append([]factory.LogLine(nil), cur...)
	}
	last := prev[len(prev)-1]
	for i := len(cur) - 1; i >= 0; i-- {
		if cur[i] == last {
			return append([]factory.LogLine(nil), cur[i+1:]...)
		}
	}
	return append([]factory.LogLine(nil), cur...)
}

// claimsMoved is each claim in cur that prev did not hold as it is now.
func claimsMoved(prev, cur []factory.Claim) []factory.Claim {
	var out []factory.Claim
	for _, cl := range cur {
		held := false
		for _, p := range prev {
			if p == cl {
				held = true
				break
			}
		}
		if !held {
			out = append(out, cl)
		}
	}
	return out
}

// asked is the question cur puts to the person, when it is a new one: the
// step it came from, its words and options, and its kind.
func asked(prev, cur factory.Item) (*factory.Asked, string) {
	if cur.State != factory.StateNeedsYou || strings.TrimSpace(cur.Question) == "" {
		return nil, ""
	}
	if prev.State == factory.StateNeedsYou && prev.Question == cur.Question {
		return nil, ""
	}
	q := factory.Asked{Question: cur.Question}
	if a := cur.Asking; a != nil {
		q.Stage, q.Options, q.Pick, q.Why = a.Stage, a.Options, a.Pick, a.Why
	} else if s := cur.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
		q.Stage = s.Phases[s.Cur].Name
	}
	return &q, cur.QKind
}

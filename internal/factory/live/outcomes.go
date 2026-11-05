package live

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// OutcomeKind routes durable job outcomes separately from live notifications.
const OutcomeKind = "job.outcome"

// OutcomeFile is the outcome record under the factory root.
const OutcomeFile = "job-outcomes.json"

// OutcomeLimit bounds the number of retained outcomes.
const OutcomeLimit = 2000

// Outcome is one finished, merged, failed or needs-you transition of a job.
type Outcome struct {
	Seq      int64     `json:"seq"`
	Kind     string    `json:"kind"`
	Job      int       `json:"job"`
	At       time.Time `json:"at"`
	Title    string    `json:"title,omitempty"`
	Group    string    `json:"group,omitempty"`
	How      string    `json:"how,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Need     string    `json:"need,omitempty"`
	Code     string    `json:"code,omitempty"`
	Step     string    `json:"step,omitempty"`
	PRNumber int       `json:"pr_number,omitempty"`
	PRURL    string    `json:"pr_url,omitempty"`
}

// OutcomeBasis retains the facts read by Outcomes in independent mutable storage.
// Unread payloads are omitted so large job details need no copy on every update.
func OutcomeBasis(it factory.Item) factory.Item {
	before := factory.Item{
		ID: it.ID, Title: it.Title, State: it.State,
		DoneHow: it.DoneHow, DoneAt: it.DoneAt, Publishing: it.Publishing, PublishError: it.PublishError,
		QKind: it.QKind, Question: it.Question, Created: it.Created, Changed: it.Changed,
		Failure: outcomeCopy(it.Failure), Asking: outcomeCopy(it.Asking),
		PersonStopped: outcomeCopy(it.PersonStopped), Merging: outcomeCopy(it.Merging),
	}
	if pr := it.PullRequest; pr != nil {
		before.PullRequest = &factory.PullRequest{
			State: pr.State, MergedAt: pr.MergedAt, Number: pr.Number, URL: pr.URL,
			Conflict: pr.Conflict, ReviewDecision: pr.ReviewDecision,
		}
	}
	if before.Asking != nil {
		before.Asking.Options = append([]string(nil), before.Asking.Options...)
	}
	if it.FollowUp != nil {
		before.FollowUp = &factory.PRFollowUp{State: it.FollowUp.State}
	}
	if it.PublishStatus != nil {
		before.PublishStatus = &factory.Publishing{State: it.PublishStatus.State}
	}
	if it.StopUndo != nil {
		// StoppedByPerson reads only whether the undo record exists.
		before.StopUndo = &factory.StopUndo{}
	}
	before.Stream = outcomeStream(it.Stream)
	return before
}

func outcomeCopy[T any](in *T) *T {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func outcomeStream(in *factory.Stream) *factory.Stream {
	if in == nil {
		return nil
	}
	out := &factory.Stream{Cur: in.Cur, Ended: in.Ended, Phases: make([]factory.Phase, len(in.Phases))}
	for i, ph := range in.Phases {
		// The phase clock supplies NeedsSince; its failure can preserve a stop.
		out.Phases[i] = factory.Phase{At: ph.At, State: ph.State, Failure: outcomeCopy(ph.Failure)}
	}
	return out
}

// Outcomes compares the durable job facts at an update, before its snapshot is visible.
func Outcomes(before, after factory.Item, now time.Time) []Event {
	var out []Event
	merged := after.PullRequest != nil && after.PullRequest.State == factory.PRMerged && (before.PullRequest == nil || before.PullRequest.State != factory.PRMerged)
	if merged {
		pr := after.PullRequest
		at := pr.MergedAt
		if at.IsZero() {
			at = after.DoneAt
		}
		out = append(out, newOutcome(after, Outcome{Kind: "merged", At: outcomeTime(at, now), PRNumber: pr.Number, PRURL: pr.URL}))
	}
	if factory.JobState(after) == "done" && factory.JobState(before) != "done" && !merged && factory.JobDoneHow(after) != "merged" {
		out = append(out, newOutcome(after, Outcome{Kind: "finished", At: outcomeTime(after.DoneAt, now), How: factory.JobDoneHow(after)}))
	}
	if f := after.Failure; f != nil && (before.Failure == nil || !before.Failure.At.Equal(f.At)) {
		out = append(out, newOutcome(after, Outcome{Kind: "failed", At: f.At, Code: f.Code, Step: f.Step}))
	}
	if needsOutcome(before, after) {
		out = append(out, newOutcome(after, Outcome{Kind: "needs_you", At: outcomeNeedsTime(after, now), Need: factory.NeedsKind(after), Reason: factory.JobReason(after)}))
	}
	return out
}

func needsOutcome(before, after factory.Item) bool {
	return factory.JobState(after) == "needs_you" && after.Failure == nil && (factory.JobState(before) != "needs_you" || factory.NeedsKind(before) != factory.NeedsKind(after) || !factory.NeedsSince(before).Equal(factory.NeedsSince(after)) || factory.JobReason(before) != factory.JobReason(after))
}

func outcomeNeedsTime(it factory.Item, now time.Time) time.Time {
	if factory.NeedsKind(it) != "" {
		return factory.NeedsSince(it)
	}
	if it.Asking != nil && it.Asking.With == factory.AskedYou {
		return it.Asking.At
	}
	return now
}

func outcomeTime(at, fallback time.Time) time.Time {
	if at.IsZero() {
		return fallback
	}
	return at
}

func newOutcome(it factory.Item, out Outcome) Event {
	out.Job, out.Title, out.Group = it.ID, it.Title, it.Group
	if len(out.Title) > 200 {
		end := 200
		for !utf8.RuneStart(out.Title[end]) {
			end--
		}
		out.Title = out.Title[:end]
	}
	// The sequence belongs to the record, so it is absent until commit.
	return New(OutcomeKind, it.ID, struct {
		Outcome
		Seq *int64 `json:"seq,omitempty"`
	}{Outcome: out})
}

type outcomeDocument struct {
	Next   int64     `json:"next"`
	Floor  int64     `json:"floor"`
	Events []Outcome `json:"events"`
}

// OutcomePage carries a bounded read and the Factory cursor to continue from.
type OutcomePage struct {
	Events []Outcome
	Cursor int64
	Reset  bool
	More   bool
}

// ReadOutcomes holds the shared replay lock while reading both durable records.
// An absent cursor resets once; zero asks for the beginning of the record.
func ReadOutcomes(root string, after *int64, limit int) (OutcomePage, error) {
	out := OutcomePage{Events: []Outcome{}, Reset: true}
	if root == "" {
		return out, nil
	}
	err := locked(root, false, func(d *document) error {
		record, _, err := loadOutcomes(root)
		if err != nil {
			return err
		}
		out.Cursor = record.Next
		if after == nil || d.OutcomesThrough != d.Next || *after < record.Floor || *after > record.Next {
			return nil
		}
		out = record.read(*after, limit)
		return nil
	})
	return out, err
}

func (d *outcomeDocument) read(after int64, limit int) OutcomePage {
	out := OutcomePage{Events: []Outcome{}, Cursor: d.Next}
	if limit < 1 {
		limit = 100
	}
	for _, e := range d.Events {
		if e.Seq <= after || e.Seq > d.Next {
			continue
		}
		if len(out.Events) == limit {
			out.More = true
			out.Cursor = out.Events[len(out.Events)-1].Seq
			break
		}
		out.Events = append(out.Events, e)
	}
	return out
}

func loadOutcomes(root string) (outcomeDocument, bool, error) {
	d := outcomeDocument{Events: []Outcome{}}
	data, err := os.ReadFile(filepath.Join(root, OutcomeFile))
	if errors.Is(err, os.ErrNotExist) {
		return d, false, nil
	}
	if err != nil {
		return d, false, err
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return d, true, fmt.Errorf("factory outcome record: %w", err)
	}
	return d, true, nil
}

func saveOutcomes(root string, d *outcomeDocument) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".job-outcomes.*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(root, OutcomeFile))
}

type outcomeCommit struct {
	before  outcomeDocument
	after   outcomeDocument
	existed bool
	changed bool
}

func prepareOutcomes(root string, d *document, events []Event) (*outcomeCommit, []Event, error) {
	record, existed, err := loadOutcomes(root)
	if err != nil {
		return nil, nil, err
	}
	c := &outcomeCommit{before: record, after: record, existed: existed}
	c.after.Events = make([]Outcome, 0, len(record.Events))
	for _, e := range record.Events {
		if e.Seq <= record.Next {
			c.after.Events = append(c.after.Events, e)
		} else {
			c.changed = true
		}
	}
	if d.OutcomesThrough != d.Next {
		c.after.Next++
		c.after.Floor = c.after.Next
		c.changed = true
	}
	var notifications []Event
	for _, e := range events {
		if e.Kind != OutcomeKind {
			notifications = append(notifications, e)
			continue
		}
		var out Outcome
		if err := json.Unmarshal(e.Data, &out); err != nil {
			return nil, nil, fmt.Errorf("factory outcome: %w", err)
		}
		c.after.Next++
		out.Seq = c.after.Next
		c.after.Events = append(c.after.Events, out)
		c.changed = true
	}
	c.trim()
	return c, notifications, nil
}

func (c *outcomeCommit) trim() {
	if n := len(c.after.Events) - OutcomeLimit; n > 0 {
		c.after.Floor = max(c.after.Floor, c.after.Events[n-1].Seq)
		c.after.Events = c.after.Events[n:]
	}
}

func (c *outcomeCommit) save(root string) error {
	if !c.changed {
		return nil
	}
	return saveOutcomes(root, &c.after)
}

func (c *outcomeCommit) restore(root string) error {
	if !c.changed {
		return nil
	}
	if c.existed {
		return saveOutcomes(root, &c.before)
	}
	err := os.Remove(filepath.Join(root, OutcomeFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func forgetOutcomes(root string, ids map[int]bool) error {
	d, existed, err := loadOutcomes(root)
	if err != nil || !existed {
		return err
	}
	kept := d.Events[:0]
	for _, e := range d.Events {
		if !ids[e.Job] {
			kept = append(kept, e)
		}
	}
	d.Events = kept
	return saveOutcomes(root, &d)
}

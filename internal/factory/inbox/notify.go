package inbox

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// Replier is a feed that can answer back on its own system: a comment with
// the pull request's link, a note that the work merged. It is optional; a feed
// that only reads is simply not a Replier.
type Replier interface {
	Reply(ctx context.Context, externalID string, ev Event) error
}

// EventKind is a moment in a job's life worth telling the source about.
type EventKind string

const (
	EventPROpened EventKind = "pr_opened"
	EventMerged   EventKind = "merged"
	EventStuck    EventKind = "stuck" // the job stopped and needs a person
)

// Event is one such moment, already in a person's words.
type Event struct {
	Kind  EventKind `json:"kind"`
	JobID int       `json:"job_id"`
	URL   string    `json:"url,omitempty"` // the pull request
	Text  string    `json:"text"`
}

// replyWait is the most one answer back may take before it is given up.
const replyWait = 30 * time.Second

var repliers = struct {
	sync.RWMutex
	by map[string]Replier
}{by: map[string]Replier{}}

// RegisterReplier makes r the answerer for every job whose source is name. A
// nil r takes the answerer away again (a connector disconnected).
func RegisterReplier(name string, r Replier) {
	repliers.Lock()
	defer repliers.Unlock()
	if r == nil {
		delete(repliers.by, name)
		return
	}
	repliers.by[name] = r
}

func replierFor(name string) Replier {
	repliers.RLock()
	defer repliers.RUnlock()
	return repliers.by[name]
}

// replies counts the answers still on their way, so a test can wait for them.
var replies sync.WaitGroup

// Notify tells the job's source about ev when that source registered a
// [Replier].
//
// IT NEVER FAILS THE JOB AND NEVER HOLDS IT UP. It returns at once: the job is
// read and the answer sent on a goroutine of its own, with its own deadline
// and detached from ctx's cancellation (a report's request ending must not
// cut the comment short). A failure is logged, redacted, and forgotten; the
// job has already moved on, and the outside system is told on the next
// moment instead.
func Notify(ctx context.Context, st *store.Store, jobID int, ev Event) {
	if st == nil || jobID <= 0 {
		return
	}
	ev.JobID = jobID
	ctx = context.WithoutCancel(ctx)
	replies.Add(1)
	go func() {
		defer replies.Done()
		it, err := st.GetSaved(jobID)
		if err != nil {
			return
		}
		source, id, ok := Split(it)
		if !ok {
			return
		}
		r := replierFor(source)
		if r == nil {
			return
		}
		ctx, cancel := context.WithTimeout(ctx, replyWait)
		defer cancel()
		if err := r.Reply(ctx, id, ev); err != nil {
			logf("factory: telling %s about job %d failed: %s", source, jobID, redact.Secrets(err.Error()))
		}
	}()
}

// Mark is what [Moments] compares: the few facts of a job, taken before a
// write, whose change is worth telling its source about.
type Mark struct {
	sourced bool
	state   factory.State
	pr      string
	merged  bool
}

// MarkOf takes it's mark. Call it before the write; the pull request is read
// by value, so a write that changes it in place still shows.
func MarkOf(it factory.Item) Mark {
	m := Mark{sourced: it.SourceRef != "", state: it.State}
	if it.PullRequest != nil {
		m.pr, m.merged = it.PullRequest.URL, it.PullRequest.State == factory.PRMerged
	}
	return m
}

// Moments is what happened to a job between its mark and after: its pull
// request opened, it merged, or it stopped for a person. A JOB THE INBOX DID
// NOT FILE HAS NO MOMENTS, so the lifecycle pays nothing for a GitHub issue.
func (m Mark) Moments(after factory.Item) []Event {
	if !m.sourced || after.SourceRef == "" {
		return nil
	}
	var out []Event
	if pr := after.PullRequest; pr != nil && pr.URL != "" {
		if m.pr == "" {
			out = append(out, Opened(pr.URL))
		}
		if !m.merged && pr.State == factory.PRMerged {
			out = append(out, Merged(pr.URL))
		}
	}
	if m.state != factory.StateNeedsYou && after.State == factory.StateNeedsYou {
		out = append(out, Stuck(after.Question))
	}
	return out
}

// Tell sends each of events for job id through [Notify].
func Tell(ctx context.Context, st *store.Store, id int, events []Event) {
	for _, ev := range events {
		Notify(ctx, st, id, ev)
	}
}

// Opened, Merged and Stuck are the three moments in a person's words.
func Opened(url string) Event {
	return Event{Kind: EventPROpened, URL: url, Text: "codeaf opened a pull request for this: " + url}
}

func Merged(url string) Event {
	return Event{Kind: EventMerged, URL: url, Text: "The pull request for this merged: " + url}
}

func Stuck(question string) Event {
	text := "codeaf stopped on this and needs a person to look."
	if question != "" {
		text += " " + question
	}
	return Event{Kind: EventStuck, Text: text}
}

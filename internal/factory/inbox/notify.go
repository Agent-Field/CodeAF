package inbox

import (
	"context"
	"strings"
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

// A REPLIER IGNORES A KIND IT DOES NOT KNOW. The list grows (wave 2 added
// filed, started, question, done and message for the Trigger API),
// and a connector written against an older list must stay quiet about the
// new moments rather than misread them.
const (
	EventFiled    EventKind = "filed"    // the work became a job
	EventStarted  EventKind = "started"  // its first step began
	EventQuestion EventKind = "question" // it stopped on a question for a person
	EventPROpened EventKind = "pr_opened"
	EventMerged   EventKind = "merged"
	EventStuck    EventKind = "stuck" // the job stopped and needs a person
	EventDone     EventKind = "done"  // the job is finished
	// EventMessage is the job's manager answering a message the source sent
	// ([Message]); Text is what the manager made of it.
	EventMessage EventKind = "message"
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

// Sink is one place the lifecycle's moments go: an AgentField control plane,
// and later ones of its kind. THE LIFECYCLE NEVER LEARNS WHICH: it
// calls [Notify], and every sink registered here is handed each moment of a
// job the inbox filed, with the job's source and its key there. The
// per-source repliers ([RegisterReplier], Jira's comments) are the first sink
// and need no registration.
//
// A sink is handed the moment on a goroutine of its own, under its own
// deadline, after the job's write is kept; it must not block for long (queue
// and return) and its failure is logged and never fails the job.
type Sink interface {
	Send(ctx context.Context, m Moment) error
}

// Moment is one event of one job, addressed as its source knows it.
type Moment struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Event
}

var sinks = struct {
	sync.RWMutex
	by map[string]Sink
}{by: map[string]Sink{}}

// RegisterSink makes s receive every moment, under name ("agentfield"). A second
// registration under the same name replaces the first; a nil s takes it away
// again.
func RegisterSink(name string, s Sink) {
	sinks.Lock()
	defer sinks.Unlock()
	if s == nil {
		delete(sinks.by, name)
		return
	}
	sinks.by[name] = s
}

// targets is every sink a moment from source goes to, the source's own
// replier first.
func targets(source string) map[string]Sink {
	out := map[string]Sink{}
	if r := replierFor(source); r != nil {
		out["replier:"+source] = replierSink{r}
	}
	sinks.RLock()
	defer sinks.RUnlock()
	for name, s := range sinks.by {
		out[name] = s
	}
	return out
}

// replierSink is a [Replier] seen as a sink.
type replierSink struct{ r Replier }

func (s replierSink) Send(ctx context.Context, m Moment) error {
	return s.r.Reply(ctx, m.ExternalID, m.Event)
}

// replies counts the answers still on their way, so a test can wait for them.
var replies sync.WaitGroup

// Notify hands ev, a moment of job jobID, to every sink: the job's source's
// [Replier] and each [RegisterSink]. A job the inbox did not file goes nowhere.
//
// IT NEVER FAILS THE JOB AND NEVER HOLDS IT UP. It returns at once: the job is
// read and each sink handed the moment on a goroutine of its own, with its
// own deadline and detached from ctx's cancellation (a report's request
// ending must not cut the answer short). A failure is logged, redacted, and
// forgotten; the job has already moved on.
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
		m := Moment{Source: source, ExternalID: id, Event: ev}
		for name, s := range targets(source) {
			replies.Add(1)
			go func() {
				defer replies.Done()
				ctx, cancel := context.WithTimeout(ctx, replyWait)
				defer cancel()
				if err := s.Send(ctx, m); err != nil {
					logf("factory: telling %s about job %d failed: %s", name, jobID, redact.Secrets(err.Error()))
				}
			}()
		}
	}()
}

// WaitSent waits until every moment handed to a sink so far has been sent or
// given up. Tests use it; the lifecycle never waits.
func WaitSent() { replies.Wait() }

// Mark is what [Moments] compares: the few facts of a job, taken before a
// write, whose change is worth telling its source about.
type Mark struct {
	sourced bool
	state   factory.State
	pr      string
	merged  bool
	ran     bool
	done    bool
}

// MarkOf takes it's mark. Call it before the write; the pull request is read
// by value, so a write that changes it in place still shows.
func MarkOf(it factory.Item) Mark {
	m := Mark{sourced: it.SourceRef != "", state: it.State, ran: it.Stream != nil, done: factory.JobState(it) == "done"}
	if it.PullRequest != nil {
		m.pr, m.merged = it.PullRequest.URL, it.PullRequest.State == factory.PRMerged
	}
	return m
}

// Moments is what happened to a job between its mark and after: its first
// step began, its pull request opened, it merged, it stopped on a question or
// for a person, or it finished. A JOB THE INBOX DID NOT FILE HAS NO MOMENTS,
// so the lifecycle pays nothing for a GitHub issue.
//
// A stop with a question is [EventQuestion] and one without is [EventStuck],
// never both: a source is told the one thing a person is waited on for.
func (m Mark) Moments(after factory.Item) []Event {
	if !m.sourced || after.SourceRef == "" {
		return nil
	}
	var out []Event
	if !m.ran && after.Stream != nil {
		out = append(out, Started())
	}
	merged := false
	if pr := after.PullRequest; pr != nil && pr.URL != "" {
		if m.pr == "" {
			out = append(out, Opened(pr.URL))
		}
		if !m.merged && pr.State == factory.PRMerged {
			out = append(out, Merged(pr.URL))
			merged = true
		}
	}
	if m.state != factory.StateNeedsYou && after.State == factory.StateNeedsYou {
		if q := strings.TrimSpace(after.Question); q != "" {
			out = append(out, Question(q))
		} else {
			out = append(out, Stuck(""))
		}
	}
	// A MERGE IS ALREADY THE JOB FINISHING, and the PR follower tells of a
	// merge alone, so a source hears "done" for it from its answerer
	// (trigger.Replier), never twice from here.
	if !m.done && !merged && factory.JobState(after) == "done" {
		out = append(out, Done(factory.JobDoneHow(after)))
	}
	return out
}

// Tell sends each of events for job id through [Notify].
func Tell(ctx context.Context, st *store.Store, id int, events []Event) {
	for _, ev := range events {
		Notify(ctx, st, id, ev)
	}
}

// Filed, Started, Question, Opened, Merged, Stuck and Done are the moments in
// a person's words.
func Filed(title string) Event {
	return Event{Kind: EventFiled, Text: "codeaf filed this as a job: " + title}
}

func Started() Event {
	return Event{Kind: EventStarted, Text: "codeaf started work on this."}
}

func Question(question string) Event {
	return Event{Kind: EventQuestion, Text: "codeaf has a question before it goes on: " + question}
}

func Done(how string) Event {
	text := "codeaf finished this."
	if how != "" {
		text = "codeaf finished this (" + how + ")."
	}
	return Event{Kind: EventDone, Text: text}
}

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

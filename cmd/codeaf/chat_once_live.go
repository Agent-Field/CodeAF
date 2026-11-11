package main

// ── codeaf chat --once, WATCHED AND STEERED FROM ANOTHER MACHINE ────────────
//
// An AgentField node runs a WorkAF step as one headless chat turn — the same
// session engine a factory step runs on this machine — and three flags let it
// be watched and steered while it works, so the step reads in WorkAF exactly
// as a native step does:
//
//   - --events PATH: every line of this conversation's own record
//     (transcript.jsonl) is copied to PATH, VERBATIM, as it is written. A
//     reader that writes those lines into a conversation folder of its own
//     has the same conversation, read by the same code, as this one.
//   - --steer PATH: each complete line appended to PATH (plain text or
//     {"text": "…"}) is put into the running turn with [session.Agent.Steer],
//     the same door the chat's own composer steers through, so it lands in the
//     record as the person's steer line. A note that arrives after the turn
//     has ended starts the turn it asks for, which this run waits out.
//   - --json: print one machine object at the end instead of the reply:
//     {ok, stop, answer, model, error} — the shape `codeaf exec --json` prints,
//     so a harness reads either door the same way.
//
// None of them changes a run that does not pass it.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// onceLive is the three flags, set by openChatV3 for runChatV3Once.
type onceLive struct {
	events string
	steer  string
	json   bool
}

var chatOnceLive onceLive

// copyTail copies every complete line appended to src into dst, from the
// start of src, until stop is closed; then it copies what is left once more.
func copyTail(src, dst string, stop <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer guard.Recover("chat-once/events")
		defer close(done)
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		defer out.Close()
		var offset int64
		var partial []byte
		step := func(final bool) {
			f, err := os.Open(src)
			if err != nil {
				return
			}
			defer f.Close()
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				return
			}
			raw, _ := io.ReadAll(f)
			offset += int64(len(raw))
			buf := append(partial, raw...)
			if cut := bytes.LastIndexByte(buf, '\n'); cut >= 0 {
				_, _ = out.Write(buf[:cut+1])
				buf = buf[cut+1:]
			}
			partial = append([]byte(nil), buf...)
			if final && len(bytes.TrimSpace(partial)) > 0 {
				_, _ = out.Write(append(partial, '\n'))
				partial = nil
			}
		}
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				step(true)
				return
			case <-t.C:
				step(false)
			}
		}
	}()
	return done
}

// steerTail feeds each note appended to path into agent's running turn. A
// note's own stream is drained into sink (the reply) on wg, so the run does
// not end under a turn the note started. The reader stops when stop closes;
// last then reads path until no note is waiting ([startOnceLive]).
func steerTail(agent *session.Agent, path string, asker *onceAsker, sink io.Writer, stderr io.Writer, wg *sync.WaitGroup, stop <-chan struct{}) (done <-chan struct{}, last func()) {
	reader := newSteerFile(path)
	feed := func(notes []string) {
		for _, note := range notes {
			if asker.take(note) {
				fmt.Fprintln(stderr, "answered a question")
				continue
			}
			events, err := agent.Steer(note)
			if err != nil {
				// No turn in flight: the note becomes the next message.
				events, err = agent.Submit(context.Background(), note)
			}
			if err != nil {
				fmt.Fprintln(stderr, "steer: "+err.Error())
				continue
			}
			fmt.Fprintln(stderr, "steered: "+note)
			wg.Add(1)
			go func() {
				defer guard.Recover("chat-once/steer-drain")
				defer wg.Done()
				_ = drainOnceEvents(events, sink, stderr)
			}()
		}
	}
	stopped := make(chan struct{})
	go func() {
		defer guard.Recover("chat-once/steer")
		defer close(stopped)
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
			}
			feed(reader.Read())
		}
	}()
	// A NOTE THAT LANDS AS THE RUN ENDS IS STILL TAKEN: written after the
	// reader's last look, it would otherwise be read by nobody. Each note
	// found gets its turn, waited out, and the file is read again until it
	// holds nothing new.
	last = func() {
		for {
			notes := reader.Read()
			if len(notes) == 0 {
				return
			}
			feed(notes)
			wg.Wait()
		}
	}
	return stopped, last
}

// startOnceLive starts --events' copy and --steer's reader for one run.
// finishSteer waits out notes in flight, stops the reader, then reads the
// steer file once more and gives any note waiting there its turn; it is
// called while the agent is still open. finishEvents makes the last copy and
// is called only AFTER the agent and the usage ledger are closed, because
// closing still writes lines to the record. Either is a no-op for a flag the
// run did not pass.
func startOnceLive(agent *session.Agent, record string, live onceLive, asker *onceAsker, reply, stderr io.Writer, steers *sync.WaitGroup) (finishSteer, finishEvents func()) {
	finishSteer, finishEvents = func() {}, func() {}
	if live.events != "" {
		stop := make(chan struct{})
		tailed := copyTail(record, live.events, stop)
		finishEvents = func() {
			close(stop)
			<-tailed
		}
	}
	if live.steer != "" {
		stop := make(chan struct{})
		stopped, last := steerTail(agent, live.steer, asker, reply, stderr, steers, stop)
		finishSteer = func() {
			steers.Wait()
			close(stop)
			<-stopped
			last()
		}
	}
	return finishSteer, finishEvents
}

// lockedWriter is a reply buffer two drains may write at once.
type lockedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// onceEnvelope is --json's one object.
type onceEnvelope struct {
	OK     bool   `json:"ok"`
	Stop   string `json:"stop"`
	Answer string `json:"answer,omitempty"`
	Model  string `json:"model,omitempty"`
	Error  string `json:"error,omitempty"`
}

func writeOnceEnvelope(w io.Writer, answer, model string, failure error) {
	env := onceEnvelope{OK: failure == nil, Stop: "done", Answer: strings.TrimSpace(answer), Model: model}
	if failure != nil {
		env.Stop, env.Error = "error", failure.Error()
	}
	raw, _ := json.Marshal(env)
	_, _ = w.Write(append(raw, '\n'))
}

// ── A QUESTION ASKED WHERE NOBODY IS AT THE KEYBOARD, ANSWERED FROM WORKAF ──
//
// A step an AgentField node runs for a WorkAF factory is `chat --once` with
// --events and --steer, under --yolo, so until now its `ask` took its own
// default or came back "your call (nobody to ask)" — while the person who
// could answer was watching the step in WorkAF. With both flags the run is
// live and steerable, so the question goes to them instead:
//
//   - it is written on --events as ONE LINE of its own, beside the record's
//     lines: {"type":"ask","id":"ask-<n>","question":"…","options":[…],
//     "pick":"…","timestamp":"RFC3339"};
//   - the call WAITS for a steer note that is the answer to it, the JSON
//     {"answer":"ask-<n>","text":"…","by":"manager"|"person"} (the SDK wraps
//     a steer as {"text": <that JSON>}; [steerFile] unwraps it), and the model
//     reads "the person answered: …";
//   - when it is answered, or the run ends first, {"type":"ask_done",
//     "id":"ask-<n>"} follows, so whoever drew the question takes it down.
//
// WorkAF sends the question to the job's manager first and then to the
// person, as it does a step of its own (factory_agentfield_run.go). Without
// both flags `ask` is what it always was here.

// onceAsker is [session.StageAsker] for a live, steerable `chat --once`.
type onceAsker struct {
	events  string
	mu      sync.Mutex
	seq     int
	waiting map[string]chan onceAnswer
}

// onceAnswer is one answer note: the words, and who gave them.
type onceAnswer struct {
	Answer string `json:"answer"`
	Text   string `json:"text"`
	By     string `json:"by,omitempty"`
}

// onceAskLine is the question's line on --events, and the line that takes it
// down (Type ask_done, with only its ID).
type onceAskLine struct {
	Type     string   `json:"type"`
	ID       string   `json:"id"`
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
	Pick     string   `json:"pick,omitempty"`
	// Person is the step's mark that only a person can answer the question
	// ([factory.Asked.Person]): the factory then never gives it to the
	// job's manager.
	Person    bool   `json:"person,omitempty"`
	Timestamp string `json:"timestamp"`
}

func newOnceAsker(events string) *onceAsker {
	return &onceAsker{events: events, waiting: map[string]chan onceAnswer{}}
}

// Ask writes the question on --events and waits for its answer note, or for
// ctx (the turn's stop, the run's end).
func (o *onceAsker) Ask(ctx context.Context, q factory.Asked) (string, error) {
	id, ch := o.open()
	defer func() {
		o.close(id)
		_ = o.write(onceAskLine{Type: "ask_done", ID: id})
	}()
	if err := o.write(onceAskLine{Type: "ask", ID: id, Question: q.Question, Options: q.Options, Pick: q.Pick, Person: q.Person}); err != nil {
		// Nobody can be told, so nobody is asked: `ask` goes its own way.
		return "", err
	}
	select {
	case a := <-ch:
		words := strings.TrimSpace(a.Text)
		if a.By == "manager" {
			return "the manager answered: " + words, nil
		}
		return "the person answered: " + words, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// open registers one question waiting for its answer.
func (o *onceAsker) open() (string, chan onceAnswer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	id := fmt.Sprintf("ask-%d", o.seq)
	ch := make(chan onceAnswer, 1)
	o.waiting[id] = ch
	return id, ch
}

// close forgets a question that was answered or withdrawn.
func (o *onceAsker) close(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.waiting, id)
}

// waitingFor is the question id's answer channel, while it waits.
func (o *onceAsker) waitingFor(id string) (chan onceAnswer, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	ch, ok := o.waiting[id]
	return ch, ok
}

// take hands note to the question it answers and says whether it did; a note
// that answers no waiting question is an ordinary steer.
func (o *onceAsker) take(note string) bool {
	if o == nil || !strings.HasPrefix(strings.TrimSpace(note), "{") {
		return false
	}
	var a onceAnswer
	if json.Unmarshal([]byte(note), &a) != nil || a.Answer == "" {
		return false
	}
	ch, ok := o.waitingFor(a.Answer)
	if !ok {
		// An answer to a question that was already answered or withdrawn is
		// no steer either: its words were meant for a question, not the turn.
		return true
	}
	select {
	case ch <- a:
	default:
	}
	return true
}

// write appends one line to --events in ONE write, so it never lands inside
// a line [copyTail] is copying (both append).
func (o *onceAsker) write(line onceAskLine) error {
	line.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(line)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(o.events, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(raw, '\n'))
	return err
}

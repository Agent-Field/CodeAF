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
//     {ok, stop, answer, model, error, tokens, spend_usd} — the shape `codeaf exec --json` prints,
//     so a harness reads either door the same way.
//
// None of them changes a run that does not pass it.
//
// THIS RELEASE LINE CARRIES THE THREE FLAGS AND NOTHING THE FACTORY ENGINE
// BUILT AROUND THEM. A question the model asks during such a run is answered
// the way any headless run here answers one (its own default, or "nobody to
// ask"); relaying it to a watcher over --events is the engine's, on dev.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

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
func steerTail(agent *session.Agent, path string, sink io.Writer, stderr io.Writer, wg *sync.WaitGroup, stop <-chan struct{}) (done <-chan struct{}, last func()) {
	reader := newSteerFile(path)
	feed := func(notes []string) {
		for _, note := range notes {
			events, err := agent.Steer(note)
			if err == nil {
				events = steerOwnTurn(events)
			} else {
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

// steerOwnTurn is the part of a steer's stream that is not already being
// printed. [session.Agent.Steer] hands back a LIVE VIEW OF THE TURN IT STEERS,
// and that turn is drained into the reply by runChatV3Once (the first turn, or
// a woken one), so printing the view too wrote every word of the answer twice:
// a watched run that was steered once returned "the answer.the answer." as its
// answer. Nothing is passed on until the steer falls through; from
// [session.EventSteerFellThrough] on, the same channel carries the turn the
// note starts by itself, which nothing else reads, so all of that is passed on.
// A note sent with no turn in flight goes through Submit, whose stream is
// already its own turn, and never comes here.
func steerOwnTurn(events <-chan session.Event) <-chan session.Event {
	own := make(chan session.Event)
	go func() {
		defer guard.Recover("chat-once/steer-own-turn")
		defer close(own)
		fell := false
		for event := range events {
			if !fell {
				fell = event.Kind == session.EventSteerFellThrough
				continue
			}
			own <- event
		}
	}()
	return own
}

// startOnceLive starts --events' copy and --steer's reader for one run.
// finishSteer waits out notes in flight, stops the reader, then reads the
// steer file once more and gives any note waiting there its turn; it is
// called while the agent is still open. finishEvents makes the last copy and
// is called only AFTER the agent and the usage ledger are closed, because
// closing still writes lines to the record. Either is a no-op for a flag the
// run did not pass.
func startOnceLive(agent *session.Agent, record string, live onceLive, reply, stderr io.Writer, steers *sync.WaitGroup) (finishSteer, finishEvents func()) {
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
		stopped, last := steerTail(agent, live.steer, reply, stderr, steers, stop)
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
	envelopeUsage
}

func writeOnceEnvelope(w io.Writer, answer, model string, usage envelopeUsage, failure error) {
	env := onceEnvelope{OK: failure == nil, Stop: "done", Answer: strings.TrimSpace(answer), Model: model, envelopeUsage: usage}
	if failure != nil {
		env.Stop, env.Error = "error", failure.Error()
	}
	raw, _ := json.Marshal(env)
	_, _ = w.Write(append(raw, '\n'))
}

// steerFile reads the notes appended to one file since the last read. It is
// polled, so it never blocks and holds no goroutine: a read that finds nothing
// new returns nothing.
type steerFile struct {
	path    string
	mu      sync.Mutex
	offset  int64
	partial string
}

func newSteerFile(path string) *steerFile { return &steerFile{path: path} }

// Read returns every complete note appended since the last call.
func (s *steerFile) Read() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer file.Close()
	if _, err := file.Seek(s.offset, io.SeekStart); err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil || len(raw) == 0 {
		return nil
	}
	s.offset += int64(len(raw))
	text := s.partial + string(raw)
	// Only complete lines are notes; a writer caught mid-line is finished on
	// the next read rather than delivered as half a sentence.
	cut := strings.LastIndexByte(text, '\n')
	if cut < 0 {
		s.partial = text
		return nil
	}
	s.partial = text[cut+1:]
	var notes []string
	scanner := bufio.NewScanner(strings.NewReader(text[:cut]))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		if note := steerLine(scanner.Text()); note != "" {
			notes = append(notes, note)
		}
	}
	return notes
}

// steerLine is one line's note: {"text": "…"} or the line itself.
func steerLine(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "{") {
		var obj struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil {
			return strings.TrimSpace(obj.Text)
		}
	}
	return line
}

// envelopeUsage is the accounting half of `codeaf exec --json`'s envelope,
// spelled the same so a harness reads either door the same way.
type envelopeUsage struct {
	// SpendUSD is what this run cost, whole, in dollars.
	SpendUSD float64 `json:"spend_usd"`
	// Tokens is what it cost in tokens, prompt and completion.
	Tokens envelopeTokens `json:"tokens"`
}

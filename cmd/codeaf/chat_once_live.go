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
// not end under a turn the note started.
func steerTail(agent *session.Agent, path string, sink io.Writer, stderr io.Writer, wg *sync.WaitGroup, stop <-chan struct{}) {
	reader := newSteerFile(path)
	go func() {
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
			}
			for _, note := range reader.Read() {
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
					defer wg.Done()
					_ = drainOnceEvents(events, sink, stderr)
				}()
			}
		}
	}()
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

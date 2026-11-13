package cellstore

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// TestW3NextSealRecoversAfterAnInterruptedAnswer proves B2 at the wire and
// recorder boundaries: do not replay a lost answer; re-dial on the next seal,
// carry the pending calls, and say the existing recovery sentence once.
func TestW3NextSealRecoversAfterAnInterruptedAnswer(t *testing.T) {
	socket := testSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var plans atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			line, err := bufio.NewReader(conn).ReadBytes('\n')
			var request wireRequest
			if err != nil || json.Unmarshal(line, &request) != nil {
				conn.Close()
				continue
			}
			var ok string
			switch request.Verb {
			case "capture-plan":
				if plans.Add(1) == 1 {
					conn.Close()
					continue
				}
				ok = `{"token":"reconnected","full":true,"paths":[]}`
			case "seal":
				ok = `{"snapshot":"recovered"}`
			case "sync":
				ok = `{"durable":true}`
			}
			raw, _ := json.Marshal(wireResponse{ID: request.ID, Ok: json.RawMessage(ok)})
			_, _ = conn.Write(append(raw, '\n'))
			conn.Close()
		}
	}()
	c := newCell(t)
	e := realEngine(t)
	fallback := &fixedTransport{out: `{"snapshot":"must-not-replay"}`}
	e.Transport = Fallback{Primary: Daemon{Socket: socket}, Secondary: fallback}
	watch := &SealWatch{}
	recorder, err := NewRecorder(&stubExec{}, e, c, e.WALPath(c), Options{Report: watch.Report})
	if err != nil {
		t.Fatal(err)
	}
	call := func() {
		t.Helper()
		if err := recorder.Around(t.Context(), executor.Call{Tool: "read"}, executor.EffectLocal, AgentRun, func() ([]byte, bool) { return []byte("ok"), false }); err != nil {
			t.Fatal(err)
		}
	}
	call()
	if !watch.Failing() || plans.Load() != 1 || fallback.calls != 0 {
		t.Fatal("interrupted answer was hidden or replayed")
	}
	if notice := watch.Take(); !strings.Contains(notice, "answer interrupted") {
		t.Fatalf("lost answer was not reported: %q", notice)
	}
	call()
	head, err := Head(c)
	if err != nil || head == nil || len(head.Receipt.Calls) != 2 || watch.Failing() || fallback.calls != 0 {
		t.Fatalf("next seal did not recover pending calls: head=%+v err=%v", head, err)
	}
	if notice := watch.Take(); notice != "sealing works again, and the calls made meanwhile are sealed now" || watch.Take() != "" {
		t.Fatalf("recovery notice = %q", notice)
	}
}

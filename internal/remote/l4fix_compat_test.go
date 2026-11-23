package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// L4-R4: A legacy surface receives only a successful legacy reply after a local close with an upload failure.
func TestL4FixLegacySurfaceSaving(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "upload", true: "interrupt"}[interrupt], func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			l := dialAgent(t, &Engine{Agent: &fakeAgent{}, SessionFile: "/chat/transcript.jsonl", FinishSaving: func(WrappedAgent) error {
				close(entered)
				<-release
				return syncwords.UploadFailure(context.DeadlineExceeded)
			}})
			hello := l.hello(Hello{Version: Version})
			var welcome map[string]any
			if err := json.Unmarshal(hello.Payload, &welcome); err != nil {
				t.Fatal(err)
			}
			if welcome["localSavingReceipts"] == true {
				t.Fatal("unrequested capability was enabled")
			}
			l.write(Frame{Kind: "call", ID: 1, Method: MethodClose})
			<-entered
			// Interrupting an older surface restores the terminal while the engine still owns this upload.
			// Completing it here also verifies that no progress frame leaked to that surface.
			if interrupt {
				_ = l.toward.Close()
			}
			close(release)
			reply := l.recv()
			if reply.Kind != "result" || reply.ID != 1 || reply.Error != "" || reply.LocalSaved || reply.SyncPending {
				t.Fatalf("legacy reply=%+v", reply)
			}
		})
	}
}

// L4-R4: A legacy engine's empty close reply stays success; its missing receipt is negotiated, never guessed.
func TestL4FixLegacyEngineSaving(t *testing.T) {
	c, _ := newEngine(t)
	if err := c.Agent().Close(); err != nil {
		t.Fatal(err)
	}
	capability, ok := any(c.Agent()).(interface{ SavingReceipts() bool })
	if !ok || capability.SavingReceipts() {
		t.Fatal("legacy welcome did not select legacy saving")
	}
}

// L4-R4: Legacy errors retain the old result and cannot acquire a new upload receipt.
func TestL4FixLegacyEngineUploadReply(t *testing.T) {
	c, e := newEngine(t)
	e.fails[MethodClose] = "upload did not finish"
	err := c.Agent().Close()
	if err == nil || syncwords.UploadPending(err) || errors.Is(err, session.ErrSavingUnanswered) {
		t.Fatalf("legacy error=%v", err)
	}
	capability, ok := any(c.Agent()).(interface{ SavingReceipts() bool })
	if !ok || capability.SavingReceipts() {
		t.Fatal("legacy engine requires legacy words")
	}
}

// L4-G6: A link that breaks before window naming still reports that selected name afterwards.
func TestL4FixSelectedNameAfterBrokenWelcome(t *testing.T) {
	client, engine := newEngine(t)
	_ = engine.conn.Close()
	if err := client.Agent().Compact(t.Context()); err == nil {
		t.Fatal("broken link succeeded")
	}
	client.SetDisplayName("desk-b")
	err := client.Agent().Compact(t.Context())
	if err == nil || !strings.Contains(err.Error(), "desk-b") || strings.Contains(err.Error(), "devbox") {
		t.Fatalf("cached failure retained the route name: %v", err)
	}
	if client.Host() != "devbox" {
		t.Fatal("window naming changed the connection route")
	}
}

package remote

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

type shellAgent struct {
	*fakeAgent
	shell string
}

func (a *shellAgent) SubmitBash(ctx context.Context, text string) (<-chan session.Event, error) {
	a.shell = text
	return a.fakeAgent.Submit(ctx, text)
}

func TestUserBashUsesExplicitDoorAndStreamsLiteralOutput(t *testing.T) {
	fake := &fakeAgent{}
	shell := &shellAgent{fakeAgent: fake}
	engine := engineOn(fake)
	engine.Agent = shell
	l := dialAgent(t, engine)
	l.hello(Hello{Version: Version})
	ref := decode[StreamRef](t, l.ok(1, MethodSubmitBash, SubmitArgs{Text: "!pwd"}).Payload)
	if shell.shell != "!pwd" {
		t.Fatal("shell submitted through generic message door")
	}
	stream := fake.stream(0)
	output := "  literal\n\nλ\n"
	stream <- session.Event{Kind: session.EventToolOutput, Tool: "bash", CallID: "user_bash_test", Text: output}
	fake.finish(stream)
	ev := decode[EventWire](t, l.recv().Payload).Unwire()
	if ev.Kind != session.EventToolOutput || ev.Text != output || ev.CallID != "user_bash_test" {
		t.Fatalf("wire changed output: %+v", ev)
	}
	closed := l.recv()
	if closed.Kind != "closed" || closed.ID != ref.Stream {
		t.Fatalf("stream did not close: %+v", closed)
	}
}

func TestUserBashRefusesEngineWithoutExplicitDoor(t *testing.T) {
	fake := &fakeAgent{}
	l := dialAgent(t, engineOn(fake))
	l.hello(Hello{Version: Version})
	if result := l.call(1, MethodSubmitBash, SubmitArgs{Text: "!pwd"}); result.Error == "" {
		t.Fatal("missing shell door silently accepted")
	}
	if len(fake.sent) != 0 {
		t.Fatal("shell fell back to generic message")
	}
}

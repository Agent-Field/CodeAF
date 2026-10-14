package crewagent

import (
	"context"
	"testing"

	appagent "github.com/Agent-Field/codeaf/internal/apps/agent"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type scriptedModel struct {
	svc.Model
	replies []string
	calls   []svc.ModelCall
}

func (m *scriptedModel) Complete(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	text := m.replies[len(m.calls)%len(m.replies)]
	m.calls = append(m.calls, call)
	return svc.ModelReply{Text: text, Cost: .1}, nil
}

type outboundConnector struct{ svc.Connectors }

func (*outboundConnector) Outbound(string) bool { return true }

func TestRealAgentUsesPublicCrewThreadAndKeepsApprovalWithAsker(t *testing.T) {
	for _, shape := range []bool{false, true} {
		name := "send approval"
		if shape {
			name = "shape request"
		}
		t.Run(name, func(t *testing.T) {
			service, all, _, approvals := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service.ctx = ctx
			def := all.Store.(*fakeStore).def
			def.Agent.Tools = []string{"gmail.send"}
			connector := def.App.Capabilities.Connectors["gmail"]
			connector.Can = append(connector.Can, "send")
			def.App.Capabilities.Connectors["gmail"] = connector
			model := &scriptedModel{replies: []string{`{"tool":"gmail.send","args":{"to":"priya@example.com","subject":"Q4"}}`, `{"say":"Sending waits on you."}`}}
			if shape {
				model.replies = []string{`{"shape_this":true}`}
			}
			all.Model = model
			all.Connectors = &outboundConnector{}
			all.Agent = appagent.New(svc.Config{Root: service.root, Ctx: ctx}, all)
			all.Crews.Agent = service
			Prepare(all)
			events, stop := all.Hub.Subscribe(bridge.TopicAgent("crm", "crew:pricing"))
			defer stop()
			req := svc.CrewAsk{App: "crm", Crew: "pricing", CrewName: "Pricing push", Asker: svc.Caller{Member: "mona", Role: svc.RoleUser}, Text: "Add a field", Linked: []bridge.Record{{Type: "deal", ID: "north"}}}
			for range 2 {
				answer, err := service.Ask(ctx, req)
				if err != nil {
					t.Fatal(err)
				}
				if shape {
					if answer.ShapeThis != req.Text || answer.Cost != .1 || len(answer.Approvals) != 0 {
						t.Fatalf("answer=%+v", answer)
					}
				} else {
					if len(answer.Approvals) != 1 || answer.Approvals[0].Who != "mona" || answer.Approvals[0].Payload["crew"] != "pricing" || answer.Approvals[0].Payload["crew_name"] != "Pricing push" {
						t.Fatalf("answer=%+v", answer)
					}
					if answer.Cost < .199999 || answer.Cost > .200001 {
						t.Fatal(answer.Cost)
					}
				}
			}
			if len(events) == 0 {
				t.Fatal("public crew thread received no events")
			}
			for len(events) > 0 {
				event := <-events
				var streamed bridge.AgentEvent
				if err := decodeEvent(event.Data, &streamed); err != nil {
					t.Fatal(err)
				}
				if streamed.Kind == bridge.AgentShapeThis && streamed.Request != req.Text {
					t.Fatalf("Shape received linked records: %q", streamed.Request)
				}
			}
			if !shape && len(approvals.opened) != 2 {
				t.Fatalf("approvals were reopened: %+v", approvals.opened)
			}
			for _, call := range model.calls {
				if call.App != "crm" {
					t.Fatalf("charged to %q", call.App)
				}
			}
		})
	}
}

type waitingModel struct {
	svc.Model
	started chan struct{}
	stopped chan struct{}
}

func (m *waitingModel) Complete(ctx context.Context, _ svc.ModelCall) (svc.ModelReply, error) {
	close(m.started)
	<-ctx.Done()
	close(m.stopped)
	return svc.ModelReply{}, ctx.Err()
}

func TestRealAgentCrewCopyStopsWithItsRequest(t *testing.T) {
	service, all, _, _ := fixture(t)
	model := &waitingModel{started: make(chan struct{}), stopped: make(chan struct{})}
	all.Model = model
	all.Agent = appagent.New(svc.Config{Root: service.root}, all)
	all.Crews.Agent = service
	Prepare(all)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.Ask(ctx, svc.CrewAsk{App: "crm", Crew: "pricing", Asker: svc.Caller{Member: "mona", Role: svc.RoleUser}, Text: "Read deals"})
		done <- err
	}()
	<-model.started
	cancel()
	<-model.stopped
	if err := <-done; err == nil {
		t.Fatal("canceled crew copy returned success")
	}
	release, err := service.acquire(context.Background(), "crm")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

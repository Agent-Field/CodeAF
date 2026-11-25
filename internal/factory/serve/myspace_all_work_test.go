package serve

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestMySpaceAfiReadsAcrossVisibleWorkstreams(t *testing.T) {
	o := newWSOrg(t)
	visible := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch"})
	o.create(t, "santosh", api.WorkstreamNew{Name: "Support"})
	o.create(t, "abir", api.WorkstreamNew{Name: "Private finances", Private: true})
	o.do(t, "santosh", "POST", "/v1/workstreams/"+visible.ID+"/tasks", -1, api.TaskCreate{Title: "Publish release notes", DoneBy: factory.TaskDoneBy{Kind: "person", ID: "santosh"}}, 201, nil)
	o.driverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		reader, ok := turn.Door.(interface {
			AllWork(context.Context) (string, error)
			AllWorkAvailable() bool
		})
		if !ok {
			t.Error("My space Afi cannot read all work")
			return AgentTurnEnd{}, nil
		}
		if turn.Chat != "" {
			if reader.AllWorkAvailable() {
				t.Error("a named chat received Afi's wider scope")
			}
			if _, err := reader.AllWork(ctx); err == nil {
				t.Error("a named chat read across workstreams")
			}
			return AgentTurnEnd{}, nil
		}
		if !reader.AllWorkAvailable() {
			t.Error("Afi's wider scope is absent")
		}
		words, err := reader.AllWork(ctx)
		if err != nil {
			t.Error(err)
			return AgentTurnEnd{}, nil
		}
		for _, want := range []string{"Launch", "Support", "Publish release notes"} {
			if !strings.Contains(words, want) {
				t.Errorf("all work omits %q", want)
			}
		}
		if strings.Contains(words, "Private finances") {
			t.Error("all work disclosed another person's private workstream")
		}
		return AgentTurnEnd{}, nil
	})
	var mine api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &mine)
	o.do(t, "santosh", "POST", "/v1/workstreams/"+mine.Workstream.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "What is all my work?"}, 202, nil)
	o.settle()
	var chat api.AgentChat
	o.do(t, "santosh", "POST", "/v1/workstreams/"+mine.Workstream.ID+"/chats", -1, nil, 201, &chat)
	o.do(t, "santosh", "POST", "/v1/workstreams/"+mine.Workstream.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "What is all my work?", Chat: chat.ID}, 202, nil)
	o.settle()
}

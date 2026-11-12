package remote

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
)

const methodFactoryStatus = "conversation.factory_status"

type factoryStatusReader interface {
	ReadFactoryStatus(context.Context, string) (string, bool, error)
}
type factoryStatusReply struct {
	Text    string
	Handled bool
}

func factoryStatusCall(agent WrappedAgent, call Frame) (json.RawMessage, bool, error) {
	if call.Method != methodFactoryStatus {
		return nil, false, nil
	}
	args, err := arg[struct{ Text string }](call)
	if err != nil {
		return nil, true, err
	}
	reply := factoryStatusReply{}
	if door, ok := agent.(factoryStatusReader); ok && factory.StatusOnlyRequest(args.Text) {
		ctx, cancel := context.WithTimeout(context.Background(), callDeadline)
		defer cancel()
		reply.Text, reply.Handled, err = door.ReadFactoryStatus(ctx, args.Text)
	}
	if err != nil {
		return nil, true, err
	}
	data, err := json.Marshal(reply)
	return data, true, err
}
func (a *Agent) ReadFactoryStatus(ctx context.Context, text string) (string, bool, error) {
	if !factory.StatusOnlyRequest(text) {
		return "", false, nil
	}
	data, err := a.c.call(ctx, methodFactoryStatus, struct{ Text string }{text})
	if err != nil {
		if strings.Contains(err.Error(), "no such method") {
			return "", false, nil
		}
		return "", true, err
	}
	var reply factoryStatusReply
	err = json.Unmarshal(data, &reply)
	return reply.Text, reply.Handled, err
}

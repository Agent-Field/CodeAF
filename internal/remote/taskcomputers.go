package remote

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/session"
)

const taskComputersUnavailable = "paired task computers are unavailable here"

type taskComputerDoor interface {
	TaskComputersSupported() bool
	StartTaskOn(context.Context, string, bool, string, ...string) (uint64, string, string, error)
	RunTaskHere(context.Context, uint64) (string, error)
}

func taskComputersKnown(agent any) bool {
	door, ok := agent.(taskComputerDoor)
	return ok && door.TaskComputersSupported()
}

// An omitted welcome field means the engine does not understand these new
// requests. A client method's presence alone must never enable its UI control.
func (a *Agent) TaskComputersSupported() bool { return a.c.Welcome().TaskComputers }

func (a *Agent) StartTaskOn(ctx context.Context, brief string, solo bool, computer string, effort ...string) (uint64, string, string, error) {
	word := ""
	if len(effort) > 0 {
		word = effort[0]
	}
	return a.StartTaskOnIn(ctx, brief, solo, computer, word, a.c.Welcome().SessionFile)
}

// The captured conversation survives a window switch while its start command
// is queued, so the new request cannot commission work in the replacement chat.
func (a *Agent) StartTaskOnIn(ctx context.Context, brief string, solo bool, computer, effort, conversation string) (uint64, string, string, error) {
	if !a.TaskComputersSupported() {
		return 0, "", "", errors.New(taskComputersUnavailable)
	}
	raw, err := a.callTaskStart(ctx, MethodTaskStartOn, TaskStartOnArgs{Brief: brief, Solo: solo, Computer: computer, Effort: effort, Session: conversation})
	if err != nil {
		return 0, "", "", err
	}
	var reply TaskStarted
	if err := json.Unmarshal(raw, &reply); err != nil {
		return 0, "", "", err
	}
	return reply.ID, reply.Title, reply.Note, nil
}

func (a *Agent) RunTaskHere(ctx context.Context, id uint64) (string, error) {
	return a.RunTaskHereIn(ctx, id, a.c.Welcome().SessionFile)
}

// Recovery carries the same owner binding as a named start. The existing
// session operation owns abandonment and local admission, not the connection.
func (a *Agent) RunTaskHereIn(ctx context.Context, id uint64, conversation string) (string, error) {
	if !a.TaskComputersSupported() {
		return "", errors.New(taskComputersUnavailable)
	}
	raw, err := a.c.call(ctx, MethodTaskRunHere, TaskSetupArgs{ID: id, Session: conversation})
	if err != nil {
		return "", err
	}
	var reply TaskStarted
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", err
	}
	return reply.Note, nil
}

func (s *server) taskComputerOwner(conversation string) (taskComputerDoor, error) {
	want, agreed := steerConversation(s.joined, conversation)
	if !agreed || want == "" {
		return nil, session.ErrNotThatConversation
	}
	owner, mine := s.session.agentOf(want)
	if !mine {
		return nil, session.ErrNotThatConversation
	}
	if !taskComputersKnown(owner) {
		return nil, errors.New(taskComputersUnavailable)
	}
	return owner.(taskComputerDoor), nil
}

func (s *server) taskComputerInvoke(call Frame) (json.RawMessage, error) {
	if call.Method == MethodTaskStartOn {
		args, err := arg[TaskStartOnArgs](call)
		if err != nil {
			return nil, err
		}
		door, err := s.taskComputerOwner(args.Session)
		if err != nil {
			return nil, err
		}
		id, title, note, err := door.StartTaskOn(context.Background(), args.Brief, args.Solo, args.Computer, args.Effort)
		if err != nil {
			return nil, err
		}
		return json.Marshal(TaskStarted{ID: id, Title: title, Note: note})
	}
	args, err := arg[TaskSetupArgs](call)
	if err != nil {
		return nil, err
	}
	door, err := s.taskComputerOwner(args.Session)
	if err != nil {
		return nil, err
	}
	words, err := door.RunTaskHere(context.Background(), args.ID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(TaskStarted{ID: args.ID, Note: words})
}

var _ taskComputerDoor = (*session.Agent)(nil)

package remote

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type taskWorkspaceDoor interface {
	ReviewTaskWorkspaceRoot(context.Context, uint64, string) (session.TaskWorkspaceReview, error)
	ApplyTaskWorkspaceRoot(context.Context, uint64, string) (string, error)
}
type taskWorkspaceArgs struct {
	ID                   uint64
	Slot, Session, Token string
	Deadline             time.Time
}
type taskWorkspaceResult struct {
	Review  session.TaskWorkspaceReview
	Receipt string
}
type taskWorkspacePending struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func taskWorkspaceKnown(agent any) bool { _, ok := agent.(taskWorkspaceDoor); return ok }

func (a *Agent) ReviewTaskWorkspaceRoot(ctx context.Context, id uint64, slot string) (session.TaskWorkspaceReview, error) {
	result, err := a.taskWorkspaceCall(ctx, MethodTaskWorkspaceReview, id, slot)
	return result.Review, err
}
func (a *Agent) ApplyTaskWorkspaceRoot(ctx context.Context, id uint64, slot string) (string, error) {
	result, err := a.taskWorkspaceCall(ctx, MethodTaskWorkspaceApply, id, slot)
	return result.Receipt, err
}
func (a *Agent) taskWorkspaceCall(ctx context.Context, method string, id uint64, slot string) (taskWorkspaceResult, error) {
	var result taskWorkspaceResult
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	welcome := a.c.Welcome()
	if !welcome.TaskWorkspace {
		return result, errors.New("reviewing and applying task roots needs a newer engine; update the engine and reconnect")
	}
	deadline := time.Now().Add(callDeadline)
	if bounded, ok := ctx.Deadline(); ok && bounded.Before(deadline) {
		deadline = bounded
	}
	args := taskWorkspaceArgs{ID: id, Slot: slot, Session: welcome.SessionFile, Token: rand.Text(), Deadline: deadline}
	payload, answered, err := a.c.callAnswered(ctx, method, args, callDeadline)
	if !answered {
		// Cancellation travels independently of the operation and its ordered UI
		// gesture. Once atomic landing is admitted, its actual receipt still wins.
		go func() {
			cancelCtx, cancel := context.WithTimeout(context.Background(), callDeadline)
			defer cancel()
			_, _ = a.c.call(cancelCtx, MethodTaskWorkspaceCancel, taskWorkspaceArgs{Token: args.Token})
		}()
		if err != nil {
			return result, fmt.Errorf("task root outcome was not confirmed; inspect it before retrying: %w", err)
		}
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(payload, &result)
	return result, err
}

// A cancel can reach the independent lane before its operation goroutine. A
// short-lived cancelled entry remembers that order; entries always expire.
func (s *server) taskWorkspacePending(args taskWorkspaceArgs) *taskWorkspacePending {
	deadline := args.Deadline
	limit := time.Now().Add(callDeadline)
	if deadline.IsZero() || deadline.After(limit) {
		deadline = limit
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	pending := &taskWorkspacePending{ctx: ctx, cancel: cancel}
	actual, loaded := s.workspaceCalls.LoadOrStore(args.Token, pending)
	if loaded {
		cancel()
		return actual.(*taskWorkspacePending)
	}
	time.AfterFunc(callDeadline, func() { s.workspaceCalls.CompareAndDelete(args.Token, pending); cancel() })
	return pending
}
func (s *server) taskWorkspaceInvoke(method string, args taskWorkspaceArgs) ([]byte, error) {
	if args.Token == "" || len(args.Token) > 128 {
		return nil, errors.New("invalid task workspace request token")
	}
	pending := s.taskWorkspacePending(args)
	if method == MethodTaskWorkspaceCancel {
		pending.cancel()
		return nil, nil
	}
	defer func() { s.workspaceCalls.CompareAndDelete(args.Token, pending); pending.cancel() }()
	if err := pending.ctx.Err(); err != nil {
		return nil, err
	}
	want, agreed := steerConversation(s.joined, args.Session)
	if !agreed || want == "" {
		return nil, session.ErrNotThatConversation
	}
	owner, mine := s.session.agentOf(want)
	if !mine {
		return nil, session.ErrNotThatConversation
	}
	door, ok := owner.(taskWorkspaceDoor)
	if !ok {
		return nil, errors.New("this engine cannot review or apply owned task roots")
	}
	if args.ID == 0 || strings.TrimSpace(args.Slot) == "" {
		return nil, errors.New("a task number and root slot are required")
	}
	var result taskWorkspaceResult
	var err error
	if method == MethodTaskWorkspaceApply {
		result.Receipt, err = door.ApplyTaskWorkspaceRoot(pending.ctx, args.ID, args.Slot)
	} else {
		result.Review, err = door.ReviewTaskWorkspaceRoot(pending.ctx, args.ID, args.Slot)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

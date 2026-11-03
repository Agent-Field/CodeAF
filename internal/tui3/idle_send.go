package tui3

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

// resumeIdleSend asks this open writer to reacquire in place. A matching head
// is evidence for admission, never permission to restore over its workspace.
func resumeIdleSend(ctx context.Context, agent Agent, source chatlist.Source, msg ownershipReadMsg, blocked bool) ownershipReadMsg {
	if blocked || msg.head == "" || msg.reading.err != nil {
		return msg
	}
	receipts, ok := source.(sendLeaseSource)
	if !ok {
		return msg
	}
	id := msg.leaseCell
	if id == "" {
		id = filepath.Base(session.DirOf(msg.file))
	}
	for _, row := range msg.reading.rows {
		if row.Cell != id {
			continue
		}
		if !row.Mine || row.Head != msg.head || !msg.retryIdle && (!row.AutoOpen || row.Status != chatlist.Idle && row.Status != chatlist.Off) {
			continue
		}
		// The directory read is complete. Taking the hold back is a separate,
		// bounded write and must not inherit the read's remaining deadline.
		ctx, cancel := context.WithTimeout(context.Background(), executor.TakeBackWithin)
		defer cancel()
		var check func() error
		if gate, ok := agent.(interface{ CheckWorkContext(context.Context) error }); ok {
			check = func() error { return gate.CheckWorkContext(ctx) }
		} else if gate, ok := agent.(interface{ CheckWork() error }); ok {
			check = gate.CheckWork
		} else {
			return msg
		}
		if err := check(); err != nil {
			// The client can lose its RPC before the engine can return the
			// typed refusal. This window was taking back its released chat;
			// keep that uncertainty across Enter and later ownership readings.
			var refusal *executor.WorkRefusal
			if !errors.As(err, &refusal) {
				err = &executor.WorkRefusal{Line: executor.TakeBackRetryLine, Retryable: true}
			}
			msg.admissionErr = err
			return msg
		}
		remote, fence, err := receipts.SendLease(row.Cell)
		holding := false
		if writer, ok := agent.(renewalReader); ok {
			_, _, holding = writer.RenewalStatus()
			if msg.retryIdle && err == nil && fence == row.Fence && !holding {
				// A confirmed retry can answer before its writer-status
				// frame. Wait for that cached fact; a different window's receipt
				// alone must never authorize this one.
				holding, _ = awaitSendWriter(ctx, writer, nil, true)
			}
		}
		if err != nil || remote != row.Cell || fence == 0 || fence < row.Fence || fence == row.Fence && (row.Released || !holding) {
			msg.admissionErr = &executor.WorkRefusal{Line: chatlist.Available}
			if msg.retryIdle {
				msg.admissionErr = &executor.WorkRefusal{Line: executor.TakeBackRetryLine, Retryable: true}
			}
			return msg
		}
		msg.proof, msg.local, msg.notHeld, msg.writerStopped = true, true, false, false
		msg.leaseCell, msg.fence = remote, fence
		msg.reading.holdCell, msg.reading.holdFence = row.Cell, fence
		msg.reading.rows = []chatlist.Row{{Cell: row.Cell, Status: chatlist.Here, Mine: true, Fence: fence, Head: row.Head}}
		return msg
	}
	return msg
}

func (a *app) refuseIdleSend(msg ownershipReadMsg) bool {
	if msg.admissionErr == nil {
		return false
	}
	var refusal *executor.WorkRefusal
	if errors.As(msg.admissionErr, &refusal) {
		id := filepath.Base(session.DirOf(msg.file))
		if a.machineRead.sendHolds == nil {
			a.machineRead.sendHolds = map[string]sendHold{}
		}
		hold := a.machineRead.sendHolds[id]
		hold.retryIdle = refusal.Retryable
		a.machineRead.sendHolds[id] = hold
		a.note(refusal.Line)
		a.blockDisplacedSend(refusal.Line)
	} else {
		a.note(messageKept(msg.host))
	}
	return true
}

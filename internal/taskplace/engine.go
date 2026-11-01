package taskplace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// Engine owns the out/run/back road. It never runs tools on the origin; the
// session retains copy preparation, ledger ownership and local landing.
type Engine struct {
	Copies     taskdoor.CopyDeps
	Computer   session.TaskComputer
	OriginName string
	Clock      session.TaskWatchClock
	Allowed    func(context.Context) error
}

func (e *Engine) dial(ctx context.Context) (*taskdoor.Conn, error) {
	if e.Allowed != nil {
		if err := e.Allowed(ctx); err != nil {
			return nil, err
		}
	}
	return taskdoor.Dial(ctx, e.Copies.Service, e.Copies.Identity, e.Copies.Device, e.Computer.Device, e.Computer.Name)
}
func (e *Engine) Land(context.Context, *plandb.Store, string, string, string) (session.RunLanding, error) {
	return session.RunLanding{}, errors.New("the returned task copy must be landed by this computer")
}

func (e *Engine) Start(ctx context.Context, spec session.RunSpec) session.RunSummary {
	record := session.TaskRemoteRecord{Executor: session.TaskExecutor{Device: e.Computer.Device, Name: e.Computer.Name}}
	if spec.Remote != nil {
		record = *spec.Remote
	}
	stopped := func() bool { return spec.RemoteStopped != nil && spec.RemoteStopped() }
	if stopped() {
		return session.RunSummary{Outcome: "stopped", KeepCopy: true, StorageSafe: true}
	}
	input, err := taskdoor.SendCopy(ctx, e.Copies, spec.Workspace)
	if err != nil {
		record.Fingerprint, _ = taskdoor.CopyFingerprint(ctx, spec.Workspace)
		return e.startFailed(spec, record, err)
	}
	record.Fingerprint = input.Fingerprint
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return e.startFailed(spec, record, err)
	}
	record.Attempt = "att_" + hex.EncodeToString(nonce[:])
	// Record before start as well as watch: a lost started reply is an uncertain
	// attempt, and recovery must ask for it rather than launch another child.
	if err := save(spec, record, true); err != nil {
		return failed(err)
	}
	conn, err := e.dial(ctx)
	if err != nil {
		return e.startFailed(spec, record, err)
	}
	defer conn.Close()
	if stopped() {
		return session.RunSummary{Outcome: "stopped", KeepCopy: true, StorageSafe: true}
	}
	request := taskdoor.Start{Chat: spec.Conversation, Task: int(spec.TaskNumber), Title: spec.Title, Instruction: spec.Brief, Model: spec.NamedModel, Effort: spec.NamedEffort, MaxSteps: spec.StepsPerTask, NoProgress: spec.NoProgress, Input: input, Limits: taskdoor.Limits{USD: spec.CostUSD, Hours: spec.Elapsed.Hours()}, OriginName: e.OriginName}
	startCtx, cancel := context.WithTimeout(ctx, taskdoor.DialWithin)
	stop := context.AfterFunc(startCtx, func() { _ = conn.Close() })
	err = conn.Send(record.Attempt, request)
	var frame taskdoor.Frame
	if err == nil {
		frame, err = conn.Receive()
	}
	stop()
	cancel()
	if err != nil {
		return e.startFailed(spec, record, err)
	}
	if frame.Type != "started" {
		var no taskdoor.Refused
		if frame.Type == "refused" && frame.Decode(&no) == nil && no.Reason != "" {
			return e.startFailed(spec, record, errors.New(no.Reason))
		}
		return e.startFailed(spec, record, errors.New(taskdoor.DifferentVersion(e.Computer.Name)))
	}
	if stopped() {
		stopCtx, cancel := context.WithTimeout(context.Background(), taskdoor.DialWithin)
		_ = e.Stop(stopCtx, record)
		cancel()
	}
	if spec.OnRemotePhase != nil {
		spec.OnRemotePhase(session.TaskRunningOn(e.Computer.Name))
	}
	_ = conn.Close()
	return e.follow(ctx, spec, record)
}
func save(spec session.RunSpec, record session.TaskRemoteRecord, force bool) error {
	if spec.SaveRemote != nil {
		return spec.SaveRemote(record, force)
	}
	return errors.New("the task's remote record cannot be saved here")
}
func failed(err error) session.RunSummary {
	return session.RunSummary{Outcome: "incomplete", Result: err.Error(), KeepCopy: true}
}
func (e *Engine) startFailed(spec session.RunSpec, record session.TaskRemoteRecord, err error) session.RunSummary {
	record.StartFailed = true
	if saveErr := save(spec, record, true); saveErr != nil {
		return failed(saveErr)
	}
	if spec.OnRemotePhase != nil {
		spec.OnRemotePhase(err.Error() + " — run it here")
	}
	return failed(err)
}
func (e *Engine) Resume(ctx context.Context, spec session.RunSpec) session.RunSummary {
	if spec.Remote == nil {
		return failed(errors.New("the task's remote record is missing"))
	}
	if spec.Remote.Abandoned {
		return failed(session.ErrTaskAttemptAbandoned)
	}
	return e.follow(ctx, spec, *spec.Remote)
}
func (e *Engine) follow(ctx context.Context, spec session.RunSpec, record session.TaskRemoteRecord) session.RunSummary {
	var final taskdoor.Finished
	if len(record.Final) > 0 {
		if err := json.Unmarshal(record.Final, &final); err != nil {
			return failed(err)
		}
	} else {
		var current taskdoor.Frame
		attempt := record.Attempt
		watch := func(ctx context.Context, from uint64, alive func(), deliver func(uint64) error) error {
			conn, err := e.dial(ctx)
			if err != nil {
				return err
			}
			defer conn.Close()
			if err = conn.Send(attempt, taskdoor.Watch{From: from}); err != nil {
				return err
			}
			alive()
			// Conn consumes ping/pong itself. Poll its actual activity rather than
			// mistaking a quiet model call for a computer that stopped answering.
			keepCtx, cancel := context.WithCancel(ctx)
			keepDone := make(chan struct{})
			defer func() { cancel(); <-keepDone }()
			go func() {
				defer close(keepDone)
				tick := time.NewTicker(time.Second)
				defer tick.Stop()
				last := conn.LastActivity()
				for {
					select {
					case <-keepCtx.Done():
						return
					case <-tick.C:
						now := conn.LastActivity()
						if now.After(last) {
							last = now
							alive()
						}
					}
				}
			}()
			for {
				frame, err := conn.Receive()
				if err != nil {
					return err
				}
				alive()
				if frame.Attempt != attempt {
					continue
				}
				if frame.Type == "unknown" {
					current = frame
					if err := deliver(from + 1); err != nil {
						return err
					}
					return session.ErrTaskWatchEnded
				}
				if frame.Seq <= from {
					continue
				}
				current = frame
				if err = deliver(frame.Seq); err != nil {
					return err
				}
				if frame.Type == "finished" {
					return session.ErrTaskWatchEnded
				}
			}
		}
		consume := func(seq uint64) error {
			record.Unreachable = false
			switch current.Type {
			case "unknown":
				final = taskdoor.Finished{Outcome: "interrupted", Report: "the other computer no longer has this task's answer"}
				record.Final, _ = json.Marshal(final)
				record.Finished = true
			case "step":
				var event taskdoor.Step
				if err := current.Decode(&event); err != nil {
					return err
				}
				event.Step.FullOutput = ""
				// Only the root trajectory travels; B's child ids name no local rows.
				event.Step.Children = nil
				raw, _ := json.Marshal(event.Step)
				if spec.OnRemoteStep != nil {
					if err := spec.OnRemoteStep(seq, raw); err != nil {
						return err
					}
				}
			case "spend":
				var event taskdoor.Spend
				if err := current.Decode(&event); err != nil {
					return err
				}
				line := event.Line
				charge := session.RunCharge{Model: line.Model, TokensIn: line.InputTokens, TokensOut: line.OutputTokens, Cached: line.CachedTokens, USD: line.USD}
				if spec.OnRemoteCharge != nil {
					if err := spec.OnRemoteCharge(seq, charge, line.Seat); err != nil {
						return err
					}
				}
				record.USD += line.USD
			case "phase":
				var phase taskdoor.Phase
				if err := current.Decode(&phase); err != nil {
					return err
				}
				if spec.OnRemotePhase != nil {
					spec.OnRemotePhase(phase.Words)
				}
			case "finished":
				if err := current.Decode(&final); err != nil {
					return err
				}
				record.Final, _ = json.Marshal(final)
				record.Finished = true
			default:
				return fmt.Errorf("the other computer sent an unreadable task event")
			}
			record.Seq = seq
			return save(spec, record, current.Type == "finished")
		}
		reach := func(missing bool) {
			record.Unreachable = missing
			_ = save(spec, record, true)
			if spec.OnRemotePhase != nil {
				words := session.TaskRunningOn(e.Computer.Name)
				if missing {
					words = session.TaskComputerStoppedAnswering(e.Computer.Name)
				}
				spec.OnRemotePhase(words)
			}
		}
		if err := session.FollowTaskWatch(ctx, e.Clock, record.Seq, watch, consume, reach); err != nil {
			_ = save(spec, record, true)
			return session.RunSummary{Outcome: "interrupted", Result: err.Error(), USD: record.USD, KeepCopy: true}
		}
	}
	if final.Result == nil && final.Outcome != "interrupted" {
		return session.RunSummary{Outcome: "incomplete", Result: final.Report + "\nThe result could not be brought from " + e.Computer.Name + "; the copy here was kept", USD: record.USD, KeepCopy: true, NeedsDecision: true}
	}
	if final.Result != nil && !record.Restored && final.Outcome != "interrupted" {
		prepared, err := taskdoor.PrepareResult(ctx, e.Copies, *final.Result, spec.Workspace, record.Fingerprint)
		if err == nil {
			defer prepared.Close()
			apply := func() error { return prepared.Install(ctx) }
			if spec.WithRemoteCopy != nil {
				err = spec.WithRemoteCopy(apply)
			} else {
				err = apply()
			}
		}
		if err != nil {
			summary := failed(err)
			summary.USD = record.USD
			summary.NeedsDecision = errors.Is(err, taskdoor.ErrCopyChanged)
			return summary
		}
		record.Restored = true
	}
	record.Finished = true
	record.Unreachable = false
	if err := save(spec, record, true); err != nil {
		return failed(err)
	}

	outcome := final.Outcome
	if outcome == "interrupted" && final.Result != nil {
		final.Report += "\nThe partial work stayed on the other computer; run it here starts from the sent copy."
	}
	switch outcome {
	case "done":
		_ = spec.Store.CompleteRoot(final.Report)
	case "stopped":
		_ = spec.Store.StopRoot(outcome)
	case "incomplete", "interrupted":
		_ = spec.Store.EndRoot(outcome)
	default:
		outcome = "incomplete"
		_ = spec.Store.EndRoot(outcome)
	}
	if spec.OnRemotePhase != nil {
		spec.OnRemotePhase(session.TaskRanOn(e.Computer.Name))
	}
	return session.RunSummary{Outcome: outcome, Result: final.Report, Model: final.Model, Nodes: 1, USD: record.USD, StorageSafe: true, KeepCopy: outcome == "interrupted"}
}
func (e *Engine) control(ctx context.Context, record session.TaskRemoteRecord, payload any) error {
	if record.Attempt == "" {
		return nil
	}
	conn, err := e.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = conn.Send(record.Attempt, payload); err != nil {
		return err
	}
	frame, err := conn.Receive()
	if err != nil {
		return err
	}
	if frame.Type == "refused" {
		var no taskdoor.Refused
		_ = frame.Decode(&no)
		return errors.New(no.Reason)
	}
	return nil
}
func (e *Engine) Stop(ctx context.Context, r session.TaskRemoteRecord) error {
	return e.control(ctx, r, taskdoor.Stop{})
}
func (e *Engine) Abandon(ctx context.Context, r session.TaskRemoteRecord) error {
	_ = e.Stop(ctx, r)
	return e.control(ctx, r, taskdoor.Ack{Disposition: "abandoned"})
}
func (e *Engine) Acknowledge(ctx context.Context, r session.TaskRemoteRecord) error {
	return e.control(ctx, r, taskdoor.Ack{Disposition: "landed"})
}
func (e *Engine) CheckCopy(ctx context.Context, workspace, fingerprint string) error {
	return taskdoor.CheckCopy(ctx, workspace, fingerprint)
}

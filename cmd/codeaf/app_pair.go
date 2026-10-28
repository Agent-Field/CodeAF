package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

const appJoinBusy = "Finish the work on this computer before joining another computer."
const appJoining = "Pairing is joining another computer. Finish pairing and restart codeaf before starting work."

type appPair struct {
	last    *appPairOperation
	mu      sync.Mutex
	next    uint64
	current *appPairOperation
	restart bool
}
type appPairOperation struct {
	backend  *appBackend
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	answer   chan string
	view     appwire.Pairing
	question uint64
	joining  bool
	finished bool
}

func pairSnapshot(p appwire.Pairing) appwire.Pairing {
	p.Lines = slices.Clone(p.Lines)
	if p.Question != nil {
		q := *p.Question
		q.Options = slices.Clone(q.Options)
		p.Question = &q
	}
	if p.Error != nil {
		e := *p.Error
		p.Error = &e
	}
	return p
}
func (*appBackend) PairingAvailable() bool { return cell.Available() }

func (b *appBackend) StartPair(p appwire.PairStartParams) (appwire.Pairing, error) {
	switch p.Mode {
	case "offer_code", "request_link":
	case "use_code":
		if strings.TrimSpace(p.Code) == "" {
			return appwire.Pairing{}, appwire.Errorf(appwire.CodeBadParams, "code is required")
		}
	case "approve_link":
		if strings.TrimSpace(p.Link) == "" {
			return appwire.Pairing{}, appwire.Errorf(appwire.CodeBadParams, "link is required")
		}
	default:
		return appwire.Pairing{}, appwire.Errorf(appwire.CodeBadParams, "unknown pairing mode")
	}
	if p.Name != "" {
		if _, err := devname.Clean(p.Name); err != nil {
			return appwire.Pairing{}, appwire.Errorf(appwire.CodeBadParams, "%s", err)
		}
	}
	b.pair.mu.Lock()
	defer b.pair.mu.Unlock()
	if b.pair.current != nil && !b.pair.current.finished {
		return appwire.Pairing{}, appwire.Errorf(appwire.CodeBusy, "Another pairing is running on this computer.")
	}
	if b.pair.restart {
		return appwire.Pairing{}, appwire.Errorf(appwire.CodeBusy, "Restart codeaf to finish joining this computer.")
	}
	joining := p.Mode == "use_code" || p.Mode == "request_link"
	busy, closed := false, false
	b.locked(func() {
		closed = b.closing
		if joining {
			for _, a := range b.agents {
				if a.opening || a.starting || a.view.State == appwire.StateWorking || a.view.State == appwire.StateNeedsYou || a.live != nil && (a.live.running || a.live.needsYou()) {
					busy = true
					break
				}
			}
		}
	})
	if closed {
		return appwire.Pairing{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	if busy {
		return appwire.Pairing{}, appwire.Errorf(appwire.CodeBusy, appJoinBusy)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.pair.next++
	op := &appPairOperation{backend: b, ctx: ctx, cancel: cancel, done: make(chan struct{}), answer: make(chan string, 1), joining: joining,
		view: appwire.Pairing{ID: fmt.Sprintf("pair-%d", b.pair.next), Mode: p.Mode, State: "preparing", Lines: []string{}}}
	b.pair.current = op
	op.emitLocked()
	guard.Go("app/pair", func() { defer close(op.done); defer cancel(); op.run(p) })
	return pairSnapshot(op.view), nil
}

// The previous finished operation remains addressable while the next runs.
func (b *appBackend) pairOperationLocked(id string) *appPairOperation {
	if op := b.pair.current; op != nil && (id == "" || op.view.ID == id) {
		return op
	}
	if op := b.pair.last; op != nil && (id == "" || op.view.ID == id) {
		return op
	}
	return nil
}

func (b *appBackend) GetPair(id string) (*appwire.Pairing, error) {
	b.pair.mu.Lock()
	defer b.pair.mu.Unlock()
	op := b.pairOperationLocked(id)
	if op == nil && id == "" {
		return nil, nil
	}
	if op == nil || id != "" && op.view.ID != id {
		return nil, appwire.Errorf(appwire.CodeNotFound, "that pairing is not here")
	}
	p := pairSnapshot(op.view)
	return &p, nil
}
func (b *appBackend) AnswerPair(p appwire.PairAnswerParams) (appwire.Pairing, error) {
	b.pair.mu.Lock()
	defer b.pair.mu.Unlock()
	op := b.pairOperationLocked(p.ID)
	if op == nil || op.view.ID != p.ID {
		return appwire.Pairing{}, appwire.Errorf(appwire.CodeNotFound, "that pairing is not here")
	}
	q := op.view.Question
	if op.finished || q == nil || q.ID != p.Question || !slices.ContainsFunc(q.Options, func(o appwire.PairOption) bool { return o.ID == p.Option }) {
		return appwire.Pairing{}, appwire.Errorf(appwire.CodeBadParams, "that answer is not waiting")
	}
	op.view.Question, op.view.State = nil, "waiting"
	op.emitLocked()
	op.answer <- p.Option
	return pairSnapshot(op.view), nil
}
func (b *appBackend) CancelPair(id string) (appwire.Pairing, error) {
	cancelPair := func() (*appPairOperation, error) {
		b.pair.mu.Lock()
		defer b.pair.mu.Unlock()
		op := b.pairOperationLocked(id)
		if op == nil || op.view.ID != id {
			return nil, appwire.Errorf(appwire.CodeNotFound, "that pairing is not here")
		}
		op.cancel()
		return op, nil
	}
	op, err := cancelPair()
	if err != nil {
		return appwire.Pairing{}, err
	}
	<-op.done
	b.pair.mu.Lock()
	defer b.pair.mu.Unlock()
	return pairSnapshot(op.view), nil
}
func (b *appBackend) closePair() {
	cancelCurrent := func() *appPairOperation {
		b.pair.mu.Lock()
		defer b.pair.mu.Unlock()
		op := b.pair.current
		if op != nil {
			op.cancel()
		}
		return op
	}
	op := cancelCurrent()
	if op != nil {
		<-op.done
	}
}
func (b *appBackend) pairingWorkRefusal() error {
	b.pair.mu.Lock()
	defer b.pair.mu.Unlock()
	if b.pair.restart || b.pair.current != nil && b.pair.current.joining && !b.pair.current.finished {
		return appwire.Errorf(appwire.CodeBusy, appJoining)
	}
	return nil
}
func (o *appPairOperation) emitLocked() {
	o.backend.emit.Emit(appwire.EventPairChanged, appwire.PairResult{Pairing: pairSnapshot(o.view)})
}
func (o *appPairOperation) update(fn func(*appwire.Pairing)) {
	o.backend.pair.mu.Lock()
	defer o.backend.pair.mu.Unlock()
	if o.finished {
		return
	}
	fn(&o.view)
	o.emitLocked()
}
func (o *appPairOperation) line(line string) {
	o.update(func(p *appwire.Pairing) { p.Lines = append(p.Lines, strings.Split(line, "\n")...) })
}
func (o *appPairOperation) ask(ctx context.Context, kind, title, detail string, options []appwire.PairOption) string {
	o.update(func(p *appwire.Pairing) {
		o.question++
		p.State = "question"
		p.Question = &appwire.PairQuestion{ID: fmt.Sprint(o.question), Kind: kind, Title: title, Detail: detail, Options: options}
	})
	select {
	case a := <-o.answer:
		return a
	case <-ctx.Done():
		return ""
	case <-o.ctx.Done():
		return ""
	}
}
func (o *appPairOperation) finish(state, line string, err error, restart bool) {
	o.backend.pair.mu.Lock()
	defer o.backend.pair.mu.Unlock()
	if o.finished {
		return
	}
	o.finished = true
	o.backend.pair.last = o
	o.view.State = state
	o.view.Question = nil
	o.view.Code = ""
	o.view.Link = ""
	o.view.ExpiresAt = time.Time{}
	if line != "" {
		o.view.Lines = append(o.view.Lines, strings.Split(line, "\n")...)
	}
	if err != nil {
		o.view.Error = &appwire.PairError{Code: appwire.CodeEngine, Message: err.Error()}
	}
	o.view.Restart = restart
	o.backend.pair.restart = restart
	o.emitLocked()
}
func (o *appPairOperation) run(p appwire.PairStartParams) {
	err, restart := o.perform(p)
	switch {
	case err == nil:
		if !restart {
			guard.Go("app/paired-chats", func() { _, _ = o.backend.listChats(true) })
		}
		// These events precede done: a desktop may immediately restart on done.
		o.backend.publishDevices()
		sync, _ := o.backend.SyncStatus()
		o.backend.emit.Emit(appwire.EventSyncChanged, appwire.SyncChanged{Sync: sync})
		o.finish("done", "", nil, restart)
	case o.ctx.Err() != nil || errors.Is(err, context.Canceled):
		o.finish("cancelled", appPairCancelled, nil, false)
	case errors.Is(err, pair.ErrRefused):
		o.finish("cancelled", appPairDeclined, nil, false)
	default:
		o.finish("failed", appPairFailure(err), errors.New(appPairFailure(err)), false)
	}
}
func (o *appPairOperation) perform(p appwire.PairStartParams) (error, bool) {
	dir := o.backend.relayHome()
	if reason := cell.UnavailableReason(); reason != "" {
		return errors.New(reason), false
	}
	if p.Name != "" {
		if _, err := devname.Set(dir, p.Name); err != nil {
			return err, false
		}
	}
	replace := false
	if o.joining {
		if _, err := identity.Load(dir); err == nil {
			fresh, err := identity.Joinable(dir)
			if err != nil {
				return err, false
			}
			if !fresh {
				a := o.ask(o.ctx, "replace_own_chats", appPairReplaceTitle, appPairReplaceDetail, appPairReplaceOptions())
				if a != "replace" {
					return context.Canceled, false
				}
				replace = true
			}
		} else if !errors.Is(err, identity.ErrNone) {
			return err, false
		}
	}
	via := ""
	if p.Mode == "approve_link" {
		via = pair.LinkRelay(p.Link)
	}
	route, err := pairMailboxAt(dir, via)
	if err != nil {
		return err, false
	}
	record := func() {
		if err := ownDeviceRecord(o.ctx, dir, route); err != nil {
			o.line(appPairRecordUnsaid)
		}
	}
	switch p.Mode {
	case "offer_code":
		grant, err := pairGrantAt(dir, route)
		if err != nil {
			return err, false
		}
		name, err := pair.Offer(o.ctx, route, grant, o)
		if err != nil {
			return err, false
		}
		record()
		o.line(appPairPaired(name))
		return nil, false
	case "use_code":
		joined, err := pair.Join(o.ctx, route, pairJoiningAt(dir)(replace), p.Code, o)
		if err != nil {
			return err, false
		}
		record()
		o.line(appPairJoinedLine(joined))
		return nil, !joined.Already
	case "request_link":
		joined, err := pair.JoinByLink(o.ctx, linkRequests(route), linkJoining(dir)(route, replace), o)
		if err != nil && !errors.Is(err, pair.ErrPairedNotRead) {
			return err, false
		}
		record()
		if err != nil {
			o.line(appPairListUnread)
		} else {
			o.line(appPairJoined)
		}
		return nil, !joined.Already
	case "approve_link":
		ref, err := pair.ReadLink(p.Link)
		if err != nil {
			return err, false
		}
		who, err := linkApprover(dir)(route)
		if err != nil {
			return err, false
		}
		who.BeforeApprove = func(ctx context.Context) error { return ownDeviceRecord(ctx, dir, route) }
		asking, err := who.Look(o.ctx, ref)
		if err != nil {
			return err, false
		}
		o.line(appPairApproveTitle(asking.Name))
		answer := o.ask(o.ctx, "approve", appPairApproveTitle(asking.Name), appPairCheck(asking.Check), pairApprovalOptions())
		if answer != "y" {
			if o.ctx.Err() != nil {
				return o.ctx.Err(), false
			}
			if err := who.Deny(o.ctx, asking); err != nil {
				return err, false
			}
			return pair.ErrRefused, false
		}
		if err := who.Approve(o.ctx, asking); err != nil {
			return err, false
		}
		if err := identity.EndSolo(dir); err != nil {
			return err, false
		}
		record()
		o.line(appPairPaired(asking.Name))
		return nil, false
	}
	return errors.New("unknown pairing mode"), false
}
func (o *appPairOperation) Show(code *pair.Code, lines string) {
	o.update(func(p *appwire.Pairing) {
		p.State = "waiting"
		p.Code = code.Shown()
		p.ExpiresAt = code.Born().Add(pair.CodeValidFor)
		p.Lines = append(p.Lines, appPairCodeLines(code.Shown())...)
	})
}
func (o *appPairOperation) Burned() { o.line(appPairBurned) }
func (o *appPairOperation) Ask(ctx context.Context, label, words string) bool {
	o.update(func(p *appwire.Pairing) { p.Words = words })
	return o.ask(ctx, "approve", appPairApproveTitle(label), appPairWords(words), pairApprovalOptions()) == "y"
}
func (o *appPairOperation) Waiting(words string) {
	o.update(func(p *appwire.Pairing) {
		p.State = "waiting"
		p.Words = words
		p.Lines = append(p.Lines, appPairWaiting(words))
	})
}
func (o *appPairOperation) Invited(in pair.Invite) {
	o.update(func(p *appwire.Pairing) {
		p.State = "waiting"
		p.Link = in.Ref.URL()
		if in.Via != "" {
			p.Link = in.Ref.URLAt(in.Via)
		}
		p.Words = in.Check
		p.ExpiresAt = time.Now().Add(in.ExpiresIn)
		p.Lines = append(p.Lines, appPairInviteLines(in.Check, in.ExpiresIn)...)
	})
}

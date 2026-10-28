package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func appTakeError(err error) error {
	if errors.Is(err, directory.ErrLeaseHeld) || errors.Is(err, directory.ErrFenceStale) {
		return &appwire.Error{Code: appwire.CodeConflict, Message: chatlist.LostRace, Detail: appwire.HolderChanged{Kind: "holder_changed"}}
	}
	if errors.Is(err, directory.ErrNotFound) {
		return appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	}
	var wire *appwire.Error
	if errors.As(err, &wire) && wire.Code != appwire.CodeEngine {
		return wire
	}
	return &appwire.Error{Code: appwire.CodeEngine, Message: appTakeFailed, Detail: err.Error()}
}
func (b *appBackend) TakeAgent(p appwire.TakeParams) (appwire.Agent, error) {
	if p.Cell == "" || p.Seen.Device == "" {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "a chat and its seen computer are required")
	}
	if !cell.ValidID(p.Cell) {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	}
	if err := b.pairingWorkRefusal(); err != nil {
		return appwire.Agent{}, err
	}
	b.chats.take.Lock()
	defer b.chats.take.Unlock()
	s, ok, err := syncsetup.Open(b.relayHome())
	if err != nil {
		return appwire.Agent{}, appTakeError(err)
	}
	if !ok {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeEngine, appSyncOff)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-b.left:
			cancel()
		case <-ctx.Done():
		}
	}()
	// Check even the already-here fast path against what the person saw.
	view, err := s.Dir.Cell(ctx, p.Cell)
	if err != nil {
		return appwire.Agent{}, appTakeError(err)
	}
	if view.Cell.Lease.Device != p.Seen.Device || view.Cell.Lease.Fence != p.Seen.Fence {
		return appwire.Agent{}, appTakeError(directory.ErrLeaseHeld)
	}
	if view.Cell.Archived || view.Cell.ParentCell != "" {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	}
	locals := b.localChats(s)
	var existing *appAgent
	if local, found := locals[p.Cell]; found {
		if !local.copyable {
			return appwire.Agent{}, appwire.Errorf(appwire.CodeEngine, "This older chat stays on this computer.")
		}
		b.locked(func() { existing = b.agents[local.agent] })
	}
	if existing != nil {
		existing.gate.Lock()
		busy := false
		b.locked(func() {
			busy = existing.opening || existing.starting || existing.live != nil && (existing.live.running || existing.live.needsYou())
		})
		if busy {
			existing.gate.Unlock()
			return appwire.Agent{}, appwire.Errorf(appwire.CodeBusy, "Finish this chat's work before continuing it here.")
		}
		if view.Cell.Lease.Device != s.Device.ID() {
			b.release(existing)
		}
		existing.gate.Unlock()
	}
	got, err := chatContinuer(s, nil).TakeSeen(ctx, p.Cell, &directory.LeaseExpectation{Device: p.Seen.Device, Fence: p.Seen.Fence})
	if err != nil {
		return appwire.Agent{}, appTakeError(err)
	}
	// The continuer's already-here shortcut may observe a local acquisition
	// that happened after our preflight read. It must not accept older consent.
	if got.AlreadyHere && (p.Seen.Device != s.Device.ID() || p.Seen.Fence != got.Taken.Driving.Fence) {
		return appwire.Agent{}, appTakeError(directory.ErrLeaseHeld)
	}
	root := got.Taken.Cell.Root
	meta, err := session.LoadMeta(root)
	if err != nil {
		return appwire.Agent{}, appTakeError(err)
	}
	transcript := filepath.Join(root, cell.TranscriptPath)
	id := appAgentID(transcript)
	if existing != nil {
		b.locked(func() { id = existing.view.ID })
	}
	b.remember(session.SessionRow{ID: id, Dir: root, Transcript: transcript, Workspace: meta.Workspace, Title: meta.Title, Created: meta.Created})
	a := b.agent(id)
	b.locked(func() { a.transcript = transcript; a.windowMoved = false })
	location := &appwire.Location{Here: true, Cell: p.Cell, Seen: &appwire.Seen{Device: s.Device.ID(), Fence: got.Taken.Driving.Fence}, Holder: &appwire.LocationHolder{ID: s.Device.ID(), Name: got.Device}}
	b.located(a, location)
	agent, err := b.OpenAgent(id)
	if err != nil {
		return appwire.Agent{}, appTakeError(err)
	}
	// A local display replica may still be catching up to the acquired receipt.
	if agent.Location != nil {
		fresh := *agent.Location
		fresh.Cell = p.Cell
		fresh.Here = true
		fresh.Seen = location.Seen
		fresh.Holder = location.Holder
		fresh.CanTake = false
		fresh.Line = ""
		location = &fresh
	}
	b.located(a, location)
	agent.Location = location
	return agent, nil
}

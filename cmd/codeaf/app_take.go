package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/guard"
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
	if errors.As(err, &wire) {
		if wire.Code != appwire.CodeEngine {
			return wire
		}
		message := appTakeWords(wire.Message)
		if strings.HasPrefix(message, "This chat used ") {
			return &appwire.Error{Code: wire.Code, Message: message, Detail: appTakeDetail(err)}
		}
	}
	var missing *continuedModelUnavailable
	if errors.As(err, &missing) {
		return &appwire.Error{Code: appwire.CodeEngine, Message: missing.Error(), Detail: fmt.Sprintf("model %q, provider %q: no usable local default model", missing.model, missing.provider)}
	}
	return &appwire.Error{Code: appwire.CodeEngine, Message: appTakeFailed, Detail: appTakeDetail(err)}
}

func appTakeDetail(err error) any {
	var wire *appwire.Error
	detail := err.Error()
	if errors.As(err, &wire) && wire.Detail != nil {
		if words, ok := wire.Detail.(string); ok {
			detail = words
		} else {
			return wire.Detail
		}
	}
	return appTakeWords(detail)
}

// Loopback and app codecs can each have added their own engine label.
func appTakeWords(words string) string {
	const prefix = appwire.CodeEngine + ": "
	for strings.HasPrefix(words, prefix) {
		words = strings.TrimPrefix(words, prefix)
	}
	return words
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
	guard.Go("app/take-cancel", func() {
		select {
		case <-b.left:
			cancel()
		case <-ctx.Done():
		}
	})
	// Check even the already-here fast path against what the person saw.
	view, err := s.Dir.Cell(ctx, p.Cell)
	if err != nil {
		return appwire.Agent{}, appTakeError(err)
	}
	seen := &directory.LeaseExpectation{Device: p.Seen.Device, Fence: p.Seen.Fence, Head: p.Seen.Head}
	if !seen.Matches(view.Cell) {
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
		releaseForTake := func() error {
			existing.gate.Lock()
			defer existing.gate.Unlock()
			busy := false
			b.locked(func() {
				busy = existing.opening || existing.starting || existing.live != nil && (existing.live.running || existing.live.needsYou())
			})
			if busy {
				return appwire.Errorf(appwire.CodeBusy, "Finish this chat's work before continuing it here.")
			}
			if view.Cell.Lease.Device != s.Device.ID() {
				b.release(existing)
			}
			return nil
		}
		if err := releaseForTake(); err != nil {
			return appwire.Agent{}, err
		}
	}
	got, err := chatContinuer(s, nil).TakeSeen(ctx, p.Cell, seen)
	if err != nil {
		return appwire.Agent{}, appTakeError(err)
	}
	// The continuer's already-here shortcut may observe a local acquisition
	// that happened after our preflight read. It must not accept older consent.
	if got.AlreadyHere && (p.Seen.Device != s.Device.ID() || !seen.Matches(directory.Cell{Head: got.Taken.Driving.Head, Lease: directory.Lease{Device: s.Device.ID(), Fence: got.Taken.Driving.Fence}})) {
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
	b.remember(session.SessionRow{ID: id, Dir: root, Transcript: transcript, Workspace: meta.Workspace, Title: meta.Title, Created: meta.Created, Model: meta.Model, ModelIdentity: meta.ModelIdentity})
	a := b.agent(id)
	b.locked(func() { a.transcript = transcript; a.windowMoved = false })
	location := &appwire.Location{Here: true, Cell: p.Cell, Seen: &appwire.Seen{Device: s.Device.ID(), Fence: got.Taken.Driving.Fence, Head: got.Taken.Driving.Head}, Holder: &appwire.LocationHolder{ID: s.Device.ID(), Name: got.Device}}
	if service, ok := s.Dir.(interface{ HeadTakes() bool }); ok && !service.HeadTakes() {
		location.Seen.Head = ""
	}
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
	// Acquisition may have been observed before installation and admission.
	// Reconcile the local id even when no later directory version will arrive.
	reconcileInventory := func() error {
		b.chats.op.Lock()
		defer b.chats.op.Unlock()
		_, err := b.readChats(ctx, s, b.chats.feed, true)
		return err
	}
	err = reconcileInventory()
	if err != nil {
		// The take has completed. Inventory recovery belongs to its follower.
		b.startChatInventory()
	}
	return agent, nil
}

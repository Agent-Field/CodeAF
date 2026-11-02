package main

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// closingWithASavedProposal is a backend closing before its crew run ever
// started, with one proposal saved: the moment a proposal has no run to order
// its changes with.
func closingWithASavedProposal(t *testing.T) (*appBackend, appwire.Agent) {
	t.Helper()
	backend, _, manager := proposingBackend(t, &proposingConversation{fakeConversation: &fakeConversation{}})
	if err := backend.crewLayer().write(func(f *appCrewLayerFile) error {
		if f.Proposals == nil {
			f.Proposals = map[string]*appwire.Proposal{}
		}
		f.Proposals["prop1"] = &appwire.Proposal{ID: "prop1", Agent: manager.ID, Name: "helpers"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	backend.locked(func() { backend.closing = true })
	return backend, manager
}

// Later refuses rather than crashing the engine.
func TestAppCrewProposalLaterWhileClosingRefuses(t *testing.T) {
	backend, manager := closingWithASavedProposal(t)
	err := backend.LaterInbox(manager.ID+"/"+appProposalAskPrefix+"prop1", true)
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("Later while closing answered %v, want not_found", err)
	}
}

// An answer still forgets the proposal, so it does not come back at the next
// launch as a question already answered.
func TestAppCrewProposalAnsweredWhileClosingStaysForgotten(t *testing.T) {
	backend, _ := closingWithASavedProposal(t)
	backend.dropProposal("prop1", appwire.ProposalKeep)
	var saved *appwire.Proposal
	if err := backend.crewLayer().read(func(f *appCrewLayerFile) { saved = f.Proposals["prop1"] }); err != nil {
		t.Fatal(err)
	}
	if saved != nil {
		t.Fatal("an answered proposal stayed saved and would reopen at the next launch")
	}
}

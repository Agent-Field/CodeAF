package main

import (
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) AppIdentityCandidates(_ appwire.Empty) (*appwire.IdentityCandidatesResult, error) {
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	list, err := s.IdentityCandidates(data.Scope{})
	return &appwire.IdentityCandidatesResult{Candidates: list}, appDataError(err)
}

func (b *appBackend) AppIdentityScan(p appwire.IdentityScanParams) (*appwire.Empty, error) {
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	return &appwire.Empty{}, appDataError(s.ScanIdentity(data.Scope{}, p.Type, p.ID, p.Fields))
}

func (b *appBackend) AppIdentityAccept(p appwire.IdentityDecisionParams) (*appwire.IdentityDecisionResult, error) {
	return b.appIdentityDecision(p, false)
}

func (b *appBackend) AppIdentityReject(p appwire.IdentityDecisionParams) (*appwire.IdentityDecisionResult, error) {
	return b.appIdentityDecision(p, true)
}

func (b *appBackend) appIdentityDecision(p appwire.IdentityDecisionParams, separate bool) (*appwire.IdentityDecisionResult, error) {
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	match, err := s.DecideIdentityMatch(data.Scope{}, p.ID, separate, p.Survivor)
	return &appwire.IdentityDecisionResult{Merge: match}, appDataError(err)
}

func (b *appBackend) AppIdentityMerge(p appwire.IdentityMergeParams) (*appwire.IdentityDecisionResult, error) {
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	merge, err := s.MergeIdentity(data.Scope{}, p.Canonical, p.Alias, p.CanonicalRevision, p.AliasRevision)
	return &appwire.IdentityDecisionResult{Merge: merge}, appDataError(err)
}

func (b *appBackend) AppIdentityUnmerge(p appwire.IdentityUnmergeParams) (*appwire.Empty, error) {
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	return &appwire.Empty{}, appDataError(s.UnmergeIdentity(data.Scope{}, p.ID, p.Revision))
}

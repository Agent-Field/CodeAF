package appwire

import "github.com/Agent-Field/codeaf/internal/apps/data"

type IdentityCandidatesResult struct {
	Candidates []data.IdentityCandidate `json:"candidates"`
}

type IdentityScanParams struct {
	Type   string              `json:"type"`
	ID     string              `json:"id"`
	Fields data.IdentityFields `json:"fields,omitzero"`
}

type IdentityDecisionParams struct {
	ID       string `json:"id"`
	Survivor string `json:"survivor,omitempty"`
}

type IdentityMergeParams struct {
	Canonical         string `json:"canonical"`
	Alias             string `json:"alias"`
	CanonicalRevision int64  `json:"canonical_revision"`
	AliasRevision     int64  `json:"alias_revision"`
}

type IdentityUnmergeParams struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

type IdentityDecisionResult struct {
	Merge *data.IdentityMerge `json:"merge,omitempty"`
}

type appIdentityCalls interface {
	AppIdentityCandidates(Empty) (*IdentityCandidatesResult, error)
	AppIdentityScan(IdentityScanParams) (*Empty, error)
	AppIdentityAccept(IdentityDecisionParams) (*IdentityDecisionResult, error)
	AppIdentityReject(IdentityDecisionParams) (*IdentityDecisionResult, error)
	AppIdentityMerge(IdentityMergeParams) (*IdentityDecisionResult, error)
	AppIdentityUnmerge(IdentityUnmergeParams) (*Empty, error)
}

func identityCall[P, R any](call func(appIdentityCalls, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		d, ok := e.(appIdentityCalls)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not handle identity reviews.")
		}
		return call(d, p)
	})
}

func init() {
	methods["apps.match_candidates"] = identityCall(appIdentityCalls.AppIdentityCandidates)
	methods["apps.match_scan"] = identityCall(appIdentityCalls.AppIdentityScan)
	methods["apps.match_accept"] = identityCall(appIdentityCalls.AppIdentityAccept)
	methods["apps.match_reject"] = identityCall(appIdentityCalls.AppIdentityReject)
	methods["apps.match_merge"] = identityCall(appIdentityCalls.AppIdentityMerge)
	methods["apps.match_unmerge"] = identityCall(appIdentityCalls.AppIdentityUnmerge)
}

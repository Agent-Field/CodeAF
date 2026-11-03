package main

import "github.com/Agent-Field/codeaf/internal/appwire"

var _ appwire.Workstreamer = (*appBackend)(nil)

// WP-1 makes this true when the store lands.
func (b *appBackend) ServesWorkstreams() bool { return false }

func (b *appBackend) WorkstreamList(appwire.WorkstreamListParams) (appwire.WorkstreamListResult, error) {
	return appwire.WorkstreamListResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamGet(appwire.WorkstreamGetParams) (appwire.WorkstreamGetResult, error) {
	return appwire.WorkstreamGetResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamCreate(appwire.WorkstreamCreateParams) (appwire.WorkstreamCreateResult, error) {
	return appwire.WorkstreamCreateResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamUpdate(appwire.WorkstreamUpdateParams) (appwire.WorkstreamUpdateResult, error) {
	return appwire.WorkstreamUpdateResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams) (appwire.WorkstreamMarkDoneResult, error) {
	return appwire.WorkstreamMarkDoneResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamReopen(appwire.WorkstreamReopenParams) (appwire.WorkstreamReopenResult, error) {
	return appwire.WorkstreamReopenResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPlanGet(appwire.WorkstreamPlanGetParams) (appwire.WorkstreamPlanGetResult, error) {
	return appwire.WorkstreamPlanGetResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPlanSet(appwire.WorkstreamPlanSetParams) (appwire.WorkstreamPlanSetResult, error) {
	return appwire.WorkstreamPlanSetResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageList(appwire.WorkstreamPageListParams) (appwire.WorkstreamPageListResult, error) {
	return appwire.WorkstreamPageListResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageGet(appwire.WorkstreamPageGetParams) (appwire.WorkstreamPageGetResult, error) {
	return appwire.WorkstreamPageGetResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageSave(appwire.WorkstreamPageSaveParams) (appwire.WorkstreamPageSaveResult, error) {
	return appwire.WorkstreamPageSaveResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageCreate(appwire.WorkstreamPageCreateParams) (appwire.WorkstreamPageCreateResult, error) {
	return appwire.WorkstreamPageCreateResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageUpdate(appwire.WorkstreamPageUpdateParams) (appwire.WorkstreamPageUpdateResult, error) {
	return appwire.WorkstreamPageUpdateResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageMove(appwire.WorkstreamPageMoveParams) (appwire.WorkstreamPageMoveResult, error) {
	return appwire.WorkstreamPageMoveResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageDelete(appwire.WorkstreamPageDeleteParams) (appwire.WorkstreamPageDeleteResult, error) {
	return appwire.WorkstreamPageDeleteResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageHistory(appwire.WorkstreamPageHistoryParams) (appwire.WorkstreamPageHistoryResult, error) {
	return appwire.WorkstreamPageHistoryResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamPageRestore(appwire.WorkstreamPageRestoreParams) (appwire.WorkstreamPageRestoreResult, error) {
	return appwire.WorkstreamPageRestoreResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamRecordList(appwire.WorkstreamRecordListParams) (appwire.WorkstreamRecordListResult, error) {
	return appwire.WorkstreamRecordListResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams) (appwire.WorkstreamRecordProposeResult, error) {
	return appwire.WorkstreamRecordProposeResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams) (appwire.WorkstreamRecordKeepResult, error) {
	return appwire.WorkstreamRecordKeepResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamRecordDrop(appwire.WorkstreamRecordDropParams) (appwire.WorkstreamRecordDropResult, error) {
	return appwire.WorkstreamRecordDropResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamRecordSupersede(appwire.WorkstreamRecordSupersedeParams) (appwire.WorkstreamRecordSupersedeResult, error) {
	return appwire.WorkstreamRecordSupersedeResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamGoAheadList(appwire.WorkstreamGoAheadListParams) (appwire.WorkstreamGoAheadListResult, error) {
	return appwire.WorkstreamGoAheadListResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamGoAheadGrant(appwire.WorkstreamGoAheadGrantParams) (appwire.WorkstreamGoAheadGrantResult, error) {
	return appwire.WorkstreamGoAheadGrantResult{}, appwire.ErrWorkstreamsNotYet
}

func (b *appBackend) WorkstreamGoAheadRevoke(appwire.WorkstreamGoAheadRevokeParams) (appwire.WorkstreamGoAheadRevokeResult, error) {
	return appwire.WorkstreamGoAheadRevokeResult{}, appwire.ErrWorkstreamsNotYet
}

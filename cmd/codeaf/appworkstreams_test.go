package main

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// V4: the real backend implements every contract stub without touching its fields.
func TestAppWorkstreamsAreNotYetServed(t *testing.T) {
	var backend appwire.Workstreamer = &appBackend{}
	if backend.ServesWorkstreams() {
		t.Fatal("the stub backend claims workstreams")
	}
	calls := []struct {
		name string
		call func() error
	}{
		{"workstream.list", func() error {
			_, err := backend.WorkstreamList(appwire.WorkstreamListParams{})
			return err
		}},
		{"workstream.get", func() error {
			_, err := backend.WorkstreamGet(appwire.WorkstreamGetParams{})
			return err
		}},
		{"workstream.create", func() error {
			_, err := backend.WorkstreamCreate(appwire.WorkstreamCreateParams{})
			return err
		}},
		{"workstream.update", func() error {
			_, err := backend.WorkstreamUpdate(appwire.WorkstreamUpdateParams{})
			return err
		}},
		{"workstream.mark_done", func() error {
			_, err := backend.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{})
			return err
		}},
		{"workstream.reopen", func() error {
			_, err := backend.WorkstreamReopen(appwire.WorkstreamReopenParams{})
			return err
		}},
		{"workstream.plan_get", func() error {
			_, err := backend.WorkstreamPlanGet(appwire.WorkstreamPlanGetParams{})
			return err
		}},
		{"workstream.plan_set", func() error {
			_, err := backend.WorkstreamPlanSet(appwire.WorkstreamPlanSetParams{})
			return err
		}},
		{"page.list", func() error {
			_, err := backend.WorkstreamPageList(appwire.WorkstreamPageListParams{})
			return err
		}},
		{"page.get", func() error {
			_, err := backend.WorkstreamPageGet(appwire.WorkstreamPageGetParams{})
			return err
		}},
		{"page.save", func() error {
			_, err := backend.WorkstreamPageSave(appwire.WorkstreamPageSaveParams{})
			return err
		}},
		{"page.create", func() error {
			_, err := backend.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{})
			return err
		}},
		{"page.update", func() error {
			_, err := backend.WorkstreamPageUpdate(appwire.WorkstreamPageUpdateParams{})
			return err
		}},
		{"page.move", func() error {
			_, err := backend.WorkstreamPageMove(appwire.WorkstreamPageMoveParams{})
			return err
		}},
		{"page.delete", func() error {
			_, err := backend.WorkstreamPageDelete(appwire.WorkstreamPageDeleteParams{})
			return err
		}},
		{"page.history", func() error {
			_, err := backend.WorkstreamPageHistory(appwire.WorkstreamPageHistoryParams{})
			return err
		}},
		{"page.restore", func() error {
			_, err := backend.WorkstreamPageRestore(appwire.WorkstreamPageRestoreParams{})
			return err
		}},
		{"record.list", func() error {
			_, err := backend.WorkstreamRecordList(appwire.WorkstreamRecordListParams{})
			return err
		}},
		{"record.propose", func() error {
			_, err := backend.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{})
			return err
		}},
		{"record.keep", func() error {
			_, err := backend.WorkstreamRecordKeep(appwire.WorkstreamRecordKeepParams{})
			return err
		}},
		{"record.drop", func() error {
			_, err := backend.WorkstreamRecordDrop(appwire.WorkstreamRecordDropParams{})
			return err
		}},
		{"record.supersede", func() error {
			_, err := backend.WorkstreamRecordSupersede(appwire.WorkstreamRecordSupersedeParams{})
			return err
		}},
		{"goahead.list", func() error {
			_, err := backend.WorkstreamGoAheadList(appwire.WorkstreamGoAheadListParams{})
			return err
		}},
		{"goahead.grant", func() error {
			_, err := backend.WorkstreamGoAheadGrant(appwire.WorkstreamGoAheadGrantParams{})
			return err
		}},
		{"goahead.revoke", func() error {
			_, err := backend.WorkstreamGoAheadRevoke(appwire.WorkstreamGoAheadRevokeParams{})
			return err
		}},
	}
	if len(calls) != len(appwire.WorkstreamMethods) {
		t.Fatal("stub test does not cover every contract method")
	}
	for i, call := range calls {
		if call.name != appwire.WorkstreamMethods[i] {
			t.Fatalf("stub test index differs at %s", call.name)
		}
		t.Run(call.name, func(t *testing.T) {
			err := call.call()
			var wireErr *appwire.Error
			if !errors.As(err, &wireErr) {
				t.Fatal("stub must return an appwire.Error")
			}
			if wireErr.Code != appwire.CodeUnknownMethod {
				t.Errorf("code = %s, want unknown_method", wireErr.Code)
			}
			if wireErr.Message != "workstreams: not implemented yet" {
				t.Errorf("message = %s", wireErr.Message)
			}
		})
	}
}

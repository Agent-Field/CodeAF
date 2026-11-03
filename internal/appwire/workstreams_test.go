package appwire

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fakeWorkstreamer exercises V4 without a store, Factory or engine binary.
type fakeWorkstreamer struct {
	*fakeEngine
	serves bool
}

var _ Workstreamer = (*fakeWorkstreamer)(nil)

func (f *fakeWorkstreamer) ServesWorkstreams() bool { return f.serves }

func (f *fakeWorkstreamer) WorkstreamList(WorkstreamListParams) (WorkstreamListResult, error) {
	return WorkstreamListResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamGet(WorkstreamGetParams) (WorkstreamGetResult, error) {
	return WorkstreamGetResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamCreate(WorkstreamCreateParams) (WorkstreamCreateResult, error) {
	return WorkstreamCreateResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamUpdate(WorkstreamUpdateParams) (WorkstreamUpdateResult, error) {
	return WorkstreamUpdateResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamMarkDone(WorkstreamMarkDoneParams) (WorkstreamMarkDoneResult, error) {
	return WorkstreamMarkDoneResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamReopen(WorkstreamReopenParams) (WorkstreamReopenResult, error) {
	return WorkstreamReopenResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPlanGet(WorkstreamPlanGetParams) (WorkstreamPlanGetResult, error) {
	return WorkstreamPlanGetResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPlanSet(WorkstreamPlanSetParams) (WorkstreamPlanSetResult, error) {
	return WorkstreamPlanSetResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageList(WorkstreamPageListParams) (WorkstreamPageListResult, error) {
	return WorkstreamPageListResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageGet(WorkstreamPageGetParams) (WorkstreamPageGetResult, error) {
	return WorkstreamPageGetResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageSave(WorkstreamPageSaveParams) (WorkstreamPageSaveResult, error) {
	return WorkstreamPageSaveResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageCreate(WorkstreamPageCreateParams) (WorkstreamPageCreateResult, error) {
	return WorkstreamPageCreateResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageUpdate(WorkstreamPageUpdateParams) (WorkstreamPageUpdateResult, error) {
	return WorkstreamPageUpdateResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageMove(WorkstreamPageMoveParams) (WorkstreamPageMoveResult, error) {
	return WorkstreamPageMoveResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageDelete(WorkstreamPageDeleteParams) (WorkstreamPageDeleteResult, error) {
	return WorkstreamPageDeleteResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageHistory(WorkstreamPageHistoryParams) (WorkstreamPageHistoryResult, error) {
	return WorkstreamPageHistoryResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamPageRestore(WorkstreamPageRestoreParams) (WorkstreamPageRestoreResult, error) {
	return WorkstreamPageRestoreResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamRecordList(WorkstreamRecordListParams) (WorkstreamRecordListResult, error) {
	return WorkstreamRecordListResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamRecordPropose(WorkstreamRecordProposeParams) (WorkstreamRecordProposeResult, error) {
	return WorkstreamRecordProposeResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamRecordKeep(WorkstreamRecordKeepParams) (WorkstreamRecordKeepResult, error) {
	return WorkstreamRecordKeepResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamRecordDrop(WorkstreamRecordDropParams) (WorkstreamRecordDropResult, error) {
	return WorkstreamRecordDropResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamRecordSupersede(WorkstreamRecordSupersedeParams) (WorkstreamRecordSupersedeResult, error) {
	return WorkstreamRecordSupersedeResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamGoAheadList(WorkstreamGoAheadListParams) (WorkstreamGoAheadListResult, error) {
	return WorkstreamGoAheadListResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamGoAheadGrant(WorkstreamGoAheadGrantParams) (WorkstreamGoAheadGrantResult, error) {
	return WorkstreamGoAheadGrantResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamGoAheadRevoke(WorkstreamGoAheadRevokeParams) (WorkstreamGoAheadRevokeResult, error) {
	return WorkstreamGoAheadRevokeResult{}, ErrWorkstreamsNotYet
}

func (f *fakeWorkstreamer) WorkstreamSourceList(WorkstreamSourceListParams) (WorkstreamSourceListResult, error) {
	return WorkstreamSourceListResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamSourceAdd(WorkstreamSourceAddParams) (WorkstreamSourceAddResult, error) {
	return WorkstreamSourceAddResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamSourceUpdate(WorkstreamSourceUpdateParams) (WorkstreamSourceUpdateResult, error) {
	return WorkstreamSourceUpdateResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamSourceRemove(WorkstreamSourceRemoveParams) (WorkstreamSourceRemoveResult, error) {
	return WorkstreamSourceRemoveResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamSuggestionList(WorkstreamSuggestionListParams) (WorkstreamSuggestionListResult, error) {
	return WorkstreamSuggestionListResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamSuggestionKeep(WorkstreamSuggestionKeepParams) (WorkstreamSuggestionKeepResult, error) {
	return WorkstreamSuggestionKeepResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamSuggestionDrop(WorkstreamSuggestionDropParams) (WorkstreamSuggestionDropResult, error) {
	return WorkstreamSuggestionDropResult{}, ErrWorkstreamsNotYet
}

func workstreamHelloHasFeature(hello map[string]any) bool {
	for _, feature := range features(hello) {
		if feature == FeatureWorkstreams {
			return true
		}
	}
	return false
}

// V1, WP-12, WP-6d: the contract index and dispatch table name the same 36 methods.
func TestEveryWorkstreamMethodIsRegistered(t *testing.T) {
	if len(WorkstreamMethods) != 36 {
		t.Fatalf("contract has %d methods, want 36", len(WorkstreamMethods))
	}
	known := make(map[string]bool, len(WorkstreamMethods))
	served := Methods()
	for _, name := range WorkstreamMethods {
		if known[name] {
			t.Errorf("duplicate contract method %s", name)
		}
		known[name] = true
		if !slices.Contains(served, name) {
			t.Errorf("method not registered: %s", name)
		}
	}
	for _, name := range served {
		for _, prefix := range []string{"workstream.", "page.", "record.", "goahead.", "wake.", "source.", "suggestion."} {
			if strings.HasPrefix(name, prefix) && !known[name] {
				t.Errorf("registered method not in contract: %s", name)
			}
		}
	}
	if !slices.Contains(Events(), EventWorkstreamChanged) {
		t.Error("workstream.changed is not registered")
	}
}

// V4: every registered stub refuses on the wire, without claiming the feature.
func TestWorkstreamMethodsAnswerNotYet(t *testing.T) {
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}}
	w := startWireOver(t, fake, fake.fakeEngine)
	if workstreamHelloHasFeature(w.next()) {
		t.Fatal("the stub claims workstreams")
	}
	for i, name := range WorkstreamMethods {
		t.Run(name, func(t *testing.T) {
			w.send(fmt.Sprintf(`{"id":%d,"method":%q,"params":{}}`, i+1, name))
			got := w.next()
			if got["id"] != float64(i+1) {
				t.Fatal("response id does not match request")
			}
			err, ok := got["error"].(map[string]any)
			if !ok {
				t.Fatal("stub response has no error")
			}
			if err["code"] != CodeUnknownMethod {
				t.Errorf("code = %v, want unknown_method", err["code"])
			}
			if err["message"] != "workstreams: not implemented yet" {
				t.Errorf("message = %v", err["message"])
			}
		})
	}
}

// V4: both interface support and ServesWorkstreams are required for hello.
func TestWorkstreamsFeatureFollowsTheEngine(t *testing.T) {
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	w := startWireOver(t, fake, fake.fakeEngine)
	if !workstreamHelloHasFeature(w.next()) {
		t.Fatal("serving engine does not announce workstreams")
	}
	plain := startWire(t, &fakeEngine{})
	if workstreamHelloHasFeature(plain.next()) {
		t.Fatal("plain engine claims workstreams")
	}
	plain.send(`{"id":1,"method":"workstream.list","params":{}}`)
	got := plain.next()
	err, ok := got["error"].(map[string]any)
	if !ok || err["code"] != CodeUnknownMethod {
		t.Fatal("plain engine does not answer unknown_method")
	}
	if err["message"] != "this engine does not serve workstreams" {
		t.Errorf("plain engine message = %v", err["message"])
	}
	hello := withWorkstreamsFeature(Hello{}, fake)
	hello = withWorkstreamsFeature(hello, fake)
	if len(hello.Features) != 2 || hello.Features[0] != FeatureWorkstreams || hello.Features[1] != FeatureWorkstreamSources {
		t.Fatal("feature hook must add workstreams and sources exactly once")
	}
}

// V1: typed handlers and event registration expose the complete contract schema.
func TestWorkstreamSchemaNamesEveryMethod(t *testing.T) {
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]json.RawMessage `json:"methods"`
		Events  map[string]json.RawMessage `json:"events"`
		Defs    map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range WorkstreamMethods {
		if doc.Methods[name] == nil {
			t.Errorf("schema lacks %s", name)
		}
	}
	if doc.Events[EventWorkstreamChanged] == nil {
		t.Error("schema lacks workstream.changed")
	}
	words, _ := doc.Defs["Workstream"].Properties["stage"]["enum"].([]any)
	if len(words) != 3 {
		t.Fatalf("Workstream.stage has %d words, want 3", len(words))
	}
	for i, want := range []string{"shaping", "building", "done"} {
		if words[i] != want {
			t.Errorf("Workstream.stage word %d = %v, want %s", i, words[i], want)
		}
	}
}

func (f *fakeWorkstreamer) WorkstreamWakeList(WorkstreamWakeListParams) (WorkstreamWakeListResult, error) {
	return WorkstreamWakeListResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamWakeApply(WorkstreamWakeApplyParams) (WorkstreamWakeApplyResult, error) {
	return WorkstreamWakeApplyResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamWakeUndo(WorkstreamWakeUndoParams) (WorkstreamWakeUndoResult, error) {
	return WorkstreamWakeUndoResult{}, ErrWorkstreamsNotYet
}
func (f *fakeWorkstreamer) WorkstreamWakeNotNow(WorkstreamWakeNotNowParams) (WorkstreamWakeNotNowResult, error) {
	return WorkstreamWakeNotNowResult{}, ErrWorkstreamsNotYet
}

func TestWakeJ_ParameterBounds(t *testing.T) {
	for _, p := range []interface{ Check() error }{WorkstreamWakeListParams{}, WorkstreamWakeApplyParams{}, WorkstreamWakeUndoParams{}, WorkstreamWakeNotNowParams{}, WorkstreamWakeListParams{WorkstreamID: "w", Limit: workstreamWireInt(101)}, WorkstreamWakeApplyParams{ID: "w", ItemID: workstreamWireString("")}} {
		if p.Check() == nil {
			t.Errorf("accepted %+v", p)
		}
	}
	for _, p := range []interface{ Check() error }{WorkstreamWakeListParams{WorkstreamID: "w"}, WorkstreamWakeApplyParams{ID: "w"}, WorkstreamWakeUndoParams{ID: "w"}, WorkstreamWakeNotNowParams{ID: "w"}} {
		if err := p.Check(); err != nil {
			t.Error(err)
		}
	}
}
func workstreamWireInt(n int) *int          { return &n }
func workstreamWireString(s string) *string { return &s }

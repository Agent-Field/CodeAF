package tui3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestCustomAddressFlowChecksBeforeAskingForAKey(t *testing.T) {
	for _, status := range []int{200, 401, 403, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Errorf("probe path %q", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("initial probe leaked credentials")
				}
				w.WriteHeader(status)
				w.Write([]byte(`{"data":[{"id":"sample"}]}`))
			}))
			defer server.Close()
			a := modelServiceTestApp(t, t.TempDir(), "sample", modelsource.NewSet(), nil)
			a.openAddProvider(false)
			for i, item := range a.addPanel.items {
				if item.custom {
					a.addPanel.cursor = i
					break
				}
			}
			a.addPanelKey(key("enter"))
			if a.addPanel.entry == nil {
				t.Fatal("custom address entry is not in the visible panel")
			}
			a.addPanel.entry.box.setText(server.URL + "/v1")
			cmd := a.addPanelKey(key("enter"))
			if cmd == nil || a.modelDraft.step != modelConnectAddress {
				t.Fatal("address was not checked asynchronously")
			}
			a.Update(cmd())
			if status == 500 {
				if a.modelDraft.step != modelConnectAddress || a.addPanel.entry == nil || a.addPanel.err == "" {
					t.Fatal("failed address did not remain editable with an error")
				}
				return
			}
			if a.modelDraft.step != modelConnectName {
				t.Fatal("live address did not reach name")
			}
			cmd = a.addPanelKey(key("enter"))
			if status == 200 {
				if cmd == nil || a.modelDraft != nil || a.addPanel.entry != nil {
					t.Fatal("public endpoint asked for a key instead of connecting")
				}
				result := cmd().(modelConnectResultMsg)
				if result.err != nil || result.outcome.Kind != modelsource.OutcomeConnected {
					t.Fatalf("anonymous connection failed: %+v", result.outcome)
				}
				a.Update(result)
				connected, found := config.ResolveSources(a.profileDir, "", config.DefaultBaseURL).ByID(modelsource.CustomID)
				if !found || !connected.Source.KeyOptional || connected.Key != "" {
					t.Fatal("anonymous connection did not survive source reload")
				}

			} else if a.modelDraft == nil || a.modelDraft.step != modelConnectKey || a.addPanel.entry == nil || !a.addPanel.entry.secret {
				t.Fatal("401/403 did not ask for a key")
			}
		})
	}
}

func TestCustomAddressFlowIgnoresCancelledEditedAndSupersededAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[]}`)) }))
	defer server.Close()
	for _, action := range []string{"cancel", "edit", "replace", "resubmit"} {
		t.Run(action, func(t *testing.T) {
			a := modelServiceTestApp(t, t.TempDir(), "sample", modelsource.NewSet(), nil)
			a.addPanel.open = true
			a.startCustomAdd(false)
			a.addPanel.entry.box.setText(server.URL + "/v1")
			old := a.addPanelKey(key("enter"))()
			switch action {
			case "cancel":
				a.addPanelKey(key("esc"))
			case "edit":
				a.addPanel.entry.box.setText(server.URL + "/other")
			case "replace":
				a.startCustomAdd(false)
			case "resubmit":
				a.addPanelKey(key("enter"))
			}
			a.Update(old)
			if action == "cancel" {
				if a.addPanel.entry != nil || a.modelDraft != nil {
					t.Fatal("cancelled check reopened its entry")
				}
			} else if a.modelDraft.step != modelConnectAddress {
				t.Fatal("stale result advanced the replacement answer")
			}
		})
	}
}

func TestCustomAddressFlowPrefillsTheDiscoveredRow(t *testing.T) {
	a := modelServiceTestApp(t, t.TempDir(), "sample", modelsource.NewSet(), nil)
	a.openAddProvider(false)
	a.addPanel.loading = false
	a.addPanel.rebuild([]LocalServerProbe{{Name: "local", Address: "http://127.0.0.1:9999/v1"}}, nil)
	for i, item := range a.addPanel.items {
		if item.probe != nil {
			a.addPanel.cursor = i
			break
		}
	}
	a.addPanelKey(key("enter"))
	if !a.addPanel.open || a.addPanel.entry == nil || a.addPanel.entry.value() != "http://127.0.0.1:9999/v1" {
		t.Fatal("selecting the discovered server did not open its prefilled address")
	}
}

func TestCustomAddressEditKeepsCredentialsWhenListingIsPublic(t *testing.T) {
	a := modelServiceTestApp(t, t.TempDir(), "sample", modelsource.NewSet(), nil)
	a.addPanel.open = true
	a.startCustomAdd(false)
	draft := a.modelDraft
	draft.editing = true
	draft.row.Address = "http://127.0.0.1:9999/v1"
	draft.row.KeyEnv = "MY_INFERENCE_KEY"
	draft.step = modelConnectName
	entry := newModelEntry(draft.entryID, draft.source.Name, "name", nil, false)
	entry.box.setText("local")
	cmd := a.modelEntryAnswer(entry)
	if cmd == nil || draft.row.KeyEnv != "MY_INFERENCE_KEY" {
		t.Fatal("public model listing erased existing inference credentials")
	}
}

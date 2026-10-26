package session_test

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePolicySurvivesMetadataUpdates(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	var meta session.Meta
	if err := json.Unmarshal([]byte(`{"id":"policy-chat","workspace_policy":{"version":1,"mode":"private-tasks"}}`), &meta); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: "policy-chat", Title: "later metadata stamp"}); err != nil {
		t.Fatal(err)
	}
	got, err := session.LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["workspace_policy"]) != `{"version":1,"mode":"private-tasks"}` {
		t.Fatalf("policy did not survive metadata roundtrip: %s", raw)
	}
}

func TestWorkspacePolicyActivatesOnlyRecordedConversations(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	for _, check := range []struct {
		name   string
		policy *session.WorkspacePolicy
		chat   string
		want   bool
	}{
		{"new conversation", session.DefaultWorkspacePolicy(), "", true},
		{"legacy conversation", nil, "", false},
		{"identity without policy", nil, "codeaf-workspace://test/chat", false},
	} {
		t.Run(check.name, func(t *testing.T) {
			place := session.Place{Dir: t.TempDir()}
			if err := session.SaveMeta(place.Dir, session.Meta{ID: "policy-chat", WorkspacePolicy: check.policy, ChatURI: check.chat}); err != nil {
				t.Fatal(err)
			}
			if got := session.WorkspaceOwnershipEnabled(place); got != check.want {
				t.Fatalf("recorded conversation enabled=%v want %v", got, check.want)
			}
		})
	}
}

func TestWorkspacePolicyRejectsUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	err := session.SaveMeta(dir, session.Meta{ID: "future-policy", WorkspacePolicy: &session.WorkspacePolicy{Version: 2, Mode: "private-tasks"}})
	if err == nil {
		t.Fatal("unsupported policy was silently admitted")
	}
}

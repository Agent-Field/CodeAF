package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestTransferredProjectIdentityReusesExistingLocalCache(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	source, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := source.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := receiver.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before, err := ownedProjectCache(receiver, local.URI)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(before, "mock-compiler-cache"), []byte("reusable mock bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Adopt(incoming, local.Path, workspaceidentity.Links{}); err != nil {
		t.Fatal(err)
	}
	after, err := ownedProjectCache(receiver, incoming.URI)
	if err != nil {
		t.Fatal(err)
	}
	if got := observedWorkerCache(t, local.Path, incoming.URI); got != before {
		t.Fatalf("actual worker did not reuse aliased cache: %s", got)
	}
	if after != before {
		t.Fatal("equivalent transferred project created a second local cache")
	}
	if raw, err := os.ReadFile(filepath.Join(after, "mock-compiler-cache")); err != nil || string(raw) != "reusable mock bytes" {
		t.Fatal("transferred project did not reuse exact existing cache bytes")
	}
	record, err := receiver.Lookup(workspaceidentity.Cache, after)
	if err != nil || record.OwnerURI != local.URI {
		t.Fatal("cache stopped belonging to the canonical local project")
	}
}

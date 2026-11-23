package workspaceidentity_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestExistingLookupNeverCreatesRegistryAndResolvesAdmittedAliasReadOnly(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent-registry")
	uri := "codeaf-workspace://11111111111111111111111111111111/22222222222222222222222222222222"
	if _, err := workspaceidentity.ResolveExisting(missing, uri); !errors.Is(err, workspaceidentity.ErrUnknown) {
		t.Fatalf("missing registry lookup: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("lookup created a registry")
	}
	root := filepath.Join(t.TempDir(), "registry")
	r, err := workspaceidentity.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	project, err := r.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	incoming := workspaceidentity.Record{URI: uri, Role: workspaceidentity.Project}
	if _, err := r.Adopt(incoming, project.Path, workspaceidentity.Links{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := workspaceidentity.ResolveExisting(root, uri)
	if err != nil || got.URI != uri || got.Path != project.Path {
		t.Fatalf("known alias missing: %+v %v", got, err)
	}
	after, err := os.ReadFile(filepath.Join(root, "registry.sqlite"))
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only lookup changed registry database")
	}
}

func TestRecordedLookupKeepsAbsentCacheIdentityAndRefusesSubstitutedLinks(t *testing.T) {
	r, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := r.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(cache, 0700); err != nil {
		t.Fatal(err)
	}
	admitted, err := r.Register(workspaceidentity.Cache, cache, workspaceidentity.Links{OwnerURI: owner.URI})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	got, err := r.LookupRecorded(workspaceidentity.Cache, cache)
	if err != nil || got != admitted {
		t.Fatalf("absent cache lost recorded identity: %+v %v", got, err)
	}
	if _, err := r.LookupRecorded(workspaceidentity.Temp, cache); err == nil {
		t.Fatal("recorded lookup ignored role")
	}
	if _, err := r.LookupRecorded(workspaceidentity.Cache, filepath.Join(filepath.Dir(cache), "unknown")); err == nil {
		t.Fatal("unknown path acquired authority")
	}
	if err := os.Symlink(t.TempDir(), cache); err != nil {
		t.Skip(err)
	}
	if _, err := r.LookupRecorded(workspaceidentity.Cache, cache); err == nil {
		t.Fatal("recorded binding followed substituted symbolic link")
	}
}

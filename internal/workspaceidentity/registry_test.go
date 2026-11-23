package workspaceidentity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestIdentitySurvivesRelocationAndRestart(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry")
	registry, err := workspaceidentity.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	original := t.TempDir()
	record, err := registry.Register(workspaceidentity.Run, original)
	if err != nil {
		t.Fatal(err)
	}
	moved := original + "-moved"
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if missing, err := registry.Resolve(record.URI); err != nil || missing.Path != original {
		t.Fatalf("cold binding lost: %#v %v", missing, err)
	}
	rebound, err := registry.Bind(record.URI, moved)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := workspaceidentity.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := reopened.Resolve(record.URI)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.URI != record.URI || actual.URI != record.URI || actual.Path != moved || actual.Role != workspaceidentity.Run {
		t.Fatalf("identity changed: %#v", actual)
	}
}

func TestForeignAndMalformedIdentitiesCannotResolveOrRebind(t *testing.T) {
	first, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := first.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{record.URI, "../../outside", record.URI + "/../other", record.URI + "?path=/", record.URI + "%2f.."} {
		if _, err := second.Resolve(uri); err == nil {
			t.Fatalf("foreign/malformed identity resolved: %q", uri)
		}
		if _, err := second.Bind(uri, t.TempDir()); err == nil {
			t.Fatalf("foreign/malformed identity rebound: %q", uri)
		}
	}
}

func TestLocalBindingDoesNotFollowAReplacedDirectoryLink(t *testing.T) {
	registry, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	original := t.TempDir()
	record, err := registry.Register(workspaceidentity.Run, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), original); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve(record.URI); err == nil {
		t.Fatal("workspace silently followed replacement link into foreign directory")
	}
}

func TestOwnershipLinksAndRepeatedAdmissionSurviveRestart(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry")
	registry, err := workspaceidentity.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, t.TempDir(), workspaceidentity.Links{OwnerURI: project.URI})
	if err != nil {
		t.Fatal(err)
	}
	runPath := t.TempDir()
	run, err := registry.Register(workspaceidentity.Run, runPath, workspaceidentity.Links{OwnerURI: chat.URI})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := workspaceidentity.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.Register(workspaceidentity.Run, runPath, workspaceidentity.Links{OwnerURI: chat.URI})
	if err != nil {
		t.Fatal(err)
	}
	if again != run || run.OwnerURI != chat.URI || run.ProjectURI != project.URI {
		t.Fatalf("ownership changed: %#v", again)
	}
	otherChat, err := registry.Register(workspaceidentity.Chat, t.TempDir(), workspaceidentity.Links{OwnerURI: project.URI})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(workspaceidentity.Run, runPath, workspaceidentity.Links{OwnerURI: otherChat.URI}); err == nil {
		t.Fatal("another chat silently acquired this run")
	}
	foreign, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Register(workspaceidentity.Run, t.TempDir(), workspaceidentity.Links{OwnerURI: chat.URI}); err == nil {
		t.Fatal("foreign owner accepted")
	}
}

func TestRebindRefusesAnotherWorkspaceBinding(t *testing.T) {
	registry, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.Register(workspaceidentity.Run, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Register(workspaceidentity.Run, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Bind(first.URI, second.Path); err == nil {
		t.Fatal("two identities acquired one run path")
	}
}

func TestRegistryRefusesPublicMetadataDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceidentity.Open(path); err == nil {
		t.Fatal("private identities accepted a public registry")
	}
}

func TestConcurrentAdmissionProducesOneIdentity(t *testing.T) {
	registryPath, runPath := filepath.Join(t.TempDir(), "registry"), t.TempDir()
	const count = 12
	records := make(chan workspaceidentity.Record, count)
	failures := make(chan error, count)
	var workers sync.WaitGroup
	for range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			registry, err := workspaceidentity.Open(registryPath)
			if err != nil {
				failures <- err
				return
			}
			record, err := registry.Register(workspaceidentity.Run, runPath)
			if err != nil {
				failures <- err
				return
			}
			records <- record
		}()
	}
	workers.Wait()
	close(records)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	uri := ""
	for record := range records {
		if uri != "" && uri != record.URI {
			t.Fatalf("concurrent admission duplicated identity: %s and %s", uri, record.URI)
		}
		uri = record.URI
	}
	if uri == "" {
		t.Fatal("no identity issued")
	}
}

func TestRolesSeparateRemoteReferencesFromLocalDirectories(t *testing.T) {
	registry, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "registry"))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []workspaceidentity.Role{workspaceidentity.Project, workspaceidentity.Chat, workspaceidentity.Run, workspaceidentity.Log, workspaceidentity.Temp, workspaceidentity.Cache} {
		record, err := registry.Register(role, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if got, err := registry.Resolve(record.URI); err != nil || got.Role != role {
			t.Fatalf("role %s: %#v %v", role, got, err)
		}
	}
	remote, err := registry.Register(workspaceidentity.ExternalRemote, "ssh://example.test/project")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := registry.Resolve(remote.URI); err != nil || got.Path != "ssh://example.test/project" {
		t.Fatalf("remote: %#v %v", got, err)
	}
	for _, path := range []string{"relative/project", "/a/../b", "file:///tmp/project", "ssh://user:password@example.test/project"} {
		if _, err := registry.Register(workspaceidentity.Run, path); err == nil {
			t.Fatalf("invalid local binding accepted: %q", path)
		}
		if _, err := registry.Register(workspaceidentity.ExternalRemote, path); err == nil {
			t.Fatalf("invalid remote binding accepted: %q", path)
		}
	}
}

func TestExplicitTransferAdoptsForeignIdentityAndKeepsItsLocalBinding(t *testing.T) {
	source, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "source"))
	if err != nil {
		t.Fatal(err)
	}
	receiverPath := filepath.Join(t.TempDir(), "receiver")
	receiver, err := workspaceidentity.Open(receiverPath)
	if err != nil {
		t.Fatal(err)
	}
	project, err := source.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chat, err := source.Register(workspaceidentity.Chat, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run, err := source.Register(workspaceidentity.Run, t.TempDir(), workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: project.URI})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Resolve(run.URI); err == nil {
		t.Fatal("foreign identity resolved before explicit adoption")
	}
	if _, err := receiver.Adopt(project, t.TempDir(), workspaceidentity.Links{}); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Adopt(chat, t.TempDir(), workspaceidentity.Links{}); err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	adopted, err := receiver.Adopt(run, local, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: project.URI})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := workspaceidentity.Open(receiverPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := reopened.Resolve(run.URI)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.URI != run.URI || resolved.Path != local || resolved.OwnerURI != chat.URI || resolved.ProjectURI != project.URI {
		t.Fatalf("transfer lost identity: %#v", resolved)
	}
	again, err := reopened.Register(workspaceidentity.Run, local, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: project.URI})
	if err != nil || again.URI != run.URI {
		t.Fatalf("admission forgot imported identity: %#v %v", again, err)
	}
}

func TestTransferRefusesMalformedIdentityAndOwnershipOrPathReplacement(t *testing.T) {
	source, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "source"))
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "receiver"))
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := source.Register(workspaceidentity.Run, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	adopted, err := receiver.Adopt(incoming, local, workspaceidentity.Links{})
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{"../../outside", incoming.URI + "/../other", incoming.URI + "?path=/", strings.ToUpper(incoming.URI)} {
		if _, err := receiver.Adopt(workspaceidentity.Record{URI: uri, Role: workspaceidentity.Run}, t.TempDir(), workspaceidentity.Links{}); err == nil {
			t.Fatalf("malformed identity imported: %q", uri)
		}
	}
	changed := incoming
	changed.Role = workspaceidentity.Cache
	if _, err := receiver.Adopt(changed, local, workspaceidentity.Links{}); err == nil {
		t.Fatal("transfer changed an existing role")
	}
	collision, err := receiver.Register(workspaceidentity.Run, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Adopt(incoming, collision.Path, workspaceidentity.Links{}); err == nil {
		t.Fatal("transfer took another run's local path")
	}
	if got, err := receiver.Resolve(adopted.URI); err != nil || got.Path != local || got.Role != workspaceidentity.Run {
		t.Fatalf("refused transfer changed admitted binding: %#v %v", got, err)
	}
}

func TestLegacyRegistryMigrationPreservesAllBindingsAndNamespace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "registry")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	scope := "0123456789abcdef0123456789abcdef"
	id := "abcdef0123456789abcdef0123456789"
	uri := "codeaf-workspace://" + scope + "/" + id
	path := t.TempDir()
	projectID, chatID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	projectURI, chatURI := "codeaf-workspace://"+scope+"/"+projectID, "codeaf-workspace://"+scope+"/"+chatID
	for _, record := range []workspaceidentity.Record{
		{URI: projectURI, Role: workspaceidentity.Project, Path: t.TempDir(), ProjectURI: projectURI},
		{URI: chatURI, Role: workspaceidentity.Chat, Path: t.TempDir()},
	} {
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.Base(record.URI)+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	legacyRaw, err := json.Marshal(workspaceidentity.Record{URI: uri, Role: workspaceidentity.Run, Path: path, OwnerURI: chatURI, ProjectURI: projectURI})
	if err != nil {
		t.Fatal(err)
	}
	legacy := string(legacyRaw)
	if err := os.WriteFile(filepath.Join(root, "namespace"), []byte(scope), 0600); err != nil {
		t.Fatal(err)
	}
	legacyFile := filepath.Join(root, id+".json")
	if err := os.WriteFile(legacyFile, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.Resolve(uri)
	if err != nil || first.Path != path || first.OwnerURI != chatURI || first.ProjectURI != projectURI {
		t.Fatalf("migration lost workspace: %#v %v", first, err)
	}
	registered, err := registry.Register(workspaceidentity.Run, path, workspaceidentity.Links{OwnerURI: chatURI, ProjectURI: projectURI})
	if err != nil || registered.URI != uri {
		t.Fatalf("migration duplicated identity: %#v %v", registered, err)
	}
	// Old receipts are retained evidence, not read authorities after successful migration.
	if err := os.WriteFile(legacyFile, []byte("corrupted obsolete receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := workspaceidentity.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.Resolve(uri); err != nil || got != first {
		t.Fatalf("old receipt corrupted committed migration: %#v %v", got, err)
	}
	next, err := reopened.Register(workspaceidentity.Cache, t.TempDir())
	if err != nil || !strings.HasPrefix(next.URI, "codeaf-workspace://"+scope+"/") {
		t.Fatalf("namespace changed: %#v %v", next, err)
	}
}

func TestFailedLegacyMigrationDoesNotResetExistingIdentities(t *testing.T) {
	root := filepath.Join(t.TempDir(), "registry")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	scope := "0123456789abcdef0123456789abcdef"
	id := "abcdef0123456789abcdef0123456789"
	if err := os.WriteFile(filepath.Join(root, "namespace"), []byte(scope), 0600); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	legacy := `{"uri":"codeaf-workspace://` + scope + `/` + id + `","role":"run","path":"` + path + `"}`
	recordFile := filepath.Join(root, id+".json")
	if err := os.WriteFile(recordFile, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(root, "ffffffffffffffffffffffffffffffff.json")
	if err := os.WriteFile(broken, []byte("broken legacy record"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceidentity.Open(root); err == nil {
		t.Fatal("corrupt legacy registry silently reset")
	}
	if raw, err := os.ReadFile(recordFile); err != nil || string(raw) != legacy {
		t.Fatalf("failed migration modified source: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "namespace")); err != nil || string(raw) != scope {
		t.Fatalf("failed migration replaced namespace: %q %v", raw, err)
	}
	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}
	repaired, err := workspaceidentity.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if record, err := repaired.Resolve("codeaf-workspace://" + scope + "/" + id); err != nil || record.Path != path {
		t.Fatalf("retry did not import original identity: %#v %v", record, err)
	}
}

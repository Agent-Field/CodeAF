package workspaceidentity_test

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestAcceptedProjectTransferKeepsExistingReceiverIdentity(t *testing.T) {
	source, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "source"))
	if err != nil {
		t.Fatal(err)
	}
	receiverPath := filepath.Join(t.TempDir(), "receiver")
	receiver, err := workspaceidentity.Open(receiverPath)
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := source.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localPath := t.TempDir()
	canonical, err := receiver.Register(workspaceidentity.Project, localPath)
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := receiver.Adopt(incoming, localPath, workspaceidentity.Links{})
	if err != nil {
		t.Fatal(err)
	}
	if adopted.URI != incoming.URI || adopted.Path != localPath || adopted.ProjectURI != incoming.URI {
		t.Fatalf("incoming identity changed: %#v", adopted)
	}
	reopened, err := workspaceidentity.Open(receiverPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{incoming.URI, canonical.URI} {
		record, err := reopened.Resolve(uri)
		if err != nil || record.URI != uri || record.Path != localPath || record.Role != workspaceidentity.Project || record.ProjectURI != uri {
			t.Fatalf("project identity disappeared: %#v %v", record, err)
		}
	}
	registered, err := reopened.Register(workspaceidentity.Project, localPath)
	if err != nil || registered != canonical {
		t.Fatalf("receiver's canonical identity changed: %#v %v", registered, err)
	}
	lookedUp, err := reopened.Lookup(workspaceidentity.Project, localPath)
	if err != nil || lookedUp != canonical {
		t.Fatalf("project path lookup is no longer canonical: %#v %v", lookedUp, err)
	}
}

func TestProjectAliasAdmissionIsRepeatableButCannotMoveItsGroup(t *testing.T) {
	source, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "source"))
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "receiver"))
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := source.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := receiver.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := receiver.Adopt(incoming, canonical.Path, workspaceidentity.Links{})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := receiver.Adopt(incoming, canonical.Path, workspaceidentity.Links{})
	if err != nil || repeated != adopted {
		t.Fatalf("repeated accepted transfer changed equivalence: %#v %v", repeated, err)
	}
	elsewhere := t.TempDir()
	for _, uri := range []string{incoming.URI, canonical.URI} {
		if _, err := receiver.Bind(uri, elsewhere); err == nil {
			t.Fatalf("individual rebind moved equivalent project %s", uri)
		}
	}
	if _, err := receiver.Adopt(incoming, elsewhere, workspaceidentity.Links{}); err == nil {
		t.Fatal("alias adoption moved its canonical project group")
	}
	if _, err := receiver.Adopt(canonical, elsewhere, workspaceidentity.Links{}); err == nil {
		t.Fatal("canonical adoption moved its aliases without a group move")
	}
	for _, uri := range []string{incoming.URI, canonical.URI} {
		record, err := receiver.Resolve(uri)
		if err != nil || record.Path != canonical.Path {
			t.Fatalf("refused move changed project: %#v %v", record, err)
		}
	}
}

func TestProjectAliasPreservesRunLinksAndRefusesOtherOwnership(t *testing.T) {
	source, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "source"))
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "receiver"))
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := source.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local, err := receiver.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chat, err := receiver.Register(workspaceidentity.Chat, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	existing, err := receiver.Register(workspaceidentity.Run, t.TempDir(), workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: local.URI})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Adopt(incoming, local.Path, workspaceidentity.Links{}); err != nil {
		t.Fatal(err)
	}
	linked, err := receiver.Register(workspaceidentity.Run, t.TempDir(), workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: incoming.URI})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []workspaceidentity.Record{existing, linked} {
		got, err := receiver.Resolve(want.URI)
		if err != nil || got != want {
			t.Fatalf("run link changed: %#v %v", got, err)
		}
	}
	foreignChat, err := source.Register(workspaceidentity.Chat, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Adopt(foreignChat, chat.Path, workspaceidentity.Links{}); err == nil {
		t.Fatal("project equivalence admitted a chat collision")
	}
	foreignRun, err := source.Register(workspaceidentity.Run, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Adopt(foreignRun, existing.Path, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: local.URI}); err == nil {
		t.Fatal("project equivalence admitted a run collision")
	}
	owned, err := receiver.Register(workspaceidentity.Project, t.TempDir(), workspaceidentity.Links{OwnerURI: chat.URI})
	if err != nil {
		t.Fatal(err)
	}
	otherIncoming, err := source.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Adopt(otherIncoming, owned.Path, workspaceidentity.Links{}); err == nil {
		t.Fatal("alias admission replaced project owner")
	}
	if _, err := receiver.Resolve(otherIncoming.URI); err == nil {
		t.Fatal("refused alias was retained")
	}
}

func TestConcurrentProjectAliasAdmissionKeepsCanonicalLookup(t *testing.T) {
	source, err := workspaceidentity.Open(filepath.Join(t.TempDir(), "source"))
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := source.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(t.TempDir(), "receiver")
	receiver, err := workspaceidentity.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := receiver.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	errors := make(chan error, 8)
	for range 8 {
		go func() {
			r, err := workspaceidentity.Open(registryPath)
			if err == nil {
				_, err = r.Adopt(incoming, canonical.Path, workspaceidentity.Links{})
			}
			errors <- err
		}()
	}
	for range 8 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	got, err := receiver.Lookup(workspaceidentity.Project, canonical.Path)
	if err != nil || got != canonical {
		t.Fatalf("canonical changed after concurrent admission: %#v %v", got, err)
	}
	alias, err := receiver.Resolve(incoming.URI)
	if err != nil || alias.URI != incoming.URI || alias.Path != canonical.Path {
		t.Fatalf("alias lost: %#v %v", alias, err)
	}
}

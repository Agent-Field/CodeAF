package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestReviewExistingOwnedHomeConversationResumes(t *testing.T) {
	for _, kind := range []string{"home", "temp"} {
		t.Run(kind, func(t *testing.T) { reviewOwnedConversation(t, kind) })
	}
}

func reviewOwnedConversation(t *testing.T, kind string) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if kind == "home" {
		t.Setenv("HOME", alias)
		t.Setenv("USERPROFILE", alias)
	} else {
		t.Setenv("HOME", t.TempDir())
	}
	oldBucket := home.Join("v3", "projects", encodeWorkspace(alias))
	old := writeV3Session(t, oldBucket, "00000000000000f1", "Remember the draft we wrote at home", time.Now())
	meta, err := session.LoadMeta(old)
	if err != nil {
		t.Fatal(err)
	}
	meta.Owned, meta.Workspace, meta.LaunchDir = true, filepath.Join(old, "work"), alias
	if err := os.MkdirAll(meta.Workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(old, meta); err != nil {
		t.Fatal(err)
	}
	project, owned := v3Workspace(alias, "")
	found, err := v3ResolveSession("", project, alias, owned)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("old bucket=%s; new project=%s; legacy meta.workspace=%s", oldBucket, project, meta.Workspace)
	if hello := v3LatestTranscript(project); hello != found.Transcript {
		t.Fatalf("hello selected %s; boot selected %s", hello, found.Transcript)
	}
	if !found.Place.Owned || found.Place.Workspace != meta.Workspace {
		t.Fatalf("resume lost owned workspace: %+v", found.Place)
	}
	if !found.Resumed || found.Transcript != (session.Place{Dir: old}).Transcript() {
		t.Fatalf("unchanged launch at symlinked HOME opened %s (resumed=%v), want %s", found.Transcript, found.Resumed, (session.Place{Dir: old}).Transcript())
	}
}

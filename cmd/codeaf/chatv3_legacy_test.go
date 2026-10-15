package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE CONVERSATIONS AN OLDER BUILD FILED UNDER ANOTHER SPELLING ARE STILL THIS
// FOLDER'S CONVERSATIONS. The upgrade regression for the resolved project key:
// a bucket minted before #1761 was named by the raw spelling the terminal
// carried, and a launch that reads only the canonical bucket would open a fresh
// journal beside every conversation a person already had. The newest spoken
// folder whose recorded workspace resolves to this folder resumes where it
// lives; a comprehensive merge of historical buckets is not required, and none
// is attempted here.
func TestAConversationInALegacyBucketStillResumes(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}

	// The old build wrote the bucket under the alias and cited the alias in the
	// folder's own meta — the citation is the identity, the bucket name is not.
	legacyBucket := filepath.Join(home.Join("v3", "projects"), encodeWorkspace(link))
	held := writeV3Session(t, legacyBucket, "00000000000000e1", "this morning", time.Now().Add(-time.Hour))
	meta, err := session.LoadMeta(held)
	if err != nil {
		t.Fatal(err)
	}
	meta.Workspace = link
	if err := session.SaveMeta(held, meta); err != nil {
		t.Fatal(err)
	}

	// A launch that stands on the resolved folder resumes the held
	// conversation, from the bucket it already lives in.
	want := filepath.Join(held, "transcript.jsonl")
	found, err := v3ResolveSession("", resolved, resolved, false)
	if err != nil {
		t.Fatal(err)
	}
	if found.Transcript != want {
		t.Fatalf("the launch opened %q, want the conversation the old build filed (%s)", found.Transcript, want)
	}
	if !found.Resumed {
		t.Fatal("a conversation somebody has spoken in resumed as a fresh one")
	}
	if found.Bucket != legacyBucket {
		t.Fatalf("the resumed conversation was filed under %q, want the bucket it lives in %q", found.Bucket, legacyBucket)
	}

	// And the engine's hello key — the question asked before anything opens —
	// answers with the same conversation, so a plain hello joins it rather than
	// booting a second agent onto the journal.
	if key := engineHelloKey(remote.Hello{Version: remote.Version, Workspace: resolved}, resolved, ""); key != want {
		t.Fatalf("a plain hello keyed to %q, want the held conversation (%s)", key, want)
	}
}

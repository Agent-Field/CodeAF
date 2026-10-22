package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/enginehost"
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

// Boot ignores abandoned empty folders. A hello must prefer the same spoken
// legacy conversation, or it will boot onto a journal the host already holds.
func TestLegacyHelloAndBootAgreeWithAnEmptyCanonicalBucket(t *testing.T) {
	shortEngineHome(t)
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	workspace, _ := v3Workspace(link, link)
	legacyBucket := home.Join("v3", "projects", encodeWorkspace(link))
	legacy := writeV3Session(t, legacyBucket, "00000000000000e2", "keep this conversation", time.Unix(100, 0))
	meta, err := session.LoadMeta(legacy)
	if err != nil {
		t.Fatal(err)
	}
	meta.Workspace = link
	if err := session.SaveMeta(legacy, meta); err != nil {
		t.Fatal(err)
	}
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	writeV3Session(t, bucket, "00000000000000e3", "", time.Time{})
	want := session.Place{Dir: legacy}.Transcript()
	for _, spelling := range []string{workspace, link} {
		key := engineHelloKey(remote.Hello{Version: remote.Version, Workspace: spelling}, spelling, "")
		if key != want {
			t.Errorf("hello through %s chose %s, want legacy conversation %s", spelling, key, want)
		}
		project, owned := v3Workspace(spelling, spelling)
		found, err := v3ResolveSession("", project, spelling, owned)
		if err != nil || found.Transcript != want || !found.Resumed {
			t.Fatalf("boot through %s = %+v, %v; want legacy conversation %s", spelling, found, err, want)
		}
	}

	// Exercise the host's real lookup over two socket connections. The second
	// plain hello must join the held agent instead of booting its journal again.
	var boots atomic.Int32
	stopped := make(chan error, 1)
	go func() {
		stopped <- enginehost.Run(workspace, enginehost.Options{
			Key: func(hello remote.Hello) string { return engineHelloKey(hello, workspace, "") },
			Boot: func(remote.Hello) (*remote.Engine, error) {
				boots.Add(1)
				found, err := v3ResolveSession("", workspace, workspace, false)
				if err != nil {
					return nil, err
				}
				return &remote.Engine{Agent: &quietAgent{}, Workspace: workspace, SessionFile: found.Transcript}, nil
			},
		})
	}()
	t.Cleanup(func() {
		_, _ = enginehost.Stop(workspace)
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("host stopped: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("host did not stop")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := enginehost.Ask(workspace, remote.WhoIs{}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("host did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, spelling := range []string{workspace, link} {
		conn, err := enginehost.Dial(spelling)
		if err != nil {
			t.Fatal(err)
		}
		client, err := remote.Dial(conn, "", remote.Hello{Version: remote.Version, Workspace: spelling})
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if got := client.Welcome().SessionFile; got != want {
			t.Fatalf("hello opened %s, want %s", got, want)
		}
	}
	if got := boots.Load(); got != 1 {
		t.Fatalf("two plain hellos booted %d agents onto one legacy journal; want one", got)
	}
}

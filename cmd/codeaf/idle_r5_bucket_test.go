package main

import (
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdleOpen_R5_3_DistinctCopyOrigins(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	origin := "/home/alice/profile/v3/projects/copy--work-repo/01J0000000000000000000000A/work"
	nested := origin + "/subproject"
	a, b := takeCopyBucket(origin), takeCopyBucket(nested)
	t.Logf("origin=%s\nnested=%s\nbuckets=%s ; %s", origin, nested, a, b)
	if a == b {
		t.Error("different project origins share a copy bucket")
	}
}

func TestIdleOpen_R5_3_LongOriginsHashOriginalIdentity(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	prefix := "/work/" + strings.Repeat("segment/", 40)
	a, b := prefix+"/x:y", prefix+"/x-y"
	ba, bb := filepath.Base(takeCopyBucket(a)), filepath.Base(takeCopyBucket(b))
	t.Logf("two distinct long origins produce bucket %q and %q", ba, bb)
	if ba == bb {
		t.Error("long-origin digest hashes the lossy encoded name")
	}
}

func TestIdleOpen_R5_3_OnlyVerifiedExactCopyWorkspaceReusesBucket(t *testing.T) {
	bucket := filepath.Join(t.TempDir(), "v3", "projects", "copy-original")
	root := filepath.Join(bucket, "01J0000000000000000000000A")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	meta := session.Meta{ID: filepath.Base(root), Owned: true, Workspace: work, Origin: "/original/project"}
	if err := session.SaveMeta(root, meta); err != nil {
		t.Fatal(err)
	}
	if got := copyBucketName(work); got != "copy-original" {
		t.Fatalf("verified copy bucket=%s", got)
	}
	if got := copyBucketName(filepath.Join(work, "subproject")); got == "copy-original" {
		t.Fatal("nested project inherited the parent's bucket")
	}
	meta.Owned = false
	if err := session.SaveMeta(root, meta); err != nil {
		t.Fatal(err)
	}
	if got := copyBucketName(work); got == "copy-original" {
		t.Fatal("unowned folder inherited a copy bucket")
	}
}

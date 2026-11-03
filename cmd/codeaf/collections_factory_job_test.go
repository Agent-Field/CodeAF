package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

// Part A (A2): add/remove/find accept --factory, show names it readably, and
// omitting or misusing the flag is refused before opening storage.
func TestPartACollectionsCLIFactoryJob(t *testing.T) {
	db := filepath.Join(t.TempDir(), "collections.db")
	c := collectionCreate(t, db, "Jobs")
	var out bytes.Buffer
	if err := runCollectionsTo([]string{"add", c.ID, "factory_job", "7", "--db", db}, &out); err == nil || !strings.Contains(err.Error(), "factory") {
		t.Fatalf("missing factory: %v", err)
	}
	collectionCommand(t, db, "add", c.ID, "factory_job", "7", "--factory", "f")
	if err := runCollectionsTo([]string{"show", c.ID, "--db", db}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "factory_job  7  factory f") {
		t.Fatalf("show: %q", out.String())
	}
	var refs []workspace.Ref
	if err := json.Unmarshal([]byte(collectionCommand(t, db, "show", c.ID)), &refs); err != nil || len(refs) != 1 || refs[0].Factory != "f" {
		t.Fatalf("refs: %+v %v", refs, err)
	}
	var parents []workspace.Collection
	if err := json.Unmarshal([]byte(collectionCommand(t, db, "find", "factory_job", "7", "--factory", "f")), &parents); err != nil || len(parents) != 1 || parents[0].ID != c.ID {
		t.Fatalf("find: %+v %v", parents, err)
	}
	collectionCommand(t, db, "remove", c.ID, "factory_job", "7", "--factory", "f")
	if got := collectionCommand(t, db, "show", c.ID); got != "[]\n" {
		t.Fatalf("remove: %s", got)
	}
	out.Reset()
	if err := runCollectionsTo([]string{"add", c.ID, "conversation", "chat", "--factory", "f", "--db", db}, &out); err == nil {
		t.Fatal("factory on conversation accepted")
	}
}

func TestPartACollectionRefConversionsRetainFactory(t *testing.T) {
	r := appwire.Ref{Kind: appwire.RefFactoryJob, ID: "7", Factory: "f"}
	if got := appWireRef(appStoreRef(r)); got != r {
		t.Fatalf("round trip: %+v", got)
	}
}

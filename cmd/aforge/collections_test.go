package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

func collectionCommand(t *testing.T, db string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	args = append(args, "--db", db, "--json")
	if err := runCollectionsTo(args, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func collectionCreate(t *testing.T, db, name string) workspace.Collection {
	t.Helper()
	var c workspace.Collection
	if err := json.Unmarshal([]byte(collectionCommand(t, db, "create", name)), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCollectionsCommandOrganizesExistingRecordsWithoutMovingWork(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	db := filepath.Join(root, "v3", "collections.db")
	if got := collectionCommand(t, db, "list"); got != "[]\n" {
		t.Fatal(got)
	}
	product := collectionCreate(t, db, "Product")
	marketing := collectionCreate(t, db, "Marketing")
	place := session.Place{Dir: filepath.Join(root, "v3", "projects", "repo", "existing-chat"), Workspace: filepath.Join(root, "repo")}
	if err := os.MkdirAll(place.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(place.Dir, session.Meta{Title: "Shared API decision", Workspace: place.Workspace}); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string][]byte{
		place.Transcript():               []byte("{\"role\":\"user\",\"content\":\"Keep the public API stable\"}\n"),
		place.Tasks():                    []byte("{\"schema\":1,\"tasks\":[]}\n"),
		filepath.Join(root, "report.md"): []byte("An existing report.\n"),
	}
	for path, data := range fixtures {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	metaBefore, err := os.ReadFile(place.MetaPath())
	if err != nil {
		t.Fatal(err)
	}
	fixtures[place.MetaPath()] = metaBefore
	for _, c := range []workspace.Collection{product, marketing} {
		collectionCommand(t, db, "add", c.ID, "conversation", place.ID())
		collectionCommand(t, db, "add", c.ID, "task", "1", "--session", place.ID())
		collectionCommand(t, db, "add", c.ID, "artifact", filepath.Join(root, "report.md"))
	}
	collectionCommand(t, db, "add", marketing.ID, "task", "1", "--session", "other-chat")
	collectionCommand(t, db, "rename", product.ID, "Engineering")
	collectionCommand(t, db, "remove", marketing.ID, "task", "1", "--session", place.ID())
	var parents []workspace.Collection
	if err := json.Unmarshal([]byte(collectionCommand(t, db, "find", "conversation", place.ID())), &parents); err != nil {
		t.Fatal(err)
	}
	if len(parents) != 2 || parents[0].ID != product.ID || parents[0].Name != "Engineering" {
		t.Fatalf("parents %+v", parents)
	}
	var members []workspace.Ref
	if err := json.Unmarshal([]byte(collectionCommand(t, db, "show", marketing.ID)), &members); err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 || members[2].SessionID != "other-chat" {
		t.Fatalf("task owners %+v", members)
	}
	for path, want := range fixtures {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("organization changed %s: %v", path, err)
		}
	}
}

func TestCollectionsRejectBadRequestsBeforeCreatingStorage(t *testing.T) {
	for _, args := range [][]string{
		{"create"}, {"create", " bad "}, {"rename", "missing"}, {"show"}, {"unknown"},
		{"add", "c", "task", "1"}, {"add", "c", "task", "01", "--session", "s"},
		{"find", "conversation", "chat", "--session", "s"}, {"list", "--session", "s"},
		{"add", "c", "made-up", "record"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "unused", "collections.db")
			var out bytes.Buffer
			err := runCollectionsTo(append(args, "--db", db), &out)
			if err == nil {
				t.Fatal("bad request succeeded")
			}
			if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created storage: %v", err)
			}
		})
	}
}

func TestCollectionsDefaultStorageIsIndependentOfMemory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	var out bytes.Buffer
	if err := runCollectionsTo([]string{"create", "Personal", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "v3", "collections.db")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "v3" {
		t.Fatalf("unexpected state: %v, %v", entries, err)
	}
	var c workspace.Collection
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runCollectionsTo([]string{"show", c.ID, "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Fatal(out.String())
	}
}

func TestCollectionsHelpListsOperationsWithoutOpeningStorage(t *testing.T) {
	db := filepath.Join(t.TempDir(), "unused.db")
	out, _ := captureUsage(t)
	err := runCollectionsTo([]string{"--help", "--db", db}, &bytes.Buffer{})
	if !errors.Is(err, exitHelped) {
		t.Fatal(err)
	}
	for _, word := range []string{"create", "rename", "show", "add|remove", "find", "--session", "--json", "--db"} {
		if !strings.Contains(out.String(), word) {
			t.Fatalf("help omits %q: %s", word, out.String())
		}
	}
	if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("help created storage", err)
	}
}

func TestCollectionsNestedMembershipRoundTrip(t *testing.T) {
	db := filepath.Join(t.TempDir(), "collections.db")
	a, b := collectionCreate(t, db, "A"), collectionCreate(t, db, "B")
	collectionCommand(t, db, "add", a.ID, "collection", b.ID)
	var out bytes.Buffer
	if err := runCollectionsTo([]string{"add", b.ID, "collection", a.ID, "--db", db}, &out); !errors.Is(err, workspace.ErrCycle) {
		t.Fatal(err)
	}
	var members []workspace.Ref
	if err := json.Unmarshal([]byte(collectionCommand(t, db, "show", a.ID)), &members); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(members, []workspace.Ref{{Kind: workspace.CollectionKind, ID: b.ID}}) {
		t.Fatal(members)
	}
}

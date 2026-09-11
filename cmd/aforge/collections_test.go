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
		{"create"}, {"create", " bad "}, {"rename", "missing"}, {"show"}, {"show", ""}, {"unknown"},
		{"add", "c", "task", "1"}, {"add", "c", "task", "01", "--session", "s"},
		{"find", "conversation", "chat", "--session", "s"}, {"list", "--session", "s"},
		{"add", "c", "made-up", "record"},
		{"add", "c", "artifact", ""},
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

// collectionPlain runs a command the way somebody at a terminal runs it, with
// no --json, because the readable output is the only output most people see and
// every other test here reads the structured form instead.
func collectionPlain(t *testing.T, db string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runCollectionsTo(append(args, "--db", db), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// These cases distinguish a missing membership from an empty store, and keep
// task ownership visible in the plain output as well as the JSON representation.
func TestCollectionsReadableOutputPreservesMeaning(t *testing.T) {
	db := filepath.Join(t.TempDir(), "collections.db")
	product := collectionCreate(t, db, "Product")
	if got := collectionPlain(t, db, "find", "conversation", "unfiled-chat"); got != "No collection references this record.\n" {
		t.Fatalf("empty find %q", got)
	}
	if got := collectionPlain(t, db, "add", product.ID, "task", "7", "--session", "chat-a"); !strings.Contains(got, "7") || !strings.Contains(got, "chat-a") {
		t.Fatalf("task owner missing from %q", got)
	}
	first := collectionPlain(t, db, "remove", product.ID, "task", "7", "--session", "chat-a")
	second := collectionPlain(t, db, "remove", product.ID, "task", "7", "--session", "chat-a")
	if first != second || !strings.Contains(second, "original record is unchanged") {
		t.Fatalf("idempotent removal: %q, %q", first, second)
	}
}

// A command that names a collection nobody created must refuse rather than
// report a quiet success, and repeating an add must not file a second copy.
func TestCollectionsRefuseUnknownCollectionsAndRepeatHarmlessly(t *testing.T) {
	db := filepath.Join(t.TempDir(), "collections.db")
	real := collectionCreate(t, db, "Product")
	for _, args := range [][]string{
		{"show", "deadbeef"},
		{"rename", "deadbeef", "Renamed"},
		{"add", "deadbeef", "conversation", "chat-a"},
		{"remove", "deadbeef", "conversation", "chat-a"},
		{"add", real.ID, "collection", "deadbeef"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			err := runCollectionsTo(append(args, "--db", db), &out)
			if !errors.Is(err, workspace.ErrNotFound) {
				t.Fatalf("err %v, output %q", err, out.String())
			}
		})
	}
	collectionCommand(t, db, "add", real.ID, "conversation", "chat-a")
	collectionCommand(t, db, "add", real.ID, "conversation", "chat-a")
	var members []workspace.Ref
	if err := json.Unmarshal([]byte(collectionCommand(t, db, "show", real.ID)), &members); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(members, []workspace.Ref{{Kind: workspace.ConversationKind, ID: "chat-a"}}) {
		t.Fatalf("repeat added %+v", members)
	}
}

func TestCollectionsBlankDatabaseIsAnInvalidArgument(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	err := runCollectionsTo([]string{"list", "--db", ""}, &bytes.Buffer{})
	if !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("blank database: %v", err)
	}
}

// Listing a new home and refusing edits to absent collections must not create
// directories or a database as a side effect of locating the requested records.
func TestCollectionsColdReadsAndMissingEditsDoNotInitializeStorage(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"find", "conversation", "chat"}, {"show", "missing"}, {"rename", "missing", "Renamed"}, {"add", "missing", "conversation", "chat"}, {"remove", "missing", "conversation", "chat"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "absent")
			db := filepath.Join(root, "collections.db")
			var out bytes.Buffer
			err := runCollectionsTo(append(args, "--json", "--db", db), &out)
			if args[0] == "list" || args[0] == "find" {
				if err != nil || out.String() != "[]\n" {
					t.Fatalf("read %q: %v", out.String(), err)
				}
			} else if !errors.Is(err, workspace.ErrNotFound) {
				t.Fatalf("missing collection: %v", err)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created state: %v", err)
			}
		})
	}
}

// C3 — The command door must treat an existing empty database exactly like a
// fresh home: list answers with the established sentence and leaves it empty.
func TestCollectionsListLeavesAnExistingEmptyDatabaseAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runCollectionsTo([]string{"list", "--db", path}, &out); err != nil {
		t.Fatal(err)
	}
	if want := "No collections found. Create one with aforge collections create <name>.\n"; out.String() != want {
		t.Fatalf("list output %q, want %q", out.String(), want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("list could not stat the empty database: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("list changed the empty database to %d bytes", info.Size())
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

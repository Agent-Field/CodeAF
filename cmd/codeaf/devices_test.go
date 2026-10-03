package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pair"
)

const (
	thisComputer  = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherComputer = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var devicesCtx = context.Background()

// chatsWith is a directory holding this computer and one other, both named the
// way a real device names itself: sealed under the metadata key.
func chatsWith(t *testing.T, other string) (chatsKind, directory.Client) {
	t.Helper()
	cellKey := bytes.Repeat([]byte{7}, 32)
	dir := directory.NewMemory(time.Now)
	for id, name := range map[string]string{thisComputer: "desk", otherComputer: other} {
		sealed, err := directory.SealName(directory.MetadataKey(cellKey), name)
		if err != nil {
			t.Fatal(err)
		}
		if err := dir.For(id).PutDevice(devicesCtx, id, directory.Device{V: 1, Name: sealed}); err != nil {
			t.Fatal(err)
		}
	}
	return chatsKindOf(dir.For(thisComputer), cellKey, thisComputer, "https://relay.example"), dir.For(thisComputer)
}

// bookWith is a book of remote devices let in, and a fresh home to keep this
// machine's own key in.
func bookWith(t *testing.T, labels ...string) *pair.Book {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	book := pair.BookAt(filepath.Join(t.TempDir(), "devices.json"))
	for i, label := range labels {
		if err := book.Admit(pair.Paired{Label: label, Key: strings.Repeat("k", i+1), Since: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	return book
}

func listed(t *testing.T, kinds ...deviceKind) string {
	t.Helper()
	printed, err := captureStdout(t, func() error { return listDevices(kinds, time.Now()) })
	if err != nil {
		t.Fatal(err)
	}
	return printed
}

// `codeaf devices` lists both kinds under their two headings, in that order,
// and one `revoke <name>` stops either.
func TestDevicesBothKinds(t *testing.T) {
	chats, dir := chatsWith(t, "laptop")
	kinds := []deviceKind{chats, bookKind{bookWith(t, "phone")}}

	printed := listed(t, kinds...)
	first, second := strings.Index(printed, "your devices"), strings.Index(printed, "devices that can use this machine")
	if first < 0 || second < first {
		t.Fatalf("the two headings are missing or out of order:\n%s", printed)
	}
	for _, want := range []string{"laptop", "this device", "phone"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the list lacks %q:\n%s", want, printed)
		}
	}

	if _, err := captureStdout(t, func() error { return stopDevice(kinds, []string{"laptop"}) }); err != nil {
		t.Fatalf("stopping a computer that holds the chats: %v", err)
	}
	l, err := dir.List(devicesCtx)
	if err != nil || !l.Devices[otherComputer].Revoked || l.Devices[thisComputer].Revoked {
		t.Fatalf("directory after the stop: %+v, %v", l.Devices, err)
	}
	if after := listed(t, kinds...); !strings.Contains(after, "removed") {
		t.Fatalf("a stopped computer is not marked:\n%s", after)
	}

	if _, err := captureStdout(t, func() error { return stopDevice(kinds, []string{"phone"}) }); err != nil {
		t.Fatalf("stopping a device that can use this machine: %v", err)
	}
	if left, _ := kinds[1].(bookKind).book.Devices(); len(left) != 0 {
		t.Fatalf("the remote device is still let in: %v", left)
	}
}

// A kind with nothing in it has no heading, and a machine with nothing of
// either kind says one sentence.
func TestDevicesEmptinessLaw(t *testing.T) {
	chats, _ := chatsWith(t, "laptop")
	only := listed(t, chats, bookKind{bookWith(t)})
	if strings.Contains(only, "devices that can use this machine") {
		t.Fatalf("an empty kind still has its heading:\n%s", only)
	}
	if none := listed(t, bookKind{bookWith(t)}); strings.TrimSpace(none) != pair.NoDevices {
		t.Fatalf("nothing paired said %q, want the one sentence", none)
	}
}

// A name that two devices answer to, or nobody does, or that is this computer,
// is one sentence that points at the list.
func TestDevicesRevokeNamesThatCannotPickOne(t *testing.T) {
	chats, dir := chatsWith(t, "phone")
	kinds := []deviceKind{chats, bookKind{bookWith(t, "phone")}}
	for name, want := range map[string]string{"phone": "more than one", "ghost": "no device is called", "desk": "this device"} {
		_, err := captureStdout(t, func() error { return stopDevice(kinds, []string{name}) })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("revoke %s: %v, want a sentence with %q", name, err, want)
		}
	}
	if l, _ := dir.List(devicesCtx); l.Devices[otherComputer].Revoked || l.Devices[thisComputer].Revoked {
		t.Fatalf("a refused name still stopped a computer: %+v", l.Devices)
	}
	// --all is the remote-access kind's word, so the name means that kind.
	if _, err := captureStdout(t, func() error { return stopDevice(kinds, []string{"phone", "--all"}) }); err != nil {
		t.Fatal(err)
	}
	if l, _ := dir.List(devicesCtx); l.Devices[otherComputer].Revoked {
		t.Fatal("--all stopped a computer that holds the chats")
	}
}

// What stopping cannot do is said where a person looks for it: in the sentence
// after the stop and in the help. Neither names a command that does not exist.
func TestDevicesRevokeText(t *testing.T) {
	chats, _ := chatsWith(t, "laptop")
	said, err := captureStdout(t, func() error { return stopDevice([]deviceKind{chats}, []string{"laptop"}) })
	if err != nil {
		t.Fatal(err)
	}
	// The front page has no room for the whole warning, so its line says that
	// stopping cannot undo what the computer holds, and the sentence after a
	// stop says what that means for a stolen one.
	says := map[string][]string{
		usageForCommand("devices revoke"): {"cannot undo"},
		said:                              {"cannot undo", "treat your chats as exposed", "codeaf identity rotate"},
	}
	for text, wants := range says {
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%q does not say %q", text, want)
			}
		}
	}
}

// This computer is listed first and a removed one last, whatever the names sort
// to, so the row a person is typing on is always where the eye starts.
func TestChatsTableListsThisComputerFirst(t *testing.T) {
	kind, dir := chatsWith(t, "atlas")
	rows, err := kind.rows()
	if err != nil || len(rows) != 2 || !rows[0].This || rows[1].Name != "atlas" {
		t.Fatalf("rows were %+v (%v), want this computer ahead of atlas", rows, err)
	}
	if err := dir.Revoke(devicesCtx, otherComputer); err != nil {
		t.Fatal(err)
	}
	table, _ := kind.table(time.Now())
	if strings.Index(table, "desk") > strings.Index(table, "atlas") || !strings.Contains(table, "removed") {
		t.Fatalf("the table was:\n%s\nwant desk first and atlas marked removed", table)
	}
}

// A list holding only this computer says nothing a person did not know, so it
// is not drawn and the one sentence takes its place.
func TestChatsTableOfOnlyThisComputerIsAbsent(t *testing.T) {
	cellKey := bytes.Repeat([]byte{7}, 32)
	dir := directory.NewMemory(time.Now)
	sealed, err := directory.SealName(directory.MetadataKey(cellKey), "desk")
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.For(thisComputer).PutDevice(devicesCtx, thisComputer, directory.Device{V: 1, Name: sealed}); err != nil {
		t.Fatal(err)
	}
	kind := chatsKindOf(dir.For(thisComputer), cellKey, thisComputer, "https://relay.example")
	if table, err := kind.table(time.Now()); table != "" || err != nil {
		t.Fatalf("a lone computer drew %q (%v), want nothing", table, err)
	}
}

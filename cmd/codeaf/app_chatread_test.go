package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// remoteJournal is a small journal in the shape the session writes: a header,
// then turns of a person's message, a call, its result and an answer.
func remoteJournal(turns int) []byte {
	var b bytes.Buffer
	b.WriteString(`{"type":"session","version":1,"id":"abc","cwd":"/w"}` + "\n")
	for i := 0; i < turns; i++ {
		at := time.Date(2026, 11, 15, 10, i, 0, 0, time.UTC).Format(time.RFC3339Nano)
		fmt.Fprintf(&b, `{"type":"message","role":"user","content":"question %d","timestamp":%q}`+"\n", i, at)
		fmt.Fprintf(&b, `{"presentation":{"audience":"human"},"type":"message","role":"assistant","content":"looking %d","toolCalls":[{"id":"bash:%d","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}],"timestamp":%q}`+"\n", i, i, at)
		fmt.Fprintf(&b, `{"type":"message","role":"tool","toolCallId":"bash:%d","content":"a.txt","timestamp":%q}`+"\n", i, at)
		fmt.Fprintf(&b, `{"presentation":{"audience":"human"},"type":"message","role":"assistant","content":"answer %d","timestamp":%q}`+"\n", i, at)
	}
	return b.Bytes()
}

type entryShape struct{ Kind, Text string }

func shapes(entries []appwire.Entry) []entryShape {
	out := make([]entryShape, len(entries))
	for i, e := range entries {
		out[i] = entryShape{e.Kind, e.Text}
	}
	return out
}

// A remote page is shaped by the same code agents.entries uses, so the whole
// journal read as a remote page is the local chat's entries, and a page cut at
// a later turn (with the header line in front, as the pager sends it) is the
// tail of them.
func TestARemotePageIsShapedLikeTheSameChatOnThisComputer(t *testing.T) {
	journal := remoteJournal(6)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, journal, 0o600); err != nil {
		t.Fatal(err)
	}
	local, _, err := appEntryPageOf("chat", session.ReadTranscript(path), time.Time{}, map[int]appTurnMark{}, nil, "", 1000,
		func(session.DisplayEntry) appwire.ToolFacts { return appwire.ToolFacts{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := appRemoteEntries("chat", journal, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(shapes(remote)) != fmt.Sprint(shapes(local)) {
		t.Fatalf("remote shapes\n%v\nlocal\n%v", shapes(remote), shapes(local))
	}
	if len(local) == 0 {
		t.Fatal("the journal shaped to nothing")
	}
	header, _, _ := bytes.Cut(journal, []byte("\n"))
	cut := bytes.Index(journal, []byte(`{"type":"message","role":"user","content":"question 4"`))
	tail := append(append(append([]byte{}, header...), '\n'), journal[cut:]...)
	page, err := appRemoteEntries("chat", tail, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := shapes(local)[len(local)-len(page):]
	if len(page) == 0 || fmt.Sprint(shapes(page)) != fmt.Sprint(want) {
		t.Fatalf("tail page\n%v\nwant the local tail\n%v", shapes(page), want)
	}
	if !strings.Contains(page[0].Text, "question 4") {
		t.Fatalf("the page opens on %q, want the person's question 4", page[0].Text)
	}
}

// A page cut from a journal a newer codeaf wrote says so, because its header
// line rides in front of every page.
func TestARemotePageOfANewerJournalSaysItCannotBeRead(t *testing.T) {
	journal := []byte(`{"type":"session","version":999,"id":"abc"}` + "\n" + `{"type":"message","role":"user","content":"hi"}` + "\n")
	entries, err := appRemoteEntries("chat", journal, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != appwire.EntryNotice || !strings.Contains(entries[0].Text, "newer codeaf") {
		t.Fatalf("entries = %+v; want the newer-codeaf notice alone", entries)
	}
}

// chats.read is absent from hello on an engine whose furrow cannot read paths.
func TestChatsReadNeedsAnEngineThatReadsPaths(t *testing.T) {
	was := appReadsPaths
	t.Cleanup(func() { appReadsPaths = was })
	b := &appBackend{}
	appReadsPaths = func() bool { return false }
	if b.CanReadChats() {
		t.Fatal("chats.read offered by an engine that cannot read a path")
	}
}

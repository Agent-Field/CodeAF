//go:build linux

package sessionhost

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/coder/websocket"
)

// engineLine is one line the engine writes to a window, built from the engine's own types so that a rename of a field
// in internal/remote fails these tests instead of leaving every list untitled.
func engineLine(t *testing.T, kind string, rev uint64, title string) []byte {
	t.Helper()
	push := remote.FactsPush{Rev: rev, Facts: session.Facts{Title: title, Model: "a-model"}}
	var payload []byte
	switch kind {
	case "welcome":
		payload, _ = json.Marshal(remote.Welcome{Facts: &push})
	default:
		payload, _ = json.Marshal(push)
	}
	line, err := json.Marshal(remote.Frame{Kind: kind, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

// tapped runs lines through a tap in the given chunk size and answers every title it reported.
func tapped(chunk int, lines ...[]byte) []string {
	var got []string
	tap := &titleTap{note: func(title string) { got = append(got, title) }}
	all := bytes.Join(lines, nil)
	for len(all) > 0 {
		n := min(chunk, len(all))
		_, _ = tap.Write(all[:n])
		all = all[n:]
	}
	return got
}

func eq(got, want []string) bool {
	return strings.Join(got, "\x00") == strings.Join(want, "\x00") && len(got) == len(want)
}

// Contract S1 and S3, at the tap: the title comes from what the engine states, in the three frames that carry a
// whole fact set, however the bytes are cut into reads.
func TestS3_TheTapReadsTheTitleFromTheWelcomeAndFromEveryLaterStatement(t *testing.T) {
	for _, chunk := range []int{1, 7, 4096} {
		got := tapped(chunk,
			engineLine(t, "welcome", 1, "Fix the login redirect"),
			engineLine(t, "facts", 2, "Fix the login redirect"), // the same title again says nothing new
			engineLine(t, "facts", 3, "Fix the login redirect and ship"),
			engineLine(t, "title", 4, "Ship the login fix"),
		)
		if want := []string{"Fix the login redirect", "Fix the login redirect and ship", "Ship the login fix"}; !eq(got, want) {
			t.Errorf("read in chunks of %d, the tap reported %q, want %q", chunk, got, want)
		}
	}
}

// Contract S3: the engine writes its status frame and its naming frame from two places, so an older statement can
// arrive after a newer one, and it must not put an old title back. A welcome is a new account (a swapped
// conversation starts again from its own revision), so it is not held to the revisions before it.
func TestS3_AnOlderStatementNeverPutsAnOldTitleBack(t *testing.T) {
	got := tapped(4096,
		engineLine(t, "welcome", 10, "First conversation"),
		engineLine(t, "facts", 12, "Renamed"),
		engineLine(t, "title", 11, "First conversation, stale"),
		engineLine(t, "welcome", 1, "A new conversation"),
		engineLine(t, "facts", 2, "A new conversation, renamed"),
	)
	if want := []string{"First conversation", "Renamed", "A new conversation", "A new conversation, renamed"}; !eq(got, want) {
		t.Fatalf("the tap reported %q, want %q", got, want)
	}
}

// Contract S2: a chat with no title yet has nothing to report, and nothing that is not a statement of the facts is read.
func TestS2_ALineWithNoTitleOrThatIsNotAStatementReportsNothing(t *testing.T) {
	other, _ := json.Marshal(remote.Frame{Kind: "event", ID: 4, Payload: json.RawMessage(`{"facts":{"facts":{"title":"not a statement"}}}`)})
	welcomeWithoutFacts, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: json.RawMessage(`{"version":3}`)})
	got := tapped(4096,
		engineLine(t, "welcome", 1, ""),
		engineLine(t, "facts", 2, ""),
		append(other, '\n'),
		append(welcomeWithoutFacts, '\n'),
		[]byte("not json at all\n"),
		[]byte("{\"kind\":\"facts\",\"payload\":\"torn\n"),
		[]byte("\n"),
	)
	if len(got) != 0 {
		t.Fatalf("the tap reported %q when the chat had no title", got)
	}
	// A title that is later said is read, and a later statement without one does not take it away.
	got = tapped(4096, engineLine(t, "welcome", 1, ""), engineLine(t, "facts", 2, "Named at last"), engineLine(t, "facts", 3, ""))
	if want := []string{"Named at last"}; !eq(got, want) {
		t.Fatalf("the tap reported %q, want %q", got, want)
	}
}

// Contract S1: a welcome large enough to be sent gzipped (32 KiB and over) still names the chat.
func TestS1_ACompressedWelcomeStillNamesTheChat(t *testing.T) {
	push := remote.FactsPush{Rev: 1, Facts: session.Facts{Title: "From a big welcome"}}
	payload, _ := json.Marshal(remote.Welcome{Facts: &push})
	var packed bytes.Buffer
	zip := gzip.NewWriter(&packed)
	_, _ = zip.Write(payload)
	_ = zip.Close()
	line, _ := json.Marshal(remote.Frame{Kind: "welcome", Encoding: "gzip", Data: packed.Bytes()})
	if got := tapped(4096, append(line, '\n')); !eq(got, []string{"From a big welcome"}) {
		t.Fatalf("the tap reported %q", got)
	}
}

// The bytes a title is read from are passed on exactly as the engine wrote them, and a title is made safe to draw on
// other people's screens.
func TestTheTapOnlyLooksAndTitlesAreMadeSafe(t *testing.T) {
	line := engineLine(t, "facts", 2, "Fix\x1b[31m the ‮reversed‬  login\n redirect")
	var through bytes.Buffer
	var got []string
	tap := &titleTap{note: func(title string) { got = append(got, title) }}
	n, err := io.Copy(&through, io.TeeReader(bytes.NewReader(line), tap))
	if err != nil || int(n) != len(line) || !bytes.Equal(through.Bytes(), line) {
		t.Fatalf("the tap changed what passed through it: %d bytes, %v", n, err)
	}
	if want := []string{"Fix[31m the reversed login redirect"}; !eq(got, want) {
		t.Fatalf("the title was kept as %q, want %q", got, want)
	}
	if long := cleanTitle(strings.Repeat("é", titleMost+50)); len([]rune(long)) != titleMost {
		t.Fatalf("a long title kept %d characters, want %d", len([]rune(long)), titleMost)
	}
}

// eventually polls a read until it says what is wanted: a title is kept on its own goroutine, after the line that said it.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("never saw %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// listedRow is the row the list gives for chatA, as the raw JSON object.
func listedRow(t *testing.T, s *supervisor) map[string]json.RawMessage {
	t.Helper()
	w := request(s, "GET", "/v1/sessions", s.cfg.Secret, "alice", "")
	var list struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Sessions) != 1 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	return list.Sessions[0]
}

func titleOnRow(t *testing.T, row map[string]json.RawMessage) (title string, present bool) {
	t.Helper()
	raw, present := row["title"]
	if present && json.Unmarshal(raw, &title) != nil {
		t.Fatalf("the title is not a string: %s", raw)
	}
	return title, present
}

// Contract S1, S2 and S3 at the runner: a chat's row names it from what its engine says on a window's wire, follows a
// rename on the same open wire, still names it after every window has gone and the chat is suspended, and gives a chat
// that was never named no title field. Reading the list starts nothing.
func TestS3_AChatsRowKeepsTheTitleItsEngineSaidAndTheListNeverStartsTheChat(t *testing.T) {
	cfg := testConfig(t)
	launcher := newWireTestLauncher()
	procs := &fakeProcesses{live: processTable{}}
	s := newTestSupervisor(t, cfg, healthyReport(), launcher, procs)
	createChat(t, s, "alice", chatA)
	if _, present := titleOnRow(t, listedRow(t, s)); present {
		t.Fatal("a chat nobody has opened has a title field")
	}

	procs.live[cfg.UIDBase] = []int{4321}
	conn := dialTestWire(t, s)
	child := launcher.child(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	say := func(kind string, rev uint64, title string) {
		t.Helper()
		if _, err := child.out.Write(engineLine(t, kind, rev, title)); err != nil {
			t.Fatal(err)
		}
	}
	say("welcome", 1, "")
	say("facts", 2, "")
	// A reader of the list who comes while the chat has no title still sees none.
	time.Sleep(50 * time.Millisecond)
	if _, present := titleOnRow(t, listedRow(t, s)); present {
		t.Fatal("a chat the engine has not named has a title field")
	}

	say("facts", 3, "Fix the login redirect")
	eventually(t, "the chat's title in its row", func() bool {
		title, present := titleOnRow(t, listedRow(t, s))
		return present && title == "Fix the login redirect"
	})
	say("facts", 4, "Fix the login redirect, then ship it")
	eventually(t, "the chat's new title with its window still open", func() bool {
		title, _ := titleOnRow(t, listedRow(t, s))
		return title == "Fix the login redirect, then ship it"
	})

	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Log(err)
	}
	select {
	case <-child.signals:
	case <-time.After(3 * time.Second):
		t.Fatal("the window's close left the bridge child running")
	}
	delete(procs.live, cfg.UIDBase)
	row := listedRow(t, s)
	var state string
	_ = json.Unmarshal(row["state"], &state)
	if title, _ := titleOnRow(t, row); state != "suspended" || title != "Fix the login redirect, then ship it" {
		t.Fatalf("a suspended chat's row says state %q and title %q", state, title)
	}
	select {
	case <-launcher.started:
		t.Fatal("reading the list started a chat process")
	default:
	}

	// The title is on disk, so a runner that restarts still names the chat.
	restarted, err := loadRegistry(cfg, time.Now())
	if err != nil || restarted.sessions[chatA].Title != "Fix the login redirect, then ship it" {
		t.Fatalf("after a restart the chat's title is %q: %v", restarted.sessions[chatA].Title, err)
	}
	// And the single-chat read says the same, since a window opening the chat reads that.
	one := request(s, "GET", "/v1/sessions/"+chatA, cfg.Secret, "alice", "")
	if !strings.Contains(one.Body.String(), `"title":"Fix the login redirect, then ship it"`) {
		t.Fatalf("the single chat read has no title: %s", one.Body)
	}
}

// A title that arrives for a chat that was deleted while it was on its way leaves nothing behind.
func TestATitleForAChatThatIsGoneIsDropped(t *testing.T) {
	cfg := testConfig(t)
	s := newTestSupervisor(t, cfg, healthyReport(), newWireTestLauncher(), &fakeProcesses{live: processTable{}})
	createChat(t, s, "alice", chatA)
	if w := request(s, "DELETE", "/v1/sessions/"+chatA, cfg.Secret, "alice", ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	s.noteTitle(chatA, "Named too late")
	s.mu.Lock()
	_, kept := s.registry.sessions[chatA]
	s.mu.Unlock()
	if kept {
		t.Fatal("a title written after a delete brought the chat back")
	}
}

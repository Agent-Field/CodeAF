package delegate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// THE INBOX IS READ ONCE, LINE BY LINE, AND A LINE STILL BEING WRITTEN WAITS.
// Two messages appended are read together; a half-written third — no newline
// yet — is left for the next read and read whole then; nothing is read twice.
func TestTheInboxReadsEachWholeLineOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), InboxName)
	in := &inbox{path: path}
	if got := in.Messages(); len(got) != 0 {
		t.Fatalf("a missing inbox answered %v", got)
	}
	for _, m := range []Message{{ID: "n1", From: FromPerson, Text: "look in grade.sh"}, {ID: "n2", From: FromConversation, Text: "stop chasing the flaky test"}} {
		if err := AppendInbox(path, m); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"id":"n3","from":"person","text":"half`); err != nil {
		t.Fatal(err)
	}
	got := in.Messages()
	if len(got) != 2 || got[0].ID != "n1" || got[1].Text != "stop chasing the flaky test" {
		t.Fatalf("first read = %+v", got)
	}
	if _, err := file.WriteString(` of it"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if got := in.Messages(); len(got) != 1 || got[0].ID != "n3" || got[0].Text != "half of it" {
		t.Fatalf("second read = %+v, want the finished third line alone", got)
	}
	if got := in.Messages(); len(got) != 0 {
		t.Fatalf("a third read answered %+v", got)
	}
	if err := AppendInbox(path, Message{ID: "n4", Text: "  "}); err == nil {
		t.Fatal("a message with no words was appended")
	}
}

type listeningSink struct {
	recorder
	heard  []string
	closed []string
}

func (s *listeningSink) Heard(ids []string)        { s.heard = append(s.heard, ids...) }
func (s *listeningSink) InboxClosed(reason string) { s.closed = append(s.closed, reason) }

// THE LISTENING RECORDS REACH A SINK THAT LISTENS, AND ONLY ONE. A hello's
// accepts is read; `heard` and a closed `inbox` go to a MessageSink; a sink
// that is not one reads the same stream without them and without error.
func TestTheListeningRecordsReachOnlyASinkThatListens(t *testing.T) {
	var stream bytes.Buffer
	emitter := NewEmitter(&stream)
	_ = emitter.HelloAccepting("fake", []string{"work"}, []string{AcceptMessages})
	_ = emitter.Heard([]string{"n1", "n2"})
	_ = emitter.InboxClosed("it has handed in")
	_ = emitter.Terminal(Ending{Status: StatusPass, Message: "done"})
	text := stream.String()

	sink := &listeningSink{}
	reading, err := Read(strings.NewReader(text), sink)
	if err != nil || reading.Hello == nil || !reading.Hello.Listening() {
		t.Fatalf("hello = %+v, err %v", reading.Hello, err)
	}
	if !reflect.DeepEqual(sink.heard, []string{"n1", "n2"}) || !reflect.DeepEqual(sink.closed, []string{"it has handed in"}) {
		t.Fatalf("heard %v closed %v", sink.heard, sink.closed)
	}
	if reading.Ignored != 0 {
		t.Fatalf("a listening stream had %d lines ignored", reading.Ignored)
	}
	plain, err := Read(strings.NewReader(text), &recorder{})
	if err != nil || plain.Terminal == nil {
		t.Fatalf("a sink that does not listen could not read the stream: %+v %v", plain, err)
	}
}

// A CHILD LISTENS ONLY WHEN ITS DECLARATION SAYS SO AND CODEAF GAVE IT AN
// INBOX. Both, and the body's host is a Listener and the hello says so; either
// missing, the host is plain and the hello accepts nothing.
func TestAChildListensOnlyWithADeclarationAndAnInbox(t *testing.T) {
	t.Setenv(EnvModelAPI, "http://127.0.0.1:9/v1")
	t.Setenv(EnvModelToken, "token")
	path := filepath.Join(t.TempDir(), InboxName)
	if err := AppendInbox(path, Message{ID: "n1", From: FromPerson, Text: "look in grade.sh"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		listens bool
		inbox   string
		want    bool
	}{
		{"declared, with an inbox", true, path, true},
		{"declared, no inbox", true, "", false},
		{"an inbox, not declared", false, path, false},
	} {
		t.Setenv(EnvInbox, tc.inbox)
		var heard []Message
		listened := false
		inv, err := Parse(testProgram(func(ctx context.Context, host Host, args []string) error {
			host.Hello([]string{"work"})
			if listener, ok := host.(Listener); ok {
				listened = true
				heard = listener.Messages()
				listener.Heard([]string{heard[0].ID})
				listener.CloseInbox("done reading")
				listener.CloseInbox("done reading")
			}
			host.Terminal(Ending{Status: StatusPass, Message: "done"})
			return nil
		}), []string{"b"}, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		inv.Program.Listens = tc.listens
		var stdout bytes.Buffer
		RunChild(context.Background(), inv, &stdout)
		sink := &listeningSink{}
		reading, _ := Read(&stdout, sink)
		if listened != tc.want || reading.Hello == nil || reading.Hello.Listening() != tc.want {
			t.Fatalf("%s: listened %v hello %+v, want %v", tc.name, listened, reading.Hello, tc.want)
		}
		if tc.want && (len(heard) != 1 || !reflect.DeepEqual(sink.heard, []string{"n1"}) || len(sink.closed) != 1) {
			t.Fatalf("%s: heard %v receipts %v closed %v (closing twice writes once)", tc.name, heard, sink.heard, sink.closed)
		}
	}
}

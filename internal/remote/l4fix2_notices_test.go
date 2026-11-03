package remote

import (
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"testing"
)

// L4-B16: Old surfaces keep their recognized notice spellings; a surface that
// requests the new vocabulary receives the current host-aware sentences.
func TestL4Fix2NoticeVocabularyOnTheWire(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy surface", true: "current surface"}[modern], func(t *testing.T) {
			for _, tc := range []struct{ text, old string }{
				{syncwords.Outage("https://sync.example"), "this chat is not syncing because other machines are unreachable; it keeps working here"},
				{syncwords.Recovered, chatlist.SyncRestored},
				{chatlist.Removed, "this device was removed by another of your devices, so this chat stays here only — run `codeaf pair` to bring it back"},
			} {
				far := &fakeAgent{prefill: []session.Event{{Kind: session.EventNotice, Text: tc.text}}}
				l := dialAgent(t, &Engine{Agent: far})
				l.write(Frame{Kind: "hello", Payload: raw(t, map[string]any{"version": Version, "syncNoticeWords": modern})})
				if welcome := l.recv(); welcome.Kind != "welcome" {
					t.Fatal(welcome)
				}
				l.ok(1, MethodSubmit, SubmitArgs{Text: "hello"})
				frame := l.await(func(f Frame) bool { return f.Kind == "event" })
				event := decode[EventWire](t, frame.Payload)
				want := tc.old
				if modern {
					want = tc.text
				}
				if event.Text != want {
					t.Fatalf("notice=%q want %q", event.Text, want)
				}
				l.end()
			}
		})
	}
}

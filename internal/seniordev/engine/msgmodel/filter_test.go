//go:build !windows

package msgmodel

import (
	"testing"
)

func filterUser(id string, parts ...Part) WithParts {
	return WithParts{
		Info:  User{MessageBase: MessageBase{ID: id, SessionID: "ses"}},
		Parts: parts,
	}
}

func filterAssistant(id, parent string, summary bool, failed error) WithParts {
	finish := "stop"
	info := Assistant{
		MessageBase: MessageBase{ID: id, SessionID: "ses"},
		ParentID:    parent, Finish: &finish,
	}
	if summary {
		flag := true
		info.Summary = &flag
	}
	if failed != nil {
		converted := NewUnknownError(failed.Error())
		info.Error = &converted
		errorFinish := "error"
		info.Finish = &errorFinish
	}
	return WithParts{
		Info: info,
		Parts: Parts{TextPart{
			PartBase: PartBase{ID: "p_" + id, SessionID: "ses", MessageID: id},
			Text:     "text " + id,
		}},
	}
}

func ids(messages []WithParts) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.Info.MessageID())
	}
	return out
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// chronological builds: u0, a0, a1, a2 (tail starts at a1), the compaction
// user message uc, then the summary attempts, then the auto-continue user.
func compactedSession(attempts ...WithParts) []WithParts {
	tail := "a1"
	messages := []WithParts{
		filterUser("u0"),
		filterAssistant("a0", "u0", false, nil),
		filterAssistant("a1", "u0", false, nil),
		filterAssistant("a2", "u0", false, nil),
		filterUser("uc", CompactionPart{
			PartBase: PartBase{ID: "pc", SessionID: "ses", MessageID: "uc"},
			Auto:     true, TailStartID: &tail,
		}),
	}
	messages = append(messages, attempts...)
	return append(messages, filterUser("ucont"))
}

func newestFirst(messages []WithParts) []WithParts {
	out := make([]WithParts, len(messages))
	for i := range messages {
		out[len(messages)-1-i] = messages[i]
	}
	return out
}

func TestFilterCompactedRotatesTailAfterTheSummary(t *testing.T) {
	session := compactedSession(filterAssistant("as", "uc", true, nil))
	got := ids(FilterCompacted(newestFirst(session)))
	want := []string{"uc", "as", "a1", "a2", "ucont"}
	if !equalIDs(got, want) {
		t.Fatalf("projection = %v, want %v", got, want)
	}
}

// An errored summary attempt sitting before the accepted one (a transport
// failure retried by the run layer) must not be chosen as the boundary: the
// tail has to land AFTER the accepted summary, exactly as it does when the
// first attempt succeeds.
func TestFilterCompactedSkipsErroredSummaryAttemptWhenRotating(t *testing.T) {
	session := compactedSession(
		filterAssistant("afail", "uc", true, errString("unexpected EOF")),
		filterAssistant("as", "uc", true, nil),
	)
	got := ids(FilterCompacted(newestFirst(session)))
	want := []string{"uc", "afail", "as", "a1", "a2", "ucont"}
	if !equalIDs(got, want) {
		t.Fatalf("projection = %v, want %v", got, want)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

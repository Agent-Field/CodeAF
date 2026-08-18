package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE DEFECT THESE TESTS PIN: a resumed conversation drew its tool calls as
// lines that expanded into nothing. The rows came from the journal, the journal
// held every byte of the arguments and the results, and the entries the surface
// was handed carried neither — so clicking a replayed write showed a blank where
// the content was.
//
// The acceptance is therefore not "the payload arrived" but "the replayed row is
// the live row": the same expansion, from the same renderer, off the same two
// fields.

// resumedApp is a surface that opened on a journal. It is the whole harness
// this file needs — the entries are what [session.Agent.Transcript] would hand
// back for a session picked up rather than created.
func resumedApp(t *testing.T, past ...session.DisplayEntry) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m", past: past})
	a.entries = nil
	a.replay()
	a.touch()
	return a
}

// replayedCall is one journaled tool call: the gloss the line shows, and the
// payload the expansion is drawn from.
func replayedCall(tool, hint, args, output string) session.DisplayEntry {
	return session.DisplayEntry{Role: "tool", Tool: tool, Hint: hint, Args: args, Output: output}
}

// expansionOf opens the first replayed call and returns the rows that hang off
// the rail — the expansion alone, without the line above it.
func expansionOf(t *testing.T, a *app) []string {
	t.Helper()
	var body []string
	for _, r := range openFirst(t, a) {
		r = strings.TrimLeft(r, " ")
		if stem, cut := strings.CutPrefix(r, railCont); cut {
			body = append(body, stem)
			continue
		}
		if stem, cut := strings.CutPrefix(r, railContASCII); cut {
			body = append(body, stem)
		}
	}
	if len(body) == 0 {
		t.Fatalf("the opened call drew no expansion:\n%s", strings.Join(plainRows(a), "\n"))
	}
	return body
}

// ── the payload arrives ─────────────────────────────────────────────────────

// A replayed WRITE expands to what it wrote. The arguments are the only record
// of that content anywhere — the tool's own result is one sentence saying it
// worked — so this is the row the defect was reported against.
func TestAReplayedWriteExpandsToItsContent(t *testing.T) {
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", Text: "write my notes down"},
		replayedCall("write", "write notes.md",
			`{"path":"notes.md","content":"the planner walks the graph\nand stops at a leaf"}`,
			"wrote notes.md"),
	)

	body := strings.Join(expansionOf(t, a), "\n")
	for _, want := range []string{"the planner walks the graph", "and stops at a leaf"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the replayed write did not expand to its content (%q missing):\n%s", want, body)
		}
	}
}

// A replayed EDIT expands to the diff it applied, computed from the old/new
// pair the arguments carried exactly as a live one is.
func TestAReplayedEditExpandsToItsDiff(t *testing.T) {
	args := editArgs(t, "internal/session/loop.go",
		[2]string{"const argsLimit = 400", "const argsLimit = 8192"})
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", Text: "raise the cap"},
		replayedCall("edit", "edit internal/session/loop.go", args, "edited internal/session/loop.go"),
	)

	body := expansionOf(t, a)
	joined := strings.Join(body, "\n")
	if !strings.Contains(joined, "internal/session/loop.go") {
		t.Fatalf("the diff does not name the file it touched:\n%s", joined)
	}
	var removed, added bool
	for _, line := range body {
		if strings.HasPrefix(line, "-const argsLimit = 400") {
			removed = true
		}
		if strings.HasPrefix(line, "+const argsLimit = 8192") {
			added = true
		}
	}
	if !removed || !added {
		t.Fatalf("the replayed edit did not expand to a diff (removed=%v added=%v):\n%s", removed, added, joined)
	}
}

// A replayed BASH shows the whole command on its line and its output in the
// expansion, exit code and all — which is what somebody scrolling back to a
// failed build came to read.
func TestAReplayedBashShowsItsCommandAndOutput(t *testing.T) {
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", Text: "build it"},
		replayedCall("bash", "bash go build ./...",
			`{"command":"cd internal/session && go build ./..."}`,
			"loop.go:114:2: declared and not used: cap\nexit status 1"),
	)

	line := toolLineOf(t, a)
	if !strings.Contains(line, "cd internal/session && go build ./...") {
		t.Fatalf("the replayed command is not on its line: %q", line)
	}
	body := strings.Join(expansionOf(t, a), "\n")
	if !strings.Contains(body, "declared and not used: cap") {
		t.Fatalf("the replayed command's output is not in its expansion:\n%s", body)
	}
}

// THE WHOLE POINT, stated as an equality: a replayed row and the live row it
// replaces draw the same expansion. Two renderings of one conversation is how a
// resumed screen starts lying about what happened.
func TestAReplayedRowExpandsExactlyLikeTheLiveRow(t *testing.T) {
	cases := []struct {
		name          string
		tool          string
		args, output  string
		hint          string
		fromLiveEvent bool
	}{
		{
			name: "write", tool: "write", hint: "write notes.md",
			args:   `{"path":"notes.md","content":"alpha\nbravo\ncharlie"}`,
			output: "wrote notes.md",
		},
		{
			name: "read", tool: "read", hint: "read notes.md",
			args:   `{"path":"notes.md"}`,
			output: "alpha\nbravo\ncharlie",
		},
		{
			name: "bash", tool: "bash", hint: "bash go test ./...",
			args:   `{"command":"go test ./..."}`,
			output: "ok  github.com/Agent-Field/aforge-v2/internal/session 7.6s",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			live := toolApp(t, tokens.NoColor, call(testCase.tool, testCase.args, testCase.output))
			replayed := resumedApp(t,
				session.DisplayEntry{Role: "user", Text: "go on then"},
				replayedCall(testCase.tool, testCase.hint, testCase.args, testCase.output),
			)
			liveBody := strings.Join(expansionOf(t, live), "\n")
			replayBody := strings.Join(expansionOf(t, replayed), "\n")
			if liveBody != replayBody {
				t.Fatalf("the replayed expansion differs from the live one:\nlive:\n%s\n\nreplayed:\n%s", liveBody, replayBody)
			}
		})
	}
}

// ── the row that has nothing ────────────────────────────────────────────────

// An older journal carried no payload at all, and its rows must not pretend
// otherwise: a hover that brightens and a click that opens a blank are the same
// defect wearing the fix's clothes.
func TestAReplayedRowWithNoPayloadIsNotInteractive(t *testing.T) {
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", Text: "what did you do?"},
		// The shape [session.DisplayEntry] had before this wave: a name and a
		// gloss, and nothing behind them.
		session.DisplayEntry{Role: "tool", Tool: "read", Hint: "read internal/session/loop.go"},
	)

	var found bool
	for i := range a.entries {
		if a.entries[i].kind != entryTool {
			continue
		}
		found = true
		if !replayInert(&a.entries[i]) {
			t.Fatalf("a payload-less replayed row is interactive: %+v", a.entries[i].detail)
		}
	}
	if !found {
		t.Fatal("the payload-less call was not replayed at all")
	}
	// It is still the line it always was — the conversation is not edited to
	// hide what an old file could not answer.
	if line := toolLineOf(t, a); !strings.Contains(line, "internal/session/loop.go") {
		t.Fatalf("the payload-less row lost its line: %q", line)
	}
}

// And the rows that DO carry a payload stay interactive — the inertness is a
// statement about an empty journal, never about replay itself.
func TestAReplayedRowWithAPayloadStaysInteractive(t *testing.T) {
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", Text: "read it"},
		replayedCall("read", "read loop.go", `{"path":"loop.go"}`, "package session"),
		// A call whose result never reached the file still has its arguments,
		// which is a whole expansion: what it was about to do.
		replayedCall("write", "write out.txt", `{"path":"out.txt","content":"body"}`, ""),
		// And a result with no arguments — a journal line that lost them — is
		// still worth opening for the result.
		replayedCall("ls", "ls .", "", "one.txt\ntwo.txt"),
	)
	for i := range a.entries {
		if a.entries[i].kind == entryTool && replayInert(&a.entries[i]) {
			t.Fatalf("a replayed row with a payload was called inert: %+v", a.entries[i])
		}
	}
}

// A LIVE row is never inert, whatever it is carrying: a call that has been
// announced and not begun has no result yet, and a spinner that stopped
// answering the pointer mid-turn would be the surface going dead under the
// person's hand.
func TestALiveRowIsNeverInert(t *testing.T) {
	for _, status := range []toolState{toolQueued, toolConsent, toolRunning} {
		e := entry{kind: entryTool, tool: "write", status: status}
		if replayInert(&e) {
			t.Fatalf("a live row (status %v) was called inert", status)
		}
	}
	if replayInert(&entry{kind: entryAssistant, text: "words"}) {
		t.Fatal("a paragraph was called an inert tool row")
	}
}

// ── the pictures ────────────────────────────────────────────────────────────

// A resumed message says which pictures it carried. The bytes are not in the
// journal and never were; the path is, and the NAME is what a terminal can
// honestly draw for a photograph.
func TestAReplayedMessageMarksItsPictures(t *testing.T) {
	a := resumedApp(t,
		session.DisplayEntry{
			Role:      "user",
			Text:      "what is wrong with this",
			ImageRefs: []string{"/home/me/shots/chart.png", "/home/me/shots/photo.png"},
		},
		session.DisplayEntry{Role: "assistant", Text: "the axis is unlabelled"},
	)

	frame := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"what is wrong with this", "[chart.png]", "[photo.png]"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the replayed message is missing %q:\n%s", want, frame)
		}
	}
	// The MARKER, not the path: a full path is not a thing anybody reads across
	// a transcript.
	if strings.Contains(frame, "/home/me/shots") {
		t.Fatalf("the replayed message drew whole paths:\n%s", frame)
	}
}

// A message that was ONLY a picture is still a message. It used to be dropped —
// the replay skipped anything with no text — which is a resumed conversation
// with a hole where somebody asked "this?" and got an answer.
func TestAPictureOnlyMessageSurvivesTheReplay(t *testing.T) {
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", ImageRefs: []string{"/tmp/screenshot.png"}},
		session.DisplayEntry{Role: "assistant", Text: "that is a stack trace"},
	)

	users := 0
	for _, e := range a.entries {
		if e.kind == entryUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user entries = %d, want the picture-only message kept: %+v", users, a.entries)
	}
	frame := strings.Join(plainRows(a), "\n")
	if !strings.Contains(frame, "[screenshot.png]") {
		t.Fatalf("the picture-only message drew no marker:\n%s", frame)
	}
	// And the turn still moved with it: the reply belongs to the same turn the
	// picture opened, which is what groups the cluster and what ctrl+o folds.
	if a.entries[len(a.entries)-1].turn != a.entries[0].turn {
		t.Fatalf("the reply landed in a different turn from the message it answers: %+v", a.entries)
	}
}

// A message with no words and no pictures is still nothing, and replay draws
// nothing for it.
func TestAnEmptyReplayedMessageDrawsNothing(t *testing.T) {
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", Text: "   "},
		session.DisplayEntry{Role: "assistant", Text: "hello"},
	)
	for _, e := range a.entries {
		if e.kind == entryUser {
			t.Fatalf("an empty message was drawn: %+v", e)
		}
	}
}

package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// -- helpers for the attachment grammar's tests -------------------------------

// fileset is a stub filesystem: the wiring's half of [Options.Attach], with no
// disk behind it. Every test here is a pure function of (draft, fileset), which
// is the point of keeping os.Stat on the wiring's side of the seam.
type fileset map[string]int64

func (f fileset) attach(token string) (Attachment, bool) {
	size, ok := f[token]
	if !ok {
		return Attachment{}, false
	}
	name := token
	if cut := strings.LastIndex(token, "/"); cut >= 0 {
		name = token[cut+1:]
	}
	return Attachment{Path: token, Name: name, Bytes: size}, true
}

// withFiles builds a composer that can attach exactly the named files.
func withFiles(files fileset, opts Options) *Model {
	if opts.Attach == nil {
		opts.Attach = files.attach
	}
	return New(opts)
}

// paths is what the model is holding, for the assertions that only care about
// which files rode along.
func paths(m *Model) []string {
	out := make([]string, 0, len(m.attachments))
	for _, a := range m.attachments {
		out = append(out, a.Path)
	}
	return out
}

// -- capture ------------------------------------------------------------------

func TestAttach_TypedPathLeavesTheDraftAndBecomesAnAttachment(t *testing.T) {
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{})
	typeString(m, "what colour is /tmp/dot.png")

	if got, want := m.Value(), "what colour is"; got != want {
		t.Fatalf("draft = %q, want %q — the path is not prose and does not stay in it", got, want)
	}
	if got := paths(m); len(got) != 1 || got[0] != "/tmp/dot.png" {
		t.Fatalf("attachments = %v, want [/tmp/dot.png]", got)
	}
	if m.cursor != len([]rune(m.Value())) {
		t.Fatalf("cursor = %d, want %d — it follows the text it was typing at",
			m.cursor, len([]rune(m.Value())))
	}
}

func TestAttach_PathInTheMiddleTakesOneSpaceWithIt(t *testing.T) {
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{})
	typeString(m, "look at /tmp/dot.png and tell me")

	if got, want := m.Value(), "look at and tell me"; got != want {
		t.Fatalf("draft = %q, want %q — one adjacent space goes with the token", got, want)
	}
}

func TestAttach_UnknownPathStaysInTheDraft(t *testing.T) {
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{})
	typeString(m, "read /tmp/missing.png please")

	if got, want := m.Value(), "read /tmp/missing.png please"; got != want {
		t.Fatalf("draft = %q, want it untouched — the wiring said no", got)
	}
	if len(m.attachments) != 0 {
		t.Fatalf("attachments = %v, want none", paths(m))
	}
}

func TestAttach_QuotedAndEscapedPathsWithSpaces(t *testing.T) {
	for _, c := range []struct {
		name  string
		typed string
		want  string
	}{
		{"double quoted", `see "/tmp/my shots/dot.png" now`, "see now"},
		{"single quoted", `see '/tmp/my shots/dot.png' now`, "see now"},
		{"backslash escaped", `see /tmp/my\ shots/dot.png now`, "see now"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := withFiles(fileset{"/tmp/my shots/dot.png": 68}, Options{})
			typeString(m, c.typed)
			if got := m.Value(); got != c.want {
				t.Fatalf("draft = %q, want %q", got, c.want)
			}
			if got := paths(m); len(got) != 1 || got[0] != "/tmp/my shots/dot.png" {
				t.Fatalf("attachments = %v, want the one path with a space in it", got)
			}
		})
	}
}

func TestAttach_TwoDifferentFilesBothRide(t *testing.T) {
	m := withFiles(fileset{"/tmp/a.png": 1, "/tmp/b.pdf": 2}, Options{})
	typeString(m, "compare /tmp/a.png with /tmp/b.pdf")

	if got, want := m.Value(), "compare with"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
	if got := paths(m); len(got) != 2 || got[0] != "/tmp/a.png" || got[1] != "/tmp/b.pdf" {
		t.Fatalf("attachments = %v, want both in the order they were named", got)
	}
}

func TestAttach_TheSameFileTwiceIsOneAttachment(t *testing.T) {
	m := withFiles(fileset{"/tmp/a.png": 1}, Options{})
	typeString(m, "/tmp/a.png and /tmp/a.png")

	if got := paths(m); len(got) != 1 {
		t.Fatalf("attachments = %v, want one — two references to one object is a lie about the message", got)
	}
	// Trimmed, because capture runs as the draft is typed: the first path is
	// taken the instant it is complete, and the space the person types next is
	// then a leading space in an empty draft rather than a separator between two
	// words. It is their keystroke, so it stays; the send trims it like any other.
	if got, want := strings.TrimSpace(m.Value()), "and"; got != want {
		t.Fatalf("draft = %q, want %q — both tokens still leave the text", got, want)
	}
}

func TestAttach_PasteCapturesTheSameWayTypingDoes(t *testing.T) {
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{})
	m.insert("/tmp/dot.png")

	if got := paths(m); len(got) != 1 {
		t.Fatalf("attachments = %v, want one — a drag and a paste arrive through the same door", got)
	}
	if m.Value() != "" {
		t.Fatalf("draft = %q, want empty", m.Value())
	}
}

// -- removal ------------------------------------------------------------------

func TestAttach_BackspaceOnAnEmptyDraftRemovesTheLastAttachment(t *testing.T) {
	m := withFiles(fileset{"/tmp/a.png": 1, "/tmp/b.pdf": 2}, Options{})
	typeString(m, "/tmp/a.png /tmp/b.pdf")
	if len(m.attachments) != 2 || m.Value() != "" {
		t.Fatalf("setup: draft=%q attachments=%v", m.Value(), paths(m))
	}

	m.Key(backspaceKey())
	if got := paths(m); len(got) != 1 || got[0] != "/tmp/a.png" {
		t.Fatalf("after one backspace = %v, want the last one gone (7.2)", got)
	}
	m.Key(backspaceKey())
	if got := paths(m); len(got) != 0 {
		t.Fatalf("after two backspaces = %v, want none", got)
	}
	m.Key(backspaceKey())
	if m.Value() != "" {
		t.Fatalf("draft = %q, want the extra backspace to be the no-op it always was", m.Value())
	}
}

func TestAttach_BackspaceWithWordsLeftEditsTheWords(t *testing.T) {
	m := withFiles(fileset{"/tmp/a.png": 1}, Options{})
	typeString(m, "hello /tmp/a.png")
	m.Key(backspaceKey())

	if got, want := m.Value(), "hell"; got != want {
		t.Fatalf("draft = %q, want %q — a draft with text to delete deletes text", got, want)
	}
	if len(m.attachments) != 1 {
		t.Fatalf("attachments = %v, want the chip untouched", paths(m))
	}
}

func TestAttach_EscClearsTheChipsWithTheDraft(t *testing.T) {
	m := withFiles(fileset{"/tmp/a.png": 1}, Options{})
	typeString(m, "hello /tmp/a.png")
	m.Key(escKey())

	if len(m.attachments) != 0 {
		t.Fatalf("attachments = %v, want none — a cleared draft carries nothing", paths(m))
	}
}

// -- send ---------------------------------------------------------------------

func TestAttach_SendGoesToOnSendWithTheFiles(t *testing.T) {
	var got Send
	var plain []string
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{
		OnSend:   func(s Send) { got = s },
		OnSubmit: func(text string) { plain = append(plain, text) },
	})
	typeString(m, "what colour is /tmp/dot.png")
	m.Key(enterKey())

	if len(plain) != 0 {
		t.Fatalf("OnSubmit fired %v, want OnSend instead for a send that carries files", plain)
	}
	if got.Text != "what colour is" {
		t.Fatalf("Send.Text = %q, want the words without the path", got.Text)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Path != "/tmp/dot.png" {
		t.Fatalf("Send.Attachments = %+v, want the one file", got.Attachments)
	}
	if m.Value() != "" || len(m.attachments) != 0 {
		t.Fatalf("after send: draft=%q attachments=%v, want both cleared", m.Value(), paths(m))
	}
}

func TestAttach_AFileWithNoWordsIsStillASend(t *testing.T) {
	var got Send
	sent := false
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{
		OnSend: func(s Send) { got, sent = s, true },
	})
	typeString(m, "/tmp/dot.png")
	m.Key(enterKey())

	if !sent {
		t.Fatal("a draft holding only an attachment did not send — attaching a file is speech")
	}
	if got.Text != "" {
		t.Fatalf("Send.Text = %q, want empty — the body is the wiring's to write", got.Text)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("Send.Attachments = %+v, want the one file", got.Attachments)
	}
}

func TestAttach_BlankEnterStillSendsNothing(t *testing.T) {
	sends, submits := 0, 0
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{
		OnSend:   func(Send) { sends++ },
		OnSubmit: func(string) { submits++ },
	})
	m.Key(enterKey())
	typeString(m, "   ")
	m.Key(enterKey())

	if sends+submits != 0 {
		t.Fatalf("sends=%d submits=%d, want an empty draft to stay unsent", sends, submits)
	}
}

func TestAttach_ADraftWithNoFilesStillGoesToOnSubmit(t *testing.T) {
	var plain []string
	sends := 0
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{
		OnSend:   func(Send) { sends++ },
		OnSubmit: func(text string) { plain = append(plain, text) },
	})
	typeString(m, "just words")
	m.Key(enterKey())

	if sends != 0 {
		t.Fatalf("OnSend fired %d times for a plain draft, want 0", sends)
	}
	if len(plain) != 1 || plain[0] != "just words" {
		t.Fatalf("OnSubmit got %v, want [just words]", plain)
	}
}

func TestAttach_AnAddressedSendCarriesItsFiles(t *testing.T) {
	var got Dispatch
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{
		Targets:    testTargets,
		OnDispatch: func(d Dispatch) { got = d },
		OnSend:     func(Send) { t.Fatal("OnSend fired for an addressed draft; routing wins") },
	})
	typeString(m, "@wisp-parity look at /tmp/dot.png")
	m.Key(enterKey())

	if got.TargetID != "t-wisp" {
		t.Fatalf("Dispatch.TargetID = %q, want t-wisp", got.TargetID)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Path != "/tmp/dot.png" {
		t.Fatalf("Dispatch.Attachments = %+v, want the file to ride the dispatch", got.Attachments)
	}
}

func TestAttach_AFileOnlySendLeavesTheRecallRingAlone(t *testing.T) {
	m := withFiles(fileset{"/tmp/dot.png": 68}, Options{OnSend: func(Send) {}})
	typeString(m, "remember me")
	m.Key(enterKey())
	typeString(m, "/tmp/dot.png")
	m.Key(enterKey())

	m.Key(upKey())
	if got, want := m.Value(), "remember me"; got != want {
		t.Fatalf("recall = %q, want %q — a send with no words wrote no ring entry", got, want)
	}
}

// -- chips --------------------------------------------------------------------

func TestAttach_ChipShowsNameAndSizeOverTheDraft(t *testing.T) {
	m := withFiles(fileset{"/tmp/shots/dot.png": 68}, Options{})
	typeString(m, "/tmp/shots/dot.png")

	rendered := ansi.Strip(m.Render(40, 3))
	if !strings.Contains(rendered, "dot.png") {
		t.Fatalf("render = %q, want the chip to name the file", rendered)
	}
	if !strings.Contains(rendered, "68B") {
		t.Fatalf("render = %q, want the chip to carry the size", rendered)
	}
	lines := strings.Split(rendered, "\n")
	if strings.Contains(lines[len(lines)-1], "dot.png") {
		t.Fatalf("last row = %q, want the chip ABOVE the draft row (8)", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-2], "dot.png") {
		t.Fatalf("row above the draft = %q, want the chip welded to it", lines[len(lines)-2])
	}
}

func TestAttach_ChipsFoldPastTheirBudget(t *testing.T) {
	files := fileset{"/tmp/a.png": 1, "/tmp/b.png": 2, "/tmp/c.png": 3, "/tmp/d.png": 4}
	m := withFiles(files, Options{})
	typeString(m, "/tmp/a.png /tmp/b.png /tmp/c.png /tmp/d.png")

	rendered := ansi.Strip(m.Render(40, 8))
	rows := strings.Split(rendered, "\n")
	if len(rows) > 1+attachChipMax {
		t.Fatalf("render used %d rows for 4 files, want at most %d", len(rows), 1+attachChipMax)
	}
	if !strings.Contains(rendered, "more") {
		t.Fatalf("render = %q, want the fold to say how many are held", rendered)
	}
}

func TestAttach_ChipNeverOverflowsItsWidth(t *testing.T) {
	m := withFiles(fileset{"/tmp/a-rather-long-screenshot-name.png": 4096}, Options{})
	typeString(m, "/tmp/a-rather-long-screenshot-name.png")

	for width := 1; width <= 60; width++ {
		for _, row := range strings.Split(m.Render(width, 3), "\n") {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("row %q measured %d cells at width %d", ansi.Strip(row), got, width)
			}
		}
	}
}

// -- the opt-out ---------------------------------------------------------------

func TestAttach_WithoutTheOptionNothingChanges(t *testing.T) {
	const draft = "what colour is /tmp/dot.png"
	plain := New(Options{})
	typeString(plain, draft)

	if got := plain.Value(); got != draft {
		t.Fatalf("draft = %q, want %q untouched with no Attach wired", got, draft)
	}
	if len(plain.attachments) != 0 {
		t.Fatalf("attachments = %v, want none", paths(plain))
	}
	// Byte-identity, stated as bytes: a composer whose Attach never says yes
	// draws exactly what a composer with no Attach at all draws.
	wired := withFiles(fileset{"/tmp/other.png": 1}, Options{})
	typeString(wired, draft)
	if got, want := wired.Render(40, 3), plain.Render(40, 3); got != want {
		t.Fatalf("render = %q, want %q — an unused grammar costs no cell",
			ansi.Strip(got), ansi.Strip(want))
	}
	if strings.Contains(plain.Render(40, 3), "\n") {
		t.Fatalf("render = %q, want the one draft row and no chrome under it",
			ansi.Strip(plain.Render(40, 3)))
	}
}

// -- the tokenizer ------------------------------------------------------------

func TestAttach_DraftTokensSpanWhatTheReaderSees(t *testing.T) {
	value := []rune(`a "b c" d`)
	got := draftTokens(value)
	if len(got) != 3 {
		t.Fatalf("tokens = %+v, want 3", got)
	}
	if got[1].value != "b c" {
		t.Fatalf("token 1 = %q, want %q with its quotes resolved", got[1].value, "b c")
	}
	if raw := string(value[got[1].start:got[1].end]); raw != `"b c"` {
		t.Fatalf("token 1 span = %q, want the quotes included so removing it removes them", raw)
	}
}

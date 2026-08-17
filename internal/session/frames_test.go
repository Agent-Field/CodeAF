package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── (f) compaction rung 2: bitmap frames ────────────────────────────────────

// framesWindow is a window big enough for the rung to be affordable: 40k puts
// the threshold at ~23.6k and the verbatim tail at 10k, which leaves room for
// the full eight pages (see [Agent.framePageBudget]).
const framesWindow = 40000

// prefix is a transcript segment past the keep-recent budget, so a manual
// compact() has something to preserve. bytes is approximate and generous.
func prefix(bytes int) string {
	line := "a path: internal/session/frames.go and an error: boom. "
	return strings.Repeat(line, bytes/len(line)+1)
}

// sees is the vision gate a test sets: this session's model can read images.
func sees(config *Config) {
	config.SupportsImages = func(string) bool { return true }
}

// overflowing is a transcript one long message too big for the window, which is
// what makes a manual compact() have a prefix to work on.
func overflowing(agent *Agent, text string) {
	agent.mu.Lock()
	agent.messages = append(agent.messages, textMessage("user", text))
	agent.mu.Unlock()
}

// imageParts is how many pictures one message carries.
func imageParts(message ai.Message) int {
	count := 0
	for _, part := range message.Content {
		if part.ImageURL != nil {
			count++
		}
	}
	return count
}

// A model that can see gets PAGES, and the pass makes no model call at all: the
// renderer is deterministic Go, and a rung that quietly still summarized would
// be paying for both.
func TestCompactionRendersFramesWhenTheModelSees(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = framesWindow
		sees(config)
	})
	overflowing(agent, prefix(45000))

	compacted, err := agent.compact(context.Background(), nil)
	if err != nil || !compacted {
		t.Fatalf("compact = %v, %v; want a pass that ran", compacted, err)
	}
	if completer.requests() != 0 {
		t.Fatalf("frames pass made %d model calls, want none", completer.requests())
	}

	agent.mu.Lock()
	messages := agent.messages
	agent.mu.Unlock()
	if len(messages) < 2 {
		t.Fatalf("compacted transcript = %v", rolesOf(messages))
	}
	frames := messages[1]
	if frames.Role != "user" || imageParts(frames) == 0 {
		t.Fatalf("second message = %+v, want the frames message", rolesOf(messages))
	}
	if text := messageText(frames); !strings.Contains(text, "kept as images below") {
		t.Fatalf("frames note = %q", text)
	}
	if strings.Contains(messageText(frames), "internal/session/frames.go") {
		t.Fatal("the prefix survived as text; the pages are what carries it")
	}

	// Journal-by-reference: the bytes are on disk under the workspace, and the
	// transcript carries them as a data URL of that same file.
	entries, err := os.ReadDir(droppingsDir(Place{}, workspace, droppingFrames))
	if err != nil || len(entries) == 0 {
		t.Fatalf("frames directory = %v, %v; want one page per image", entries, err)
	}
	if got, want := len(entries), imageParts(frames); got != want {
		t.Fatalf("files on disk = %d, image parts = %d", got, want)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(droppingsDir(Place{}, workspace, droppingFrames), entry.Name()))
		if err != nil {
			t.Fatalf("read page: %v", err)
		}
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("page %s is not a PNG: %v", entry.Name(), err)
		}
	}
}

// No vision, no frames: the same session summarizes exactly as it always did.
func TestCompactionSummarizesWhenTheModelIsBlind(t *testing.T) {
	for _, row := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"gate says no", func(config *Config) { config.SupportsImages = func(string) bool { return false } }},
		{"nobody can say", func(*Config) {}},
	} {
		t.Run(row.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse("## Goal\nfit the window"), nil
				},
			}}
			agent, workspace := newTestAgent(t, completer, func(config *Config) {
				config.ContextWindow = framesWindow
				row.mutate(config)
			})
			overflowing(agent, prefix(45000))

			if compacted, err := agent.compact(context.Background(), nil); err != nil || !compacted {
				t.Fatalf("compact = %v, %v", compacted, err)
			}
			if completer.requests() != 1 {
				t.Fatalf("model calls = %d, want the one summarization call", completer.requests())
			}

			agent.mu.Lock()
			note := messageText(agent.messages[1])
			images := imageParts(agent.messages[1])
			agent.mu.Unlock()
			if images != 0 {
				t.Fatalf("a blind model was sent %d pictures", images)
			}
			if !strings.Contains(note, "[context compacted]") || !strings.Contains(note, "fit the window") {
				t.Fatalf("summary note = %q", note)
			}
			if _, err := os.Stat(droppingsDir(Place{}, workspace, droppingFrames)); !os.IsNotExist(err) {
				t.Fatalf("frames directory exists after a summary pass: %v", err)
			}
		})
	}
}

// A focus is an instruction to a SUMMARIZER about what must survive being
// paraphrased. A renderer paraphrases nothing and can honour none of it, so the
// request itself sends the pass back to rung 3 — even on a model that sees.
func TestCompactionFocusForcesSummary(t *testing.T) {
	var focused string
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			focused = messageText(messages[0])
			return textResponse("## Goal\nthe API decisions"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = framesWindow
		sees(config)
	})
	overflowing(agent, prefix(45000))

	if err := agent.CompactWithFocus(context.Background(), "keep the API decisions"); err != nil {
		t.Fatalf("CompactWithFocus: %v", err)
	}
	if completer.requests() != 1 {
		t.Fatalf("model calls = %d, want the focused summarization call", completer.requests())
	}
	if !strings.Contains(focused, "Additional focus: keep the API decisions") {
		t.Fatalf("summarizer system prompt = %q", focused)
	}

	agent.mu.Lock()
	images := imageParts(agent.messages[1])
	agent.mu.Unlock()
	if images != 0 {
		t.Fatalf("a focused pass rendered %d pages; the focus has no meaning on rung 2", images)
	}
}

// Frames are a decision about the context about to be SENT, never a mode the
// session is in: the pass after a switch to a blind model summarizes, with the
// earlier pass's pages still sitting above it in the transcript.
func TestFramesAreNotSticky(t *testing.T) {
	sighted := true
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("## Goal\nfit the window"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = framesWindow
		config.SupportsImages = func(string) bool { return sighted }
	})

	overflowing(agent, prefix(45000))
	if _, err := agent.compact(context.Background(), nil); err != nil {
		t.Fatalf("first compact: %v", err)
	}
	agent.mu.Lock()
	first := imageParts(agent.messages[1])
	agent.mu.Unlock()
	if first == 0 {
		t.Fatal("the sighted pass rendered no pages")
	}

	sighted = false
	overflowing(agent, prefix(45000))
	if _, err := agent.compact(context.Background(), nil); err != nil {
		t.Fatalf("second compact: %v", err)
	}
	if completer.requests() != 1 {
		t.Fatalf("model calls = %d, want only the blind pass's summary", completer.requests())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if images := imageParts(agent.messages[1]); images != 0 {
		t.Fatalf("the blind pass rendered %d pages", images)
	}
	if note := messageText(agent.messages[1]); !strings.Contains(note, "fit the window") {
		t.Fatalf("second pass note = %q", note)
	}
}

// The page cap is a TOKEN cap wearing a page's clothes. A prefix bigger than it
// renders the head and summarizes the run-up, and the two land in the order the
// conversation happened in.
func TestFramesPageCapSummarizesTheOverflow(t *testing.T) {
	var summarized string
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			summarized = messageText(messages[1])
			return textResponse("## Goal\nthe tail that did not fit"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = framesWindow
		sees(config)
	})
	// One full-width line per row, far past framesMaxPages*framesRows of them.
	var transcript strings.Builder
	for line := 0; line < framesMaxPages*framesRows+200; line++ {
		transcript.WriteString(strings.Repeat("x", framesCols))
		transcript.WriteString("\n")
	}
	overflowing(agent, transcript.String())

	if compacted, err := agent.compact(context.Background(), nil); err != nil || !compacted {
		t.Fatalf("compact = %v, %v", compacted, err)
	}
	if completer.requests() != 1 {
		t.Fatalf("model calls = %d, want the one overflow summary", completer.requests())
	}
	if strings.Count(summarized, "\n") > 400 {
		t.Fatalf("the summarizer was sent %d lines; only the overflow should reach it",
			strings.Count(summarized, "\n"))
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if got := imageParts(agent.messages[1]); got != framesMaxPages {
		t.Fatalf("pages = %d, want the cap of %d", got, framesMaxPages)
	}
	if note := messageText(agent.messages[2]); !strings.Contains(note, "the tail that did not fit") {
		t.Fatalf("overflow summary = %q, want it after the pages", note)
	}
}

// The renderer is deterministic, which is what makes a page's digest a stable
// name on disk: the same lines render to the same bytes, and different lines do
// not.
func TestFramePageRenderIsDeterministic(t *testing.T) {
	lines := frameLines("[user]\nread internal/session/frames.go\n\n[assistant]\nboom: no such file\n")
	first, err := renderFramePage("a session", lines, 1, 1)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	second, err := renderFramePage("a session", lines, 1, 1)
	if err != nil {
		t.Fatalf("render again: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("two renders of the same page differ: %d vs %d bytes", len(first), len(second))
	}

	for _, row := range []struct {
		name  string
		page  []byte
		title string
		lines []string
		n, m  int
	}{
		{name: "different text", lines: append(lines, "one more line"), title: "a session", n: 1, m: 1},
		{name: "different title", lines: lines, title: "another session", n: 1, m: 1},
		{name: "different page number", lines: lines, title: "a session", n: 2, m: 3},
	} {
		other, err := renderFramePage(row.title, row.lines, row.n, row.m)
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if bytes.Equal(first, other) {
			t.Fatalf("%s rendered identical bytes", row.name)
		}
	}
}

// Every page is the same size and holds no more than framesRows lines: the
// geometry is arithmetic on constants, not measurement.
func TestFramesPaginate(t *testing.T) {
	lines := make([]string, framesRows*2+5)
	for index := range lines {
		lines[index] = "line"
	}
	pages, err := renderFrames("a session", lines, framesMaxPages)
	if err != nil {
		t.Fatalf("renderFrames: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3", len(pages))
	}
	width, height := 0, 0
	for index, data := range pages {
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("page %d: %v", index, err)
		}
		if index == 0 {
			width, height = config.Width, config.Height
			continue
		}
		if config.Width != width || config.Height != height {
			t.Fatalf("page %d is %dx%d, want %dx%d", index, config.Width, config.Height, width, height)
		}
	}
	if want := 2*framesMargin + framesCols*framesAdvance; width != want {
		t.Fatalf("page width = %d, want %d for %d columns", width, want, framesCols)
	}
}

// Everything the 7x13 face cannot draw is transliterated rather than dropped,
// tabs become spaces, escape sequences go, and a line longer than the page is
// wrapped rather than cut.
func TestFrameLinesSanitize(t *testing.T) {
	lines := frameLines("\x1b[31mred\x1b[0m — done · ok\tnow ☃")
	if len(lines) != 1 {
		t.Fatalf("lines = %q, want one", lines)
	}
	if got, want := lines[0], "red -- done * ok    now ?"; got != want {
		t.Fatalf("sanitized = %q, want %q", got, want)
	}

	long := strings.Repeat("x", framesCols*2+7)
	wrapped := frameLines(long)
	if len(wrapped) != 3 {
		t.Fatalf("wrapped = %d lines, want 3", len(wrapped))
	}
	if strings.Join(wrapped, "") != long {
		t.Fatal("wrapping lost characters")
	}
	for index, line := range wrapped {
		if len(line) > framesCols {
			t.Fatalf("line %d is %d columns wide", index, len(line))
		}
	}
}

// The journal writes references, never pictures, and a resume rebuilds the same
// message from them — byte for byte, because the digest still matches.
func TestFramesJournalRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = framesWindow
		config.SessionFile = path
		sees(config)
	})
	overflowing(agent, prefix(45000))
	if compacted, err := agent.compact(context.Background(), nil); err != nil || !compacted {
		t.Fatalf("compact = %v, %v", compacted, err)
	}
	agent.mu.Lock()
	live := agent.messages[1]
	agent.mu.Unlock()
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The file itself: references on the marker, and no base64 anywhere in it.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if bytes.Contains(raw, []byte("data:image/png;base64")) {
		t.Fatal("the journal holds the picture's bytes; it must hold a reference")
	}

	journal, replayed, err := openSessionFile(path, t.TempDir(), "test/model", "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	if len(replayed) == 0 {
		t.Fatal("replay returned nothing")
	}
	frames := replayed[0]
	if frames.Role != "user" || imageParts(frames) != imageParts(live) {
		t.Fatalf("replayed %d pictures, live had %d", imageParts(frames), imageParts(live))
	}
	if !strings.Contains(messageText(frames), "kept as images below") {
		t.Fatalf("replayed note = %q", messageText(frames))
	}
	for index := range frames.Content {
		if frames.Content[index].ImageURL == nil {
			continue
		}
		if frames.Content[index].ImageURL.URL != live.Content[index].ImageURL.URL {
			t.Fatalf("replayed page %d is not the page that was sent", index)
		}
	}
	// And the index knows where each picture came from, which is the question a
	// surface asks about a page it is drawing a row for.
	if refs := journal.imageRefs(frames); len(refs) != imageParts(frames) {
		t.Fatalf("imageRefs = %v, want one path per page", refs)
	}
}

// A page that was deleted or overwritten comes back as a placeholder saying so.
// A transcript that admits it lost a page is worth more than one that quietly
// resumes as if it still had it.
func TestFramesReplayWithoutTheirFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = framesWindow
		config.SessionFile = path
		config.Workspace = workspace
		sees(config)
	})
	overflowing(agent, prefix(45000))
	if _, err := agent.compact(context.Background(), nil); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := os.RemoveAll(droppingsDir(Place{}, workspace, droppingFrames)); err != nil {
		t.Fatalf("remove pages: %v", err)
	}

	journal, replayed, err := openSessionFile(path, workspace, "test/model", "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	if len(replayed) == 0 {
		t.Fatal("replay returned nothing")
	}
	if imageParts(replayed[0]) != 0 {
		t.Fatal("a deleted page came back as a picture")
	}
	if !strings.Contains(messageText(replayed[0]), "file changed or gone") {
		t.Fatalf("replayed message = %q, want a placeholder naming the page", messageText(replayed[0]))
	}
}

// The overflow case round-trips too: pages AND a summary on one marker, replayed
// in the order the conversation happened in.
func TestFramesOverflowJournalRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal, _, err := openSessionFile(path, t.TempDir(), "test/model", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	pages, err := renderFrames("a session", []string{"the first half"}, framesMaxPages)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	workspace := t.TempDir()
	ref, err := writeFrame(Place{}, workspace, pages[0])
	if err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	pass := compactionPass{
		summary: "## Goal\nthe run-up",
		frames:  []framePage{{png: pages[0], ref: ref}},
	}
	journal.appendCompaction(pass, 42000, []ai.Message{textMessage("user", "the kept tail")})
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, replayed, err := openSessionFile(path, t.TempDir(), "test/model", "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got, want := rolesOf(replayed), []string{"user", "user", "user"}; !equalStrings(got, want) {
		t.Fatalf("replayed = %v, want frames + summary + kept tail", got)
	}
	if imageParts(replayed[0]) != 1 {
		t.Fatalf("replayed frames message = %+v", replayed[0])
	}
	if !strings.Contains(messageText(replayed[1]), "the run-up") {
		t.Fatalf("replayed summary = %q", messageText(replayed[1]))
	}
	if messageText(replayed[2]) != "the kept tail" {
		t.Fatalf("replayed tail = %q", messageText(replayed[2]))
	}
	expected := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pages[0])
	if replayed[0].Content[1].ImageURL.URL != expected {
		t.Fatal("the replayed page is not the page that was written")
	}
}

// A marker written before frames existed replays exactly as it always did.
func TestCompactionMarkerWithoutPartsIsUnchanged(t *testing.T) {
	messages := compactionMessages(sessionEntry{Type: "compaction", Summary: "## Goal\nship it"})
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want one note", len(messages))
	}
	if got := messageText(messages[0]); got != compactionNote("## Goal\nship it") {
		t.Fatalf("note = %q", got)
	}
}

// No workspace, no durable page: a transcript holding pictures no resume could
// recover is worse than a summary that resumes, so the pass falls back.
func TestFramesNeedAWorkspace(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = framesWindow
		sees(config)
	})
	agent.config.Workspace = ""
	if agent.framesChosen(context.Background(), "test/model") {
		t.Fatal("frames were chosen with nowhere to write a page")
	}
}

// A page costs a flat imagePartTokens whatever is on it, so a window that cannot
// afford one summarizes. Without this a small-window session renders pages, lands
// back over its own threshold, and the very next pass throws them away — a
// picture cannot be re-rendered from a picture.
func TestFramesNeedRoomInTheWindow(t *testing.T) {
	for _, row := range []struct {
		window int
		want   int
	}{
		{window: 200, want: 0},
		// 8k: a 4k threshold over a 2k tail affords exactly one page, which is
		// 1k against the 2k of headroom left — the pass still lands well under
		// the threshold it fired at.
		{window: 8000, want: 1},
		{window: 24000, want: 4},
		{window: framesWindow, want: framesMaxPages},
		{window: 200000, want: framesMaxPages},
	} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
			config.ContextWindow = row.window
			sees(config)
		})
		if got := agent.framePageBudget(); got != row.want {
			t.Fatalf("framePageBudget(window %d) = %d, want %d", row.window, got, row.want)
		}
		if chosen := agent.framesChosen(context.Background(), "test/model"); chosen != (row.want > 0) {
			t.Fatalf("framesChosen(window %d) = %v", row.window, chosen)
		}
	}
}

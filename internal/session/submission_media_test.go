package session

// One message: words, pictures and what the person marked on them.
//
// These tests drive the real Agent against a scripted completer — the request the
// fake receives is the request the SDK would marshal — and read the real journal
// back. Nothing here calls a provider.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// writePNG writes a real, decodable picture of the given size and returns its path.
func writePNG(t *testing.T, directory, name string, width, height int, shade uint8) string {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{R: shade, G: uint8(x), B: uint8(y), A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// sparse makes a file of the given size that costs the disk nothing, so a size
// bound can be tested without a test that is itself heavy.
func sparse(t *testing.T, directory, name string, size int64) string {
	t.Helper()
	path := filepath.Join(directory, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// markedSubmission is the message a mark-up sends: the words, the original, the
// marked copy and two marks.
func markedSubmission(original, marked string) Submission {
	said := "make the sky warmer"
	area := 6.0
	return Submission{
		Text: "[image 1] original.png\n[image 2] original.marked.png\nMarks, in picture coordinates (0–100):\n1 pin at (24, 18): \"warmer\"\nChange only what I marked.",
		Said: &said,
		Attachments: []MessageAttachment{
			{ID: "at1", Kind: "file", Name: "original.png", Path: original},
			{ID: "at2", Kind: "file", Name: "original.marked.png", Path: marked},
		},
		Media: []SubmissionMedia{
			{ID: "at1", Path: original, Source: &MediaReference{SourceID: "src_abc", Path: original}},
			{ID: "at2", Path: marked},
		},
		Annotation: &Annotation{
			Original: "at1", Marked: "at2", MarksPath: filepath.Join(filepath.Dir(original), "original.marks.json"),
			ChangeOnlyMarked: true,
			Marks: []AnnotationMark{
				{Kind: "pin", Note: "warmer", At: &Point{X: 24, Y: 18}},
				{Kind: "box", From: &Point{X: 70, Y: 56}, To: &Point{X: 82, Y: 76}},
				{Kind: "shade", Note: "dim this", Shade: &Shade{Points: []Point{{X: 14, Y: 82}, {X: 16, Y: 85}}, BrushWidth: 4, AreaPercent: &area}},
			},
		},
	}
}

// journalUserLines returns every user message line of a journal.
func journalUserLines(t *testing.T, path string) []sessionEntry {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var lines []sessionEntry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Type == "message" && entry.Role == "user" {
			lines = append(lines, entry)
		}
	}
	return lines
}

func TestAComposedMessageIsOneJournaledMessageWithRealPartsAndMarks(t *testing.T) {
	completer := &scriptedCompleter{}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journal
	})
	original := writePNG(t, workspace, "original.png", 40, 30, 10)
	marked := writePNG(t, workspace, "original.marked.png", 40, 30, 200)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	receipt, events, err := agent.SubmitComposed(ctx, markedSubmission(original, marked))
	if err != nil {
		t.Fatalf("SubmitComposed: %v", err)
	}
	collect(t, events)

	if !strings.HasPrefix(receipt.MessageID, "msg_") || len(receipt.MessageID) != 28 {
		t.Fatalf("message id = %q, want msg_ and 24 hex", receipt.MessageID)
	}
	if receipt.Delivery != DeliveryImage || receipt.SeenBy != "" || receipt.Queued {
		t.Fatalf("receipt = %+v, want image delivery seen by nobody else", receipt)
	}

	// The model's request: the words, then the two pictures, as image_url parts.
	request := completer.request(0)
	sent := request[len(request)-1]
	if len(sent.Content) != 3 || sent.Content[0].Type != "text" || !strings.Contains(sent.Content[0].Text, "Marks, in picture coordinates") {
		t.Fatalf("sent content = %+v, want the words then two pictures", sent.Content)
	}
	urls := imagePartURLs(sent)
	if len(urls) != 2 || !strings.HasPrefix(urls[0], "data:image/png;base64,") || urls[0] == urls[1] {
		t.Fatalf("sent pictures = %d, want two distinct png data urls", len(urls))
	}

	// The live reading: one user entry, with the id, the measured pictures and
	// the marks, and the person's own words.
	var users []DisplayEntry
	for _, entry := range agent.Transcript() {
		if entry.Role == "user" {
			users = append(users, entry)
		}
	}
	if len(users) != 1 {
		t.Fatalf("the transcript holds %d user entries, want exactly one", len(users))
	}
	person := users[0].Person
	if person == nil || person.Composed == nil || person.Composed.MessageID != receipt.MessageID {
		t.Fatalf("person mark = %+v, want the receipt's id", person)
	}
	composed := person.Composed
	if users[0].Text != "make the sky warmer" || !person.Authored {
		t.Fatalf("entry text = %q authored=%v, want the person's own words", users[0].Text, person.Authored)
	}
	if len(composed.Media) != 2 || composed.Media[0].Width != 40 || composed.Media[0].Height != 30 ||
		composed.Media[0].MIME != "image/png" || composed.Media[0].Role != MediaRoleOriginal ||
		composed.Media[1].Role != MediaRoleMarked || composed.Media[0].SHA256 == "" ||
		composed.Media[0].Source == nil || composed.Media[0].Source.SourceID != "src_abc" {
		t.Fatalf("media facts = %+v", composed.Media)
	}
	if composed.Delivery == nil || composed.Delivery.Mode != DeliveryImage || composed.Delivery.Model != "test/model" {
		t.Fatalf("delivery = %+v", composed.Delivery)
	}
	if composed.Annotation == nil || len(composed.Annotation.Marks) != 3 || !composed.Annotation.ChangeOnlyMarked ||
		composed.Annotation.Marks[2].Shade.BrushWidth != 4 {
		t.Fatalf("annotation = %+v", composed.Annotation)
	}

	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The journal: exactly one user line, parts as references, the mark on the
	// same line, and no bytes anywhere.
	lines := journalUserLines(t, journal)
	if len(lines) != 1 {
		t.Fatalf("the journal holds %d user lines, want one", len(lines))
	}
	if len(lines[0].Parts) != 2 || lines[0].Parts[0].Path != original || lines[0].Parts[0].SHA256 == "" {
		t.Fatalf("journal parts = %+v", lines[0].Parts)
	}
	if lines[0].Presentation == nil || lines[0].Presentation.Composed == nil || lines[0].Presentation.Composed.MessageID != receipt.MessageID {
		t.Fatalf("journal presentation = %+v", lines[0].Presentation)
	}
	raw, _ := os.ReadFile(journal)
	if strings.Contains(string(raw), "data:image") || strings.Contains(string(raw), "base64") {
		t.Fatal("the journal holds picture bytes")
	}

	// Reopened for display: the same id, once, with the references and no file
	// opened (the files are removed first to prove it).
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	record := ReadTranscript(journal)
	reopened := 0
	for _, entry := range record.Entries {
		if entry.Role != "user" {
			continue
		}
		reopened++
		if entry.Person == nil || entry.Person.Composed == nil || entry.Person.Composed.MessageID != receipt.MessageID ||
			len(entry.Person.Composed.Annotation.Marks) != 3 || len(entry.ImageRefs) != 2 {
			t.Fatalf("reopened entry = %+v person=%+v", entry, entry.Person)
		}
	}
	if reopened != 1 {
		t.Fatalf("reopened %d user entries, want one", reopened)
	}
}

func TestAResumedComposedMessageSendsTheSameBytes(t *testing.T) {
	completer := &scriptedCompleter{}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journal
	})
	original := writePNG(t, workspace, "a.png", 8, 8, 1)
	marked := writePNG(t, workspace, "b.png", 8, 8, 2)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	_, events, err := agent.SubmitComposed(ctx, markedSubmission(original, marked))
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	live := imagePartURLs(agent.messages[1])
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	replayed, err := replaySessionFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	restored := imagePartURLs(replayed.messages[0])
	if len(restored) != 2 || restored[0] != live[0] || restored[1] != live[1] {
		t.Fatalf("restored pictures differ from the ones first sent (%d vs %d)", len(restored), len(live))
	}
}

func TestABlindTalkModelReadsTheSeersWordsThroughTheConfiguredVisionModel(t *testing.T) {
	completer := &scriptedCompleter{}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SupportsImages = func(model string) bool { return model == "vendor/seer" }
		config.MediaModel = func(modality string) string {
			if modality == "vision" {
				return "vendor/seer"
			}
			return ""
		}
		config.SessionFile = journal
	})
	original := writePNG(t, workspace, "a.png", 8, 8, 1)
	marked := writePNG(t, workspace, "b.png", 8, 8, 2)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	submission := markedSubmission(original, marked)
	receipt, events, err := agent.SubmitComposed(ctx, submission)
	if err != nil {
		t.Fatalf("SubmitComposed: %v", err)
	}
	collect(t, events)

	if receipt.Delivery != DeliveryDescription || receipt.SeenBy != "vendor/seer" {
		t.Fatalf("receipt = %+v, want a description by the configured vision model", receipt)
	}
	if completer.model(0) != "vendor/seer" {
		t.Fatalf("the look rode %q, want the configured vision model", completer.model(0))
	}
	seen := completer.request(0)
	if len(seen) != 1 || len(imagePartURLs(seen[0])) != 2 || !strings.Contains(seen[0].Content[0].Text, "Marks, in picture coordinates") {
		t.Fatalf("the seer was sent %+v, want the person's words and both pictures", seen)
	}

	// The talking model's transcript keeps the words it was sent (the marks text) and names the
	// files; it never holds the pictures.
	agent.mu.Lock()
	kept := agent.messages[1]
	agent.mu.Unlock()
	if len(imagePartURLs(kept)) != 0 || !strings.Contains(messageContentText(kept), "Marks, in picture coordinates") ||
		!strings.Contains(messageContentText(kept), "[attached images: a.png, b.png]") {
		t.Fatalf("kept message = %+v", kept)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	lines := journalUserLines(t, journal)
	if len(lines) != 1 || lines[0].Presentation == nil || lines[0].Presentation.Composed == nil {
		t.Fatalf("journal lines = %+v", lines)
	}
	composed := lines[0].Presentation.Composed
	if composed.MessageID != receipt.MessageID || composed.Delivery == nil ||
		composed.Delivery.Mode != DeliveryDescription || composed.Delivery.Model != "vendor/seer" ||
		len(composed.Annotation.Marks) != 3 || len(lines[0].Parts) != 2 {
		t.Fatalf("recorded composed mark = %+v parts=%d", composed, len(lines[0].Parts))
	}
}

func TestABlindModelWithNoSeerRefusesAndRecordsNothing(t *testing.T) {
	completer := &scriptedCompleter{}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SupportsImages = func(string) bool { return false }
		config.MediaModel = func(string) string { return "" }
		config.SessionFile = journal
	})
	original := writePNG(t, workspace, "a.png", 8, 8, 1)
	marked := writePNG(t, workspace, "b.png", 8, 8, 2)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	_, events, err := agent.SubmitComposed(ctx, markedSubmission(original, marked))
	if err == nil || events != nil || !strings.Contains(err.Error(), "test/model") {
		t.Fatalf("err=%v events=%v, want the blind refusal naming the model", err, events)
	}
	if completer.requests() != 0 || len(journalUserLines(t, journal)) != 0 {
		t.Fatal("a refused composed message called a model or was recorded")
	}
}

func TestAnInvalidSubmissionIsRefusedBeforeAnythingIsReadOrRecorded(t *testing.T) {
	nan := math.NaN()
	for name, mutate := range map[string]func(*Submission){
		"nan coordinate":        func(s *Submission) { s.Annotation.Marks[0].At.X = nan },
		"infinite coordinate":   func(s *Submission) { s.Annotation.Marks[0].At.Y = math.Inf(1) },
		"coordinate over 100":   func(s *Submission) { s.Annotation.Marks[0].At.X = 100.5 },
		"negative coordinate":   func(s *Submission) { s.Annotation.Marks[1].From.X = -1 },
		"inverted box":          func(s *Submission) { s.Annotation.Marks[1].To.X = 10 },
		"unknown kind":          func(s *Submission) { s.Annotation.Marks[0].Kind = "arrow" },
		"pin with a box":        func(s *Submission) { s.Annotation.Marks[0].From = &Point{X: 1, Y: 1} },
		"shade without points":  func(s *Submission) { s.Annotation.Marks[2].Shade.Points = nil },
		"shade too many points": func(s *Submission) { s.Annotation.Marks[2].Shade.Points = make([]Point, MaxShadePoints+1) },
		"shade zero brush":      func(s *Submission) { s.Annotation.Marks[2].Shade.BrushWidth = 0 },
		"note too long":         func(s *Submission) { s.Annotation.Marks[0].Note = strings.Repeat("x", MaxMarkNote+1) },
		"note not utf8":         func(s *Submission) { s.Annotation.Marks[0].Note = "\xff\xfe" },
		"too many marks": func(s *Submission) {
			s.Annotation.Marks = make([]AnnotationMark, MaxAnnotationMarks+1)
			for i := range s.Annotation.Marks {
				s.Annotation.Marks[i] = AnnotationMark{Kind: "pin", At: &Point{}}
			}
		},
		"no marks":                 func(s *Submission) { s.Annotation.Marks = nil },
		"original not a picture":   func(s *Submission) { s.Annotation.Original = "at9" },
		"marked is the original":   func(s *Submission) { s.Annotation.Marked = "at1" },
		"duplicate picture id":     func(s *Submission) { s.Media[1].ID = "at1" },
		"picture without a path":   func(s *Submission) { s.Media[0].Path = "  " },
		"negative moment":          func(s *Submission) { at := int64(-1); s.Media[0].Source.AtMS = &at },
		"too many pictures":        func(s *Submission) { s.Media = append(s.Media, make([]SubmissionMedia, MaxSubmissionMedia)...) },
		"annotation without media": func(s *Submission) { s.Media = nil },
	} {
		t.Run(name, func(t *testing.T) {
			completer := &scriptedCompleter{}
			journal := filepath.Join(t.TempDir(), "session.jsonl")
			agent, workspace := newTestAgent(t, completer, func(config *Config) {
				withVision(config)
				config.SessionFile = journal
			})
			submission := markedSubmission(writePNG(t, workspace, "a.png", 8, 8, 1), writePNG(t, workspace, "b.png", 8, 8, 2))
			mutate(&submission)
			ctx, cancel := deadline(10 * time.Second)
			defer cancel()
			_, events, err := agent.SubmitComposed(ctx, submission)
			if !errors.Is(err, ErrMessageMark) || events != nil {
				t.Fatalf("err=%v events=%v, want a refusal wrapping ErrMessageMark", err, events)
			}
			if completer.requests() != 0 || len(journalUserLines(t, journal)) != 0 {
				t.Fatal("a refused submission called a model or was recorded")
			}
		})
	}
}

func TestAPictureThatCannotBeCarriedIsRefusedWithoutBeingRead(t *testing.T) {
	// TotalAlloc measures the whole process. Run this unchanged allocation law
	// alone so background work left by other session tests cannot enter its interval.
	const childEnv = "CODEAF_TEST_IMAGE_REFUSAL_ALLOCATION_CHILD"
	if os.Getenv(childEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPictureThatCannotBeCarriedIsRefusedWithoutBeingRead$", "-test.count=1", "-test.timeout=30s")
		child.Env = append(os.Environ(), childEnv+"=1")
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("isolated allocation law: %v\n%s", err, output)
		}
		return
	}
	completer := &scriptedCompleter{}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journal
	})
	good := writePNG(t, workspace, "good.png", 8, 8, 1)
	notAnImage := writeImage(t, workspace, "fake.png", "this is text wearing a png name")
	directory := filepath.Join(workspace, "folder.png")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	huge := sparse(t, workspace, "huge.png", 64<<20)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	for name, path := range map[string]string{"text with a png name": notAnImage, "a folder": directory, "missing": filepath.Join(workspace, "gone.png")} {
		_, events, err := agent.SubmitComposed(ctx, Submission{Text: "look", Media: []SubmissionMedia{{ID: "a", Path: good}, {ID: "b", Path: path}}})
		if !errors.Is(err, ErrMessageMark) || events != nil {
			t.Fatalf("%s: err=%v, want a refusal", name, err)
		}
	}

	// A 64MB file is refused on its stat. The refusal must not allocate for it:
	// the whole call stays within a megabyte of allocation.
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _, err := agent.SubmitComposed(ctx, Submission{Text: "look", Media: []SubmissionMedia{{ID: "a", Path: huge}}})
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrMessageMark) || !strings.Contains(err.Error(), "image limit") {
		t.Fatalf("err = %v, want the image limit", err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("refusing a 64MB file allocated %d bytes", grown)
	}

	// Three pictures each under the per-image limit but over the message's are
	// refused on their stats too, before the first is read.
	runtime.ReadMemStats(&before)
	media := []SubmissionMedia{}
	for i, name := range []string{"s1.png", "s2.png", "s3.png"} {
		media = append(media, SubmissionMedia{ID: string(rune('a' + i)), Path: sparse(t, workspace, name, 9<<20)})
	}
	_, _, err = agent.SubmitComposed(ctx, Submission{Text: "look", Media: media})
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrMessageMark) || !strings.Contains(err.Error(), "a single message may carry") {
		t.Fatalf("err = %v, want the message limit", err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("refusing 27MB of pictures allocated %d bytes", grown)
	}
	if completer.requests() != 0 || len(journalUserLines(t, journal)) != 0 {
		t.Fatal("a refused picture called a model or was recorded")
	}
}

func TestACancelledSubmissionIsRefusedAndRecordsNothing(t *testing.T) {
	completer := &scriptedCompleter{}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journal
	})
	original := writePNG(t, workspace, "a.png", 8, 8, 1)
	marked := writePNG(t, workspace, "b.png", 8, 8, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, events, err := agent.SubmitComposed(ctx, markedSubmission(original, marked))
	if !errors.Is(err, context.Canceled) || events != nil {
		t.Fatalf("err=%v, want the cancellation", err)
	}
	if completer.requests() != 0 || len(journalUserLines(t, journal)) != 0 {
		t.Fatal("a cancelled submission called a model or was recorded")
	}
}

func TestAComposedMessageSteersARunningTurnAndIsRecordedOnce(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			close(started)
			<-release
			return toolResponse("call-1", "todo", `{"action":"list"}`), nil
		},
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journal
	})
	original := writePNG(t, workspace, "a.png", 8, 8, 1)
	marked := writePNG(t, workspace, "b.png", 8, 8, 2)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "start working")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	receipt, steered, err := agent.SubmitComposed(ctx, markedSubmission(original, marked))
	if err != nil || steered == nil || !receipt.Queued {
		t.Fatalf("receipt=%+v err=%v, want a queued message", receipt, err)
	}
	close(release)
	collect(t, events)

	lines := journalUserLines(t, journal)
	var composed []sessionEntry
	for _, line := range lines {
		if line.Presentation != nil && line.Presentation.Composed != nil && len(line.Presentation.Composed.Media) > 0 {
			composed = append(composed, line)
		}
	}
	if len(composed) != 1 || composed[0].Presentation.Composed.MessageID != receipt.MessageID || len(composed[0].Parts) != 2 {
		t.Fatalf("composed lines = %+v", composed)
	}
}

func TestSubmitMessageNeverDropsPicturesToFitAPlainDoor(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withVision)
	path := writePNG(t, workspace, "a.png", 8, 8, 1)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitMessage(ctx, Submission{Text: "look", Media: []SubmissionMedia{{ID: "x", Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	request := completer.request(0)
	if len(imagePartURLs(request[len(request)-1])) != 1 {
		t.Fatal("SubmitMessage dropped the picture")
	}
}

func TestAPlainComposedMessageStillGetsADurableID(t *testing.T) {
	completer := &scriptedCompleter{}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = journal })
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	receipt, events, err := agent.SubmitComposed(ctx, Submission{Text: "just words"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if receipt.Delivery != DeliveryText || receipt.MessageID == "" {
		t.Fatalf("receipt = %+v", receipt)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	lines := journalUserLines(t, journal)
	if len(lines) != 1 || lines[0].Presentation.Composed.MessageID != receipt.MessageID || len(lines[0].Parts) != 0 {
		t.Fatalf("lines = %+v", lines)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

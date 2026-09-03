package session

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// writeImage puts a file on disk with an image extension and returns its path.
// Nothing in this slice decodes a picture — the bytes travel base64'd and the
// model is the only thing that looks at them — so the content only has to be
// distinct per file, which is what makes the digest assertions mean something.
func writeImage(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// noGate leaves Config.SupportsImages nil — a caller that holds no catalog.
func noGate(*Config) {}

// withVision is the gate saying yes.
func withVision(config *Config) {
	config.SupportsImages = func(string) bool { return true }
}

func imagePartURLs(message ai.Message) []string {
	var urls []string
	for _, part := range message.Content {
		if part.Type == "image_url" && part.ImageURL != nil {
			urls = append(urls, part.ImageURL.URL)
		}
	}
	return urls
}

// ── (1) the parts a turn is actually sent ───────────────────────────────────

func TestSubmitImageAssemblesTextAndImageParts(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withVision)

	first := writeImage(t, workspace, "one.png", "PNG-ONE")
	second := writeImage(t, workspace, "two.jpg", "JPEG-TWO")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is in these?", []Image{{Path: first}, {Path: second}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)

	request := completer.request(0)
	if len(request) == 0 {
		t.Fatal("the model was never called")
	}
	sent := request[len(request)-1]
	if sent.Role != "user" {
		t.Fatalf("last message role = %q, want user", sent.Role)
	}
	if len(sent.Content) != 3 {
		t.Fatalf("content has %d parts, want 3 (text + 2 images): %+v", len(sent.Content), sent.Content)
	}
	if sent.Content[0].Type != "text" || sent.Content[0].Text != "what is in these?" {
		t.Fatalf("first part = %+v, want the person's text", sent.Content[0])
	}
	urls := imagePartURLs(sent)
	if len(urls) != 2 {
		t.Fatalf("got %d image parts, want 2", len(urls))
	}
	// base64("PNG-ONE") and base64("JPEG-TWO"), with the media type read from
	// the extension.
	if urls[0] != "data:image/png;base64,UE5HLU9ORQ==" {
		t.Fatalf("first image url = %q", urls[0])
	}
	if urls[1] != "data:image/jpeg;base64,SlBFRy1UV08=" {
		t.Fatalf("second image url = %q", urls[1])
	}
}

// A message that is only a picture is a message. Submit refuses empty text
// because a turn with nothing in it asks the model nothing; this one carries
// content either way, and the leading empty text part is left out rather than
// sent as a content block several backends reject.
func TestSubmitImageWithNoTextSendsOnlyTheImage(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withVision)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "   ", []Image{{Path: writeImage(t, workspace, "shot.png", "BYTES")}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)

	request := completer.request(0)
	sent := request[len(request)-1]
	if len(sent.Content) != 1 || sent.Content[0].Type != "image_url" {
		t.Fatalf("content = %+v, want exactly one image part", sent.Content)
	}
}

// No images is exactly Submit, so a surface with an empty tray calls one method.
func TestSubmitImageWithNoImagesIsSubmit(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, withVision)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "just words", nil)
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)

	request := completer.request(0)
	sent := request[len(request)-1]
	if sent.Role != "user" || len(sent.Content) != 1 || sent.Content[0].Text != "just words" {
		t.Fatalf("sent %+v, want the plain text message Submit would have sent", sent)
	}
}

// ── (2) the capability gate ─────────────────────────────────────────────────

func TestSubmitImageRefusesAModelThatCannotSeeAndNamesIt(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
	}{
		// Nil is FALSE here, unlike every other nil hook in Config: a caller
		// that holds no catalog must not be able to send parts to a model
		// nobody has vouched for.
		{"nil gate", noGate},
		{"gate says no", func(config *Config) { config.SupportsImages = func(string) bool { return false } }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			completer := &scriptedCompleter{}
			agent, workspace := newTestAgent(t, completer, testCase.mutate)
			agent.SetModel("vendor/blind-model")

			ctx, cancel := deadline(10 * time.Second)
			defer cancel()
			events, err := agent.SubmitImage(ctx, "look at this", []Image{
				{Path: writeImage(t, workspace, "shot.png", "BYTES")},
			})
			if err == nil {
				t.Fatal("SubmitImage accepted images for a model that cannot read them")
			}
			if events != nil {
				t.Fatal("a refused SubmitImage handed back a stream")
			}
			if !strings.Contains(err.Error(), "vendor/blind-model") {
				t.Fatalf("refusal %q does not name the model", err)
			}
			// Refused BEFORE anything is recorded: the person still holds their
			// message and the model was never called.
			if completer.requests() != 0 {
				t.Fatalf("the model was called %d times for a refused message", completer.requests())
			}
			if roles := transcriptRoles(agent); len(roles) != 1 {
				t.Fatalf("transcript = %v, want the system message alone", roles)
			}
		})
	}
}

// The gate reads the model the NEXT turn will ride, not the one the session
// started on: /model is what makes vision arrive and go away mid-session.
func TestSubmitImageGateFollowsTheCurrentModel(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SupportsImages = func(model string) bool { return model == "vendor/sees" }
	})
	path := writeImage(t, workspace, "shot.png", "BYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	if _, err := agent.SubmitImage(ctx, "look", []Image{{Path: path}}); err == nil {
		t.Fatal("the session's starting model has no vision and was accepted")
	}
	agent.SetModel("vendor/sees")
	events, err := agent.SubmitImage(ctx, "look", []Image{{Path: path}})
	if err != nil {
		t.Fatalf("SubmitImage after /model: %v", err)
	}
	collect(t, events)
}

// ── (3) the journal holds a reference, never the bytes ──────────────────────

func TestImageJournalsAReferenceAndNotTheBytes(t *testing.T) {
	completer := &scriptedCompleter{}
	directory := t.TempDir()
	journalPath := filepath.Join(directory, "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journalPath
	})
	path := writeImage(t, workspace, "shot.png", "PHOTOBYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is this?", []Image{{Path: path}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	// The whole point: a 4MB photo base64'd into a JSONL is how a session file
	// dies, so no data URL and no payload may appear anywhere in it.
	if strings.Contains(string(raw), "data:image") || strings.Contains(string(raw), "UEhPVE9CWVRFUw==") {
		t.Fatalf("the journal holds image bytes:\n%s", raw)
	}

	var entry sessionEntry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var candidate sessionEntry
		if err := json.Unmarshal([]byte(line), &candidate); err != nil {
			continue
		}
		if candidate.Type == "message" && candidate.Role == "user" {
			entry = candidate
			break
		}
	}
	if entry.Content != "what is this?" {
		t.Fatalf("journaled content = %q", entry.Content)
	}
	if len(entry.Parts) != 1 {
		t.Fatalf("journaled parts = %+v, want one reference", entry.Parts)
	}
	part := entry.Parts[0]
	if part.Type != "image" || part.Path != path || part.MIME != "image/png" {
		t.Fatalf("reference = %+v", part)
	}
	// sha256("PHOTOBYTES")
	if part.SHA256 == "" || len(part.SHA256) != 64 {
		t.Fatalf("reference digest = %q, want a sha256", part.SHA256)
	}
}

// ── (3b) replay: unchanged file round-trips, changed file is admitted ────────

func TestImageReplayRereadsAnUnchangedFile(t *testing.T) {
	completer := &scriptedCompleter{}
	directory := t.TempDir()
	journalPath := filepath.Join(directory, "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journalPath
	})
	path := writeImage(t, workspace, "shot.png", "PHOTOBYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is this?", []Image{{Path: path}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)

	live := agent.messages[1]
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	replayed, err := replaySessionFile(journalPath)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(replayed.messages) == 0 {
		t.Fatal("replay recovered nothing")
	}
	restored := replayed.messages[0]
	// Round-trip equality is the contract: a resumed turn puts the same bytes
	// on the wire as the turn that first sent the picture.
	if len(restored.Content) != len(live.Content) {
		t.Fatalf("restored %d parts, live had %d", len(restored.Content), len(live.Content))
	}
	for index := range live.Content {
		wanted, got := live.Content[index], restored.Content[index]
		if wanted.Type != got.Type || wanted.Text != got.Text {
			t.Fatalf("part %d: restored %+v, live %+v", index, got, wanted)
		}
		if (wanted.ImageURL == nil) != (got.ImageURL == nil) {
			t.Fatalf("part %d: image presence differs", index)
		}
		if wanted.ImageURL != nil && wanted.ImageURL.URL != got.ImageURL.URL {
			t.Fatalf("part %d: restored url %q, live %q", index, got.ImageURL.URL, wanted.ImageURL.URL)
		}
	}
}

func TestImageReplayPlaceholderWhenTheFileChangedOrWentAway(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		after func(t *testing.T, path string)
	}{
		{"edited", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("A DIFFERENT PICTURE"), 0o644); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
		}},
		{"deleted", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove: %v", err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			completer := &scriptedCompleter{}
			directory := t.TempDir()
			journalPath := filepath.Join(directory, "session.jsonl")
			agent, workspace := newTestAgent(t, completer, func(config *Config) {
				withVision(config)
				config.SessionFile = journalPath
			})
			path := writeImage(t, workspace, "shot.png", "PHOTOBYTES")

			ctx, cancel := deadline(10 * time.Second)
			defer cancel()
			events, err := agent.SubmitImage(ctx, "what is this?", []Image{{Path: path}})
			if err != nil {
				t.Fatalf("SubmitImage: %v", err)
			}
			collect(t, events)
			if err := agent.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			testCase.after(t, path)

			replayed, err := replaySessionFile(journalPath)
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			restored := replayed.messages[0]
			if len(restored.Content) != 2 {
				t.Fatalf("restored %+v, want text + placeholder", restored.Content)
			}
			placeholder := restored.Content[1]
			if placeholder.Type != "text" || placeholder.ImageURL != nil {
				t.Fatalf("placeholder = %+v, want a text part and no image", placeholder)
			}
			// The transcript stays honest: it says which picture is gone rather
			// than silently sending whatever now sits at that path.
			if !strings.Contains(placeholder.Text, path) || !strings.Contains(placeholder.Text, "file changed or gone") {
				t.Fatalf("placeholder text = %q", placeholder.Text)
			}
		})
	}
}

// A journal written before images existed — and every ordinary text message
// since — replays exactly as it did.
func TestReplayOfATextMessageIsUnchanged(t *testing.T) {
	entry := sessionEntry{Type: "message", Role: "assistant", Content: "plain words"}
	message := replayedMessage(entry, true)
	if len(message.Content) != 1 || message.Content[0].Type != "text" || message.Content[0].Text != "plain words" {
		t.Fatalf("replayed %+v", message.Content)
	}
}

// ── (4) the size guards ─────────────────────────────────────────────────────

func TestSubmitImageRefusesOversizeImages(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, withVision)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()

	t.Run("one image over the per-image limit", func(t *testing.T) {
		_, err := agent.SubmitImage(ctx, "big", []Image{
			{Path: "/tmp/huge.png", Bytes: make([]byte, maxImageBytes+1)},
		})
		if err == nil {
			t.Fatal("an oversize image was accepted")
		}
		if !strings.Contains(err.Error(), "huge.png") || !strings.Contains(err.Error(), "10MB") {
			t.Fatalf("refusal %q names neither the file nor the limit", err)
		}
	})

	t.Run("several images over the per-message limit", func(t *testing.T) {
		// Each is under the per-image ceiling; together they are not. Without
		// the second guard this is a 21MB turn re-sent on every step after it.
		each := make([]byte, 7<<20)
		_, err := agent.SubmitImage(ctx, "lots", []Image{
			{Path: "/tmp/a.png", Bytes: each},
			{Path: "/tmp/b.png", Bytes: each},
			{Path: "/tmp/c.png", Bytes: each},
		})
		if err == nil {
			t.Fatal("21MB of images in one message was accepted")
		}
		if !strings.Contains(err.Error(), "20MB") {
			t.Fatalf("refusal %q does not name the per-message limit", err)
		}
	})

	t.Run("a file on disk over the limit", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "big.png")
		file, err := os.Create(path)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		// Sparse: the guard reads the size before it reads the bytes, which is
		// the point — a limit enforced after the read has already paid for it.
		if err := file.Truncate(maxImageBytes + 1); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		_ = file.Close()

		if _, err := agent.SubmitImage(ctx, "big", []Image{{Path: path}}); err == nil {
			t.Fatal("an oversize file was accepted")
		}
	})

	// Nothing was recorded for any of them.
	if roles := transcriptRoles(agent); len(roles) != 1 {
		t.Fatalf("transcript = %v, want the system message alone", roles)
	}
}

func TestSubmitImageRefusesAFormatNobodyReads(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withVision)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()

	_, err := agent.SubmitImage(ctx, "look", []Image{
		{Path: writeImage(t, workspace, "notes.tiff", "TIFF")},
	})
	if err == nil {
		t.Fatal("an unsupported format was accepted")
	}
	if !strings.Contains(err.Error(), "notes.tiff") {
		t.Fatalf("refusal %q does not name the file", err)
	}
}

// ── (5) the wire ────────────────────────────────────────────────────────────

// TestImagePartsReachTheWire is the full-stack contract, the same shape as
// TestSetModelChangesTheWireModel: session.New → provider client → HTTP body.
// The SDK collapses a message's content to a bare string when it is a lone text
// part, so a build that assembled parts correctly and still sent them flattened
// would pass every test above and reach the model with no picture in it. This
// reads the image_url out of the body the server actually received.
func TestImagePartsReachTheWire(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err == nil {
			mu.Lock()
			bodies = append(bodies, body)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"a cat"}}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	workspace := t.TempDir()
	agent, err := New(Config{
		Workspace:      workspace,
		Model:          "vendor/vision-model",
		APIKey:         "test",
		BaseURL:        server.URL,
		System:         "SYSTEM",
		SupportsImages: func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer agent.Close()

	path := writeImage(t, workspace, "cat.png", "PHOTOBYTES")
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is this?", []Image{{Path: path}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	for event := range events {
		if event.Kind == EventError {
			t.Fatalf("turn errored: %v", event.Err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("the server saw no request")
	}
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(bodies[0]["messages"], &messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	last := messages[len(messages)-1]
	if last.Role != "user" {
		t.Fatalf("last wire message role = %q", last.Role)
	}
	// The system message beside it is a bare string — that collapse is the SDK's
	// compatibility shape for a lone text part, and it is exactly what a picture
	// must not get. Decoding this one as an array is the assertion: a flattened
	// image message fails here, which is the regression worth catching.
	var content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(last.Content, &content); err != nil {
		t.Fatalf("the user message's content is not a part array — a content part was flattened: %v\ncontent: %s", err, last.Content)
	}
	if len(content) != 2 {
		t.Fatalf("wire content has %d parts, want text + image: %s", len(content), last.Content)
	}
	if content[0].Type != "text" || content[0].Text != "what is this?" {
		t.Fatalf("wire text part = %+v", content[0])
	}
	image := content[1]
	if image.Type != "image_url" || image.ImageURL == nil {
		t.Fatalf("wire image part = %+v", image)
	}
	if image.ImageURL.URL != "data:image/png;base64,UEhPVE9CWVRFUw==" {
		t.Fatalf("wire image url = %q", image.ImageURL.URL)
	}
}

// ── steering ────────────────────────────────────────────────────────────────

// A picture attached while the model is working lands like any other steering
// message: at the next step boundary, in the transcript, with its journal
// reference intact — the assembly happened when the person sent it, minutes
// before the line is written.
func TestSubmitImageSteersARunningTurn(t *testing.T) {
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "todo", `{"action":"list"}`), nil
		},
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			<-release
			return textResponse("done"), nil
		},
	}}
	directory := t.TempDir()
	journalPath := filepath.Join(directory, "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journalPath
	})
	path := writeImage(t, workspace, "mid.png", "MIDTURN")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "start working")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Wait until the turn is genuinely in flight before steering into it.
	waitUntil(t, func() bool {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		return agent.running
	})
	steered, err := agent.SubmitImage(ctx, "and this one too", []Image{{Path: path}})
	if err != nil {
		t.Fatalf("steering SubmitImage: %v", err)
	}
	if steered == nil {
		t.Fatal("a steering SubmitImage handed back no stream")
	}
	close(release)
	collect(t, events)

	var found bool
	agent.mu.Lock()
	for _, message := range agent.messages {
		if message.Role == "user" && len(imagePartURLs(message)) == 1 {
			found = true
		}
	}
	agent.mu.Unlock()
	if !found {
		t.Fatal("the steered image never reached the transcript")
	}

	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(raw), `"sha256"`) {
		t.Fatalf("the steered image journaled no reference:\n%s", raw)
	}
	if strings.Contains(string(raw), "data:image") {
		t.Fatalf("the steered image journaled its bytes:\n%s", raw)
	}
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	limit := time.Now().Add(5 * time.Second)
	for time.Now().Before(limit) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition never held")
}

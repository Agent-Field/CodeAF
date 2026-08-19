package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// The scripted media client, the resolver table and the reference-file helper
// are tools_media_test.go's, shared by all three generation verbs.

// pngOfSize is a real, decodable picture — the dimensions in the tool's result
// have to come out of the bytes, so the bytes have to be an image.
func pngOfSize(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	canvas.Set(0, 0, color.RGBA{R: 200, G: 40, B: 40, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return encoded.Bytes()
}

// newPainterAgent wires a session whose media client paints and whose resolver
// answers for the image modality alone — the shape of a machine with a drawing
// model and nothing else, which is the shape most of these tests want.
func newPainterAgent(t *testing.T, painter *scriptedMedia, model string) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: model})
	})
}

// pinnedSource is a settings source holding exactly the keys it was given.
func pinnedSource(pairs map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := pairs[key]
		return value, ok
	}
}

// ── (1) the tool exists only when something is behind it ────────────────────

// A belt is a promise. A model told it can paint, whose generate_image then
// answers "no image model is configured", plans around a hand it does not have
// for the rest of the turn — so the tool is absent instead (tools_search.go's
// law, applied to the second conditional group).
func TestGenerateImageIsOnTheBeltOnlyWithAClientAndAModel(t *testing.T) {
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))}
	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{"nothing wired", func(*Config) {}, false},
		{"a resolver with no client", func(config *Config) {
			config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		}, false},
		{"a client with no resolver", func(config *Config) { config.Media = painter }, false},
		{"a client whose resolver has no image model", func(config *Config) {
			config.Media = painter
			config.MediaModel = mediaModels(map[string]string{modalitySpeech: "talk/model"})
		}, false},
		// A resolver that answers only whitespace is a resolver that answered
		// nothing: the rung it read was a settings row somebody left blank.
		{"a client whose resolver answers whitespace", func(config *Config) {
			config.Media = painter
			config.MediaModel = mediaModels(map[string]string{modalityImage: "   "})
		}, false},
		{"both", func(config *Config) {
			config.Media = painter
			config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, testCase.mutate)
			if got := hasTool(agent, "generate_image"); got != testCase.want {
				t.Fatalf("generate_image on the belt = %v, want %v", got, testCase.want)
			}
			// The wire form follows the belt: a tool that is not on one is not
			// advertised on the other.
			advertised := false
			for _, definition := range agent.definitions {
				if definition.Function.Name == "generate_image" {
					advertised = true
				}
			}
			if advertised != testCase.want {
				t.Fatalf("generate_image advertised = %v, want %v", advertised, testCase.want)
			}
		})
	}
}

// ── (2) the bytes go to disk, the path comes back ───────────────────────────

func TestGenerateImageSavesTheBytesAndReturnsPathAndDimensions(t *testing.T) {
	picture := pngOfSize(t, 40, 30)
	encoded := base64.StdEncoding.EncodeToString(picture)
	painter := &scriptedMedia{base64: encoded, mediaType: "image/png"}
	agent, workspace := newPainterAgent(t, painter, "paint/model")

	result, isError := runTool(t, agent, "generate_image", `{"prompt":"Sunset over the Harbour, 35mm"}`)
	if isError {
		t.Fatalf("generate_image failed: %s", result)
	}

	// The default path: the surface's own directory, a sortable stamp, and a
	// slug a person can recognise the prompt in.
	directory := filepath.Join(workspace, ".aforge-v3", "images")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read %s: %v", directory, err)
	}
	if len(entries) != 1 {
		t.Fatalf("%s holds %d files, want 1", directory, len(entries))
	}
	name := entries[0].Name()
	if !regexp.MustCompile(`^\d{8}-\d{6}-sunset-over-the-harbour-35mm\.png$`).MatchString(name) {
		t.Fatalf("generated name %q is not <timestamp>-<slug>.png", name)
	}

	// The bytes on disk are the bytes the provider sent, unchanged.
	written, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatalf("read the generated image: %v", err)
	}
	if !bytes.Equal(written, picture) {
		t.Fatalf("the saved file is %d bytes, want the %d the provider sent", len(written), len(picture))
	}

	// The result names the path and the size the file actually has.
	wantPath := ".aforge-v3/images/" + name
	if !strings.Contains(result, wantPath) {
		t.Fatalf("result %q does not carry the path %q", result, wantPath)
	}
	if !strings.Contains(result, "40×30") {
		t.Fatalf("result %q does not carry the dimensions", result)
	}
	if !strings.Contains(result, "paint/model") {
		t.Fatalf("result %q does not say which model painted it", result)
	}

	// AND NEVER THE BYTES. image.go's journal-by-reference law is the same law
	// here: a picture in the result is a picture re-sent on every step of every
	// turn after this one.
	if strings.Contains(result, encoded) || strings.Contains(result, encoded[:32]) {
		t.Fatal("the tool result carries the image bytes")
	}
	if len(result) > 200 {
		t.Fatalf("result is %d bytes, far past a line naming a file: %q", len(result), result)
	}

	// The request is one png, on the configured model.
	request := painter.request(0)
	if request.Model != "paint/model" || request.Prompt != "Sunset over the Harbour, 35mm" ||
		request.N != 1 || request.OutputFormat != "png" {
		t.Fatalf("image request = %+v", request)
	}
}

// "Make me three of these" is three pictures of one prompt in one second, and
// the third silently overwriting the second would lose work nobody asked to
// lose. The tool batch runs its calls in parallel, so the name is claimed rather
// than checked.
func TestGenerateImageNeverOverwritesADefaultName(t *testing.T) {
	painter := &scriptedMedia{
		base64:    base64.StdEncoding.EncodeToString(pngOfSize(t, 4, 4)),
		mediaType: "image/png",
	}
	agent, workspace := newPainterAgent(t, painter, "paint/model")

	var wait sync.WaitGroup
	results := make([]string, 3)
	for index := range results {
		wait.Add(1)
		go func(slot int) {
			defer wait.Done()
			tool := beltTool(t, agent, "generate_image")
			text, _, err := tool.Execute(context.Background(), []byte(`{"prompt":"a harbour"}`))
			if err != nil {
				t.Errorf("generate_image returned a harness error: %v", err)
			}
			results[slot] = text
		}(index)
	}
	wait.Wait()

	entries, err := os.ReadDir(filepath.Join(workspace, ".aforge-v3", "images"))
	if err != nil {
		t.Fatalf("read the image directory: %v", err)
	}
	if len(entries) != 3 {
		names := make([]string, len(entries))
		for index, entry := range entries {
			names[index] = entry.Name()
		}
		t.Fatalf("three generations left %d files: %v", len(entries), names)
	}
	for index, result := range results {
		if strings.TrimSpace(result) == "" {
			t.Fatalf("generation %d returned nothing", index)
		}
	}
}

// ── (3) a path the model chose is the path it gets ──────────────────────────

func TestGenerateImageHonoursACustomPath(t *testing.T) {
	picture := pngOfSize(t, 8, 8)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	agent, workspace := newPainterAgent(t, painter, "paint/model")

	result, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour","path":"art/hero.png"}`)
	if isError {
		t.Fatalf("generate_image failed: %s", result)
	}
	written, err := os.ReadFile(filepath.Join(workspace, "art", "hero.png"))
	if err != nil {
		t.Fatalf("the custom path was not written: %v", err)
	}
	if !bytes.Equal(written, picture) {
		t.Fatal("the custom path does not hold the generated bytes")
	}
	if !strings.Contains(result, "art/hero.png") {
		t.Fatalf("result %q does not name the custom path", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3", "images")); err == nil {
		t.Fatal("a custom path still wrote into the default directory")
	}

	// A name with no extension gets the provider's, so the file opens.
	if _, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour","path":"art/plain"}`); isError {
		t.Fatal("a path without an extension was refused")
	}
	if _, err := os.Stat(filepath.Join(workspace, "art", "plain.png")); err != nil {
		t.Fatalf("a path without an extension did not gain one: %v", err)
	}
}

// A failure is the model's to act on, never the turn's to die of.
func TestGenerateImageFailuresAreToolErrors(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		painter *scriptedMedia
		want    string
	}{
		{"the provider refused", &scriptedMedia{err: context.DeadlineExceeded}, "paint/model"},
		{"no image came back", &scriptedMedia{empty: true}, "no image"},
		{"the image was unreadable", &scriptedMedia{base64: "not base64 at all!!"}, "unreadable"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, _ := newPainterAgent(t, testCase.painter, "paint/model")
			result, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour"}`)
			if !isError {
				t.Fatalf("a failed generation reported success: %s", result)
			}
			if !strings.Contains(result, testCase.want) {
				t.Fatalf("result %q does not say %q", result, testCase.want)
			}
		})
	}
}

// The picture is paid for whether or not a turn asked for it, so it lands on the
// session's total and on no turn's.
func TestGenerateImageUsageFoldsIntoTheSessionTotal(t *testing.T) {
	cost := 0.04
	painter := &scriptedMedia{
		base64:    base64.StdEncoding.EncodeToString(pngOfSize(t, 4, 4)),
		mediaType: "image/png",
		usage:     &ai.Usage{PromptTokens: 12, CompletionTokens: 0, Cost: &cost},
	}
	agent, _ := newPainterAgent(t, painter, "paint/model")

	if _, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour"}`); isError {
		t.Fatal("generate_image failed")
	}
	usage := agent.Usage()
	if usage.Input != 12 || usage.CostUSD != cost {
		t.Fatalf("session usage = %+v, want the picture's 12 tokens and $%.2f", usage, cost)
	}
	if usage.Turns != 0 {
		t.Fatalf("session Turns = %d; a picture is not a step of the conversation", usage.Turns)
	}
}

// ── (4) the vision fallback ─────────────────────────────────────────────────

// blindWithVision is a session whose model cannot see and whose vision role
// resolves to one that can.
func blindWithVision(config *Config) {
	config.SupportsImages = func(string) bool { return false }
	config.RolesSource = pinnedSource(map[string]string{
		roles.PinKey(roles.RoleVision): "vendor/eyes",
	})
}

func TestVisionFallbackAnswersForAModelThatCannotSee(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("a red harbour at dusk"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, blindWithVision)
	agent.SetModel("vendor/blind")
	path := writeImage(t, workspace, "shot.png", "PHOTOBYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is in this?", []Image{{Path: path}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collected := collect(t, events)

	// ── the call: the parts, and the vision model ──
	if completer.requests() != 1 {
		t.Fatalf("the fallback made %d calls, want exactly one", completer.requests())
	}
	if got := completer.model(0); got != "vendor/eyes" {
		t.Fatalf("the fallback called %q, want vendor/eyes", got)
	}
	request := completer.request(0)
	if len(request) != 1 {
		t.Fatalf("the vision model was sent %d messages, want the person's one alone", len(request))
	}
	if request[0].Role != "user" {
		t.Fatalf("the vision model was sent a %q message", request[0].Role)
	}
	urls := imagePartURLs(request[0])
	if len(urls) != 1 {
		t.Fatalf("the vision call carried %d image parts, want 1", len(urls))
	}
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("PHOTOBYTES"))
	if urls[0] != wantURL {
		t.Fatalf("image part = %q, want the file's bytes", urls[0])
	}
	if text := messageContentText(request[0]); text != "what is in this?" {
		t.Fatalf("the vision call carried %q, want the person's words", text)
	}

	// ── the reply: the answer, said in somebody's name ──
	var streamed strings.Builder
	sawDone := false
	for _, event := range collected {
		switch event.Kind {
		case EventTextDelta:
			streamed.WriteString(event.Text)
		case EventTurnDone:
			sawDone = true
		case EventError:
			t.Fatalf("the fallback ended with an error: %v", event.Err)
		}
	}
	if !sawDone {
		t.Fatalf("the fallback never ended the turn; events: %v", kinds(collected))
	}
	if want := "[vision: vendor/eyes]\n\na red harbour at dusk"; streamed.String() != want {
		t.Fatalf("streamed %q, want %q", streamed.String(), want)
	}

	// ── the transcript: text, never parts ──
	if roles := transcriptRoles(agent); len(roles) != 3 ||
		roles[1] != "user" || roles[2] != "assistant" {
		t.Fatalf("transcript = %v, want system, user, assistant", roles)
	}
	agent.mu.Lock()
	recorded := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	for _, message := range recorded {
		if len(imagePartURLs(message)) != 0 {
			t.Fatal("the transcript kept image parts for a model that cannot read them")
		}
	}
	user := messageContentText(recorded[1])
	if !strings.Contains(user, "what is in this?") || !strings.Contains(user, "[attached image: shot.png]") {
		t.Fatalf("the recorded question = %q, want the words and the attachment named", user)
	}
	answer := messageContentText(recorded[2])
	if !strings.HasPrefix(answer, "[vision: vendor/eyes]") || !strings.Contains(answer, "a red harbour at dusk") {
		t.Fatalf("the recorded answer = %q", answer)
	}
}

// The picture the vision model looked at is journaled the way every other
// picture is: a reference, a digest, and never the bytes.
func TestVisionFallbackJournalsTheReference(t *testing.T) {
	completer := &scriptedCompleter{}
	journalPath := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		blindWithVision(config)
		config.SessionFile = journalPath
	})
	agent.SetModel("vendor/blind")
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
		t.Fatalf("read the journal: %v", err)
	}
	journal := string(raw)
	if !strings.Contains(journal, "shot.png") {
		t.Fatal("the journal does not name the picture")
	}
	if strings.Contains(journal, base64.StdEncoding.EncodeToString([]byte("PHOTOBYTES"))) {
		t.Fatal("the journal holds the image bytes")
	}
}

// The refusal is what is left when NOTHING can see: it fires only then, it names
// the model, and it records nothing.
func TestVisionFallbackRefusesOnlyWhenNoVisionModelResolves(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"no settings at all", func(config *Config) {
			config.SupportsImages = func(string) bool { return false }
		}},
		{"settings with nothing for vision", func(config *Config) {
			config.SupportsImages = func(string) bool { return false }
			config.RolesSource = pinnedSource(map[string]string{"roles.title": "cheap/model"})
		}},
		// A tier pointed at the model we just established cannot see is a
		// configuration, not a capability.
		{"vision resolves to the blind model itself", func(config *Config) {
			config.SupportsImages = func(string) bool { return false }
			config.RolesSource = pinnedSource(map[string]string{
				roles.TierKey(roles.TierHigh): "vendor/blind",
			})
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			completer := &scriptedCompleter{}
			agent, workspace := newTestAgent(t, completer, testCase.mutate)
			agent.SetModel("vendor/blind")

			ctx, cancel := deadline(10 * time.Second)
			defer cancel()
			events, err := agent.SubmitImage(ctx, "look at this", []Image{
				{Path: writeImage(t, workspace, "shot.png", "BYTES")},
			})
			if err == nil {
				t.Fatal("a session with no vision model accepted images")
			}
			if events != nil {
				t.Fatal("a refused SubmitImage handed back a stream")
			}
			if !strings.Contains(err.Error(), "vendor/blind") {
				t.Fatalf("refusal %q does not name the model", err)
			}
			if completer.requests() != 0 {
				t.Fatalf("a refused message still made %d calls", completer.requests())
			}
			if roles := transcriptRoles(agent); len(roles) != 1 {
				t.Fatalf("transcript = %v, want the system message alone", roles)
			}
		})
	}
}

// The fallback rides a model no turn of the person's ran on, so its usage is the
// session's and the turn reports none of it.
func TestVisionFallbackUsageFoldsIntoAuxiliaryUsage(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, blindWithVision)
	agent.SetModel("vendor/blind")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is this?", []Image{
		{Path: writeImage(t, workspace, "shot.png", "BYTES")},
	})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}

	var done Usage
	for _, event := range collect(t, events) {
		if event.Kind == EventTurnDone {
			done = event.Usage
		}
	}
	// textResponse's usage is 10 in, 5 out.
	if session := agent.Usage(); session.Input != 10 || session.Output != 5 {
		t.Fatalf("session usage = %+v, want the vision call's 10/5", session)
	}
	if done.Input != 0 || done.Output != 0 {
		t.Fatalf("the turn reported %+v, want nothing — the call was auxiliary", done)
	}
	if done.Duration <= 0 {
		t.Fatal("the turn reported no duration")
	}
	if turns := agent.Usage().Turns; turns != 0 {
		t.Fatalf("session Turns = %d, want 0 for an auxiliary call", turns)
	}
}

// The sighted path steers a running turn; this one cannot — the running turn's
// model is the one that cannot read the picture — so the person is told to wait
// instead of having their message quietly dropped into it.
func TestVisionFallbackWaitsForTheRoom(t *testing.T) {
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			<-release
			return textResponse("done"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, blindWithVision)
	agent.SetModel("vendor/blind")
	path := writeImage(t, workspace, "shot.png", "BYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "start working")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitFor(t, "the turn to be in flight", func() bool {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		return agent.running
	})

	steered, err := agent.SubmitImage(ctx, "and look at this", []Image{{Path: path}})
	if err == nil {
		t.Fatal("the fallback started a second turn on top of a running one")
	}
	if steered != nil {
		t.Fatal("a refused fallback handed back a stream")
	}
	if !strings.Contains(err.Error(), "vendor/eyes") {
		t.Fatalf("refusal %q does not name the model that would have answered", err)
	}
	close(release)
	collect(t, events)

	// Nothing of the refused message reached the running turn.
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for _, message := range agent.messages {
		if strings.Contains(messageContentText(message), "and look at this") {
			t.Fatal("the refused message was spliced into the running turn")
		}
	}
}

// A sighted model is untouched by all of this: the fallback is only ever the
// answer to a gate that said no.
func TestVisionFallbackDoesNotFireForASightedModel(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.RolesSource = pinnedSource(map[string]string{
			roles.PinKey(roles.RoleVision): "vendor/eyes",
		})
	})
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "look", []Image{
		{Path: writeImage(t, workspace, "shot.png", "BYTES")},
	})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)
	if got := completer.model(0); got != "test/model" {
		t.Fatalf("a sighted session called %q, want its own model", got)
	}
	if urls := imagePartURLs(completer.request(0)[len(completer.request(0))-1]); len(urls) != 1 {
		t.Fatal("the sighted path stopped sending image parts")
	}
}

// ── (5) image-to-image: the references, and the frame ───────────────────────

// The leverage the description teaches has to be real: a path in
// reference_paths becomes the file's own bytes on the wire, and a model can pass
// back the path this very tool just handed it.
func TestGenerateImageCarriesReferencesAndTheFrame(t *testing.T) {
	first := pngOfSize(t, 6, 6)
	painter := &scriptedMedia{
		base64:    base64.StdEncoding.EncodeToString(first),
		mediaType: "image/png",
	}
	agent, workspace := newPainterAgent(t, painter, "paint/model")

	sketch := writeReference(t, workspace, "art/sketch.png", pngOfSize(t, 3, 2))
	result, isError := runTool(t, agent, "generate_image",
		`{"prompt":"ink it","reference_paths":["`+sketch+`"],"aspect_ratio":"16:9","size":"1024x576"}`)
	if isError {
		t.Fatalf("generate_image with a reference failed: %s", result)
	}
	request := painter.request(0)
	if len(request.InputReferences) != 1 {
		t.Fatalf("the request carried %d references, want 1", len(request.InputReferences))
	}
	// The envelope is the point: input_references is an array of objects on the
	// wire, and the bare data URL this used to send was refused outright with
	// "expected object, received string" (provider.NewImageReference, and
	// TestImageReferencesRideAsObjectsOnTheWire pins the JSON itself).
	carried := referenceURL(t, request.InputReferences[0])
	if !bytes.Equal(dataURLBytes(t, carried), pngOfSize(t, 3, 2)) {
		t.Fatal("the reference did not carry the file's own bytes")
	}
	if !strings.HasPrefix(carried, "data:image/png;base64,") {
		t.Fatalf("reference %q is not a png data URL", carried[:32])
	}
	// A frame type is the video endpoint's slot; an image reference has none.
	if request.InputReferences[0].FrameType != "" {
		t.Fatalf("image reference carried a frame type %q", request.InputReferences[0].FrameType)
	}
	// The frame arguments are passed through untouched — this belt does not
	// second-guess a shape the image model spells its own way.
	if request.AspectRatio != "16:9" || request.Size != "1024x576" {
		t.Fatalf("frame = %q / %q, want 16:9 and 1024x576", request.AspectRatio, request.Size)
	}

	// And the loop the description promises: the path just returned is a valid
	// reference, so a model can iterate on its own last render.
	returned, _, _ := strings.Cut(result, " — ")
	if second, isError := runTool(t, agent, "generate_image",
		`{"prompt":"now in colour","reference_paths":["`+returned+`"]}`); isError {
		t.Fatalf("passing back the returned path failed: %s", second)
	}
	if refs := painter.request(1).InputReferences; len(refs) != 1 ||
		!bytes.Equal(dataURLBytes(t, referenceURL(t, refs[0])), first) {
		t.Fatal("the second call did not carry the first render as its reference")
	}
}

// A reference that cannot be read is the model's typo to fix, and it costs
// nothing: the refusal names the path and no generation was paid for.
func TestGenerateImageRefusesAReferenceItCannotRead(t *testing.T) {
	painter := &scriptedMedia{
		base64:    base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2)),
		mediaType: "image/png",
	}
	agent, workspace := newPainterAgent(t, painter, "paint/model")
	notes := writeReference(t, workspace, "notes.txt", []byte("not a picture"))

	for _, testCase := range []struct {
		name string
		path string
		want string
	}{
		{"a file that is not there", "art/missing.png", "could not read art/missing.png"},
		{"a file that is not a picture", notes, "notes.txt is not a picture"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, isError := runTool(t, agent, "generate_image",
				`{"prompt":"ink it","reference_paths":["`+testCase.path+`"]}`)
			if !isError {
				t.Fatalf("an unreadable reference reported success: %s", result)
			}
			if !strings.Contains(result, testCase.want) {
				t.Fatalf("result %q does not say %q", result, testCase.want)
			}
		})
	}
	if len(painter.seen) != 0 {
		t.Fatalf("a refused reference still cost %d generations", len(painter.seen))
	}
}

package main

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// TestHarnessToolArgs pins the one translation this wiring makes: a harness
// page writes a tool's arguments as a sentence, and the wire tools take JSON.
func TestHarnessToolArgs(t *testing.T) {
	cases := []struct {
		tool, args, want string
		refused          bool
	}{
		{tool: "bash", args: "go test ./...", want: `{"command":"go test ./..."}`},
		{tool: "read", args: "internal/subharness/run.go", want: `{"path":"internal/subharness/run.go"}`},
		{tool: "grep", args: "func Run", want: `{"pattern":"func Run"}`},
		{tool: "ls", args: "", want: `{}`},
		// Already JSON: passed through byte for byte, so a page that needs a
		// second field is never second-guessed.
		{tool: "bash", args: `{"command":"go vet ./...","timeout":30}`, want: `{"command":"go vet ./...","timeout":30}`},
		{tool: "edit", args: `{"path":"a.go","edits":[]}`, want: `{"path":"a.go","edits":[]}`},
		// The two that take two fields say so rather than guessing.
		{tool: "edit", args: "fix the typo", refused: true},
		{tool: "write", args: "hello", refused: true},
	}
	for _, c := range cases {
		payload, err := harnessToolArgs(c.tool, c.args)
		if c.refused {
			if err == nil {
				t.Fatalf("%s %q was accepted, want a refusal naming JSON", c.tool, c.args)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s %q: %v", c.tool, c.args, err)
		}
		if string(payload) != c.want {
			t.Fatalf("%s %q became %s, want %s", c.tool, c.args, payload, c.want)
		}
	}
}

// TestHarnessToolsBridge runs one real wire tool through the bridge, so the
// whitelist-to-belt hop is exercised rather than only described.
//
// The seams are empty here, which is a machine with no media models: the bridge
// resolves the seven wire tools and nothing else, exactly as it did before the
// media family joined the harness belt (internal/session's harness_belt.go).
func TestHarnessToolsBridge(t *testing.T) {
	tools := v3HarnessTools(t.TempDir(), session.HarnessBeltSeams{})
	out, err := tools(context.Background(), "bash", "echo harnessed")
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(out, "harnessed") {
		t.Fatalf("bash said %q", out)
	}
	if _, err := tools(context.Background(), "nosuchtool", ""); err == nil ||
		!strings.Contains(err.Error(), "no tool named") {
		t.Fatalf("an unknown tool gave %v, want a refusal", err)
	}
	// And a media verb is UNKNOWN on a machine with no media models, rather
	// than present and failing on every call.
	if _, err := tools(context.Background(), "generate_image", "a harbour"); err == nil ||
		!strings.Contains(err.Error(), "no tool named") {
		t.Fatalf("generate_image with no media wiring gave %v, want the unknown-tool refusal", err)
	}
}

// AND THE MEDIA VERBS ARE REACHABLE WHEN THE MACHINE HAS THEM. The run door
// hands its media pair down (v3RunHarness), and this is the hop that says a
// whitelisted media verb resolves to a real tool through the same bridge `bash`
// goes through — including the sentence grammar, so a harness page may write
// `generate_image` with a bare prompt the way it writes `bash` with a bare
// command.
func TestHarnessToolsBridgeReachesTheMediaVerbs(t *testing.T) {
	var asked provider.ImageRequest
	media := &harnessScriptedMedia{onImage: func(request provider.ImageRequest) { asked = request }}
	tools := v3HarnessTools(t.TempDir(), session.HarnessBeltSeams{
		Media: media,
		MediaModel: func(modality string) string {
			return map[string]string{"image": "paint/model"}[modality]
		},
	})
	if _, err := tools(context.Background(), "generate_image", "a harbour at dawn"); err != nil {
		t.Fatalf("generate_image through the harness bridge: %v", err)
	}
	if asked.Model != "paint/model" || asked.Prompt != "a harbour at dawn" {
		t.Fatalf("the harness bridge sent %+v", asked)
	}
	// The verbs this machine has no model for are still absent: presence is per
	// modality here as everywhere else.
	if _, err := tools(context.Background(), "generate_music", "a piano loop"); err == nil ||
		!strings.Contains(err.Error(), "no tool named") {
		t.Fatalf("generate_music with no music model gave %v, want the unknown-tool refusal", err)
	}
}

// harnessScriptedMedia is a [session.MediaGenerator] that records the image
// request and answers with a one-pixel png. Only the drawing half is exercised;
// the rest satisfies the interface.
type harnessScriptedMedia struct {
	onImage func(provider.ImageRequest)
}

func (m *harnessScriptedMedia) GenerateImage(_ context.Context, request provider.ImageRequest) (*provider.ImageResponse, error) {
	if m.onImage != nil {
		m.onImage(request)
	}
	return &provider.ImageResponse{Data: []provider.GeneratedImage{{
		Base64:    base64.StdEncoding.EncodeToString([]byte("not really a png, but bytes")),
		MediaType: "image/png",
	}}}, nil
}

func (m *harnessScriptedMedia) Speak(context.Context, provider.SpeechRequest) (*provider.SpeechResponse, error) {
	return &provider.SpeechResponse{}, nil
}

func (m *harnessScriptedMedia) GenerateMusic(context.Context, provider.MusicRequest) (*provider.MusicResponse, error) {
	return &provider.MusicResponse{}, nil
}

func (m *harnessScriptedMedia) GenerateVideo(context.Context, provider.VideoRequest) (*provider.VideoResponse, error) {
	return &provider.VideoResponse{}, nil
}

func (m *harnessScriptedMedia) Transcribe(context.Context, provider.TranscriptionRequest) (*provider.TranscriptionResponse, error) {
	return &provider.TranscriptionResponse{}, nil
}

// TestHarnessReport states what a turn gets back: the trail, the run's own
// output in full, and where the evidence landed — and that a FAILED run still
// reports all three rather than vanishing into an error toast.
func TestHarnessReport(t *testing.T) {
	trace := subharness.Trace{
		Id:     subharness.Id{Name: "triage", Version: 2},
		Status: subharness.StatusOK,
		Trail: []subharness.Trail{
			{Step: 1, Id: "look", Kind: subharness.KindAgentLoop, Out: "the first line\nand the rest of it", Elapsed: time.Second},
		},
	}
	report := harnessReport(trace, "/tmp/run.json", nil, nil)
	for _, want := range []string{"triage · v2 · ok", "look", "and the rest of it", "trace · /tmp/run.json"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report is missing %q:\n%s", want, report)
		}
	}

	trace.Status = subharness.StatusFailed
	trace.Trail = append(trace.Trail, subharness.Trail{Step: 2, Id: "build", Kind: subharness.KindToolCall, Err: "exit status 2"})
	failed := harnessReport(trace, "/tmp/run.json", nil, context.Canceled)
	for _, want := range []string{"failed", "exit status 2", "The run ended here"} {
		if !strings.Contains(failed, want) {
			t.Fatalf("the failed report is missing %q:\n%s", want, failed)
		}
	}
}

// TestHarnessEntriesFromStore is the detection half: what is SAVED is what a
// turn is matched against, at the head version.
func TestHarnessEntriesFromStore(t *testing.T) {
	store := subharness.At(t.TempDir())
	if entries := v3HarnessEntries(store); len(entries) != 0 {
		t.Fatalf("an empty registry offered %v", entries)
	}
	if entries := v3HarnessEntries(nil); entries != nil {
		t.Fatalf("no registry at all offered %v", entries)
	}
	page := subharness.Harness{
		Id: subharness.Id{Name: "triage", Desc: "sorts an incident out"},
		Program: subharness.Program{Nodes: []subharness.Node{
			{Id: "look", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "look at it"}},
		}},
		Verify: subharness.Verify{Ladder: subharness.VerifyAccept},
	}
	if _, err := store.Save(page); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if _, err := store.Save(page); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	entries := v3HarnessEntries(store)
	if len(entries) != 1 {
		t.Fatalf("the registry offered %v, want one entry", entries)
	}
	if entries[0].Name != "triage" || entries[0].Description != "sorts an incident out" || entries[0].Revision != 2 {
		t.Fatalf("the entry reads %+v, want the head version's own words", entries[0])
	}
	// The card can only be raised for a harness somebody NAMED, because a page
	// carries no cue list (v3HarnessEntries says why at length).
	if _, ok := subharness.Best(subharness.Turn{Text: "run the triage harness on this"}, entries); !ok {
		t.Fatalf("naming the harness did not clear the threshold")
	}
	if _, ok := subharness.Best(subharness.Turn{Text: "can you sort out this incident"}, entries); ok {
		t.Fatalf("a turn that named nothing raised the card")
	}
}

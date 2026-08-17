package session

// Tests for read's senses.
//
// Every fixture is BUILT HERE, like tools_pdf_test.go's PDFs, and for the same
// reason: a checked-in binary is a fixture nobody can review. These files are
// smaller than the real thing — a valid header and some bytes — because nothing
// in this package decodes them. The sniff reads the first sixteen bytes and the
// rungs are scripted, so a file that is honest about what it claims to be is a
// complete fixture.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── fixtures ────────────────────────────────────────────────────────────────

func pngBytes(padding int) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, padding)...)
}

func mp3Bytes(padding int) []byte {
	return append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, padding)...)
}

func mp4Bytes(padding int) []byte {
	return append([]byte("\x00\x00\x00\x18ftypisom"), make([]byte, padding)...)
}

// ── the scripted media client ───────────────────────────────────────────────

// scriptedMedia answers the one method the senses call and embeds the interface
// for the other three, so a later addition to MediaGenerator does not break
// every test in this file (media_contract.go says to do exactly this).
type scriptedMedia struct {
	MediaGenerator

	mu    sync.Mutex
	calls []provider.TranscriptionRequest
	text  string
	err   error
}

func (m *scriptedMedia) Transcribe(_ context.Context, request provider.TranscriptionRequest) (*provider.TranscriptionResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, request)
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	cost := 0.01
	return &provider.TranscriptionResponse{Text: m.text, Usage: &ai.Usage{Cost: &cost}}, nil
}

func (m *scriptedMedia) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// senseResolver is the knob lane's closure, scripted: one answer per modality.
func senseResolver(answers map[string]string) func(string) string {
	return func(modality string) string { return answers[modality] }
}

// ── the image sense ─────────────────────────────────────────────────────────

// A picture is a file, so read reads it — through the looking model, with the
// fixed extraction prompt, and the answer says who looked.
func TestReadLooksAtAnImage(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("A login screen. The heading reads \"Sign in\"."), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.MediaModel = senseResolver(map[string]string{"vision": "see/model"})
	})
	name := dropFile(t, workspace, "shot.png", pngBytes(64))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.HasPrefix(text, "[vision: see/model]\n") {
		t.Fatalf("the answer should name who looked: %q", text)
	}
	if !strings.Contains(text, "Sign in") {
		t.Fatalf("the description should be the body: %q", text)
	}
	if completer.model(0) != "see/model" {
		t.Fatalf("the look should ride the looking model, not %q", completer.model(0))
	}

	// One shot: the person's words are nowhere in it, the transcript is nowhere
	// in it, and the picture rides as a content part rather than as bytes.
	sent := completer.request(0)
	if len(sent) != 1 || sent[0].Role != "user" || len(sent[0].Content) != 2 {
		t.Fatalf("a one-shot is one user message of two parts: %+v", sent)
	}
	if sent[0].Content[0].Text != senseImagePrompt {
		t.Fatalf("the prompt is fixed: %q", sent[0].Content[0].Text)
	}
	if sent[0].Content[1].ImageURL == nil ||
		!strings.HasPrefix(sent[0].Content[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("the picture should ride as an image part: %+v", sent[0].Content[1])
	}
}

// A file's name is a claim and its first bytes are a fact: a screenshot saved
// with no extension is the file people actually drop in.
func TestReadSniffsAnImageWithoutExtension(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) { return textResponse("a chart"), nil },
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.MediaModel = senseResolver(map[string]string{"vision": "see/model"})
	})
	name := dropFile(t, workspace, "clipboard", pngBytes(32))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.Contains(text, "a chart") {
		t.Fatalf("the magic-byte sniff missed the picture: %q", text)
	}
}

// ── the audio ladder ────────────────────────────────────────────────────────

// Rung 1 is the transcription endpoint, and a real transcript stops the ladder
// there: nothing more expensive runs for a voice memo.
func TestReadTranscribesAudioOnTheFirstRung(t *testing.T) {
	media := &scriptedMedia{text: "The stand-up is moved to ten past nine tomorrow morning, in the small room."}
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.Media = media
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model", "listen": "hear/model"})
	})
	name := dropFile(t, workspace, "memo.mp3", mp3Bytes(64))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.HasPrefix(text, "[transcript: ears/model]\n") {
		t.Fatalf("the answer should name the rung that spoke: %q", text)
	}
	if !strings.Contains(text, "ten past nine") {
		t.Fatalf("the transcript should be the body: %q", text)
	}
	if completer.requests() != 0 {
		t.Fatalf("a good transcript must not climb to the listening model")
	}
	if media.calls[0].MIME != "audio/mpeg" || media.calls[0].Model != "ears/model" {
		t.Fatalf("the transcription request drifted: %+v", media.calls[0])
	}
}

// Rung 1 failing is not the answer. The ladder climbs, and the model above can
// hear the file itself.
func TestAudioLadderClimbsWhenTranscriptionFails(t *testing.T) {
	media := &scriptedMedia{err: errors.New("402 insufficient credits")}
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("Slow acoustic guitar, melancholy, no vocals."), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.Media = media
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model", "listen": "hear/model"})
	})
	name := dropFile(t, workspace, "clip.mp3", mp3Bytes(64))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.HasPrefix(text, "[audio: hear/model]\n") {
		t.Fatalf("the second rung should have answered: %q", text)
	}
	if !strings.Contains(text, "acoustic guitar") {
		t.Fatalf("the description should be the body: %q", text)
	}
	sent := completer.request(0)
	if sent[0].Content[0].Text != senseListenPrompt {
		t.Fatalf("the listening prompt is fixed: %q", sent[0].Content[0].Text)
	}
	if sent[0].Content[1].InputAudio == nil || sent[0].Content[1].InputAudio.Format != "mp3" {
		t.Fatalf("the file should ride as an input_audio part: %+v", sent[0].Content[1])
	}
}

// THIS IS THE CASE THE LADDER EXISTS FOR. A song handed to a speech recognizer
// comes back as a page of "[Music]" — a successful call returning nothing
// anybody asked for — and the model never had to choose an endpoint to get past
// it.
func TestAudioLadderClimbsPastNoise(t *testing.T) {
	media := &scriptedMedia{text: strings.Repeat("[Music] ", 20)}
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("Uptempo synthwave: arpeggiated bass, gated drums, no speech."), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.Media = media
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model", "listen": "hear/model"})
	})
	name := dropFile(t, workspace, "song.mp3", mp3Bytes(64))

	text, _ := readTool(t, agent, map[string]any{"path": name})
	if !strings.Contains(text, "synthwave") {
		t.Fatalf("a page of [Music] should have been climbed past: %q", text)
	}
}

// A thin transcript climbs too — but if there is nothing above it, the thin
// answer IS the answer. A four-second recording really does transcribe to three
// words, and a refusal invented by a threshold is worse than a short truth.
func TestAudioLadderClimbsOnThinAndKeepsItAsTheLastWord(t *testing.T) {
	media := &scriptedMedia{text: "Yes, done."}
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("A short spoken reply: \"Yes, done.\""), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.Media = media
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model", "listen": "hear/model"})
	})
	climbed, _ := readTool(t, agent, map[string]any{"path": dropFile(t, workspace, "a.mp3", mp3Bytes(64))})
	if !strings.HasPrefix(climbed, "[audio: hear/model]\n") {
		t.Fatalf("a thin transcript should climb: %q", climbed)
	}

	// The same thin transcript, with no rung above it.
	lastWord := &scriptedMedia{text: "Yes, done."}
	alone, workspaceAlone := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = lastWord
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model"})
	})
	text, isError := readTool(t, alone, map[string]any{"path": dropFile(t, workspaceAlone, "b.mp3", mp3Bytes(64))})
	if isError {
		t.Fatalf("the last rung's short truth is an answer, not a failure: %q", text)
	}
	if !strings.HasPrefix(text, "[transcript: ears/model]\n") || !strings.Contains(text, "Yes, done.") {
		t.Fatalf("the thin transcript should be kept: %q", text)
	}
}

// Every rung named, in the order tried, so the person reading the transcript
// knows whether to add credit or change a setting.
func TestAudioFailureNamesEveryRung(t *testing.T) {
	media := &scriptedMedia{err: errors.New("402 insufficient credits")}
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) { return nil, errors.New("no endpoint") },
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.Media = media
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model", "listen": "hear/model"})
	})
	text, isError := readTool(t, agent, map[string]any{"path": dropFile(t, workspace, "x.mp3", mp3Bytes(64))})
	if !isError {
		t.Fatalf("two dead rungs is an error result: %q", text)
	}
	for _, want := range []string{"ears/model: 402 insufficient credits", "hear/model: no endpoint"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the failure should name %q: %q", want, text)
		}
	}
}

// ── the video sense ─────────────────────────────────────────────────────────

func TestReadWatchesVideo(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("A terminal session; the command `make build` succeeds."), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.MediaModel = senseResolver(map[string]string{"watch": "eyes/model"})
	})
	name := dropFile(t, workspace, "capture.mp4", mp4Bytes(64))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if isError {
		t.Fatalf("read reported an error: %s", text)
	}
	if !strings.HasPrefix(text, "[video: eyes/model]\n") || !strings.Contains(text, "make build") {
		t.Fatalf("the watching answer drifted: %q", text)
	}
	sent := completer.request(0)
	if sent[0].Content[0].Text != senseWatchPrompt {
		t.Fatalf("the watching prompt is fixed: %q", sent[0].Content[0].Text)
	}
	if sent[0].Content[1].VideoURL == nil ||
		!strings.HasPrefix(sent[0].Content[1].VideoURL.URL, "data:video/mp4;base64,") {
		t.Fatalf("the film should ride as a video part: %+v", sent[0].Content[1])
	}
}

// ── the honest absences ─────────────────────────────────────────────────────

// A resolver that answers nothing is answered with a sentence naming what is
// missing — never a silent fall-through to bare, which would hand the model a
// screenful of binary and let it conclude the file was corrupt.
func TestAbsentResolverAnswersHonestly(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	for name, want := range map[string]string{
		"shot.png":    "is an image, and this session has no model that can look at one",
		"memo.mp3":    "is audio, and this session has no model that can listen to one",
		"capture.mp4": "is video, and this session has no model that can watch one",
	} {
		var body []byte
		switch {
		case strings.HasSuffix(name, ".png"):
			body = pngBytes(16)
		case strings.HasSuffix(name, ".mp3"):
			body = mp3Bytes(16)
		default:
			body = mp4Bytes(16)
		}
		text, isError := readTool(t, agent, map[string]any{"path": dropFile(t, workspace, name, body)})
		if !isError {
			t.Fatalf("%s: a missing sense is an error result: %q", name, text)
		}
		if !strings.Contains(text, want) {
			t.Fatalf("%s: honest sentence drifted: %q", name, text)
		}
	}
}

// A media client that was never wired removes the transcription rung and not
// the sense: the listening model still answers.
func TestNilMediaClientLeavesTheListeningRung(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("rain on a window"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model", "listen": "hear/model"})
	})
	text, isError := readTool(t, agent, map[string]any{"path": dropFile(t, workspace, "r.mp3", mp3Bytes(16))})
	if isError || !strings.Contains(text, "rain on a window") {
		t.Fatalf("the listening rung should have answered: %q", text)
	}
}

// A transcription model with no client behind it says which half is missing.
// "No model is set" would send the person to a settings row that already names
// one.
func TestTranscribeModelWithoutAClientSaysWhichHalfIsMissing(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model"})
	})
	text, isError := readTool(t, agent, map[string]any{"path": dropFile(t, workspace, "q.mp3", mp3Bytes(16))})
	if !isError {
		t.Fatalf("an unreachable rung is an error result: %q", text)
	}
	if !strings.Contains(text, "ears/model is set to transcribe it but this session has no media client to reach") {
		t.Fatalf("the sentence should name the half that is missing: %q", text)
	}
}

// ── the caps ────────────────────────────────────────────────────────────────

func TestSensesRefuseAnOversizeFile(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.MediaModel = senseResolver(map[string]string{"vision": "see/model"})
	})
	name := dropFile(t, workspace, "huge.png", pngBytes(maxImageBytes+1))

	text, isError := readTool(t, agent, map[string]any{"path": name})
	if !isError {
		t.Fatalf("an oversize picture is an error result: %q", text)
	}
	if text != "huge.png is over the 10MB image limit" {
		t.Fatalf("cap wording drifted: %q", text)
	}
}

// ── the memo ────────────────────────────────────────────────────────────────

// Paging through a description must be free. The rung runs once per file, and
// the second read — a page of the same answer — costs nothing.
func TestSenseMemoMakesPagingFree(t *testing.T) {
	media := &scriptedMedia{text: strings.Repeat("a line of the transcript\n", 40)}
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = media
		config.MediaModel = senseResolver(map[string]string{"transcribe": "ears/model"})
	})
	name := dropFile(t, workspace, "long.mp3", mp3Bytes(64))

	first, _ := readTool(t, agent, map[string]any{"path": name, "limit": 2})
	if !strings.Contains(first, "more lines in file. Use offset=3 to continue.]") {
		t.Fatalf("the transcript should page by pi's law: %q", first)
	}
	second, _ := readTool(t, agent, map[string]any{"path": name, "offset": 3})
	if !strings.HasPrefix(second, "[transcript: ears/model]\n") {
		t.Fatalf("the second page should carry the same provenance: %q", second)
	}
	if media.count() != 1 {
		t.Fatalf("paging paid the rung %d times; it should pay once", media.count())
	}
}

// ── pass-through, unchanged ─────────────────────────────────────────────────

// The overwhelmingly common case: an ordinary file reaches pi's read untouched.
// The senses cost one stat and a map lookup, and change nothing about the bytes.
func TestSensesLeaveOrdinaryFilesAlone(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.MediaModel = senseResolver(map[string]string{"vision": "see/model", "listen": "hear/model"})
	})
	body := "package main\n\nfunc main() {}\n"
	text, isError := readTool(t, agent, map[string]any{"path": dropFile(t, workspace, "main.go", []byte(body))})
	if isError || text != body {
		t.Fatalf("pass-through altered the file: %q", text)
	}
}

// ── the sniff ───────────────────────────────────────────────────────────────

func TestSniffReadsTheBytesWhenTheNameIsSilent(t *testing.T) {
	for _, testCase := range []struct {
		what   string
		header []byte
		want   senseKind
	}{
		{"png", pngBytes(8), senseImage},
		{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, senseImage},
		{"gif", []byte("GIF89a0123456789"), senseImage},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), senseImage},
		{"wav", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), senseAudio},
		{"ogg", []byte("OggS000000000000"), senseAudio},
		{"flac", []byte("fLaC000000000000"), senseAudio},
		{"mp3 with an id3 tag", mp3Bytes(8), senseAudio},
		{"mp4", mp4Bytes(8), senseVideo},
		{"m4a", []byte("\x00\x00\x00\x18ftypM4A "), senseAudio},
		{"matroska", []byte{0x1a, 0x45, 0xdf, 0xa3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, senseVideo},
		{"go source", []byte("package main\n\nfu"), senseNone},
		{"an mp3 frame sync, deliberately not claimed", []byte{0xff, 0xfb, 0x90, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, senseNone},
	} {
		if got := senseFromMagic(testCase.header).kind; got != testCase.want {
			t.Errorf("%s: sniffed %d, want %d", testCase.what, got, testCase.want)
		}
	}
}

func TestAudioNoiseKnowsARecognizerDescribingItself(t *testing.T) {
	for _, noise := range []string{"[Music]", strings.Repeat("[MUSIC]\n", 30), "you", "Thank you.", "(inaudible)", "[BLANK_AUDIO]"} {
		if !audioNoise(noise) {
			t.Errorf("%q should read as noise", noise)
		}
	}
	for _, speech := range []string{"Thank you for coming in today.", "[Music] and then she said hello", "Yes, done."} {
		if audioNoise(speech) {
			t.Errorf("%q is speech, not noise", speech)
		}
	}
}

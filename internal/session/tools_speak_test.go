package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── (1) the tool exists only when something is behind it ────────────────────

// The absence law, per verb: the client and the SPEECH model must both be
// there. A machine that can paint and cannot talk does not get a speak tool it
// would only ever refuse with.
func TestSpeakIsOnTheBeltOnlyWithAClientAndASpeechModel(t *testing.T) {
	media := &scriptedMedia{audio: []byte("ID3 audio")}
	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{"nothing wired", func(*Config) {}, false},
		{"a resolver with no client", func(config *Config) {
			config.MediaModel = mediaModels(map[string]string{modalitySpeech: "talk/model"})
		}, false},
		{"a client with no resolver", func(config *Config) { config.Media = media }, false},
		{"a client whose resolver only paints", func(config *Config) {
			config.Media = media
			config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		}, false},
		{"both", func(config *Config) {
			config.Media = media
			config.MediaModel = mediaModels(map[string]string{modalitySpeech: "talk/model"})
		}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, testCase.mutate)
			if got := hasTool(agent, "speak"); got != testCase.want {
				t.Fatalf("speak on the belt = %v, want %v", got, testCase.want)
			}
		})
	}
}

// ── (2) the bytes go to disk, the path comes back ───────────────────────────

func TestSpeakSavesTheAudioAndReturnsThePath(t *testing.T) {
	spoken := []byte("ID3\x04this is an mp3 as far as anybody here knows")
	media := &scriptedMedia{audio: spoken}
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newMediaAgent(t, media, func(config *Config) {
		config.ArtifactsIndex = index
	})

	result, isError := runTool(t, agent, "speak", `{"text":"Good morning, Harbour Road"}`)
	if isError {
		t.Fatalf("speak failed: %s", result)
	}

	directory := filepath.Join(workspace, ".aforge-v3", "audio")
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%s holds %v (%v), want one file", directory, entries, err)
	}
	name := entries[0].Name()
	if !regexp.MustCompile(`^\d{8}-\d{6}-good-morning-harbour-road\.mp3$`).MatchString(name) {
		t.Fatalf("generated name %q is not <timestamp>-<slug>.mp3", name)
	}
	written, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil || !bytes.Equal(written, spoken) {
		t.Fatalf("the saved file is not the audio the provider sent (%v)", err)
	}

	// The result is one line: where it is, how big it is, who said it — and
	// never the audio, which is image.go's law applied to a bigger blob.
	if !strings.Contains(result, ".aforge-v3/audio/"+name) {
		t.Fatalf("result %q does not carry the path", result)
	}
	if !strings.Contains(result, "talk/model") {
		t.Fatalf("result %q does not say which model spoke", result)
	}
	if strings.Contains(result, "ID3") {
		t.Fatal("the tool result carries the audio bytes")
	}

	// The request: the text verbatim, mp3, and NO voice — the provider's own
	// default is what an absent voice means.
	request := media.speech(0)
	if request.Model != "talk/model" || request.Input != "Good morning, Harbour Road" {
		t.Fatalf("speech request = %+v", request)
	}
	if request.ResponseFormat != "mp3" {
		t.Fatalf("speech request asked for %q, want mp3", request.ResponseFormat)
	}
	if request.Voice != "" {
		t.Fatalf("an absent voice was sent as %q, want the provider's default", request.Voice)
	}

	// And the row: a deliverable nobody can find again is not a deliverable.
	rows := ReadArtifacts(index)
	if len(rows) != 1 || rows[0].Kind != "audio" {
		t.Fatalf("artifact rows = %+v, want one audio row", rows)
	}
	if rows[0].Title != "good morning harbour road" {
		t.Fatalf("row title = %q, want the text's own words", rows[0].Title)
	}
	if rows[0].Path != filepath.Join(directory, name) {
		t.Fatalf("row path = %q, want the file that was written", rows[0].Path)
	}
}

// A voice the model named rides through untouched, and a path it chose is the
// path it gets.
func TestSpeakHonoursAVoiceAndAPath(t *testing.T) {
	media := &scriptedMedia{audio: []byte("ID3 audio")}
	agent, workspace := newMediaAgent(t, media, nil)

	result, isError := runTool(t, agent, "speak",
		`{"text":"the harbour at dawn","voice":"alto","path":"clips/intro"}`)
	if isError {
		t.Fatalf("speak failed: %s", result)
	}
	if got := media.speech(0).Voice; got != "alto" {
		t.Fatalf("voice = %q, want alto", got)
	}
	// A name with no extension gets one, so the file plays when it is clicked.
	if _, err := os.Stat(filepath.Join(workspace, "clips", "intro.mp3")); err != nil {
		t.Fatalf("the custom path was not written: %v", err)
	}
	if !strings.Contains(result, "clips/intro.mp3") {
		t.Fatalf("result %q does not name the custom path", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3", "audio")); err == nil {
		t.Fatal("a custom path still wrote into the default directory")
	}
}

// Every failure is the model's to act on, never the turn's to die of, and each
// one names the model so "try another" is advice it can follow.
func TestSpeakFailuresAreToolErrors(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		media *scriptedMedia
		args  string
		want  string
	}{
		{"no text", &scriptedMedia{audio: []byte("x")}, `{"text":"  "}`, "text is required"},
		{"arguments that do not parse", &scriptedMedia{audio: []byte("x")}, `{"text":`, "Invalid arguments"},
		{"the provider refused", &scriptedMedia{speechErr: context.DeadlineExceeded}, `{"text":"hello"}`, "talk/model"},
		{"no audio came back", &scriptedMedia{}, `{"text":"hello"}`, "no audio"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, _ := newMediaAgent(t, testCase.media, nil)
			result, isError := runTool(t, agent, "speak", testCase.args)
			if !isError {
				t.Fatalf("a failed speak reported success: %s", result)
			}
			if !strings.Contains(result, testCase.want) {
				t.Fatalf("result %q does not say %q", result, testCase.want)
			}
		})
	}
}

// The audio is paid for whether or not anybody plays it, so it lands on the
// session's total and on no turn's.
func TestSpeakUsageFoldsIntoTheSessionTotal(t *testing.T) {
	cost := 0.02
	media := &scriptedMedia{
		audio:      []byte("ID3 audio"),
		speechCost: &ai.Usage{PromptTokens: 9, Cost: &cost},
	}
	agent, _ := newMediaAgent(t, media, nil)

	if _, isError := runTool(t, agent, "speak", `{"text":"hello"}`); isError {
		t.Fatal("speak failed")
	}
	usage := agent.Usage()
	if usage.Input != 9 || usage.CostUSD != cost {
		t.Fatalf("session usage = %+v, want the audio's 9 tokens and $%.2f", usage, cost)
	}
	if usage.Turns != 0 {
		t.Fatalf("session Turns = %d; speaking is not a step of the conversation", usage.Turns)
	}
}

// The landing law is one law for every modality: a BORROWED session's audio
// lands in the session's own artifacts/ and never in the person's repository.
func TestSpokenAudioLandsInArtifactsForABorrowedSession(t *testing.T) {
	media := &scriptedMedia{audio: []byte("ID3 audio")}
	place := newPlace(t, false)
	agent, workspace := newMediaAgent(t, media, func(config *Config) {
		config.Place = place
	})
	if result, isError := runTool(t, agent, "speak", `{"text":"a harbour"}`); isError {
		t.Fatalf("speak failed: %s", result)
	}
	entries, err := os.ReadDir(place.Artifacts())
	if err != nil || len(entries) != 1 {
		t.Fatalf("artifacts directory = %v, %v; want one audio file", entries, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session spoke into the person's repository: %v", err)
	}
}

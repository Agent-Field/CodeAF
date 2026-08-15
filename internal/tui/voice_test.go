package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/voice"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type scriptedTranscriber struct {
	results map[string]voice.Transcript
	errors  map[string]error
}

func (s scriptedTranscriber) Transcribe(_ context.Context, audio []byte, _ voice.TranscriptionOptions) (voice.Transcript, error) {
	return s.results[string(audio)], s.errors[string(audio)]
}

func voiceTestModel(recorder voice.Recorder, transcriber voice.Transcriber) *Model {
	commander := newFakeCommander()
	commander.current["voice"] = "qwen/asr"
	return NewWithVoice(&fakeBackend{}, "voice", commander, recorder, transcriber)
}

func startScriptedVoice(t *testing.T, model *Model, recorder *voice.ScriptedRecorder) int {
	t.Helper()
	_ = model.startVoice()
	if err := recorder.Start(); err != nil {
		t.Fatal(err)
	}
	generation := model.voiceGeneration
	_ = model.applyVoiceStarted(voiceStartedMsg{generation: generation})
	if model.voiceState != voiceRecording {
		t.Fatalf("state = %v, want recording", model.voiceState)
	}
	return generation
}

func TestVoiceStateMachinePreservesTypedDraftAndFinalizesFullClip(t *testing.T) {
	recorder := &voice.ScriptedRecorder{Final: []byte("full")}
	transcriber := scriptedTranscriber{results: map[string]voice.Transcript{
		"chunk-one": {Text: "live one"},
		"chunk-two": {Text: "live two"},
		"full":      {Text: "final words"},
	}}
	model := voiceTestModel(recorder, transcriber)
	model.input.SetValue("typed before")
	generation := startScriptedVoice(t, model, recorder)
	model.input.SetValue("typed before and during")

	// Results deliberately arrive out of order; pending text must not.
	model.voiceInFlight = 2
	_ = model.applyVoiceChunkResult(voiceChunkResultMsg{generation: generation, index: 1, transcript: transcriber.results["chunk-two"]})
	if model.voicePending != "" {
		t.Fatalf("out-of-order chunk became visible: %q", model.voicePending)
	}
	_ = model.applyVoiceChunkResult(voiceChunkResultMsg{generation: generation, index: 0, transcript: transcriber.results["chunk-one"]})
	if model.voicePending != "live one live two" {
		t.Fatalf("pending = %q", model.voicePending)
	}
	if !strings.Contains(model.renderInput(), "live one live two") {
		t.Fatal("pending transcript is not rendered inline")
	}

	model.voiceState = voiceFinalizing
	_, _ = recorder.Stop()
	_ = model.applyVoiceChunk(voiceChunkMsg{generation: generation, closed: true})
	_ = model.applyVoiceFinal(voiceFinalResultMsg{generation: generation, transcript: transcriber.results["full"]})
	if got := model.input.Value(); got != "typed before and during final words" {
		t.Fatalf("merged draft = %q", got)
	}
	if model.voiceState != voiceIdle || model.voicePending != "" {
		t.Fatalf("voice state was not cleared: state=%v pending=%q", model.voiceState, model.voicePending)
	}
}

func TestVoiceCancelDiscardsOnlyTranscript(t *testing.T) {
	recorder := &voice.ScriptedRecorder{Final: []byte("full")}
	model := voiceTestModel(recorder, scriptedTranscriber{})
	model.input.SetValue("draft")
	_ = startScriptedVoice(t, model, recorder)
	model.input.SetValue("draft typed during")
	model.voicePending = "provisional words"
	command := model.cancelVoice()
	if command != nil {
		_ = command()
	}
	if got := model.input.Value(); got != "draft typed during" {
		t.Fatalf("cancel changed draft to %q", got)
	}
	if model.voicePending != "" || model.voiceHint != "voice discarded" {
		t.Fatalf("cancel state = pending %q hint %q", model.voicePending, model.voiceHint)
	}
}

func TestVoiceFinalFailureUsesOrderedLiveTranscript(t *testing.T) {
	recorder := &voice.ScriptedRecorder{Final: []byte("full")}
	model := voiceTestModel(recorder, scriptedTranscriber{})
	model.input.SetValue("draft")
	generation := startScriptedVoice(t, model, recorder)
	model.voiceInFlight = 2
	_ = model.applyVoiceChunkResult(voiceChunkResultMsg{generation: generation, index: 0, err: errors.New("chunk failed")})
	_ = model.applyVoiceChunkResult(voiceChunkResultMsg{
		generation: generation, index: 1, transcript: voice.Transcript{Text: "live fallback"},
	})
	model.voiceState = voiceFinalizing
	model.voiceChunksClosed = true
	_ = model.applyVoiceFinal(voiceFinalResultMsg{generation: generation, err: errors.New("offline")})
	if got := model.input.Value(); got != "draft live fallback" {
		t.Fatalf("fallback merge = %q", got)
	}
	if model.voiceHint != "voice: used live transcript — final pass failed" {
		t.Fatalf("hint = %q", model.voiceHint)
	}
}

func TestVoiceTotalFailureKeepsDraftAndStartFailureIsCalm(t *testing.T) {
	recorder := &voice.ScriptedRecorder{StartErr: errors.New("permission denied")}
	model := voiceTestModel(recorder, scriptedTranscriber{})
	model.input.SetValue("sacred draft")
	_ = model.startVoice()
	_ = model.applyVoiceStarted(voiceStartedMsg{generation: model.voiceGeneration, err: recorder.StartErr})
	if got := model.input.Value(); got != "sacred draft" {
		t.Fatalf("start failure changed draft to %q", got)
	}
	if model.voiceHint != "mic unavailable — check System Settings › Privacy › Microphone" {
		t.Fatalf("hint = %q", model.voiceHint)
	}

	recorder.StartErr = nil
	generation := startScriptedVoice(t, model, recorder)
	model.input.SetValue("sacred draft plus typing")
	model.voiceState = voiceFinalizing
	model.voiceChunksClosed = true
	_ = model.applyVoiceFinal(voiceFinalResultMsg{generation: generation, err: errors.New("failed")})
	if got := model.input.Value(); got != "sacred draft plus typing" {
		t.Fatalf("total failure changed draft to %q", got)
	}
	if model.voicePending != "" || model.voiceState != voiceIdle {
		t.Fatalf("orphaned provisional state: %q %v", model.voicePending, model.voiceState)
	}
}

func TestWaveformRenderingAtFixedLevels(t *testing.T) {
	levels := []float64{0, 0.15, 0.30, 0.45, 0.60, 0.75, 0.90, 1}
	if got, want := renderWaveform(levels, 8), "▁▂▃▄▅▆▇█"; got != want {
		t.Fatalf("waveform = %q, want %q", got, want)
	}
}

func TestVoicePickerUsesReusableFilteredList(t *testing.T) {
	model := voiceTestModel(&voice.ScriptedRecorder{}, scriptedTranscriber{})
	model.modelRole = "voice"
	model.voiceModelCatalog = []ModelChoice{
		{Slug: "qwen/asr-flash", Name: "Qwen Flash"},
		{Slug: "openai/gpt-audio", Name: "GPT Audio"},
	}
	model.input.SetValue("qwen")
	choices := model.modelPickerSeam().filtered(model.input.Value())
	if len(choices) != 1 || choices[0].Slug != "qwen/asr-flash" {
		t.Fatalf("filtered choices = %+v", choices)
	}
}

func TestVoiceKeyboardAndMicClickShareTheToggle(t *testing.T) {
	for _, test := range []struct {
		name string
		act  func(*Model) tea.Cmd
	}{
		{
			name: "keyboard",
			act: func(model *Model) tea.Cmd {
				_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
				return command
			},
		},
		{
			name: "click",
			act: func(model *Model) tea.Cmd {
				_ = model.View() // establishes the right-edge mic hit target
				_, command := model.Update(tea.MouseMsg{
					X: model.micBounds.x, Y: model.micBounds.y,
					Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
				})
				return command
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := voiceTestModel(&voice.ScriptedRecorder{}, scriptedTranscriber{})
			if command := test.act(model); command == nil {
				t.Fatal("toggle did not return a capture command")
			}
			if model.voiceState != voiceStarting {
				t.Fatalf("state = %v, want starting", model.voiceState)
			}
		})
	}
}

func TestVoiceDropdownChoiceHasMouseSelectionPath(t *testing.T) {
	commander := newFakeCommander()
	commander.current["voice"] = "first/asr"
	model := NewWithCommander(&fakeBackend{}, "voice-picker", commander)
	model.voiceModelCatalog = []ModelChoice{{Slug: "first/asr"}, {Slug: "second/asr"}}
	model.voiceCatalogRequested = true
	_ = model.openModelPicker("voice")
	model.paletteSelected = 1
	_ = model.View()
	if len(model.modelPickerRows) < 2 {
		t.Fatalf("voice dropdown rows = %#v", model.modelPickerRows)
	}
	row := model.modelPickerRows[1]
	_, _ = model.Update(tea.MouseMsg{
		X: row.bounds.x + 1, Y: row.bounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if commander.setRole != "voice" || commander.setModel != "second/asr" {
		t.Fatalf("mouse selection = (%q, %q)", commander.setRole, commander.setModel)
	}
}

func TestVoiceDiscardHasEscapeAndClickPaths(t *testing.T) {
	for _, click := range []bool{false, true} {
		model := voiceTestModel(&voice.ScriptedRecorder{}, scriptedTranscriber{})
		model.voiceState = voiceRecording
		model.voiceGeneration = 1
		model.voicePending = "pending"
		model.input.SetValue("draft")
		var command tea.Cmd
		if click {
			_ = model.View()
			_, command = model.Update(tea.MouseMsg{
				X: model.voiceCancelBounds.x, Y: model.voiceCancelBounds.y,
				Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
			})
		} else {
			_, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		}
		if command == nil || model.voiceState != voiceIdle || model.input.Value() != "draft" || model.voicePending != "" {
			t.Fatalf("click=%v discard = command %v state %v draft %q pending %q",
				click, command != nil, model.voiceState, model.input.Value(), model.voicePending)
		}
	}
}

func TestVoiceCoexistsWithPendingTextQuestion(t *testing.T) {
	model := voiceTestModel(&voice.ScriptedRecorder{}, scriptedTranscriber{})
	model.cards = []jobCard{{
		ID: "ask", State: cardQuestion, QuestionKind: questionText,
		Title: "Waiting", Question: "Name the release branch",
	}}
	model.setSize(100, 30)

	// alt+v still starts capture while the question is pending.
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if command == nil || model.voiceState != voiceStarting {
		t.Fatalf("alt+v with a pending question = command %v state %v", command != nil, model.voiceState)
	}

	// The provisional transcript and the "answering:" context line stay on
	// separate rows: the question owns the surface's first line, the live
	// transcript rides the prompt line beneath it.
	model.voiceState = voiceRecording
	model.voicePending = "provisional words"
	lines := strings.Split(model.renderInput(), "\n")
	if len(lines) < 2 {
		t.Fatalf("input surface = %q", lines)
	}
	first, rest := lines[0], strings.Join(lines[1:], "\n")
	if !strings.Contains(first, "answering: Name the release branch") || strings.Contains(first, "provisional words") {
		t.Fatalf("question line collided with the transcript: %q", first)
	}
	if !strings.Contains(rest, "provisional words") {
		t.Fatalf("provisional transcript missing from the prompt line: %q", rest)
	}
	// The frame's top edge sits between the question line and the prompt row.
	if model.micBounds.y != model.textQuestionDismissBounds.y+2 {
		t.Fatalf("mic row %d does not sit below the question row %d", model.micBounds.y, model.textQuestionDismissBounds.y)
	}
}

func TestVoiceInputNeverExceedsPaneWidth(t *testing.T) {
	model := voiceTestModel(&voice.ScriptedRecorder{}, scriptedTranscriber{})
	model.voiceState = voiceRecording
	model.voiceStartedAt = time.Now()
	model.voicePending = strings.Repeat("provisional ", 12)
	model.input.SetValue(strings.Repeat("typed-draft ", 12))
	model.setSize(42, 20)
	for index, line := range strings.Split(model.renderInput(), "\n") {
		if width := lipgloss.Width(line); width > 42 {
			t.Fatalf("line %d width = %d, want <= 42: %q", index, width, line)
		}
	}
}

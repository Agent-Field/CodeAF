package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/voice"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type voiceState uint8

const (
	voiceIdle voiceState = iota
	voiceStarting
	voiceRecording
	voiceFinalizing
)

type voiceServices interface {
	VoiceRecorder() voice.Recorder
	VoiceTranscriber() voice.Transcriber
	RecordVoiceUsage(float64)
}

type voiceStartedMsg struct {
	generation int
	err        error
}

type voiceChunkMsg struct {
	generation int
	chunk      voice.Chunk
	closed     bool
}

type voiceChunkResultMsg struct {
	generation int
	index      int
	transcript voice.Transcript
	err        error
}

type voiceFinalResultMsg struct {
	generation int
	transcript voice.Transcript
	err        error
}

type voiceStoppedMsg struct{}
type voiceUsageRecordedMsg struct{}

func (m *Model) toggleVoice() tea.Cmd {
	m.voiceUsed = true
	switch m.voiceState {
	case voiceIdle:
		return m.startVoice()
	case voiceStarting:
		return m.cancelVoice()
	case voiceRecording:
		return m.finalizeVoice()
	default:
		return m.flashVoiceHint("finishing voice input…")
	}
}

func (m *Model) startVoice() tea.Cmd {
	if m.voiceRecorder == nil || m.voiceTranscriber == nil {
		return m.flashVoiceHint("mic unavailable — check System Settings › Privacy › Microphone")
	}
	m.voiceGeneration++
	generation := m.voiceGeneration
	m.voiceState = voiceStarting
	m.voiceStartedAt = time.Now()
	m.voicePending = ""
	m.voiceChunkText = map[int]string{}
	m.voiceNextChunk = 0
	m.voiceInFlight = 0
	m.voiceChunksClosed = false
	m.voiceFinalReady = false
	m.voiceFinalText = ""
	m.voiceFinalErr = nil
	m.voiceLevels = nil
	m.voiceHintShown = true
	m.voiceHint = ""
	m.voiceContext, m.voiceCancel = context.WithCancel(context.Background())
	m.setSize(m.width, m.height)
	recorder := m.voiceRecorder
	start := func() tea.Msg {
		return voiceStartedMsg{generation: generation, err: recorder.Start()}
	}
	return tea.Batch(start, m.scheduleAnimation())
}

func (m *Model) applyVoiceStarted(message voiceStartedMsg) tea.Cmd {
	if message.generation != m.voiceGeneration || m.voiceState != voiceStarting {
		if message.err == nil && m.voiceRecorder != nil {
			recorder := m.voiceRecorder
			return func() tea.Msg {
				_, _ = recorder.Stop()
				return voiceStoppedMsg{}
			}
		}
		return nil
	}
	if message.err != nil {
		m.resetVoice()
		return m.flashVoiceHint("mic unavailable — check System Settings › Privacy › Microphone")
	}
	m.voiceState = voiceRecording
	m.voiceStartedAt = time.Now()
	m.setSize(m.width, m.height)
	return tea.Batch(waitForVoiceChunk(message.generation, m.voiceRecorder.Chunks()), m.scheduleAnimation())
}

func waitForVoiceChunk(generation int, chunks <-chan voice.Chunk) tea.Cmd {
	return func() tea.Msg {
		if chunks == nil {
			return voiceChunkMsg{generation: generation, closed: true}
		}
		chunk, ok := <-chunks
		return voiceChunkMsg{generation: generation, chunk: chunk, closed: !ok}
	}
}

func (m *Model) applyVoiceChunk(message voiceChunkMsg) tea.Cmd {
	if message.generation != m.voiceGeneration || m.voiceState == voiceIdle {
		return nil
	}
	if message.closed {
		m.voiceChunksClosed = true
		if m.voiceState == voiceRecording {
			recorder := m.voiceRecorder
			m.resetVoice()
			m.flashVoiceHint("mic unavailable — check System Settings › Privacy › Microphone")
			return func() tea.Msg {
				if recorder != nil {
					_, _ = recorder.Stop()
				}
				return voiceStoppedMsg{}
			}
		}
		m.completeVoiceIfReady()
		return nil
	}
	m.voiceInFlight++
	transcriber := m.voiceTranscriber
	model := m.currentModel("voice")
	chunk := message.chunk
	voiceContext := m.voiceContext
	if voiceContext == nil {
		voiceContext = context.Background()
	}
	transcribe := func() tea.Msg {
		result, err := transcriber.Transcribe(voiceContext, chunk.Audio, voice.TranscriptionOptions{Model: model})
		return voiceChunkResultMsg{generation: message.generation, index: chunk.Index, transcript: result, err: err}
	}
	return tea.Batch(transcribe, waitForVoiceChunk(message.generation, m.voiceRecorder.Chunks()))
}

func (m *Model) applyVoiceChunkResult(message voiceChunkResultMsg) tea.Cmd {
	usageCommand := m.recordVoiceUsage(message.transcript.Usage.Cost)
	if message.generation != m.voiceGeneration || m.voiceState == voiceIdle {
		return usageCommand
	}
	if m.voiceInFlight > 0 {
		m.voiceInFlight--
	}
	text := ""
	if message.err == nil {
		text = strings.TrimSpace(message.transcript.Text)
	}
	m.voiceChunkText[message.index] = text
	for {
		part, ok := m.voiceChunkText[m.voiceNextChunk]
		if !ok {
			break
		}
		delete(m.voiceChunkText, m.voiceNextChunk)
		m.voiceNextChunk++
		if part != "" {
			m.voicePending = joinVoiceText(m.voicePending, part)
		}
	}
	m.setSize(m.width, m.height)
	m.completeVoiceIfReady()
	return usageCommand
}

func (m *Model) finalizeVoice() tea.Cmd {
	if m.voiceState != voiceRecording {
		return nil
	}
	m.voiceState = voiceFinalizing
	m.setSize(m.width, m.height)
	generation := m.voiceGeneration
	recorder := m.voiceRecorder
	transcriber := m.voiceTranscriber
	model := m.currentModel("voice")
	voiceContext := m.voiceContext
	if voiceContext == nil {
		voiceContext = context.Background()
	}
	finalize := func() tea.Msg {
		audio, stopErr := recorder.Stop()
		if len(audio) == 0 {
			if stopErr == nil {
				stopErr = errors.New("microphone captured no audio")
			}
			return voiceFinalResultMsg{generation: generation, err: stopErr}
		}
		result, err := transcriber.Transcribe(voiceContext, audio, voice.TranscriptionOptions{Model: model})
		if err == nil && strings.TrimSpace(result.Text) == "" {
			err = errors.New("voice transcription was empty")
		}
		return voiceFinalResultMsg{generation: generation, transcript: result, err: err}
	}
	return tea.Batch(finalize, m.scheduleAnimation())
}

func (m *Model) applyVoiceFinal(message voiceFinalResultMsg) tea.Cmd {
	usageCommand := m.recordVoiceUsage(message.transcript.Usage.Cost)
	if message.generation != m.voiceGeneration || m.voiceState != voiceFinalizing {
		return usageCommand
	}
	m.voiceFinalReady = true
	m.voiceFinalText = strings.TrimSpace(message.transcript.Text)
	m.voiceFinalErr = message.err
	m.completeVoiceIfReady()
	return usageCommand
}

func (m *Model) completeVoiceIfReady() {
	if m.voiceState != voiceFinalizing || !m.voiceFinalReady {
		return
	}
	if m.voiceFinalErr == nil && m.voiceFinalText != "" {
		m.mergeVoiceText(m.voiceFinalText)
		m.resetVoice()
		return
	}
	// A failed final pass waits until every silence chunk has arrived and every
	// live request has resolved before deciding whether a useful fallback exists.
	if !m.voiceChunksClosed || m.voiceInFlight > 0 {
		return
	}
	if strings.TrimSpace(m.voicePending) != "" {
		m.mergeVoiceText(m.voicePending)
		m.resetVoice()
		m.flashVoiceHint("voice: used live transcript — final pass failed")
		return
	}
	m.resetVoice()
	m.flashVoiceHint("voice unavailable — your draft was kept")
}

func (m *Model) cancelVoice() tea.Cmd {
	if m.voiceState == voiceIdle {
		return nil
	}
	recorder := m.voiceRecorder
	m.voiceGeneration++ // invalidate any transcription already in flight
	m.resetVoice()
	m.flashVoiceHint("voice discarded")
	if recorder == nil {
		return nil
	}
	return func() tea.Msg {
		_, _ = recorder.Stop()
		return voiceStoppedMsg{}
	}
}

func (m *Model) resetVoice() {
	if m.voiceCancel != nil {
		m.voiceCancel()
	}
	m.voiceContext = nil
	m.voiceCancel = nil
	m.voiceState = voiceIdle
	m.voiceStartedAt = time.Time{}
	m.voicePending = ""
	m.voiceChunkText = map[int]string{}
	m.voiceNextChunk = 0
	m.voiceInFlight = 0
	m.voiceChunksClosed = false
	m.voiceFinalReady = false
	m.voiceFinalText = ""
	m.voiceFinalErr = nil
	m.voiceLevels = nil
	m.setSize(m.width, m.height)
}

func (m *Model) mergeVoiceText(transcript string) {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return
	}
	m.input.SetValue(joinVoiceText(m.input.Value(), transcript))
	m.paletteDismissed = false
	m.syncPalette()
}

func joinVoiceText(draft, transcript string) string {
	if draft == "" {
		return transcript
	}
	if transcript == "" {
		return draft
	}
	last, _ := lastRune(draft)
	if unicode.IsSpace(last) {
		return draft + transcript
	}
	return draft + " " + transcript
}

func lastRune(text string) (rune, int) {
	if text == "" {
		return 0, 0
	}
	char, size := utf8.DecodeLastRuneInString(text)
	return char, size
}

func (m *Model) recordVoiceUsage(cost float64) tea.Cmd {
	if cost <= 0 {
		return nil
	}
	if services, ok := m.commander.(voiceServices); ok {
		return func() tea.Msg {
			services.RecordVoiceUsage(cost)
			return voiceUsageRecordedMsg{}
		}
	}
	return nil
}

func (m *Model) flashVoiceHint(hint string) tea.Cmd {
	m.voiceHint = hint
	m.voiceHintUntil = time.Now().Add(statusTTL)
	return nil
}

func (m *Model) voiceAnimating() bool { return m.voiceState != voiceIdle }

func (m *Model) sampleVoiceLevel() {
	if m.voiceState != voiceRecording || m.voiceRecorder == nil {
		return
	}
	m.voiceLevels = append(m.voiceLevels, m.voiceRecorder.Level())
	if len(m.voiceLevels) > 8 {
		m.voiceLevels = append([]float64(nil), m.voiceLevels[len(m.voiceLevels)-8:]...)
	}
}

func renderWaveform(levels []float64, bars int) string {
	const heights = "▁▂▃▄▅▆▇█"
	if bars <= 0 {
		return ""
	}
	shown := make([]float64, bars)
	if len(levels) > bars {
		levels = levels[len(levels)-bars:]
	}
	copy(shown[bars-len(levels):], levels)
	var result strings.Builder
	for _, level := range shown {
		level = max(0, min(1, level))
		index := int(level * float64(len([]rune(heights))-1))
		result.WriteRune([]rune(heights)[index])
	}
	return result.String()
}

func (m *Model) voiceControl() string {
	glyph := m.voiceMicGlyph()
	switch m.voiceState {
	case voiceStarting:
		return glyph + " " + mutedStyle.Render("········ 0:00") + " " + mutedStyle.Faint(true).Render("⟨×⟩")
	case voiceRecording:
		elapsed := time.Since(m.voiceStartedAt)
		return glyph + " " + mutedStyle.Render(renderWaveform(m.voiceLevels, 8)+" "+formatVoiceElapsed(elapsed)) + " " + mutedStyle.Faint(true).Render("⟨×⟩")
	case voiceFinalizing:
		elapsed := time.Since(m.voiceStartedAt)
		return glyph + " " + mutedStyle.Render(renderWaveform(m.voiceLevels, 8)+" "+formatVoiceElapsed(elapsed)) + " " + mutedStyle.Faint(true).Render("⟨×⟩")
	default:
		return glyph
	}
}

func (m *Model) voiceMicGlyph() string {
	if m.voiceState == voiceIdle {
		return mutedStyle.Faint(true).Render("◌")
	}
	return m.matteSweep("⟨●⟩")
}

func (m *Model) voiceControlWidth() int { return lipgloss.Width(m.voiceControl()) + 1 }

func formatVoiceElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	minutes := int(elapsed / time.Minute)
	seconds := int(elapsed/time.Second) % 60
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

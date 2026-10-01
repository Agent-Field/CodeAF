package tui3

import "testing"

func TestComposingPickerKeepsAudioRowsMarkedAsMusic(t *testing.T) {
	if !composesMusic(Model{ID: "google/lyria-3-clip-preview", Output: []string{"text", "audio"}}) {
		t.Fatal("the composing picker rejected Lyria's audio output row")
	}
	if composesMusic(Model{ID: "openai/gpt-4o-mini-tts", Output: []string{"audio"}}) {
		t.Fatal("the composing picker admitted an unmarked speech row")
	}
}

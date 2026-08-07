package voice

import (
	"encoding/binary"
	"testing"
)

func pcmFrames(amplitude int16, frames int) []byte {
	pcm := make([]byte, frameBytes*frames)
	for index := 0; index < len(pcm); index += 2 {
		binary.LittleEndian.PutUint16(pcm[index:index+2], uint16(amplitude))
	}
	return pcm
}

func TestVADCutsOnlyAfterMinimumSpeechAndSustainedSilence(t *testing.T) {
	vad := NewVAD()
	if chunks := vad.Push(pcmFrames(12_000, 55)); len(chunks) != 0 {
		t.Fatalf("cut a chunk before 1.5s: %d", len(chunks))
	}
	if chunks := vad.Push(pcmFrames(0, 19)); len(chunks) != 0 {
		t.Fatalf("cut on less than 400ms silence: %d", len(chunks))
	}
	chunks := vad.Push(pcmFrames(0, 1))
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want one at the 400ms boundary", len(chunks))
	}
	if got, want := len(chunks[0]), frameBytes*75; got != want {
		t.Fatalf("chunk bytes = %d, want %d", got, want)
	}
}

func TestVADDoesNotEmitAChunkForRoomSilence(t *testing.T) {
	vad := NewVAD()
	if chunks := vad.Push(pcmFrames(0, 200)); len(chunks) != 0 {
		t.Fatalf("silent room emitted %d chunks", len(chunks))
	}
}

func TestWAVHeaderDescribesSixteenKMonoPCM(t *testing.T) {
	pcm := pcmFrames(1_000, 1)
	wav, err := WAV(pcm)
	if err != nil {
		t.Fatal(err)
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("invalid RIFF/WAVE markers: %q %q", wav[:4], wav[8:12])
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != SampleRate {
		t.Fatalf("sample rate = %d", got)
	}
	if got := binary.LittleEndian.Uint16(wav[22:24]); got != Channels {
		t.Fatalf("channels = %d", got)
	}
	if got := int(binary.LittleEndian.Uint32(wav[40:44])); got != len(pcm) {
		t.Fatalf("data size = %d, want %d", got, len(pcm))
	}
}

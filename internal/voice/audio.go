// Package voice owns microphone capture and OpenRouter speech-to-text.
// The terminal model depends only on the small Recorder and Transcriber
// interfaces here, so its state machine never needs audio hardware in tests.
package voice

import (
	"encoding/binary"
	"errors"
	"math"
)

const (
	SampleRate       = 16_000
	Channels         = 1
	BitsPerSample    = 16
	MaxAudioBytes    = 25 << 20
	frameDurationMS  = 20
	frameBytes       = SampleRate * Channels * (BitsPerSample / 8) * frameDurationMS / 1000
	defaultThreshold = 0.015
	defaultSilence   = 400
	defaultMinimum   = 1500
)

// Chunk is a silence-delimited WAV clip. Index is monotonic within one Start
// call and lets the UI preserve spoken order even when requests finish out of
// order.
type Chunk struct {
	Index int
	Audio []byte
}

// Recorder is the complete microphone seam used by the TUI.
type Recorder interface {
	Start() error
	Stop() ([]byte, error)
	Level() float64
	Chunks() <-chan Chunk
}

// VAD cuts pcm16 mono audio only after a sustained low-energy interval. It is
// intentionally small: the final transcription always uses the uninterrupted
// clip; these chunks exist solely to provide honest live text.
type VAD struct {
	Threshold     float64
	SilenceMillis int
	MinimumMillis int

	remainder     []byte
	segment       []byte
	segmentFrames int
	silenceFrames int
	heardSpeech   bool
}

// NewVAD returns the capture defaults: a 400ms silence boundary and a 1.5s
// minimum chunk so normal pauses do not split words.
func NewVAD() *VAD {
	return &VAD{Threshold: defaultThreshold, SilenceMillis: defaultSilence, MinimumMillis: defaultMinimum}
}

// Push consumes arbitrary byte boundaries and returns zero or more raw pcm16
// chunks. An incomplete 20ms frame is retained for the next call.
func (v *VAD) Push(pcm []byte) [][]byte {
	if len(pcm) == 0 {
		return nil
	}
	data := append(v.remainder, pcm...)
	v.remainder = v.remainder[:0]
	var chunks [][]byte
	for len(data) >= frameBytes {
		frame := data[:frameBytes]
		data = data[frameBytes:]
		v.segment = append(v.segment, frame...)
		v.segmentFrames++
		if PCM16RMS(frame) < v.threshold() {
			v.silenceFrames++
		} else {
			v.silenceFrames = 0
			v.heardSpeech = true
		}
		if v.heardSpeech && v.segmentFrames >= v.minimumFrames() && v.silenceFrames >= v.silenceBoundaryFrames() {
			chunks = append(chunks, append([]byte(nil), v.segment...))
			v.segment = v.segment[:0]
			v.segmentFrames = 0
			v.silenceFrames = 0
			v.heardSpeech = false
		}
	}
	v.remainder = append(v.remainder, data...)
	return chunks
}

func (v *VAD) threshold() float64 {
	if v.Threshold <= 0 {
		return defaultThreshold
	}
	return v.Threshold
}

func (v *VAD) minimumFrames() int {
	millis := v.MinimumMillis
	if millis <= 0 {
		millis = defaultMinimum
	}
	return max(1, millis/frameDurationMS)
}

func (v *VAD) silenceBoundaryFrames() int {
	millis := v.SilenceMillis
	if millis <= 0 {
		millis = defaultSilence
	}
	return max(1, millis/frameDurationMS)
}

// PCM16RMS returns a normalized 0..1 root-mean-square level.
func PCM16RMS(pcm []byte) float64 {
	samples := len(pcm) / 2
	if samples == 0 {
		return 0
	}
	var sum float64
	for index := 0; index+1 < len(pcm); index += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm[index : index+2]))
		value := float64(sample) / 32768
		sum += value * value
	}
	return math.Sqrt(sum / float64(samples))
}

// WAV wraps raw 16kHz mono pcm16 in the canonical 44-byte RIFF header.
func WAV(pcm []byte) ([]byte, error) {
	if len(pcm)%2 != 0 {
		return nil, errors.New("pcm16 audio has an incomplete sample")
	}
	if len(pcm)+44 > MaxAudioBytes {
		return nil, errors.New("audio exceeds OpenRouter's 25MB limit")
	}
	wav := make([]byte, 44+len(pcm))
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], Channels)
	binary.LittleEndian.PutUint32(wav[24:28], SampleRate)
	byteRate := SampleRate * Channels * (BitsPerSample / 8)
	binary.LittleEndian.PutUint32(wav[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(wav[32:34], Channels*(BitsPerSample/8))
	binary.LittleEndian.PutUint16(wav[34:36], BitsPerSample)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(len(pcm)))
	copy(wav[44:], pcm)
	return wav, nil
}

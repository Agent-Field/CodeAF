package voice

import (
	"encoding/binary"
	"testing"
	"time"
)

// speech builds pcm16 loud enough for the VAD to call it speech, silence the
// same length of nothing. A chunk is cut after the minimum speech followed by
// the silence boundary, so the two together are one chunk's worth of audio.
func speech(millis int) []byte {
	samples := SampleRate * millis / 1000
	pcm := make([]byte, samples*2)
	for index := 0; index < samples; index++ {
		value := int16(6000)
		if index%2 == 0 {
			value = -6000
		}
		binary.LittleEndian.PutUint16(pcm[index*2:], uint16(value))
	}
	return pcm
}

func silence(millis int) []byte {
	return make([]byte, SampleRate*millis/1000*2)
}

// The deadlock, at the unit that had it. A consumer that stopped draining used
// to park the reader goroutine on the send forever: command.Wait was never
// reached, the child never reaped, and Stop blocked on a done that nobody would
// ever write. Killing the process cannot free a goroutine blocked on a channel.
func TestRecorderNeverParksOnAChunkNobodyTakes(t *testing.T) {
	recorder := newCommandRecorder("unused", nil)
	recorder.vad = NewVAD()
	chunks := make(chan Chunk, 1)
	quit := make(chan struct{})

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		// Four chunks' worth into a buffer of one, with nothing draining it.
		for round := 0; round < 4; round++ {
			recorder.acceptPCM(speech(1600), chunks, quit)
			recorder.acceptPCM(silence(600), chunks, quit)
		}
	}()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("acceptPCM parked on a full chunk channel")
	}

	// The recording itself is untouched by the dropped partials.
	if len(recorder.pcm) == 0 {
		t.Fatal("dropped a live partial and the recording with it")
	}
	if got := <-chunks; len(got.Audio) == 0 {
		t.Fatal("the one chunk that fit should still be a chunk")
	}
}

func TestRecorderStopsSendingOnceStopping(t *testing.T) {
	recorder := newCommandRecorder("unused", nil)
	recorder.vad = NewVAD()
	chunks := make(chan Chunk)
	quit := make(chan struct{})
	close(quit)

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		recorder.acceptPCM(speech(1600), chunks, quit)
		recorder.acceptPCM(silence(600), chunks, quit)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("a stopping recorder still waited for a consumer")
	}
}

// Stop must return whatever the capture backend does. A tool that ignores the
// interrupt — and holding the microphone open in an uninterruptible driver call
// is the ordinary way one does — used to mean Stop waited on `done` forever.
func TestRecorderStopIsBoundedWhenCaptureIgnoresInterrupt(t *testing.T) {
	recorder := newCommandRecorder("/bin/sh", []string{"-c", `trap "" INT; sleep 5`})
	recorder.interruptGrace = 100 * time.Millisecond
	recorder.killGrace = 100 * time.Millisecond
	if err := recorder.Start(); err != nil {
		t.Skipf("no shell to stand in for a capture backend: %v", err)
	}

	stopped := make(chan error, 1)
	go func() {
		_, err := recorder.Stop()
		stopped <- err
	}()
	select {
	case err := <-stopped:
		// No audio was captured, so an error is the honest answer; the point is
		// that there is an answer at all.
		if err == nil {
			t.Fatal("a capture that produced nothing should say so")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop blocked on a child that would not exit")
	}

	// And it stays stopped: a second Stop returns the same settled result
	// rather than waiting again.
	settled := make(chan struct{})
	go func() { defer close(settled); _, _ = recorder.Stop() }()
	select {
	case <-settled:
	case <-time.After(time.Second):
		t.Fatal("a second Stop blocked")
	}
}

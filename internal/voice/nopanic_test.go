package voice

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

func quietVoiceLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return buffer
}

// faultingReader stands in for any bug on the capture path: the pipe read, the
// VAD, the WAV framing. The reader goroutine is the only thing that ever writes
// done or closes chunks.
type faultingReader struct{}

func (faultingReader) Read([]byte) (int, error) { panic("capture read hit a bad frame") }

// The reader goroutine owes the recording two things however it ends: the exit
// status on done, which Stop waits for, and a closed chunks channel, which the
// live-caption consumer ranges over. A fault that skipped either would trade a
// crash for a panel that never comes back — the deadlock this package already
// paid for once.
func TestAFaultingReaderStillEndsTheRecording(t *testing.T) {
	logged := quietVoiceLog(t)
	recorder := newCommandRecorder("unused", nil)
	recorder.vad = NewVAD()
	done := make(chan error, 1)
	chunks := make(chan Chunk, 1)
	quit := make(chan struct{})

	go recorder.read(nil, faultingReader{}, done, chunks, quit)

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "internal fault in voice/recorder read") {
			t.Fatalf("done carried %v, want the fault as the capture's failure", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("done was never written: Stop would block on it forever")
	}

	select {
	case _, open := <-chunks:
		if open {
			t.Fatal("chunks was fed rather than closed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("chunks was never closed: a caption consumer would range over it forever")
	}

	if !strings.Contains(logged.String(), `scope="voice/recorder read"`) {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

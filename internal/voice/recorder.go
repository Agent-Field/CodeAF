package voice

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// SystemRecorder uses the first quiet command-line capture backend installed
// on the machine. The preferred in-process miniaudio binding cannot be
// vendored in network-restricted builds, so this runtime seam keeps capture
// optional and the rest of the application hardware-independent.
func NewSystemRecorder() Recorder {
	if path, err := exec.LookPath("rec"); err == nil {
		return newCommandRecorder(path, []string{"-q", "-t", "raw", "-e", "signed-integer", "-b", "16", "-c", "1", "-r", "16000", "-"})
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return newCommandRecorder(path, []string{"-hide_banner", "-loglevel", "error", "-f", "avfoundation", "-i", ":0", "-ac", "1", "-ar", "16000", "-f", "s16le", "pipe:1"})
	}
	return unavailableRecorder{}
}

type unavailableRecorder struct{}

func (unavailableRecorder) Start() error { return errors.New("no microphone capture backend") }
func (unavailableRecorder) Stop() ([]byte, error) {
	return nil, errors.New("microphone is not recording")
}
func (unavailableRecorder) Level() float64       { return 0 }
func (unavailableRecorder) Chunks() <-chan Chunk { return nil }

type commandRecorder struct {
	path string
	args []string

	mu       sync.RWMutex
	cmd      *exec.Cmd
	pcm      []byte
	level    float64
	chunks   chan Chunk
	done     chan error
	vad      *VAD
	next     int
	overflow bool
	stopOnce sync.Once
	stopped  chan struct{}
	stopWAV  []byte
	stopErr  error
}

func newCommandRecorder(path string, args []string) *commandRecorder {
	return &commandRecorder{path: path, args: append([]string(nil), args...)}
}

func (r *commandRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return errors.New("microphone is already recording")
	}
	command := exec.Command(r.path, r.args...)
	output, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open microphone capture: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return fmt.Errorf("start microphone capture: %w", err)
	}
	r.cmd = command
	r.pcm = nil
	r.level = 0
	r.chunks = make(chan Chunk, 16)
	r.done = make(chan error, 1)
	r.vad = NewVAD()
	r.next = 0
	r.overflow = false
	r.stopOnce = sync.Once{}
	r.stopped = make(chan struct{})
	r.stopWAV = nil
	r.stopErr = nil
	go r.read(command, output, r.done, r.chunks)
	return nil
}

func (r *commandRecorder) read(command *exec.Cmd, output io.Reader, done chan<- error, chunks chan<- Chunk) {
	buffer := make([]byte, frameBytes*5)
	for {
		count, readErr := output.Read(buffer)
		if count > 0 {
			r.acceptPCM(buffer[:count], chunks)
		}
		if readErr != nil {
			waitErr := command.Wait()
			if !errors.Is(readErr, io.EOF) && waitErr == nil {
				waitErr = readErr
			}
			done <- waitErr
			close(chunks)
			return
		}
	}
}

func (r *commandRecorder) acceptPCM(pcm []byte, chunks chan<- Chunk) {
	r.mu.Lock()
	if len(r.pcm)+len(pcm)+44 > MaxAudioBytes {
		r.overflow = true
		r.mu.Unlock()
		return
	}
	r.pcm = append(r.pcm, pcm...)
	r.level = PCM16RMS(pcm)
	cut := r.vad.Push(pcm)
	ready := make([]Chunk, 0, len(cut))
	for _, raw := range cut {
		wav, err := WAV(raw)
		if err != nil {
			continue
		}
		ready = append(ready, Chunk{Index: r.next, Audio: wav})
		r.next++
	}
	r.mu.Unlock()
	for _, chunk := range ready {
		chunks <- chunk
	}
}

func (r *commandRecorder) Stop() ([]byte, error) {
	r.mu.RLock()
	command, done, stopped := r.cmd, r.done, r.stopped
	r.mu.RUnlock()
	if stopped == nil {
		return nil, errors.New("microphone is not recording")
	}
	r.stopOnce.Do(func() { r.finishStop(command, done, stopped) })
	<-stopped
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]byte(nil), r.stopWAV...), r.stopErr
}

func (r *commandRecorder) finishStop(command *exec.Cmd, done <-chan error, stopped chan struct{}) {
	if command != nil && command.Process != nil {
		_ = command.Process.Signal(os.Interrupt)
	}
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(3 * time.Second):
		if command != nil && command.Process != nil {
			_ = command.Process.Kill()
		}
		waitErr = <-done
	}
	r.mu.Lock()
	pcm := append([]byte(nil), r.pcm...)
	overflow := r.overflow
	r.cmd = nil
	r.level = 0
	defer r.mu.Unlock()
	defer close(stopped)
	if overflow {
		r.stopErr = errors.New("recording exceeds OpenRouter's 25MB limit")
		return
	}
	if len(pcm) == 0 {
		if waitErr != nil {
			r.stopErr = fmt.Errorf("microphone capture failed: %w", waitErr)
			return
		}
		r.stopErr = errors.New("microphone captured no audio")
		return
	}
	// An interrupt is the normal way these tools finish. Once audio exists its
	// exit status carries no additional user-facing information.
	r.stopWAV, r.stopErr = WAV(pcm)
}

func (r *commandRecorder) Level() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.level
}

func (r *commandRecorder) Chunks() <-chan Chunk {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.chunks
}

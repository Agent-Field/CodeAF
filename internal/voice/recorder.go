package voice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
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

const (
	// recorderInterruptGrace is how long the capture tool has to finish on the
	// interrupt that is its ordinary ending, and recorderKillGrace how long the
	// kill after it has to land. Neither wait is unbounded, because the caller
	// of Stop is a keystroke away from a user.
	recorderInterruptGrace = 3 * time.Second
	recorderKillGrace      = 2 * time.Second
	// recorderWaitDelay bounds exec's own wait on the child's pipes after the
	// process is gone, so a leaked grandchild holding stdout cannot hold Wait.
	recorderWaitDelay = time.Second
)

// errCaptureStillRunning is what a bounded wait reports: the child has not
// reported its exit status yet. It is only user-facing when no audio was
// captured at all, since a recording that has audio does not need the tool's
// exit status to be a recording.
var errCaptureStillRunning = errors.New("microphone capture did not exit")

// waitFor takes the exit status if it is there within the grace, and says so
// rather than blocking if it is not.
func waitFor(done <-chan error, grace time.Duration) error {
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return errCaptureStillRunning
	}
}

type unavailableRecorder struct{}

func (unavailableRecorder) Start() error { return errors.New("no microphone capture backend") }
func (unavailableRecorder) Stop() ([]byte, error) {
	return nil, errors.New("microphone is not recording")
}
func (unavailableRecorder) Level() float64       { return 0 }
func (unavailableRecorder) Chunks() <-chan Chunk { return nil }

// How a recording ends, and why every step of it is a channel.
//
// The capture tool is a child process writing PCM into a pipe, read by one
// goroutine that also hands voice-activity chunks to whoever is listening. Stop
// has to reach all of that: interrupt the child, let the reader see EOF, collect
// the exit status, and hand back the WAV. Each of those had a way to hang.
//
// The reader parked on an unconditional send the moment the consumer stopped
// draining — a TUI that closed the panel, a partial transcription that stopped
// caring — and a goroutine parked on a channel send cannot be freed by killing
// the process it is reading from. command.Wait was then never reached, the child
// became a zombie, done was never written, and Stop blocked on it forever. So
// the chunk send is now conditional: it takes the quit channel and it takes a
// default, because a live partial is worth dropping and a wedged microphone is
// not. Nothing is lost from the recording itself, which accumulates in pcm.
//
// quit is closed first thing in finishStop, before the interrupt, so a reader
// already parked is freed before anything waits on it. The wait for the exit
// status is bounded twice over — interrupt, then kill, then give up — and the
// context on the command is the backstop for both.
type commandRecorder struct {
	path string
	args []string

	mu       sync.RWMutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	pcm      []byte
	level    float64
	chunks   chan Chunk
	done     chan error
	vad      *VAD
	next     int
	overflow bool
	// stopOnce is a pointer, replaced per recording rather than reset in
	// place. Assigning a fresh sync.Once over the old one wrote the field under
	// the lock while Stop called Do on it without — a race the detector sees
	// and a correctness bug the moment two Stops overlap a Start.
	stopOnce *sync.Once
	stopped  chan struct{}
	quit     chan struct{}
	stopWAV  []byte
	stopErr  error

	// The two graces are fields so a test can assert that Stop is bounded
	// without waiting out the real ones.
	interruptGrace time.Duration
	killGrace      time.Duration
}

func newCommandRecorder(path string, args []string) *commandRecorder {
	return &commandRecorder{
		path: path, args: append([]string(nil), args...),
		interruptGrace: recorderInterruptGrace, killGrace: recorderKillGrace,
	}
}

func (r *commandRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return errors.New("microphone is already recording")
	}
	// CommandContext so that abandoning the recorder abandons the child: the
	// interrupt in finishStop is the polite path, and this is the one that
	// holds when the tool ignores it or the process outlives its pipe.
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, r.path, r.args...)
	command.WaitDelay = recorderWaitDelay
	output, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("open microphone capture: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		cancel()
		return fmt.Errorf("start microphone capture: %w", err)
	}
	r.cmd = command
	r.cancel = cancel
	r.pcm = nil
	r.level = 0
	r.chunks = make(chan Chunk, 16)
	r.done = make(chan error, 1)
	r.vad = NewVAD()
	r.next = 0
	r.overflow = false
	r.stopOnce = &sync.Once{}
	r.stopped = make(chan struct{})
	r.quit = make(chan struct{})
	r.stopWAV = nil
	r.stopErr = nil
	go r.read(command, output, r.done, r.chunks, r.quit)
	return nil
}

func (r *commandRecorder) read(command *exec.Cmd, output io.Reader, done chan<- error, chunks chan<- Chunk, quit <-chan struct{}) {
	// However this goroutine leaves, it owes the recording its ending: Stop
	// waits on done, and a live-caption consumer ranges over chunks until it
	// closes. A fault that skipped either would trade a crash for a wedged
	// panel, so it delivers both — as the failure it is.
	ended := false
	end := func(err error) {
		if ended {
			return
		}
		ended = true
		done <- err
		close(chunks)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			end(guard.Note("voice/recorder read", recovered))
		}
	}()

	buffer := make([]byte, frameBytes*5)
	for {
		count, readErr := output.Read(buffer)
		if count > 0 {
			r.acceptPCM(buffer[:count], chunks, quit)
		}
		if readErr != nil {
			waitErr := command.Wait()
			if !errors.Is(readErr, io.EOF) && waitErr == nil {
				waitErr = readErr
			}
			end(waitErr)
			return
		}
	}
}

func (r *commandRecorder) acceptPCM(pcm []byte, chunks chan<- Chunk, quit <-chan struct{}) {
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
		select {
		case chunks <- chunk:
		case <-quit:
			// Stopping. The recording is already in pcm; the live partials are
			// what is being abandoned, and abandoning them is the point.
			return
		default:
			// Nobody is draining. A dropped partial costs one live caption; a
			// parked reader costs the child process, the exit status, and Stop.
		}
	}
}

func (r *commandRecorder) Stop() ([]byte, error) {
	r.mu.RLock()
	command, done, stopped, quit, cancel, once := r.cmd, r.done, r.stopped, r.quit, r.cancel, r.stopOnce
	r.mu.RUnlock()
	if stopped == nil || once == nil {
		return nil, errors.New("microphone is not recording")
	}
	once.Do(func() { r.finishStop(command, done, stopped, quit, cancel) })
	<-stopped
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]byte(nil), r.stopWAV...), r.stopErr
}

func (r *commandRecorder) finishStop(command *exec.Cmd, done <-chan error, stopped, quit chan struct{}, cancel context.CancelFunc) {
	// First, before anything is asked to wait: free a reader parked on a chunk
	// send, or it will never reach command.Wait and done will never be written.
	if quit != nil {
		close(quit)
	}
	if command != nil && command.Process != nil {
		_ = command.Process.Signal(os.Interrupt)
	}
	waitErr := waitFor(done, r.interruptGrace)
	if errors.Is(waitErr, errCaptureStillRunning) {
		if command != nil && command.Process != nil {
			_ = command.Process.Kill()
		}
		if cancel != nil {
			cancel()
		}
		// Bounded a second time. A kill the child cannot receive — a process
		// stuck in an uninterruptible driver call is the ordinary way a capture
		// backend does this — must not cost the caller its own goroutine.
		waitErr = waitFor(done, r.killGrace)
	}
	if cancel != nil {
		cancel()
	}
	r.mu.Lock()
	pcm := append([]byte(nil), r.pcm...)
	overflow := r.overflow
	r.cmd = nil
	r.cancel = nil
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

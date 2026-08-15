package voice

import (
	"errors"
	"sync"
)

// ScriptedRecorder is a hardware-free recorder for state-machine tests and
// embedders. PushChunk and SetLevel may be called while it is recording.
type ScriptedRecorder struct {
	mu       sync.RWMutex
	StartErr error
	StopErr  error
	Final    []byte
	level    float64
	chunks   chan Chunk
	running  bool
}

func (r *ScriptedRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.StartErr != nil {
		return r.StartErr
	}
	if r.running {
		return errors.New("scripted recorder is already running")
	}
	r.chunks = make(chan Chunk, 16)
	r.running = true
	return nil
}

func (r *ScriptedRecorder) Stop() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		close(r.chunks)
	}
	r.running = false
	r.level = 0
	return append([]byte(nil), r.Final...), r.StopErr
}

func (r *ScriptedRecorder) Level() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.level
}

func (r *ScriptedRecorder) Chunks() <-chan Chunk {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.chunks
}

func (r *ScriptedRecorder) SetLevel(level float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.level = level
}

func (r *ScriptedRecorder) PushChunk(chunk Chunk) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.running {
		select {
		case r.chunks <- chunk:
		default:
		}
	}
}

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var threadID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// thread is one persisted conversation with the app agent.
type thread struct {
	ID       string             `json:"id"`
	Messages []svc.ModelMessage `json:"messages"`
	Spent    float64            `json:"spent"`
}

// threadStore keeps threads in the app folder, one file per thread, and
// serialises turns on the same thread.
type threadStore struct {
	root string
	mu   sync.Mutex
	lock map[string]*sync.Mutex
}

func newThreadStore(root string) *threadStore {
	return &threadStore{root: root, lock: map[string]*sync.Mutex{}}
}

func (s *threadStore) path(app, id string) string {
	return filepath.Join(s.root, app, "threads", id+".json")
}

// turnLock returns the mutex that orders turns of one thread.
func (s *threadStore) turnLock(app, id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := app + "/" + id
	if s.lock[key] == nil {
		s.lock[key] = &sync.Mutex{}
	}
	return s.lock[key]
}

func (s *threadStore) load(app, id string) (*thread, error) {
	if !threadID.MatchString(id) {
		return nil, fmt.Errorf("invalid thread id %q", id)
	}
	data, err := os.ReadFile(s.path(app, id))
	if errors.Is(err, os.ErrNotExist) {
		return &thread{ID: id}, nil
	}
	if err != nil {
		return nil, err
	}
	t := &thread{}
	if err := json.Unmarshal(data, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *threadStore) save(app string, t *thread) error {
	path := s.path(app, t.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

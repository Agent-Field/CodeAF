package main

// chatprefs.go holds the model preferences and media-model snapshot an errand
// (`codeaf do`) reads, and the session id it mints. These helpers are what is
// left of the v1 surface's internal/command after the window it drove was
// removed, kept here because an errand still needs them.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/store"
)

// Prefs persists model choices across launches. It lives beside the graph
// database so the whole resident state moves as one directory.
type Prefs struct {
	ChatModel string `json:"chat_model,omitempty"`
	TaskModel string `json:"task_model,omitempty"`
	// PlanModel empty means the plan slot follows the work model live —
	// the same contract as an empty boost slot.
	PlanModel   string `json:"plan_model,omitempty"`
	BoostModel  string `json:"boost_model,omitempty"`
	VoiceModel  string `json:"voice_model,omitempty"`
	ImageModel  string `json:"image_model,omitempty"`
	SpeechModel string `json:"speech_model,omitempty"`
	MusicModel  string `json:"music_model,omitempty"`
	VideoModel  string `json:"video_model,omitempty"`

	// SplitPct persists the pane share the v1 window wrote. The field stays
	// because a settings row still fronts it.
	SplitPct int `json:"split_pct,omitempty"`
}

// MediaModels is the resolved media configuration seen by new leaves.
// Each leaf takes one value snapshot, preserving the same "next job/leaf"
// boundary used by the hot-swappable work model.
type MediaModels struct {
	// fill supplies the slot defaults that have to be asked of the model
	// catalog. It runs at most once, on the first read, so a cold catalog is
	// paid for by the first leaf that needs a media model rather than by the
	// errand waiting to begin.
	once  sync.Once
	fill  func(*exec.MediaTools)
	mu    sync.RWMutex
	tools exec.MediaTools
}

// NewMediaModels seats the media configuration new leaves are cut from: the
// tools resolved at construction, and the fill that asks the catalog for the
// slot defaults it owns. fill runs at most once, on the first read.
func NewMediaModels(tools exec.MediaTools, fill func(*exec.MediaTools)) *MediaModels {
	return &MediaModels{fill: fill, tools: tools}
}

func (m *MediaModels) Snapshot() exec.MediaTools {
	if m == nil {
		return exec.MediaTools{}
	}
	m.resolve()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tools
}

func (m *MediaModels) resolve() {
	m.once.Do(func() {
		if m.fill == nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.fill(&m.tools)
	})
}

// attachmentStoreRoot is the same cas/ directory the store spills folds into:
// one profile, one blob store.
func attachmentStoreRoot(database string) string {
	return filepath.Join(filepath.Dir(database), "cas")
}

func prefsPath(dir string) string { return filepath.Join(dir, "settings.json") }

func loadChatPrefs(dir string) Prefs {
	var prefs Prefs
	raw, err := os.ReadFile(prefsPath(dir))
	if err != nil {
		return prefs
	}
	_ = json.Unmarshal(raw, &prefs)
	// A ":batch" pick can linger in a settings file written before the
	// catalog stopped offering batch-only endpoints; the base slug is the
	// same model on the interactive endpoint.
	for _, slot := range []*string{&prefs.ChatModel, &prefs.TaskModel,
		&prefs.PlanModel, &prefs.BoostModel} {
		*slot = strings.TrimSuffix(*slot, ":batch")
	}
	return prefs
}

// newSessionID mints a fresh conversation id.
//
// The generator itself lives in the store because a room is a store concept
// and every caller needs the same names. This stays as the errand's name for it
// and forwards.
func newSessionID() string { return store.NewSessionID() }

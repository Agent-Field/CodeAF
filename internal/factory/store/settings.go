package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	DefaultRunnerTimeoutSeconds = factory.DefaultRunnerTimeoutSeconds
	DefaultKeepOutputDays       = factory.DefaultKeepOutputDays
	DefaultKeepOutputMB         = factory.DefaultKeepOutputMB
)

// RunnerSettings is non-secret policy shared by the host and its local workers.
type RunnerSettings struct {
	RunnerTimeoutSeconds int `json:"runner_timeout_s"`
	KeepOutputDays       int `json:"keep_output_days"`
	KeepOutputMB         int `json:"keep_output_mb"`
}

func DefaultRunnerSettings() RunnerSettings {
	return RunnerSettings{DefaultRunnerTimeoutSeconds, DefaultKeepOutputDays, DefaultKeepOutputMB}
}
func (v RunnerSettings) Valid() bool {
	return v.RunnerTimeoutSeconds >= factory.MinRunnerTimeoutSeconds && v.RunnerTimeoutSeconds <= factory.MaxRunnerTimeoutSeconds && v.KeepOutputDays >= factory.MinKeepOutputDays && v.KeepOutputDays <= factory.MaxKeepOutputDays && v.KeepOutputMB >= factory.MinKeepOutputMB && v.KeepOutputMB <= factory.MaxKeepOutputMB
}
func (st *Store) RunnerSettings() (RunnerSettings, error) {
	out := DefaultRunnerSettings()
	raw, err := os.ReadFile(filepath.Join(st.root, "runner-settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil || !out.Valid() {
		return DefaultRunnerSettings(), errors.New("invalid runner settings")
	}
	return out, nil
}

// ChangeRunnerSettings validates and replaces one snapshot under a cross-process lock.
func (st *Store) ChangeRunnerSettings(change func(*RunnerSettings) error) (RunnerSettings, error) {
	var out RunnerSettings
	err := st.underLock(filepath.Join(st.root, "runner-settings.lock"), func() error {
		var err error
		out, err = st.RunnerSettings()
		if err != nil {
			return err
		}
		if err = change(&out); err != nil {
			return err
		}
		if !out.Valid() {
			return fmt.Errorf("runner timeout must be %d–%d seconds, output age %d–%d days and output size %d–%d MB", factory.MinRunnerTimeoutSeconds, factory.MaxRunnerTimeoutSeconds, factory.MinKeepOutputDays, factory.MaxKeepOutputDays, factory.MinKeepOutputMB, factory.MaxKeepOutputMB)
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, "runner-settings.json"), append(raw, '\n'))
	})
	return out, err
}

// Log retention leaves execution results and proof intact. Unknown log times stay unknown.
// Only an explicitly saved policy changes old stores; the next item write collects the trimmed bytes.
func (st *Store) retainOutput(it *factory.Item) {
	if it.Stream == nil {
		return
	}
	days, bytes := 0, 0
	if st.outputPolicy != nil {
		days, bytes = st.outputPolicy()
	} else {
		if _, err := os.Stat(filepath.Join(st.root, "runner-settings.json")); err != nil {
			return
		}
		settings, err := st.RunnerSettings()
		if err != nil {
			return
		}
		days, bytes = settings.KeepOutputDays, settings.KeepOutputMB*(1<<20)
	}
	if days < 1 || bytes < 1 {
		return
	}
	cutoff := st.now().Add(-time.Duration(days) * 24 * time.Hour)
	budget := bytes
	kept := []factory.LogLine{}
	for i := len(it.Stream.Log) - 1; i >= 0; i-- {
		line := it.Stream.Log[i]
		if !line.At.IsZero() && line.At.Before(cutoff) {
			continue
		}
		if len(line.Text) > budget {
			break
		}
		budget -= len(line.Text)
		kept = append(kept, line)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	it.Stream.Log = kept
}

// SetOutputPolicy binds a local runner before it starts; subsequent reads follow live app settings.
func (st *Store) SetOutputPolicy(policy func() (days, bytes int)) { st.outputPolicy = policy }

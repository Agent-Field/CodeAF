package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// MySpaceSettings is the operator's policy for every person's own space
// (serve/myspace.go): what one space may spend in a calendar month. It is
// kept in its own file, apart from the runner settings, so neither write
// can drop the other's field.
type MySpaceSettings struct {
	// LimitUSD is nil until the operator sets one, and then reads as the
	// default (api.MySpaceLimitDefaultUSD).
	LimitUSD *float64 `json:"limit_usd,omitempty"`
}

const mySpaceSettingsFile = "myspace-settings.json"

// MySpaceSettings reads the policy; a store that never kept one answers the
// zero value.
func (st *Store) MySpaceSettings() (MySpaceSettings, error) {
	var out MySpaceSettings
	raw, err := os.ReadFile(filepath.Join(st.root, mySpaceSettingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return MySpaceSettings{}, errors.New("invalid My space settings")
	}
	return out, nil
}

// ChangeMySpaceSettings replaces the policy under a cross-process lock.
func (st *Store) ChangeMySpaceSettings(change func(*MySpaceSettings) error) (MySpaceSettings, error) {
	var out MySpaceSettings
	err := st.underLock(filepath.Join(st.root, "myspace-settings.lock"), func() error {
		var err error
		if out, err = st.MySpaceSettings(); err != nil {
			return err
		}
		if err = change(&out); err != nil {
			return err
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, mySpaceSettingsFile), append(raw, '\n'))
	})
	return out, err
}

package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE ORGANIZATION'S OWN WORDS, AND ITS FIRST SET-UP ─────────────────────
//
// "Start your organization" (v2 designs J2) asks two things: a name, and what
// the organization does in a line, which Afi reads. They are kept here, beside
// the teams, and so is the one set-up Afi proposed for the person who started
// it (J3) and how far the press that applies it got.

const (
	orgProfileFile = "org-profile.json"
	orgSetupFile   = "org-setup.json"
)

// OrgProfile is what an organization says about itself. Name empty is the
// name the server was started with. SetUpAt is when its first set-up was
// made (or skipped with "Set up by hand"); nil is an organization nobody has
// set up yet, whose operator lands on Afi's set-up conversation.
type OrgProfile struct {
	Name        string     `json:"name,omitempty"`
	Description string     `json:"description,omitempty"`
	SetUpAt     *time.Time `json:"set_up_at,omitempty"`
	SetUpBy     string     `json:"set_up_by,omitempty"`
	ByHand      bool       `json:"by_hand,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// OrgSetupMade is what one press made of a card, by the card's own names, so
// a retry does only what is left. Keys are "team:<name>",
// "workstream:<name>", "inside:<parent>/<name>" and "invite:<name>",
// lower-cased; values are the ids the server gave.
type OrgSetupMade map[string]string

// OrgSetupState is each person's pending card and what their press made.
type OrgSetupState struct {
	Cards map[string]factory.OrgSetupCard `json:"cards,omitempty"`
	Made  map[string]OrgSetupMade         `json:"made,omitempty"`
}

// OrgProfile reads the organization's words. None kept is the zero profile.
func (st *Store) OrgProfile() (OrgProfile, error) {
	var p OrgProfile
	err := readJSON(filepath.Join(st.root, orgProfileFile), &p, "the organization's name and line cannot be read")
	return p, err
}

// ChangeOrgProfile rewrites the profile under a lock; an error from change
// writes nothing.
func (st *Store) ChangeOrgProfile(change func(*OrgProfile) error) (OrgProfile, error) {
	var out OrgProfile
	err := st.underLock(filepath.Join(st.root, "org-profile.lock"), func() error {
		p, err := st.OrgProfile()
		if err != nil {
			return err
		}
		if err := change(&p); err != nil {
			return err
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		out = p
		return writeAtomic(filepath.Join(st.root, orgProfileFile), append(raw, '\n'))
	})
	return out, err
}

// OrgSetup reads every person's pending set-up card and what was made.
func (st *Store) OrgSetup() (OrgSetupState, error) {
	var s OrgSetupState
	err := readJSON(filepath.Join(st.root, orgSetupFile), &s, "the organization's set-up cannot be read")
	return s, err
}

// ChangeOrgSetup rewrites the set-up state under a lock; an error from change
// writes nothing.
func (st *Store) ChangeOrgSetup(change func(*OrgSetupState) error) (OrgSetupState, error) {
	var out OrgSetupState
	err := st.underLock(filepath.Join(st.root, "org-setup.lock"), func() error {
		s, err := st.OrgSetup()
		if err != nil {
			return err
		}
		if err := change(&s); err != nil {
			return err
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		out = s
		return writeAtomic(filepath.Join(st.root, orgSetupFile), append(raw, '\n'))
	})
	return out, err
}

// readJSON reads path into out. A missing file leaves out as it is; one that
// cannot be read or parsed is the error words.
func readJSON(path string, out any, words string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New(words)
	}
	return nil
}

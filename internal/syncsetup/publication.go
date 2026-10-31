package syncsetup

import (
	"log"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/home"
)

func (s *Sync) publicationPath(c cell.Cell) string {
	return filepath.Join(s.Home, "v3", "sync", "publication", c.ID)
}

func (s *Sync) publicationScope() string { return s.Ledger + "\n" + s.Device.ID() + "\n" }

// PublicationExpected identifies chats this computer began publishing while
// paired. Upload counters are shared by chats, so they cannot establish this.
// A receipt from another identity, relay or device never grants repair permission.
func (s *Sync) PublicationExpected(c cell.Cell) bool {
	if !s.publicationRecorded(c) {
		return false
	}
	branches, err := cellsync.OpenBranchMap(s.Home)
	return err == nil && branches.Resolve(c.ID) == c.ID
}

func (s *Sync) publicationRecorded(c cell.Cell) bool {
	if !cell.ValidID(c.ID) {
		return false
	}
	raw, err := os.ReadFile(s.publicationPath(c))
	return err == nil && string(raw) == s.publicationScope()
}

// expectPublication decides eligibility once per drive. An ineligible chat never
// reads its growing history again; a failed marker write can still be retried.
func (d *Drive) expectPublication() error {
	d.publicationMu.Lock()
	defer d.publicationMu.Unlock()
	if !d.publicationChecked {
		if d.sync.publicationRecorded(d.cell) {
			d.publicationChecked, d.publicationSaved = true, true
		} else {
			first, err := cellstore.FirstTurn(d.cell)
			if err != nil {
				return err
			}
			d.publicationEligible = first != nil && first.Device == d.sync.Device.Cert.Device
			d.publicationChecked = true
		}
	}
	if d.publicationSaved || !d.publicationEligible {
		return nil
	}
	if err := d.sync.recordPublication(d.cell); err != nil {
		return err
	}
	d.publicationSaved = true
	return nil
}

// recordPublication keeps the paired writer's intent outside sealed chat content.
func (s *Sync) recordPublication(c cell.Cell) error {
	path := s.publicationPath(c)
	if err := home.MkdirAllUnder(s.Home, filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".publication-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(s.publicationScope())
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fsreplace.Rename(f.Name(), path)
}

// logPublicationError uses the chat's file log even at the one-message door,
// where the standard logger still points at the person's terminal.
func (s *Sync) logPublicationError(err error) {
	if home.Removed(s.Home) {
		return
	}
	f, openErr := os.OpenFile(config.ProfilePath(s.profile(), "chat.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if openErr != nil {
		return
	}
	defer f.Close()
	log.New(f, log.Prefix(), log.Flags()).Print("sync: recording chat publication: ", err)
}

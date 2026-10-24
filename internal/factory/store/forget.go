package store

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ForgetPRJob permanently removes an obsolete standalone PR job. The numeric
// mint stays monotonic so a stale client cannot address a new issue by its ID.
func (st *Store) ForgetPRJob(id int) error {
	return st.underLock(st.mintLockPath(), func() error {
		high, err := st.highestID()
		if err != nil {
			return err
		}
		return st.underItemLock(id, func() error {
			doc, err := st.read(st.ItemPath(id))
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if doc.Item.Kind != factory.KindPR {
				return errors.New("only obsolete PR jobs may be forgotten")
			}
			if err := writeAtomic(filepath.Join(st.root, "mint-high-water"), []byte(strconv.Itoa(high)+"\n")); err != nil {
				return err
			}
			return os.Remove(st.ItemPath(id))
		})
	})
}

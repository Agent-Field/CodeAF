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

// ForgetUnstartedCopy removes a provisional copy whose start was refused.
// A job that reached the queue is retained even if recording its start failed.
// The mint stays monotonic, so a refused start's number is never reassigned.
func (st *Store) ForgetUnstartedCopy(id int) error {
	return st.underLock(st.mintLockPath(), func() error {
		high, err := st.highestID()
		if err != nil {
			return err
		}
		return st.underItemLock(id, func() error {
			doc, err := st.read(st.ItemPath(id))
			if err != nil {
				return err
			}
			it := doc.Item
			if it.FromJob <= 0 || it.State != factory.StateNew || it.Stream != nil {
				return errors.New("only an unstarted job copy may be forgotten")
			}
			if err := writeAtomic(filepath.Join(st.root, "mint-high-water"), []byte(strconv.Itoa(high)+"\n")); err != nil {
				return err
			}
			return os.Remove(st.ItemPath(id))
		})
	})
}

package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// spendingFile keeps the factory's spending defaults ([factory.Spending])
// beside its rules. It is not a number, so [Store.List] never reads it as a job.
const spendingFile = "factory-spending.json"

// Spending is what this factory says about budgets and models for every
// repository it watches. A factory that was never given any answers nothing
// set, never an error; a file that cannot be read is an error, so a start never
// quietly runs on figures the person did not choose.
func (st *Store) Spending() (factory.Spending, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, spendingFile))
	if errors.Is(err, os.ErrNotExist) {
		return factory.Spending{}, nil
	}
	if err != nil {
		return factory.Spending{}, err
	}
	var out factory.Spending
	if json.Unmarshal(raw, &out) != nil || out.Check() != nil {
		return factory.Spending{}, errors.New("the factory's spending file cannot be read")
	}
	return out, nil
}

// ChangeSpending replaces the factory's spending defaults under a
// cross-process lock and answers what was saved. change sees the current
// values; a result the factory would not keep is refused whole.
func (st *Store) ChangeSpending(change func(*factory.Spending) error) (factory.Spending, error) {
	var out factory.Spending
	err := st.underLock(filepath.Join(st.root, "factory-spending.lock"), func() error {
		cur, err := st.Spending()
		if err != nil {
			return err
		}
		if err := change(&cur); err != nil {
			return err
		}
		if err := cur.Check(); err != nil {
			return err
		}
		raw, err := json.Marshal(cur)
		if err != nil {
			return err
		}
		out = cur
		return writeAtomic(filepath.Join(st.root, spendingFile), append(raw, '\n'))
	})
	return out, err
}

package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Accepted personal messages own the job even when its manager already existed.
func (b *appBackend) touchJobConversation(file string) error {
	owner, ok := readFactoryOwner(file)
	if !ok {
		return nil
	}
	st, err := store.Open(owner.Root)
	if err != nil {
		return err
	}
	it, err := st.Get(owner.Job)
	if err != nil {
		return err
	}
	if it.PersonTouched {
		return nil
	}
	return st.Update(owner.Job, func(cur *factory.Item) error { factory.TouchIssue(cur); return nil })
}

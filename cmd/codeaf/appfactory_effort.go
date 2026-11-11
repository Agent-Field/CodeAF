package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Factory-owned conversations share the job preference; regular chats remain per-chat.
func (b *appBackend) saveJobEffort(file, effort string) error {
	owner, ok := readFactoryOwner(file)
	if !ok {
		return nil
	}
	st, err := store.Open(owner.Root)
	if err != nil {
		return err
	}
	if it, e := st.Get(owner.Job); e == nil && it.Effort == effort {
		return nil
	}
	return st.Update(owner.Job, func(it *factory.Item) error { it.Effort = effort; return nil })
}

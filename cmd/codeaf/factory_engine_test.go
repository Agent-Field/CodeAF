package main

import (
	"sync"
	"testing"
)

// freshEngineFactoryStore keeps an in-process engine's factory store, opened
// once per process, from outliving the temporary home of the test that opened it.
func freshEngineFactoryStore(t *testing.T) {
	t.Helper()
	reset := func() {
		if factoryPollStop != nil {
			factoryPollStop()
			factoryPollStop = nil
		}
		if factoryTriageStop != nil {
			factoryTriageStop()
			factoryTriageStop = nil
		}
		factoryPoll = sync.Once{}
		factoryTriage = sync.Once{}
		factoryBusyClear = sync.Once{}
		// Store.Open holds no handles; dropping the memo releases the store.
		engineFactoryStore.once = sync.Once{}
		engineFactoryStore.store = nil
	}
	reset()
	t.Cleanup(reset)
}

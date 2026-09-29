package tui3

import "testing"

func TestMemoryCommandsUseTheHostedStoreWhenTheAgentHasNoLocalBrain(t *testing.T) {
	a, _, _ := tabApp(t)
	memory := &struct {
		panelMemoryStore
		*rememberingAgent
	}{rememberingAgent: &rememberingAgent{}}
	a.memory = memory

	brain, ok := a.brain()
	if !ok {
		t.Fatalf("hosted memory was not selected: brain=%T", brain)
	}
	title, err := brain.Remember("the build uses make")
	if err != nil || title == "" {
		t.Fatalf("hosted memory command failed: title=%q err=%v", title, err)
	}
}

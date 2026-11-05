package session

import (
	"strings"
	"testing"
)

func TestTaskFactPromisesOwnFolderWithoutNamingMethod(t *testing.T) {
	page := renderBeltFacts(Config{}, beltFacts, "\n")
	var fact string
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, "- ON `propose_task`") {
			fact = line
			break
		}
	}
	for _, want := range []string{"a folder of its own", "NEVER NAME THE METHOD", `"work in this repo directly", a branch or a checkout is never yours to specify`} {
		if !strings.Contains(fact, want) {
			t.Errorf("task fact missing %q: %s", want, fact)
		}
	}
	if strings.Contains(fact, "copy") {
		t.Errorf("task fact promises a copy: %s", fact)
	}
}

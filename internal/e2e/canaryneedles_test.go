package e2e

// canaryneedles_test.go keeps the bench canary's screen needles inside the same
// vocabulary the tmux suite is held to. The canary (bench/canary/lib/chat.sh)
// waits for words on a live chat screen; when the surface respells one, the
// canary does not fail loudly — every chat cell records noframe and the grid
// reads as a wall of model failures (issue #1034). So each needle the canary
// declares must be a word tuiWords spells, and this test names the canary when
// one is not.

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// canaryNeedleNames are the needles chat.sh must declare. A canary that waits
// for a word the surface stopped drawing has no declaration here to check, so
// a missing name is a failure too.
var canaryNeedleNames = []string{"CHAT_NEEDLE_IDLE", "CHAT_NEEDLE_ACCEPT"}

// canaryNeedleLine matches a declaration such as CHAT_NEEDLE_IDLE='idle'.
var canaryNeedleLine = regexp.MustCompile(`(?m)^(CHAT_NEEDLE_[A-Z_]+)='([^']*)'`)

// spelledInTuiWords reports whether needle is a screen word the table spells,
// either alone, as its key and word, or as the source word it is found by.
func spelledInTuiWords(needle string) bool {
	for _, w := range tuiWords {
		if needle == w.screen || needle == w.source {
			return true
		}
		if w.key != "" && needle == keyedWord(w.key, w.screen) {
			return true
		}
	}
	return false
}

func TestCanaryNeedlesAreWordsTheSuiteKnows(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "bench", "canary", "lib", "chat.sh")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the canary's chat library: %v", err)
	}

	declared := map[string]string{}
	for _, m := range canaryNeedleLine.FindAllStringSubmatch(string(raw), -1) {
		declared[m[1]] = m[2]
	}
	for _, name := range canaryNeedleNames {
		needle, ok := declared[name]
		if !ok {
			t.Errorf("bench/canary/lib/chat.sh declares no %s: the canary waits for a screen word nothing checks, and every chat cell would record noframe", name)
			continue
		}
		if !spelledInTuiWords(needle) {
			t.Errorf("bench/canary/lib/chat.sh %s=%q is not a word tuiwords_test.go spells: the surface respelled it, so update the canary's needle (or the table) before the grid reads empty", name, needle)
		}
	}
}

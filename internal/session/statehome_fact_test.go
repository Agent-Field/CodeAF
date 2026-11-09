package session

import (
	"strings"
	"testing"
	"time"
)

// A WINDOW ON A MOVED HOME SAYS WHICH ONE. On the 2026-11-06 audit the desk's
// assistant, on an app running against another codeaf home, read the manual's
// `~/.codeaf/graph.db` and listed the person's real one. The page now names the
// folder in use whenever CODEAF_HOME moved it, and says nothing otherwise.
func TestThePageNamesAMovedStateHomeAndOnlyThen(t *testing.T) {
	now := time.Now()
	moved := Config{Workspace: "/w", StateHome: "/tmp/other-home"}
	for name, page := range map[string]string{"chat": chatPage(moved, now), "footer": workerFooter(moved, now)} {
		if !strings.Contains(page, "codeaf's own data folder in this window is /tmp/other-home") || !strings.Contains(page, "never read or change ~/.codeaf") {
			t.Errorf("%s page does not name the moved home:\n%s", name, page)
		}
	}
	plain := Config{Workspace: "/w"}
	if strings.Contains(chatPage(plain, now)+workerFooter(plain, now), "own data folder") {
		t.Error("the default home is named on the page")
	}
}

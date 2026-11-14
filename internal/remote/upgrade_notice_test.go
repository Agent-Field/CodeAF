package remote

import (
	"strings"
	"testing"
)

func TestUpgradeNoticeOnlyOnInteractiveWelcome(t *testing.T) {
	for _, headless := range []bool{true, false} {
		t.Run(map[bool]string{true: "headless", false: "interactive"}[headless], func(t *testing.T) {
			engine := engineOn(&fakeAgent{model: "m"})
			engine.Headless = headless
			calls := 0
			engine.EntryNotice = func(file string) string {
				calls++
				if file != engine.SessionFile {
					t.Errorf("notice read wrong transcript %q", file)
				}
				return "Found 1 rule from an earlier version of codeaf. They were not carried over."
			}
			l := dialAgent(t, engine)
			welcome := decode[Welcome](t, l.hello(Hello{Version: Version, Headless: headless}).Payload)
			if headless {
				if calls != 0 || strings.Contains(welcome.Note, "not carried over") {
					t.Fatal("headless consumed notice")
				}
			} else if calls != 1 || !strings.Contains(welcome.Note, "not carried over") {
				t.Fatalf("interactive welcome hid recovery news: %q, calls %d", welcome.Note, calls)
			}
		})
	}
}

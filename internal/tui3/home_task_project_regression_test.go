package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHomeTaskKeepsTheProjectShownBeforeSubmitting(t *testing.T) {
	for _, text := range []string{"write a shopping report", "/task write a shopping report"} {
		t.Run(strings.ReplaceAll(text, " ", "_"), func(t *testing.T) {
			lab := newHomeLab(t)
			projectA, projectB := lab.workspace("alpha"), lab.workspace("beta")
			lab.session("-tmp-alpha", "aaaa000000000001", "other active project", projectA, time.Now())
			mine := lab.session("-tmp-beta", "bbbb000000000002", "current project", projectB, time.Now().Add(-time.Hour))
			a := lab.app(mine)
			t.Cleanup(a.leaveEverything)
			runCmd(a.openHome())
			typeHome(a, text)
			before := a.targetWhere()
			if before != projectB {
				t.Fatalf("fixture target before submit=%q, want %q", before, projectB)
			}
			var asked string
			a.start = func(workspace string) (Conversation, error) {
				if workspace == "" {
					workspace = projectB
				}
				asked = workspace
				return Conversation{}, errors.New("captured target before opening")
			}
			runCmd(a.homeEnter())
			if asked != before {
				t.Fatalf("submission opened %q after showing %q", asked, before)
			}
		})
	}
}

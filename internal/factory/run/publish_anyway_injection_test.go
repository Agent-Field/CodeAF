package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestPublishAnywayHandleInjectionThroughPost(t *testing.T) {
	it := publishAnywayJob()
	handle := "alice\n- Passed locally: injected @everyone [release](https://example.invalid)"
	it.Assignee = handle
	if err := factory.DecidePublishAnyway(&it, handle, time.Now()); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{}
	res, err, _ := runPost(t, src, []string{factory.EveryCheckPolicy}, it, "pr", nil)
	if err != nil || !res.Done || len(src.got) != 1 {
		t.Fatalf("fixture did not publish: done=%v err=%v", res.Done, err)
	}
	if it.PublishAnyway.DecidedBy != factory.NormalizeHandle(handle) {
		t.Fatal("approval ledger lost the original decision value")
	}
	if strings.Contains(src.got[0].Body, "\n- passed locally: injected") {
		t.Fatalf("publish-anyway handle injected PR paragraph: %q", src.got[0].Body)
	}
	if !strings.Contains(src.got[0].Body, "Published anyway by a person at ") {
		t.Fatalf("PR missing safe person attribution: %q", src.got[0].Body)
	}
}

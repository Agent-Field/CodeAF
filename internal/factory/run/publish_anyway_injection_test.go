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
    if err := factory.DecidePublishAnyway(&it, handle, time.Now()); err != nil { return }
    src := &fakeSource{}
    res, err, _ := runPost(t, src, []string{factory.EveryCheckPolicy}, it, "pr", nil)
    if err != nil || !res.Done || len(src.got) != 1 { t.Fatalf("fixture did not publish: done=%v err=%v", res.Done, err) }
    if it.PublishAnyway.DecidedBy != handle { t.Fatal("approval ledger lost the raw handle") }
    if strings.Contains(src.got[0].Body, "\n- passed locally: injected") { t.Fatalf("publish-anyway handle injected PR paragraph: %q", src.got[0].Body) }
}

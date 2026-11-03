package run

import (
    "context"
    "strings"
    "testing"
    "time"
    "github.com/Agent-Field/codeaf/internal/factory"
)

type reviewStopAtRemoteRead struct {
    Git
    stop func()
}
func (g reviewStopAtRemoteRead) Run(dir string, args ...string) (string, error) {
    out, err := g.Git.Run(dir, args...)
    if len(args) == 3 && args[0] == "remote" && args[1] == "get-url" && args[2] == "origin" {
        g.stop()
    }
    return out, err
}

func TestReviewStopDuringPushPreparation(t *testing.T) {
    g := newPublishRig(t, factory.Item{Num:12, Kind:factory.KindIssue, Repo:"acme/api", Title:"Stop queued push"})
    stopped := false
    g.r.opts.Push = GitPush(reviewStopAtRemoteRead{Git:ExecGit{}, stop:func() {
        if err := g.st.Update(g.id, func(it *factory.Item) error {return StopItem(it,time.Now())}); err != nil {t.Error(err)}
        stopped = true
    }})
    if err := g.r.Publish(g.id,testTrailer); err != nil {t.Fatal(err)}
    it := g.settle(t)
    branches := mustGit(t,g.bare,"branch","--list")
    if !stopped {t.Fatal("Stop hook did not run")}
    if branches != "" {t.Fatalf("Stop recorded before git push, but remote branch was created: %s; job=%s",strings.TrimSpace(branches),it.State)}
}

var _ = context.Background

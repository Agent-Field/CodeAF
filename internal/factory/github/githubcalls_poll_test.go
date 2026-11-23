package github

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/githubcalls"
)

type noteSource struct {
	factory.Source
	read chan struct{}
}

func (noteSource) Name() string { return "github" }

func (s noteSource) Read(ctx context.Context, _ string) ([]factory.Item, string, error) {
	githubcalls.NoteGH(ctx, "poll probe")
	close(s.read)
	<-ctx.Done()
	return nil, "", ctx.Err()
}

func TestThePollsCallsAreMarkedAsThePoll(t *testing.T) {
	read := githubLog(t)
	st := openStore(t)
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	src := noteSource{read: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); Poll(ctx, src, st, time.Hour, nil) }()
	select {
	case <-src.read:
	case <-ctx.Done():
		t.Fatal("the poll never read")
	}
	cancel()
	<-done
	if log := read(); !strings.Contains(log, `"caller":"poll"`) {
		t.Fatalf("log: %s", log)
	}
}

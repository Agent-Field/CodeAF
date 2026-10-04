package executor_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/seniordev/util"
	"github.com/Agent-Field/codeaf/internal/verify"
)

// probeSeat hands out unjailed runners of one class and remembers each ask.
type probeSeat struct {
	executor.Stance
	mu   sync.Mutex
	dirs []string
}

func (p *probeSeat) In(dir string) executor.Runner {
	p.mu.Lock()
	p.dirs = append(p.dirs, dir)
	p.mu.Unlock()
	return executor.Local{Root: dir, Class: p.Class}
}

func (p *probeSeat) asked() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.dirs)
}

// Every tool site takes its executor from the seat its context carries, so the
// class a session declared is the class the site runs under.
func TestEverySiteRunsOnTheContextSeat(t *testing.T) {
	sites := map[string]func(context.Context, string){
		"bash": func(ctx context.Context, dir string) {
			_, _, _ = bare.RunBash(ctx, dir, json.RawMessage(`{"command":"true"}`), bare.DefaultCaps(), nil)
		},
		"reading": func(ctx context.Context, dir string) {
			_, _ = verify.RunReading(ctx, dir, verify.Strategy{Command: "true"}, time.Minute)
		},
		"process": func(ctx context.Context, dir string) {
			if child, err := util.SpawnProcess(ctx, []string{"true"}, util.ProcessOptions{Cwd: dir}); err == nil {
				<-child.Exited
			}
		},
	}
	classes := map[string]executor.Class{
		"files-only": executor.FilesOnly, "sandboxed": executor.Sandboxed, "host-bound": executor.HostBound,
	}
	for class, c := range classes {
		for name, site := range sites {
			t.Run(class+"/"+name, func(t *testing.T) {
				seat := &probeSeat{Stance: executor.Stance{Class: c}}
				site(executor.With(context.Background(), seat), t.TempDir())
				if seat.asked() == 0 {
					t.Fatalf("the %s site never asked the session's seat", name)
				}
			})
		}
	}
}

func TestForRefusesAContextThatCarriesNoSession(t *testing.T) {
	runner := executor.For(context.Background()).In(t.TempDir())
	if _, err := runner.Exec(context.Background(), executor.ExecRequest{Argv: []string{"true"}}, nil); !errors.Is(err, executor.ErrNoSeat) {
		t.Fatalf("Exec err = %v, want ErrNoSeat", err)
	}
	if _, err := runner.Command(context.Background(), executor.ExecRequest{Argv: []string{"true"}}); !errors.Is(err, executor.ErrNoSeat) {
		t.Fatalf("Command err = %v, want ErrNoSeat", err)
	}
	seat := &probeSeat{}
	if executor.For(executor.With(context.Background(), seat)) != executor.Seat(seat) {
		t.Fatal("the carried seat was not returned")
	}
}

func TestTwoSeatsOnOnePathKeepTheirOwnClass(t *testing.T) {
	dir := t.TempDir()
	files := executor.Stance{Class: executor.FilesOnly}.In(dir).(executor.Local)
	host := executor.Stance{Class: executor.HostBound}.In(dir).(executor.Local)
	if files.Class != executor.FilesOnly || host.Class != executor.HostBound {
		t.Fatalf("classes crossed: %d and %d", files.Class, host.Class)
	}
	if _, err := files.Exec(context.Background(), executor.ExecRequest{Argv: []string{"true"}}, nil); err == nil {
		t.Fatal("a files-only seat ran a process")
	}
	if _, err := host.Exec(context.Background(), executor.ExecRequest{Argv: []string{"true"}}, nil); err != nil {
		t.Fatalf("the host-bound seat on the same path was refused: %v", err)
	}
}

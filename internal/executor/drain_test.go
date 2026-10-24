package executor

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// C18 preserves dev's helpers without changing the executor's default bound
// or a caller's explicit bound. Late child output must survive an unbounded drain.
func TestUnboundedDrainRetainsChildOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	for _, bound := range []time.Duration{-1, 0, 50 * time.Millisecond} {
		res, err := Host.In(t.TempDir()).Exec(context.Background(), ExecRequest{Argv: []string{"sh", "-c", "printf first; (sleep 0.4; printf second) & exit 0"}, Group: GroupInherit, WaitDelay: bound}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bound < 0 {
			if res.Failure() != nil || string(res.Stdout) != "firstsecond" {
				t.Fatalf("lost helper output: %+v %q", res, res.Stdout)
			}
		} else if !res.PipesForced {
			t.Fatalf("bound %s stopped applying: %+v", bound, res)
		}
	}
}

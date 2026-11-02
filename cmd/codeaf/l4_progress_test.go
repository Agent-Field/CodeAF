package main

import (
	"bytes"
	"testing"
	"time"
)

// L4-M11a: two seconds of silence produce one stderr line, only on a terminal.
func TestL4CommandProgress(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		var out l4ProgressOutput
		out.wrote = make(chan struct{})
		tick := make(chan time.Time, 1)
		armed := make(chan time.Duration, 1)
		stop := syncProgress("http://127.0.0.1:8901", terminal, &out, func(d time.Duration) <-chan time.Time { armed <- d; return tick })
		if terminal {
			if d := <-armed; d != 2*time.Second {
				t.Fatal(d)
			}
			tick <- time.Time{}
		}
		// Joining after the fake clock fires gives a deterministic final transcript.
		if terminal {
			<-out.wrote
		}
		stop()
		if terminal && out.String() != "asking the sync service at 127.0.0.1:8901…\n" {
			t.Fatal(out.String())
		}
		if !terminal && out.Len() != 0 {
			t.Fatal(out.String())
		}
	}
}

type l4ProgressOutput struct {
	bytes.Buffer
	wrote chan struct{}
}

func (o *l4ProgressOutput) Write(p []byte) (int, error) {
	n, err := o.Buffer.Write(p)
	close(o.wrote)
	return n, err
}

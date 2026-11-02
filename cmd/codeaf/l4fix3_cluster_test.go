package main

import (
	"bytes"
	"context"
	"errors"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// L4-H6f, fix round 3: A command's selected service survives errors that carry
// no request URL, independently of the default and the presentation mode.
func TestL4Fix3ClusterKeepsSelectedService(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(syncsetup.URLVar, "https://another.example")
	const selected = "http://selected.example/fabric"
	for _, verb := range []string{"status", "lead"} {
		for _, verbose := range []bool{false, true} {
			for _, cause := range []error{context.DeadlineExceeded, syscall.ECONNREFUSED} {
				var output bytes.Buffer
				err := clusterCommandMode(t.Context(), clusterFixture{err: cause}, verb, &output, verbose, selected)
				want := syncwords.Explain(cause, selected).Error()
				if err == nil || err.Error() != want || !errors.Is(err, cause) || output.Len() != 0 {
					t.Fatalf("%s verbose=%v: error=%v output=%q; want %q", verb, verbose, err, output.String(), want)
				}
			}
		}
	}
}

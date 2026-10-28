package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestClusterRemovedDeviceUsesDevicesSentence(t *testing.T) {
	want := (chatsKind{}).explain(directory.ErrRevoked).Error()
	for _, verb := range []string{"status", "lead"} {
		t.Run(verb, func(t *testing.T) {
			var out bytes.Buffer
			err := clusterCommand(context.Background(), clusterFixture{err: directory.ErrRevoked}, verb, &out)
			if err == nil || exitCodeOf(err) == 0 || !errors.Is(err, directory.ErrRevoked) {
				t.Fatalf("removed command must fail with its cause: %v", err)
			}
			if got := plainWords(err.Error()); got != plainWords(want) || out.Len() != 0 {
				t.Fatalf("command says %q, want %q; stdout=%q", got, plainWords(want), out.String())
			}
		})
	}
}

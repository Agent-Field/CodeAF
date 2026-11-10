package config

import (
	"strings"
	"testing"
)

func TestTaskReviewHintExplainsOnAndOffInPlainWords(t *testing.T) {
	row, ok := registry(t, t.TempDir()).Row(KeyTaskAudit)
	if !ok {
		t.Fatal("task review setting is absent")
	}
	// The no-machinery-vocabulary law applies to the choice editor's hint too.
	for _, banned := range []string{"auditor", "verdict", "verified", "refuted", "unaudited"} {
		if strings.Contains(strings.ToLower(row.Hint), banned) {
			t.Errorf("task review hint exposes %q: %s", banned, row.Hint)
		}
	}
	for _, want := range []string{"on", "off", "checks", "diff", "lands done", "default", "cost"} {
		if !strings.Contains(row.Hint, want) {
			t.Errorf("task review hint omits %q: %s", want, row.Hint)
		}
	}
}

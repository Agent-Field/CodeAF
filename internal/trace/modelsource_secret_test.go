package trace_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

func TestNoServiceKeyReachesTheRecordWhateverItsShape(t *testing.T) {
	dir := t.TempDir()
	key := "zai_plain_secret_for_trace"
	t.Setenv("ZHIPU_API_KEY", key)
	if err := config.WriteSources(dir, []config.PersistedSource{{ID: "z-ai", Written: "z-ai", Region: "intl", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	for _, credential := range config.Credentials(dir) {
		trace.Secret(credential)
	}
	got := string(trace.Scrub([]byte("the service answered with " + key)))
	if strings.Contains(got, key) || !strings.Contains(got, "[redacted]") {
		t.Fatalf("scrubbed record = %q", got)
	}
}

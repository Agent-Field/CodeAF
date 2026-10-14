package agentsession

import (
	"strings"
	"testing"
)

// DISPLAY WORDS STAY WORDS, but a gateway may reuse the cache key as a request
// header. Exercise the actual wire body across turns, labels and sessions.
func TestUnicodeLabelsKeepTheirWordsAndUseStableASCIIWireAffinity(t *testing.T) {
	root := repo(t)
	label := "auth hunter · src/登录.py:12"
	api := (&fakeAPI{}).then(callTool("read", `{"path":"app/views.py"}`)).
		then(answer(`{"file":"app/views.py","line":4}`, 0)).
		then(answer(`{"file":"app/views.py","line":4}`, 0)).
		then(answer(`{"file":"app/views.py","line":4}`, 0))
	var heard []string
	app := mustNew(t, api.serve(t), Config{Root: root, SessionModel: "vendor/work",
		Watch: &Watch{Session: func(label string, _ SessionResult, _ error) { heard = append(heard, label) }}})
	for _, name := range []string{label, label, "auth hunter · src/登入.py:12"} {
		opts := appxOptions("", root)
		opts.Label = name
		var dest map[string]any
		result, err := app.Harness(t.Context(), "read and answer", findingSchema, &dest, opts)
		if err != nil || result.IsError {
			t.Fatalf("session %q: %+v, %v", name, result, err)
		}
	}
	if strings.Join(heard, "|") != label+"|"+label+"|auth hunter · src/登入.py:12" {
		t.Fatalf("display labels changed: %v", heard)
	}
	for _, body := range api.bodies {
		if body.PromptCacheKey == "" || strings.IndexFunc(body.PromptCacheKey, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
			t.Fatalf("unsafe wire affinity %q", body.PromptCacheKey)
		}
		if body.Model != "vendor/work" || len(body.Messages) == 0 || len(body.Tools) == 0 {
			t.Fatalf("model, messages or tools lost: %+v", body)
		}
	}
	if api.bodies[0].PromptCacheKey != api.bodies[1].PromptCacheKey {
		t.Fatal("a tool turn changed the session's affinity")
	}
	if api.bodies[1].PromptCacheKey == api.bodies[2].PromptCacheKey || api.bodies[2].PromptCacheKey == api.bodies[3].PromptCacheKey {
		t.Fatal("distinct sessions or Unicode labels share affinity")
	}
}

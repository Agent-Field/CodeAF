package serve

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

const libraryPath = "/v1/pipeline-library"

func libraryText(text string) *string { return &text }

// /v1/pipeline-library: everyone signed in reads the library, the operator
// replaces it whole and it is kept as written; a member's write, a stale
// version, a key a library does not hold and a pipeline named pr are refused
// with the phase marker and change nothing.
func TestPipelineLibraryRouteReadsReplacesAndRefusesWithoutSaving(t *testing.T) {
	f, g := linkFixture(t)
	_, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	member := signInHandle(t, f.h, g, &f.now, "priya", gate)

	var got api.PipelineLibraryAnswer
	f.request(t, "GET", libraryPath, member, nil, 200, &got)
	if got.Text != "" || got.Version != 1 || got.Can.Change || got.Pipelines == nil || len(got.Pipelines) != 0 {
		t.Fatalf("a new factory's library, read by a member: %+v", got)
	}
	text := "pipeline:\n  docs: [write, proof]\nsteps:\n  write:\n    prompt: write the docs\n"
	f.request(t, "PUT", libraryPath, "operator-secret", api.PipelineLibraryRequest{Text: &text, Expect: &api.PipelineCopyExpect{Version: 1}}, 200, &got)
	want := "pipeline:\n    docs:\n        - write\n        - proof\nsteps:\n    write:\n        prompt: write the docs\n"
	if got.Text != want || got.Version != 2 || got.ChangedBy == "" || !got.Can.Change {
		t.Fatalf("saved %+v", got)
	}
	if len(got.Pipelines) != 1 || got.Pipelines[0].Name != "docs" || got.Pipelines[0].From != "library" || strings.Join(got.Pipelines[0].Steps, ",") != "write,proof" {
		t.Fatalf("pipelines %+v", got.Pipelines)
	}
	for _, bad := range []struct {
		token string
		body  api.PipelineLibraryRequest
		code  int
	}{
		{member, api.PipelineLibraryRequest{Text: libraryText(""), Expect: &api.PipelineCopyExpect{Version: 2}}, 403},
		{"operator-secret", api.PipelineLibraryRequest{Text: libraryText(""), Expect: &api.PipelineCopyExpect{Version: 1}}, 409},
		{"operator-secret", api.PipelineLibraryRequest{Text: libraryText("start:\n  labels: [x]\n"), Expect: &api.PipelineCopyExpect{Version: 2}}, 422},
		{"operator-secret", api.PipelineLibraryRequest{Text: libraryText("pipeline:\n  pr: [read, proof]\n"), Expect: &api.PipelineCopyExpect{Version: 2}}, 422},
		{"operator-secret", api.PipelineLibraryRequest{Expect: &api.PipelineCopyExpect{Version: 2}}, 422},
		{"operator-secret", api.PipelineLibraryRequest{Text: libraryText("")}, 422},
	} {
		w := identityRequest(f.h, "PUT", libraryPath, rulesBody(t, bad.body), bad.token, nil)
		if w.Code != bad.code || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
			t.Fatalf("%+v answered %d with phase %q: %s", bad.body, w.Code, w.Header().Get(api.PhaseHeader), w.Body)
		}
	}
	if kept, _ := f.st.PipelineLibrary(); kept.Text != want || kept.Version != 2 {
		t.Fatalf("a refused write changed the library: %+v", kept)
	}
	// "" empties it.
	f.request(t, "PUT", libraryPath, "operator-secret", api.PipelineLibraryRequest{Text: libraryText(""), Expect: &api.PipelineCopyExpect{Version: 2}}, 200, &got)
	if got.Text != "" || got.Version != 3 || len(got.Pipelines) != 0 {
		t.Fatalf("emptied %+v", got)
	}
	f.request(t, "GET", libraryPath, "", nil, 401, nil)
}

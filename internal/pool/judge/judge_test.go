package judge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
)

// fakeAsk records every prompt it is handed and answers with the replies it was
// given, in order, or with an error when one was set.
type fakeAsk struct {
	systems []string
	users   []string
	replies []string
	err     error
}

func (f *fakeAsk) ask(_ context.Context, system, user string) (string, error) {
	f.systems = append(f.systems, system)
	f.users = append(f.users, user)
	if f.err != nil {
		return "", f.err
	}
	if len(f.replies) == 0 {
		return `{"score": 1, "reason": "no reply was set"}`, nil
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	return reply, nil
}

func TestAScoreComesBackForEverySeatInRoleOrder(t *testing.T) {
	rec := Record{
		Brief: "do the thing",
		Seats: map[Role]string{RoleMastermind: "v/mind", RoleHigh: "v/high", RoleWorker: "v/worker"},
	}
	ask := &fakeAsk{replies: []string{
		`{"score": 80, "reason": "the worker did what the brief asked"}`,
		`{"score": 70, "reason": "the check read the work"}`,
		`{"score": 60, "reason": "the plan held the brief"}`,
	}}
	scores, err := Judge(context.Background(), ask.ask, rec)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	want := []struct {
		role  Role
		model string
		score float64
	}{
		{RoleWorker, "v/worker", 80},
		{RoleHigh, "v/high", 70},
		{RoleMastermind, "v/mind", 60},
	}
	if len(scores) != len(want) {
		t.Fatalf("got %d scores, want %d", len(scores), len(want))
	}
	for i, w := range want {
		if scores[i].Role != w.role || scores[i].Model != w.model || scores[i].Score != w.score {
			t.Errorf("score %d is %+v, want %s %s %v", i, scores[i], w.role, w.model, w.score)
		}
		if scores[i].Reason == "" {
			t.Errorf("score %d carries no reason", i)
		}
	}
}

func TestASeatTheJudgeHoldsIsSkippedAndNamed(t *testing.T) {
	rec := Record{
		Seats: map[Role]string{RoleWorker: "v/worker", RoleHigh: "v/judge", RoleMastermind: "v/mind"},
		Judge: "v/judge",
	}
	ask := &fakeAsk{replies: []string{
		`{"score": 80, "reason": "ok"}`,
		`{"score": 60, "reason": "ok"}`,
	}}
	scores, err := Judge(context.Background(), ask.ask, rec)
	if err == nil {
		t.Fatal("expected an error naming the skipped seat")
	}
	if !strings.Contains(err.Error(), "high") {
		t.Errorf("the error %q does not name the high seat", err)
	}
	if len(scores) != 2 {
		t.Fatalf("got %d scores, want 2", len(scores))
	}
	for _, score := range scores {
		if score.Role == RoleHigh {
			t.Error("the high seat was scored though the judge holds it")
		}
	}
	if len(ask.systems) != 2 {
		t.Errorf("the judge was asked %d questions, want 2", len(ask.systems))
	}
}

func TestAMalformedAnswerFailsOnlyItsSeat(t *testing.T) {
	rec := Record{Seats: map[Role]string{RoleWorker: "v/worker", RoleHigh: "v/high"}}
	ask := &fakeAsk{replies: []string{"I would rather not say.", `{"score": 70, "reason": "fine"}`}}
	scores, err := Judge(context.Background(), ask.ask, rec)
	if err == nil {
		t.Fatal("expected an error naming the worker seat")
	}
	if !strings.Contains(err.Error(), "worker") {
		t.Errorf("the error %q does not name the worker seat", err)
	}
	if len(scores) != 1 || scores[0].Role != RoleHigh {
		t.Fatalf("got %+v, want only the high seat scored", scores)
	}
}

func TestAScoreOutsideTheScaleFailsItsSeat(t *testing.T) {
	rec := Record{Seats: map[Role]string{RoleWorker: "v/worker", RoleHigh: "v/high"}}
	ask := &fakeAsk{replies: []string{`{"score": 140, "reason": "off the scale"}`, `{"score": 70, "reason": "fine"}`}}
	scores, err := Judge(context.Background(), ask.ask, rec)
	if err == nil {
		t.Fatal("expected an error for the out-of-range score")
	}
	if len(scores) != 1 || scores[0].Role != RoleHigh {
		t.Fatalf("got %+v, want only the high seat scored", scores)
	}
}

func TestPromptsStayInsideTheClipBudget(t *testing.T) {
	full := strings.Repeat("b", recordClip)
	rec := Record{
		Brief:       full + "BRIEF-TAIL",
		Deliverable: full + "DELIVERABLE-TAIL",
		Report:      strings.Repeat("r", recordClip+500) + "REPORT-TAIL",
		Seats:       map[Role]string{RoleWorker: "v/worker"},
	}
	ask := &fakeAsk{replies: []string{`{"score": 50, "reason": "ok"}`}}
	if _, err := Judge(context.Background(), ask.ask, rec); err != nil {
		t.Fatalf("judge: %v", err)
	}
	user := ask.users[0]
	if strings.Contains(user, "BRIEF-TAIL") {
		t.Error("the brief was carried past the clip budget")
	}
	if strings.Contains(user, "DELIVERABLE-TAIL") {
		t.Error("the deliverable was carried past the clip budget")
	}
	if !strings.Contains(user, "REPORT-TAIL") {
		t.Error("the report was clipped though it travels whole")
	}
}

func TestARecordWithNoSeatsScoresNothingAndFailsNothing(t *testing.T) {
	ask := &fakeAsk{err: errors.New("the judge should not have been asked")}
	scores, err := Judge(context.Background(), ask.ask, Record{Brief: "nothing ran"})
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if len(scores) != 0 {
		t.Errorf("got %d scores, want none", len(scores))
	}
	if len(ask.systems) != 0 {
		t.Errorf("the judge was asked %d questions with no seats to read", len(ask.systems))
	}
}

func TestPickExcludesTheCrewsOwnModelsAndTheWorkerVendor(t *testing.T) {
	models := []catalog.Model{
		{ID: "worker-vendor/cheap", PromptPrice: 0.1, CompletionPrice: 0.1, CodingIndex: 90, Parameters: []string{"tools"}},
		{ID: "other/crew-held", PromptPrice: 0.2, CompletionPrice: 0.2, CodingIndex: 90, Parameters: []string{"tools"}},
		{ID: "judge-vendor/fine", PromptPrice: 1, CompletionPrice: 1, CodingIndex: 80, Parameters: []string{"tools"}},
	}
	crew := map[Role]string{RoleWorker: "worker-vendor/w", RoleHigh: "other/crew-held"}
	got, ok := Pick(models, crew, DefaultFloor)
	if !ok {
		t.Fatal("Pick found no judge though one qualifies")
	}
	if got != "judge-vendor/fine" {
		t.Errorf("the judge is %q, want judge-vendor/fine", got)
	}
}

// TestCandidatesExcludesTheWorkerVendorWhenTheSeatCarriesTheAliasMarker pins
// the exclusion through the spelling a client routes by: a seat written
// `~vendor/worker` holds the same vendor `vendor`, and a row of that vendor is
// no candidate whatever marker the seat's spelling carries.
func TestCandidatesExcludesTheWorkerVendorWhenTheSeatCarriesTheAliasMarker(t *testing.T) {
	models := []catalog.Model{
		{ID: "vendor/cheap", PromptPrice: 0.1, CompletionPrice: 0.1, CodingIndex: 90, Parameters: []string{"tools"}},
		{ID: "other/judge", PromptPrice: 0.5, CompletionPrice: 0.5, CodingIndex: 80, Parameters: []string{"tools"}},
	}
	crew := map[Role]string{RoleWorker: "~vendor/worker"}
	got := Candidates(models, crew, DefaultFloor)
	if len(got) != 1 || got[0] != "other/judge" {
		t.Fatalf("Candidates named %v, want only other/judge — a tilde-spelled seat is still vendor's own", got)
	}
}

func TestPickHonoursTheFloorAndSkipsUnpricedRows(t *testing.T) {
	models := []catalog.Model{
		{ID: "a/below-floor", PromptPrice: 0.01, CompletionPrice: 0.01, CodingIndex: 40, Parameters: []string{"tools"}},
		{ID: "b/unpriced", PromptPrice: 0, CompletionPrice: 0, PriceUnknown: true, CodingIndex: 99, Parameters: []string{"tools"}},
		{ID: "c/no-tools", PromptPrice: 0.02, CompletionPrice: 0.02, CodingIndex: 90},
		{ID: "d/qualifies", PromptPrice: 0.5, CompletionPrice: 0.5, CodingIndex: 80, Parameters: []string{"tools"}},
	}
	got, ok := Pick(models, nil, DefaultFloor)
	if !ok || got != "d/qualifies" {
		t.Errorf("the judge is %q, ok %v, want d/qualifies", got, ok)
	}
	if _, ok := Pick(nil, nil, DefaultFloor); ok {
		t.Error("Pick found a judge in an empty catalog")
	}
}

func TestPickIsDeterministicUnderShuffledInput(t *testing.T) {
	first := catalog.Model{ID: "a/one", PromptPrice: 1, CompletionPrice: 1, CodingIndex: 80, Parameters: []string{"tools"}}
	second := catalog.Model{ID: "b/two", PromptPrice: 1, CompletionPrice: 1, CodingIndex: 80, Parameters: []string{"tools"}}
	forward, _ := Pick([]catalog.Model{first, second}, nil, DefaultFloor)
	backward, _ := Pick([]catalog.Model{second, first}, nil, DefaultFloor)
	if forward != backward {
		t.Errorf("Pick depends on order: %q then %q", forward, backward)
	}
	if forward != "a/one" {
		t.Errorf("a tie picked %q, want the lower id a/one", forward)
	}
}

func TestCandidatesNeverNamesAFreeRowWhileAPaidRowQualifies(t *testing.T) {
	models := []catalog.Model{
		{ID: "free/vendor:free", PromptPrice: 0, CompletionPrice: 0, CodingIndex: 90, Parameters: []string{"tools"}},
		{ID: "zero-priced/vendor", PromptPrice: 0, CompletionPrice: 0, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "paid/vendor", PromptPrice: 0.5, CompletionPrice: 0.5, CodingIndex: 65, Parameters: []string{"tools"}},
	}
	got := Candidates(models, nil, DefaultFloor)
	if len(got) != 1 || got[0] != "paid/vendor" {
		t.Fatalf("Candidates named %v, want only the paid row", got)
	}
}

func TestCandidatesIsOrderedByCostThenIdAndExcludesWhatPickExcludes(t *testing.T) {
	models := []catalog.Model{
		{ID: "b/dear", PromptPrice: 1, CompletionPrice: 1, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "crew-held/cheap", PromptPrice: 0.1, CompletionPrice: 0.1, CodingIndex: 99, Parameters: []string{"tools"}},
		{ID: "worker-vendor/cheap", PromptPrice: 0.1, CompletionPrice: 0.1, CodingIndex: 90, Parameters: []string{"tools"}},
		{ID: "a/cheap", PromptPrice: 0.5, CompletionPrice: 0.5, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "c/tie", PromptPrice: 0.5, CompletionPrice: 0.5, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "d/below-floor", PromptPrice: 0.01, CompletionPrice: 0.01, CodingIndex: 40, Parameters: []string{"tools"}},
	}
	crew := map[Role]string{RoleWorker: "worker-vendor/w", RoleHigh: "crew-held/cheap"}
	got := Candidates(models, crew, DefaultFloor)
	want := []string{"a/cheap", "c/tie", "b/dear"}
	if len(got) != len(want) {
		t.Fatalf("Candidates named %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Candidates named %v, want %v", got, want)
		}
	}
}

// roundTripFunc is the fake transport the snapshot catalog is read through: it
// answers every request with the captured bytes, the way internal/catalog's own
// captured-response tests do.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

// snapshotCatalog loads the captured OpenRouter listing internal/catalog's own
// tests read, through a transport that answers with the bytes on disk.
func snapshotCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "catalog", "testdata", "openrouter-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Request:    request,
		}, nil
	})}
	return catalog.Load(context.Background(), catalog.Options{
		BaseURL:    catalog.DefaultBaseURL,
		Dir:        t.TempDir(),
		HTTPClient: client,
	})
}

func TestCandidatesFromTheCapturedListingAreAllPaidAndNoneFree(t *testing.T) {
	c := snapshotCatalog(t)
	models := c.ModelsNow()
	if len(models) == 0 {
		t.Fatal("the captured listing read back no rows")
	}
	crew := map[Role]string{RoleWorker: "z-ai/glm-5.3-flash", RoleHigh: "anthropic/claude-fable-5.1"}
	got := Candidates(models, crew, DefaultFloor)
	if len(got) == 0 {
		t.Fatal("the captured listing yielded no judge candidates")
	}
	byID := make(map[string]catalog.Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	for _, id := range got {
		if strings.HasSuffix(strings.ToLower(id), ":free") {
			t.Errorf("the candidate %q is a free row", id)
		}
		model, ok := byID[id]
		if !ok {
			t.Fatalf("the candidate %q is not a row of the listing", id)
		}
		if model.PromptPrice+model.CompletionPrice <= 0 {
			t.Errorf("the candidate %q carries no price", id)
		}
	}
}

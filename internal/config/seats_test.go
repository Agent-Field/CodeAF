package config

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE LADDER, PINNED RUNG BY RUNG. The bug it exists to prevent is not a wrong
// model — it is a rung that answers when a rung above it should have, which is
// invisible in a run's output and shows up as a benchmark measuring a crew it
// was not using (#166). So every rung is asserted for both seats, including the
// order between them.
func TestTheSeatLadderAnswersInItsOwnOrder(t *testing.T) {
	for _, test := range []struct {
		name string
		// crew is written with the same writer /crew uses, or left empty for a
		// profile nobody has touched.
		crew           string
		hand           map[string]string
		env, planEnv   string
		flag, planFlag string

		work, workRung string
		plan, planRung string
	}{
		{
			name: "a profile nobody has touched answers with the build's default",
			work: DefaultModel, workRung: "default",
			plan: "", planRung: "default",
		},
		{
			name: "the crew answers when it is the only thing said",
			crew: CrewFrugal,
			work: "deepseek/deepseek-v4-flash-0731", workRung: "crew frugal",
			plan: "z-ai/glm-5.3-flash:high", planRung: "crew frugal",
		},
		{
			name:    "the environment outranks the crew",
			crew:    CrewFrugal,
			env:     "vendor/from-the-environment",
			planEnv: "vendor/plans-from-the-environment",
			work:    "vendor/from-the-environment", workRung: ModelEnv,
			plan: "vendor/plans-from-the-environment", planRung: PlanModelEnv,
		},
		{
			name:     "the flag outranks the environment",
			crew:     CrewFrugal,
			env:      "vendor/from-the-environment",
			planEnv:  "vendor/plans-from-the-environment",
			flag:     "vendor/from-the-flag",
			planFlag: "vendor/plans-from-the-flag",
			work:     "vendor/from-the-flag", workRung: "--model",
			plan: "vendor/plans-from-the-flag", planRung: "--plan-model",
		},
		{
			// The two seats climb independently: one flag does not settle both.
			name:    "one seat may be flagged while the other still climbs",
			crew:    CrewFrugal,
			planEnv: "vendor/plans-from-the-environment",
			flag:    "vendor/from-the-flag",
			work:    "vendor/from-the-flag", workRung: "--model",
			plan: "vendor/plans-from-the-environment", planRung: PlanModelEnv,
		},
		{
			// The mastermind row is the one the presets differ in, and it is
			// the only one carrying a thinking level. THE LEVEL STAYS ON, the
			// way it stays on a flag: it is applied per call by the role ladder,
			// and the client seam takes it off the slug it sends
			// (Config.providerConfig).
			name: "the crew's thinking level travels with the value",
			crew: CrewMax,
			work: "z-ai/glm-5.3", workRung: "crew max",
			plan: "moonshotai/kimi-k3:high", planRung: "crew max",
		},
		{
			// And a flag carrying one is not shortened either, so the two rungs
			// hand the same kind of value to everything downstream.
			name:     "a flagged level travels the same way",
			planFlag: "moonshotai/kimi-k3:low",
			work:     DefaultModel, workRung: "default",
			plan: "moonshotai/kimi-k3:low", planRung: "--plan-model",
		},
		{
			// One row answered by hand is a custom crew, and the receipt says
			// so rather than naming a preset the four rows do not make.
			name: "a hand-written row reads as the custom crew it makes",
			crew: CrewFrugal,
			// THE WORK SEAT IS THE WORKER ROW, and not the small-work row beside
			// it: the same row a task handed off in conversation rides.
			hand: map[string]string{ModelTierWorker: "vendor/my-own-worker"},
			work: "vendor/my-own-worker", workRung: "crew custom",
			plan: "z-ai/glm-5.3-flash:high", planRung: "crew custom",
		},
		{
			// A row cleared on purpose means "follow the conversation", and a
			// headless run has no conversation to follow.
			name: "a cleared row falls through to the default",
			crew: CrewFrugal,
			hand: map[string]string{ModelTierWorker: "", ModelTierMastermind: ""},
			work: DefaultModel, workRung: "default",
			plan: "", planRung: "default",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.crew != "" {
				if err := ApplyCrew(dir, test.crew); err != nil {
					t.Fatalf("writing the %s crew: %v", test.crew, err)
				}
			}
			for tier, model := range test.hand {
				if err := writeTierModel(dir, tier, model); err != nil {
					t.Fatalf("writing the %s row: %v", tier, err)
				}
			}
			// Set at call time, both of them, so a case that names neither is
			// running against a shell that has.
			t.Setenv(ModelEnv, test.env)
			t.Setenv(PlanModelEnv, test.planEnv)

			seats := ResolveSeats(dir, test.flag, test.planFlag)
			if seats.Work.Model != test.work || seats.Work.Rung() != test.workRung {
				t.Errorf("work seat = %q (%s), want %q (%s)",
					seats.Work.Model, seats.Work.Rung(), test.work, test.workRung)
			}
			if seats.Plan.Model != test.plan || seats.Plan.Rung() != test.planRung {
				t.Errorf("plan seat = %q (%s), want %q (%s)",
					seats.Plan.Model, seats.Plan.Rung(), test.plan, test.planRung)
			}
		})
	}
}

// AN UNTOUCHED PROFILE IS NOT THE CREW ANSWERING, and this is the decision
// written down where it can be argued with.
//
// The four shipped tier defaults ARE the balanced row (crew.go), so [CrewAt]
// says `balanced` about a profile nobody has ever opened. Reading the seats
// through [TierModelAt] would therefore let the crew rung answer for a person
// who has never said anything about models — the bottom rung would become
// unreachable, and this build's headless default model would silently change
// from [DefaultModel] to the low tier's. So the ladder asks whether the key was
// WRITTEN, and `crew` on a receipt means somebody chose a crew.
func TestAnUntouchedProfileReadsTheDefaultAndNotTheBalancedCrew(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "")

	if got := CrewAt(dir); got != DefaultCrew {
		t.Fatalf("an untouched profile reads crew %q, so this test is about the wrong thing", got)
	}
	seats := ResolveSeats(dir, "", "")
	if seats.Work.Source != SeatDefault || seats.Work.Model != DefaultModel {
		t.Fatalf("work seat = %q (%s), want the build default", seats.Work.Model, seats.Work.Rung())
	}
	if seats.Plan.Source != SeatDefault || seats.Plan.Model != "" {
		t.Fatalf("plan seat = %q (%s), want an empty seat that follows the work model",
			seats.Plan.Model, seats.Plan.Rung())
	}

	// And the same profile once somebody has actually said `balanced`: the same
	// four models, and now the crew is what answered.
	if err := ApplyCrew(dir, CrewBalanced); err != nil {
		t.Fatal(err)
	}
	seats = ResolveSeats(dir, "", "")
	if seats.Work.Source != SeatCrew || seats.Work.Rung() != "crew balanced" {
		t.Fatalf("after /crew balanced the work seat reads %s", seats.Work.Rung())
	}
}

// The receipt is the whole point of resolving the rung, so its sentence is
// pinned: both seats, each with what chose it, on one line.
func TestTheReceiptNamesBothSeatsAndTheirRungs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ModelEnv, "")
	t.Setenv(PlanModelEnv, "vendor/plans")
	if err := ApplyCrew(dir, CrewFrugal); err != nil {
		t.Fatal(err)
	}
	line := ResolveSeats(dir, "", "").Line()
	frugal, _ := CrewModels(CrewFrugal)
	want := "models: work " + frugal[ModelTierWorker] + " (crew frugal) · plan vendor/plans (" + PlanModelEnv + ")"
	if line != want {
		t.Fatalf("the opening line reads\n\t%s\nwant\n\t%s", line, want)
	}

	// An unfilled plan seat is said in words rather than left as a gap.
	t.Setenv(PlanModelEnv, "")
	empty := ResolveSeats(t.TempDir(), "", "")
	if !strings.Contains(empty.Line(), "plan follows the work model (default)") {
		t.Fatalf("an empty plan seat reads %q", empty.Line())
	}
}

// THE LEVEL COMES OFF AT THE WIRE AND NOWHERE EARLIER.
//
// `moonshotai/kimi-k3:low` is how a tier row, a flag and a variable all say
// "that model, thinking a little". The level is applied per call by the role
// ladder, so the value travels whole; what a provider is asked for is the model
// alone, because the slug with a level on it is one no provider publishes. Sent
// whole it is a 404 on every call the seat makes — which is what the flag path
// did before this seam knew the difference.
func TestTheThinkingLevelNeverReachesTheProviderAsPartOfTheSlug(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		asked = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	settings := Config{APIKey: "k", BaseURL: server.URL, Timeout: DefaultTimeout, MaxTokens: 100}
	ask := func(client router.Client, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		asked = ""
		if _, err := client.CompleteWithMessages(context.Background(),
			[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}}); err != nil {
			t.Fatal(err)
		}
		return asked
	}

	// The plan seat, however it was filled.
	if got := ask(settings.ClientFor("moonshotai/kimi-k3:low")); got != "moonshotai/kimi-k3" {
		t.Fatalf("the plan client asked for %q, want the model id alone", got)
	}
	// And the work seat, which reaches the same seam through Client().
	settings.Model = "moonshotai/kimi-k3:high"
	if got := ask(settings.Client()); got != "moonshotai/kimi-k3" {
		t.Fatalf("the work client asked for %q, want the model id alone", got)
	}
	// A suffix that is not a level is part of the id and is left alone.
	if got := ask(settings.ClientFor("vendor/model:free")); got != "vendor/model:free" {
		t.Fatalf("a non-level suffix was eaten: %q", got)
	}
	// The settings keep what the person wrote, so the role ladder still sees
	// the level it has to apply.
	if settings.Model != "moonshotai/kimi-k3:high" {
		t.Fatalf("building a client rewrote the seat to %q", settings.Model)
	}
}

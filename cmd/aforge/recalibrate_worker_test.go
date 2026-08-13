package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// rulerCaptureClient answers a recalibration with a ruler long enough to be
// accepted and remembers what it was asked.
type rulerCaptureClient struct {
	messages []ai.Message
	anchors  string
}

func (c *rulerCaptureClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.messages = append(c.messages, messages...)
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
		Content: []ai.ContentPart{{Type: "text", Text: `{"anchors":` + quoteJSON(c.anchors) + `}`}},
	}}}}, nil
}

func quoteJSON(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func (c *rulerCaptureClient) asked() string {
	var whole strings.Builder
	for _, message := range c.messages {
		for _, part := range message.Content {
			whole.WriteString(part.Text)
			whole.WriteString("\n")
		}
	}
	return whole.String()
}

// Signal three, whole: when a specialist's own profile says its ruler is wrong,
// the rewrite runs against that worker's own anchors, is told what that worker
// is for, is shown what that worker said about its own fit — and the answer
// lands in the file AND in the ruler in force, without a restart.
func TestASpecialistsRulerIsRewrittenFromItsOwnEvidence(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{
		Name:         "swe",
		Purpose:      "an end-to-end software-engineering pipeline for changing code",
		PriorAnchors: "the swe worker's three worked examples, as registered",
	})
	settings := config.Config{ProfileDir: t.TempDir(), Model: "worker/model"}
	installMeasuredRulers(settings, settings.Model)
	if got := plan.AnchorsFor("swe"); got != "the swe worker's three worked examples, as registered" {
		t.Fatalf("the specialist did not start on its own prior: %q", got)
	}

	// Enough measured leaves, a third of them out of budget: the profile's own
	// reading of "the ruler is too generous". Not more than nine in ten, which
	// the profile refuses to read as evidence about size at all — that pattern
	// points at the budget rather than at the ruler.
	var landed []landedProfileRecord
	for index := 0; index < profile.MinSamples; index++ {
		record := profile.Record{
			Title: "one coding issue", Summary: "take it whole", Size: "atomic",
			Turns: 20 + index, Tokens: 400_000, Cost: 6.5, SourcesKnown: true,
			Verdict: provider.VerdictVerifiedSuccess,
		}
		if index < 3 {
			record.Verdict = provider.VerdictBudgetStop
		}
		if index == 0 {
			record.EscalatedFrom = exec.LinearSubharness
			record.Calibration = []string{"it spent its whole cost ceiling and was still working"}
		}
		landed = append(landed, landedProfileRecord{planID: index + 1, record: record})
	}

	rewritten := strings.TrimSpace(strings.Repeat("TOO SMALL — one obvious edit whose location is known. ", 3) +
		strings.Repeat("RIGHT — one coding issue taken whole. ", 3) +
		strings.Repeat("TOO BIG — more than one product-scale goal. ", 3))
	client := &rulerCaptureClient{anchors: rewritten}

	report, pending := recordAndCalibrateWorker(context.Background(), client, settings,
		settings.Model, "swe", landed)
	if len(pending) != profile.MinSamples {
		t.Fatalf("the records did not land: %d", len(pending))
	}
	if !strings.Contains(report, "ruler recalibrated") {
		t.Fatalf("the specialist's ruler was not rewritten: %q", report)
	}

	asked := client.asked()
	for _, want := range []string{
		// (a) its own anchors as the starting text
		"the swe worker's three worked examples, as registered",
		// (b) its purpose, so the rewriter knows which worker it is describing
		"an end-to-end software-engineering pipeline for changing code",
		// (c) the calibration notes
		"it spent its whole cost ceiling and was still working",
		"the linear worker tried this first and could not finish it",
	} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the recalibration call never saw %q:\n%s", want, asked)
		}
	}

	// (d) durable in the file, and hot-swapped in the ruler in force.
	if got := plan.AnchorsFor("swe"); got != rewritten {
		t.Fatalf("the ruler in force was not swapped mid-session: %q", got)
	}
	if got := plan.AnchorsFor(exec.LinearSubharness); got == rewritten {
		t.Fatal("the specialist's rewrite replaced the generalist's ruler")
	}
	reloaded, err := profile.Load(settings.ProfileDir, settings.Model, "swe")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Anchors != rewritten {
		t.Fatalf("the rewritten ruler did not survive the file: %q", reloaded.Anchors)
	}
	// And it survives a restart: the next process seats it from the file.
	plan.UseAnchorsFor("swe", "")
	installMeasuredRulers(settings, settings.Model)
	if got := plan.AnchorsFor("swe"); got != rewritten {
		t.Fatalf("a restart went back to the prior: %q", got)
	}
}

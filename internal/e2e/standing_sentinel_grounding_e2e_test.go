//go:build e2e

package e2e

// standing_sentinel_grounding_e2e_test.go is the LIVE regression for the
// readiness false positive the independent review recorded on d179d5aa1: a
// status-only probe against a readiness ask fired "yes" 10/10 times, including
// with no hint.
//
// IT DRIVES THE PRODUCTION SEAM, NOT A RECONSTRUCTION. [session.NewStandingSentinelVerdict]
// composes the real sentinel prompt and the real question and sends them to the
// real provider; nothing here copies a prompt, fakes a verdict, or reads a
// canned answer. The model is the exact slug the review used,
// deepseek/deepseek-v4.1-flash, pinned through the item's own OneModel origin so
// there is no role pin, no tier and no fallback ladder to rescue a bad slug.
//
// IT IS OPT-IN BY THE e2e TAG AND THE USUAL KEY GATE. No key on the machine is a
// DOCUMENTED SKIP (the skip line names every road the product reads) -- a skip
// is never acceptance. Root reruns this after the safe matrix; the recorded
// failing shape is the one case that must have moved.
//
// A DOUBLE CANNOT STAND IN FOR THIS. The in-package witnesses read strings out
// of the composed prompt; only a call through the provider shows what the model
// does with it, which is exactly the gap the review found.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// groundingSlug is the exact model every call in this lane is pinned to.
const groundingSlug = "deepseek/deepseek-v4.1-flash"

// groundingAsk is the person's own sentence from the recorded failure: an
// application readiness condition, NOT a status condition.
const groundingAsk = "Notify me once when the health endpoint at http://127.0.0.1:18777/ready becomes ready. While it is not ready, check it every five minutes."

// statusOnlyCurl is the probe the failure reduced the ask to: it throws the body
// away and prints only the status code, so 200 is printed in both states.
const statusOnlyCurl = "curl -s -o /dev/null -w '%{http_code}' --max-time 10 http://127.0.0.1:18777/ready"

// bodyCurl is the probe that reads what the person named: the response body
// itself, where a readiness endpoint reports its real state.
const bodyCurl = "curl -s --max-time 10 http://127.0.0.1:18777/ready"

// groundingCase is one row of the matrix.
type groundingCase struct {
	name     string
	words    string
	hint     string
	command  string
	evidence string
	expect   standing.SentinelReading
	// decisive marks the recorded failing shape: a wrong answer here is a
	// failure of the fix, not a wobble to note.
	decisive bool
	samples  int
}

func groundingVerdictName(v standing.SentinelReading) string {
	switch v {
	case standing.VerdictYes:
		return "yes"
	case standing.VerdictNo:
		return "no"
	}
	return "unknown"
}

// groundingMatrix is the whole recorded shape plus the states the fix must not
// break: the person's own status criterion (legitimate bodyless contract), a
// body that contradicts the hint both ways, the first look finding it already
// true, and the fail-closed readings.
func groundingMatrix() []groundingCase {
	return []groundingCase{
		{name: "bare200_vs_readiness", words: groundingAsk, hint: "yes when the output is 200", command: statusOnlyCurl, evidence: "200", expect: standing.VerdictUnknown, decisive: true, samples: 3},
		{name: "bare200_nohint", words: groundingAsk, hint: "", command: statusOnlyCurl, evidence: "200", expect: standing.VerdictUnknown, decisive: true, samples: 2},
		{name: "explicit_user_http200", words: "Notify me once when the endpoint answers HTTP 200.", hint: "yes when the output is 200", command: statusOnlyCurl, evidence: "200", expect: standing.VerdictYes, samples: 2},
		{name: "jsonfalse_contradicts_hint", words: groundingAsk, hint: "yes when the output is 200", command: bodyCurl, evidence: `{"ready": false}`, expect: standing.VerdictNo, samples: 1},
		{name: "jsontrue_contradicts_hint", words: groundingAsk, hint: "yes when the output is 200", command: bodyCurl, evidence: `{"ready": true}`, expect: standing.VerdictYes, samples: 1},
		{name: "currently_ready_first_look", words: groundingAsk, hint: "yes when the output is 200", command: bodyCurl, evidence: `{"ready": true}`, expect: standing.VerdictYes, samples: 1},
		{name: "missing_body", words: groundingAsk, hint: "yes when the output is 200", command: statusOnlyCurl, evidence: "", expect: standing.VerdictUnknown, samples: 1},
		{name: "failed_command", words: groundingAsk, hint: "yes when the output is 200", command: statusOnlyCurl, evidence: "200\n(the command failed: exit status 7)", expect: standing.VerdictUnknown, samples: 1},
		{name: "clipped_body", words: groundingAsk, hint: "yes when the output is 200", command: "tail -c 65536 /var/log/app.log", evidence: "\u2026\n" + strings.Repeat("noise line with no readiness in it\n", 200), expect: standing.VerdictUnknown, samples: 1},
	}
}

// groundingJudgment is the exact judgment the live ticker would hand the
// sentinel: the person's words, the proposer's hint, the probe command, the
// evidence, and the one-model origin that pins the model.
func groundingJudgment(c groundingCase) standing.Judgment {
	return standing.Judgment{
		Item: standing.Item{
			Words: c.words,
			When: standing.When{
				Kind:  standing.WhenProbe,
				Hint:  c.hint,
				Probe: standing.Probe{Command: c.command},
			},
			Origin: standing.Origin{OneModel: true, PinnedModel: groundingSlug},
		},
		Evidence: c.evidence,
	}
}

// TestStandingSentinelGroundingE2E is the live matrix. It fails when a case
// answers other than the recorded truth, and it fails the whole run when the
// recorded failing shape does not answer unknown.
func TestStandingSentinelGroundingE2E(t *testing.T) {
	key := liveKey(t)
	w := newWorld(t)
	cfg := w.posture()
	cfg.Model = groundingSlug
	cfg.APIKey = key
	sentinel := session.NewStandingSentinelVerdict(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Logf("model: %s (pinned OneModel, no fallback); %s is the recorded failure shape", groundingSlug, "bare200_vs_readiness")
	spent, wrong, decided := 0.0, 0, true
	for _, c := range groundingMatrix() {
		for i := 0; i < c.samples; i++ {
			verdict, line, usd, err := sentinel(ctx, groundingJudgment(c))
			spent += usd
			if err != nil {
				if c.decisive {
					decided = false
				}
				t.Errorf("%s sample %d: sentinel error: %v", c.name, i+1, err)
				continue
			}
			got := groundingVerdictName(verdict)
			t.Logf("%s sample %d: expect %s got %s -- %s", c.name, i+1, groundingVerdictName(c.expect), got, line)
			if verdict != c.expect {
				wrong++
				if c.decisive {
					decided = false
				}
				t.Errorf("%s sample %d: want %s got %s -- %s", c.name, i+1, groundingVerdictName(c.expect), got, line)
			}
		}
	}
	t.Logf("matrix cost: $%.6f", spent)
	if !decided {
		t.Fatalf("the recorded failing shape (bare200_vs_readiness) did not answer unknown: the readiness false positive is live")
	}
	if wrong > 0 {
		t.Fatalf("%d of the matrix's samples answered other than the recorded truth", wrong)
	}
	fmt.Println("standing sentinel grounding matrix: all cases answered the recorded truth")
}

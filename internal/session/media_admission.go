package session

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// MediaAdmission is a generation route resolved at the actual tool invocation.
// A connected surface can explain a refused capability without guessing intent
// from conversation prose or choosing an arbitrary newly advertised model.
type MediaAdmission struct {
	Model   string
	Reason  string
	Message string
	Options []string
}

func (a *Agent) refuseMediaAdmission(ctx context.Context, kind string, route MediaAdmission) (string, bool, error) {
	message := strings.TrimSpace(route.Message)
	if message == "" {
		message = "Choose a compatible " + kind + " model before generating."
	}
	jobs := a.renderJobs()
	if jobs == nil {
		return message + " The refused attempt could not be saved because the session is closed.", true, nil
	}
	ledger := jobs.renderLedger()
	made := madeBy(ctx, nil)
	now := time.Now()
	id, err := ledger.beginChecked(0, kind, "", made.Run, made.Tool, now)
	if err != nil {
		return message + " The refused attempt could not be saved: " + err.Error(), true, nil
	}
	// The engine made no provider request: zero is a known local admission fact,
	// never an inference from an absent provider usage receipt.
	zero := float64(0)
	ledger.settle(id, renderPatch{state: RenderFailed, usage: &ai.Usage{Cost: &zero}, err: message, refusal: route.Reason, options: route.Options}, time.Now())
	ledger.release(id)
	return message, true, nil
}

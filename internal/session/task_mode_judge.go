package session

// WHETHER THIS TASK WORKS IN THEIR FOLDER.
//
// Ground and mode are two questions. The ladder answers the first: which
// folder the work is about. This file answers the second, and only the half
// of it that cannot fall out of the deliverable: did THIS PERSON ask to
// change that folder itself, with no copy set aside?
//
// A model-filled `where` used to answer both questions at once and skip the
// tree. The shaper was told to invent `in place` for "non-code work". A
// parent proposing children copied the repository path it could see. Neither
// is the person opting out. So this judge reads their request and nothing
// else — not the groomed brief, not the `where` argument — and every failure
// is a no: the task still gets a copy of its own.
//
// Place.Mode is a different door and is unchanged. That is a word they
// already recorded on a folder. This is the word on this request.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func init() {
	roles.Register(roles.RolePlace, roles.TierLow,
		"whether a task works in your folder or a copy of its own")
}

const (
	// taskPlaceWindow is how long a door will wait for this bit. It is the
	// sizing judge's own bound: one JSON object, and a stall must not hold
	// a `/task` the person just typed. Past it the answer is no, which is
	// isolation, which is the default.
	taskPlaceWindow = 3 * time.Second
	taskPlaceTokens = 40
	taskPlaceRepair = `Repair the answer. Return only {"in_place":false} or {"in_place":true}.`
)

// applyPersonMode is the door's write: judge this request, and if they asked
// to work in the folder itself record that on the spec so the ladder can
// obey it. A model-filled `where` is cleared first — it is not a decision.
func (a *Agent) applyPersonMode(ctx context.Context, spec *taskSpec) {
	if spec == nil {
		return
	}
	spec.where = ""
	if a.judgePersonMode(ctx, spec.request) {
		spec.personMode = TaskModeInPlace
		spec.where = "in place"
	}
}

// judgePersonMode reports whether the person's own words asked to skip the
// copy. No model, a stall, prose where JSON was asked for: no.
func (a *Agent) judgePersonMode(ctx context.Context, request string) bool {
	request = strings.TrimSpace(request)
	if request == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, taskPlaceWindow)
	defer cancel()

	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	messages := []ai.Message{textMessage("system", placePrompt), textMessage("user", request)}
	for attempt := 0; attempt < 2; attempt++ {
		response, judge, callErr := a.callRole(ctx, roles.RolePlace, model, messages,
			ai.WithMaxTokens(taskPlaceTokens))
		if callErr != nil || response == nil {
			return false
		}
		a.addAuxiliaryUsage(response, judge, 1)
		if inPlace, ok := parsePlaceJudge(response.Text()); ok {
			return inPlace
		}
		if strings.TrimSpace(response.Text()) == "" {
			return false
		}
		messages = append(messages, textMessage("assistant", response.Text()),
			textMessage("user", taskPlaceRepair))
	}
	return false
}

func parsePlaceJudge(text string) (bool, bool) {
	raw, err := subharness.Salvage(text)
	if err != nil {
		return false, false
	}
	var verdict struct {
		InPlace bool `json:"in_place"`
	}
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return false, false
	}
	return verdict.InPlace, true
}

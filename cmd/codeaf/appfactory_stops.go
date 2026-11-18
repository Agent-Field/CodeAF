package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A STOP IS THE MAIN AGENT'S FIRST ────────────────────────────────────────
//
// A job that stops on something its main agent may decide (internal/factory's
// handoff.go: a step out of rounds, a failed step, a Follow PR that stopped)
// is handed to that agent before its person is asked. On this machine's
// factory the hand-over is ONE turn of the job's own conversation
// ([appFactoryHost.stopped], wired as serve's Config.Stopped), by the road the
// stall's turn takes ([shapeTurns.through]).
//
// THE TURN DECIDES THROUGH THE AGENT'S OWN TOOLS, and the service enforces
// what it may decide (serve's manager_decide door, factory.ManagerDecides):
// the sentences below tell the agent what fits this stop, and nothing here is
// the rule. A turn that moves nothing hands the stop to the person, with what
// the agent said as its reason on the job's log.

// stopTurnWait is how long the main agent's turn on a stop may take: the
// stall turn's wait.
const stopTurnWait = stallTurnWait

// The hand-over's words, said once so the tests quote the same ones.
const (
	stopAsk       = "This job stopped, and the stop is yours to decide before its person is asked: %s"
	stopAskNoWord = "This job stopped, and the stop is yours to decide before its person is asked."
	stopAskTail   = "You may also send a delegate into the checkout (factory_task) or propose a public act (factory_act). Never merge. In Manual nothing goes public without the person: do not approve publication, mark the pull request ready or approve acts there unless the person said so in their own words. This is your one turn on this stop. If nothing you may do settles it, hand it to the person with factory_answer action hand_to_person and one plain sentence of why; a turn that changes nothing hands it over with whatever you say. Say in one sentence what you did."
)

// stopAskKinds says, for each kind of stop, what the main agent may do about
// it, by the tool actions the service takes.
var stopAskKinds = map[string]string{
	factory.StopRounds:   "A step used its rounds without finishing. With factory_answer you may give it another round with your instructions (action another_round), send the work back to the step before with why (send_back), run a step again from the start (rerun, with the step), or, only where the job offers it, take the step as it stands with your reason (accept).",
	factory.StopQuestion: "A step asked a question and nobody answered it. Read factory_status for the question and its id, then answer it with factory_answer (action answer), or hand it to the person if it is theirs to answer.",
	factory.StopFailed:   "A step failed. With factory_answer you may run it again with your instructions (action rerun, with the step), or, when the pull request is open, start a Follow PR round on it (resume_pipeline).",
	factory.StopUnsaved:  "The factory refused the results a step sent, and the runner still holds them. With factory_answer you may have the runner send them again (action resume_pipeline) or run the step again (rerun, with the step).",
	factory.StopPublish:  "Publishing failed after it was approved. With factory_answer you may try it again (action retry_publish); it publishes only the commit that was approved.",
	factory.StopFollow:   "Follow PR stopped on the open pull request. With factory_answer you may start another round with your instructions (action resume_pipeline), or go back to watching GitHub when nothing needs doing now (wait_github).",
}

// stopAskFor is the hand-over one main-agent turn is given: what stopped in
// the job's own words, the wait and offers the service stored, the job's last
// log lines, and what the agent may do about this kind of stop.
func stopAskFor(it factory.Item, wait factory.Wait) string {
	parts := []string{stopAskNoWord}
	if words := oneLineWords(wait.Words); words != "" {
		parts[0] = fmt.Sprintf(stopAsk, words)
	}
	if line := factory.WaitLine(it); line != "" {
		parts = append(parts, line)
	}
	parts = append(parts, stallLog(it))
	if kind := stopAskKinds[factory.ManagerStop(it)]; kind != "" {
		parts = append(parts, kind)
	}
	parts = append(parts, stopAskTail)
	return strings.Join(parts, " · ")
}

// Stopped is the main agent's one turn on a stop, with door as its tools for
// the turn. It answers what the agent said.
func (s *shapeTurns) Stopped(ctx context.Context, it factory.Item, wait factory.Wait, door session.RunDoor) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, stopTurnWait)
	defer cancel()
	return s.through(ctx, it, stopAskFor(it, wait), door)
}

// stopped is serve's Config.Stopped on the factory this host serves. The
// turn is charged to the job, as the stall's is ([appFactoryHost.stalled]),
// and carries the agent's own door, so every tool it has when its person
// talks to it works here under the same rules.
//
// A QUIET FACTORY ASKS NOBODY. Until a person turns its runner on it spends
// nothing by itself, and the stop goes straight to the person. So does the
// stop of a job that has no conversation of its own: it has no main agent.
func (h *appFactoryHost) stopped(ctx context.Context, it factory.Item, wait factory.Wait) (string, error) {
	if !h.readsOn() || strings.TrimSpace(it.Talk) == "" {
		return "", serve.ErrNotAsked
	}
	source, raw, ok := h.parentSource()
	if !ok {
		return "", errors.New("this machine has no model settings to open the main agent with")
	}
	current := func() session.Config {
		cfg := source.Config()
		if cfg.JobChat == nil {
			cfg.JobChat = jobChatDoorOf(h.floor)
		}
		return cfg
	}
	turns := factoryShapeTurnsFrom(h.floor, h.workspace, h.profile, current)
	turns.refusal = source.Refusal
	turns.liveOK = factoryLiveOK(raw, h.profile)
	return turns.Stopped(ctx, it, wait, runDoor(h.floor, h.workspace, h.profile))
}

// stopHanded has the service look at its jobs when a write hands a stop to a
// main agent: that write came from a runner's report or the GitHub poll, and
// no request follows it. It is told from the store's observer, which must not
// wait, so the look is asked for off it.
func (h *appFactoryHost) stopHanded(before, after factory.Item) {
	if _, held := factory.HandedOver(after); !held {
		return
	}
	if _, was := factory.HandedOver(before); was {
		return
	}
	guard.Go("factory/stop-handed", func() {
		if handler := h.serving(); handler != nil {
			handler.Wake()
		}
	})
}

// serving is the service this host has made, nil before its first use.
func (h *appFactoryHost) serving() *serve.Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handler
}

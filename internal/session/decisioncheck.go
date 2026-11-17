package session

// THE DECISION CHECK: FINISHED WORK, AND NEW WORK, AGAINST WHAT WAS DECIDED.
//
// A workstream keeps a Decisions list that records itself (an answered Ask, a
// request that changed shared work, a rule set in People and rules). It is the
// workstream's shared memory: how one person's agents know what another person
// decided. "Agents check finished work against the decisions. A conflict
// pauses that work and raises an Ask to the person who made the decision."
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - THE CHECK IS THE APP'S, NOT THE MODEL'S GOODWILL. It runs in the belt,
//     around ws_task_create and around ws_task_update when the work is called
//     done, whichever door answers those (the desk's client tools on a
//     Personal workstream, the server's door on an organization). A model
//     that never thought to look still cannot slip work past a decision.
//
//   - ONE FOCUSED QUESTION, ON THE PERSON'S OWN MODEL. The check is a small
//     classification asked of the conversation's model with nothing else in
//     the prompt: the decisions, numbered, and the work. A long instruction
//     block buried in a chat turn is what a small fast model skims; a short
//     question with one job is what it answers well.
//
//   - A CONFLICT ASKS WHOEVER MADE THE DECISION. The door raises the Ask
//     (ws_task_conflict, a verb the doors answer and the model never holds),
//     to the maker, with "Keep the decision" recommended. Nothing is done
//     anyway, and nobody else is asked.
//
//   - WHEN IN DOUBT, IT IS NOT A CONFLICT. Work that respects a decision, or
//     merely mentions its subject, passes: a check that cries wolf trains
//     people to answer "Go ahead anyway" without reading, which is worse than
//     no check. The prompt says so, and a reply that cannot be read passes.
//
//   - ABSENT, NOT BROKEN. A door that keeps no decisions (no ws_decisions
//     verb, or an empty list) is never asked to check anything.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The verbs the doors answer for the check. They are never on the model's
// belt: the check calls them itself.
const (
	// WSDecisionsTool lists the workstream's current decisions as
	// {"decisions":[{"id","text","by","by_name"}]}. It IS on the belt too: an
	// agent reads what was decided before it plans.
	WSDecisionsTool = "ws_decisions"
	// WSTaskConflictVerb raises the conflict Ask on a task: {"task",
	// "decision", "why"}. Only the check calls it.
	WSTaskConflictVerb = "ws_task_conflict"
)

// decisionCheckMost bounds what one check reads: the decisions it weighs and
// the words of the work.
const (
	decisionCheckDecisionsMost = 60
	decisionCheckWorkMost      = 8000
)

// checkDecision is one decision as the check weighs it.
type checkDecision struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	By     string `json:"by"`
	ByName string `json:"by_name"`
}

// checkWork is what is checked: a new task's request, or finished work.
type checkWork struct {
	Title   string
	Request string
	// Result is the words of what was made (a document's text, the check
	// line, links), empty for new work.
	Result string
}

// decisionConflict is one decision the work goes against, and the check's one
// line on how.
type decisionConflict struct {
	Decision checkDecision
	Why      string
}

// decisionCheckPrompt is the one question asked. KEEP IT SHORT AND
// SINGLE-PURPOSE (the law above); the examples teach the line between a
// conflict and a mention, which is where a small model goes wrong. They are
// deliberately NOT drawn from the requests the check was measured on
// (live2/E6), so the measurement says what the check does on work it has
// never seen.
func decisionCheckPrompt(decisions []checkDecision, work checkWork) string {
	var b strings.Builder
	b.WriteString("You check work against a team's decisions. Each decision is something the team settled; work must not go against one unless the person who made it agrees.\n\n")
	b.WriteString("Decisions:\n")
	for i, d := range decisions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, oneLine(d.Text))
	}
	b.WriteString("\nThe work:\n")
	fmt.Fprintf(&b, "Title: %s\n", oneLine(work.Title))
	if r := strings.TrimSpace(work.Request); r != "" {
		fmt.Fprintf(&b, "Request: %s\n", r)
	}
	if r := strings.TrimSpace(work.Result); r != "" {
		fmt.Fprintf(&b, "What was made:\n%s\n", r)
	}
	b.WriteString("\nDoes doing this work, or shipping what was made, go against any decision? ")
	b.WriteString("Going against means it would break, undo or contradict what was decided, including indirectly ")
	b.WriteString("(a delivery-notes field that asks for a home address breaks \"we never store addresses\"; a launch planned for after a stated deadline breaks the deadline). ")
	b.WriteString("Work that keeps to a decision, plans around it, or only mentions its subject does NOT go against it. When unsure, it does not.\n\n")
	b.WriteString("Answer with JSON only, no other words:\n")
	b.WriteString(`{"conflicts":[{"decision":<number>,"why":"<one short line on how the work goes against it>"}]}`)
	b.WriteString("\nor, when nothing conflicts:\n{\"conflicts\":[]}")
	return b.String()
}

var decisionJSON = regexp.MustCompile(`(?s)\{.*\}`)

// readDecisionCheck reads the model's answer. A reply that cannot be read, or
// names a decision that is not there, finds no conflict (WHEN IN DOUBT, IT IS
// NOT ONE).
func readDecisionCheck(reply string, decisions []checkDecision) []decisionConflict {
	raw := decisionJSON.FindString(reply)
	if raw == "" {
		return nil
	}
	var got struct {
		Conflicts []struct {
			Decision json.RawMessage `json:"decision"`
			Why      string          `json:"why"`
		} `json:"conflicts"`
	}
	if json.Unmarshal([]byte(raw), &got) != nil {
		return nil
	}
	var out []decisionConflict
	seen := map[int]bool{}
	for _, c := range got.Conflicts {
		n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(string(c.Decision)), `"`))
		if err != nil || n < 1 || n > len(decisions) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, decisionConflict{Decision: decisions[n-1], Why: oneLine(c.Why)})
	}
	return out
}

// checkAgainstDecisions asks model whether work goes against decisions. No
// decisions, no question.
func checkAgainstDecisions(ctx context.Context, ask func(ctx context.Context, prompt string) (string, error), decisions []checkDecision, work checkWork) ([]decisionConflict, error) {
	if len(decisions) == 0 {
		return nil, nil
	}
	if len(decisions) > decisionCheckDecisionsMost {
		decisions = decisions[len(decisions)-decisionCheckDecisionsMost:]
	}
	if len(work.Result) > decisionCheckWorkMost {
		work.Result = work.Result[:decisionCheckWorkMost]
	}
	reply, err := ask(ctx, decisionCheckPrompt(decisions, work))
	if err != nil {
		return nil, err
	}
	return readDecisionCheck(reply, decisions), nil
}

// ── the guard around the two tools that start or finish work ──

// workstreamCall answers one workstream verb by name: the server's door, or
// the desk's client tools.
type workstreamCall func(ctx context.Context, tool string, args json.RawMessage) (text string, isError bool, err error)

// guardsDecisions is whether tool is one the check stands around.
//
// ws_file_create is among them because a document an agent writes is shared
// the moment it lands: live2/E6 measured flash naming the right decision in
// 20 of 20 conflicting requests and still writing the conflicting work into
// shared Files or Tasks in 8 of 10. Detection was never the gap; acting on it
// was, so the app acts.
func guardsDecisions(tool string) bool {
	return tool == WSTaskCreateTool || tool == WSTaskUpdateTool || tool == WSFileCreateTool
}

// guardDecisions runs tool through call, checked against the workstream's
// decisions first. New work that goes against one is made, then paused on the
// Ask to its maker before anything runs; finished work that goes against one
// is NOT called done, everything else the call changed is kept, and the Ask
// is raised. Anything the check cannot do (no decisions, a door without the
// verbs, a model that does not answer) lets the call through unchanged: the
// check never stands in the way of work it could not read.
func (a *Agent) guardDecisions(ctx context.Context, tool string, args json.RawMessage, call workstreamCall) (string, bool, error) {
	return guardDecisionsWith(ctx, tool, args, call, func(ctx context.Context, prompt string) (string, error) {
		return a.askModel(ctx, toolAsk{tool: tool, doing: "checking against the decisions", model: a.Model(), prompt: prompt})
	})
}

// guardDecisionsWith is the guard with the check's model as ask.
func guardDecisionsWith(ctx context.Context, tool string, args json.RawMessage, call workstreamCall, ask func(ctx context.Context, prompt string) (string, error)) (string, bool, error) {
	var in map[string]json.RawMessage
	if json.Unmarshal(args, &in) != nil {
		return call(ctx, tool, args)
	}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(in[k], &s)
		return strings.TrimSpace(s)
	}
	if tool == WSFileCreateTool {
		return guardFile(ctx, args, str, call, ask)
	}
	finishing := tool == WSTaskUpdateTool && str("state") == "done"
	if tool == WSTaskUpdateTool && !finishing {
		return call(ctx, tool, args)
	}
	decisions := workstreamDecisions(ctx, call)
	if len(decisions) == 0 {
		return call(ctx, tool, args)
	}
	work := checkWork{Title: str("title"), Request: str("request")}
	if finishing {
		work = finishedWork(ctx, call, str("task"), in)
	}
	conflicts, err := checkAgainstDecisions(ctx, ask, decisions, work)
	if err != nil || len(conflicts) == 0 {
		return call(ctx, tool, args)
	}
	task, said, madeText := str("task"), "", ""
	if finishing {
		// Everything but "done" is kept: the result and the check line are the
		// work's, and the person deciding reads them.
		delete(in, "state")
		if hasChange(in) {
			rest, _ := json.Marshal(in)
			text, isErr, err := call(ctx, tool, rest)
			if err != nil || isErr {
				return text, isErr, err
			}
		}
	} else {
		text, isErr, err := call(ctx, tool, args)
		if err != nil || isErr {
			return text, isErr, err
		}
		var made struct {
			Said string `json:"said"`
			Task struct {
				ID string `json:"id"`
			} `json:"task"`
		}
		if json.Unmarshal([]byte(text), &made) != nil || made.Task.ID == "" {
			return text, isErr, err
		}
		task, said, madeText = made.Task.ID, made.Said, text
	}
	var asked []string
	for _, c := range conflicts {
		body, _ := json.Marshal(map[string]string{"task": task, "decision": c.Decision.ID, "why": c.Why})
		reply, isErr, err := call(ctx, WSTaskConflictVerb, body)
		if err != nil || isErr {
			continue
		}
		asked = append(asked, "“"+c.Decision.Text+"” ("+c.Why+"), so "+conflictAsked(reply, c.Decision)+" is asked")
	}
	if len(asked) == 0 {
		// NOTHING WAS ASKED, so nothing is said to have been. New work is
		// already made and is answered as made; calling the tool again would
		// be answered from the first call (the same call id), and finished
		// work goes through as the check lets any failure through.
		if !finishing {
			return madeText, false, nil
		}
		return call(ctx, tool, args)
	}
	lead := "Not done: this goes against "
	if !finishing {
		lead = strings.TrimSpace(said+" ") + " Paused before it starts: it goes against "
	}
	out, _ := json.Marshal(map[string]string{"said": strings.TrimSpace(lead + strings.Join(asked, "; and against ") +
		". Nothing goes ahead until they answer; tell the person you are talking to, and do not work around the decision."), "task": task})
	return string(out), false, nil
}

// hasChange is whether an update still changes anything beside naming its
// task and rev.
func hasChange(in map[string]json.RawMessage) bool {
	for k := range in {
		if k != "task" && k != "rev" {
			return true
		}
	}
	return false
}

func firstNonBlank(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

// workstreamDecisions are the workstream's current decisions, or none when
// its door keeps none.
func workstreamDecisions(ctx context.Context, call workstreamCall) []checkDecision {
	text, isErr, err := call(ctx, WSDecisionsTool, json.RawMessage(`{}`))
	if err != nil || isErr {
		return nil
	}
	var got struct {
		Decisions []checkDecision `json:"decisions"`
	}
	if json.Unmarshal([]byte(text), &got) != nil {
		return nil
	}
	return got.Decisions
}

// finishedWork is what a task about to be called done made: its title and
// request, the check line, and the words of up to three result files.
func finishedWork(ctx context.Context, call workstreamCall, task string, in map[string]json.RawMessage) checkWork {
	var w checkWork
	read, _ := json.Marshal(map[string]string{"task": task})
	var results []struct {
		Kind   string `json:"kind"`
		FileID string `json:"file_id"`
		File   string `json:"file"`
		URL    string `json:"url"`
		Label  string `json:"label"`
	}
	if text, isErr, err := call(ctx, WSTasksTool, read); err == nil && !isErr {
		var got struct {
			Task struct {
				Title   string `json:"title"`
				Request string `json:"request"`
				Check   string `json:"check"`
				Results json.RawMessage
			} `json:"task"`
		}
		if json.Unmarshal([]byte(text), &got) == nil {
			w.Title, w.Request, w.Result = got.Task.Title, got.Task.Request, got.Task.Check
			_ = json.Unmarshal(got.Task.Results, &results)
		}
	}
	if raw, ok := in["results"]; ok {
		results = nil
		_ = json.Unmarshal(raw, &results)
	}
	var check string
	if json.Unmarshal(in["check"], &check) == nil && strings.TrimSpace(check) != "" {
		w.Result = check
	}
	var b strings.Builder
	if w.Result != "" {
		b.WriteString(w.Result + "\n")
	}
	files := 0
	for _, r := range results {
		id := firstNonBlank(r.FileID, r.File)
		switch {
		case id != "" && files < 3:
			files++
			body, _ := json.Marshal(map[string]string{"file": id})
			if text, isErr, err := call(ctx, WSFileReadTool, body); err == nil && !isErr {
				b.WriteString(text + "\n")
			}
		case r.URL != "":
			b.WriteString(firstNonBlank(r.Label, r.URL) + ": " + r.URL + "\n")
		}
	}
	w.Result = strings.TrimSpace(b.String())
	return w
}

// guardFile checks a document an agent is about to write into shared Files.
// One that goes against a decision is NOT WRITTEN: there is no task to pause
// yet, so the agent is told to make one, which the check pauses on an Ask to
// the decision's maker. A folder, a link or an empty document is not checked.
func guardFile(ctx context.Context, args json.RawMessage, str func(string) string, call workstreamCall, ask func(context.Context, string) (string, error)) (string, bool, error) {
	words := str("markdown")
	if words == "" {
		return call(ctx, WSFileCreateTool, args)
	}
	decisions := workstreamDecisions(ctx, call)
	if len(decisions) == 0 {
		return call(ctx, WSFileCreateTool, args)
	}
	conflicts, err := checkAgainstDecisions(ctx, ask, decisions, checkWork{Title: str("name"), Result: words})
	if err != nil || len(conflicts) == 0 {
		return call(ctx, WSFileCreateTool, args)
	}
	var against []string
	for _, c := range conflicts {
		maker := firstNonBlank(c.Decision.ByName, c.Decision.By, "whoever made it")
		against = append(against, "“"+c.Decision.Text+"” ("+maker+"'s decision; "+c.Why+")")
	}
	return "Nothing written: this goes against " + strings.Join(against, " and ") +
		". Write nothing shared that goes against it. Make a task for this work with ws_task_create: the app pauses it and asks whoever made the decision, and you tell the person you are talking to who was asked.", true, nil
}

// conflictAsked is who the door says it asked, in words for the model: the
// person in the conversation on a Personal workstream ("you"), else the
// decider's name, else the decision's maker as the list spells them.
func conflictAsked(reply string, d checkDecision) string {
	var got struct {
		Asked string `json:"asked"`
	}
	_ = json.Unmarshal([]byte(reply), &got)
	switch a := strings.TrimSpace(got.Asked); {
	case strings.EqualFold(a, "you"):
		return "the person you are talking to"
	case a != "":
		return a
	}
	return firstNonBlank(d.ByName, d.By, "whoever made it")
}

// clientToolGuard is the guard standing around an app's tool name in set, or
// nil. A Personal workstream's tools are the desk's, so the decision check
// stands around them as it does around the server's door, wherever the set
// keeps decisions.
func (a *Agent) clientToolGuard(set ClientToolSet, name string) decisionGuard {
	if guardsDecisions(name) && clientSetServes(set, WSDecisionsTool) {
		return a.guardDecisions
	}
	return nil
}

// ClientToolUnregistered is whether an app's set answers name without having
// registered it, given the names it did register: the decision check's own
// verb, which the model must never hold, reaches any set that keeps
// decisions. Refusing it made every conflict the check found pause nothing
// (live2/E6b).
func ClientToolUnregistered(name string, registered func(string) bool) bool {
	return name == WSTaskConflictVerb && registered(WSDecisionsTool)
}

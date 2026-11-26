package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── A RUN STARTS WITH ITS CONTEXT PACKET (DESIGN §11.1–§11.2) ─────────────
//
// The work-loop audit of 2026-11-17 watched every dependant of a six-task plan
// spend four to seven tool calls (pages, ws_tasks, ws_decisions, ws_files,
// ws_tasks again) finding the release notes it was meant to build on, because
// the run was given the task's title and one paragraph and nothing else. It
// found them only because the workstream held six files.
//
//   - A TASK AN AGENT DOES IS WRITTEN AS A BRIEF. ws_task_create takes a goal,
//     a deliverable, two to six acceptance lines, inputs as references and
//     constraints; the door refuses an agent's task without the first three,
//     and every task it waits on becomes one of its inputs by itself.
//
//   - THE RUN'S FIRST MESSAGE CARRIES THE INPUTS' WORDS. taskPacket inlines
//     each input (a file's text; a task's hand-off note, check line and result
//     files; a decision's line) and the workstream's decisions in force, up to
//     [taskPacketBytes], so the doer starts writing instead of searching. Each
//     file is read through ws_file_read, the same door the model would use, so
//     a file the packet hands a run is marked as used by that task exactly as
//     one the run read itself.
//
//   - DONE CARRIES A HAND-OFF NOTE, AND THE NOTE TRAVELS. What the doer says it
//     made and assumed is kept on the task and is the first thing the next
//     task's packet says about it.

// taskPacketBytes is the most of the inputs' words one packet inlines: about
// twelve thousand tokens, the budget DESIGN §11.2 proposes.
const taskPacketBytes = 48 << 10

// taskPacketDecisionsMost is how many of the workstream's decisions in force
// the packet lists, newest kept.
const taskPacketDecisionsMost = 30

// agentBriefArgs are the brief's fields as ws_task_create takes them.
type agentBriefArgs struct {
	Goal        string                   `json:"goal"`
	Deliverable string                   `json:"deliverable"`
	Acceptance  []string                 `json:"acceptance"`
	Inputs      []factory.TaskBriefInput `json:"inputs"`
	Constraints []string                 `json:"constraints"`
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// briefOf checks the model's brief against the workstream and answers it
// clean. agentWork is whether an agent does the task: such a task is refused
// without a goal, a deliverable and its acceptance lines, because the run
// that does it never saw the conversation it came from. Every task in
// waitingOn becomes a task input when the brief does not name it already.
func (d *agentDoor) briefOf(ctx context.Context, a agentBriefArgs, waitingOn []string, agentWork bool) (*factory.TaskBrief, error) {
	b := factory.TaskBrief{Goal: a.Goal, Deliverable: a.Deliverable, Acceptance: a.Acceptance, Inputs: a.Inputs, Constraints: a.Constraints}
	for _, id := range waitingOn {
		id = strings.TrimSpace(id)
		if id != "" && !slices.ContainsFunc(b.Inputs, func(in factory.TaskBriefInput) bool { return in.Kind == factory.TaskInputTask && in.ID == id }) {
			b.Inputs = append(b.Inputs, factory.TaskBriefInput{Kind: factory.TaskInputTask, ID: id, Why: "waited on"})
		}
	}
	clean, err := factory.CleanBrief(&b)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if agentWork {
		var missing []string
		if clean == nil || clean.Goal == "" {
			missing = append(missing, "goal")
		}
		if clean == nil || clean.Deliverable == "" {
			missing = append(missing, "deliverable")
		}
		if clean == nil || len(clean.Acceptance) < factory.TaskAcceptanceFewest {
			missing = append(missing, fmt.Sprintf("acceptance (%d to %d checkable lines)", factory.TaskAcceptanceFewest, factory.TaskAcceptanceMost))
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("invalid arguments: an agent's task is written so another agent who never saw this conversation can do it; it still needs %s", strings.Join(missing, ", "))
		}
	}
	if clean == nil {
		return nil, nil
	}
	return clean, d.checkInputs(ctx, clean.Inputs)
}

// checkInputs refuses an input that names nothing in this workstream, so a
// brief never points its run at a file or task that is not there.
func (d *agentDoor) checkInputs(ctx context.Context, inputs []factory.TaskBriefInput) error {
	var (
		files     map[string]bool
		tasks     map[string]bool
		decisions map[string]bool
	)
	for _, in := range inputs {
		switch in.Kind {
		case factory.TaskInputFile:
			if files == nil {
				all, err := d.files(ctx, "")
				if err != nil {
					return err
				}
				files = map[string]bool{}
				for _, f := range all.Files {
					files[f.ID] = true
				}
				if all.From != nil {
					for _, f := range all.From.Files {
						files[f.ID] = true
					}
				}
			}
			if !files[in.ID] {
				return fmt.Errorf("invalid arguments: this workstream has no file %q; an input names a file by its id from ws_files", in.ID)
			}
		case factory.TaskInputTask:
			if tasks == nil {
				all, err := d.tasks(ctx)
				if err != nil {
					return err
				}
				tasks = map[string]bool{}
				for _, t := range all {
					tasks[t.ID] = true
				}
			}
			if !tasks[in.ID] {
				return fmt.Errorf("invalid arguments: this workstream has no task %q; an input names a task by its id from ws_tasks", in.ID)
			}
		case factory.TaskInputDecision:
			if decisions == nil {
				ws, err := d.Workstream(ctx)
				if err != nil {
					return err
				}
				decisions = map[string]bool{}
				for _, x := range factory.CurrentDecisions(ws.Decisions) {
					decisions[x.ID] = true
				}
			}
			if !decisions[in.ID] {
				return fmt.Errorf("invalid arguments: this workstream has no decision %q in force; ws_decisions lists them", in.ID)
			}
		}
	}
	return nil
}

// packetPart is one input's words in a packet, before the budget is shared.
type packetPart struct {
	head string
	// lead is said whole (a hand-off note, a check line); body is cut to the
	// part's share of the budget.
	lead, body string
	// more says how to read on when body is cut.
	more string
}

// taskPacket is the first message of t's run: words (the task and, for work
// sent back, why) followed by its brief, the workstream's decisions in force,
// and the words of every input, inlined up to taskPacketBytes. A part that
// cannot be read is named with why, never dropped silently. It answers the
// packet and the names of what it inlined.
func (h *Handler) taskPacket(ctx context.Context, d *agentDoor, task, words string) (string, []string) {
	t, err := d.task(ctx, task)
	if err != nil {
		return words, nil
	}
	ws, _ := d.Workstream(ctx)
	var out strings.Builder
	out.WriteString(strings.TrimSpace(words))
	b := t.Brief
	if b != nil {
		if b.Goal != "" {
			out.WriteString("\n\nGoal: " + b.Goal)
		}
		if b.Deliverable != "" {
			out.WriteString("\nDeliverable: " + b.Deliverable)
		}
		if len(b.Acceptance) > 0 {
			out.WriteString("\n\nAcceptance (your check answers each line):")
			for _, l := range b.Acceptance {
				out.WriteString("\n- " + l)
			}
		}
		if len(b.Constraints) > 0 {
			out.WriteString("\n\nConstraints:")
			for _, l := range b.Constraints {
				out.WriteString("\n- " + l)
			}
		}
	}
	if p := strings.TrimSpace(ws.Purpose); p != "" {
		out.WriteString("\n\nThe workstream " + ws.Name + " is for: " + p)
	}
	// The workstream's rules in words, as its people wrote them, bind every task.
	if said := ws.RulesOrDefault().Said; len(said) > 0 {
		out.WriteString("\n\nRules here (keep to them):")
		for _, l := range said {
			out.WriteString("\n- " + l)
		}
	}
	current := nameDecisions(factory.CurrentDecisions(ws.Decisions), h.personNames())
	// THE PACKET SAYS THE DECISIONS EVEN WHEN THERE ARE NONE. The rules tell
	// an agent to read the decisions before it writes anything shared, and a
	// packet silent on them sent every run of the live L7 plan to
	// ws_decisions "to be thorough".
	if len(current) == 0 {
		out.WriteString("\n\nDecisions in force: none yet.")
	} else {
		out.WriteString("\n\nDecisions in force (keep to them):")
		for _, x := range current[max(0, len(current)-taskPacketDecisionsMost):] {
			out.WriteString("\n- " + x.Text)
			if x.ByName != "" {
				out.WriteString(" (" + x.ByName + ")")
			}
		}
	}

	var parts []packetPart
	var used []string
	for _, in := range packetInputs(t) {
		ps, names := h.inputParts(ctx, d, in, current)
		parts = append(parts, ps...)
		used = append(used, names...)
	}
	if len(parts) > 0 {
		out.WriteString("\n\nInputs. Their words are below; you do not need to look them up.")
		left := taskPacketBytes
		for i, p := range parts {
			share := left / (len(parts) - i)
			body := p.body
			if len(body) > share {
				cut := cutBytes(body, share)
				body = cut + "\n[Cut here for length. " + p.more + "]"
			}
			left -= len(body)
			out.WriteString("\n\n## " + p.head)
			if p.lead != "" {
				out.WriteString("\n" + p.lead)
			}
			if body != "" {
				out.WriteString("\n" + body)
			}
		}
	}
	// THE PREDICTION IS KEPT BY THE RUN. A plan said this task "asks first";
	// the run is told the same rule, and which step it is, so the Ask comes
	// where the owner was promised it (v2 J6, DELTA §4.1).
	rules := ws.RulesOrDefault()
	switch {
	case t.AsksFirst == "starting":
		out.WriteString("\n\nRule: " + factory.AskRuleLine(rules) + ". Before you start, raise an Ask to the owner (ws_task_update ask) with how you will do it as its recommended option, and stop until it is answered.")
	case t.AsksFirst != "":
		out.WriteString("\n\nRule: " + factory.AskRuleLine(rules) + ". This task involves " + t.AsksFirst +
			": make it ready, then raise an Ask to the owner (ws_task_update ask, the step as its recommended option) and stop before you do it.")
	case rules.PublishesWithoutAsking:
		out.WriteString("\n\nRule: " + factory.PublishesWithoutAskingRule + " in this workstream.")
	}
	// THE EXCEPTIONS ARE SAID, AND ENFORCED. The run is told which kinds it
	// decides itself, and every Ask names its kind, so an Ask of a kind the
	// steward let go is decided by the server even when the run asks anyway.
	if len(rules.Exceptions) > 0 {
		kinds := make([]string, 0, len(rules.Exceptions))
		for _, e := range rules.Exceptions {
			kinds = append(kinds, e.Kind)
		}
		out.WriteString("\n\nDecided without asking here: " + strings.Join(kinds, "; ") +
			". Decide questions of these kinds yourself and list each under assumed; do not raise an Ask for them.")
	}
	out.WriteString("\n\nEvery Ask you raise names its kind: a few words for what sort of question it is (\"docs wording\", \"release date\"), the same words for the same sort each time.")
	out.WriteString(fmt.Sprintf("\n\nThis is everything the task needs, already read for you: the decisions above are the workstream's decisions, and the inputs' words are above. "+
		"So do not call ws_decisions, ws_tasks, ws_files or ws_file_read before you write; read with them only something this does not cover. "+
		"This task's id is %s; leave rev out when you update it. ", t.ID) +
		"Make the deliverable with ws_file_create, from_task " + t.ID + ". " +
		"When it is finished, call ws_task_update with state done, check saying what you checked it against, and handoff: two to four lines on what you made, what you assumed, and what the next task should know. " +
		"If you cannot do it (an input is missing, or it needs something you do not have, such as a sign-in only a person holds), do not fail it: raise an ask to the owner with options such as \"Paste their text\".")
	return out.String(), used
}

// packetInputs is what t's packet inlines: its brief's inputs, then every
// task it waits on that the brief does not name. A wait added after the task
// was made (ws_task_update waiting_on) never changes the brief, and the run
// still builds on what it waited for (live L7 run: the support macro was set
// to wait on the release notes afterwards).
func packetInputs(t factory.WorkstreamTask) []factory.TaskBriefInput {
	var out []factory.TaskBriefInput
	if t.Brief != nil {
		out = append(out, t.Brief.Inputs...)
	}
	for _, id := range t.WaitingOn {
		if !slices.ContainsFunc(out, func(in factory.TaskBriefInput) bool { return in.Kind == factory.TaskInputTask && in.ID == id }) {
			out = append(out, factory.TaskBriefInput{Kind: factory.TaskInputTask, ID: id, Why: "waited on"})
		}
	}
	return out
}

// inputParts reads one input into packet parts, and the names of what it read.
func (h *Handler) inputParts(ctx context.Context, d *agentDoor, in factory.TaskBriefInput, current []factory.WorkstreamDecision) ([]packetPart, []string) {
	why := ""
	if in.Why != "" {
		why = " (" + in.Why + ")"
	}
	switch in.Kind {
	case factory.TaskInputFile:
		p, name := d.filePart(ctx, in.ID, "File: ", why)
		return []packetPart{p}, []string{name}
	case factory.TaskInputTask:
		dep, err := d.task(ctx, in.ID)
		if err != nil {
			return []packetPart{{head: "Task " + in.ID + why, lead: "It could not be read: " + packetErr(err)}}, nil
		}
		head := fmt.Sprintf("Result of the task %q%s", dep.Title, why)
		var lead []string
		if dep.State != factory.TaskDone {
			lead = append(lead, "It is not done yet ("+dep.State+"); what it has so far is below.")
		}
		if dep.Handoff != "" {
			lead = append(lead, "Its hand-off note: "+dep.Handoff)
		}
		if dep.Check != "" {
			lead = append(lead, "Its check: "+dep.Check)
		}
		parts := []packetPart{{head: head, lead: strings.Join(lead, "\n")}}
		names := []string{dep.Title}
		for _, r := range dep.Results {
			switch {
			case r.FileID != "":
				p, name := d.filePart(ctx, r.FileID, "Its result file: ", "")
				parts = append(parts, p)
				names = append(names, name)
			case r.URL != "":
				parts[0].lead = strings.TrimSpace(parts[0].lead + "\nIts result link: " + firstNonEmpty(r.Label, r.URL) + " " + r.URL)
			}
		}
		return parts, names
	case factory.TaskInputDecision:
		for _, x := range current {
			if x.ID == in.ID {
				return []packetPart{{head: "Decision" + why, lead: x.Text}}, []string{"1 decision"}
			}
		}
		return []packetPart{{head: "Decision " + in.ID + why, lead: "It is no longer in force."}}, nil
	case factory.TaskInputURL:
		return []packetPart{{head: "Link" + why, lead: in.URL + "\nIts page is not inlined here. If you cannot open it, ask the owner for its text."}}, nil
	}
	return nil, nil
}

// filePart reads one file through ws_file_read (so it counts as used by the
// run's task) into a packet part, and answers the file's name.
func (d *agentDoor) filePart(ctx context.Context, id, lead, why string) (packetPart, string) {
	args, _ := json.Marshal(map[string]string{"file": id})
	raw, err := d.Work(ctx, "ws_file_read", args)
	if err != nil {
		return packetPart{head: lead + id + why, lead: "It could not be read: " + packetErr(err)}, ""
	}
	var f struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Markdown string `json:"markdown"`
		Text     string `json:"text"`
		URL      string `json:"url"`
		Said     string `json:"said"`
		MoreFrom int    `json:"more_from"`
	}
	_ = json.Unmarshal([]byte(raw), &f)
	p := packetPart{head: lead + firstNonEmpty(f.Name, id) + why, body: firstNonEmpty(f.Markdown, f.Text),
		more: "ws_file_read " + id + " reads it whole."}
	if f.MoreFrom > 0 {
		p.more = fmt.Sprintf("ws_file_read %s with from %d reads on.", id, f.MoreFrom)
	}
	switch {
	case p.body == "" && f.URL != "":
		p.lead = "A link to " + f.URL
	case p.body == "" && f.Said != "":
		p.lead = f.Said
	}
	return p, firstNonEmpty(f.Name, id)
}

// cutBytes cuts s to at most n bytes on a line, else a rune, boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8RuneStart(s[len(cut)]) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, '\n'); i > len(cut)/2 {
		cut = cut[:i]
	}
	return cut
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

func packetErr(err error) string {
	return cutRunes(strings.Join(strings.Fields(err.Error()), " "), 200)
}

// fileNames is the names of the files ids, joined for a sentence.
func (d *agentDoor) fileNames(ctx context.Context, ids []string) string {
	all, _ := d.files(ctx, "")
	var names []string
	for _, id := range ids {
		name := id
		if i := slices.IndexFunc(all.Files, func(f factory.WorkstreamFile) bool { return f.ID == id }); i >= 0 {
			name = all.Files[i].Name
		}
		names = append(names, fmt.Sprintf("%q", name))
	}
	return strings.Join(names, ", ")
}

package main

// ── THE TERMINAL DOOR: A TASK, STARTED AND FOLLOWED ────────────────────────
//
// `codeaf task "<what to do>"` makes an ordinary task in one of the person's
// workstreams on their organization's server, owned by them (PRD "Doors": work
// started from a door is an ordinary task, owned by the person who asked).
// Afi does it, started at once, when the organization's agents run on its
// server; with --me it is the person's own.
//
// Then the door follows it in ONE line, redrawn in place on a terminal: the
// state, what the agent is doing in its own words, and how long it has been
// going. Without a terminal a line is printed only when the state or the step
// changes. It ends on Done with what the task made (a pull request's address,
// a file), or on Failed, Stopped and the rest, in the app's words.
//
// AN ASK IS ANSWERED HERE ONLY WHEN IT IS QUICK AND YOURS: it can be undone, it
// recommends an answer, it needs no words of your own, and you decide it. Enter
// takes the recommendation. Anything else is a decision, and a decision is
// never answered in a door: the line says to open WorkAF, and where.
//
// The line is read by polling the task: what the run is doing now is filled in
// by the server when the task is read and is not published as an event, so the
// event stream could not move the line by itself.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

// doorEvery is how often a followed task is read again. A variable so a test
// can follow a task in milliseconds.
var doorEvery = 2 * time.Second

// doorNow is the door's clock, for elapsed time.
var doorNow = time.Now

// doorTitleMost is the longest title a task takes; a longer request keeps its
// first line, cut, as the title and goes whole into the request.
const doorTitleMost = 300

// runTask is `codeaf task "<what to do>" …` and `codeaf task follow <task>`.
func runTask(args []string) error {
	if len(args) > 0 && args[0] == "follow" {
		return runTaskFollow(args[1:])
	}
	flags := commandFlags("task")
	wsWant := flags.String("workstream", "", "the workstream, by name or id (asked when you are in several)")
	afi := flags.Bool("afi", false, "Afi does it, started now (the default when agents run on the server)")
	me := flags.Bool("me", false, "it is your own task, for you to do")
	orgWant := flags.String("org", "", "the organization, by address or name (default: the last one signed in to)")
	request := flags.String("request", "", "more words on what is wanted")
	noFollow := flags.Bool("no-follow", false, "start it and return without following it")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	words := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if words == "" {
		return wrongCall(`use codeaf task "<what to do>" [--workstream name] [--afi|--me]`)
	}
	if *afi && *me {
		return wrongCall("choose one of --afi and --me")
	}
	s, err := readDoorState()
	if err != nil {
		return err
	}
	o, err := s.org(*orgWant)
	if err != nil {
		return err
	}
	ctx := context.Background()
	ws, err := doorWorkstream(ctx, o, *wsWant)
	if err != nil {
		return err
	}
	byAfi := *afi
	if !*afi && !*me {
		byAfi = doorAgentsOnServer(ctx, o)
	}
	title, more := doorTitle(words)
	if r := strings.TrimSpace(*request); r != "" {
		more = strings.TrimSpace(strings.TrimSpace(more + "\n\n" + r))
	}
	body := factory.TaskCreate{Title: title, Request: more}
	if byAfi {
		body.DoneBy = factory.TaskDoneBy{Kind: factory.TaskByAfi}
		body.State = factory.TaskRunning
	} else {
		body.DoneBy = factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: o.Handle, Name: o.Person}
	}
	var made factory.WorkstreamTask
	if err := doorCall(ctx, o, http.MethodPost, doorTasksPath(ws.ID), body, &made); err != nil {
		return fmt.Errorf("the task was not started: %v", err)
	}
	if made.ID == "" {
		return errors.New("the organization answered without the task; look for it in WorkAF before trying again")
	}
	ref := doorTaskRef{Org: o.URL, WS: ws.ID, Task: made.ID, Title: made.Title}
	if err := updateDoorState(func(s *doorState) { s.remember(ref) }); err != nil {
		fmt.Fprintln(usageErr, "the task was started, but this terminal could not remember it:", err)
	}
	who := "yours, for you to do"
	if byAfi {
		who = factory.DoerWords(made.DoneBy) + " does it"
	}
	fmt.Fprintf(doorOut, "Started in %s: %s (%s) · %s\n", doorText(ws.Name), doorShort(doorText(made.Title), 80), doorText(made.ID), doorText(who))
	if !byAfi || *noFollow {
		fmt.Fprintf(doorOut, "Follow it with codeaf task follow %s, or open it in WorkAF.\n%s\n", doorText(made.ID), doorMadeWith)
		return nil
	}
	return doorFollow(ctx, o, ws, made)
}

// runTaskFollow is `codeaf task follow <task> [--org url]`. The task is named
// by its id, or as <workstream-id>/<task-id>.
func runTaskFollow(args []string) error {
	flags := commandFlags("task follow")
	orgWant := flags.String("org", "", "the organization, by address or name (default: the last one signed in to)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return wrongCall("use codeaf task follow <task-id>")
	}
	s, err := readDoorState()
	if err != nil {
		return err
	}
	o, err := s.org(*orgWant)
	if err != nil {
		return err
	}
	ctx := context.Background()
	ws, t, err := doorFindTask(ctx, o, s, strings.TrimSpace(flags.Arg(0)))
	if err != nil {
		return err
	}
	fmt.Fprintf(doorOut, "Following in %s: %s (%s)\n", doorText(ws.Name), doorShort(doorText(t.Title), 80), doorText(t.ID))
	return doorFollow(ctx, o, ws, t)
}

// doorTitle is the title a request is made under, and what goes in the request
// when the words do not fit a title.
func doorTitle(words string) (title, request string) {
	if !strings.Contains(words, "\n") && utf8.RuneCountInString(words) <= doorTitleMost {
		return words, ""
	}
	first, _, _ := strings.Cut(words, "\n")
	first = strings.TrimSpace(first)
	if r := []rune(first); len(r) > 120 {
		first = strings.TrimSpace(string(r[:120])) + "…"
	}
	return first, words
}

func doorTasksPath(ws string) string { return "/v1/workstreams/" + url.PathEscape(ws) + "/tasks" }

func doorTaskPath(ws, id string) string { return doorTasksPath(ws) + "/" + url.PathEscape(id) }

// doorAgentsOnServer is whether the organization runs its agents on its
// server, where Afi can start a task the moment it is made.
func doorAgentsOnServer(ctx context.Context, o doorOrg) bool {
	var settings appwire.OrgAgentRunner
	if doorCall(ctx, o, http.MethodGet, "/v1/settings", nil, &settings) != nil {
		return false
	}
	return settings.AgentRunner == appwire.OrgAgentRunnerServer
}

// doorWorkstreams are the workstreams the person can add a task to: the ones
// they may edit, not archived.
func doorWorkstreams(ctx context.Context, o doorOrg) ([]appwire.OrgWorkstreamSummary, error) {
	var list appwire.OrgWorkstreams
	if err := doorCall(ctx, o, http.MethodGet, "/v1/workstreams", nil, &list); err != nil {
		return nil, err
	}
	out := []appwire.OrgWorkstreamSummary{}
	for _, ws := range list.Workstreams {
		if ws.ArchivedAt != "" || (ws.MyRole != "edit" && ws.MyRole != "full") {
			continue
		}
		out = append(out, ws)
	}
	return out, nil
}

// doorWorkstream is the workstream a task goes in: the one named (by id, by
// name, or by the start of a name, when only one matches), the only one the
// person is in, or the one they pick from a list. Without a terminal to pick
// at, the refusal names the choices.
func doorWorkstream(ctx context.Context, o doorOrg, want string) (appwire.OrgWorkstreamSummary, error) {
	list, err := doorWorkstreams(ctx, o)
	if err != nil {
		return appwire.OrgWorkstreamSummary{}, err
	}
	if len(list) == 0 {
		return appwire.OrgWorkstreamSummary{}, fmt.Errorf("you are in no workstream on %s that you can add work to; join one in WorkAF", o.label())
	}
	if want = strings.TrimSpace(want); want != "" {
		var matches []appwire.OrgWorkstreamSummary
		for _, ws := range list {
			if ws.ID == want || strings.EqualFold(ws.Name, want) {
				return ws, nil
			}
			if strings.HasPrefix(strings.ToLower(ws.Name), strings.ToLower(want)) {
				matches = append(matches, ws)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			list = matches
		} else {
			return appwire.OrgWorkstreamSummary{}, fmt.Errorf("no workstream of yours is called %q; yours are %s", want, doorNames(list))
		}
	} else if len(list) == 1 {
		return list[0], nil
	}
	if !doorInIsTerminal() {
		return appwire.OrgWorkstreamSummary{}, fmt.Errorf("say which workstream with --workstream: %s", doorNames(list))
	}
	fmt.Fprintln(doorOut, "Which workstream?")
	for i, ws := range list {
		fmt.Fprintf(doorOut, "  %d. %s\n", i+1, doorText(ws.Name))
	}
	fmt.Fprint(doorOut, "Number: ")
	line, err := doorReadLine()
	if err != nil {
		return appwire.OrgWorkstreamSummary{}, errors.New("no workstream was chosen")
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(list) {
		return appwire.OrgWorkstreamSummary{}, errors.New("that is not one of the numbers")
	}
	return list[n-1], nil
}

// doorNames lists workstreams for a sentence: "Press kit", "Spring launch".
func doorNames(list []appwire.OrgWorkstreamSummary) string {
	names := make([]string, len(list))
	for i, ws := range list {
		names[i] = strconv.Quote(doorText(ws.Name))
	}
	return strings.Join(names, ", ")
}

// doorFindTask finds the task ref names: <ws>/<task>, a task this terminal
// started, or else a task in one of the person's workstreams.
func doorFindTask(ctx context.Context, o doorOrg, s doorState, ref string) (appwire.OrgWorkstreamSummary, factory.WorkstreamTask, error) {
	var none factory.WorkstreamTask
	wsID, taskID, split := strings.Cut(ref, "/")
	if !split {
		wsID, taskID = "", ref
		for _, kept := range s.Tasks {
			if kept.Org == o.URL && kept.Task == taskID {
				wsID = kept.WS
				break
			}
		}
	}
	if taskID == "" {
		return appwire.OrgWorkstreamSummary{}, none, wrongCall("use codeaf task follow <task-id>")
	}
	var list appwire.OrgWorkstreams
	if err := doorCall(ctx, o, http.MethodGet, "/v1/workstreams", nil, &list); err != nil {
		return appwire.OrgWorkstreamSummary{}, none, err
	}
	for _, ws := range list.Workstreams {
		if wsID != "" && ws.ID != wsID {
			continue
		}
		if wsID == "" && ws.MyRole == "" {
			continue
		}
		var t factory.WorkstreamTask
		err := doorCall(ctx, o, http.MethodGet, doorTaskPath(ws.ID, taskID), nil, &t)
		if err == nil && t.ID != "" {
			return ws, t, nil
		}
		if wsID != "" {
			return ws, none, fmt.Errorf("no task %s in %s: %v", taskID, doorText(ws.Name), err)
		}
	}
	return appwire.OrgWorkstreamSummary{}, none, fmt.Errorf("no task %s in any workstream you can see on %s", taskID, o.label())
}

// doorFollower follows one task on one line.
type doorFollower struct {
	org    doorOrg
	ws     appwire.OrgWorkstreamSummary
	task   factory.WorkstreamTask
	redraw bool // the line is redrawn in place (a terminal)
	answer bool // a quick Ask can be answered here (someone is at the keys)
	drawn  string
	said   string
	// settled are the open Asks the door is done with: said (one that is not
	// answered here), answered, or left for WorkAF by the person. An Ask that
	// closes is forgotten, so one reopened (an answer undone) is asked again.
	settled  map[string]bool
	stop     chan os.Signal // ^C, while following
	interval time.Duration
}

// doorFollow follows t until it ends, or until the person leaves with ^C (the
// task goes on without them; the door says how to come back).
func doorFollow(ctx context.Context, o doorOrg, ws appwire.OrgWorkstreamSummary, t factory.WorkstreamTask) error {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	f := &doorFollower{
		org: o, ws: ws, task: t,
		redraw: doorOutIsTerminal(), answer: doorInIsTerminal(),
		settled: map[string]bool{}, stop: interrupts, interval: doorEvery,
	}
	return f.run(ctx)
}

func (f *doorFollower) run(ctx context.Context) error {
	misses := 0
	for {
		f.show()
		if doorTaskEnded(f.task.State) {
			return f.finish(ctx)
		}
		if f.asks(ctx) {
			// The answer's reply is the task as it is now: draw it at once.
			continue
		}
		select {
		case <-f.stop:
			f.clear()
			fmt.Fprintf(doorOut, "Stopped following; the task goes on in %s. Follow it again with codeaf task follow %s\n%s\n", doorText(f.ws.Name), doorText(f.task.ID), doorMadeWith)
			return exitStatus(130)
		case <-time.After(f.interval):
		}
		if err := f.read(ctx); err != nil {
			// A server that blinks does not end the follow; one that stays
			// away, or refuses, does.
			var ended doorSignInEnded
			var refused client.Refusal
			misses++
			if errors.As(err, &ended) || errors.As(err, &refused) || misses >= 5 {
				f.clear()
				return err
			}
			continue
		}
		misses = 0
	}
}

func (f *doorFollower) read(ctx context.Context) error {
	var t factory.WorkstreamTask
	if err := doorCall(ctx, f.org, http.MethodGet, doorTaskPath(f.ws.ID, f.task.ID), nil, &t); err != nil {
		return err
	}
	if t.ID != "" {
		f.task = t
	}
	return nil
}

// show draws the task's one line. On a terminal it replaces the line already
// there; otherwise a line is printed only when the state or step changed.
func (f *doorFollower) show() {
	line := doorLine(f.task, f.org.Handle, doorNow())
	if f.redraw {
		if w := doorWidth(); w > 1 && utf8.RuneCountInString(line) >= w {
			line = string([]rune(line)[:w-2]) + "…"
		}
		if line != f.drawn {
			fmt.Fprint(doorOut, "\r\x1b[K"+line)
			f.drawn = line
		}
		return
	}
	// Elapsed time moves every second; only a change of what is happening
	// earns a new line in a log.
	what := doorLineWithout(f.task, f.org.Handle)
	if what != f.said {
		fmt.Fprintln(doorOut, line)
		f.said = what
	}
}

// clear takes the live line away so a block of text starts at column one.
func (f *doorFollower) clear() {
	if f.redraw && f.drawn != "" {
		fmt.Fprint(doorOut, "\r\x1b[K")
		f.drawn = ""
	}
}

// asks says each open Ask once: a quick one of yours is answered here, any
// other is pointed at WorkAF. It reports whether an answer was sent.
func (f *doorFollower) asks(ctx context.Context) bool {
	open := factory.OpenAsks(f.task)
	// An Ask no longer open is forgotten: if it opens again (its answer was
	// undone), it is put to the person again.
	for id := range f.settled {
		if !slices.ContainsFunc(open, func(a factory.TaskAsk) bool { return a.ID == id }) {
			delete(f.settled, id)
		}
	}
	for _, ask := range open {
		if f.settled[ask.ID] {
			continue
		}
		f.clear()
		decider := factory.AskDecider(f.task, ask)
		where := f.where()
		title := doorText(ask.Title)
		switch {
		case !f.mayAnswer(ask):
			f.settled[ask.ID] = true
			fmt.Fprintf(doorOut, "Waiting on %s to answer: %s\n", doorText(firstNonEmptyString(decider.Name, decider.ID, "its owner")), title)
		case !doorQuickAsk(ask):
			f.settled[ask.ID] = true
			fmt.Fprintf(doorOut, "%s asks: %s\nThis one is a decision: open WorkAF to answer it (%s).\n", doorAsker(ask), title, where)
		case !f.answer:
			f.settled[ask.ID] = true
			fmt.Fprintf(doorOut, "%s asks: %s\nAnswer it in WorkAF (%s), or here with codeaf task follow %s at a terminal.\n", doorAsker(ask), title, where, doorText(f.task.ID))
		default:
			// Settled only once it is answered or the person leaves it: an
			// answer the server did not take is asked again at the next read.
			answered, done := f.ask(ctx, ask)
			if done {
				f.settled[ask.ID] = true
			}
			if answered {
				return true
			}
		}
	}
	return false
}

// mayAnswer is whether the person at this door may answer ask, by the server's
// own rule (factory.MayAnswerAsk): they decide it (handles compared the way
// the server compares them), they may answer for an owner of Everyone, or they
// are the workstream's fallback and the server says the Ask has waited long
// enough for them to cover it.
func (f *doorFollower) mayAnswer(ask factory.TaskAsk) bool {
	actor := factory.TaskActor{
		Handle: factory.NormalizeHandle(f.org.Handle),
		Edit:   f.ws.MyRole == "edit" || f.ws.MyRole == "full",
	}
	if actor.Handle == "" {
		return false
	}
	// The door is not told the workstream's rules; the server names who covers
	// an Ask that has waited past the wait (CoveredBy, filled when the task is
	// read), and that person stands in from the moment it was asked.
	var standin factory.Standin
	if ask.CoveredBy != nil {
		standin = factory.Standin{ID: ask.CoveredBy.ID, After: time.Nanosecond}
	}
	return factory.MayAnswerAsk(f.task, ask.ID, actor, standin, doorNow())
}

// ask puts one quick Ask to the person and sends their answer. answered is
// whether the server took an answer; done is whether the door is finished with
// the Ask (answered, or left for WorkAF by the person).
func (f *doorFollower) ask(ctx context.Context, ask factory.TaskAsk) (answered, done bool) {
	fmt.Fprintf(doorOut, "%s asks: %s\n", doorAsker(ask), doorText(ask.Title))
	if d := strings.TrimSpace(doorBlock(ask.Detail)); d != "" {
		fmt.Fprintf(doorOut, "  %s\n", strings.ReplaceAll(d, "\n", "\n  "))
	}
	recommended := 0
	for i, opt := range ask.Options {
		mark := ""
		if opt.Recommended && recommended == 0 {
			recommended, mark = i+1, " (recommended)"
		}
		fmt.Fprintf(doorOut, "  %d. %s%s\n", i+1, doorText(opt.Label), mark)
	}
	fmt.Fprintf(doorOut, "Enter takes %d, a number picks another, s leaves it for WorkAF: ", recommended)
	// While the person reads, ^C does what it always does: it leaves.
	signal.Stop(f.stop)
	line, err := doorReadLine()
	signal.Notify(f.stop, os.Interrupt)
	line = strings.TrimSpace(line)
	if err != nil || strings.EqualFold(line, "s") {
		fmt.Fprintf(doorOut, "\nLeft for later: answer it in WorkAF (%s).\n", f.where())
		return false, true
	}
	pick := recommended
	if line != "" {
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(ask.Options) {
			fmt.Fprintf(doorOut, "That is not one of the numbers; it is left for WorkAF (%s).\n", f.where())
			return false, true
		}
		pick = n
	}
	opt := ask.Options[pick-1]
	if opt.Words {
		fmt.Fprintf(doorOut, "That answer needs your own words: give it in WorkAF (%s).\n", f.where())
		return false, true
	}
	body := map[string]string{"option": opt.ID, "ask": ask.ID}
	var reply factory.WorkstreamTask
	if err := doorCall(ctx, f.org, http.MethodPost, doorTaskPath(f.ws.ID, f.task.ID)+"/answer", body, &reply); err != nil {
		fmt.Fprintf(doorOut, "The answer was not taken: %s\nIt will be asked again.\n", doorText(err.Error()))
		return false, false
	}
	fmt.Fprintf(doorOut, "Answered: %s. It can be undone in WorkAF.\n", doorText(opt.Label))
	if reply.ID != "" {
		f.task = reply
	}
	return true, true
}

// finish ends the line and says what the task came to: what it made when it
// is done, why when it is not.
func (f *doorFollower) finish(ctx context.Context) error {
	if f.redraw {
		fmt.Fprintln(doorOut)
	}
	t := f.task
	var names map[string]string
	for _, r := range t.Results {
		switch {
		case r.URL != "":
			fmt.Fprintf(doorOut, "  %s  %s\n", doorText(firstNonEmptyString(r.Label, "Link")), doorText(r.URL))
		case r.FileID != "":
			if names == nil {
				names = doorFileNames(ctx, f.org, f.ws.ID)
			}
			fmt.Fprintf(doorOut, "  %s (in the workstream's Files)\n", doorText(firstNonEmptyString(r.Label, names[r.FileID], "A file")))
		}
	}
	if note := strings.TrimSpace(t.Handoff); note != "" && t.State == factory.TaskDone {
		first, _, _ := strings.Cut(note, "\n")
		if r := []rune(first); len(r) > 200 {
			first = string(r[:200]) + "…"
		}
		fmt.Fprintf(doorOut, "  %s: %s\n", doorText(factory.DoerWords(t.DoneBy)), doorText(first))
	}
	fmt.Fprintf(doorOut, "Open it in WorkAF: %s\n%s\n", f.where(), doorMadeWith)
	if t.State == factory.TaskDone {
		return nil
	}
	return exitStatus(1)
}

// where names the task the way WorkAF shows it: workstream › task, with a long
// title cut so the sentence stays on a line or two.
func (f *doorFollower) where() string {
	return doorText(f.ws.Name) + " › " + doorShort(doorText(f.task.Title), 60)
}

// doorShort is s cut to most characters, with an ellipsis when it was cut.
func doorShort(s string, most int) string {
	if r := []rune(s); len(r) > most {
		return strings.TrimSpace(string(r[:most])) + "…"
	}
	return s
}

// doorFileNames are the names of a workstream's files by id, for saying what a
// task made; empty when they cannot be read, and the line says "A file".
func doorFileNames(ctx context.Context, o doorOrg, ws string) map[string]string {
	var list struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	names := map[string]string{}
	if doorCall(ctx, o, http.MethodGet, "/v1/workstreams/"+url.PathEscape(ws)+"/files", nil, &list) == nil {
		for _, f := range list.Files {
			names[f.ID] = f.Name
		}
	}
	return names
}

// doorTaskEnded is whether a task in state s has stopped moving by itself.
func doorTaskEnded(s string) bool {
	switch s {
	case factory.TaskDone, factory.TaskFailed, factory.TaskInterrupted, factory.TaskStopped, factory.TaskNoReport:
		return true
	}
	return false
}

// doorQuickAsk is whether ask can be answered in a door: it can be undone, it
// recommends an answer, and none of its answers needs the person's own words.
func doorQuickAsk(ask factory.TaskAsk) bool {
	if !ask.Reversible || factory.NeedsWords(ask) {
		return false
	}
	for _, opt := range ask.Options {
		if opt.Recommended {
			return true
		}
	}
	return false
}

// doorAsker is who raised ask, as the line names them.
func doorAsker(ask factory.TaskAsk) string {
	if ask.By != nil {
		switch ask.By.Kind {
		case factory.TaskByAfi:
			return "Afi"
		case factory.TaskByFactory:
			return "The Factory"
		}
		if name := doorText(ask.By.Name); name != "" {
			return name
		}
	}
	return "Afi"
}

// doorLine is the task's one line: its state in the app's words, what it is
// doing now, and how long it has been going.
func doorLine(t factory.WorkstreamTask, me string, now time.Time) string {
	what := doorLineWithout(t, me)
	if took := doorElapsed(t, now); took != "" {
		return what + " · " + took
	}
	return what
}

// doorLineWithout is doorLine without the time, so a log can tell a change of
// what is happening from the clock moving.
func doorLineWithout(t factory.WorkstreamTask, me string) string {
	parts := []string{doorStateWords(t, me)}
	switch t.State {
	case factory.TaskRunning:
		if t.Run != nil && strings.TrimSpace(t.Run.Now) != "" {
			parts = append(parts, strings.TrimSpace(t.Run.Now))
		} else if t.Step != "" {
			parts = append(parts, t.Step)
		}
	case factory.TaskNeeds:
		if asks := factory.OpenAsks(t); len(asks) > 0 {
			parts = append(parts, asks[0].Title)
		}
	case factory.TaskFailed, factory.TaskInterrupted:
		if r := strings.TrimSpace(t.StateReason); r != "" {
			parts = append(parts, r)
		}
	}
	line := doorText(strings.Join(parts, " · "))
	return strings.Join(strings.Fields(line), " ")
}

// doorStateWords is a task's state as the app says it.
func doorStateWords(t factory.WorkstreamTask, me string) string {
	switch t.State {
	case factory.TaskUpNext:
		if t.Queue != nil && t.Queue.Place > 0 {
			return "Queued · " + doorOrdinal(t.Queue.Place)
		}
		return "Up next"
	case factory.TaskRunning:
		return "Running"
	case factory.TaskNeeds:
		return "Waiting on an answer"
	case factory.TaskDone:
		return "Done"
	case factory.TaskFailed:
		return "Failed"
	case factory.TaskInterrupted:
		return "Interrupted"
	case factory.TaskStopped:
		if t.StoppedBy != nil {
			if t.StoppedBy.ID == me {
				return "Stopped by you"
			}
			return "Stopped by " + doorText(firstNonEmptyString(t.StoppedBy.Name, t.StoppedBy.ID))
		}
		return "Stopped"
	case factory.TaskNoReport:
		return "Didn't report back"
	}
	return doorText(t.State)
}

// doorElapsed is how long the task has been going: from when it was made to
// when it was done, else now. Not from its run's start: an answered Ask starts
// a new run, and the clock a person watches must not go back to zero.
func doorElapsed(t factory.WorkstreamTask, now time.Time) string {
	start, err := time.Parse(time.RFC3339Nano, t.CreatedAt)
	if err != nil {
		return ""
	}
	end := now
	if t.DoneAt != "" {
		if done, err := time.Parse(time.RFC3339Nano, t.DoneAt); err == nil {
			end = done
		}
	}
	return doorDuration(end.Sub(start))
}

// doorDuration is a duration as the line says it: 42s, 3m 05s, 1h 02m.
func doorDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func doorOrdinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

// doorText is text from the organization's server (an agent's words, a title,
// a name) made safe to print on one terminal line: control characters (C0, DEL,
// C1) and invisible format characters (Unicode Cf, which can reorder what a
// line shows) are dropped, and tabs and line breaks become spaces. A task's
// words must never move the cursor, recolour the terminal or set its title.
func doorText(s string) string { return doorClean(s, false) }

// doorBlock is doorText for text printed over several lines: line breaks are
// kept (a lone carriage return is not, it would draw over the line).
func doorBlock(s string) string { return doorClean(s, true) }

func doorClean(s string, lines bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' && lines:
			b.WriteByte('\n')
		case r == '\t' || r == '\n':
			b.WriteByte(' ')
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, unicode.Is(unicode.Cf, r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

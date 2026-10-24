package tui3

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Replay keeps runtime receipts as inspectable work without rewriting their journal.
func replayCompletionEntries(e session.DisplayEntry, blocks []entry, turn int) []entry {
	if !e.RoutineCompletion {
		return nil
	}
	if len(e.CompletionTasks) > 0 {
		out := make([]entry, 0, len(e.CompletionTasks))
		for _, tag := range e.CompletionTasks {
			facts := session.TaskFacts{State: tag.State}
			if tag.Stopped {
				facts.Ending = session.TaskEndingStopped
			}
			state := session.ProjectTask(facts).Word
			c := &workCompletion{owner: replayCompletionOwner(blocks, tag.ID, 0, tag.Request), task: tag.ID, title: tag.Title, state: state, text: e.Text, key: fmt.Sprintf("task/%d/%d", tag.ID, tag.Revision)}
			out = append(out, entry{kind: entryNote, turn: turn, text: e.Text, completion: c})
		}
		return out
	}
	task, job, title, state := replayCompletionIdentity(e.Text)
	c := &workCompletion{owner: replayCompletionOwner(blocks, task, job, ""), task: task, job: job, title: title, state: state, text: e.Text}
	switch {
	case task > 0:
		c.key = fmt.Sprintf("task/%d", task)
	case job > 0:
		c.key = fmt.Sprintf("job/%d", job)
	default:
		c.key = fmt.Sprintf("receipt/%x", sha256.Sum256([]byte(e.Text)))
	}
	return []entry{{kind: entryNote, turn: turn, text: e.Text, completion: c}}
}

// Only an exact original request, proposal id or reserved process-start receipt proves ownership.
func replayCompletionOwner(blocks []entry, task uint64, job int, request string) int {
	proven, requestTurn := 0, 0
	proofFound, proofAmbiguous, requestFound, requestAmbiguous := false, false, false, false
	for i := range blocks {
		e := &blocks[i]
		proof := task > 0 && e.kind == entryTask && e.card != nil && e.card.id == task
		proof = proof || job > 0 && e.kind == entryTool && e.tool == "jobs" && strings.HasPrefix(e.detail.Output, fmt.Sprintf("job %d started; log at ", job))
		if proof {
			if proofFound && proven != e.turn {
				proofAmbiguous = true
			}
			proven, proofFound = e.turn, true
		}
		if request != "" && e.kind == entryUser && strings.TrimSpace(e.text) == strings.TrimSpace(request) {
			if requestFound && requestTurn != e.turn {
				requestAmbiguous = true
			}
			requestTurn, requestFound = e.turn, true
		}
	}
	if proofFound {
		if proofAmbiguous {
			return 0
		}
		return proven
	}
	if requestFound && !requestAmbiguous {
		return requestTurn
	}
	return 0
}

// These identities are read only after the session has proved the occurrence is a runtime receipt.
func replayCompletionIdentity(text string) (task uint64, job int, title, state string) {
	text = strings.TrimPrefix(strings.TrimSpace(text), "while you worked: ")
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		id, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || id == 0 {
			continue
		}
		switch fields[0] {
		case "task":
			task = id
			if fields[2] == "crew" {
				return task, 0, "Task " + fields[1], ""
			}
			_, rest, ok := strings.Cut(line, ": ")
			if ok {
				title, _, _ = strings.Cut(rest, " · transcript ")
			}
			state = strings.TrimSuffix(fields[2], ":")
			return task, 0, title, state
		case "job":
			if fields[2] == "exited" || fields[2] == "finished" {
				return 0, int(id), "Process " + fields[1], "finished"
			}
		}
	}
	return 0, 0, "Background work", ""
}

// Older task notes can acquire their exact source from the answer that consumed them.
func associateReplayCompletionSources(blocks []entry, tags []session.TaskReplyTag) {
	for _, tag := range tags {
		for i := len(blocks) - 1; i >= 0; i-- {
			if blocks[i].kind == entryAssistant {
				answeredBefore := false
				for _, prior := range blocks[i].replyTags {
					answeredBefore = answeredBefore || prior.ID == tag.ID
				}
				if answeredBefore {
					break
				}
			}
			c := blocks[i].completion
			if c == nil || c.task != tag.ID {
				continue
			}
			wanted := fmt.Sprintf("task/%d/%d", tag.ID, tag.Revision)
			if c.key != "task/"+strconv.FormatUint(tag.ID, 10) && c.key != wanted {
				continue
			}
			c.key = wanted
			if c.owner == 0 {
				c.owner = replayCompletionOwner(blocks[:i], tag.ID, 0, tag.Request)
			}
			if strings.TrimSpace(tag.Title) != "" {
				c.title = tag.Title
			}
		}
	}
}

// A toast can reopen a retained receipt's actual disclosure even when old history cannot prove its admission turn.
func (a *app) workCompletionReplayTurn(task uint64) (int, bool) {
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if c := e.completion; c != nil && c.task == task {
			if c.owner > 0 {
				return c.owner, true
			}
			if e.turn > 0 {
				return e.turn, true
			}
		}
	}
	return 0, false
}

// Identity finds the actual disclosure, including old background groups with a synthetic key.
func (a *app) revealCompletionIdentity(task uint64, job int) bool {
	key, latest, found := 0, -1, false
	for _, fold := range a.deckFolds(a.bodyDeck()) {
		for _, c := range fold.completions {
			matches := task != 0 && c.task == task || job != 0 && c.job == job
			if matches && fold.start >= latest {
				key, latest, found = fold.key, fold.start, true
			}
		}
	}
	if !found {
		return false
	}
	a.openWorkfold(key)
	a.touch()
	for i, row := range a.visible(a.bodyWidth()) {
		if row.hit == hitWorkFold && row.turn == key {
			a.offset = max(0, i-a.viewHeight()/3)
			a.stick = false
			return true
		}
	}
	return false
}

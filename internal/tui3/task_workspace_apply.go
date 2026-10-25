package tui3

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

type taskWorkspaceDoor interface {
	ReviewTaskWorkspaceRoot(context.Context, uint64, string) (session.TaskWorkspaceReview, error)
	ApplyTaskWorkspaceRoot(context.Context, uint64, string) (string, error)
}

// These commands enter through the person's existing ordered task command
// line. Neither a task result nor model output dispatches this door.
func (a *app) runTaskWorkspaceCommand(arg string) (tea.Cmd, bool) {
	words := strings.Fields(arg)
	if len(words) == 0 || words[0] != "review" && words[0] != "apply" {
		return nil, false
	}
	if len(words) != 3 {
		a.note("usage: /task " + words[0] + " <task number> <root slot>")
		return nil, true
	}
	id, err := strconv.ParseUint(words[1], 10, 64)
	if err != nil || id == 0 {
		a.note("name the completed task's number and root slot")
		return nil, true
	}
	door, ok := a.agent.(taskWorkspaceDoor)
	if !ok {
		a.note("this session cannot review or apply owned task roots")
		return nil, true
	}
	ctx, slot, action := a.ctx, words[2], words[0]
	return a.offLoop(func() func(bool) tea.Cmd {
		var line string
		var err error
		if action == "apply" {
			line, err = door.ApplyTaskWorkspaceRoot(ctx, id, slot)
		} else {
			var review session.TaskWorkspaceReview
			review, err = door.ReviewTaskWorkspaceRoot(ctx, id, slot)
			if err == nil {
				line = fmt.Sprintf("task %d root %s\nPrivate copy: %s\nApply destination: %s\nSnapshot: %s\n", id, slot, review.Copy, review.Destination, review.Snapshot)
				if len(review.Changed) == 0 {
					line += "No changes to apply."
				} else {
					line += review.Diff + fmt.Sprintf("\nApply this root: /task apply %d %s", id, slot)
				}
			}
		}
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			if err != nil {
				a.note("could not " + action + " task root · " + err.Error())
			} else {
				a.note(line)
			}
			a.touch()
			return nil
		}
	}), true
}

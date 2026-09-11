package tui3

import (
	tea "charm.land/bubbletea/v2"
	"strconv"
	"strings"
)

// A typed /task has no proposal card to retain its prompt. Keep its admitted
// brief with this conversation until the independently delivered node arrives.
// There is no timer: a slow host must not make the prompt disappear.
func (a *app) taskBriefConv() string { return a.convKey(a.file) }

func (a *app) adoptTypedBrief(msg taskStartedMsg) tea.Cmd {
	brief := strings.TrimSpace(msg.brief)
	id, err := strconv.ParseUint(strings.TrimSpace(msg.id), 10, 64)
	if brief == "" || msg.err != nil || err != nil || id == 0 || msg.conv != a.taskBriefConv() {
		return nil
	}
	if a.typedTaskBriefs == nil {
		a.typedTaskBriefs = make(map[uint64]string)
	}
	a.typedTaskBriefs[id] = brief
	a.takeTypedTaskBrief(a.tasks[id])
	return nil
}

// A proposal's own contract wins; an empty node adopts only its matching brief.
func (a *app) takeTypedTaskBrief(node *taskNode) {
	if node == nil {
		return
	}
	brief, ok := a.typedTaskBriefs[node.id]
	if !ok {
		return
	}
	delete(a.typedTaskBriefs, node.id)
	if strings.TrimSpace(node.brief) == "" {
		node.brief = brief
		a.roomTouched()
		a.touch()
	}
}

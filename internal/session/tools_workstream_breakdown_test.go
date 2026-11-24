package session

import (
	"strconv"
	"strings"
	"testing"
)

// AFI BREAKS BIG WORK INTO FIVE TO EIGHT DELIVERABLES, NEVER CRUMBS (OWNER-DECISIONS
// 2026-11-17 §7). A launch plan once arrived as twenty rows like "open the file",
// each its own thing to start and answer for. The rule is said where the model
// reads it twice, in the rules and on the tool that makes a task, and every
// number in it comes from the one constant.
func TestTheRulesBreakBigWorkIntoFiveToEightDeliverables(t *testing.T) {
	create := ""
	for _, tool := range WorkstreamWorkTools() {
		if tool.Name == WSTaskCreateTool {
			create = tool.Description + string(tool.InputSchema)
		}
	}
	span := strconv.Itoa(WorkstreamPlanFewest) + " to " + strconv.Itoa(WorkstreamPlanMost)
	smallest := strconv.Itoa(WorkstreamTaskSmallestMinutes) + " minutes"
	for _, said := range []struct{ where, text string }{{"the rules", WorkstreamWorkRules}, {WSTaskCreateTool, create}} {
		// A deliverable's steps are its checklist (CodeAF #2745), never tasks of their own.
		for _, rule := range []string{span, smallest, "checklist"} {
			if !strings.Contains(said.text, rule) {
				t.Errorf("%s do not say %q", said.where, rule)
			}
		}
	}
	for _, rule := range []string{"deliverable of hours to days", "never more", "one press of Start releases all of it", "never a Start per task",
		// SMALL WORK IS DONE, NOT DECLINED (owner, 2026-11-17): live, Afi answered a typo with only why it made no task,
		// and once said it had suggested the fix without calling a tool.
		"even when asked for a task: do it yourself now", "only after the tool has answered", "first offer what you can do right now", "never \"I can't\""} {
		if !strings.Contains(WorkstreamWorkRules, rule) {
			t.Errorf("the rules do not say %q", rule)
		}
	}
}

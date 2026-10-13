package main

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// checker verifies that every name a file uses exists: record types, fields,
// screens, runs, commands and kit parts.
type checker struct {
	b      *spec.Bundle
	kit    *spec.Kit
	parts  map[string]*spec.Part
	issues []string
	notes  []string
	cover  map[string]*coverage
}

func newChecker(b *spec.Bundle, kit *spec.Kit) *checker {
	c := &checker{b: b, kit: kit, parts: map[string]*spec.Part{}, cover: map[string]*coverage{}}
	for _, p := range kit.Parts {
		c.parts[p.Name] = p
	}
	for t := range b.App.Records {
		c.cover[t] = &coverage{}
	}
	return c
}

func (c *checker) run() {
	c.checkApp()
	for _, id := range c.b.ScreenIDs() {
		c.checkScreen(id, c.b.Screens[id])
	}
	for _, id := range c.b.RunIDs() {
		c.checkRun(id, c.b.Runs[id])
	}
	c.checkPlan()
	c.checkCoverage()
}

func (c *checker) fail(file, format string, args ...any) {
	c.issues = append(c.issues, fileIssue(file, format, args...))
}

func (c *checker) note(format string, args ...any) {
	c.notes = append(c.notes, fmt.Sprintf(format, args...))
}

func (c *checker) hasType(t string) bool { _, ok := c.b.App.Records[t]; return ok }

func (c *checker) hasField(t, f string) bool {
	rec, ok := c.b.App.Records[t]
	if !ok {
		return false
	}
	_, ok = rec.Fields[f]
	return ok
}

func (c *checker) checkApp() {
	app := c.b.App
	for _, t := range sortedKeys(app.Records) {
		rec := app.Records[t]
		if _, ok := rec.Fields[rec.Title]; !ok {
			c.fail("app.json", "record %s: title %q is not one of its fields", t, rec.Title)
		}
		for _, f := range sortedKeys(rec.Fields) {
			if to := rec.Fields[f].To; to != "" && !c.hasType(to) {
				c.fail("app.json", "record %s field %s: ref to unknown record type %q", t, f, to)
			}
		}
	}
	for _, t := range sortedKeys(app.Capabilities.Records) {
		if !c.hasType(t) {
			c.fail("app.json", "capabilities.records names unknown record type %q", t)
		}
	}
	c.checkTriggers()
	c.checkCommands()
	c.checkAgent()
}

func (c *checker) checkTriggers() {
	for _, tr := range c.b.App.Triggers {
		if _, ok := c.b.Runs[tr.Run]; !ok {
			c.fail("app.json", "trigger %s runs %q but runs/%s.yaml does not exist", tr.ID, tr.Run, tr.Run)
		}
		if tr.Type != "" && !c.hasType(tr.Type) {
			c.fail("app.json", "trigger %s: unknown record type %q", tr.ID, tr.Type)
		}
	}
}

func (c *checker) checkCommands() {
	for _, cmd := range c.b.App.Commands {
		if _, ok := c.b.Runs[cmd.Run]; cmd.Run != "" && !ok {
			c.fail("app.json", "command %s runs %q but runs/%s.yaml does not exist", cmd.ID, cmd.Run, cmd.Run)
		}
		if cmd.For != "" && !c.hasType(cmd.For) {
			c.fail("app.json", "command %s: unknown record type %q", cmd.ID, cmd.For)
		}
	}
}

func (c *checker) checkPlan() {
	plan := c.b.Plan
	if plan == nil {
		return
	}
	for _, l := range plan.Data {
		if !c.hasType(l.ID) {
			c.fail("plan.json", "data line %q has no record type in app.json", l.ID)
		}
	}
	for _, l := range plan.Screens {
		if _, ok := c.b.Screens[l.ID]; !ok {
			c.fail("plan.json", "screen line %q has no screens/%s.json", l.ID, l.ID)
		}
	}
	for _, l := range plan.Automation {
		if _, ok := c.b.Runs[l.ID]; !ok {
			c.fail("plan.json", "automation line %q has no runs/%s.yaml", l.ID, l.ID)
		}
	}
	if strings.TrimSpace(c.b.Agent.Body) == "" {
		c.fail("agent.md", "instructions are empty")
	}
}

// checkAgent holds the agent to what app.json grants it and to the connectors
// that are actually connected.
func (c *checker) checkAgent() {
	app := c.b.App
	for name := range app.Capabilities.Connectors {
		if !contains(connected, name) {
			c.fail("app.json", "connector %q is not connected", name)
		}
	}
	if !strings.Contains(app.Agent.Model, "/") {
		c.fail("app.json", "agent model %q is not an OpenRouter model id", app.Agent.Model)
	}
	for _, tool := range c.b.Agent.Tools {
		if !c.toolAllowed(tool) {
			c.fail("agent.md", "tool %q is not covered by app.json capabilities", tool)
		}
	}
}

func (c *checker) toolAllowed(tool string) bool {
	caps := c.b.App.Capabilities
	connector, verb, found := strings.Cut(tool, ".")
	if !found {
		return false
	}
	if connector == "records" {
		return len(caps.Records) > 0
	}
	grant, ok := caps.Connectors[connector]
	return ok && contains(grant.Can, verb)
}

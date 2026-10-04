package preflight

import (
	"fmt"
	"strings"
)

// Line is the one-line text for the chat list.
func (it Item) Line() string {
	switch {
	case it.Bucket == Impossible:
		return fmt.Sprintf("%s — not possible here", it.Reason)
	case it.Bucket == Installable:
		return fmt.Sprintf("needs %s — agent can set it up", it.label(it.Want, it.need()))
	case it.Severity == Blocking:
		return fmt.Sprintf("needs %s; %s here — cannot take over", it.label(it.Want, it.need()), major(it.Have))
	case it.Severity == Advisory && it.Name == "platform":
		return fmt.Sprintf("%s used; %s here", it.Want, it.Have)
	case it.Severity == Advisory:
		return fmt.Sprintf("%s used; %s here", it.label(it.Want, short), short(it.Have))
	}
	return fmt.Sprintf("%s here", it.label(it.Have, short))
}

// need is the version form for a requirement: major for a locked service
// family (only the major matters), major.minor otherwise.
func (it Item) need() func(string) string {
	if lockedFamily(it.Name) {
		return major
	}
	return short
}

func (it Item) label(version string, form func(string) string) string {
	name := it.Name
	if fam, ok := lockedFamilies[name]; ok {
		name = fam
	}
	if v := form(version); v != "" {
		return name + " " + v
	}
	return name
}

// Lines renders every item in order.
func (r Report) Lines() []string {
	out := make([]string, len(r.Items))
	for i, it := range r.Items {
		out[i] = it.Line()
	}
	return out
}

// SetupBrief is the instruction a setup turn gives the agent: what the chat left
// behind when it moved, one line per installable need in the words the chat list
// uses, and the limits it works within. It never names an impossible item, so
// nothing impossible is tried.
func (r Report) SetupBrief() string {
	if r.Resume.hasFacts() {
		return r.Resume.Brief() + setupLimits
	}
	var b strings.Builder
	b.WriteString("This chat needs the following on this machine so it can run here. Install each one " +
		"in user space, without root. Only this folder is writable, so put programs in ./bin " +
		"(./.venv/bin and ./node_modules/.bin work too). Check that each now runs, then stop; " +
		"do not explore the machine beyond that:\n")
	for _, it := range r.Pending() {
		b.WriteString("- " + it.Line() + "\n")
	}
	return b.String()
}

// setupLimits are the terms a setup turn works within when it brings back what a
// move left behind.
const setupLimits = "Work in user space, without root. Only this folder is writable, so put programs in ./bin " +
	"(./.venv/bin and ./node_modules/.bin work too). Check that each thing you bring back now works, then stop.\n"

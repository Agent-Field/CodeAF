// Package preflight decides, before a chat moves to another device, whether
// that device can carry it (docs/ARCHITECTURE.md sections 8.3 and 8.4).
//
// It compares the observed [inventory.Inventory] with the [Capabilities] of
// the target and sorts every need into three buckets: already PRESENT,
// INSTALLABLE by the agent in a setup turn, or IMPOSSIBLE on that machine.
// The rules live in tables (rules.go); Check only walks the inventory.
package preflight

import "github.com/Agent-Field/codeaf/internal/inventory"

// Bucket says where a need stands on the target.
type Bucket string

const (
	Present     Bucket = "present"
	Installable Bucket = "installable"
	Impossible  Bucket = "impossible"
)

// Severity says how much a finding matters to taking over.
type Severity string

const (
	None     Severity = ""
	Advisory Severity = "advisory"
	Blocking Severity = "blocking"
)

// Platform is an operating system and architecture.
type Platform = inventory.Platform

// Capabilities is what a target device offers.
type Capabilities struct {
	Platform Platform
	Tools    map[string]string // name -> version string ("" when unknown)
	GPU      bool
	Services map[string]string // name -> version string
}

// Item is one finding.
type Item struct {
	Bucket   Bucket
	Severity Severity
	Name     string
	Want     string // version the chat used
	Have     string // version on the target
	Reason   string
	Size     string // download size hint; empty until a catalog supplies it
}

// Report is every finding, in inventory order, and what the chat left behind
// that is not on this machine.
type Report struct {
	Items  []Item
	Resume Resume
}

// Idle reports that there is nothing for a setup turn to do: no tool to install
// and nothing the chat left behind.
func (r Report) Idle() bool { return len(r.Pending()) == 0 && !r.Resume.hasFacts() }

// CanTakeOver is true when nothing is impossible and nothing blocks.
func (r Report) CanTakeOver() bool {
	for _, it := range r.Items {
		if it.Bucket == Impossible || it.Severity == Blocking {
			return false
		}
	}
	return true
}

// Check compares an inventory with a target's capabilities.
func Check(inv inventory.Inventory, caps Capabilities) Report {
	var items []Item
	if it, ok := checkPlatform(inv.Platform, caps.Platform); ok {
		items = append(items, it)
	}
	for _, t := range inv.Tools {
		items = append(items, checkTool(t, caps))
	}
	for _, s := range inv.Services {
		items = append(items, checkService(s, caps))
	}
	return Report{Items: items}
}

func checkPlatform(want, have Platform) (Item, bool) {
	if want == have || want.OS == "" {
		return Item{}, false
	}
	return Item{Bucket: Present, Severity: Advisory, Name: "platform", Want: platformString(want), Have: platformString(have)}, true
}

func checkTool(t inventory.Tool, caps Capabilities) Item {
	it := Item{Name: t.Name, Want: t.VersionString}
	have, ok := caps.Tools[t.Name]
	if ok {
		it.Have = have
		return present(it, differs(t.VersionString, have))
	}
	if req, bad := impossibleTools[t.Name]; bad && !req.Met(caps) {
		it.Bucket, it.Severity, it.Reason = Impossible, Blocking, req.Reason
		return it
	}
	it.Bucket = Installable
	return it
}

func checkService(s inventory.Service, caps Capabilities) Item {
	it := Item{Name: s.Name, Want: s.Version}
	have, ok := caps.Services[s.Name]
	if !ok {
		it.Bucket = Installable
		return it
	}
	it.Have = have
	if s.Stateful() && lockedFamily(s.Name) && differsMajor(s.Version, have) {
		it.Bucket, it.Severity, it.Reason = Present, Blocking, "data directory needs the same major version"
		return it
	}
	return present(it, differs(s.Version, have))
}

func present(it Item, mismatch bool) Item {
	it.Bucket = Present
	if mismatch {
		it.Severity = Advisory
	}
	return it
}

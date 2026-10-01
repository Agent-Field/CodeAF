package cellstore

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

// heldName is a path the engine could not write when it restored a head,
// because this machine's file system keeps one name where the head has two.
type heldName struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// noteHeld keeps each held name out of every later seal and writes it into the
// record, in the take that found it. The policy block is what keeps the name out
// and the record entry is what says so to a person, so they are written together;
// an engine that gives no reason gets the general one.
func noteHeld(c cell.Cell, policyDir string, held []heldName) error {
	if len(held) == 0 {
		return nil
	}
	policy, err := readPolicyAt(policyDir)
	if err != nil {
		return err
	}
	paths := make([]string, len(held))
	entries := make([]inventory.Withheld, len(held))
	for i, h := range held {
		paths[i] = h.Path
		entries[i] = inventory.Withheld{Path: h.Path, Reason: orDefault(h.Reason, reasonHeld)}
	}
	if err := policy.holding(paths).write(policyDir); err != nil {
		return err
	}
	return inventory.Record(c.Root, func(inv *inventory.Inventory) {
		inv.SetWithheld(func(path string) bool { return inRules(paths, path) }, entries)
	})
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

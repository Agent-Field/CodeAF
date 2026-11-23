package taskcopy

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func restorationOrder(names []string, parents map[string]string) ([]string, error) {
	available := make(map[string]bool, len(names))
	for _, name := range names {
		available[name] = true
	}
	for child, parent := range parents {
		if !localTreeID(child) || !localTreeID(parent) || !available[child] || !available[parent] {
			return nil, fmt.Errorf("task parent binding %q -> %q is outside the carried copies", child, parent)
		}
	}
	var order []string
	state := make(map[string]uint8, len(names))
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 2 {
			return nil
		}
		if state[name] == 1 {
			return errors.New("carried task parents contain a cycle")
		}
		state[name] = 1
		if parent := parents[name]; parent != "" {
			if err := visit(parent); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, name)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func localTreeID(name string) bool {
	return name != "." && filepath.IsLocal(name) && filepath.Base(name) == name
}

// ValidateParentBindings checks the whole carried graph before any copy is
// written. Bindings name IDs, never directories outside the receiving cell.
func ValidateParentBindings(c cell.Cell, parents map[string]string) error {
	_, err := restorationOrder(Carried(c), parents)
	return err
}

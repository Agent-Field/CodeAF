package serve

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
)

// AllWorkAvailable keeps cross-workstream reading exclusive to Afi in My space.
func (d *agentDoor) AllWorkAvailable() bool { return d.allWork }

// AllWork reads through the person's current access, never the operator's disk.
// Writes retain the original door's id, so this scope cannot widen a write.
func (d *agentDoor) AllWork(ctx context.Context) (string, error) {
	if !d.allWork {
		return "", errors.New("all work is available only from Afi in My space")
	}
	if _, err := d.Workstream(ctx); err != nil {
		return "", err
	}
	all, err := d.h.config.Store.Workstreams()
	if err != nil {
		return "", err
	}
	v := d.viewer()
	ids := make([]string, 0, len(all))
	for id, ws := range all {
		if !ws.Space && v.canSee(ws) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	work := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		ws := all[id]
		door := &agentDoor{h: d.h, id: id, asker: d.asker, agent: d.agentID(), operator: d.operator}
		tasks, err := door.tasks(ctx)
		if err != nil {
			return "", err
		}
		rows := make([]map[string]any, 0, len(tasks))
		for _, task := range tasks {
			rows = append(rows, taskBrief(task))
		}
		work = append(work, map[string]any{"id": id, "name": ws.Name, "purpose": ws.Purpose, "tasks": rows})
	}
	raw, err := json.Marshal(map[string]any{"looking_at": "All my work", "workstreams": work})
	return string(raw), err
}

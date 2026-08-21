package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// The model a job runs on is settled at splice time and journaled there, and a
// restart inherits it. That covers everything except the case a person actually
// hits: the job is running, it is going badly or going too expensively, and the
// answer is neither to kill it nor to wait — it is to change what does the rest
// of it. Until now there was no verb for that at all.
//
// This is worker.go's exception a second time, and for the same reason: the row
// is a view of the journal, so a sweep that changed rows alone would be undone
// by the next rebuild and the leaf would quietly go back to the model nobody
// wanted. One event per node, replayed by id, is what makes the change as
// durable as the pin it replaces.
//
// Nothing here interrupts anything. A leaf already claimed is already holding
// the client it was handed; the pin is read out of the row when a leaf is
// claimed, so re-pointing the row is exactly a change that takes effect at each
// node's next provider call and at no other moment.

// nodeModelPayload is the durable record of one node's model moving: what it
// runs on now, what it ran on before, and why it moved.
//
// RunModel is set only where the node already carried one. It is "the model the
// work ran on", recorded when the plan slot split from the work slot, and a
// receipt that names a model the remaining work is no longer going to touch is
// a receipt that lies — the same reasoning that moves it on a restart.
// PlanModel is never touched: who structured the job is history, and history
// does not move because the hands changed.
type nodeModelPayload struct {
	WorkModel string `json:"work_model"`
	RunModel  string `json:"run_model,omitempty"`
	Previous  string `json:"previous,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ModelRebinding is what one subtree rebinding actually moved. Running is
// counted separately because it is the half the person has to be told about:
// those steps finish where they started, and a receipt that implies otherwise
// promises an immediacy the design deliberately refuses.
type ModelRebinding struct {
	Root    string
	Model   string
	Nodes   []string
	Running int
}

// SetSubtreeWorkModel re-points every live node at or beneath root onto model.
//
// Live means exactly what the funnel means by it — unfolded and not settled.
// Work that is already finished keeps the model it was done on, because what
// ran is a fact and not a preference; work that has not started yet is a
// preference and nothing else. A node already pinned to this model is left
// alone rather than journaled again, so asking twice writes once.
//
// The store carries a name and no opinion about which names exist, exactly as
// it does for workers: whether a slug reaches a reachable model is the client
// pool's question, one layer up, at dispatch.
func (s *Store) SetSubtreeWorkModel(root, model, reason string) (ModelRebinding, error) {
	root = strings.TrimSpace(root)
	model = strings.TrimSpace(model)
	if root == "" || model == "" {
		return ModelRebinding{}, fmt.Errorf("set subtree model: %w: root and model are required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return ModelRebinding{}, fmt.Errorf("set subtree model: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, root); err != nil {
		return ModelRebinding{}, fmt.Errorf("set subtree model: %w", err)
	}
	live, err := liveSubtreeModels(tx, root)
	if err != nil {
		return ModelRebinding{}, fmt.Errorf("set subtree model: %w", err)
	}
	rebinding := ModelRebinding{Root: root, Model: model}
	for _, node := range live {
		payload := nodeModelPayload{WorkModel: model, Previous: node.workModel, Reason: strings.TrimSpace(reason)}
		if node.runModel != "" {
			payload.RunModel = model
		}
		if node.workModel == model && node.runModel == payload.RunModel {
			continue
		}
		seq, _, err := appendEvent(tx, node.id, EventNodeModelChanged, payload)
		if err != nil {
			return ModelRebinding{}, fmt.Errorf("set subtree model: %w", err)
		}
		if err := applyNodeModelView(tx, node.id, payload, seq); err != nil {
			return ModelRebinding{}, fmt.Errorf("set subtree model: %w", err)
		}
		rebinding.Nodes = append(rebinding.Nodes, node.id)
		if node.status == Claimed || node.status == Running {
			rebinding.Running++
		}
	}
	if len(rebinding.Nodes) == 0 {
		return rebinding, nil
	}
	if err := tx.Commit(); err != nil {
		return ModelRebinding{}, fmt.Errorf("set subtree model: %w", err)
	}
	return rebinding, nil
}

type subtreeModel struct {
	id        string
	status    Status
	workModel string
	runModel  string
}

// liveSubtreeModels reads the rows one rebinding may touch: one recursive CTE
// down the parent index, then one primary-key lookup per node in it. The CROSS
// JOIN is the same load-bearing word it is in subtreeSpend — it fixes the join
// order so the cost is the subtree's own rows, and the read is bounded by the
// task rather than by the graph.
func liveSubtreeModels(tx *sql.Tx, root string) ([]subtreeModel, error) {
	rows, err := tx.Query(`
		WITH RECURSIVE descendants(id) AS (
		    SELECT id FROM nodes WHERE id = ?
		    UNION ALL
		    SELECT child.id FROM nodes AS child
		    JOIN descendants ON child.parent_id = descendants.id
		)
		SELECT nodes.id, nodes.status, nodes.work_model, nodes.run_model
		FROM descendants CROSS JOIN nodes ON nodes.id = descendants.id
		WHERE nodes.folded = 0 AND nodes.status IN (?, ?, ?)
		ORDER BY nodes.id`, root, Pending, Claimed, Running)
	if err != nil {
		return nil, fmt.Errorf("read live subtree %q: %w", root, err)
	}
	defer rows.Close()
	live := make([]subtreeModel, 0, 8)
	for rows.Next() {
		var node subtreeModel
		if err := rows.Scan(&node.id, &node.status, &node.workModel, &node.runModel); err != nil {
			return nil, fmt.Errorf("read live subtree %q: %w", root, err)
		}
		live = append(live, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read live subtree %q: %w", root, err)
	}
	return live, nil
}

func applyNodeModelView(tx *sql.Tx, id string, payload nodeModelPayload, seq int64) error {
	if payload.RunModel != "" {
		return replayUpdate(tx, id,
			`UPDATE nodes SET work_model = ?, run_model = ?, updated_seq = ? WHERE id = ?`,
			payload.WorkModel, payload.RunModel, seq, id)
	}
	return replayUpdate(tx, id, `UPDATE nodes SET work_model = ?, updated_seq = ? WHERE id = ?`,
		payload.WorkModel, seq, id)
}

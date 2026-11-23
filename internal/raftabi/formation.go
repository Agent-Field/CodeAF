package raftabi

import (
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func (r *Runtime) formation(d *decoder) ([]byte, error) {
	node := r.nodes[d.u32()]
	if node == nil {
		return nil, errFrame
	}
	var manifest cs.LegacyManifest
	raw, birth, registry := d.blob(), d.blob(), d.blob()
	if d.end() != nil || cs.Decode(raw, &manifest) != nil || !validSingleton(manifest, birth) || len(registry) > 0 {
		return nil, errFrame
	}
	plan, err := node.PlanFormation(manifest.Config.Anchors)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(plan.Change)
	e.flag(plan.Checkpoint)
	e.flag(plan.Settle)
	e.flag(plan.Configure)
	return e.b, nil
}

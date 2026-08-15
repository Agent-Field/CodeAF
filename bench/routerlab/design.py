#!/usr/bin/env python3
"""The connected incomplete block design.

The naive experiment is the full factorial: every model answers every task.
That is 7 x 36 = 252 cells, plus 70 more for a replicate, and most of those
cells buy almost nothing -- once four models have failed a task, the fifth
failure adds very little to the estimate of how hard it is.

An IRT model does not need a rectangular matrix. It needs the model-task
bipartite graph to be CONNECTED: if the graph splits into two components,
ability in one component is unidentifiable relative to the other and the two
halves of the scale can slide past each other freely. So the design is built
in two blocks:

  ANCHOR (10 tasks x 7 models x 2 replicates = 140 cells)
      Ten tasks spanning the whole intended difficulty range and all three
      work-classes, answered by every model. This block alone makes the graph
      connected and pins the scale; everything else hangs off it. The two
      replicates at temperature 0.2 cost 70 extra cells and are what let the
      report say how much of the outcome matrix is residual stochasticity
      rather than ability.

  SPOKE (26 tasks x 3 models x 1 replicate = 78 cells)
      Every remaining task is seen by exactly three models, chosen by a cyclic
      allocation with offsets {0, 1, 3}. Those offsets are a perfect difference
      set mod 7, so as the cycle turns every PAIR of models co-occurs on
      roughly the same number of tasks -- the design is pairwise balanced, not
      merely connected, which is what keeps the relative ability estimates from
      leaning on any one shared task.

  Total 218 cells against 322 for the naive design: a 32% cut, and the cut
  falls entirely on the cells that were carrying the least information.

The spoke tasks are ordered by (level, class) before the cycle is applied, so
each model's spoke workload is spread across the difficulty range rather than
concentrated at one end -- otherwise a model's ability estimate would be
confounded with the difficulty of the tasks it happened to draw.
"""
from tasks import TASKS, BY_ID, BY_ID_R1

# Ten anchors: three plan, four code, three reason, intended levels
# 1,1,1,2,3,3,3,4,4,5 -- both ends of the range are represented, because an
# anchor set that is all mid-difficulty pins the middle of the scale and lets
# the ends drift.
ANCHOR_IDS = ["P02", "P07", "P09",
              "C02", "C05", "C08", "C12",
              "R01", "R08", "R10"]

ANCHOR_REPLICATES = 2
SPOKE_MODELS_PER_TASK = 3
SPOKE_OFFSETS = (0, 1, 3)  # perfect difference set mod 7


def build_cells(panel):
    """Return [(model_slug, task_id, replicate)] for the whole design."""
    slugs = [m["slug"] for m in panel]
    n = len(slugs)
    assert n == 7, f"the {{0,1,3}} difference set assumes 7 models, got {n}"

    cells = []
    for tid in ANCHOR_IDS:
        assert tid in BY_ID, tid
        for rep in range(ANCHOR_REPLICATES):
            for s in slugs:
                cells.append((s, tid, rep))

    # The sort key is the ROUND-1 intended level, deliberately, even when the
    # round-2 task is what will be run. The design is a fixed allocation of
    # models to tasks; if the key moved when a task was re-levelled during the
    # difficulty recalibration, every spoke downstream of it would be handed to
    # a different model and the round-1 results for untouched tasks would no
    # longer belong to the same design. Pinning the key keeps the two rounds
    # mergeable and the per-model workload balanced.
    spokes = [t for t in TASKS if t["id"] not in set(ANCHOR_IDS)]
    spokes.sort(key=lambda t: (BY_ID_R1[t["id"]]["level"], t["cls"], t["id"]))
    for i, t in enumerate(spokes):
        for off in SPOKE_OFFSETS:
            cells.append((slugs[(i + off) % n], t["id"], 0))
    return cells


def summarise(panel, cells):
    from collections import Counter
    per_model = Counter(c[0] for c in cells)
    per_task = Counter(c[1] for c in cells)
    pairs = {}
    by_task = {}
    for m, t, r in cells:
        by_task.setdefault(t, set()).add(m)
    for t, ms in by_task.items():
        ms = sorted(ms)
        for i in range(len(ms)):
            for j in range(i + 1, len(ms)):
                pairs[(ms[i], ms[j])] = pairs.get((ms[i], ms[j]), 0) + 1
    n = len(panel)
    return {
        "cells": len(cells),
        "naive_full_factorial": len(BY_ID) * n + len(ANCHOR_IDS) * n,
        "min_models_per_task": min(len(v) for v in by_task.values()),
        "tasks_covered": len(by_task),
        "cells_per_model": dict(per_model),
        "cells_per_task_min": min(per_task.values()),
        "cells_per_task_max": max(per_task.values()),
        "model_pairs_covered": len(pairs),
        "model_pairs_possible": n * (n - 1) // 2,
        "min_pair_cooccurrence": min(pairs.values()) if pairs else 0,
    }


def connected(cells):
    """Union-find over the model-task bipartite graph. If this is False the
    IRT scale is not identified and the whole analysis is meaningless."""
    parent = {}

    def find(x):
        parent.setdefault(x, x)
        while parent[x] != x:
            parent[x] = parent[parent[x]]
            x = parent[x]
        return x

    def union(a, b):
        ra, rb = find(a), find(b)
        if ra != rb:
            parent[ra] = rb

    for m, t, _ in cells:
        union(("m", m), ("t", t))
    roots = {find(k) for k in parent}
    return len(roots) == 1


if __name__ == "__main__":
    import json
    panel = json.load(open("panel.json"))["panel"]
    cells = build_cells(panel)
    s = summarise(panel, cells)
    print(json.dumps(s, indent=2))
    print("connected:", connected(cells))
    cut = 1 - s["cells"] / s["naive_full_factorial"]
    print(f"cut vs naive: {cut:.1%}")

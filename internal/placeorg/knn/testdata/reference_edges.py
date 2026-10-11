"""Export only the harness W edge keys, using saved embeddings and no model calls."""
import json
import pathlib
import sys

import numpy as np

ref = pathlib.Path(sys.argv[1])
x = np.load(ref / "emb_all.npy")
ids = json.loads((ref / "emb_row_ids.json").read_text())
assert x.ndim == 2 and len(x) == len(ids) and len(x) > 8
sims = x @ x.T
np.fill_diagonal(sims, -1)
w = {}
for i in range(len(x)):
    for j in np.argpartition(-sims[i], 8)[:8]:
        if sims[i, j] < 0.35:
            continue
        key = (min(i, int(j)), max(i, int(j)))
        # The bounded derived anchor boost replaces the harness boost after
        # selection, so neither boost changes these reference edge keys.
        w[key] = max(float(sims[i, j]), w.get(key, 0))
pathlib.Path(sys.argv[2]).write_text(json.dumps(sorted(w)))
print(f"Saved reference: {len(x)} rows, {len(w)} edges")

"""Write private parity input to stdout; redirect it outside the repository.

Usage: python3 reference_candidates.py "$PLACEORG_REF" > /tmp/name-reference.json
The saved embedding row order and assignments fix the corpus and memberships.
Only the reference's scoring functions are evaluated; no model is called.
"""
import ast
import collections
import json
import pathlib
import re
import sys

import numpy as np

ref = pathlib.Path(sys.argv[1])
ids = json.loads((ref / "emb_row_ids.json").read_text())
assert np.load(ref / "emb_all.npy").shape[0] == len(ids)
assignments = json.loads((ref / "assignments.json").read_text())["assignments"]
records = {}
for path in [ref.parent.parent / "corpus-cc.jsonl", ref.parent.parent / "corpus.jsonl"]:
    for line in path.read_text().splitlines():
        c = json.loads(line)
        records[c["id"]] = c
chats = []
for id in ids:
    c = records[id]
    text = f"{c.get('title', '')}\n{(c.get('first_user') or c.get('first_msg') or '')[:400]}".strip()
    chats.append({"ID": id, "Title": c.get("title", ""), "Text": text})

# Extract the actual reference functions so parity cannot silently redefine their scoring.
module = ast.parse((ref / "places_harness.py").read_text())
selected = [n for n in module.body if isinstance(n, ast.FunctionDef) and n.name in {"tokens", "ctfidf"}]
stop = next(n for n in module.body if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "STOP" for t in n.targets))
namespace = {"np": np, "re": re, "n": len(chats)}
exec(compile(ast.Module(body=[stop] + selected, type_ignores=[]), "reference", "exec"), namespace)
namespace["docs"] = [namespace["tokens"](c["Text"]) for c in chats]
namespace["df"] = dict(collections.Counter(w for doc in namespace["docs"] for w in set(doc)))

nodes, candidates, answers = [], {}, []
for root in sorted({a["root"] for a in assignments.values() if a["live"]}):
    root_rows = [i for i, id in enumerate(ids) if assignments[id]["live"] and assignments[id]["root"] == root]
    groups = [(f"r{root}", "", root_rows, "root_name")]
    for mid in sorted({assignments[ids[i]]["mid"] for i in root_rows if assignments[ids[i]]["mid"] >= 0}):
        mid_rows = [i for i in root_rows if assignments[ids[i]]["mid"] == mid]
        groups.append((f"m{mid}", f"r{root}", mid_rows, "mid_name"))
        for leaf in sorted({assignments[ids[i]]["leaf"] for i in mid_rows if assignments[ids[i]]["leaf"] >= 0}):
            rows = [i for i in mid_rows if assignments[ids[i]]["leaf"] == leaf]
            if len(rows) >= 4:
                groups.append((f"l{leaf}", f"m{mid}", rows, "leaf_name"))
    for key, parent, rows, field in groups:
        nodes.append({"ID": key, "Parent": parent, "Members": [ids[i] for i in rows]})
        candidates[key] = namespace["ctfidf"](rows)
        if not parent:
            answers.append("Member subjects")
        answer = assignments[ids[rows[0]]][field].split("/")[-1]
        # The naming pass preceded two review renames in the saved run.
        if not parent:
            renames = json.loads((ref / "runs/run1/places_results.json").read_text()).get("review", {}).get("applied_renames", {})
            answer = next((old for old, new in renames.items() if new == answer), answer)
        answers.append(answer)
json.dump({"Corpus": {"Chats": chats}, "Nodes": nodes, "Candidates": candidates, "Answers": answers}, sys.stdout)

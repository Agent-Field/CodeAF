#!/usr/bin/env python3
"""How often does our judge agree with DeepSource's, on the same findings?

    calibrate.py <results-dir-of-a-replay-run>

A replay run (tools/replay.sh) hands our judge the findings DeepSource's judge
saw for one of their tools — their processed rows — so the only variable is the
judge. This reads our verdicts beside theirs and prints agreement, the
confusion between the two, and every disagreement with both reasonings, so a
reader can decide which judge was right rather than take either on faith.

The comparison table is only worth quoting against if this agreement is high;
README.md records the measured figure."""
import json
import os
import sys


def main():
    root = sys.argv[1].rstrip("/")
    meta = json.load(open(os.path.join(root, "meta.json")))
    tool = meta.get("replay_tool")
    if not tool:
        print("not a replay run (meta.json has no replay_tool)", file=sys.stderr); sys.exit(2)
    work = meta.get("work") or os.environ.get("WORK") or os.path.join(os.path.dirname(os.path.dirname(root)), "work")
    theirs = {}
    for line in open(os.path.join(work, "deepsource", "judged", tool + ".jsonl")):
        r = json.loads(line)
        theirs[(r["cve_id"], r["variant"])] = r
    agree = n = 0
    conf = {"both_hit": 0, "both_miss": 0, "ours_only": 0, "theirs_only": 0}
    disagreements = []
    for cve in sorted(os.listdir(root)):
        for variant in ("unfixed", "fixed"):
            jp = os.path.join(root, cve, variant, "judge.json")
            if not os.path.exists(jp) or (cve, variant) not in theirs:
                continue
            ours = json.load(open(jp))
            t = theirs[(cve, variant)]
            their_hit = bool(t["TP"] or t["FP"])
            our_hit = bool(ours["hit"])
            n += 1
            if our_hit == their_hit:
                agree += 1
                conf["both_hit" if our_hit else "both_miss"] += 1
            else:
                conf["ours_only" if our_hit else "theirs_only"] += 1
                disagreements.append((cve, variant, our_hit, ours.get("reasoning", ""), t.get("judge_reasoning", "")))
    pct = 100.0 * agree / n if n else 0.0
    print("replay of %s: %d cells compared, %d agree (%.1f%%)" % (tool, n, agree, pct))
    print("  both hit %d · both miss %d · ours only %d · theirs only %d" % (
        conf["both_hit"], conf["both_miss"], conf["ours_only"], conf["theirs_only"]))
    for cve, variant, our_hit, ours_why, theirs_why in disagreements:
        print("\n%s/%s — ours: %s, theirs: %s" % (cve, variant, "hit" if our_hit else "miss", "miss" if our_hit else "hit"))
        print("  ours:   " + ours_why.replace("\n", " ")[:400])
        print("  theirs: " + theirs_why.replace("\n", " ")[:400])
    json.dump({"tool": tool, "compared": n, "agree": agree, "agreement_pct": round(pct, 1),
               "confusion": conf,
               "disagreements": [{"cve": c, "variant": v, "ours_hit": o, "ours": a, "theirs": b}
                                 for c, v, o, a, b in disagreements]},
              open(os.path.join(root, "calibration.json"), "w"), indent=2)


if __name__ == "__main__":
    main()

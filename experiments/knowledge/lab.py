#!/usr/bin/env python3
"""Run private knowledge experiments alongside the open source binary."""
import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import statistics

from corpus import read_messages
from knowledge import build, recall, update_once, organize_hierarchy
from provider import MODEL, OpenRouter
from scheduler import simulate
from storage import save, viewer, write_json
from editor import compose_notes, review_articles


def metrics(vault):
    claims = vault["claims"]
    active = {c["id"] for c in claims if c["status"] == "active"}
    sizes = [len(active & set(n["claims"])) for n in vault["notes"]]
    neighbors = {n["id"]: set() for n in vault["notes"]}
    for note in vault["notes"]:
        for link in note["links"]:
            if link["id"] in neighbors:
                neighbors[note["id"]].add(link["id"])
                neighbors[link["id"]].add(note["id"])
    unseen, components = set(neighbors), []
    while unseen:
        todo, count = [unseen.pop()], 0
        while todo:
            node = todo.pop()
            count += 1
            fresh = neighbors[node] & unseen
            unseen -= fresh
            todo.extend(fresh)
        components.append(count)
    return {"notes": len(vault["notes"]), "current_claims": len(active), "all_claims": len(claims),
            "states": dict(Counter(c["status"] for c in claims)),
            "kinds": dict(Counter(c["kind"] for c in claims if c["id"] in active)),
            "links": sum(len(n["links"]) for n in vault["notes"]),
            "notes_without_outgoing_links": sum(not n["links"] for n in vault["notes"]),
            "single_claim_notes": sum(s == 1 for s in sizes),
            "folder_roots": len({n["folder"][0] for n in vault["notes"] if n["folder"]}),
            "folder_paths": len({tuple(n["folder"]) for n in vault["notes"]}),
            "isolated_notes": sum(not adjacent for adjacent in neighbors.values()),
            "component_sizes": sorted(components, reverse=True),
            "rejected_claims": len(vault["rejected"]),
            "quotation_checks_pass": all(e["quote"] in vault["sources"][e["source"]]["text"]
                                         for c in claims for e in c["evidence"]),
            "caveat": "Exact quotation checks prove source presence, not semantic entailment or completeness."}


def audit(vault, model):
    """Separate verifier calls can quarantine misleading claims while retaining their history."""
    batches = [vault["claims"][i:i+32] for i in range(0, len(vault["claims"]), 32)]
    def check(batch):
        return model("judge", {"claims": [{**c, "evidence": [
            {**e, "role": vault["sources"][e["source"]]["role"],
             "ts": vault["sources"][e["source"]]["ts"],
             "context": vault["sources"][e["source"]]["text"]} for e in c["evidence"]]} for c in batch]})
    with ThreadPoolExecutor(max_workers=3) as pool:
        results = list(pool.map(check, batches))
    known = {c["id"]: c for c in vault["claims"]}
    checks = [c for result in results for c in result.get("checks", []) if c.get("id") in known]
    checked = set()
    for check in checks:
        claim = known[check["id"]]
        checked.add(claim["id"])
        claim["audit"] = check
        if check.get("supported") is not True:
            claim["status"] = "quarantined"
            claim["unsure"] = True
    for claim_id in set(known) - checked:
        known[claim_id]["status"] = "quarantined"
        known[claim_id]["audit"] = {"supported": False, "reason": "Verifier did not return a judgment"}
    return {"checks": len(checks), "supported": sum(c.get("supported") is True for c in checks),
            "quarantined": sum(c["status"] == "quarantined" for c in vault["claims"]),
            "not_checked": len(set(known) - checked), "results": results,
            "limitation": "Same-model semantic audit, not independent human ground truth."}


def cost_report(directory, messages):
    ledger = directory / "calls.jsonl"
    rows = [json.loads(l) for l in ledger.read_text().splitlines()] if ledger.exists() else []
    roles = {}
    for role in sorted({r["role"] for r in rows}):
        group = [r for r in rows if r["role"] == role]
        roles[role] = {"calls": len(group),
                       "input_tokens": sum(r["usage"].get("prompt_tokens", 0) for r in group),
                       "output_tokens_including_reasoning": sum(r["usage"].get("completion_tokens", 0) for r in group),
                       "usd": round(sum(r["usage"].get("cost", 0) or 0 for r in group), 6),
                       "median_seconds": round(statistics.median(r["seconds"] for r in group), 2)}
    extraction = roles.get("extract", {})
    turns = sum(m["role"] == "user" for m in messages)
    unit = extraction.get("usd", 0) / max(1, turns)
    failures = [json.loads(p.read_text()) for p in directory.glob("*.failure.json")]
    failed_usd = sum(f.get("usage", {}).get("cost", 0) or 0 for f in failures)
    successful_usd = sum(x["usd"] for x in roles.values())
    return {"model": MODEL, "recorded_models": sorted({r["model"] for r in rows}), "roles": roles,
            "recorded_successful_call_usd": round(successful_usd, 6),
            "known_failed_response_usd": round(failed_usd, 6),
            "accounted_minimum_usd": round(successful_usd + failed_usd, 6),
            "billing_limitation": "Early failed attempts and errors without usage may be billable but are not measurable from this record. This is a lower bound, not a billing reconciliation.",
            "failed_calls_included": False,
            "projection_assumptions": "Extraction cost divided by eligible human messages in this replay. Excludes failed attempts, reconciliation, auditing, recall, storage and hosting; historical batching differs from live scheduling. Not a quote or production forecast.",
            "extraction_only_scenarios": [{"new_human_turns_per_day": n, "daily_usd": round(unit*n, 4),
                                            "monthly_30_days_usd": round(unit*n*30, 3)} for n in ((100, 1000, 10000) if extraction and turns else ())]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    replay = sub.add_parser("replay", help="Read a snapshot and compile direct and reconciled experiments")
    replay.add_argument("--database", required=True, type=Path)
    replay.add_argument("--out", required=True, type=Path)
    replay.add_argument("--sessions", type=int, default=0, help="Latest eligible sessions; zero means all")
    replay.add_argument("--workers", type=int, default=3)
    replay.add_argument("--audit", action="store_true", help="Run a separate semantic verifier over every claim")
    replay.add_argument("--hierarchy", action="store_true", help="Review folder vocabulary and cross-topic links")
    replay.add_argument("--articles", action="store_true", help="Write and verify subject articles with provenance in citations")
    ask = sub.add_parser("ask", help="Answer from current sourced claims")
    ask.add_argument("--out", required=True, type=Path)
    ask.add_argument("--scope")
    ask.add_argument("--retrieval", choices=("lexical", "graph", "coverage"), default="coverage")
    ask.add_argument("question")
    update = sub.add_parser("update", help="Compile new messages from several chats in one normal completion")
    update.add_argument("--messages", required=True, type=Path, help="JSON array of sourced messages")
    update.add_argument("--out", required=True, type=Path)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True, mode=0o700)
    model = OpenRouter(args.out / "calls")
    if args.command == "update":
        path = args.out / "reconciled/vault.json"
        previous = json.loads(path.read_text()) if path.exists() else None
        vault = update_once(json.loads(args.messages.read_text()), previous, model)
        save(vault, args.out / "reconciled")
        print(json.dumps(vault.get("update_receipt", {"new_sources": 0, "provider_calls": 0}), indent=2))
        return
    if args.command == "ask":
        vault = json.loads((args.out / "reconciled/vault.json").read_text())
        print(json.dumps(recall(vault, args.question, model, args.scope, retrieval=args.retrieval), indent=2))
        return
    messages, inventory = read_messages(args.database, args.sessions)
    write_json(args.out / "corpus.json", {"messages": messages, "inventory": inventory})
    print(json.dumps(inventory), flush=True)
    direct = build(messages, model, workers=args.workers)
    direct["inventory"] = inventory
    save(direct, args.out / "direct")
    # Reuse exactly the same extracted claims; compare organization, not two different samples.
    reconciled = build([], model, strategy="reconciled", previous=direct)
    reconciled["inventory"] = inventory
    audit_result = audit(reconciled, model) if args.audit else {"limitation": "Semantic audit not requested"}
    write_json(args.out / "audit.json", audit_result)
    if args.hierarchy:
        reconciled = organize_hierarchy(reconciled, model)
    if args.articles:
        reconciled = review_articles(compose_notes(reconciled, model), model)
    save(reconciled, args.out / "reconciled")
    schedules = {name: simulate(messages, quiet, wait) for name, quiet, wait in [
        ("per_message", 0, 0), ("quiet_30s", 30, 120), ("quiet_120s", 120, 300)]}
    write_json(args.out / "scheduling.json", schedules)
    comparison = {"direct": metrics(direct), "reconciled": metrics(reconciled),
                  "audit": {k: v for k, v in audit_result.items() if k != "results"},
                  "scheduling": {n: {k: v for k, v in s.items() if k != "batches"} for n, s in schedules.items()},
                  "cost": cost_report(args.out / "calls", messages)}
    write_json(args.out / "comparison.json", comparison)
    research_path = args.out / "designer-research.json"
    research = json.loads(research_path.read_text()) if research_path.exists() else {}
    viewer({"direct": direct, "reconciled": reconciled}, comparison, research, args.out / "viewer")
    print(json.dumps({"comparison": comparison, "viewer": str(args.out / "viewer/index.html")}, indent=2))


if __name__ == "__main__":
    main()

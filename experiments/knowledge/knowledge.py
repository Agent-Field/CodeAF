"""Experimental conversation-to-vault compiler, independent of the live memory store."""
import hashlib
import json
import re
import math
import copy
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone


def identity(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()[:16]


def instant(value):
    value = datetime.fromisoformat(value.replace("Z", "+00:00"))
    return value.replace(tzinfo=timezone.utc) if value.tzinfo is None else value.astimezone(timezone.utc)


def organize_hierarchy(vault, model):
    """Repair the human navigation projection without rewriting claims or source identities."""
    vault = copy.deepcopy(vault)
    claims = {c["id"]: c for c in vault["claims"]}
    catalog = [{**n, "gist": [claims[i]["text"] for i in n["claims"] if i in claims and
                             claims[i]["status"] == "active"][:3]} for n in vault["notes"]]
    result = model("hierarchy", {"notes": catalog})
    notes = {n["id"]: n for n in vault["notes"]}
    removed = {(r.get("from"), r.get("to")) for r in result.get("bad_links", [])}
    for note in notes.values():
        note["links"] = [l for l in note["links"] if (note["id"], l["id"]) not in removed]
    for assignment in result.get("assignments", []):
        folder = assignment.get("folder")
        if assignment.get("id") in notes and isinstance(folder, list) and 1 <= len(folder) <= 2 and all(
                isinstance(f, str) and f.strip() and len(f) <= 100 for f in folder):
            notes[assignment["id"]]["folder"] = folder
    for link in result.get("links", []):
        source, target = link.get("from"), link.get("to")
        if source in notes and target in notes and source != target and link.get("reason"):
            if not any(l["id"] == target for l in notes[source]["links"]):
                notes[source]["links"].append({"id": target, "reason": str(link["reason"])})
    vault["hierarchy_review"] = result
    return vault


def update_once(messages, previous, model):
    """One normal completion for a mixed-chat context, followed by deterministic validation."""
    previous = copy.deepcopy(previous) if previous else None
    known_sources = previous["sources"] if previous else {}
    fresh = [m for m in messages if m["id"] not in known_sources]
    if not fresh:
        for message in messages:
            if known_sources[message["id"]] != message:
                raise ValueError("Source changed under an existing immutable ID")
        return previous or {"schema": 1, "strategy": "reconciled", "sources": {}, "claims": [],
                            "notes": [], "relations": [], "rejected": [], "scope_aliases": []}
    if sum(len(m["text"]) for m in fresh) > 24000:
        raise ValueError("Update context exceeds 24,000 characters; split the pending updates")
    terms = set(re.findall(r"\w+", " ".join(m["text"] for m in fresh).casefold()))
    old_claims = previous["claims"] if previous else []
    ranked = sorted(old_claims, key=lambda c: (-len(terms & set(re.findall(r"\w+", c["text"].casefold()))), c["id"]))
    candidates = ranked[:48]
    candidate_ids = {c["id"] for c in candidates}
    old_notes = previous["notes"] if previous else []
    affected = [n for n in old_notes if candidate_ids & set(n["claims"])][:12]
    result = model("update", {"messages": fresh, "existing_claims": candidates, "existing_notes": affected,
                              "aliases": previous.get("scope_aliases", []) if previous else []})
    additions = result.get("claims", [])
    references = {"new:" + str(c.get("local_id")): identity([c.get("text"), c.get("evidence", [])])
                  for c in additions}
    def resolve(ref):
        return references.get(ref, ref)
    proposed_notes = []
    for note in result.get("notes", []):
        note = dict(note)
        note["claims"] = [resolve(ref) for ref in note.get("claims", [])]
        proposed_notes.append(note)
    patched = {(n["scope"], n["title"]) for n in proposed_notes if n.get("scope") and n.get("title")}
    notes = proposed_notes + [n for n in old_notes if (n["scope"], n["title"]) not in patched]
    relations = [{**r, "from": resolve(r.get("from")), "to": resolve(r.get("to"))}
                 for r in result.get("relations", [])]
    def response(role, payload):
        if role == "extract":
            return {"claims": additions}
        if role == "scope_aliases":
            return {"aliases": result.get("aliases", [])}
        scope = payload["claims"][0]["scope"]
        return {"notes": [n for n in notes if n.get("scope") == scope], "relations": relations,
                "_native_references": True}
    vault = build(messages, response, strategy="reconciled", previous=previous, workers=1)
    vault["update_receipt"] = {"new_sources": len(fresh), "candidate_claims": len(candidates),
                               "candidate_notes": len(affected), "provider_calls": 1,
                               "semantic_audit": "not run; new claims have only structural/quote validation"}
    return vault


def build(messages, model, strategy="direct", previous=None, workers=4):
    """Compile sourced claims and note projections without changing the source log."""
    if strategy not in ("direct", "reconciled"):
        raise ValueError("Unknown strategy")
    previous = copy.deepcopy(previous) if previous else None
    sources = dict(previous["sources"]) if previous else {}
    fresh = []
    for message in messages:
        if message["id"] in sources:
            if sources[message["id"]] != message:
                raise ValueError("Source changed under an existing immutable ID")
        else:
            sources[message["id"]] = message
            fresh.append(message)
    if previous and not fresh and previous["strategy"] == strategy:
        return previous
    batches, batch, size, session = [], [], 0, None
    for message in fresh:
        if batch and (session != message["session"] or size + len(message["text"]) > 24000):
            batches.append(batch)
            batch, size = [], 0
        session = message["session"]
        batch.append(message)
        size += len(message["text"])
    if batch:
        batches.append(batch)
    with ThreadPoolExecutor(max_workers=workers) as pool:
        results = list(pool.map(lambda chunk: model("extract", {"messages": chunk}), batches))
    claims = previous["claims"] if previous else []
    rejected = previous["rejected"] if previous else []
    exact = {(c["scope"], c["kind"], c["text"].casefold()): c for c in claims}
    for raw in [c for result in results for c in result.get("claims", [])]:
        if not all(isinstance(raw.get(k), str) and raw[k].strip() for k in ("text", "scope", "topic", "kind")):
            rejected.append({"claim": raw, "reason": "invalid claim schema"})
            continue
        if raw["kind"] not in ("fact", "preference", "decision", "proposal", "reported"):
            rejected.append({"claim": raw, "reason": "invalid claim kind"})
            continue
        evidence = raw.get("evidence", [])
        if not evidence or any(e.get("source") not in sources or not e.get("quote") or
                               e["quote"] not in sources[e["source"]]["text"] for e in evidence):
            rejected.append({"claim": raw, "reason": "unsupported quote"})
            continue
        claim = dict(raw)
        claim["evidence"] = []
        for evidence_item in evidence:
            quote = evidence_item["quote"]
            start = sources[evidence_item["source"]]["text"].index(quote)
            claim["evidence"].append({**evidence_item, "span_start": start, "span_end": start + len(quote)})
        if all(sources[e["source"]]["role"] != "user" for e in evidence):
            claim["kind"] = "reported"
            if not claim["text"].startswith("The agent reported"):
                claim["text"] = "The agent reported: " + claim["text"]
        key = (claim["scope"], claim["kind"], claim["text"].casefold())
        if key in exact:
            saved = exact[key]
            saved["evidence"] += [e for e in evidence if e not in saved["evidence"]]
            continue
        claim["id"] = identity([raw["text"], evidence])
        claim["status"] = "active"
        claims.append(claim)
        exact[key] = claim
    relations = list(previous.get("relations", [])) if previous else []
    suggested_notes = []
    aliases = previous.get("scope_aliases", []) if previous else []
    if strategy == "reconciled" and claims:
        labels = sorted({c["scope"] for c in claims})
        if len(labels) > 1:
            proposals = model("scope_aliases", {"scopes": [
                {"label": label, "examples": [c["text"] for c in claims if c["scope"] == label][:6]}
                for label in labels]}).get("aliases", [])
            targets = {p.get("to") for p in proposals}
            valid = [p for p in proposals if p.get("from") in labels and p.get("to") in labels and
                     p["from"] != p["to"] and p["from"] not in targets and
                     "Personal" not in (p["from"], p["to"])]
            mapping = {p["from"]: p["to"] for p in valid}
            aliases = aliases + [p for p in valid if p not in aliases]
            for claim in claims:
                if claim["scope"] in mapping:
                    claim.setdefault("original_scope", claim["scope"])
                    claim["scope"] = mapping[claim["scope"]]
    by_id = {c["id"]: c for c in claims}
    if strategy == "reconciled" and claims:
        scopes = sorted({c["scope"] for c in claims})
        def organize(scope):
            selected = [c for c in claims if c["scope"] == scope]
            names = {c["id"]: f"C{i}" for i, c in enumerate(selected)}
            reverse = {short: full for full, short in names.items()}
            existing = copy.deepcopy([n for n in previous["notes"] if n["scope"] == scope]) if previous and previous["strategy"] == "reconciled" else []
            for note in existing:
                note["claims"] = [names[i] for i in note["claims"] if i in names]
            result = model("organize", {"claims": [{**c, "id": names[c["id"]]} for c in selected],
                                        "target_note_count": max(1, round(math.sqrt(len(selected)))),
                                        "existing_notes": existing})
            if result.pop("_native_references", False):
                return result
            for note in result.get("notes", []):
                note["claims"] = [reverse[i] for i in note.get("claims", []) if isinstance(i, str) and i in reverse]
            for relation in result.get("relations", []):
                relation["from"] = reverse.get(relation.get("from"), "")
                relation["to"] = reverse.get(relation.get("to"), "")
            return result
        with ThreadPoolExecutor(max_workers=workers) as pool:
            organized_groups = list(pool.map(organize, scopes))
        suggested_notes = [n for group in organized_groups for n in group.get("notes", [])]
        for rel in [r for group in organized_groups for r in group.get("relations", [])]:
            new, old = by_id.get(rel.get("from")), by_id.get(rel.get("to"))
            if not new or not old or new == old or new["scope"] != old["scope"]:
                continue
            if rel.get("kind") == "supersedes":
                if new.get("key") and old.get("key") and new["key"].casefold() != old["key"].casefold():
                    continue
                new_times = [instant(sources[e["source"]]["ts"]) for e in new["evidence"]]
                old_times = [instant(sources[e["source"]]["ts"]) for e in old["evidence"]]
                human = any(sources[e["source"]]["role"] == "user" for e in new["evidence"])
                if not human or min(new_times) <= max(old_times) or new["kind"] == "proposal":
                    continue
                old["status"] = "superseded"
            elif rel.get("kind") == "duplicate":
                if new["kind"] != old["kind"]:
                    continue
                new_order = (min(instant(sources[e["source"]]["ts"]) for e in new["evidence"]), new["id"])
                old_order = (min(instant(sources[e["source"]]["ts"]) for e in old["evidence"]), old["id"])
                if new_order <= old_order or old["status"] != "active":
                    continue
                new["status"] = "duplicate"
            elif rel.get("kind") == "contradicts":
                new["unsure"] = old["unsure"] = True
            else:
                continue
            if rel not in relations:
                relations.append(rel)
    groups = defaultdict(list)
    for claim in claims:
        groups[(claim["scope"], claim["topic"])].append(claim["id"])
    notes, assigned = [], set()
    for note in suggested_notes:
        ids = list(dict.fromkeys(i for i in note.get("claims", []) if i in by_id and by_id[i]["scope"] == note.get("scope")))
        ids = [i for i in ids if i not in assigned]
        if not ids or not note.get("title"):
            continue
        assigned.update(ids)
        folder = note.get("folder", [note["scope"]])
        if not isinstance(folder, list) or not folder or any(not isinstance(f, str) for f in folder):
            folder = [note["scope"]]
        notes.append({"id": identity([note["scope"], note["title"]]), "title": note["title"],
                      "scope": note["scope"], "folder": folder[:2],
                      "claims": ids, "links": [], "suggested_links": note.get("links", [])})
    for key, ids in groups.items():
        ids = [i for i in ids if i not in assigned]
        if ids:
            notes.append({"id": identity(key), "title": key[1], "scope": key[0],
                          "folder": [key[0]], "claims": ids, "links": []})
    unique = {}
    for note in notes:
        if note["id"] in unique:
            saved = unique[note["id"]]
            saved["claims"] = list(dict.fromkeys(saved["claims"] + note["claims"]))
            saved.setdefault("suggested_links", []).extend(note.get("suggested_links", []))
        else:
            unique[note["id"]] = note
    notes = list(unique.values())
    lookup = {(n["scope"], n["title"]): n for n in notes}
    for note in notes:
        for link in note.pop("suggested_links", []):
            target = lookup.get((link.get("scope"), link.get("title")))
            if target and target["id"] != note["id"]:
                note["links"].append({"id": target["id"], "reason": link.get("reason", "Related topic")})
    if previous:
        used = set()
        remapped = {}
        for note in notes:
            matches = [n for n in previous["notes"] if n["scope"] == note["scope"] and n["id"] not in used]
            if matches:
                best = max(matches, key=lambda n: len(set(n["claims"]) & set(note["claims"])))
                if set(best["claims"]) & set(note["claims"]):
                    old_id = note["id"]
                    note["id"] = best["id"]
                    used.add(best["id"])
                    remapped[old_id] = note["id"]
        # Apply the whole rename permutation once, so swaps cannot redirect links twice.
        for note in notes:
            for link in note["links"]:
                link["id"] = remapped.get(link["id"], link["id"])
        prior_notes = {n["id"]: n for n in previous["notes"]}
        prior_claims = {c["id"]: c for c in previous["claims"]}
        for note in notes:
            old_note = prior_notes.get(note["id"])
            if old_note and "sections" in old_note:
                current = set(note["claims"])
                unchanged = current == set(old_note["claims"]) and all(
                    by_id[i] == prior_claims.get(i) for i in current)
                kept_sections = []
                for section in old_note["sections"]:
                    paragraphs = [p for p in section["paragraphs"] if all(
                        i in current and by_id[i]["status"] == "active" and
                        by_id[i]["text"] == prior_claims.get(i, {}).get("text") for i in p["claims"])]
                    if paragraphs:
                        kept_sections.append({**section, "paragraphs": paragraphs})
                note["sections"] = kept_sections
                note["needs_editorial_update"] = not unchanged
                for field in ("composition", "article_rejections"):
                    if field in old_note:
                        note[field] = old_note[field]
    return {"schema": 1, "strategy": strategy, "sources": sources, "claims": claims,
            "notes": notes, "rejected": rejected, "relations": relations, "scope_aliases": aliases}


def recall(vault, question, model, scope=None, limit=16, retrieval="lexical"):
    """Retrieve current evidence in an explicit scope and require resolvable citations."""
    def words(text):
        return set(re.findall(r"[\w.-]+", text.lower()))

    terms = words(question)
    aliases = {a["from"].casefold(): a["to"].casefold() for a in vault.get("scope_aliases", [])}
    def canonical(label):
        label = label.casefold()
        seen = set()
        while label in aliases and label not in seen:
            seen.add(label)
            label = aliases[label]
        return label
    def visible(label):
        return scope is None or canonical(label) in (canonical(scope), "personal")
    selected_notes = set()
    eligible_notes = [n for n in vault["notes"] if visible(n["scope"])]
    if retrieval in ("graph", "coverage"):
        plan = model("query", {"question": question, "notes": [
            {"id": n["id"], "title": n["title"], "scope": n["scope"]} for n in eligible_notes]})
        terms.update(words(" ".join(plan.get("terms", [])[:12])))
        selected_notes = set(plan.get("notes", [])) & {n["id"] for n in eligible_notes}
    candidates = [c for c in vault["claims"] if c["status"] == "active" and
                  visible(c["scope"])]
    counts = {t: sum(t in words(c["text"]) for c in candidates) for t in terms}
    def score(claim):
        tokens = words(claim["text"] + " " + claim.get("topic", "") + " " + claim["scope"])
        return sum(math.log(1 + (len(candidates) + 1) / (counts[t] + 1)) for t in terms & tokens)
    boosts = defaultdict(float)
    if selected_notes:
        notes_by_id = {n["id"]: n for n in eligible_notes}
        # Bounded one-hop diffusion. Scope filtering happens before any traversal.
        for note_id in selected_notes:
            for claim_id in notes_by_id[note_id]["claims"]:
                boosts[claim_id] += 4
            for link in notes_by_id[note_id]["links"]:
                if link["id"] in notes_by_id:
                    for claim_id in notes_by_id[link["id"]]["claims"]:
                        boosts[claim_id] += 1
    ranked = sorted(((score(c) + boosts[c["id"]], c) for c in candidates), key=lambda x: (-x[0], x[1]["id"]))
    selected = [c for s, c in ranked[:limit] if s > 0]
    if retrieval == "coverage":
        selected, remaining, covered = [], [(s, c) for s, c in ranked if s > 0], set()
        while remaining and len(selected) < limit:
            def gain(item):
                relevance, claim = item
                features = words(claim["text"]) & terms
                novelty = len(features - covered)
                overlap = max((len(words(claim["text"]) & words(c["text"])) /
                               max(1, len(words(claim["text"]) | words(c["text"]))) for c in selected), default=0)
                return relevance * (1 - 0.6 * overlap) + novelty
            best = max(remaining, key=gain)
            remaining.remove(best)
            selected.append(best[1])
            covered.update(words(best[1]["text"]) & terms)
    if not selected:
        return {"answer": "I could not find supported knowledge for that question.", "citations": [], "claims": []}
    answer = model("recall", {"question": question, "claims": selected})
    known = {c["id"]: c for c in selected}
    refs = answer.get("citations", [])
    if any(i not in known for i in refs) or (answer.get("answer") and not refs and not answer.get("abstain")):
        raise ValueError("Answer contains an unsupported citation or lacks citations")
    return {"answer": answer.get("answer", ""), "citations": [
        {"claim": i, **e} for i in dict.fromkeys(refs) for e in known[i]["evidence"]],
        "claims": [c["id"] for c in selected]}

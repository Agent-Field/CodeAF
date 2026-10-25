"""Compose readable subject articles as a projection over sourced claims."""
import copy
from concurrent.futures import ThreadPoolExecutor
import re


def compose_notes(vault, model, workers=3):
    """Synthesize subject prose; reject paragraphs with missing or foreign claim references."""
    vault = copy.deepcopy(vault)
    by_id = {c["id"]: c for c in vault["claims"]}
    for note in vault["notes"]:
        note["sections"] = []
    batches, batch, count = [], [], 0
    for note in vault["notes"]:
        size = sum(by_id[i]["status"] == "active" for i in note["claims"])
        if batch and (count + size > 80 or len(batch) >= 8):
            batches.append(batch)
            batch, count = [], 0
        batch.append(note)
        count += size
    if batch:
        batches.append(batch)

    def compose(batch):
        notes = {f"N{i}": n for i, n in enumerate(batch)}
        claim_ids = list(dict.fromkeys(i for n in batch for i in n["claims"] if by_id[i]["status"] == "active"))
        names = {claim_id: f"C{i}" for i, claim_id in enumerate(claim_ids)}
        reverse = {v: k for k, v in names.items()}
        if not claim_ids:
            return
        payload = {"notes": [{"id": local_id, "title": n["title"], "scope": n["scope"],
                              "claims": [names[i] for i in n["claims"] if i in names],
                              "related_notes": n["links"]} for local_id, n in notes.items()],
                   "claims": [{"id": names[i], "text": by_id[i]["text"], "kind": by_id[i]["kind"],
                               "unsure": by_id[i].get("unsure", False)} for i in claim_ids]}
        result = model("compose", payload)
        for article in result.get("notes", []):
            note = notes.get(article.get("id"))
            if not note:
                continue
            allowed = set(note["claims"])
            sections, used, rejected = [], set(), 0
            for section in article.get("sections", []):
                paragraphs = []
                for paragraph in section.get("paragraphs", []):
                    refs, text = paragraph.get("claims", []), paragraph.get("text", "")
                    ids = [reverse.get(i) for i in refs if isinstance(i, str)]
                    narration = re.search(r"\b(?:the user|the agent|user|agent) (?:said|reported|asked|requested|wants)\b", text, re.I)
                    if not text or narration or not ids or len(ids) != len(refs) or any(i not in allowed for i in ids):
                        rejected += 1
                        continue
                    paragraphs.append({"text": text, "claims": list(dict.fromkeys(ids))})
                    used.update(ids)
                if paragraphs:
                    sections.append({"heading": str(section.get("heading", "")), "paragraphs": paragraphs})
            note["sections"] = sections
            note["composition"] = {"cited_claims": len(used), "available_claims": len(set(claim_ids) & allowed),
                                   "rejected_paragraphs": rejected,
                                   "limitation": "References validated; generated prose still needs semantic review."}
    with ThreadPoolExecutor(max_workers=workers) as pool:
        list(pool.map(compose, batches))
    return vault


def review_articles(vault, model, workers=3):
    """Remove prose that a separate model pass cannot support from its cited claims."""
    vault = copy.deepcopy(vault)
    claims = {c["id"]: c for c in vault["claims"]}
    items = []
    for note in vault["notes"]:
        for section in note.get("sections", []):
            for paragraph in section["paragraphs"]:
                items.append((note, section, paragraph))
    batches = [items[i:i+32] for i in range(0, len(items), 32)]
    def review(batch):
        paragraphs = {f"P{i}": item for i, item in enumerate(batch)}
        result = model("review_articles", {"paragraphs": [
            {"id": key, "title": note["title"], "heading": section["heading"], "text": paragraph["text"],
             "claims": [{"text": claims[i]["text"], "kind": claims[i]["kind"]} for i in paragraph["claims"]]}
            for key, (note, section, paragraph) in paragraphs.items()]})
        checks = {c["id"]: c for c in result.get("checks", []) if c.get("id") in paragraphs}
        for key, (note, section, paragraph) in paragraphs.items():
            check = checks.get(key, {"supported": False, "reason": "No paragraph review returned"})
            paragraph["review"] = check
    with ThreadPoolExecutor(max_workers=workers) as pool:
        list(pool.map(review, batches))
    for note in vault["notes"]:
        sections, rejected = [], []
        for section in note.get("sections", []):
            kept = []
            for paragraph in section["paragraphs"]:
                if paragraph.get("review", {}).get("supported") is True:
                    kept.append(paragraph)
                else:
                    rejected.append({**paragraph, "reason": paragraph.get("review", {}).get("reason", "Unreviewed")})
            if kept:
                sections.append({**section, "paragraphs": kept})
        note["sections"] = sections
        note["article_rejections"] = rejected
    return vault

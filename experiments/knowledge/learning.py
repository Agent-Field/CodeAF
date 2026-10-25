"""Evidence-driven subject understanding and bounded curiosity, separate from source truth."""
import copy
import re
from collections import Counter
from concurrent.futures import ThreadPoolExecutor

from knowledge import identity
from evidence import basis

VERSION = 'learning-1'


def learn(vault, model, previous=None, *, workers=3):
    """Discover subjects, explain evidence, and retain grounded unresolved questions.

    No research is executed and no generated interpretation becomes a source claim.
    Unchanged subject inputs reuse their explanations without a model call.
    """
    claims = {c['id']: c for c in vault['claims'] if c['status'] == 'active'}
    notes = {n['id']: n for n in vault['notes'] if set(n['claims']) & claims.keys()}
    catalog = [{'id': n['id'], 'title': n['title'], 'scope': n['scope'],
                'claim_count': sum(c in claims for c in n['claims']),
                'gist': [claims[c]['text'][:240] for c in n['claims'] if c in claims][:2]}
               for n in sorted(notes.values(), key=lambda n: n['id'])]
    if len(catalog) > 256:
        raise ValueError('Discovery exceeds 256 notes; partition by permitted collection before learning')
    catalog_hash = identity([VERSION, catalog])
    if not catalog:
        return {'version': VERSION, 'catalog_hash': catalog_hash, 'discovery': [], 'subjects': []}
    if previous and previous.get('catalog_hash') == catalog_hash:
        discovery = previous['discovery']
    else:
        discovery = model('discover', {'catalog': catalog}).get('subjects', [])
    # Give a broad subject one bounded chance to discover its own useful substructure.
    refined = []
    for subject in discovery:
        note_ids = [i for i in subject.get('notes', []) if i in notes]
        members = {c for n in note_ids for c in notes[n]['claims'] if c in claims}
        if len(members) > 160 and len(note_ids) > 1:
            partition = model('discover', {'catalog': [n for n in catalog if n['id'] in note_ids],
                                           'partition_parent': subject.get('title'),
                                           'constraint': 'Split this oversized group into coherent subjects with at most 160 claims each.'}).get('subjects', [])
            covered = {i for child in partition for i in child.get('notes', []) if i in note_ids}
            valid = all(set(child.get('notes', [])) <= set(note_ids) for child in partition)
            if partition and valid and covered == set(note_ids):
                refined.extend(partition)
                continue
        refined.append(subject)
    discovery = refined
    previous_subjects = {s['id']: s for s in (previous or {}).get('subjects', [])}
    planned = []
    seen = set()
    for raw in discovery:
        title, scope = raw.get('title'), raw.get('scope')
        note_ids = list(dict.fromkeys(i for i in raw.get('notes', []) if i in notes))
        if not isinstance(title, str) or not title.strip() or not isinstance(scope, str) or not note_ids:
            continue
        subject_id = identity(['subject', scope.casefold(), title.casefold()])
        # A unique unchanged membership is a conservative rename, not a semantic merge.
        matches = [s for s in previous_subjects.values() if s['scope'].casefold() == scope.casefold()
                   and set(s['notes']) == set(note_ids) and s['id'] not in seen]
        aliases = list(raw.get('aliases', []))
        if subject_id not in previous_subjects and len(matches) == 1:
            subject_id = matches[0]['id']
            aliases = list(dict.fromkeys(aliases + matches[0].get('aliases', []) + [matches[0]['title']]))
        if subject_id in seen:
            continue
        seen.add(subject_id)
        ids = list(dict.fromkeys(c for n in note_ids for c in notes[n]['claims'] if c in claims))
        # Never silently drop source knowledge to satisfy a context cap.
        if len(ids) > 160:
            planned.append({'id': subject_id, 'title': title, 'scope': scope, 'notes': note_ids,
                            'claims': ids, 'sections': [], 'questions': [], 'conflicts': [],
                            'state': 'needs_partition', 'reason': 'More than 160 claims; split this subject before synthesis'})
            continue
        evidence = [{**claims[i], 'source_context': [
            {'role': vault['sources'][e['source']]['role'], 'ts': vault['sources'][e['source']]['ts']}
            for e in claims[i].get('evidence', []) if e['source'] in vault['sources']]} for i in ids]
        digest = identity([VERSION, title, scope, note_ids, evidence])
        old = previous_subjects.get(subject_id)
        if old and old.get('input_hash') == digest:
            planned.append(copy.deepcopy(old))
        else:
            planned.append({'id': subject_id, 'title': title, 'scope': scope, 'notes': note_ids,
                            'claims': ids, 'input_hash': digest, 'aliases': aliases,
                            '_evidence': evidence})

    def understand(subject):
        if '_evidence' not in subject:
            return subject
        evidence = subject.pop('_evidence')
        raw = model('understand', {'subject': {k: v for k, v in subject.items() if k != 'input_hash'},
                                  'claims': evidence})
        allowed = set(subject['claims'])
        sections = []
        for section in raw.get('sections', []):
            paragraphs = []
            for paragraph in section.get('paragraphs', []):
                refs = paragraph.get('claims', [])
                text = paragraph.get('text')
                narration = isinstance(text, str) and re.search(
                    r"\b(?:the user|the agent|the owner|user|agent|owner) (?:said|reported|asked|requested|wants|instructed|corrected|specified)\b", text, re.I)
                if isinstance(text, str) and not narration and refs and all(i in allowed for i in refs):
                    paragraphs.append({'text': paragraph['text'], 'claims': list(dict.fromkeys(refs))})
            if paragraphs:
                sections.append({'heading': str(section.get('heading', '')), 'paragraphs': paragraphs})
        questions = []
        for question in raw.get('questions', [])[:5]:
            refs = question.get('claims', [])
            if isinstance(question.get('question'), str) and refs and all(i in allowed for i in refs):
                questions.append({'question': question['question'], 'why': str(question.get('why', '')),
                                  'claims': list(dict.fromkeys(refs)),
                                  'search_terms': [s for s in question.get('search_terms', []) if isinstance(s, str)][:8],
                                  'state': 'unresolved_in_inspected_evidence'})
        paragraphs = [p for s in sections for p in s['paragraphs']]
        conflicts = [c for c in raw.get('conflicts', []) if len(set(c.get('claims', []))) == 2 and
                     all(i in allowed for i in c['claims'])]
        review = model('review_understanding', {'claims': evidence,
            'paragraphs': [{'id': f'P{i}', **p} for i, p in enumerate(paragraphs)],
            'questions': [{'id': f'Q{i}', **q} for i, q in enumerate(questions)],
            'conflicts': [{'id': f'X{i}', **c} for i, c in enumerate(conflicts)]}) if paragraphs or questions or conflicts else {}
        checks = {c.get('id'): c for c in review.get('checks', [])}
        accepted = {id(p) for i, p in enumerate(paragraphs) if checks.get(f'P{i}', {}).get('supported') is True}
        subject['sections'] = [{**s, 'paragraphs': [p for p in s['paragraphs'] if id(p) in accepted]}
                               for s in sections if any(id(p) in accepted for p in s['paragraphs'])]
        question_checks = {c.get('id'): c for c in review.get('questions', [])}
        subject['questions'] = [q for i, q in enumerate(questions) if question_checks.get(f'Q{i}', {}).get('grounded') is True]
        conflict_checks = {c.get('id'): c for c in review.get('conflicts', [])}
        subject['conflicts'] = [{**c, 'state': 'unresolved'} for i, c in enumerate(conflicts)
                                if conflict_checks.get(f'X{i}', {}).get('supported') is True]
        subject['review'] = review
        subject['state'] = 'explained' if subject['sections'] else 'needs_evidence'
        return subject

    with ThreadPoolExecutor(max_workers=workers) as pool:
        subjects = list(pool.map(understand, planned))
    # Curiosity first spends a bounded search on the knowledge already available.
    corpus_hash = identity([(i, claims[i]['text'], claims[i]['kind'], claims[i]['scope']) for i in sorted(claims)])
    pending = []
    stop = {'the', 'and', 'how', 'are', 'what', 'does', 'for', 'with', 'this', 'that', 'from', 'can'}
    for subject in subjects:
        for question in subject.get('questions', []):
            if question.get('search', {}).get('corpus_hash') == corpus_hash:
                continue
            terms = set(re.findall(r"\w+", ' '.join(question.get('search_terms', [])).casefold())) - stop
            inspected = set(subject['claims'])
            ranked = sorted(((len(terms & set(re.findall(r"\w+", c['text'].casefold()))), c['id'])
                             for c in claims.values() if c['id'] not in inspected), key=lambda r: (-r[0], r[1]))
            ids = [i for score, i in ranked if score >= 2][:6]
            question['search'] = {'corpus_hash': corpus_hash, 'candidate_claims': ids,
                                  'method': 'bounded lexical search of active extracted claims; raw sources not exhaustively searched'}
            question['state'] = 'unresolved_in_inspected_evidence'
            question.pop('related_claims', None)
            if ids:
                pending.append((question, ids))
    for start in range(0, len(pending), 16):
        batch = pending[start:start+16]
        ids = list(dict.fromkeys(i for _, refs in batch for i in refs))
        result = model('resolve_questions', {
            'questions': [{'id': f'Q{i}', 'question': q['question'], 'candidates': refs}
                          for i, (q, refs) in enumerate(batch)],
            'claims': [{k: claims[i][k] for k in ('id', 'text', 'scope', 'kind')} for i in ids]})
        by_id = {f'Q{i}': (q, refs) for i, (q, refs) in enumerate(batch)}
        for match in result.get('matches', []):
            if match.get('id') not in by_id:
                continue
            question, permitted = by_id[match['id']]
            refs = match.get('claims', [])
            if match.get('relation') == 'answers' and refs and all(i in permitted for i in refs):
                question['state'] = 'answer_candidate_found'
                question['related_claims'] = list(dict.fromkeys(refs))
    assigned = {n for s in subjects for n in s['notes']}
    return {'version': VERSION, 'catalog_hash': catalog_hash, 'discovery': discovery, 'subjects': subjects,
            'coverage': {'eligible_notes': len(notes), 'assigned_notes': len(assigned),
                         'notes_without_subject': sorted(set(notes) - assigned)}}


def project(vault, learning, *, as_of=None, stale_after_seconds=None):
    """Project subject maps and reviewed gap answers without changing source claims."""
    result = copy.deepcopy(vault)
    original = {n['id']: n for n in result['notes']}
    active_claims = {c['id']: c for c in vault['claims'] if c['status'] == 'active'}
    active = set(active_claims)
    memberships = {}
    for subject in learning['subjects']:
        if subject['id'] in original:
            raise ValueError('Project subjects from the original vault, not an earlier subject projection')
        for note_id in subject['notes']:
            memberships.setdefault(note_id, []).append(subject['id'])
    for subject in learning['subjects']:
        sections, stale = [], False
        for section in subject.get('sections', []):
            paragraphs = []
            for paragraph in section['paragraphs']:
                evidence = paragraph.get('evidence', [])
                valid_claims = set(paragraph.get('claims', []) + paragraph.get('depends_on', [])) <= active
                valid_sources = all(e['source'] in vault['sources'] and
                    e['quote'] in vault['sources'][e['source']]['text'] and
                    e.get('source_hash') == identity(vault['sources'][e['source']]['text']) for e in evidence)
                has_support = bool(paragraph.get('claims')) or (
                    paragraph.get('derivation') == 'subject_guide' and paragraph.get('review') and bool(evidence))
                if valid_claims and valid_sources and has_support:
                    paragraphs.append(copy.deepcopy(paragraph))
            stale |= len(paragraphs) != len(section['paragraphs'])
            if paragraphs:
                sections.append({**section, 'paragraphs': paragraphs})
        questions = copy.deepcopy(subject.get('questions', []))
        answer_claims = []
        for question in questions:
            answer = question.get('answer')
            if not answer:
                continue
            valid = set(question['claims']) <= active and set(answer['claims']) <= active and all(
                e['source'] in vault['sources'] and e['quote'] in vault['sources'][e['source']]['text'] and
                e['source_hash'] == identity(vault['sources'][e['source']]['text']) for e in answer['evidence'])
            if not valid:
                question.pop('answer', None)
                question['state'] = 'needs_review'
                stale = True
                continue
            if question.get('state') in ('answered', 'partially_answered'):
                sections.append({'heading': answer['heading'], 'paragraphs': [
                    {'text': answer['text'], 'claims': answer['claims'], 'evidence': answer['evidence'],
                     'question': question['id'], 'partial': question['state'] == 'partially_answered',
                     'derivation': 'gap_answer', 'review': answer['review'], 'depends_on': question['claims']}]})
                answer_claims.extend(answer['claims'])
        conflict_claims = {cid for conflict in subject.get('conflicts', [])
                           if conflict.get('state') == 'unresolved' and set(conflict['claims']) <= active
                           for cid in conflict['claims']}
        for section in sections:
            for paragraph in section['paragraphs']:
                supporting = [active_claims[cid] for cid in paragraph.get('claims', []) if cid in active_claims]
                evidence = paragraph.get('evidence', []) + [e for c in supporting for e in c.get('evidence', [])]
                roles = sorted({vault['sources'][e['source']]['role'] for e in evidence if e['source'] in vault['sources']})
                paragraph['evidence_basis'] = {
                    'source_roles': roles, 'claim_kinds': sorted({c['kind'] for c in supporting}),
                    'unresolved_conflict': bool(set(paragraph.get('claims', [])) & conflict_claims),
                    **basis(vault['sources'], evidence, as_of=as_of, stale_after_seconds=stale_after_seconds)}
        roots = Counter(original[i]['folder'][0] for i in subject['notes'] if i in original and original[i].get('folder'))
        root = sorted(roots, key=lambda k: (-roots[k], k))[0] if roots else subject['scope']
        links = [{'id': i, 'kind': 'explains', 'reason': 'Detailed evidence for this subject'} for i in subject['notes'] if i in original]
        related = {}
        for source in subject['notes']:
            for edge in original.get(source, {}).get('links', []):
                for target in memberships.get(edge['id'], []):
                    if target != subject['id'] and edge.get('reason'):
                        related.setdefault(target, {'id': target, 'kind': 'related', 'reason': edge['reason'],
                                                    'via': [source, edge['id']]})
        links.extend(list(related.values())[:3])
        result['notes'].append({
            'id': subject['id'], 'title': subject['title'], 'scope': subject['scope'],
            'folder': [root], 'claims': list(dict.fromkeys(subject['claims'] + answer_claims)),
            'sections': sections, 'questions': questions, 'aliases': subject.get('aliases', []),
            'conflicts': subject.get('conflicts', []), 'learning_state': 'needs_review' if stale else subject['state'],
            'kind': 'subject', 'links': links})
    return result


def main():
    import argparse
    import json
    from pathlib import Path
    from provider import OpenRouter
    from storage import save, viewer
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vault', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True, mode=0o700)
    vault = json.loads(args.vault.read_text())
    state = args.out / 'learning.json'
    previous = json.loads(state.read_text()) if state.exists() else None
    result = learn(vault, OpenRouter(args.out / 'calls'), previous)
    state.write_text(json.dumps(result, ensure_ascii=False, indent=2))
    projection = project(vault, result)
    save(projection, args.out / 'knowledge')
    viewer({'direct': vault, 'reconciled': projection}, {'learning': result['coverage']}, {}, args.out / 'viewer')
    print(json.dumps({'subjects': len(result['subjects']), 'coverage': result['coverage'],
                      'viewer': str(args.out / 'viewer/standalone.html')}, indent=2))


if __name__ == '__main__':
    main()

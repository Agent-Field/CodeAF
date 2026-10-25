"""Budgeted gap closure over existing claims and original conversation evidence."""
import copy
import json
import re
import sqlite3

from knowledge import identity

POLICY = 'maintenance-3'


def words(text):
    return list(dict.fromkeys(re.findall(r'\w{3,}', text.casefold())))


def maintain(vault, learning, model, previous=None, *, budget=8, context='', advance=False):
    """Investigate a bounded frontier; unchanged evidence never repeats an attempt.

    Input must already be a permitted collection. Semantic scopes are not ACLs.
    Answers are reviewed derived projections, never new source claims.
    """
    if not 0 <= budget <= 32:
        raise ValueError('Question budget must be between zero and 32')
    result = copy.deepcopy(learning)
    active = {c['id']: c for c in vault['claims'] if c['status'] == 'active'}
    sources = vault['sources']
    old_questions = {q['id']: q for s in (previous or {}).get('subjects', [])
                     for q in s.get('questions', []) if q.get('id')}
    candidates = []
    question_map = {}
    for subject in result['subjects']:
        for question in subject.get('questions', []):
            qid = identity(['question', subject['id'], question['question'].strip().casefold()])
            question['id'] = qid
            old = old_questions.get(qid)
            if old:
                for key in ('answer', 'attempt_hash', 'history', 'state', 'search', 'priority_reason', 'processed_wakes'):
                    if key in old:
                        question[key] = copy.deepcopy(old[key])
            question.setdefault('state', 'backlog')
            if not question.get('claims') or any(i not in active for i in question['claims']):
                question.pop('answer', None)
                question['state'] = 'retired_evidence'
                continue
            answer = question.get('answer')
            if answer and (any(i not in active for i in answer['claims']) or any(
                    e['source'] not in sources or e['quote'] not in sources[e['source']]['text'] or
                    e['source_hash'] != identity(sources[e['source']]['text']) for e in answer['evidence'])):
                question.setdefault('history', []).append({'answer': question.pop('answer'), 'reason': 'Supporting evidence changed or retired'})
                question.pop('attempt_hash', None)
                question['state'] = 'needs_review'
            question_map[qid] = question
            if question.get('curiosity', {}).get('state') == 'parked' and not question.get('wake_sources'):
                continue
            candidates.append({'id': qid, 'subject_id': subject['id'], 'subject': subject['title'],
                               'question': question['question'], 'why': question.get('why', ''),
                               'wake_sources': question.get('wake_sources', [])[:2],
                               'waking_source_count':len(question.get('wake_sources', [])),
                               'kinds': sorted({active[i]['kind'] for i in question['claims']})})
    # Prioritize meaning rather than launching a researcher for every proposed question.
    catalog_hash = identity([POLICY, candidates, context, budget])
    prior_plan = (previous or {}).get('maintenance', {})
    calls = 0
    if budget == 0 or not candidates:
        selection = []
    elif not advance and prior_plan.get('plan_hash') == catalog_hash:
        selection = prior_plan['selection']
    else:
        # A bounded, diverse shortlist keeps the planning context from growing unbounded.
        by_subject = {}
        for item in sorted(candidates, key=lambda q: (not bool(q.get('wake_sources')), q['subject_id'], q['id'])):
            by_subject.setdefault(item['subject_id'], []).append(item)
        shortlist = []
        for level in range(5):
            for group in by_subject.values():
                if level < len(group) and len(shortlist) < 64:
                    shortlist.append(group[level])
        response = model('frontier', {'context': context, 'budget': budget, 'questions': [
            {**q, 'state': question_map[q['id']]['state']} for q in shortlist]})
        calls += 1
        permitted = {q['id']: q for q in shortlist}
        selection, counts = [], {}
        for item in response.get('selected', []):
            qid = item.get('id')
            if qid not in permitted or qid in {s['id'] for s in selection} or len(selection) >= budget:
                continue
            sid = permitted[qid]['subject_id']
            if counts.get(sid, 0) >= 2:
                continue
            counts[sid] = counts.get(sid, 0) + 1
            selection.append({'id': qid, 'reason': str(item.get('reason', ''))})
    selected = {q['id'] for q in selection if q['id'] in question_map}
    for qid, question in question_map.items():
        if qid not in selected and question['state'] not in ('answered', 'partially_answered', 'retired_evidence'):
            question['state'] = 'backlog'
    # One local full-text index serves all selected questions. No model rereads history.
    db = sqlite3.connect(':memory:')
    db.execute('CREATE VIRTUAL TABLE documents USING fts5(kind UNINDEXED, key UNINDEXED, body)')
    db.executemany('INSERT INTO documents VALUES(?,?,?)', [('claim', i, c['text']) for i, c in active.items()])
    db.executemany('INSERT INTO documents VALUES(?,?,?)', [('source', i, s['text']) for i, s in sources.items()])
    investigations, evidence_claims, evidence_sources, question_spans = [], {}, {}, {}
    reused = 0
    context_limit = 32000
    for chosen in selection:
        qid = chosen['id']
        if qid not in question_map:
            continue
        question = question_map[qid]
        question['priority_reason'] = chosen['reason']
        terms = words(' '.join(question.get('search_terms', [])) or question['question'])[:12]
        if not terms:
            question['state'] = 'blocked_by_evidence'
            continue
        query = ' OR '.join('"' + t + '"' for t in terms)
        hits = db.execute('SELECT kind,key,body FROM documents WHERE documents MATCH ? ORDER BY bm25(documents),key LIMIT 48', (query,)).fetchall()
        claim_candidates, source_candidates = [], []
        for kind, key, body in hits:
            if kind == 'claim' and len(claim_candidates) < 4 and len(body) <= 1200:
                claim_candidates.append({k: active[key][k] for k in ('id', 'text', 'scope', 'kind')})
            if kind == 'source' and len(source_candidates) < 3:
                # Snippets remain exact spans of the immutable source, including escaped text.
                positions = [body.casefold().find(t) for t in sorted(terms, key=len, reverse=True) if t in body.casefold()]
                start = max(0, (positions[0] if positions else 0) - 300)
                quote = body[start:start+1200]
                source_candidates.append({'id': identity([key, start, quote]), 'source': key, 'quote': quote, 'role': sources[key]['role'],
                                          'ts': sources[key]['ts'], 'source_hash': identity(body)})
        # Explicit matching new evidence must not disappear below the ordinary top-k.
        waking = []
        for sid in question.get('wake_sources', [])[:2]:
            if sid not in sources:
                continue
            body = sources[sid]['text']
            positions = [body.casefold().find(t) for t in terms if t in body.casefold()]
            start = max(0, (min(positions) if positions else 0)-300)
            quote = body[start:start+1200]
            waking.append({'id':identity([sid,start,quote]),'source':sid,'quote':quote,
                           'role':sources[sid]['role'],'ts':sources[sid]['ts'],'source_hash':identity(body)})
        if waking:
            ids = {s['source'] for s in waking}
            source_candidates = (waking+[s for s in source_candidates if s['source'] not in ids])[:3]
        attempt_hash = identity([POLICY, question['question'], claim_candidates, source_candidates])
        if question.get('attempt_hash') == attempt_hash:
            question['processed_wakes'] = sorted(set(question.get('processed_wakes',[])+[s['source'] for s in waking]))
            question.pop('wake_sources', None)
            reused += 1
            if question.get('answer'):
                question['state'] = 'answered' if question['answer'].get('complete') else 'partially_answered'
            else:
                question['state'] = 'blocked_by_evidence'
            continue
        if question.get('answer'):
            question.setdefault('history', []).append({'answer': question.pop('answer'), 'reason': 'Relevant search evidence changed; review required'})
        if not claim_candidates and not source_candidates:
            question['attempt_hash'] = attempt_hash
            question['state'] = 'blocked_by_evidence'
            continue
        item = {'id': qid, 'question': question['question'],
                'claims': [c['id'] for c in claim_candidates], 'sources': [e['source'] for e in source_candidates]}
        proposed_claims = {**evidence_claims, **{c['id']: c for c in claim_candidates}}
        proposed_sources = {**evidence_sources, **{e['id']: e for e in source_candidates}}
        proposed = {'questions': investigations + [item], 'claims': list(proposed_claims.values()), 'sources': list(proposed_sources.values())}
        if len(json.dumps(proposed, ensure_ascii=False)) > context_limit:
            question['state'] = 'budget_deferred'
            continue
        question['attempt_hash'] = attempt_hash
        question['state'] = 'blocked_by_evidence'
        question['search'] = {'claims': item['claims'], 'sources': item['sources'],
                              'method': 'bounded FTS search over active claims and original messages'}
        question['processed_wakes'] = sorted(set(question.get('processed_wakes',[])+[s['source'] for s in waking]))
        question.pop('wake_sources', None)
        question_spans[qid] = source_candidates
        investigations.append(item)
        evidence_claims, evidence_sources = proposed_claims, proposed_sources
    db.close()
    accepted = []
    if investigations:
        response = model('investigate', {'questions': investigations, 'claims': list(evidence_claims.values()),
                                          'sources': list(evidence_sources.values())})
        calls += 1
        permitted = {q['id']: q for q in investigations}
        seen = set()
        for raw in response.get('answers', []):
            qid = raw.get('question')
            if qid not in permitted or qid in seen or not isinstance(raw.get('text'), str) or not raw['text'].strip():
                continue
            refs, evidence = raw.get('claims', []), raw.get('evidence', [])
            if (not refs and not evidence) or any(i not in permitted[qid]['claims'] for i in refs):
                continue
            if any(e.get('source') not in permitted[qid]['sources'] or not e.get('quote') or
                   not any(e['source'] == span['source'] and e['quote'] in span['quote'] for span in question_spans[qid]) for e in evidence):
                continue
            answer = {'question': qid, 'heading': str(raw.get('heading', 'Further understanding')),
                      'text': raw['text'], 'claims': list(dict.fromkeys(refs)),
                      'evidence': [{**e, 'source_hash': identity(sources[e['source']]['text']),
                                    'role': sources[e['source']]['role'], 'ts': sources[e['source']]['ts']}
                                   for e in evidence]}
            accepted.append(answer)
            seen.add(qid)
        if accepted:
            review = model('review_answers', {'answers': accepted,
                                               'questions': [{'id': a['question'], 'question': question_map[a['question']]['question']} for a in accepted],
                                               'claims': list(evidence_claims.values()),
                                               'sources': list(evidence_sources.values())})
            calls += 1
            checks = {c.get('question'): c for c in review.get('checks', [])}
            for answer in accepted:
                qid = answer['question']
                if checks.get(qid, {}).get('supported') is True:
                    question_map[qid]['answer'] = {**answer, 'complete': checks[qid].get('complete') is True, 'review': 'separate same-model entailment check'}
                    question_map[qid]['state'] = 'answered' if checks[qid].get('complete') is True else 'partially_answered'
    current_ids = {q.get('id') for s in result['subjects'] for q in s.get('questions', [])}
    retired = {q['id']: copy.deepcopy(q) for q in (previous or {}).get('retired_questions', [])}
    for qid, question in old_questions.items():
        if qid not in current_ids:
            retired[qid] = {**copy.deepcopy(question), 'retirement_reason': 'Question removed or rephrased; semantic equivalence not assumed'}
    result['retired_questions'] = list(retired.values())
    questions = list(question_map.values())
    result['maintenance'] = {'policy': POLICY, 'plan_hash': catalog_hash, 'selection': selection,
                             'selected': len(selected), 'backlog': sum(q['state'] == 'backlog' for q in questions),
                             'answered': sum(q['state'] == 'answered' for q in questions),
                             'states': {state: sum(q['state'] == state for q in questions) for state in sorted({q['state'] for q in questions})},
                             'provider_calls': calls, 'reused_attempts': reused, 'context_limit_characters': context_limit,
                             'limitation': 'Bounded source search and same-model review; top-k candidate changes trigger repair, but lower-ranked unseen evidence can be missed. Blocked means no accepted answer in this attempt, including review rejection. No external research or new source facts.'}
    return result


def main():
    import argparse
    from pathlib import Path
    from learning import project
    from provider import OpenRouter
    from storage import save, viewer, write_json
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vault', type=Path, required=True)
    parser.add_argument('--learning', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--questions', type=int, default=8)
    parser.add_argument('--context', default='')
    parser.add_argument('--advance', action='store_true', help='Explicitly select another frontier from the backlog')
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True, mode=0o700)
    vault = json.loads(args.vault.read_text())
    learning = json.loads(args.learning.read_text())
    state = args.out / 'maintained.json'
    previous = json.loads(state.read_text()) if state.exists() else None
    maintained = maintain(vault, learning, OpenRouter(args.out / 'calls'), previous,
                          budget=args.questions, context=args.context, advance=args.advance)
    write_json(state, maintained)
    projection = project(vault, maintained)
    save(projection, args.out / 'knowledge')
    viewer({'direct': vault, 'reconciled': projection}, {'maintenance': maintained['maintenance']}, {}, args.out / 'viewer')
    print(json.dumps({'maintenance': maintained['maintenance'], 'viewer': str(args.out / 'viewer/standalone.html')}, indent=2))


if __name__ == '__main__':
    main()

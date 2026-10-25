"""A bounded read/search/write learning agent that proposes a coherent subject guide."""
import copy
import json
import re
import sqlite3

from knowledge import identity
from maintenance import words

VERSION = 'learner-3'


def learn_subject(vault, subject, model, previous=None, *, goal='', max_steps=4, read_budget=18000):
    """Learn one subject from an already permitted vault, then review its guide.

    No filesystem, shell, web, recursive spawning or direct knowledge writes are tools.
    All article text is a proposal until exact-reference and separate semantic review.
    """
    if not 1 <= max_steps <= 8 or not 1000 <= read_budget <= 40000:
        raise ValueError('Learner bounds: 1–8 steps and 1,000–40,000 source characters')
    sources = vault['sources']
    claims = {c['id']: c for c in vault['claims'] if c['status'] == 'active' and c['id'] in subject['claims']}
    seed_sources = sorted({e['source'] for c in claims.values() for e in c.get('evidence', []) if e['source'] in sources})
    digest = identity([VERSION, subject['id'], subject['title'], list(claims.values()), goal, max_steps, read_budget,
                       [(i, identity(sources[i]['text']), sources[i]['role'], sources[i]['ts']) for i in seed_sources], subject.get('conflicts', [])])
    if previous and previous.get('input_hash') == digest and all(
            e['source'] in sources and e['source_hash'] == identity(sources[e['source']]['text']) and
            e.get('role') == sources[e['source']]['role'] and e.get('ts') == sources[e['source']]['ts']
            for e in previous.get('blocks', [])):
        return copy.deepcopy(previous)
    seed = []
    for claim in claims.values():
        candidate = {k: claim[k] for k in ('id', 'text', 'kind', 'scope')}
        candidate['source_roles'] = sorted({sources[e['source']]['role'] for e in claim.get('evidence', []) if e['source'] in sources})
        if len(json.dumps(seed+[candidate])) <= 14000:
            seed.append(candidate)
    shown = {c['id'] for c in seed}
    db = sqlite3.connect(':memory:')
    db.execute('CREATE VIRTUAL TABLE source_text USING fts5(source UNINDEXED, body)')
    db.executemany('INSERT INTO source_text VALUES(?,?)', [(i, s['text']) for i, s in sources.items()])
    blocks, trace, used, draft = {}, [], 0, None

    def block(source_id, start, length):
        nonlocal used
        if source_id not in sources or used >= read_budget:
            return {'error': 'Unknown permitted source or source-read budget exhausted'}
        text = sources[source_id]['text']
        start = max(0, min(start, len(text)))
        length = min(max(1, length), 4000, read_budget-used)
        quote = text[start:start+length]
        if not quote:
            return {'error': 'No text at that position'}
        key = identity([source_id, start, quote])
        if key not in blocks:
            used += len(quote)
            blocks[key] = {'id': key, 'source': source_id, 'quote': quote, 'span_start': start,
                           'span_end': start+len(quote), 'source_hash': identity(text),
                           'role': sources[source_id]['role'], 'ts': sources[source_id]['ts']}
        return blocks[key]

    try:
        for step in range(max_steps):
            response = model('learner_step', {
                'subject': {'id': subject['id'], 'title': subject['title'], 'scope': subject['scope'],
                            'questions': [q['question'] for q in subject.get('questions', [])][:5],
                            'conflicts': subject.get('conflicts', [])},
                'goal': goal, 'claims': seed, 'omitted_claims': len(claims)-len(seed),
                'source_catalog': [{'id': i, 'role': sources[i]['role'], 'ts': sources[i]['ts'],
                                    'characters': len(sources[i]['text'])} for i in seed_sources[:80]],
                'notes': [{'id': n['id'], 'title': n['title']} for n in vault['notes'] if n['id'] in subject['notes']],
                'blocks': list(blocks.values()), 'trace': trace,
                'remaining_steps': max_steps-step, 'remaining_source_characters': read_budget-used})
            action = response.get('action')
            if action == 'write':
                draft = response
                break
            if action == 'read':
                source_id = response.get('source')
                start, length = response.get('start', 0), response.get('length', 3000)
                if not isinstance(source_id, str) or not isinstance(start, int) or not isinstance(length, int):
                    observation = {'error': 'Read requires a permitted source ID and integer span'}
                else:
                    item = block(source_id, start, length)
                    observation = {'block': item.get('id'), 'error': item.get('error')}
            elif action == 'search':
                query = response.get('query', '')
                terms = words(query)[:12] if isinstance(query, str) else []
                hits = db.execute('SELECT source,body FROM source_text WHERE source_text MATCH ? ORDER BY bm25(source_text),source LIMIT 5',
                                  (' OR '.join('"'+t+'"' for t in terms),)).fetchall() if terms else []
                observation = {'query': query, 'hits': []}
                for source_id, text in hits:
                    positions = [text.casefold().find(t) for t in sorted(terms, key=len, reverse=True) if t in text.casefold()]
                    item = block(source_id, max(0, (positions[0] if positions else 0)-100), 500)
                    if item.get('id'):
                        observation['hits'].append({'source': source_id, 'block': item['id']})
            else:
                observation = {'error': 'Only search, read and write are supported'}
            trace.append({'step': step+1, 'action': action, 'observation': observation})
    finally:
        db.close()
    sections = []
    for section in (draft or {}).get('sections', [])[:8]:
        paragraphs = []
        for raw in section.get('paragraphs', [])[:3]:
            refs, source_blocks, text = raw.get('claims', []), raw.get('blocks', []), raw.get('text')
            if not isinstance(text, str) or not text.strip() or (not refs and not source_blocks):
                continue
            if any(i not in shown for i in refs) or any(i not in blocks for i in source_blocks):
                continue
            if re.search(r'\b(?:the owner|the user|the agent) (?:asked|said|requested|instructed|wants)\b', text, re.I):
                continue
            paragraphs.append({'text': text, 'claims': list(dict.fromkeys(refs)),
                               'evidence': [blocks[i] for i in dict.fromkeys(source_blocks)]})
        if paragraphs:
            sections.append({'heading': str(section.get('heading', '')), 'paragraphs': paragraphs})
    paragraphs = [p for s in sections for p in s['paragraphs']]
    if paragraphs:
        cited_claims = {cid for p in paragraphs for cid in p['claims']}
        cited_blocks = {e['id'] for p in paragraphs for e in p['evidence']}
        review = model('review_guide', {'subject': subject['title'],
                       'conflicts': subject.get('conflicts', []),
                       'claims': [c for c in seed if c['id'] in cited_claims],
                       'blocks': [b for b in blocks.values() if b['id'] in cited_blocks],
                       'paragraphs': [{'id': f'P{i}', 'text': p['text'], 'claims': p['claims'],
                                       'blocks': [e['id'] for e in p['evidence']]} for i, p in enumerate(paragraphs)]})
        checks = {c.get('id'): c for c in review.get('checks', [])}
        accepted = {id(p) for i, p in enumerate(paragraphs) if checks.get(f'P{i}', {}).get('supported') is True and checks.get(f'P{i}', {}).get('useful') is True}
        for p in paragraphs:
            p.update({'derivation': 'subject_guide', 'review': 'separate same-model support and reference-usefulness checks', 'depends_on': list(p['claims'])})
        sections = [{**s, 'paragraphs': [p for p in s['paragraphs'] if id(p) in accepted]}
                    for s in sections if any(id(p) in accepted for p in s['paragraphs'])]
    return {'subject': subject['id'], 'input_hash': digest, 'state': 'reviewed' if sections else 'insufficient_evidence',
            'sections': sections, 'blocks': list(blocks.values()), 'trace': trace,
            'gaps': [q for q in (draft or {}).get('gaps', []) if isinstance(q, str)][:5],
            'source_characters': used, 'steps': len(trace)+(1 if draft else 0),
            'limitation': 'Read-only bounded learner; same-model review and partial source coverage, no new canonical claims.'}

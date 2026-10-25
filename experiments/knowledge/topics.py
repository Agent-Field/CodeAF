"""Stable concept registry with host-reviewed reversible changes and reading maps."""
import copy
from collections import defaultdict
from knowledge import identity
from learning import project


def reconcile(learning, previous=None, *, decisions=()):
    """Keep identities separate from labels. Decisions must come from the trusted host.

    A model proposal is not a decision. The caller authenticates reviewed_by before
    passing it here. Undo is deliberately last-in-first-out; it never edits sources.
    """
    state = copy.deepcopy(previous or {'concepts': {}, 'redirects': {}, 'history': [], 'applied': []})
    prior_ids = set(state['concepts'])
    matched = set()
    for subject in learning['subjects']:
        matches = [c for c in state['concepts'].values() if c['state']=='active' and subject['id'] in c['source_subjects']]
        if not matches:
            matches = [c for c in state['concepts'].values() if c['id'] in prior_ids and c['id'] not in matched and c['state'] == 'active' and
                       c['scope'].casefold() == subject['scope'].casefold() and set(c['notes']) == set(subject['notes'])]
        if len(matches) == 1 and not matches[0].get('membership_locked'):
            concept = matches[0]
            matched.add(concept['id'])
            concept['source_subjects'] = sorted(set(concept['source_subjects'] + [subject['id']]))
            concept['notes'] = sorted(set(concept['notes'] + subject['notes']))
        elif not matches and subject['id'] not in state['concepts']:
            state['concepts'][subject['id']] = {
                'id': subject['id'], 'title': subject['title'], 'scope': subject['scope'],
                'notes': list(subject['notes']), 'source_subjects': [subject['id']],
                'aliases': list(subject.get('aliases', [])), 'state': 'active'}
    for decision in decisions:
        did = decision.get('id')
        if not isinstance(did, str) or not did or not decision.get('reviewed_by'):
            raise ValueError('Authenticated host decision ID/reviewer required')
        if did in state['applied']:
            continue
        before = {k: copy.deepcopy(state[k]) for k in ('concepts', 'redirects')}
        concepts, op = state['concepts'], decision.get('op')
        if op == 'undo':
            if not state['history'] or state['history'][-1]['id'] != decision.get('target'):
                raise ValueError('Undo must name the latest structural decision')
            prior = state['history'].pop()
            state.update(copy.deepcopy(prior['before']))
            state['applied'].append(did)
            continue
        target = decision.get('target', decision.get('into'))
        if target not in concepts or concepts[target]['state'] != 'active':
            raise ValueError('Decision target must be an active concept')
        current = concepts[target]
        if op == 'rename':
            title = decision.get('title')
            if not isinstance(title, str) or not title.strip():
                raise ValueError('Nonempty title required')
            current['aliases'] = sorted(set(current['aliases'] + [current['title']]))
            current['title'] = title.strip()
        elif op == 'merge':
            ids = decision.get('from', [])
            if not ids or target in ids or len(set(ids)) != len(ids):
                raise ValueError('Distinct merge sources required')
            if any(i not in concepts or concepts[i]['state'] != 'active' or concepts[i]['scope'].casefold() != current['scope'].casefold() for i in ids):
                raise ValueError('Merge sources must be active in the same semantic scope')
            for cid in ids:
                other = concepts[cid]
                for key in ('notes', 'source_subjects', 'aliases'):
                    current[key] = sorted(set(current[key] + other[key]))
                current['aliases'] = sorted(set(current['aliases'] + [other['title']]))
                other['state'] = 'retired'
                state['redirects'][cid] = [target]
        elif op == 'split':
            children = decision.get('children', [])
            members = [n for child in children for n in child.get('notes', [])]
            if len(children) < 2 or len(set(members)) != len(members) or set(members) != set(current['notes']):
                raise ValueError('Split must partition all member notes without overlap')
            child_ids = []
            for child in children:
                if not isinstance(child.get('title'), str) or not child['title'].strip() or not child['notes']:
                    raise ValueError('Each split child needs a title and member notes')
                cid = identity(['concept-child', target, sorted(child['notes'])])
                if cid in concepts:
                    raise ValueError('Split identity already exists; undo or review instead')
                concepts[cid] = {**copy.deepcopy(current), 'id': cid, 'title': child['title'],
                                 'notes': sorted(child['notes']), 'aliases': [], 'membership_locked': True}
                child_ids.append(cid)
            current['state'] = 'retired'
            state['redirects'][target] = child_ids
        else:
            raise ValueError('Unknown structural decision')
        state['history'].append({'id': did, 'op': op, 'reviewed_by': decision['reviewed_by'], 'before': before})
        state['applied'].append(did)
    # Structural undo restores labels/partitions, not an older knowledge snapshot.
    # Refresh unlocked memberships from current inputs after every decision.
    current_sources={s['id']:s for s in learning['subjects']}
    for concept in state['concepts'].values():
        if concept['state']=='active' and not concept.get('membership_locked'):
            incoming=[n for sid in concept['source_subjects']if sid in current_sources for n in current_sources[sid]['notes']]
            concept['notes']=sorted(set(concept['notes']+incoming))
    return state


def publish(vault, learning, registry, *, as_of=None, stale_after_seconds=None):
    """Project active concepts plus scope reading maps; contains edges are navigation."""
    by_id = {s['id']: s for s in learning['subjects']}
    original_notes = {n['id']: n for n in vault['notes']}
    subjects = []
    for concept in registry['concepts'].values():
        if concept['state'] != 'active':
            continue
        parts = [by_id[i] for i in concept['source_subjects'] if i in by_id]
        if not parts:
            continue
        notes = [n for n in concept['notes'] if n in original_notes]
        claims = list(dict.fromkeys(c for n in notes for c in original_notes[n]['claims']))
        allowed, sections, seen = set(claims), [], set()
        for part in parts:
            for section in part['sections']:
                paragraphs = []
                for paragraph in section['paragraphs']:
                    key = identity([paragraph['text'], paragraph.get('claims', []), paragraph.get('evidence', [])])
                    if key not in seen and (not concept.get('membership_locked') or
                            (paragraph.get('claims') and set(paragraph['claims']) <= allowed)):
                        paragraphs.append(copy.deepcopy(paragraph))
                        seen.add(key)
                if paragraphs:
                    sections.append({**section, 'paragraphs': paragraphs})
        questions = [copy.deepcopy(q) for part in parts for q in part.get('questions', [])
                     if not concept.get('membership_locked') or set(q['claims']) <= allowed]
        conflicts = [copy.deepcopy(c) for part in parts for c in part.get('conflicts', []) if set(c['claims']) <= allowed]
        subjects.append({**copy.deepcopy(parts[0]), 'id': concept['id'], 'title': concept['title'],
                         'scope': concept['scope'], 'aliases': concept['aliases'], 'notes': notes,
                         'claims': claims, 'sections': sections, 'questions': questions, 'conflicts': conflicts,
                         'state':parts[0]['state'] if sections else 'needs_evidence',
                         'editorial_pending_claims': sorted({cid for part in parts for cid in part.get('editorial_pending_claims', []) if cid in allowed})})
    result = project(vault, {'subjects': subjects}, as_of=as_of, stale_after_seconds=stale_after_seconds)
    groups = defaultdict(list)
    for note in result['notes']:
        if note.get('kind') == 'subject':
            groups[note['scope'].casefold()].append(note)
    for members in groups.values():
        if len(members) < 2:
            continue
        scope = sorted(n['scope'] for n in members)[0]
        map_id = identity(['scope-map', scope.casefold()])
        if any(n['id'] == map_id for n in result['notes']):
            raise ValueError('Map identity collides with existing knowledge')
        result['notes'].append({'id': map_id, 'kind': 'map', 'title': scope + ' — topic map',
                                'scope': scope, 'folder': [scope], 'claims': [], 'sections': [],
                                'links': [{'id': n['id'], 'kind': 'contains', 'reason': 'Subject in this reading collection; not a factual relationship'}
                                          for n in sorted(members, key=lambda n: n['title'].casefold())]})
        for member in members:
            member['links'].append({'id': map_id, 'kind': 'index', 'reason': 'Browse the wider topic collection'})
    result['topic_registry'] = copy.deepcopy(registry)
    return result

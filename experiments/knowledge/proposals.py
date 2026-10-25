"""Collect optional working-agent learning deltas without another model call."""
import copy
import re

from knowledge import identity


def collect(envelope, committed_response, permitted_sources, partition, previous=None, *, claim_ids=()):
    """Validate and stage an optional sidecar after the normal response is committed.

    The caller supplies trusted owner/project keys and already permitted sources.
    Neither schema validation nor a matching quotation establishes semantic truth.
    """
    if set(partition) != {'owner', 'project'} or not all(isinstance(v, str) and v for v in partition.values()):
        raise ValueError('Host must supply proven owner/project partition keys')
    proposals = copy.deepcopy((previous or {}).get('proposals', []))
    if any(p['partition'] != partition for p in proposals):
        raise ValueError('Do not mix proposal outboxes from different host partitions')
    sources = {**permitted_sources, committed_response['id']: committed_response}
    if committed_response['id'] in permitted_sources and permitted_sources[committed_response['id']] != committed_response:
        raise ValueError('Committed response changed under an immutable source ID')
    rejected, new, duplicates = [], 0, 0
    delta = envelope.get('knowledge_delta', []) if isinstance(envelope, dict) else []
    if not isinstance(delta, list):
        rejected.append({'reason': 'Optional knowledge_delta must be a list'})
        delta = []
    by_id = {p['id']: p for p in proposals}
    for index, raw in enumerate(delta):
        if index >= 4:
            rejected.append({'index': index, 'reason': 'At most four proposals per checkpoint'})
            continue
        if not isinstance(raw, dict) or raw.get('op') not in ('learn', 'correct', 'procedure', 'question'):
            rejected.append({'index': index, 'reason': 'Unknown proposal operation'})
            continue
        text, subject = raw.get('text'), raw.get('subject')
        if not isinstance(text, str) or not 1 <= len(text.strip()) <= 1000 or not isinstance(subject, str) or not 1 <= len(subject.strip()) <= 120:
            rejected.append({'index': index, 'reason': 'Missing or oversized text/subject'})
            continue
        target = raw.get('target') if raw['op'] == 'correct' else None
        if raw['op'] == 'correct' and target not in claim_ids:
            rejected.append({'index': index, 'reason': 'Correction must name a known claim; it is still only a proposal'})
            continue
        raw_evidence = raw.get('evidence')
        if not isinstance(raw_evidence, list) or not 1 <= len(raw_evidence) <= 4:
            rejected.append({'index': index, 'reason': 'Proposal needs one to four evidence spans'})
            continue
        evidence = []
        for item in raw_evidence:
            if not isinstance(item, dict):
                break
            source_id = committed_response['id'] if item.get('source') == 'self' else item.get('source')
            if not isinstance(source_id, str):
                break
            source, quote = sources.get(source_id), item.get('quote')
            if not source or not isinstance(quote, str) or not quote or quote not in source['text']:
                break
            start = source['text'].index(quote)
            evidence.append({'source': source_id, 'quote': quote, 'span_start': start, 'span_end': start+len(quote),
                             'role': source['role'], 'source_hash': identity(source['text'])})
        if len(evidence) != len(raw_evidence):
            rejected.append({'index': index, 'reason': 'Evidence absent from permitted committed sources'})
            continue
        normalized = re.sub(r'\s+', ' ', text.strip()).casefold()
        proposal_id = identity([partition, raw['op'], normalized, target])
        if proposal_id in by_id:
            existing = by_id[proposal_id]
            existing['evidence'] += [e for e in evidence if e not in existing['evidence']]
            duplicates += 1
            continue
        proposal = {'id': proposal_id, 'partition': dict(partition), 'op': raw['op'], 'text': text.strip(),
                    'subject_hint': subject.strip(), 'evidence': evidence, 'target': target,
                    'state': 'pending_semantic_review'}
        proposals.append(proposal)
        by_id[proposal_id] = proposal
        new += 1
    return {'proposals': proposals, 'rejected': rejected,
            'receipt': {'new': new, 'duplicates': duplicates, 'rejected': len(rejected), 'provider_calls': 0,
                        'limitation': 'Staged only. Exact quotes and schema checks do not prove truth.'}}

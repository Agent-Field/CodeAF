"""Host-assigned provenance and temporal metadata, never model-certified truth."""
import copy
from knowledge import identity, instant

KINDS = {'user_statement', 'agent_synthesis', 'repository_snapshot', 'document_fetch', 'command_result', 'legacy_unknown'}
PRIMARY_ARTIFACTS = {'repository_snapshot', 'document_fetch', 'command_result'}


def record(message, *, partition, kind, recorded_at, valid_at=None, locator=None, revision=None):
    """Register an already acquired source; the trusted host supplies all metadata.

    This does not fetch URLs, execute commands or independently verify content.
    Calling code must authenticate the partition and artifact origin first.
    """
    if set(partition) != {'owner', 'project'} or not all(isinstance(v, str) and v for v in partition.values()):
        raise ValueError('Host owner/project partition required')
    if kind not in KINDS or not isinstance(message.get('text'), str) or not message.get('id'):
        raise ValueError('Known source kind and immutable source ID/text required')
    instant(recorded_at)
    instant(valid_at or message['ts'])
    if kind == 'user_statement' and message['role'] != 'user':
        raise ValueError('An agent response cannot be registered as a human statement')
    if kind in PRIMARY_ARTIFACTS and not locator:
        raise ValueError('Primary artifact requires its host-recorded locator')
    if kind == 'repository_snapshot' and not revision:
        raise ValueError('Repository snapshot requires an exact revision')
    result = {k: copy.deepcopy(message[k]) for k in ('id', 'session', 'role', 'ts', 'text', 'seq') if k in message}
    result['provenance'] = {'kind': kind, 'partition': dict(partition), 'content_hash': identity(message['text']),
                            'recorded_at': recorded_at, 'valid_at': valid_at or message['ts'],
                            'locator': locator, 'revision': revision, 'independently_verified': False}
    return result


def basis(sources, evidence, *, as_of=None, stale_after_seconds=None):
    """Describe provenance and optional age; primary artifacts are not truth scores."""
    ids = sorted({e['source'] for e in evidence if e['source'] in sources})
    kinds, observed, primary = set(), [], []
    for sid in ids:
        source = sources[sid]
        provenance = source.get('provenance', {})
        kind = provenance.get('kind', 'user_statement' if source['role'] == 'user' else 'legacy_unknown')
        kinds.add(kind)
        observed.append(provenance.get('valid_at', source['ts']))
        if kind in PRIMARY_ARTIFACTS:
            primary.append(sid)
    stale = None
    if as_of is not None and stale_after_seconds is not None:
        if stale_after_seconds < 0:
            raise ValueError('Nonnegative freshness horizon required')
        stale = any((instant(as_of)-instant(t)).total_seconds() > stale_after_seconds for t in observed)
    return {'source_kinds': sorted(kinds), 'primary_artifact_sources': primary,
            'observed_from': min(observed, key=instant) if observed else None,
            'observed_to': max(observed, key=instant) if observed else None,
            'age_exceeds_policy': stale, 'independently_verified': False}

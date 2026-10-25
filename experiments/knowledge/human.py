"""Durable human page overrides, applied after generated knowledge publication."""
import copy
import hashlib
import json
from pathlib import Path


def overrides(directory):
    result = {}
    directory = Path(directory)
    if not directory.exists():
        return result
    for path in sorted(directory.glob('*.json')):
        value = json.loads(path.read_text())
        identifier = value.get('id', '')
        if not identifier or path.name != hashlib.sha256(identifier.encode()).hexdigest() + '.json':
            raise ValueError('Knowledge override identity mismatch')
        result[identifier] = value
    return result


def apply(vault, directory):
    changes = overrides(directory)
    if not changes:
        return vault
    result = copy.deepcopy(vault)
    kept = []
    suppressed_claims = set()
    claims = {claim['id']: claim for claim in result['claims']}
    for note in result['notes']:
        change = changes.get(note['id'])
        if change:
            suppressed_claims.update(note.get('original_claims', note.get('claims', [])))
            if change.get('deleted'):
                continue
            original = note.get('original_evidence', [])
            seen = {(e['source'], e['quote']) for e in original}
            for claim_id in note.get('claims', []):
                for evidence in claims.get(claim_id, {}).get('evidence', []):
                    key = evidence['source'], evidence['quote']
                    if key not in seen:
                        original.append(copy.deepcopy(evidence))
                        seen.add(key)
            for section in note.get('sections', []):
                for paragraph in section.get('paragraphs', []):
                    for evidence in paragraph.get('evidence', []):
                        key = evidence['source'], evidence['quote']
                        if key not in seen:
                            original.append(copy.deepcopy(evidence))
                            seen.add(key)
            note['original_evidence'] = original
            note['original_claims'] = note.get('original_claims', note.get('claims', []))
            note['human_edited'] = True
            note['human_edited_at'] = change.get('updated_at', '1970-01-01T00:00:00Z')
            note['links'] = []
            note['backlinks'] = []
            note['manual_markdown'] = change.get('markdown', '')
            note['claims'] = []
            note['sections'] = [{'heading': '', 'paragraphs': [
                {'text': note['manual_markdown'], 'claims': [], 'evidence': [], 'derivation': 'human_edit'}]}]
            note['needs_editorial_update'] = False
            note['learning_state'] = 'human_edited'
            for field in ('questions', 'conflicts', 'composition', 'article_rejections'):
                note.pop(field, None)
        kept.append(note)
    visible = {note['id'] for note in kept}
    references = set()
    for note in kept:
        references.update(note.get('claims', []))
        for section in note.get('sections', []):
            for paragraph in section.get('paragraphs', []):
                references.update(paragraph.get('claims', []))
                paragraph['inline_links'] = [link for link in paragraph.get('inline_links', []) if link['id'] in visible]
        note['links'] = [link for link in note.get('links', []) if link['id'] in visible]
        note['backlinks'] = [link for link in note.get('backlinks', []) if link['id'] in visible]
    result['notes'] = kept
    # A page deletion does not erase shared facts from other pages. Facts used
    # only by removed/generated prose must no longer leak through claim recall.
    result['claims'] = [claim for claim in result['claims'] if claim['id'] not in suppressed_claims or claim['id'] in references]
    claim_ids = {claim['id'] for claim in result['claims']}
    result['relations'] = [edge for edge in result.get('relations', []) if edge['from'] in claim_ids and edge['to'] in claim_ids]
    return result

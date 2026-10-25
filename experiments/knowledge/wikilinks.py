"""Disposable canonical mention links; original prose and evidence remain untouched."""
import copy
import re
from collections import defaultdict

# Existing markup is deliberately opaque: linking must not rewrite code or URLs.
_PROTECTED = re.compile(r'```[\s\S]*?```|~~~[\s\S]*?~~~|`+[^`]*`+|\[\[[^\]]*\]\]|!?\[[^\]]*\]\([^)]*\)|<[^>]*>|https?://\S+')


def link_vault(vault):
    """Return a projection with canonical spans and mention backlinks, at zero model cost.

    A label shared by multiple pages is never guessed. Matching is longest-first,
    case-insensitive and word-aware. Mentions are navigation, not factual edges.
    """
    result = copy.deepcopy(vault)
    labels = defaultdict(set)
    spellings = set()
    for note in result['notes']:
        for label in [note['title']] + note.get('aliases', []):
            if isinstance(label, str) and label.strip():
                labels[label.strip().casefold()].add(note['id'])
                spellings.add(label.strip())
    unique = {label: next(iter(ids)) for label, ids in labels.items() if len(ids) == 1}
    pattern = re.compile(r'(?<!\w)(?:' + '|'.join(re.escape(label) for label in sorted((label for label in spellings if label.casefold() in unique), key=lambda s: (-len(s), s))) + r')(?!\w)', re.IGNORECASE) if unique else None
    note_ids = {note['id'] for note in result['notes']}
    incoming = defaultdict(set)
    for note in result['notes']:
        note['links'] = [edge for edge in note.get('links', []) if edge.get('kind') != 'mention']
        targets = set()
        for section in note.get('sections', []):
            for paragraph in section.get('paragraphs', []):
                text = paragraph['text']
                protected = [(m.start(), m.end()) for m in _PROTECTED.finditer(text)]
                spans = []
                for token in _PROTECTED.finditer(text):
                    markup = token.group()
                    if not (markup.startswith('[[') and markup.endswith(']]')):
                        continue
                    target_label, separator, display = markup[2:-2].partition('|')
                    target_label = target_label.strip()
                    target = target_label if target_label in note_ids else unique.get(target_label.casefold())
                    if target and target != note['id']:
                        spans.append({'start': token.start(), 'end': token.end(), 'id': target,
                                      'label': display if separator else target_label})
                        targets.add(target)
                for match in pattern.finditer(text) if pattern else []:
                    target = unique.get(match.group().casefold())
                    if not target or target == note['id'] or any(match.start() < end and match.end() > start for start, end in protected):
                        continue
                    spans.append({'start': match.start(), 'end': match.end(), 'id': target})
                    targets.add(target)
                paragraph['inline_links'] = sorted(spans, key=lambda span: span['start'])
        existing = {edge['id'] for edge in note['links']}
        for target in sorted(targets):
            incoming[target].add(note['id'])
            if target not in existing:
                note['links'].append({'id': target, 'kind': 'mention', 'reason': 'Mentioned in the article; navigation only'})
    for note in result['notes']:
        note['backlinks'] = [{'id': source, 'kind': 'mention'} for source in sorted(incoming[note['id']])]
    return result


def markdown_text(paragraph):
    text = paragraph['text']
    for span in reversed(paragraph.get('inline_links', [])):
        start, end = span['start'], span['end']
        text = text[:start] + '[[' + span['id'] + '|' + span.get('label', text[start:end]) + ']]' + text[end:]
    return text

"""Human edit/delete behavior through published knowledge and cited recall."""
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from bridge import publish_overrides, compile_workspace
from knowledge import recall
from storage import save


class HumanPagesTests(unittest.TestCase):
    def fixture(self, root):
        vault = {'sources': {'s': {'id': 's', 'session': 'chat', 'role': 'user', 'ts': '2026-10-25T12:00:00Z', 'text': 'Original generated architecture.'}},
                 'claims': [{'id': 'c', 'scope': 'Project', 'kind': 'fact', 'status': 'active', 'text': 'Original generated architecture.', 'evidence': [{'source': 's', 'quote': 'Original generated architecture.'}]}],
                 'relations': [], 'notes': [
                     {'id': 'page', 'title': 'Architecture', 'scope': 'Project', 'folder': ['Project'], 'claims': ['c'], 'links': []},
                     {'id': 'other', 'title': 'Other', 'scope': 'Project', 'folder': ['Project'], 'claims': [], 'links': [{'id': 'page', 'kind': 'related', 'reason': 'Reading'}]}]}
        save(vault, root / 'knowledge')
        return vault

    def override(self, root, identifier, **value):
        directory = root / 'overrides'
        directory.mkdir(exist_ok=True)
        path = directory / (hashlib.sha256(identifier.encode()).hexdigest() + '.json')
        path.write_text(json.dumps({'id': identifier, **value}))

    def test_compiler_publication_preserves_manual_body_and_its_origin(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            generated = self.fixture(root)
            body = 'Corrected architecture uses [[other|Other]].'
            self.override(root, 'page', markdown=body)
            # Re-publishing original generated output cannot overwrite the edit.
            save(generated, root / 'knowledge')
            publish_overrides(root)
            published = json.loads((root / 'knowledge/vault.json').read_text())
            page = next(note for note in published['notes'] if note['id'] == 'page')
            self.assertTrue(page['human_edited'])
            self.assertEqual(page['manual_markdown'], body)
            self.assertEqual(page['claims'], [])
            self.assertEqual(page['sections'][0]['paragraphs'][0]['evidence'], [])
            self.assertEqual(page['original_evidence'][0]['source'], 's')
            self.assertIn(body, (root / 'knowledge/markdown/page.md').read_text())
            self.assertIn('not been verified', (root / 'knowledge/markdown/page.md').read_text())
            contexts = []
            def model(role, payload):
                self.assertEqual(role, 'recall')
                contexts.extend(payload['claims'])
                return {'answer': 'The human-edited note describes corrected architecture.', 'citations': [payload['claims'][0]['id']]}
            answer = recall(published, 'Corrected architecture', model)
            self.assertTrue(contexts[0]['human_edited'])
            self.assertEqual(answer['citations'][0]['origin'], 'human_edit')

    def test_deleted_identity_never_reappears_in_exports_or_recall(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            generated = self.fixture(root)
            self.override(root, 'page', deleted=True, markdown='Last human version')
            save(generated, root / 'knowledge')
            publish_overrides(root)
            save(generated, root / 'knowledge')
            published = json.loads((root / 'knowledge/vault.json').read_text())
            self.assertEqual([note['id'] for note in published['notes']], ['other'])
            self.assertEqual(published['notes'][0]['links'], [])
            self.assertFalse((root / 'knowledge/markdown/page.md').exists())
            self.assertEqual(published['sources'], generated['sources'])
            answer = recall(published, 'Original architecture', lambda *args: self.fail('Deleted facts reached model recall'))
            self.assertEqual(answer['claims'], [])

    def test_unchanged_compiler_fast_path_still_applies_new_human_override(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def no_model(*args):
                self.fail('Human-only publication made a model request')
            compile_workspace(root, '/project/a', force=True, model=no_model)
            self.fixture(root)
            self.override(root, 'page', markdown='Human-only publication changes architecture.')
            compile_workspace(root, '/project/a', force=True, model=no_model)
            published = json.loads((root / 'knowledge/vault.json').read_text())
            page = next(note for note in published['notes'] if note['id'] == 'page')
            self.assertEqual(page['manual_markdown'], 'Human-only publication changes architecture.')
            self.assertIn('Human-only publication changes architecture.', (root / 'knowledge/markdown/page.md').read_text())

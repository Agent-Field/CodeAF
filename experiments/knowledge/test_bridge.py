"""Binary bridge tests at saved-source → published knowledge seam."""
import json
from pathlib import Path
import tempfile
import unittest
from bridge import compile_workspace


class BridgeTests(unittest.TestCase):
    def test_durable_source_replay_does_not_call_model_again(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'queue').mkdir()
            source = {'id': 'message-1', 'session': 'chat-1', 'role': 'user',
                      'ts': '2026-10-25T12:00:00Z', 'text': 'hello', 'kind': 'user_statement'}
            (root / 'queue' / 'event.json').write_text(json.dumps(source))
            calls = []
            def model(role, payload):
                calls.append(role)
                return {'claims': [], 'notes': [], 'relations': [], 'aliases': []}
            first = compile_workspace(root, '/project/a', force=True, model=model)
            self.assertEqual(calls, ['update'])
            self.assertEqual(first['pending'], 0)
            self.assertNotIn('imported', first)
            self.assertFalse((root / 'queue' / 'event.json').exists())
            self.assertEqual(len(list((root / 'archive').glob('*/*.json'))), 1)
            def no_model(*args):
                self.fail('Unchanged source replay called model')
            from unittest.mock import patch
            with patch('bridge.Workspace.push', side_effect=AssertionError('Archive re-imported')):
                second = compile_workspace(root, '/project/a', force=True, model=no_model)
            self.assertEqual(second['pending'], 0)
            vault = json.loads((root / 'knowledge' / 'vault.json').read_text())
            self.assertEqual(vault['sources']['message-1']['provenance']['kind'], 'user_statement')

    def test_failed_extraction_keeps_pending_sources(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'queue').mkdir()
            source = {'id': 'message-1', 'session': 'chat-1', 'role': 'assistant',
                      'ts': '2026-10-25T12:00:00Z', 'text': 'A proposed design.', 'kind': 'agent_synthesis'}
            (root / 'queue' / 'event.json').write_text(json.dumps(source))
            def fail(*args):
                raise RuntimeError('private provider detail')
            with self.assertRaises(RuntimeError):
                compile_workspace(root, '/project/a', force=True, model=fail)
            status = json.loads((root / 'status.json').read_text())
            self.assertEqual(status['pending'], 1)
            self.assertNotIn('private provider detail', status['last_error'])
            self.assertEqual(len(list((root / 'archive').glob('*/*.json'))), 1)

    def test_composition_failure_retries_after_extraction_is_already_committed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'queue').mkdir()
            source = {'id': 'source', 'session': 'chat', 'role': 'user',
                      'ts': '2026-10-25T12:00:00Z', 'text': 'Cedar queues jobs.', 'kind': 'user_statement'}
            (root / 'queue' / 'source.json').write_text(json.dumps(source))
            def model(role, payload):
                if role == 'update':
                    return {'claims': [{'local_id': 'c', 'text': source['text'], 'topic': 'Queues',
                             'scope': 'Cedar', 'kind': 'fact', 'evidence': [{'source': 'source', 'quote': source['text']}]}],
                            'notes': [], 'relations': [], 'aliases': []}
                raise RuntimeError('Composition interrupted')
            with self.assertRaises(RuntimeError):
                compile_workspace(root, '/project/a', force=True, model=model)
            status = json.loads((root / 'status.json').read_text())
            self.assertEqual(status['pending'], 0)
            self.assertTrue(status['needs_update'])
            calls = []
            def resume(role, payload):
                calls.append(role)
                self.assertEqual(role, 'discover', 'Committed extraction repeated')
                return {'subjects': []}
            result = compile_workspace(root, '/project/a', force=True, model=resume)
            self.assertEqual(calls, ['discover'])
            self.assertFalse(result['needs_update'])
            self.assertEqual(result['last_error'], '')
            self.assertTrue((root / 'knowledge' / 'vault.json').exists())

    def test_selected_model_is_used_for_extraction_and_composition(self):
        import io
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'queue').mkdir()
            source = {'id': 'source', 'session': 'chat', 'role': 'user',
                      'ts': '2026-10-25T12:00:00Z', 'text': 'Cedar queues jobs.', 'kind': 'user_statement'}
            (root / 'queue' / 'source.json').write_text(json.dumps(source))
            requests = []
            def respond(request, **kwargs):
                body = json.loads(request.data)
                requests.append(body)
                if len(requests) == 1:
                    result = {'claims': [{'local_id': 'c', 'text': source['text'], 'topic': 'Queues',
                              'scope': 'Cedar', 'kind': 'fact', 'evidence': [{'source': 'source', 'quote': source['text']}]}],
                              'notes': [], 'relations': [], 'aliases': []}
                else:
                    result = {'subjects': []}
                return io.BytesIO(json.dumps({'model': 'example/knowledge-model',
                    'choices': [{'finish_reason': 'stop', 'message': {'content': json.dumps(result)}}]}).encode())
            with patch.dict('os.environ', {'OPENROUTER_API_KEY': 'fixture-only'}), patch('provider.urllib.request.urlopen', side_effect=respond):
                status = compile_workspace(root, '/project/a', force=True, model_id='example/knowledge-model')
            self.assertEqual(len(requests), 2)
            self.assertEqual([r['model'] for r in requests], ['example/knowledge-model', 'example/knowledge-model'])
            self.assertEqual(status['model'], 'example/knowledge-model')
            self.assertEqual(status['pending'], 0)
            self.assertFalse(status['needs_update'])

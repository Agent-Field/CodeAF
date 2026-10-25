"""A small learning agent reads permitted evidence and proposes reviewed subject guides."""
import unittest
from learner import learn_subject
from test_maintenance import fixture


class LearnerTests(unittest.TestCase):
    def test_the_agent_reads_before_writing_a_sourced_sectioned_guide(self):
        vault, learning = fixture()
        calls = []
        def model(role, payload):
            calls.append(role)
            if role == 'learner_step':
                if not payload['blocks']:
                    return {'action': 'read', 'source': 's2', 'start': 0, 'length': 2000}
                return {'action': 'write', 'sections': [
                    {'heading': 'Routing', 'paragraphs': [{'text': 'Cedar routes jobs.',
                     'claims': [vault['claims'][0]['id']], 'blocks': []}]},
                    {'heading': 'Failure recovery', 'paragraphs': [{'text': 'The reported recovery path retries twice, then uses the dead-letter queue.',
                     'claims': [], 'blocks': [payload['blocks'][0]['id']]}]}], 'gaps': [], 'reading_path': []}
            return {'checks': [{'id': 'P0', 'supported': True}, {'id': 'P1', 'supported': True}]}
        result = learn_subject(vault, learning['subjects'][0], model, max_steps=3)
        self.assertEqual(result['state'], 'reviewed')
        self.assertEqual(len(result['sections']), 2)
        self.assertEqual(result['sections'][1]['paragraphs'][0]['evidence'][0]['source'], 's2')
        self.assertEqual(result['trace'][0]['action'], 'read')
        self.assertEqual(calls, ['learner_step', 'learner_step', 'review_guide'])
        self.assertEqual(len(vault['claims']), 1)
        from learning import project
        from knowledge import recall
        learning['subjects'][0]['sections'] = result['sections']
        projected = project(vault, learning)
        self.assertEqual(len(projected['notes'][-1]['sections']), 2)
        def answer(role, payload):
            candidate = next(c for c in payload['claims'] if 'dead-letter' in c['text'])
            return {'answer': candidate['text'], 'citations': [candidate['id']]}
        recalled = recall(projected, 'Cedar dead-letter retry recovery', answer, scope='Cedar')
        self.assertEqual(recalled['citations'][0]['source'], 's2')
        cached = learn_subject(vault, learning['subjects'][0], lambda *args: self.fail('Cached guide called model'),
                               previous=result, max_steps=3)
        self.assertEqual(cached, result)

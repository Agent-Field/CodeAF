"""A small learning agent reads permitted evidence and proposes reviewed subject guides."""
import unittest
from learner import learn_subject
from test_maintenance import fixture


class LearnerTests(unittest.TestCase):
    def test_a_sourced_execution_receipt_does_not_become_reference_prose(self):
        vault, learning = fixture()
        cid = vault['claims'][0]['id']
        def model(role, payload):
            if role == 'learner_step':
                return {'action': 'write', 'sections': [{'heading': 'Purpose', 'paragraphs': [
                    {'text': 'Cedar routes jobs.', 'claims': [cid], 'blocks': []},
                    {'text': 'The current run is waiting for its next editing stage.', 'claims': [cid], 'blocks': []}]}]}
            return {'checks': [{'id': 'P0', 'supported': True, 'useful': True},
                               {'id': 'P1', 'supported': True, 'useful': False}]}
        guide = learn_subject(vault, learning['subjects'][0], model)
        self.assertEqual([p['text'] for s in guide['sections'] for p in s['paragraphs']], ['Cedar routes jobs.'])

    def test_a_source_authority_change_requires_review_instead_of_cached_prose(self):
        vault, learning = fixture()
        cid = vault['claims'][0]['id']
        def model(role, payload):
            if role == 'learner_step':
                return {'action': 'write', 'sections': [{'heading': 'Purpose', 'paragraphs': [
                    {'text': 'Cedar routes jobs.', 'claims': [cid], 'blocks': []}]}]}
            return {'checks': [{'id': 'P0', 'supported': payload['claims'][0]['source_roles'] == ['user'], 'useful': True}]}
        guide = learn_subject(vault, learning['subjects'][0], model)
        self.assertEqual(guide['state'], 'reviewed')
        vault['sources']['s1']['role'] = 'agent'
        revised = learn_subject(vault, learning['subjects'][0], model, previous=guide)
        self.assertEqual(revised['state'], 'insufficient_evidence')

    def test_retiring_an_uncited_claim_preserves_a_supported_guide_paragraph(self):
        import copy
        from learning import project
        vault, learning = fixture()
        cited = vault['claims'][0]['id']
        other = {**copy.deepcopy(vault['claims'][0]), 'id': 'unrelated', 'text': 'Cedar has an unrelated proposal.'}
        vault['claims'].append(other)
        subject = learning['subjects'][0]
        subject['claims'].append(other['id'])
        def model(role, payload):
            if role == 'learner_step':
                return {'action': 'write', 'sections': [{'heading': 'Purpose', 'paragraphs': [
                    {'text': 'Cedar routes jobs.', 'claims': [cited], 'blocks': []}]}]}
            return {'checks': [{'id': 'P0', 'supported': True, 'useful': True}]}
        guide = learn_subject(vault, subject, model)
        subject['sections'] = guide['sections']
        other['status'] = 'superseded'
        projection = project(vault, learning)
        self.assertEqual(projection['notes'][-1]['sections'][0]['paragraphs'][0]['text'], 'Cedar routes jobs.')
        vault['claims'][0]['status'] = 'superseded'
        self.assertEqual(project(vault, learning)['notes'][-1]['sections'], [])

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
            return {'checks': [{'id': 'P0', 'supported': True, 'useful': True}, {'id': 'P1', 'supported': True, 'useful': True}]}
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

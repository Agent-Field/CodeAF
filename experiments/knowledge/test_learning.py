"""Subject understanding is tested at the agreed sourced-knowledge interface."""
import unittest
from learning import learn


class LearningTests(unittest.TestCase):
    def test_oversized_emergent_subject_is_reconsidered_instead_of_truncating_evidence(self):
        claims = [{'id': str(i), 'text': f'Record {i}', 'kind': 'fact', 'status': 'active',
                   'scope': 'Cedar', 'evidence': []} for i in range(180)]
        vault = {'sources': {}, 'claims': claims, 'notes': [
            {'id': 'a', 'title': 'Routing', 'scope': 'Cedar', 'claims': [str(i) for i in range(90)], 'links': []},
            {'id': 'b', 'title': 'Recovery', 'scope': 'Cedar', 'claims': [str(i) for i in range(90,180)], 'links': []}]}
        def model(role, payload):
            if role == 'discover':
                if payload.get('partition_parent'):
                    return {'subjects': [{'title': n['title'], 'scope': 'Cedar', 'notes': [n['id']]} for n in payload['catalog']]}
                return {'subjects': [{'title': 'Cedar', 'scope': 'Cedar', 'notes': ['a','b']}]}
            return {'sections': [], 'questions': [], 'conflicts': [], 'checks': []}
        result = learn(vault, model)
        self.assertEqual({s['title'] for s in result['subjects']}, {'Routing', 'Recovery'})
        self.assertEqual({c for s in result['subjects'] for c in s['claims']}, {str(i) for i in range(180)})

    def test_curiosity_searches_other_subjects_before_leaving_a_question_open(self):
        vault = {'sources': {}, 'claims': [
            {'id': 'c', 'text': 'Cedar routes jobs.', 'kind': 'fact', 'status': 'active', 'scope': 'Cedar', 'evidence': []},
            {'id': 'd', 'text': 'Cedar routing failures retry jobs twice.', 'kind': 'fact', 'status': 'active', 'scope': 'Cedar', 'evidence': []}],
            'notes': [{'id': 'n', 'title': 'Routing', 'scope': 'Cedar', 'claims': ['c'], 'links': []},
                      {'id': 'm', 'title': 'Failure recovery', 'scope': 'Cedar', 'claims': ['d'], 'links': []}]}
        def model(role, payload):
            if role == 'discover':
                return {'subjects': [{'title': 'Cedar routing', 'scope': 'Cedar', 'notes': ['n']}]}
            if role == 'understand':
                return {'sections': [], 'questions': [{'question': 'How are Cedar routing failures handled?',
                        'claims': ['c'], 'search_terms': ['Cedar', 'routing', 'failures']}], 'conflicts': []}
            if role == 'resolve_questions':
                return {'matches': [{'id': 'Q0', 'claims': ['d'], 'relation': 'answers', 'reason': 'Two retries.'}]}
            return {'checks': [], 'questions': [{'id': 'Q0', 'grounded': True}]}
        result = learn(vault, model)
        question = result['subjects'][0]['questions'][0]
        self.assertEqual(question['state'], 'answer_candidate_found')
        self.assertEqual(question['related_claims'], ['d'])
        self.assertEqual(result['subjects'][0]['sections'], [])

    def test_semantically_unsupported_explanations_and_answered_questions_are_withheld(self):
        vault = {'sources': {}, 'claims': [{'id': 'c', 'text': 'Cedar is proposed.', 'kind': 'reported',
                 'status': 'active', 'scope': 'Cedar', 'evidence': []}],
                 'notes': [{'id': 'n', 'title': 'Cedar proposal', 'scope': 'Cedar', 'claims': ['c'], 'links': []}]}
        def model(role, payload):
            if role == 'discover':
                return {'subjects': [{'title': 'Cedar', 'scope': 'Cedar', 'notes': ['n']}]}
            if role == 'understand':
                return {'sections': [{'heading': 'Status', 'paragraphs': [{'text': 'Cedar is deployed.', 'claims': ['c']}]}],
                        'questions': [{'question': 'Is Cedar a proposal?', 'claims': ['c']}], 'conflicts': []}
            return {'checks': [{'id': 'P0', 'supported': False, 'reason': 'Proposal is not deployment.'}],
                    'questions': [{'id': 'Q0', 'grounded': False, 'reason': 'Already answered.'}]}
        subject = learn(vault, model)['subjects'][0]
        self.assertEqual(subject['sections'], [])
        self.assertEqual(subject['questions'], [])
        self.assertEqual(subject['state'], 'needs_evidence')

    def test_unfamiliar_subject_emerges_with_grounded_questions_and_cached_understanding(self):
        vault = {'sources': {'s': {'id': 's', 'role': 'user', 'ts': '2026-01-01', 'text': 'Cedar routes jobs.'}},
                 'claims': [{'id': 'c', 'text': 'Cedar routes jobs.', 'kind': 'fact', 'status': 'active',
                             'scope': 'Cedar', 'evidence': [{'source': 's', 'quote': 'Cedar routes jobs.'}]}],
                 'notes': [{'id': 'n', 'title': 'Routing', 'scope': 'Cedar', 'claims': ['c'], 'links': []}]}
        calls = []
        def model(role, payload):
            calls.append(role)
            if role == 'discover':
                return {'subjects': [{'title': 'Cedar job routing', 'scope': 'Cedar', 'notes': ['n'],
                                      'aliases': ['Cedar'], 'related': []}]}
            if role == 'understand':
                return {'sections': [{'heading': 'Purpose', 'paragraphs': [
                    {'text': 'Cedar routes jobs.', 'claims': ['c']},
                    {'text': 'Cedar has global failover.', 'claims': ['invented']}]}],
                    'questions': [{'question': 'How are routing failures handled?', 'why': 'Required to operate job routing.',
                                   'claims': ['c'], 'search_terms': ['Cedar', 'routing', 'failure']}],
                    'conflicts': [{'claims': ['c', 'invented'], 'reason': 'imaginary conflict'}]}
            return {'checks': [{'id': 'P0', 'supported': True}], 'questions': [{'id': 'Q0', 'grounded': True}]}
        result = learn(vault, model)
        subject = result['subjects'][0]
        self.assertEqual(subject['title'], 'Cedar job routing')
        self.assertEqual(subject['sections'][0]['paragraphs'][0]['claims'], ['c'])
        self.assertEqual(len(subject['sections'][0]['paragraphs']), 1)
        self.assertEqual(subject['questions'][0]['state'], 'unresolved_in_inspected_evidence')
        self.assertEqual(subject['conflicts'], [])
        self.assertEqual(learn(vault, model, previous=result), result)
        self.assertEqual(calls, ['discover', 'understand', 'review_understanding'])

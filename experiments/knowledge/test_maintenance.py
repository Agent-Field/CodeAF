"""Gap closure through the agreed conversation→knowledge and question→cited recall seams."""
import unittest
from knowledge import build, recall
from learning import project
from maintenance import maintain


def fixture():
    messages = [
        {'id': 's1', 'session': 'work', 'role': 'user', 'ts': '2026-10-24T10:00:00Z', 'text': 'Cedar routes jobs.'},
        {'id': 's2', 'session': 'work', 'role': 'agent', 'ts': '2026-10-24T10:01:00Z',
         'text': 'Cedar retries a failed routing attempt twice before putting the job in its dead-letter queue.'}]
    def extract(role, payload):
        return {'claims': [{'text': 'Cedar routes jobs.', 'topic': 'Routing', 'scope': 'Cedar', 'kind': 'fact',
                            'evidence': [{'source': 's1', 'quote': 'Cedar routes jobs.'}]}]}
    vault = build(messages, extract)
    cid = vault['claims'][0]['id']
    learning = {'version': 'learning-1', 'subjects': [{'id': 'cedar', 'title': 'Cedar routing', 'scope': 'Cedar',
                'notes': [vault['notes'][0]['id']], 'claims': [cid], 'sections': [], 'conflicts': [], 'state': 'explained',
                'questions': [{'question': 'What happens when Cedar routing fails?', 'why': 'Needed to operate routing.',
                               'claims': [cid], 'search_terms': ['Cedar', 'routing', 'failed']},
                              {'question': 'How are Cedar routing jobs configured?', 'claims': [cid],
                               'search_terms': ['Cedar', 'routing', 'configuration']}]}]}
    return vault, learning


class MaintenanceTests(unittest.TestCase):
    def test_rephrased_or_removed_questions_keep_their_previous_answer_history(self):
        vault, learning = fixture()
        first = maintain(vault, learning, lambda role, payload: {'selected': []}, budget=1)
        old_id = first['subjects'][0]['questions'][0]['id']
        learning['subjects'][0]['questions'].pop(0)
        revised = maintain(vault, learning, lambda role, payload: self.fail('Zero budget must not call a model'),
                           previous=first, budget=0)
        self.assertEqual(revised['retired_questions'][0]['id'], old_id)

    def test_agents_can_recall_the_new_understanding_with_original_source_citations(self):
        vault, learning = fixture()
        def model(role, payload):
            if role == 'frontier':
                return {'selected': [{'id': payload['questions'][0]['id']}]}
            if role == 'investigate':
                return {'answers': [{'question': payload['questions'][0]['id'], 'heading': 'Failure recovery',
                        'text': 'The report describes two retries before the dead-letter queue.', 'claims': [],
                        'evidence': [{'source': 's2', 'quote': vault['sources']['s2']['text']}]}]}
            return {'checks': [{'question': payload['answers'][0]['question'], 'supported': True, 'complete': True}]}
        maintained = maintain(vault, learning, model, budget=1)
        projection = project(vault, maintained)
        def answer(role, payload):
            matching = [c for c in payload['claims'] if 'dead-letter' in c['text']]
            if not matching:
                return {'answer': 'No answer found.', 'citations': [], 'abstain': True}
            return {'answer': 'The report describes two retries.', 'citations': [matching[0]['id']]}
        result = recall(projection, 'How do Cedar routing retries reach the dead-letter queue?', answer, scope='Cedar')
        self.assertEqual(result['citations'][0]['source'], 's2')
        self.assertEqual(len(vault['claims']), 1)

    def test_review_can_assess_the_whole_question_rather_than_an_opaque_identifier(self):
        vault, learning = fixture()
        def model(role, payload):
            if role == 'frontier':
                return {'selected': [{'id': payload['questions'][0]['id']}]}
            if role == 'investigate':
                return {'answers': [{'question': payload['questions'][0]['id'], 'text': 'The report describes two retries.',
                        'claims': [], 'evidence': [{'source': 's2', 'quote': vault['sources']['s2']['text']}]}]}
            questions = {q['id']: q['question'] for q in payload.get('questions', [])}
            return {'checks': [{'question': a['question'], 'supported': True,
                                'complete': questions.get(a['question']) == 'What happens when Cedar routing fails?'}
                               for a in payload['answers']]}
        result = maintain(vault, learning, model, budget=1)
        self.assertEqual(result['maintenance']['answered'], 1)

    def test_supported_but_partial_evidence_does_not_close_a_question(self):
        vault, learning = fixture()
        def model(role, payload):
            if role == 'frontier':
                return {'selected': [{'id': payload['questions'][0]['id']}]}
            if role == 'investigate':
                return {'answers': [{'question': payload['questions'][0]['id'], 'text': 'A report mentions retries.',
                        'claims': [], 'evidence': [{'source': 's2', 'quote': 'retries'}]}]}
            return {'checks': [{'question': payload['answers'][0]['question'], 'supported': True, 'complete': False}]}
        result = maintain(vault, learning, model, budget=1)
        self.assertEqual(result['subjects'][0]['questions'][0]['state'], 'partially_answered')
        self.assertEqual(result['maintenance']['answered'], 0)

    def test_new_relevant_evidence_withholds_an_old_answer_until_it_passes_review_again(self):
        vault, learning = fixture()
        def model(role, payload):
            if role == 'frontier':
                return {'selected': [{'id': payload['questions'][0]['id']}]}
            if role == 'investigate':
                if any(s['source'] == 's3' for s in payload['sources']):
                    return {'answers': []}
                return {'answers': [{'question': payload['questions'][0]['id'], 'text': 'Reported behavior is two retries.',
                        'claims': [], 'evidence': [{'source': 's2', 'quote': vault['sources']['s2']['text']}]}]}
            return {'checks': [{'question': payload['answers'][0]['question'], 'supported': True, 'complete': True}]}
        first = maintain(vault, learning, model, budget=1)
        self.assertEqual(first['maintenance']['answered'], 1)
        def no_model(role, payload):
            self.fail('Unchanged or irrelevant evidence repeated a model request')
        vault['sources']['irrelevant'] = {'id': 'irrelevant', 'session': 'other', 'role': 'user',
                                         'ts': '2026-10-25', 'text': 'Bananas ripen quickly.'}
        unchanged = maintain(vault, learning, no_model, previous=first, budget=1)
        self.assertEqual(unchanged['maintenance']['provider_calls'], 0)
        vault['sources']['s3'] = {'id': 's3', 'session': 'work', 'role': 'user', 'ts': '2026-10-25',
                                  'text': 'Correction: Cedar does not retry routing failures.'}
        revised = maintain(vault, learning, model, previous=first, budget=1)
        question = revised['subjects'][0]['questions'][0]
        self.assertNotIn('answer', question)
        self.assertEqual(question['state'], 'blocked_by_evidence')
        self.assertEqual(len(question['history']), 1)

    def test_two_questions_can_use_different_spans_of_the_same_original_message(self):
        vault, learning = fixture()
        first = 'Cedar routing failures retry twice.'
        last = 'Cedar configuration uses a YAML file.'
        vault['sources']['s2']['text'] = first + ' filler' * 1000 + last
        def model(role, payload):
            if role == 'frontier':
                return {'selected': [{'id': q['id']} for q in payload['questions']]}
            if role == 'investigate':
                return {'answers': [{'question': q['id'], 'text': text, 'claims': [],
                        'evidence': [{'source': 's2', 'quote': text}]} for q, text in
                        zip(payload['questions'], [first, last])]}
            return {'checks': [{'question': a['question'], 'supported': True, 'complete': True} for a in payload['answers']]}
        result = maintain(vault, learning, model, budget=2)
        self.assertEqual(result['maintenance']['answered'], 2)

    def test_one_prioritized_question_finds_a_reviewed_answer_in_unextracted_source(self):
        vault, learning = fixture()
        calls = []
        def model(role, payload):
            calls.append(role)
            if role == 'frontier':
                return {'selected': [{'id': q['id'], 'reason': 'Needed for operation.'} for q in payload['questions']]}
            if role == 'investigate':
                question = payload['questions'][0]
                return {'answers': [{'question': question['id'], 'heading': 'Failure recovery',
                        'text': 'The available report describes two retries before a job enters the dead-letter queue.',
                        'claims': [], 'evidence': [{'source': 's2', 'quote': vault['sources']['s2']['text']}]}]}
            return {'checks': [{'question': payload['answers'][0]['question'], 'supported': True, 'complete': True}]}
        result = maintain(vault, learning, model, budget=1)
        answers = [q for s in result['subjects'] for q in s['questions'] if q['state'] == 'answered']
        self.assertEqual(len(answers), 1)
        self.assertEqual(answers[0]['answer']['evidence'][0]['source'], 's2')
        self.assertEqual(result['maintenance']['selected'], 1)
        self.assertEqual(result['maintenance']['backlog'], 1)
        self.assertEqual(len(vault['claims']), 1)
        self.assertNotIn('answer', learning['subjects'][0]['questions'][0])
        self.assertEqual(calls, ['frontier', 'investigate', 'review_answers'])

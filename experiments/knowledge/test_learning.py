"""Subject understanding is tested at the agreed sourced-knowledge interface."""
import unittest
from learning import learn, project


class LearningTests(unittest.TestCase):
    def test_subject_prose_exposes_source_authority_and_unresolved_conflicts(self):
        from test_maintenance import fixture
        from knowledge import identity
        vault, learning = fixture()
        subject = learning['subjects'][0]
        cid = subject['claims'][0]
        vault['claims'].append({**vault['claims'][0], 'id': 'other', 'text': 'Cedar routing is unresolved.'})
        subject['conflicts'] = [{'claims': [cid, 'other'], 'state': 'unresolved', 'reason': 'Candidate discrepancy'}]
        subject['sections'] = [{'heading': 'Purpose', 'paragraphs': [
            {'text': 'Cedar routes jobs.', 'claims': [cid]},
            {'text': 'A report describes two retries.', 'claims': [], 'derivation': 'subject_guide',
             'review': 'reviewed', 'evidence': [{'source': 's2', 'quote': vault['sources']['s2']['text'],
              'source_hash': identity(vault['sources']['s2']['text'])}]}]}]
        paragraphs = project(vault, learning)['notes'][-1]['sections'][0]['paragraphs']
        self.assertEqual(paragraphs[0]['evidence_basis']['source_roles'], ['user'])
        self.assertTrue(paragraphs[0]['evidence_basis']['unresolved_conflict'])
        self.assertEqual(paragraphs[1]['evidence_basis']['source_roles'], ['agent'])
        self.assertFalse(paragraphs[1]['evidence_basis']['independently_verified'])

    def test_subject_navigation_preserves_real_connections_and_withholds_retired_prose(self):
        vault = {'sources': {}, 'claims': [
            {'id': 'c', 'text': 'Routing selects a worker.', 'status': 'active', 'kind': 'fact'},
            {'id': 'd', 'text': 'Recovery requeues failed work.', 'status': 'active', 'kind': 'fact'},
            {'id': 'e', 'text': 'Old retry behavior.', 'status': 'superseded', 'kind': 'fact'}],
            'notes': [{'id': 'n', 'title': 'Routing', 'scope': 'Cedar', 'folder': ['Cedar','Routing'],
                       'claims': ['c'], 'links': [{'id': 'm', 'reason': 'Recovery requeues work rejected by routing.'}]},
                      {'id': 'm', 'title': 'Recovery', 'scope': 'Cedar', 'folder': ['Cedar','Recovery'],
                       'claims': ['d','e'], 'links': []}]}
        learning = {'subjects': [
            {'id': 'a', 'title': 'Cedar routing', 'scope': 'Cedar', 'notes': ['n'], 'claims': ['c'],
             'state': 'explained', 'sections': [], 'questions': [], 'conflicts': []},
            {'id': 'b', 'title': 'Cedar recovery', 'scope': 'Cedar', 'notes': ['m'], 'claims': ['d','e'],
             'state': 'explained', 'sections': [{'heading': 'Recovery', 'paragraphs': [
                 {'text': 'Old retry behavior.', 'claims': ['e']}]}], 'questions': [], 'conflicts': []}]}
        result = project(vault, learning)
        a, b = result['notes'][-2:]
        self.assertTrue(any(l['id']=='n' and l['kind']=='explains' for l in a['links']))
        self.assertTrue(any(l['id']=='b' and l['kind']=='related' and 'requeues' in l['reason'] for l in a['links']))
        self.assertEqual(b['sections'], [])
        self.assertEqual(b['learning_state'], 'needs_review')

    def test_a_subject_rename_keeps_its_identity_and_previous_name(self):
        vault = {'sources': {}, 'claims': [{'id': 'c', 'text': 'Cedar routes jobs.', 'kind': 'fact',
                 'status': 'active', 'scope': 'Cedar', 'evidence': []}],
                 'notes': [{'id': 'n', 'title': 'Routing', 'scope': 'Cedar', 'claims': ['c'], 'links': []}]}
        title = ['Cedar routing']
        def model(role, payload):
            if role == 'discover':
                return {'subjects': [{'title': title[0], 'scope': 'Cedar', 'notes': ['n'], 'aliases': []}]}
            return {'sections': [], 'questions': [], 'conflicts': [], 'checks': []}
        first = learn(vault, model)
        title[0] = 'Cedar delivery mechanism'
        vault['notes'][0]['title'] = 'Routing mechanism'
        second = learn(vault, model, previous=first)
        self.assertEqual(second['subjects'][0]['id'], first['subjects'][0]['id'])
        self.assertIn('Cedar routing', second['subjects'][0]['aliases'])
        self.assertEqual(second['subjects'][0]['title'], 'Cedar delivery mechanism')

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

    def test_incremental_subject_exposes_new_sourced_claims_without_rewriting_guide(self):
        import copy
        from workflow import extend
        from topics import reconcile, publish
        from test_maintenance import fixture
        vault, learning = fixture()
        subject = learning['subjects'][0]
        original = copy.deepcopy(subject['sections'])
        vault['sources']['new'] = {'id': 'new', 'session': 'chat', 'role': 'user',
            'ts': '2026-10-25T14:00:00Z', 'text': 'Cedar may retain jobs for seven days.'}
        claim = {**vault['claims'][0], 'id': 'new-claim', 'kind': 'proposal',
            'text': vault['sources']['new']['text'], 'evidence': [{'source': 'new', 'quote': vault['sources']['new']['text']}]}
        vault['claims'].append(claim)
        vault['notes'][0]['claims'].append(claim['id'])
        def no_model(*args):
            self.fail('Known subject update called model')
        updated = extend(vault, learning, no_model)
        self.assertEqual(updated['subjects'][0]['sections'], original)
        page = next(n for n in publish(vault, updated, reconcile(updated))['notes'] if n.get('kind') == 'subject')
        self.assertTrue(page['needs_editorial_update'])
        recent = next(s for s in page['sections'] if s['heading'] == 'Recent knowledge')
        self.assertEqual(len(recent['paragraphs']), 1)
        paragraph = recent['paragraphs'][0]
        self.assertEqual(paragraph['text'], '[proposal] Cedar may retain jobs for seven days.')
        self.assertEqual(paragraph['claims'], ['new-claim'])
        self.assertEqual(paragraph['evidence'][0]['source'], 'new')
        self.assertFalse(paragraph['evidence_basis']['independently_verified'])
        vault['claims'][-1]['status'] = 'superseded'
        retired = next(n for n in publish(vault, updated, reconcile(updated))['notes'] if n.get('kind') == 'subject')
        self.assertFalse(any(s['heading'] == 'Recent knowledge' for s in retired['sections']))
        self.assertFalse(retired['needs_editorial_update'])

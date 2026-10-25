"""Curiosity changes are checked through sourced notes and cited answers."""
import unittest
from test_maintenance import fixture
from maintenance import maintain


class CuriosityTests(unittest.TestCase):
    def test_closing_a_question_does_not_require_full_catalog_recuration(self):
        import copy
        from curiosity import curate
        vault,learning=fixture()
        first=curate(vault,learning,lambda role,payload:{'keep':[{'id':payload['questions'][0]['id'],'reason':'Needed to operate.'}]})
        changed=copy.deepcopy(first)
        changed['subjects'][0]['questions'][0]['state']='answered'
        closed=curate(vault,changed,lambda*a:self.fail('Closing a gap invoked global curation'),previous=first)
        self.assertEqual(closed['curation']['active'],0)
        self.assertEqual(closed['curation']['model_calls'],0)

    def test_large_question_sets_are_shortlisted_without_blocking_knowledge_publication(self):
        import copy
        from curiosity import curate
        vault,learning=fixture()
        base=learning['subjects'][0]
        learning['subjects']=[]
        for i in range(150):
            subject=copy.deepcopy(base);subject['id']='subject-'+str(i);subject['title']='Subject '+str(i)
            learning['subjects'].append(subject)
        def model(role,payload):
            self.assertLessEqual(len(payload['questions']),256)
            return {'keep':[]}
        result=curate(vault,learning,model)
        self.assertEqual(len(result['subjects']),150)
        self.assertGreater(result['curation']['not_inspected'],0)

    def test_a_new_matching_source_wakes_a_parked_question_and_reaches_cited_recall(self):
        import tempfile
        from workspace import Workspace
        from curiosity import wake
        from learning import project
        from knowledge import recall
        vault, learning = fixture()
        for i in range(4):
            vault['sources']['d'+str(i)] = {'id':'d'+str(i),'session':'old','role':'agent','ts':'2026-10-24T10:00:00Z',
                'text':'Cedar routing failed failure retries queue configuration.'}
        def unanswered(role,payload):
            if role=='frontier':return {'selected':[{'id':payload['questions'][0]['id']}]}
            return {'answers':[]}
        previous = maintain(vault,learning,unanswered,budget=1)
        for q in previous['subjects'][0]['questions']:q['curiosity']={'state':'parked'}
        with tempfile.TemporaryDirectory() as directory:
            work = Workspace(directory,{'owner':'person','project':'collection'})
            work.seed(vault)
            work.watch(previous)
            text = 'filler '*1000+'Cedar routing failures now enter a parking queue.'
            work.push({'id':'z','session':'new','role':'user','ts':'2026-10-25T10:00:00Z','text':text},
                      kind='user_statement',recorded_at='2026-10-25T10:00:00Z')
            revised = work.flush(lambda *a:{'claims':[],'notes':[],'relations':[],'aliases':[]},force=True)['vault']
            woken = wake(previous,work.wakeups(),revised)
            def close(role,payload):
                if role=='frontier':return {'selected':[{'id':payload['questions'][0]['id']}]}
                if role=='investigate':return {'answers':[{'question':payload['questions'][0]['id'],'heading':'Recovery',
                    'text':'Cedar routing failures now enter a parking queue.','claims':[],
                    'evidence':[{'source':'z','quote':'Cedar routing failures now enter a parking queue.'}]}]}
                return {'checks':[{'question':payload['answers'][0]['question'],'supported':True,'complete':True}]}
            closed = maintain(revised,woken,close,previous=previous,budget=1)
            self.assertEqual(closed['maintenance']['answered'],1)
            def answer(role,payload):
                claim = next(c for c in payload['claims'] if 'parking queue' in c['text'])
                return {'answer':claim['text'],'citations':[claim['id']]}
            result = recall(project(revised,closed),'Cedar parking queue routing failures',answer,scope='Cedar')
            self.assertEqual(result['citations'][0]['source'],'z')

    def test_only_consequential_questions_run_and_unchanged_curation_is_free(self):
        from curiosity import curate
        vault, learning = fixture()
        def model(role, payload):
            return {'keep': [{'id': payload['questions'][0]['id'], 'reason': 'Failure recovery is needed to operate routing.'}]}
        result = curate(vault, learning, model, limit_per_subject=1)
        active = [q for s in result['subjects'] for q in s['questions'] if q['curiosity']['state'] == 'active']
        self.assertEqual(len(active), 1)
        again = curate(vault, learning, lambda *a: self.fail('Unchanged curation called model'), previous=result, limit_per_subject=1)
        self.assertEqual(again['curation']['model_calls'], 0)
        def frontier(role, payload):
            self.assertEqual(role, 'frontier')
            self.assertEqual(len(payload['questions']), 1)
            return {'selected': []}
        maintained = maintain(vault, result, frontier, budget=1)
        self.assertEqual(maintained['maintenance']['selected'], 0)

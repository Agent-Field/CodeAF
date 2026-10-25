"""Full sourced-note publication replays without model work on unchanged evidence."""
import tempfile
import unittest
from pathlib import Path
from test_maintenance import fixture


class WorkflowTests(unittest.TestCase):
    def test_new_source_wakeups_do_not_expand_the_visible_question_budget(self):
        from workspace import Workspace
        from workflow import run
        vault,learning=fixture()
        subject=learning['subjects'][0]
        subject['questions'] += [{'question':'How does Cedar routing prioritize work?','claims':subject['claims'],'search_terms':['Cedar','routing']},
                                 {'question':'How does Cedar routing cancel work?','claims':subject['claims'],'search_terms':['Cedar','routing']}]
        with tempfile.TemporaryDirectory()as directory:
            root=Path(directory);partition={'owner':'person','project':'collection'}
            work=Workspace(root/'work',partition);work.seed(vault);work.watch(learning)
            work.push({'id':'new','session':'new','role':'user','ts':'2026-10-25T10:00:00Z',
                       'text':'Cedar routing background detail.'},kind='user_statement',recorded_at='2026-10-25T10:00:00Z')
            def model(role,payload):
                if role=='update':return {'claims':[],'notes':[],'relations':[],'aliases':[]}
                if role=='curate_questions':return {'keep':[{'id':q['id'],'reason':'Needed for operation.'}for q in payload['questions'][:2]]}
                return {'selected':[]}
            report=run(root/'work',partition,learning,root/'out',model,questions=1)
            self.assertEqual(report['curation']['active'],2)

    def test_a_published_output_directory_cannot_be_reused_for_another_owner(self):
        from workspace import Workspace
        from workflow import run
        vault,learning=fixture()
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            for owner in ['first','second']:
                Workspace(root/owner,{'owner':owner,'project':'collection'}).seed(vault)
            run(root/'first',{'owner':'first','project':'collection'},learning,root/'out',lambda*a:{'keep':[]},questions=0)
            with self.assertRaises(ValueError):
                run(root/'second',{'owner':'second','project':'collection'},learning,root/'out',lambda*a:{'keep':[]},questions=0)

    def test_unchanged_published_knowledge_does_not_reopen_completed_curiosity(self):
        from workspace import Workspace
        from workflow import run
        vault,learning = fixture()
        with tempfile.TemporaryDirectory() as directory:
            partition={'owner':'person','project':'collection'}
            Workspace(Path(directory)/'work',partition).seed(vault)
            def model(role,payload):
                if role=='curate_questions':return {'keep':[{'id':payload['questions'][0]['id'],'reason':'Operation requires recovery.'}]}
                if role=='frontier':return {'selected':[{'id':payload['questions'][0]['id']}]}
                if role=='investigate':return {'answers':[{'question':payload['questions'][0]['id'],'heading':'Recovery',
                    'text':'The report describes two retries then a dead-letter queue.','claims':[],
                    'evidence':[{'source':'s2','quote':vault['sources']['s2']['text']}]}]}
                return {'checks':[{'question':payload['answers'][0]['question'],'supported':True,'complete':True}]}
            first=run(Path(directory)/'work',partition,learning,Path(directory)/'out',model,questions=1)
            self.assertEqual(first['maintenance']['answered'],1)
            self.assertEqual(first['curation']['active'],0)
            again=run(Path(directory)/'work',partition,learning,Path(directory)/'out',
                      lambda*a:self.fail('Unchanged workflow invoked model'),questions=1)
            self.assertTrue(again['reused'])

"""Persistent capture is tested from committed sources to published sourced notes."""
import tempfile
import unittest


class WorkspaceTests(unittest.TestCase):
    def test_a_long_original_source_can_be_revisited_through_an_exact_bounded_window(self):
        from workspace import Workspace
        from test_maintenance import fixture
        vault,_=fixture()
        quote='Cedar retains jobs for seven days.'
        vault['sources']['s1']['text'] += ' filler'*5000+quote
        original=vault['sources']['s1']['text']
        with tempfile.TemporaryDirectory()as directory:
            work=Workspace(directory,{'owner':'person','project':'collection'})
            work.seed(vault)
            work.revisit(['s1'],windows={'s1':{'start':original.index(quote),'length':len(quote)}})
            def model(role,payload):
                self.assertEqual(payload['messages'][0]['text'],quote)
                return {'claims':[{'local_id':'x','text':quote,'topic':'Retention','scope':'Cedar','kind':'fact',
                        'evidence':[{'source':'s1','quote':quote}]},
                        {'local_id':'bad','text':'Unsupported routing inference.','topic':'Routing','scope':'Cedar','kind':'fact',
                         'evidence':[{'source':'s1','quote':'Cedar routes jobs.'}]}],'notes':[],'relations':[],'aliases':[]}
            result=work.flush(model,force=True)
            self.assertEqual(len(result['vault']['claims']),2)
            self.assertEqual(result['vault']['sources']['s1']['text'],original)

    def test_optional_correction_hints_keep_host_keys_out_of_the_compiler(self):
        from workspace import Workspace
        from test_maintenance import fixture
        vault,_=fixture()
        with tempfile.TemporaryDirectory() as directory:
            work=Workspace(directory,{'owner':'person','project':'internal-collection'})
            work.seed(vault)
            message={'id':'new','session':'a','role':'user','ts':'2026-10-25T10:00:00Z',
                     'text':'Cedar routes jobs. Cedar now keeps seven days.'}
            envelope={'knowledge_delta':[{'op':'correct','text':'Cedar keeps seven days.','subject':'Cedar',
                'target':vault['claims'][0]['id'],'evidence':[{'source':'self','quote':'Cedar routes jobs.'},
                                                          {'source':'self','quote':'Cedar now keeps seven days.'}]}]}
            work.push(message,kind='user_statement',recorded_at=message['ts'],envelope=envelope)
            def model(role,payload):
                self.assertEqual(len(payload['proposals']),1)
                self.assertNotIn('partition',payload['proposals'][0])
                self.assertNotIn('partition',payload['messages'][0]['provenance'])
                return {'claims':[],'notes':[],'relations':[],'aliases':[]}
            result=work.flush(model,force=True)
            self.assertEqual(len(result['vault']['claims']),1)
            self.assertEqual(result['receipt']['model_calls'],1)
            work.push(message,kind='user_statement',recorded_at=message['ts'],envelope=envelope)
            self.assertFalse(work.flush(lambda*a:self.fail('Identical sidecar replay called model'),force=True)['published'])

    def test_concurrent_compilers_do_not_double_publish_and_push_remains_available(self):
        import threading
        from concurrent.futures import ThreadPoolExecutor
        from workspace import Workspace
        with tempfile.TemporaryDirectory() as directory:
            partition = {'owner':'person','project':'collection'}
            work = Workspace(directory,partition)
            def push(i):
                work.push({'id':str(i),'session':str(i),'role':'user','ts':'2026-10-25T10:00:00Z',
                           'text':'Cedar setting '+str(i)+' is enabled.'},kind='user_statement',recorded_at='2026-10-25T10:00:00Z')
            push(1)
            entered, release = threading.Event(), threading.Event()
            def model(role,payload):
                entered.set()
                if not release.wait(5):raise RuntimeError('Test synchronization failed')
                return {'claims':[{'local_id':str(i),'text':m['text'],'topic':'Settings','scope':'Cedar','kind':'fact',
                    'evidence':[{'source':m['id'],'quote':m['text']}]}for i,m in enumerate(payload['messages'])],
                    'notes':[],'relations':[],'aliases':[]}
            with ThreadPoolExecutor(max_workers=1) as pool:
                pending = pool.submit(work.flush,model,force=True)
                self.assertTrue(entered.wait(5))
                contender = Workspace(directory,partition)
                self.assertFalse(contender.flush(lambda*a:self.fail('Second compiler called model'),force=True)['published'])
                push(2)
                release.set()
                self.assertEqual(len(pending.result()['vault']['claims']),1)
            result = work.flush(model,force=True)
            self.assertEqual(len(result['vault']['claims']),2)
            self.assertEqual(result['receipt']['publication_revision'],2)

    def test_an_omission_can_be_repaired_without_forging_a_new_source_identity(self):
        from workspace import Workspace
        with tempfile.TemporaryDirectory() as directory:
            work = Workspace(directory, {'owner':'person','project':'collection'})
            message = {'id':'s','session':'a','role':'user','ts':'2026-10-25T10:00:00Z',
                       'text':'Cedar uses SQLite. Cedar retains jobs for seven days.'}
            work.push(message,kind='user_statement',recorded_at=message['ts'])
            def model(text):
                return lambda role,payload: {'claims':[{'local_id':'c','text':text,'topic':'Storage','scope':'Cedar',
                    'kind':'fact','evidence':[{'source':'s','quote':text}]}],'notes':[],'relations':[],'aliases':[]}
            first = work.flush(model('Cedar uses SQLite.'),force=True)['vault']
            work.revisit(['s'], now='2026-10-25T10:01:00Z')
            revised = work.flush(model('Cedar retains jobs for seven days.'),force=True)['vault']
            self.assertEqual(len(revised['claims']),2)
            self.assertEqual(revised['sources'],first['sources'])
            self.assertTrue(all(c['evidence'][0]['source']=='s' for c in revised['claims']))

    def test_mixed_chats_publish_once_after_debounce_and_survive_a_failed_attempt(self):
        from workspace import Workspace
        partition = {'owner': 'person', 'project': 'collection'}
        with tempfile.TemporaryDirectory() as directory:
            work = Workspace(directory, partition)
            for sid, chat, text in [('s1', 'a', 'Cedar uses SQLite.'), ('s2', 'b', 'Cedar does not use Redis.')]:
                message = {'id': sid, 'session': chat, 'role': 'user', 'ts': '2026-10-25T10:00:00Z', 'text': text}
                work.push(message, kind='user_statement', recorded_at='2026-10-25T10:00:00Z')
            self.assertFalse(work.flush(lambda *a: self.fail('Early capture called model'), now='2026-10-25T10:00:10Z')['published'])
            with self.assertRaises(RuntimeError):
                work.flush(lambda *a: (_ for _ in ()).throw(RuntimeError('retryable')), now='2026-10-25T10:01:00Z')
            work = Workspace(directory, partition)
            calls = []
            def model(role, payload):
                calls.append(role)
                return {'claims': [{'local_id': str(i), 'text': m['text'], 'topic': 'Storage', 'scope': 'Cedar',
                        'kind': 'fact', 'evidence': [{'source': m['id'], 'quote': m['text']}]}
                        for i,m in enumerate(payload['messages'])], 'notes': [], 'relations': [], 'aliases': []}
            result = work.flush(model, now='2026-10-25T10:01:00Z')
            self.assertEqual(calls, ['update'])
            self.assertEqual(len(result['vault']['claims']), 2)
            self.assertTrue(result['vault']['notes'])
            self.assertFalse(work.flush(lambda *a: self.fail('Repeated capture called model'), force=True)['published'])
            other = Workspace(directory, {'owner': 'other', 'project': 'collection'})
            self.assertEqual(other.read()['claims'], [])
            with self.assertRaises(ValueError):
                work.push({'id':'s1','session':'a','role':'user','ts':'2026-10-25T10:00:00Z','text':'Changed'},
                          kind='user_statement', recorded_at='2026-10-25T10:00:00Z')

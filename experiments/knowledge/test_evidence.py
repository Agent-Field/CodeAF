"""Host provenance is observed through sourced knowledge and its reading projection."""
import unittest
from test_maintenance import fixture
from learning import project


class EvidenceTests(unittest.TestCase):
    def test_freshness_is_an_explicit_policy_signal_and_does_not_rewrite_facts(self):
        vault, learning = fixture()
        cid = vault['claims'][0]['id']
        learning['subjects'][0]['sections']=[{'heading':'Purpose','paragraphs':[{'text':'Cedar routes jobs.','claims':[cid]}]}]
        result = project(vault,learning,as_of='2026-10-26T10:00:00Z',stale_after_seconds=86400)
        paragraph = result['notes'][-1]['sections'][0]['paragraphs'][0]
        self.assertTrue(paragraph['evidence_basis']['age_exceeds_policy'])
        self.assertEqual(result['claims'],vault['claims'])
        self.assertEqual(len(result['notes'][-1]['sections']),1)

    def test_host_partition_identifiers_cannot_become_semantic_subjects(self):
        from evidence import record
        from knowledge import update_once
        source = record({'id':'s','session':'chat','role':'user','ts':'2026-10-25T10:00:00Z',
                         'text':'Cedar stores data locally.'},partition={'owner':'person','project':'internal-collection'},
                         kind='user_statement',recorded_at='2026-10-25T10:00:00Z')
        def model(role,payload):
            message = payload['messages'][0]
            scope = message.get('provenance',{}).get('partition',{}).get('project','Cedar')
            return {'claims':[{'local_id':'x','text':message['text'],'scope':scope,'topic':'Storage','kind':'fact',
                    'evidence':[{'source':'s','quote':message['text']}]}],'notes':[],'relations':[],'aliases':[]}
        vault = update_once([source],None,model)
        self.assertEqual(vault['claims'][0]['scope'],'Cedar')
        self.assertEqual(vault['sources']['s']['provenance']['partition']['project'],'internal-collection')

    def test_a_primary_snapshot_and_a_copied_report_remain_distinct_in_subject_prose(self):
        from evidence import record
        vault, learning = fixture()
        partition = {'owner': 'person', 'project': 'collection'}
        vault['sources']['s1'] = record(vault['sources']['s1'], partition=partition, kind='user_statement',
                                      recorded_at='2026-10-25T10:00:00Z')
        vault['sources']['s2'] = record(vault['sources']['s2'], partition=partition, kind='repository_snapshot',
                                      recorded_at='2026-10-25T10:00:00Z', valid_at='2026-10-24T10:00:00Z',
                                      locator='repository:cedar/routing', revision='abc123')
        cid = vault['claims'][0]['id']
        learning['subjects'][0]['sections'] = [{'heading': 'Purpose', 'paragraphs': [
            {'text': 'Cedar routes jobs.', 'claims': [cid]}]}]
        paragraph = project(vault, learning)['notes'][-1]['sections'][0]['paragraphs'][0]
        self.assertEqual(paragraph['evidence_basis']['source_kinds'], ['user_statement'])
        self.assertFalse(paragraph['evidence_basis']['independently_verified'])
        self.assertEqual(vault['sources']['s2']['provenance']['valid_at'], '2026-10-24T10:00:00Z')
        forged = {**vault['sources']['s2'], 'provenance': {'kind': 'document_fetch', 'verified': True}}
        report = record(forged, partition=partition, kind='agent_synthesis', recorded_at='2026-10-25T10:00:00Z')
        self.assertEqual(report['provenance']['kind'], 'agent_synthesis')
        self.assertFalse(report['provenance']['independently_verified'])

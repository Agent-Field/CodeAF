"""Working-agent sidecars are staged evidence, not accepted memory."""
import unittest
from proposals import collect


class ProposalTests(unittest.TestCase):
    def test_malformed_optional_evidence_cannot_break_the_completed_response(self):
        response = {'id': 'a1', 'role': 'agent', 'text': 'Done.'}
        result = collect({'knowledge_delta': [{'op': 'learn', 'text': 'X', 'subject': 'X',
                          'evidence': [{'source': ['not', 'an', 'id'], 'quote': 'X'}]}]}, response, {},
                         {'owner': 'owner', 'project': 'project'})
        self.assertEqual(result['proposals'], [])
        self.assertEqual(result['receipt']['rejected'], 1)
        self.assertEqual(response['text'], 'Done.')

    def test_optional_sidecar_binds_self_and_uses_only_host_partition(self):
        response = {'id': 'a1', 'session': 'chat', 'role': 'agent', 'ts': '2026-10-25',
                    'text': 'The proposed Cedar design retries twice.'}
        delta = {'response': response['text'], 'knowledge_delta': [
            {'op': 'learn', 'text': response['text'], 'subject': 'Cedar', 'owner': 'invented-owner',
             'evidence': [{'source': 'self', 'quote': 'Cedar design retries twice.'}]},
            {'op': 'learn', 'text': 'It is globally deployed.', 'subject': 'Cedar',
             'evidence': [{'source': 'self', 'quote': 'globally deployed'}]}]}
        partition = {'owner': 'actual-owner', 'project': 'actual-project'}
        result = collect(delta, response, {}, partition)
        self.assertEqual(len(result['proposals']), 1)
        proposal = result['proposals'][0]
        self.assertEqual(proposal['partition'], partition)
        self.assertEqual(proposal['evidence'][0]['source'], 'a1')
        self.assertEqual(proposal['state'], 'pending_semantic_review')
        self.assertEqual(result['receipt']['rejected'], 1)
        replay = collect(delta, response, {}, partition, previous=result)
        self.assertEqual(len(replay['proposals']), 1)
        self.assertEqual(replay['receipt']['new'], 0)
        self.assertEqual(replay['receipt']['provider_calls'], 0)

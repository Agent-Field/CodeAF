"""Recovered billing must identify the same completed generation and model."""
import importlib.util
from pathlib import Path
import unittest
spec = importlib.util.spec_from_file_location('reconcile', Path(__file__).parents[1]/'lib/reconcile.py')
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)
class ReconcileTests(unittest.TestCase):
    def test_canonical_slug_requires_exact_catalog_evidence(self):
        row = dict(phase='settled', request_id='r', generation_id='g',
                   model='deepseek/deepseek-v4-flash-0731', cost_usd=None)
        data = dict(id='g', model='deepseek/deepseek-v4-flash-20260731',
                    total_cost=0.00000076, cancelled=True)
        self.assertFalse(r.valid_receipt(data, row))
        identities = r.catalog_identities([dict(id=row['model'], canonical_slug=data['model'])])
        evidence = []
        got = r.reconcile([row], lambda _: data, evidence, identities)
        self.assertEqual(got[0]['cost_usd'], data['total_cost'])
        self.assertEqual(evidence[0]['model_identity']['canonical_slug'], data['model'])
        self.assertFalse(r.valid_receipt(dict(data, model='deepseek/something-else'), row, identities))
        self.assertFalse(r.valid_receipt(data, dict(row, model='deepseek/deepseek-v4-flash'), identities))

    def test_ambiguous_or_missing_catalog_identity_is_not_authority(self):
        self.assertEqual(r.catalog_identities([{'id':'m','canonical_slug':'a'},
                                               {'id':'m','canonical_slug':'b'}]), {})
        self.assertEqual(r.catalog_identities([None, {'id':'m'}, {'id':None,'canonical_slug':'x'}]), {})
        self.assertFalse(r.valid_receipt({'id':'g','total_cost':0,'cancelled':True},
                                         {'generation_id':'g'}))

    def test_matches_identity_and_completion(self):
        row = dict(phase='settled', request_id='r1', generation_id='g1', model='open-model', cost_usd=None)
        data = dict(id='g1', model='open-model', total_cost=0.2, finish_reason='stop')
        evidence = []
        result = r.reconcile([row], lambda _:data, evidence)
        self.assertEqual(result[0]['cost_usd'], 0.2)
        self.assertIsNone(row['cost_usd'])
        self.assertEqual(len(evidence),1)
        for override in [dict(id='other'),dict(model='other'),dict(total_cost=-1),dict(total_cost=float('nan')),dict(finish_reason=None),dict(total_cost=True)]:
            bad = dict(data, **override)
            self.assertFalse(r.valid_receipt(bad,row))
    def test_unavailable_stays_unknown(self):
        row = dict(phase='settled',generation_id='g',cost_usd=None)
        self.assertIsNone(r.reconcile([row], lambda _: None, [])[0]['cost_usd'])
    def test_no_inference_or_fetch_for_priced_calls(self):
        def unexpected(_): self.fail('priced calls require no recovery')
        self.assertEqual(r.reconcile([dict(phase='settled',cost_usd=0.1)], unexpected, [])[0]['cost_usd'],0.1)
    def test_killed_guard_identity_can_recover_without_replaying(self):
        rows = [dict(phase='admitted',request_id='r',model='m'),
                dict(phase='generation',request_id='r',model='m',generation_id='g')]
        data = dict(id='g',model='m',total_cost=0.01,cancelled=True)
        got = r.reconcile(rows, lambda _:data, [])
        self.assertEqual(got[-1]['cost_usd'],0.01)
        self.assertEqual(len(rows),2)
        duplicate = r.reconcile(rows+[rows[0]], lambda _:data, [])
        self.assertFalse(any(x['phase']=='settled' for x in duplicate))
if __name__=='__main__':unittest.main()

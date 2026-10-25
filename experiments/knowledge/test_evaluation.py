"""A sampled capture audit stages exact-sourced omissions without publishing facts."""
import unittest
from test_maintenance import fixture


class EvaluationTests(unittest.TestCase):
    def test_omission_audits_sample_deterministically_and_do_not_promote_findings(self):
        from evaluation import audit_capture
        vault,_ = fixture()
        def model(role,payload):
            source = next(s for s in payload['sources'] if s['id']=='s2')
            return {'omissions':[{'source':'s2','text':'Cedar retry recovery is missing.',
                    'quote':source['text'],'reason':'Needed for operation.'}]}
        result = audit_capture(vault,model,sample_size=2,seed='fixed')
        self.assertEqual(result['candidates'][0]['state'],'pending_review')
        self.assertEqual(result['sampled_sources'],audit_capture(vault,model,sample_size=2,seed='fixed')['sampled_sources'])
        self.assertEqual(len(vault['claims']),1)

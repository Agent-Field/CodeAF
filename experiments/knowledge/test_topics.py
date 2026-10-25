"""Topic changes are observed through sourced subject pages and navigation."""
import copy
import unittest
from test_maintenance import fixture


class TopicTests(unittest.TestCase):
    def test_new_evidence_memberships_survive_a_merge_and_its_later_undo(self):
        from topics import reconcile,publish
        vault,learning=fixture()
        second=copy.deepcopy(learning['subjects'][0]);second.update(id='second',title='Operations')
        learning['subjects'].append(second)
        initial=reconcile(learning)
        merged=reconcile(learning,initial,decisions=[{'id':'m','op':'merge','from':['second'],'into':'cedar','reviewed_by':'host'}])
        detail=copy.deepcopy(vault['notes'][0]);detail.update(id='new-detail',title='Additional evidence')
        vault['notes'].append(detail);learning['subjects'][1]['notes'].append('new-detail')
        updated=reconcile(learning,merged)
        topic=next(n for n in publish(vault,learning,updated)['notes']if n['id']=='cedar')
        self.assertIn('new-detail',{e['id']for e in topic['links']})
        undone=reconcile(learning,updated,decisions=[{'id':'u','op':'undo','target':'m','reviewed_by':'host'}])
        topic=next(n for n in publish(vault,learning,undone)['notes']if n['id']=='second')
        self.assertIn('new-detail',{e['id']for e in topic['links']})
        replay=reconcile(learning,undone,decisions=[{'id':'m','op':'merge','from':['second'],'into':'cedar','reviewed_by':'host'}])
        self.assertEqual(len([n for n in publish(vault,learning,replay)['notes']if n.get('kind')=='subject']),2)

    def test_a_reviewed_split_preserves_identity_history_without_claim_leakage(self):
        from topics import reconcile, publish
        vault, learning = fixture()
        first = vault['notes'][0]
        second = copy.deepcopy(first)
        second.update(id='n2',title='Recovery',claims=['c2'])
        vault['notes'].append(second)
        vault['claims'].append({**vault['claims'][0],'id':'c2','text':'Cedar retries work.'})
        subject = learning['subjects'][0]
        subject['notes'].append('n2')
        subject['claims'].append('c2')
        registry = reconcile(learning)
        split = reconcile(learning,registry,decisions=[{'id':'s1','op':'split','target':'cedar',
            'reviewed_by':'host','children':[{'title':'Routing','notes':[first['id']]},{'title':'Recovery','notes':['n2']}]}])
        projected = publish(vault,learning,split)
        children = [n for n in projected['notes'] if n.get('kind')=='subject']
        self.assertEqual(len(children),2)
        self.assertTrue(all(n['learning_state']=='needs_evidence' for n in children))
        self.assertEqual(len(split['redirects']['cedar']),2)
        self.assertFalse(set(children[0]['claims']) & set(children[1]['claims']))
        self.assertEqual(len(projected['claims']),len(vault['claims']))
        with self.assertRaises(ValueError):
            reconcile(learning,registry,decisions=[{'id':'bad','op':'merge','from':['cedar'],'into':'cedar'}])

    def test_reviewed_merge_and_undo_keep_sources_and_create_navigational_maps(self):
        from topics import reconcile, publish
        vault, learning = fixture()
        original = copy.deepcopy(vault)
        other = copy.deepcopy(learning['subjects'][0])
        other.update(id='second', title='Cedar operations')
        learning['subjects'].append(other)
        registry = reconcile(learning)
        decision = {'id': 'merge-1', 'op': 'merge', 'from': ['second'], 'into': 'cedar', 'reviewed_by': 'host'}
        merged = reconcile(learning, registry, decisions=[decision])
        result = publish(vault, learning, merged)
        self.assertEqual(len([n for n in result['notes'] if n.get('kind') == 'subject']), 1)
        self.assertEqual(merged['redirects']['second'], ['cedar'])
        undone = reconcile(learning, merged, decisions=[{'id': 'undo-1', 'op': 'undo', 'target': 'merge-1', 'reviewed_by': 'host'}])
        result = publish(vault, learning, undone)
        maps = [n for n in result['notes'] if n.get('kind') == 'map']
        self.assertEqual(len(maps), 1)
        self.assertEqual({e['kind'] for e in maps[0]['links']}, {'contains'})
        self.assertEqual(result['sources'], original['sources'])
        self.assertEqual(result['claims'], original['claims'])
        renamed = reconcile(learning, undone, decisions=[{'id': 'rename-1', 'op': 'rename', 'target': 'cedar', 'title': 'Cedar concepts', 'reviewed_by': 'host'}])
        self.assertEqual(next(n for n in publish(vault, learning, renamed)['notes'] if n['id'] == 'cedar')['title'], 'Cedar concepts')

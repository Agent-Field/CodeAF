"""Run the experimental local knowledge workflow; no installed binary hooks."""
import copy
import fcntl
import json
from pathlib import Path
from curiosity import curate, wake
from knowledge import identity
from learning import learn
from maintenance import maintain
from topics import reconcile, publish
from workspace import Workspace
from storage import save, viewer, write_json

VERSION = 'workflow-3'  # Bump when semantic projection/cache behavior changes.


def extend(vault, learning, model):
    """Explain newly uncovered notes; mark changed known subjects for later editing."""
    result = copy.deepcopy(learning)
    active = {c['id']for c in vault['claims']if c['status']=='active'}
    assigned = {n for s in result['subjects']for n in s['notes']}
    notes = [n for n in vault['notes']if n['id'] not in assigned and set(n['claims']) & active]
    dirty = []
    by_id = {n['id']:n for n in vault['notes']}
    for subject in result['subjects']:
        members = {c for n in subject['notes'] if n in by_id for c in by_id[n]['claims']if c in active}
        added = members-set(subject['claims'])
        if added:
            subject['claims'] = sorted(set(subject['claims'])|added)
            subject['needs_editorial_update'] = True
            dirty.append(subject['id'])
    if notes:
        ids = {c for n in notes for c in n['claims']if c in active}
        subset = {**vault,'notes':notes,'claims':[c for c in vault['claims']if c['id']in ids]}
        discovered = learn(subset,model)
        by_subject = {s['id']:s for s in result['subjects']}
        for subject in discovered['subjects']:
            if subject['id'] in by_subject:
                old = by_subject[subject['id']]
                old['notes'] = sorted(set(old['notes']+subject['notes']))
                old['claims'] = sorted(set(old['claims']+subject['claims']))
                old['sections'] += subject['sections']
                old['needs_editorial_update'] = True
            else:
                result['subjects'].append(subject)
    result['incremental_discovery'] = {'new_notes':len(notes),'dirty_existing_subjects':dirty,
                                     'limitation':'Changed known subjects keep sourced prose and are marked for bounded later editing; not rewritten automatically.'}
    return result


def _run(directory, partition, learning, out, model, *, context='', questions=4, advance=False, as_of=None, stale_after_seconds=None):
    """One owner-scoped compile/refine/export pass, with reusable local receipts."""
    out = Path(out)
    out.mkdir(parents=True,exist_ok=True,mode=0o700)
    work = Workspace(directory,partition)
    capture = work.flush(model)
    vault = capture['vault']
    if not vault['sources']:
        raise ValueError('Seed or ingest a permitted source collection before refining')
    binding = out/'.partition.json'
    if binding.exists() and json.loads(binding.read_text()) != partition:
        raise ValueError('Output directory belongs to another host partition')
    write_json(binding,partition)
    registry_file = out/'topics.json'
    previous_registry = json.loads(registry_file.read_text())if registry_file.exists()else None
    def fingerprint(registry):
        return identity([VERSION,vault,learning,context,questions,as_of,stale_after_seconds,registry])
    prior_report = out/'report.json'
    if prior_report.exists() and not advance:
        prior = json.loads(prior_report.read_text())
        if prior.get('input_hash') == fingerprint(previous_registry):
            replay = {**prior,'reused':True,'capture':{k:v for k,v in capture.items()if k!='vault'},
                    'curation':{**prior['curation'],'model_calls':0},
                    'maintenance':{**prior['maintenance'],'provider_calls':0}}
            cached_vault = out/'knowledge/vault.json'
            if cached_vault.exists():
                viewer({'direct':vault,'reconciled':json.loads(cached_vault.read_text())},replay,{'architecture':'Cached knowledge with current inspector assets; no model work.'},out/'viewer')
            return replay
    state = out/'learning.json'
    current = json.loads(state.read_text())if state.exists()else learning
    current = extend(vault,current,model)
    old_curated = out/'curated.json'
    curated = curate(vault,current,model,json.loads(old_curated.read_text())if old_curated.exists()else None,
                     context=context,limit_per_subject=2,budget=48)
    write_json(old_curated,curated)
    work.watch(curated)
    woken = wake(curated,work.wakeups(),vault)
    old_maintained = out/'maintained.json'
    maintained = maintain(vault,woken,model,json.loads(old_maintained.read_text())if old_maintained.exists()else None,
                          budget=questions,context=context,advance=advance)
    selected = {item['id'] for item in maintained['maintenance']['selection']}
    pending=[]
    for subject in maintained['subjects']:
        for question in subject.get('questions',[]):
            if question.get('state')=='answered':
                question['curiosity']={'state':'closed','reason':'Reviewed complete answer exists.'}
                continue
            info=question.get('curiosity',{})
            eligible=info.get('state')=='active' or info.get('was_active') or question.get('id')in selected
            if eligible and question.get('state')!='retired_evidence':
                pending.append((question.get('id')not in selected,subject['id'],question['id'],question))
            question['curiosity']={**info,'state':'parked'}
    counts,total={},0
    for _,sid,qid,question in sorted(pending,key=lambda item:item[:3]):
        if total<48 and counts.get(sid,0)<2:
            question['curiosity']['state']='active'
            counts[sid]=counts.get(sid,0)+1
            total+=1
    maintained['curation']['active']=total
    maintained['curation']['parked']=sum(q.get('curiosity',{}).get('state')=='parked' for s in maintained['subjects']for q in s.get('questions',[]))
    maintained['curation']['woken_waiting']=sum(bool(q.get('wake_sources')) for s in maintained['subjects']for q in s.get('questions',[]))
    write_json(old_maintained,maintained)
    registry = reconcile(maintained,previous_registry)
    projected = publish(vault,maintained,registry,as_of=as_of,stale_after_seconds=stale_after_seconds)
    save(projected,out/'knowledge')
    write_json(state,maintained)
    write_json(registry_file,registry)
    report = {'input_hash':fingerprint(registry),'reused':False,'capture':{k:v for k,v in capture.items()if k!='vault'},'subjects':len(maintained['subjects']),'topic_maps':sum(n.get('kind')=='map'for n in projected['notes']),
              'curation':maintained['curation'],'maintenance':maintained['maintenance'],
              'incremental_discovery':maintained['incremental_discovery'],'as_of':as_of,
              'stale_after_seconds':stale_after_seconds,'viewer':str(out/'viewer/standalone.html'),
              'limitation':'Local experimental runner; host authentication, binary response hooks and production scheduling are not installed.'}
    viewer({'direct':vault,'reconciled':projected},report,{'architecture':'Persistent capture, host provenance, stable concepts, reading maps, selective curiosity and explicit age policy.'},out/'viewer')
    write_json(out/'report.json',report)
    return report


def run(directory, partition, learning, out, model, **options):
    """Serialize refinement/export in one partition-bound output directory."""
    out = Path(out)
    out.mkdir(parents=True,exist_ok=True,mode=0o700)
    with (out/'.workflow.lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        try:
            return _run(directory,partition,learning,out,model,**options)
        finally:
            fcntl.flock(lock,fcntl.LOCK_UN)


def main():
    import argparse
    from provider import OpenRouter
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--workspace',type=Path,required=True)
    parser.add_argument('--owner',required=True)
    parser.add_argument('--project',required=True)
    parser.add_argument('--learning',type=Path,required=True)
    parser.add_argument('--out',type=Path,required=True)
    parser.add_argument('--context',default='')
    parser.add_argument('--questions',type=int,default=4)
    parser.add_argument('--advance',action='store_true')
    parser.add_argument('--as-of')
    parser.add_argument('--stale-after-seconds',type=int)
    args = parser.parse_args()
    report = run(args.workspace,{'owner':args.owner,'project':args.project},json.loads(args.learning.read_text()),args.out,
                 OpenRouter(args.out/'calls',reasoning_effort='none',max_tokens=8000),context=args.context,questions=args.questions,advance=args.advance,
                 as_of=args.as_of,stale_after_seconds=args.stale_after_seconds)
    print(json.dumps(report,indent=2))


if __name__ == '__main__':
    main()

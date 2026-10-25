"""Frozen support contracts and reproducible omission sampling, not model self-grades."""
import copy
import json
from pathlib import Path
from knowledge import identity, recall
from workspace import Workspace


def audit_capture(vault, model, *, sample_size=8, seed='capture-audit-1', previous=None):
    if not 1 <= sample_size <= 16:
        raise ValueError('Audit samples one to sixteen sources')
    ids = sorted(vault['sources'],key=lambda sid:identity([seed,sid]))[:sample_size]
    sources = [{**{k:vault['sources'][sid][k] for k in ('id','role','ts')},
                'text':vault['sources'][sid]['text'][:1500]} for sid in ids]
    eligible = [c for c in vault['claims'] if c['status']=='active' and any(e['source']in ids for e in c.get('evidence',[]))]
    claims = []
    for c in eligible:
        item = {**{k:c[k] for k in ('id','text','kind','scope')}, 'source_ids':sorted({e['source']for e in c['evidence']})}
        if len(claims)<48 and len(json.dumps({'sources':sources,'claims':claims+[item]}))<=28000:
            claims.append(item)
    digest = identity(['omission-audit-1',sources,claims])
    if previous and previous.get('input_hash') == digest:
        return {**copy.deepcopy(previous),'model_calls':0}
    response = model('audit_omissions',{'sources':sources,'captured_claims':claims,'max_candidates':8}) if sources else {}
    supplied = {s['id']:s['text']for s in sources}
    candidates, seen = [], set()
    for item in response.get('omissions',[])[:8]:
        sid,quote,text = item.get('source'),item.get('quote'),item.get('text')
        if not isinstance(sid,str) or sid not in supplied or not isinstance(quote,str) or not quote or quote not in supplied[sid] or not isinstance(text,str) or not text.strip():
            continue
        key = identity([sid,quote,text])
        if key in seen:
            continue
        seen.add(key)
        candidates.append({'id':key,'source':sid,'text':text[:1000],'quote':quote,
                           'reason':str(item.get('reason',''))[:400],'state':'pending_review'})
    return {'input_hash':digest,'sampled_sources':ids,'candidates':candidates,'model_calls':int(bool(sources)),
            'captured_claims_omitted':len(eligible)-len(claims),
            'truncated_sources':[sid for sid in ids if len(vault['sources'][sid]['text'])>1500],
            'limitation':'Sampled same-model omission candidates, not measured capture recall or accepted facts. Only initial source spans and at most 48 claims were inspected.'}


def run_suite(directory, model, *, cases_path=None):
    """Replay frozen synthetic cases; score explicit support contracts, not eloquence."""
    cases = json.loads(Path(cases_path or Path(__file__).with_name('eval_cases.json')).read_text())
    results = []
    for case in cases:
        work = Workspace(Path(directory)/case['id'],{'owner':'benchmark','project':case['id']})
        for batch in case['batches']:
            for source in batch:
                work.push(source,kind='user_statement' if source['role']=='user' else 'agent_synthesis',recorded_at=source['ts'])
            work.flush(model,force=True)
        vault = work.read()
        answer = recall(vault,case['question'],model,scope=case.get('scope'))
        cited = {e['source']for e in answer['citations']}
        active = [c for c in vault['claims']if c['status']=='active']
        checks = {'required_sources':set(case.get('required_sources',[])) <= cited,
                  'any_required_source':not case.get('any_required_sources') or bool(set(case['any_required_sources']) & cited),
                  'answer_signal':not case.get('answer_must_include_any') or any(t.casefold() in answer['answer'].casefold() for t in case['answer_must_include_any']),
                  'forbidden_sources':not (set(case.get('forbidden_sources',[])) & cited),
                  'required_kind':not case.get('required_kind') or any(c['kind']==case['required_kind']for c in active),
                  'reported_only':not case.get('reported_only') or all(c['kind']=='reported'for c in active),
                  'abstention':not case.get('abstain') or not cited,
                  'retired_source':not case.get('retire_source') or all(c['status']!='active'for c in vault['claims']if any(e['source']==case['retire_source']for e in c['evidence']))}
        results.append({'id':case['id'],'passed':all(checks.values()),'checks':checks,'answer':answer})
    return {'manifest_hash':identity(cases),'suite':'frozen-support-contracts-1','cases':results,'passed':sum(r['passed']for r in results),
            'total':len(results),'limitation':'Deterministic checks of source support, modality, retirement and scoping. This does not establish general semantic accuracy or production recall.'}


def main():
    import argparse
    from provider import OpenRouter
    from storage import write_json
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=['suite','audit'])
    parser.add_argument('--out',type=Path,required=True)
    parser.add_argument('--vault',type=Path)
    parser.add_argument('--sample',type=int,default=8)
    args = parser.parse_args()
    args.out.mkdir(parents=True,exist_ok=True,mode=0o700)
    model = OpenRouter(args.out/'calls',reasoning_effort='none',max_tokens=8000)
    if args.command=='suite':
        result = run_suite(args.out/'cases',model)
    else:
        if not args.vault:
            parser.error('audit requires --vault')
        previous = args.out/'audit.json'
        result = audit_capture(json.loads(args.vault.read_text()),model,sample_size=args.sample,
                               previous=json.loads(previous.read_text())if previous.exists()else None)
    write_json(args.out/('suite.json'if args.command=='suite'else'audit.json'),result)
    print(json.dumps(result,indent=2))
    if args.command=='suite' and result['passed']!=result['total']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()

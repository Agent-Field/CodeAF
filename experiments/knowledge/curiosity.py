"""Keep a small consequential question set; park history and wake on new evidence."""
import copy
from knowledge import identity


def curate(vault, learning, model, previous=None, *, context='', limit_per_subject=2, budget=64):
    if not 0 <= limit_per_subject <= 5 or not 0 <= budget <= 128:
        raise ValueError('Invalid curation bounds')
    result = copy.deepcopy(learning)
    active = {c['id'] for c in vault['claims'] if c['status'] == 'active'}
    catalog, questions, eligible = [], {}, set()
    for subject in result['subjects']:
        for q in subject.get('questions', []):
            qid = identity(['question',subject['id'],q['question'].strip().casefold()])
            q['id'] = qid
            questions[qid] = (subject['id'],q)
            catalog.append({'id':qid,'subject_id':subject['id'],'subject':subject['title'],
                            'question':q['question'],'why':q.get('why',''),'claims':q['claims']})
            if q.get('state')!='answered' and q['claims'] and set(q['claims']) <= active:
                eligible.add(qid)
    by_id={q['id']:q for q in catalog}
    hashes={q['id']:identity(q) for q in catalog}
    settings=identity(['curiosity-2',context,limit_per_subject,budget])
    digest=identity([settings,hashes])
    old=(previous or {}).get('curation',{})
    same_context=old.get('context_hash')==settings
    retained=[q for q in old.get('selection',[])if q['id']in eligible and old.get('question_hashes',{}).get(q['id'])==hashes[q['id']]] if same_context else []
    changed=[q for q in catalog if q['id']in eligible and old.get('question_hashes',{}).get(q['id'])!=hashes[q['id']]]
    pool=([by_id[q['id']]for q in retained]+changed)if same_context else [q for q in catalog if q['id']in eligible]
    pool=list({q['id']:q for q in pool}.values())
    inspected=set(old.get('inspected_question_ids',[])) & eligible if same_context else set()
    calls=0
    if not budget or not limit_per_subject:
        chosen=[]
    elif same_context and not changed:
        chosen=retained
    elif pool:
        # Diverse bounded reading, without blocking larger collections or pretending
        # the uninspected tail was semantically judged. Stable IDs provide hysteresis.
        import json
        groups={}
        for item in pool:groups.setdefault(item['subject_id'],[]).append(item)
        shortlist=[];size=2
        ordered=sorted(groups,key=lambda sid:identity([context,sid]))
        for level in range(max(map(len,groups.values()),default=0)):
            for sid in ordered:
                if level>=len(groups[sid]) or len(shortlist)>=256:continue
                item=groups[sid][level];length=len(json.dumps(item))+2
                if size+length<=80000:shortlist.append(item);size+=length
        inspected.update(q['id']for q in shortlist)
        response=model('curate_questions',{'context':context,'per_subject':limit_per_subject,'budget':budget,'questions':shortlist})
        calls=1
        allowed={q['id']for q in shortlist}
        chosen=[];counts={};seen=set()
        for item in response.get('keep',[]):
            qid=item.get('id')
            if not isinstance(qid,str)or qid not in allowed or qid in seen:continue
            sid,_=questions[qid]
            if len(chosen)>=budget or counts.get(sid,0)>=limit_per_subject or not isinstance(item.get('reason'),str)or not item['reason'].strip():continue
            chosen.append({'id':qid,'reason':item['reason'][:400]});seen.add(qid);counts[sid]=counts.get(sid,0)+1
    else:
        chosen=retained
    selected={q['id']:q['reason']for q in chosen}
    for qid,(_,q)in questions.items():
        if q.get('state')=='answered':
            q['curiosity']={'state':'closed','reason':'Reviewed complete answer exists.'}
        else:
            q['curiosity']={'state':'active'if qid in selected else 'parked',
                            'reason':selected.get(qid,'Parked in bounded curation; no claim of unimportance.'),
                            'inspected':qid in inspected,
                            'wake_condition':'New matching evidence, relevant task context, or explicit request'}
    result['curation']={'input_hash':digest,'context_hash':settings,'question_hashes':hashes,
                        'inspected_question_ids':sorted(inspected),'selection':chosen,'model_calls':calls,
                        'active':len(chosen),'parked':sum(q.get('curiosity',{}).get('state')=='parked'for _,q in questions.values()),
                        'not_inspected':len(eligible-inspected),
                        'limitation':'Incremental model usefulness selection; parked or uninspected questions are not proven unimportant.'}
    return result


def wake(learning, wakeups, vault):
    result = copy.deepcopy(learning)
    for subject in result['subjects']:
        for q in subject.get('questions',[]):
            qid = identity(['question',subject['id'],q['question'].strip().casefold()])
            seen = set(q.get('processed_wakes',[]))
            new = [x['source'] for x in wakeups.get(qid,[]) if x['source'] in vault['sources'] and x['source'] not in seen]
            if new:
                q['wake_sources'] = sorted(set(q.get('wake_sources',[])+new))
                q['curiosity'] = {'state':'woken','was_active':q.get('curiosity',{}).get('state')=='active',
                                  'reason':'New source matches indexed question vocabulary',
                                  'wake_condition':'Review these sources once; time alone does not retry'}
    return result

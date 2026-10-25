"""Persistent partition-scoped push capture with bounded synchronous publication."""
import copy
import fcntl
import json
import sqlite3
from pathlib import Path
from datetime import datetime, timezone
from evidence import record
from knowledge import identity, instant, update_once
from proposals import collect


def empty():
    return {'schema': 1, 'strategy': 'reconciled', 'sources': {}, 'claims': [], 'notes': [],
            'relations': [], 'rejected': [], 'scope_aliases': []}


class Workspace:
    """The host authenticates partition and source kinds before calling this module.

    SQLite publication is atomic. A per-partition process lock serializes compilers
    without holding the SQLite write transaction during network calls. Pushes stay
    available. A crash may repeat a model request; provider idempotency/cache is
    separate from exactly-once local publication. No daemon or schedule is installed.
    """
    def __init__(self, directory, partition):
        if set(partition) != {'owner', 'project'} or not all(isinstance(v, str) and v for v in partition.values()):
            raise ValueError('Authenticated host partition required')
        self.partition = dict(partition)
        self.key = identity(partition)
        self.directory = Path(directory)
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.path = self.directory / 'workspace.sqlite'
        with self.connect() as db:
            db.executescript('''
                CREATE TABLE IF NOT EXISTS events(partition TEXT, id TEXT, payload TEXT, arrived REAL,
                    status TEXT DEFAULT 'pending', attempts INTEGER DEFAULT 0, PRIMARY KEY(partition,id));
                CREATE TABLE IF NOT EXISTS publications(partition TEXT PRIMARY KEY, payload TEXT, revision INTEGER);
                CREATE TABLE IF NOT EXISTS proposals(partition TEXT, id TEXT, payload TEXT, PRIMARY KEY(partition,id));
                CREATE TABLE IF NOT EXISTS receipts(partition TEXT, payload TEXT);
                CREATE TABLE IF NOT EXISTS source_windows(partition TEXT,id TEXT,start INTEGER,length INTEGER,PRIMARY KEY(partition,id));
                CREATE INDEX IF NOT EXISTS pending_lookup ON events(partition,status,arrived,id);
                CREATE TABLE IF NOT EXISTS claim_keys(partition TEXT,id TEXT,PRIMARY KEY(partition,id));
                CREATE TABLE IF NOT EXISTS proposal_sources(partition TEXT,id TEXT,source TEXT,PRIMARY KEY(partition,id,source));
                CREATE INDEX IF NOT EXISTS proposal_source_lookup ON proposal_sources(partition,source);
                CREATE TABLE IF NOT EXISTS watches(partition TEXT,id TEXT,terms TEXT,PRIMARY KEY(partition,id));
                CREATE TABLE IF NOT EXISTS wakes(partition TEXT,id TEXT,source TEXT,reason TEXT,
                    PRIMARY KEY(partition,id,source));
            ''')

    def connect(self):
        return sqlite3.connect(self.path, timeout=30)

    def read(self):
        with self.connect() as db:
            row = db.execute('SELECT payload FROM publications WHERE partition=?', (self.key,)).fetchone()
        return json.loads(row[0]) if row else empty()

    def seed(self, vault):
        """Host imports one already-permitted collection; never overwrite a publication."""
        for source in vault['sources'].values():
            if source.get('provenance', {}).get('partition', self.partition) != self.partition:
                raise ValueError('Seed contains another host partition')
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            old = db.execute('SELECT payload FROM publications WHERE partition=?', (self.key,)).fetchone()
            if old:
                if json.loads(old[0]) != vault:
                    raise ValueError('Seed cannot replace an existing publication')
                return
            db.execute('INSERT INTO publications VALUES(?,?,0)', (self.key,json.dumps(vault)))
            db.executemany('INSERT OR IGNORE INTO claim_keys VALUES(?,?)',[(self.key,c['id'])for c in vault['claims']])
            for sid, source in vault['sources'].items():
                db.execute('INSERT INTO events(partition,id,payload,arrived,status) VALUES(?,?,?,?,?)',
                           (self.key,sid,json.dumps(source),instant(source['ts']).timestamp(),'done'))

    def watch(self, learning):
        """Index question vocabulary for relevant new-source wakeups without a model."""
        from maintenance import words
        stop = {'the','and','what','how','does','when','which','with','from','that','this','are','for'}
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            db.execute('DELETE FROM watches WHERE partition=?', (self.key,))
            for subject in learning['subjects']:
                for question in subject.get('questions', []):
                    qid = identity(['question',subject['id'],question['question'].strip().casefold()])
                    terms = sorted(set(words(' '.join(question.get('search_terms',[]))+' '+question['question']+' '+subject['scope']))-stop)
                    db.execute('INSERT OR REPLACE INTO watches VALUES(?,?,?)',(self.key,qid,json.dumps(terms)))

    def push(self, message, *, kind, recorded_at, valid_at=None, locator=None, revision=None, envelope=None):
        source = record(message, partition=self.partition, kind=kind, recorded_at=recorded_at,
                        valid_at=valid_at, locator=locator, revision=revision)
        if len(source['text']) > 24000:
            raise ValueError('Source exceeds 24,000 characters; host must create explicitly identified spans')
        payload = json.dumps(source, sort_keys=True)
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            existing = db.execute('SELECT payload FROM events WHERE partition=? AND id=?',(self.key,source['id'])).fetchone()
            if existing:
                # Arrival time is part of immutable host provenance; replay exact records.
                if json.loads(existing[0]) != source:
                    raise ValueError('Source changed under an immutable ID')
            else:
                db.execute('INSERT INTO events(partition,id,payload,arrived) VALUES(?,?,?,?)',
                           (self.key,source['id'],payload,instant(recorded_at).timestamp()))
                from maintenance import words
                terms = set(words(source['text']))
                for qid, raw in db.execute('SELECT id,terms FROM watches WHERE partition=?',(self.key,)).fetchall():
                    matched = terms & set(json.loads(raw))
                    if len(matched) >= 2:
                        db.execute('INSERT OR IGNORE INTO wakes VALUES(?,?,?,?)',
                                   (self.key,qid,source['id'],json.dumps(sorted(matched))))
            if envelope is not None:
                # Read only cited sources and exact duplicate candidates, not the full archive.
                import re
                raw = envelope.get('knowledge_delta',[]) if isinstance(envelope,dict) else []
                raw = raw[:4] if isinstance(raw,list) else []
                referenced, proposal_ids, targets = set(), set(), set()
                for item in raw:
                    if not isinstance(item,dict) or not isinstance(item.get('text'),str):continue
                    target = item.get('target') if item.get('op')=='correct' else None
                    if target is not None and not isinstance(target,str):continue
                    if target:targets.add(target)
                    normalized = re.sub(r'\s+',' ',item['text'].strip()).casefold()
                    proposal_ids.add(identity([self.partition,item.get('op'),normalized,target]))
                    evidence = item.get('evidence',[])
                    if isinstance(evidence,list):
                        referenced.update(e['source']for e in evidence[:4]if isinstance(e,dict)and isinstance(e.get('source'),str)and e['source']!='self')
                def rows_for(table, values):
                    if not values:return []
                    column='id'
                    sql='SELECT id,payload FROM '+table+' WHERE partition=? AND '+column+' IN ('+','.join('?'for _ in values)+')'
                    return db.execute(sql,[self.key,*sorted(values)]).fetchall()
                sources = {sid:json.loads(payload)for sid,payload in rows_for('events',referenced)}
                prior = {'proposals':[json.loads(payload)for _,payload in rows_for('proposals',proposal_ids)]}
                known_targets = {r[0]for r in db.execute('SELECT id FROM claim_keys WHERE partition=? AND id IN ('+','.join('?'for _ in targets)+')',[self.key,*sorted(targets)])} if targets else set()
                staged = collect(envelope,source,sources,self.partition,prior,claim_ids=known_targets)
                for proposal in staged['proposals']:
                    # Keep bounded citations in staging; original source events remain archived.
                    proposal['evidence'] = proposal['evidence'][:8]
                    db.execute('INSERT OR REPLACE INTO proposals VALUES(?,?,?)',(self.key,proposal['id'],json.dumps(proposal)))
                    db.execute('DELETE FROM proposal_sources WHERE partition=? AND id=?',(self.key,proposal['id']))
                    db.executemany('INSERT OR IGNORE INTO proposal_sources VALUES(?,?,?)',[(self.key,proposal['id'],e['source'])for e in proposal['evidence']])
                if existing and staged['receipt']['new']:
                    db.execute('UPDATE events SET status=? WHERE partition=? AND id=? AND status=?',('revisit',self.key,source['id'],'done'))
        return {'source':source['id'],'new':not bool(existing),'model_calls':0}

    def revisit(self, source_ids, *, now=None, windows=None):
        """Explicitly recompile audited omissions from the same immutable sources."""
        at = instant(now).timestamp() if now else datetime.now(timezone.utc).timestamp()
        windows = windows or {}
        if not set(windows) <= set(source_ids):raise ValueError('Window source must be explicitly revisited')
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            for sid in source_ids:
                row = db.execute('SELECT status,payload FROM events WHERE partition=? AND id=?',(self.key,sid)).fetchone()
                if not row:
                    raise ValueError('Cannot revisit a source outside this host partition')
                db.execute('DELETE FROM source_windows WHERE partition=? AND id=?',(self.key,sid))
                if sid in windows:
                    window=windows[sid]
                    start,length=window['start'],window['length']
                    if not isinstance(start,int) or not isinstance(length,int) or start<0 or not 1<=length<=24000 or start+length>len(json.loads(row[1])['text']):
                        raise ValueError('Invalid exact source window')
                    db.execute('INSERT INTO source_windows VALUES(?,?,?,?)',(self.key,sid,start,length))
                if row[0] == 'done':
                    db.execute('UPDATE events SET status=?,arrived=? WHERE partition=? AND id=?',('revisit',at,self.key,sid))

    def wakeups(self):
        with self.connect() as db:
            rows = db.execute('SELECT id,source,reason FROM wakes WHERE partition=? ORDER BY id,source',(self.key,)).fetchall()
        result = {}
        for qid, sid, reason in rows:
            result.setdefault(qid,[]).append({'source':sid,'matched_terms':json.loads(reason)})
        return result

    def flush(self, model, *, now=None, force=False, quiet_seconds=30, max_wait_seconds=120, max_chars=24000):
        if not 1 <= max_chars <= 24000 or quiet_seconds < 0 or max_wait_seconds <= 0:
            raise ValueError('Invalid capture scheduling bounds')
        at = instant(now).timestamp() if now else datetime.now(timezone.utc).timestamp()
        with (self.directory/(self.key+'.lock')).open('a') as lock:
            try:
                fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
            except BlockingIOError:
                return {'published':False,'reason':'compiler_busy','vault':self.read()}
            try:
                return self._flush(model,at,force,quiet_seconds,max_wait_seconds,max_chars)
            finally:
                fcntl.flock(lock,fcntl.LOCK_UN)

    def _flush(self, model, at, force, quiet, max_wait, max_chars):
        with self.connect() as db:
            rows = db.execute('SELECT id,payload,arrived,status FROM events WHERE partition=? AND status IN (?,?) ORDER BY arrived,id LIMIT 64',
                              (self.key,'pending','revisit')).fetchall()
            pending_times = db.execute('SELECT min(arrived),max(arrived) FROM events WHERE partition=? AND status IN (?,?)',(self.key,'pending','revisit')).fetchone()
            windows = {r[0]:{'start':r[1],'length':r[2]} for r in db.execute('SELECT id,start,length FROM source_windows WHERE partition=?',(self.key,))}
            old = db.execute('SELECT payload,revision FROM publications WHERE partition=?',(self.key,)).fetchone()
        previous = json.loads(old[0]) if old else None
        if not rows:
            return {'published':False,'reason':'unchanged','vault':previous or empty()}
        due = min(pending_times[1]+quiet,pending_times[0]+max_wait)
        if not force and at < due and sum(windows[r[0]]['length'] if r[0] in windows else len(json.loads(r[1])['text'])for r in rows) < max_chars:
            return {'published':False,'reason':'debouncing','due':due,'vault':previous or empty()}
        selected, size = [], 0
        for sid,payload,arrived,status in rows[:64]:
            source = json.loads(payload)
            characters=windows[sid]['length']if sid in windows else len(source['text'])
            if size+characters > max_chars:
                break
            selected.append(source)
            size += characters
        if not selected:
            raise ValueError('Oldest pending source exceeds requested context cap')
        with self.connect() as db:
            ids = [s['id']for s in selected]
            proposal_rows = db.execute('SELECT DISTINCT p.payload FROM proposals p JOIN proposal_sources s ON p.partition=s.partition AND p.id=s.id WHERE p.partition=? AND s.source IN ('+','.join('?'for _ in ids)+') ORDER BY p.id LIMIT 128',[self.key,*ids]).fetchall()
            proposals = [json.loads(r[0])for r in proposal_rows]
        with self.connect() as db:
            db.executemany('UPDATE events SET attempts=attempts+1 WHERE partition=? AND id=?',[(self.key,s['id'])for s in selected])
        calls = []
        hint_receipt = {'supplied':0,'omitted':0,'lookup_capped':len(proposals)==128}
        def tracked(role,payload):
            calls.append(role)
            if role == 'update':
                ids = {s['id']for s in selected}
                hints=[]
                for p in proposals:
                    evidence=[{k:e[k]for k in ('source','span_start','span_end')}for e in p['evidence']if e['source']in ids]
                    if not evidence:continue
                    hint={k:p[k]for k in ('op','text','subject_hint','target')}
                    hint['evidence']=evidence
                    if len(hints)<32 and len(json.dumps(hints+[hint]))<=12000:hints.append(hint)
                hint_receipt.update(supplied=len(hints),omitted=len(proposals)-len(hints))
                payload = {**payload,'proposals':hints}

            return model(role,payload)
        vault = update_once(selected,previous,tracked,revisit=[r[0] for r in rows if r[3]=='revisit' and r[0] in {s['id'] for s in selected}],windows={s['id']:windows[s['id']]for s in selected if s['id']in windows})
        receipt = {'events':len(selected),'chats':len({s['session']for s in selected}),
                   'source_characters':size,'revisited_events':sum(r[3]=='revisit'for r in rows if r[0]in {s['id']for s in selected}),'proposal_hints':hint_receipt,'model_calls':len(calls),'roles':calls,
                   'oldest_wait_seconds':max(0,at-rows[0][2]),'publication_revision':(old[1]+1)if old else 1,'publication_bytes':len(json.dumps(vault).encode())}
        with self.connect() as db:
            db.execute('BEGIN IMMEDIATE')
            db.execute('INSERT OR REPLACE INTO publications VALUES(?,?,?)',(self.key,json.dumps(vault),receipt['publication_revision']))
            db.executemany('UPDATE events SET status=? WHERE partition=? AND id=?',[('done',self.key,s['id'])for s in selected])
            db.executemany('INSERT OR IGNORE INTO claim_keys VALUES(?,?)',[(self.key,c['id'])for c in vault['claims']])
            db.executemany('DELETE FROM source_windows WHERE partition=? AND id=?',[(self.key,s['id'])for s in selected])
            db.execute('INSERT INTO receipts VALUES(?,?)',(self.key,json.dumps(receipt)))
        return {'published':True,'vault':vault,'receipt':receipt}


def main():
    import argparse
    from provider import OpenRouter
    from storage import save
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=['seed','push','flush','revisit'])
    parser.add_argument('--directory',type=Path,required=True)
    parser.add_argument('--owner',required=True)
    parser.add_argument('--project',required=True)
    parser.add_argument('--vault',type=Path)
    parser.add_argument('--source',type=Path)
    parser.add_argument('--kind',choices=sorted(__import__('evidence').KINDS))
    parser.add_argument('--recorded-at')
    parser.add_argument('--valid-at')
    parser.add_argument('--locator')
    parser.add_argument('--revision')
    parser.add_argument('--source-id',action='append',default=[])
    parser.add_argument('--window-start',type=int)
    parser.add_argument('--window-length',type=int)
    parser.add_argument('--force',action='store_true')
    args = parser.parse_args()
    work = Workspace(args.directory,{'owner':args.owner,'project':args.project})
    if args.command=='seed':
        if not args.vault:parser.error('seed requires --vault')
        work.seed(json.loads(args.vault.read_text()))
        result={'seeded':True}
    elif args.command=='push':
        if not args.source or not args.kind or not args.recorded_at:parser.error('push requires --source, --kind and --recorded-at')
        result=work.push(json.loads(args.source.read_text()),kind=args.kind,recorded_at=args.recorded_at,
                         valid_at=args.valid_at,locator=args.locator,revision=args.revision)
    elif args.command=='revisit':
        if not args.source_id:parser.error('revisit requires --source-id')
        windows=None
        if args.window_start is not None or args.window_length is not None:
            if len(args.source_id)!=1 or args.window_start is None or args.window_length is None:
                parser.error('a window requires one source ID, --window-start and --window-length')
            windows={args.source_id[0]:{'start':args.window_start,'length':args.window_length}}
        work.revisit(args.source_id,windows=windows)
        result={'revisit_requested':args.source_id}
    else:
        flushed=work.flush(OpenRouter(args.directory/'calls',reasoning_effort='none',max_tokens=8000),force=args.force)
        save(flushed['vault'],args.directory/'knowledge')
        result={k:v for k,v in flushed.items()if k!='vault'}
    print(json.dumps(result,indent=2))


if __name__=='__main__':
    main()

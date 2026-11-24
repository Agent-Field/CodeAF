#!/usr/bin/env python3
"""The keyless smoke test of a codeaf organization deployment. Run it INSIDE the runner container
(it reaches every service by its network name and drives the runner's engine over its socket):

    docker compose exec -T runner python3 /opt/org/smoke.py all --operator-link '<link>'

Sections (run one at a time; `all` runs control and job):
  control    AgentField answers, SWE-AF, PR-AF and the factory are registered, PR-AF answers a call
  job        a factory job reaches SWE-AF through AgentField; its plan question reaches the owner
             and the answer resumes (then ends) the run; AgentField's strict-proof counters agree
  org        (run it in the codeaf container, or over https) the operator claims the organization with
             its link, invites a second person, who joins; roles are enforced
  after      (after `docker compose restart`) every node is registered again, AgentField still holds the runs
  after-org  (in the codeaf container) the organization still knows the operator

An organization server takes a password only over https or from its own machine, so `org` and
`after-org` run inside the codeaf container with ORG_URL=http://127.0.0.1:8787 (smoke.sh does).

It prints PASS or FAIL per check and exits non-zero if any failed. Secrets are never printed.
Everything the model is asked goes to the scripted stub, so it spends nothing; the stub's answers
are minimal and the "work" is not real.
"""
import argparse
import json
import os
import re
import secrets
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

AGENTFIELD = os.environ.get('AGENTFIELD_URL', 'http://agentfield:8080').rstrip('/')
KEY = os.environ.get('AGENTFIELD_API_KEY', '')
ORG = os.environ.get('ORG_URL', 'http://codeaf:8787').rstrip('/')
SOCKET = os.environ.get('RUNNER_SOCKET', '/data/engine.sock')
FIXTURE_PATH = os.environ.get('SMOKE_FIXTURE_PATH', '/workspaces/smoke-fixture')  # on the swe-af container (compose.yaml, .env.stub)
STATE = Path(os.environ.get('SMOKE_STATE', '/data/smoke-state.json'))


def password():
    """A fresh password for the people the smoke test makes, kept (mode 600) beside its state so the
    check after a restart can sign in. Never a fixed string: the organization stays reachable afterwards."""
    try:
        saved = json.loads(STATE.read_text()).get('password')
        if saved:
            return saved
    except (OSError, ValueError):
        pass
    return '-'.join(secrets.token_hex(6) for _ in range(4))

failures = 0


def check(name, ok, evidence=''):
    global failures
    if not ok:
        failures += 1
    print(('PASS' if ok else 'FAIL') + '  ' + name + (('   ' + str(evidence)) if evidence != '' else ''), flush=True)
    return ok


def http(method, url, token=None, body=None, headers=None, timeout=30):
    head = {'Content-Type': 'application/json'}
    if token:
        head['Authorization'] = 'Bearer ' + token
    head.update(headers or {})
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, method=method, headers=head)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw, code = response.read(), response.status
    except urllib.error.HTTPError as error:
        raw, code = error.read(), error.code
    except Exception as error:  # noqa: BLE001
        return 0, str(error)
    try:
        return code, (json.loads(raw) if raw else None)
    except ValueError:
        return code, raw.decode(errors='replace')


def short(value, limit=160):
    text = value if isinstance(value, str) else json.dumps(value)
    return text if len(text) <= limit else text[:limit] + '...'


def af(method, path, body=None, timeout=30):
    return http(method, AGENTFIELD + path, KEY, body, timeout=timeout)


def rpc(method, params=None):
    with socket.socket(socket.AF_UNIX) as conn:
        conn.settimeout(300)
        conn.connect(SOCKET)
        conn.sendall((json.dumps({'id': 1, 'method': method, 'params': params or {}}) + '\n').encode())
        with conn.makefile() as stream:
            reply = json.loads(stream.readline())
    if reply.get('error'):
        raise RuntimeError(method + ': ' + str(reply['error'].get('message')))
    return reply.get('result')


def wait_until(predicate, seconds, every=2):
    deadline = time.time() + seconds
    value = None
    while time.time() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(every)
    return value


def node_ids():
    code, body = af('GET', '/api/v1/nodes')
    nodes = body.get('nodes', body) if isinstance(body, dict) else body
    if not isinstance(nodes, list):
        return code, []
    return code, sorted((n.get('id') or n.get('node_id') or '') for n in nodes)


def counters():
    """The control plane's run-owner claim counters, by result."""
    code, text = http('GET', AGENTFIELD + '/metrics', KEY)
    found = {}
    if isinstance(text, str):
        for line in text.splitlines():
            match = re.match(r'agentfield_run_owner_claims_total\{[^}]*result="([a-z_]+)"[^}]*\}\s+([0-9.e+]+)', line)
            if match:
                found[match.group(1)] = float(match.group(2))
    return found


# ---------------------------------------------------------------------------------------------
def control():
    print('--- control plane and nodes')
    code, body = http('GET', AGENTFIELD + '/api/v1/health')
    check('AgentField answers its health check', code == 200, f'{code}')
    ids = wait_until(lambda: (lambda c, n: n if {'swe-planner', 'pr-af', 'codeaf-factory'} <= set(n) else None)(*node_ids()), 120)
    code, ids = node_ids() if not ids else (200, ids)
    for node in ('swe-planner', 'pr-af', 'codeaf-factory'):
        check(f'{node} is registered on AgentField', node in ids, ids if node not in ids else '')
    diff = ('diff --git a/hello.py b/hello.py\n--- a/hello.py\n+++ b/hello.py\n@@ -1,2 +1,3 @@\n'
            ' def hello():\n-    return "hi"\n+    name = input()\n+    return "hi " + name\n')
    code, body = af('POST', '/api/v1/execute/pr-af.review', {'input': {'diff_text': diff, 'depth': 'quick'}}, timeout=90)
    # What is proved is that the control plane reached PR-AF and PR-AF's own code answered. With the keyless
    # stub the review itself cannot finish ("no review dimensions ran"), and that answer is PR-AF's, not a
    # transport failure. A real model key gets a real review here.
    text = short(body, 400).lower()
    transport = any(word in text for word in ('connection refused', 'no such host', 'unreachable', 'dial tcp', 'node not found', 'not registered', 'timed out'))
    answered = isinstance(body, dict) and bool(body.get('execution_id')) and not transport
    check('PR-AF is callable through AgentField (PR-AF itself answers a review request)', answered, f'{code} {short(body, 120)}')


# ---------------------------------------------------------------------------------------------
def join(link, username, name, secret):
    return http('POST', ORG + '/v1/factory-access', None,
                {'link': link, 'verb': 'sign_up', 'username': username, 'password': secret, 'name': name})


def org(operator_link):
    print('--- organization server')
    if not operator_link:
        check('an operator link was given', False, 'pass --operator-link (docker compose logs codeaf | grep "operator access link")')
        return
    # The link names the address the server prints (a laptop's loopback or the public name); this
    # script reaches the server on its network name instead, so only the link's secret part is used.
    secret = password()
    code, joined = join(operator_link, 'owner', 'Owner', secret)
    token = (joined or {}).get('access_token') if isinstance(joined, dict) else None
    check('the operator claims the organization with the printed link', code == 200 and bool(token) and joined.get('state') == 'joined',
          f'{code} state={joined.get("state") if isinstance(joined, dict) else joined}')
    code2, again = join(operator_link, 'intruder', 'Intruder', secret)
    check('the operator link works once', code2 != 200 or not (again or {}).get('access_token'), f'{code2} state={again.get("state") if isinstance(again, dict) else again}')
    if not token:
        return
    code, made = http('POST', ORG + '/v1/invites', token, {'name': 'Priya'})
    invite = (made or {}).get('url') if isinstance(made, dict) else None
    check('the operator makes an invite link', code in (200, 201) and bool(invite), f'{code}')
    if not invite:
        return
    code, joined2 = join(invite, 'priya', 'Priya', secret)
    member = (joined2 or {}).get('access_token') if isinstance(joined2, dict) else None
    check('a second person joins with the invite link', code == 200 and bool(member) and joined2.get('state') == 'joined', f'{code}')
    code3, reuse = join(invite, 'priya2', 'Priya Two', secret)
    check('an invite link works once', code3 != 200 or not (reuse or {}).get('access_token'), f'{code3} state={reuse.get("state") if isinstance(reuse, dict) else reuse}')
    code, people = http('GET', ORG + '/v1/people', token)
    handles = sorted(p.get('handle') for p in (people or {}).get('people', [])) if isinstance(people, dict) else []
    check('the operator sees both people', handles == ['owner', 'priya'], handles)
    code, policy = http('GET', ORG + '/v1/org/policy', member)
    check('the member can read the organization policy', code == 200, f'{code}')
    code, _ = http('PUT', ORG + '/v1/org/policy', member, {})
    check('the member cannot change the organization policy', code in (401, 403), f'{code}')
    STATE.write_text(json.dumps({'people': ['owner', 'priya'], 'password': secret}))
    STATE.chmod(0o600)


# ---------------------------------------------------------------------------------------------
PIPELINE = """pipeline:
  swe-af: [build]
steps:
  build:
    label: Build on AgentField
    kind: agentfield
    target: swe-planner.build_issue
    input:
      title: '{title}'
      body: '{body}'
      repo_path: %s
      plan_review: 'true'
      config: '{"check_ci": false}'
""" % FIXTURE_PATH


def make_fixture():
    root = Path(os.environ.get('SMOKE_FIXTURE', '/data/fixture/repo'))
    if not (root / '.git').exists():
        (root / '.codeaf').mkdir(parents=True, exist_ok=True)
        (root / 'README.md').write_text('# smoke fixture\n')
        (root / '.codeaf' / 'factory.yaml').write_text(PIPELINE)
        env = dict(os.environ, GIT_AUTHOR_NAME='smoke', GIT_AUTHOR_EMAIL='smoke@example.invalid',
                   GIT_COMMITTER_NAME='smoke', GIT_COMMITTER_EMAIL='smoke@example.invalid')
        for args in (['init', '-q', '-b', 'main'], ['add', '.'], ['commit', '-q', '-m', 'fixture']):
            subprocess.run(['git', '-C', str(root)] + args, check=True, env=env)
    return root


def job_view(factory, job):
    reply = rpc('factory.job', {'factory': factory, 'job': job})
    return reply.get('current', reply)


def waits_for_you(view, phrase):
    wait = view.get('wait') or {}
    return view.get('state') == 'needs_you' and wait.get('who') == 'you' and phrase in (wait.get('words') or '').lower()


def expect(view):
    return {'version': view.get('version'), 'state': view.get('state')}


def job():
    print('--- factory job through AgentField')
    ready = json.loads(Path('/data/ready.json').read_text())
    factory = ready['factory']
    before = counters()
    root = make_fixture()
    created = rpc('factory.job_create', {'factory': factory, 'folder': str(root), 'title': 'Add a Count method',
                                         'text': 'Smoke test: add a Count method and run the tests.',
                                         'start': 'now', 'assignee': '', 'pipeline': 'swe-af'})
    number = (created or {}).get('job') or (created or {}).get('number')
    check('a factory job is created and started on the pipeline that targets SWE-AF', bool(number), short(created))
    if not number:
        return
    view = wait_until(lambda: (lambda v: v if waits_for_you(v, 'approve the plan') else None)(job_view(factory, number)), 900, 3)
    check('the run reaches SWE-AF and its plan question reaches the owner', bool(view),
          short((view or job_view(factory, number)).get('wait')))
    if not view:
        return
    try:
        rpc('factory.job_request_changes', {'factory': factory, 'job': number, 'words': 'smoke: please revise once', 'expect': expect(view)})
        again = wait_until(lambda: (lambda v: v if waits_for_you(v, 'revised') else None)(job_view(factory, number)), 900, 3)
        check('the owner answers with a request for changes: the run resumes, replans and asks again', bool(again),
              short((again or job_view(factory, number)).get('wait')))
    except RuntimeError as error:
        again = None
        check('the owner answers with a request for changes: the run resumes, replans and asks again', False, error)
    view = again or job_view(factory, number)
    try:
        rpc('factory.job_reject_plan', {'factory': factory, 'job': number, 'words': 'smoke: stop here', 'expect': expect(view)})

        def ended():
            v = job_view(factory, number)
            steps = [(st.get('name'), st.get('state')) for st in (v.get('steps') or [])]
            gone = any(state in ('cancelled', 'failed', 'stopped') for _, state in steps)
            return v if gone or v.get('state') in ('stopped', 'closed', 'failed', 'done', 'cancelled') else None
        over = wait_until(ended, 300, 3)
        check('rejecting the plan ends the run', bool(over), short(job_view(factory, number).get('state')))
    except RuntimeError as error:
        check('rejecting the plan ends the run', False, error)
    after = counters()
    proven = after.get('proven', 0) - before.get('proven', 0)
    bad = {k: v for k, v in after.items() if k != 'proven' and v - before.get(k, 0) > 0}
    check('AgentField, in strict mode, accepted the factory\'s run-owner proofs and refused or accepted-unproven none',
          proven > 0 and not bad, f'proven +{proven:g} other {bad}')


# ---------------------------------------------------------------------------------------------
def after():
    print('--- after a restart')
    ids = wait_until(lambda: (lambda c, n: n if {'swe-planner', 'pr-af', 'codeaf-factory'} <= set(n) else None)(*node_ids()), 180)
    check('every node is registered again after the restart', bool(ids), ids)
    code, body = af('GET', '/api/ui/v2/workflow-runs?limit=5')
    count = len(body.get('runs', [])) if isinstance(body, dict) else 0
    check('AgentField still holds the earlier runs', code == 200 and count > 0, f'{code} runs={count}')


def after_org():
    print('--- the organization after a restart')
    code, signed = http('POST', ORG + '/v1/auth/password', None, {'username': 'owner', 'password': password()})
    check('the organization still knows the operator (password sign-in)', code == 200 and bool((signed or {}).get('access_token')), f'{code}')
    token = (signed or {}).get('access_token') if isinstance(signed, dict) else None
    if token:
        code, people = http('GET', ORG + '/v1/people', token)
        handles = sorted(p.get('handle') for p in (people or {}).get('people', [])) if isinstance(people, dict) else []
        check('and still knows the second person', handles == ['owner', 'priya'], handles)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('section', choices=['all', 'control', 'job', 'org', 'after', 'after-org'])
    parser.add_argument('--operator-link', default='')
    args = parser.parse_args()
    for name, run in (('control', control), ('job', job), ('org', lambda: org(args.operator_link)), ('after', after), ('after-org', after_org)):
        if args.section == name or (args.section == 'all' and name in ('control', 'job')):
            run()
    print('RESULT ' + ('PASS' if failures == 0 else f'FAIL ({failures} check(s) failed)'))
    sys.exit(1 if failures else 0)


if __name__ == '__main__':
    main()

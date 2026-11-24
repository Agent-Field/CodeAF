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
  hosted-prepare (in the codeaf container, after `org`) makes the chats the hosted section uses
  hosted-seed (in the sessionhost container, ids piped in from `hosted-prepare`) lets those chats run commands
             without a person at a keyboard: see the section's comment
  hosted     (in the codeaf container, after `org` and the two above; needs the `hosted` profile and a codeaf with the
             chats' machine in it) two people each run chats on the chats' machine through
             `codeaf chat --org`: isolation, secrets, the network, who may attach, leaving and coming back
  hosted-host (in the sessionhost container, state piped in from `hosted`) what only the machine itself can
             show: no process of a stopped chat is left, no org secret is anywhere on its volume
  hosted-after (in the codeaf container, after the chats' machine restarted) the chats are still there,
             their files are intact and a person gets back into one

An organization server takes a password only over https or from its own machine, so `org` and
`after-org` run inside the codeaf container with ORG_URL=http://127.0.0.1:8787 (smoke.sh does).

It prints PASS or FAIL per check and exits non-zero if any failed. Secrets are never printed.
Everything the model is asked goes to the scripted stub, so it spends nothing; the stub's answers
are minimal and the "work" is not real.
"""
import argparse
import base64
import glob
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
from urllib.parse import urlparse

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


# ---------------------------------------------------------------------------------------------
# Hosted chats. Every check here goes through the real client door (`codeaf chat --org <url> --once`) or
# the organization server's own HTTP address, as a person would; what only the machine itself can show
# is checked by `hosted-host`, run inside the sessionhost container by smoke.sh.
HOSTED_STATE = Path(os.environ.get('SMOKE_HOSTED_STATE', '/data/smoke-hosted.json'))
HOSTED_IDS = Path(os.environ.get('SMOKE_HOSTED_IDS', '/data/smoke-hosted-ids.json'))
CLIENT_HOME = Path(os.environ.get('SMOKE_CLIENT_HOME', '/tmp/smoke-client'))
CLIENT_CWD = CLIENT_HOME / 'cwd'
STUB = os.environ.get('STUB_URL', 'http://modelstub:8890').rstrip('/')
GITSTUB = os.environ.get('GITSTUB_URL', 'http://gitstub:8891').rstrip('/')
# The key the organization's model door sends upstream (.env.stub.hosted sets it, and it is the stub's, not a secret): distinct
# from every other service's, so the stub's record of calls shows exactly what the hosted chats spent.
HOSTED_ORG_KEY = os.environ.get('SMOKE_HOSTED_MODEL_KEY', 'stub-hosted-org-key-0001')
GIT_TOKEN = os.environ.get('GITSTUB_TOKEN', '')
STUB_COST = float(os.environ.get('SMOKE_STUB_COST_USD', '0.01'))
SESSION_CAP = float(os.environ.get('SMOKE_SESSION_CAP_USD', '0.50'))
P1_STATE = Path(os.environ.get('SMOKE_P1_STATE', '/data/smoke-p1.json'))
# What a chat's shell may not reach on the private network, and the suffix Railway's names carry. On Compose the
# chats' machine is on a network of its own and the names do not resolve; on Railway they do, and the kernel answers.
NET_SUFFIX = os.environ.get('SMOKE_NET_SUFFIX', '')
NO_SUCH_CHAT = 'There is no such chat.'
UNKNOWN_CHAT = 'hs_' + '0' * 24
BANNED_WORDS = ('uid', 'sandbox', 'sessionhost', 'fence', 'landlock', 'daemon')


def sign_in(username):
    code, signed = http('POST', ORG + '/v1/auth/password', None, {'username': username, 'password': password()})
    return (signed or {}).get('access_token') if code == 200 and isinstance(signed, dict) else None


def client_env(token):
    env = {k: v for k, v in os.environ.items() if not k.startswith(('CODEAF_', 'AGENTFIELD_', 'OPENROUTER_', 'OPENAI_'))}
    env.update(CODEAF_ORG_TOKEN=token, HOME=str(CLIENT_HOME), CODEAF_HOME=str(CLIENT_HOME / 'codeaf'),
               CODEAF_TELEMETRY='off', DO_NOT_TRACK='1', CODEAF_NO_UPDATE_CHECK='1')
    return env


def client_argv(text, session=None):
    argv = ['codeaf', 'chat', '--org', ORG, '--once', text]
    return argv + (['--session', session] if session else [])


def chat(token, text, session=None, timeout=150):
    """One message to a hosted chat through the client door. Returns (exit status, stdout, stderr)."""
    CLIENT_CWD.mkdir(parents=True, exist_ok=True)
    done = subprocess.run(client_argv(text, session), cwd=CLIENT_CWD, env=client_env(token), stdin=subprocess.DEVNULL,
                          capture_output=True, text=True, timeout=timeout)
    return done.returncode, done.stdout, done.stderr


def field(out, name):
    """The value of a `NAME value` item a scripted command printed. The first one follows the stub's own "STUB ran: "."""
    match = re.search(r'(?:^|[\s:])' + re.escape(name) + r' ([^\n]*)', out)
    return match.group(1).strip() if match else None


def trace(label, marker, status, out, err):
    """Say what a scripted command really answered when the item the next checks read is missing from it."""
    if marker not in out:
        print('  trace %s: exit %s, out %r, err %r' % (label, status, short(out, 300), short(err, 300)))


def hosted_sessions(token, headers=None):
    code, body = http('GET', ORG + '/v1/hosted/sessions', token, headers=headers)
    rows = body.get('sessions') if isinstance(body, dict) else None
    return code, rows if isinstance(rows, list) else []


def wire_answer(token, session):
    """What the organization server says to a WebSocket upgrade on a chat's wire, by hand: (status, body)."""
    url = urlparse(ORG)
    port = url.port or (443 if url.scheme == 'https' else 80)
    with socket.create_connection((url.hostname, port), 15) as conn:
        conn.settimeout(15)
        key = base64.b64encode(os.urandom(16)).decode()
        conn.sendall(('GET /v1/hosted/sessions/%s/wire HTTP/1.1\r\nHost: %s:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n'
                      'Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nAuthorization: Bearer %s\r\n\r\n'
                      % (session, url.hostname, port, key, token)).encode())
        raw = b''
        while True:
            try:
                piece = conn.recv(65536)
            except socket.timeout:
                break
            if not piece:
                break
            raw += piece
    head, _, body = raw.partition(b'\r\n\r\n')
    status = int(head.split(b' ', 2)[1]) if head.startswith(b'HTTP/') else 0
    return status, body.decode(errors='replace').strip()


def hosted_prepare():
    """Makes the chats the test uses (two for the owner, two for the member) and writes their names to
    HOSTED_IDS, which smoke.sh pipes to `hosted-seed` on the chats' machine before anyone types."""
    print('--- hosted chats: making the chats')
    ids = {}
    for who, name in (('owner', 'owner_first'), ('owner', 'owner_second'), ('priya', 'member'), ('priya', 'member_cap')):
        token = sign_in(who)
        code, made = http('POST', ORG + '/v1/hosted/sessions', token, {}) if token else (0, None)
        ids[name] = (made or {}).get('id', '') if isinstance(made, dict) else ''
        check(f'{who} is given a chat ({name})', code in (200, 201) and ids[name].startswith('hs_'), f'{code} {short(made, 120)}')
    HOSTED_IDS.write_text(json.dumps(ids))
    HOSTED_IDS.chmod(0o600)


def as_user(uid, gid, work):
    """The JSON-able result of `work()` run AS a chat's own user, or None when it failed there.

    The chats' machine has no right to read a chat's closed folder or to look inside its processes, root included, so
    a check made from the machine's side would find nothing and pass for the wrong reason. A short-lived child takes
    the chat's identity (the machine keeps two capabilities for exactly that) and looks, and says only what it found."""
    reader, writer = os.pipe()
    child = os.fork()
    if child == 0:
        code = 1
        try:
            os.close(reader)
            os.setgroups([])
            os.setresgid(gid, gid, gid)
            os.setresuid(uid, uid, uid)
            os.write(writer, json.dumps(work()).encode())
            code = 0
        except Exception:
            pass
        finally:
            os._exit(code)
    os.close(writer)
    data = b''
    while True:
        part = os.read(reader, 65536)
        if not part:
            break
        data += part
    os.close(reader)
    _, status = os.waitpid(child, 0)
    return json.loads(data.decode()) if status == 0 and data else None


def read_as_chat_user(chat_dir, path):
    """The text of `path` read as the chat's own user, or None when it is missing or unreadable even to that user."""
    info = os.stat(chat_dir)

    def read():
        with open(path) as handle:
            return handle.read()
    return as_user(info.st_uid, info.st_gid, read)


def hosted_seed():
    """Run inside the sessionhost container as root, with HOSTED_IDS on stdin.

    A `--once` message has nobody at a keyboard to answer a command's approval card, so under the ordinary rules the
    chat's first command is refused ("needs approval but no resolver is attached"), exactly as it is for a local
    `codeaf chat --once`. The test is about where commands run and what they can reach, not about approval, so each
    chat's own settings file is given the one row that lets its commands run: `tools.approvalMode: allow`. It is
    written as the chat's own user, the way the chat's own settings are, and nothing else is set."""
    print('--- hosted chats: letting the test chats run commands')
    ids = json.load(sys.stdin)
    for name, session in ids.items():
        folders = [p for p in __import__('glob').glob('/chats/people/*/' + session) if os.path.isdir(p)]
        check(f'the {name} chat has a folder on the chats\' machine', len(folders) == 1, folders)
        if len(folders) != 1:
            continue
        info = os.stat(folders[0])
        path = os.path.join(folders[0], 'home', 'config.json')
        # The folder is the chat's own user's and closed to everyone else, root included (the machine's root holds
        # no right to read it), so the file is written AS that user: a short-lived child takes the chat's identity
        # with the two capabilities the machine has for exactly that, and writes.
        child = os.fork()
        if child == 0:
            code = 1
            try:
                os.setgroups([])
                os.setresgid(info.st_gid, info.st_gid, info.st_gid)
                os.setresuid(info.st_uid, info.st_uid, info.st_uid)
                with open(path, 'w') as handle:
                    json.dump({'tools.approvalMode': 'allow'}, handle)
                os.chmod(path, 0o600)
                code = 0
            finally:
                os._exit(code)
        _, status = os.waitpid(child, 0)
        check(f'the {name} chat\'s commands may run', status == 0, f'exit status {status}')


def hosted():
    print('--- hosted chats')
    owner, member = sign_in('owner'), sign_in('priya')
    check('both people sign in to the organization', bool(owner and member))
    if not (owner and member):
        return
    shared = {}
    CLIENT_CWD.mkdir(parents=True, exist_ok=True)
    cwd_before = sorted(os.listdir(CLIENT_CWD))

    ids = json.loads(HOSTED_IDS.read_text())
    first_id, second_id, member_id = ids['owner_first'], ids['owner_second'], ids['member']

    # C1: a whole coding session (shell, git, a worktree) runs on the chats' machine, not here.
    first = ('bash: git init -q -b main . && git -c user.name=smoke -c user.email=smoke@example.invalid commit -q --allow-empty -m init '
             '&& git worktree add -q wt && echo A-ONLY > secretA && echo hi > wt/f '
             '&& echo "STATUS $(git -C wt status --short)" && echo "HOST $(hostname)" && echo "PWD $(pwd)" && echo "UID $(id -u)"')
    status, out, err = chat(owner, first, session=first_id)
    check('C1 a chat opens through `codeaf chat --org` and runs a shell, git and a worktree', status == 0 and 'STUB ran' in out and field(out, 'STATUS') == '?? f',
          short(out + err, 300))
    host = field(out, 'HOST')
    check('C1 the commands ran on another machine than the organization server', bool(host) and host != socket.gethostname(), f'{host} vs {socket.gethostname()}')
    work = field(out, 'PWD') or ''
    check('C1 the chat works in a folder of its own on that machine', work.startswith('/chats/') and work.endswith('/work'), work)
    uid = field(out, 'UID') or ''
    check('C1 the chat runs as a user of its own, not root and not the server\'s', uid.isdigit() and int(uid) >= 200000, uid)
    check('C1 nothing was written in the folder the client ran from', sorted(os.listdir(CLIENT_CWD)) == cwd_before, os.listdir(CLIENT_CWD))
    code, rows = hosted_sessions(owner)
    check('C1 the owner sees their own two chats and not the other person\'s', code == 200 and sorted(r['id'] for r in rows) == sorted([first_id, second_id]),
          f'{code} {len(rows)}')
    shared['owner_first'] = {'id': first_id, 'work': work}
    status, again, _ = chat(owner, 'bash: cat wt/f; cat secretA', session=first_id)
    check('C1 a later message reaches the same chat and its files', status == 0 and 'hi' in again and 'A-ONLY' in again, short(again, 200))

    # The owner's second chat and the member's chat each leave a process of their own running.
    status, out2, _ = chat(owner, 'bash: pwd | sed "s/^/PWD /"; setsid sleep 600 </dev/null >/dev/null 2>&1 & echo PID $!', session=second_id)
    second_work, second_pid = field(out2, 'PWD') or '', field(out2, 'PID') or ''
    status, out1, _ = chat(owner, 'bash: setsid sleep 600 </dev/null >/dev/null 2>&1 & echo PID $!', session=first_id)
    first_pid = field(out1, 'PID') or ''
    check('C7 each of the owner\'s two chats left a process of its own running', first_pid.isdigit() and second_pid.isdigit(), f'{first_pid} {second_pid}')
    shared['owner_first']['pid'] = first_pid
    shared['owner_second'] = {'id': second_id, 'work': second_work, 'pid': second_pid}

    status, out, _ = chat(member, 'bash: pwd | sed "s/^/PWD /"; echo mine > secretB; echo "UID $(id -u)"; setsid sleep 600 </dev/null >/dev/null 2>&1 & echo PID $!', session=member_id)
    member_work, member_pid = field(out, 'PWD') or '', field(out, 'PID') or ''
    check('C1 a second person has a chat of their own, in a different folder', bool(member_work) and member_work != work,
          f'{member_work}')
    shared['member'] = {'id': member_id, 'work': member_work, 'pid': member_pid}

    # C7: a person's limit counts chats that are RUNNING, so a third chat can be made (it is only a folder and a
    # name) but cannot be started while two run, and the refusal is a sentence, not a stack trace.
    code, made = http('POST', ORG + '/v1/hosted/sessions', owner, {})
    third_id = (made or {}).get('id', '') if isinstance(made, dict) else ''
    check('C7 a third chat can be made while two run (it is not started yet)', code == 201 and third_id.startswith('hs_'), f'{code} {short(made, 120)}')
    status, out, err = chat(owner, 'hello', session=third_id) if third_id else (1, '', '')
    refused = out + err
    check('C7 starting a third running chat is refused in plain words', status != 0 and 'You already have 2 chats running; stop one first.' in refused,
          f'exit {status} {short(refused, 200)}')
    sentence = refused.strip()
    shared['owner_third'] = {'id': third_id}

    # C2: the member's chat tries to reach the owner's chat.
    probe = ('cat {w}/secretA 2>&1; echo "READ $?"; echo planted > {w}/planted 2>&1; echo "WRITE $?"; ls {w} 2>&1; echo "LIST $?"; '
             'kill -0 {p} 2>&1; echo "SIGNAL $?"; ls /proc/{p}/fd >/dev/null 2>&1; echo "ENVIRON $?"; '
             'cat {w}/../home/.bash_history 2>&1; echo "HOME $?"; echo "DATA $(ls -A /data 2>/dev/null | wc -l)"').format(w=work, p=first_pid)
    status, out, err = chat(member, 'bash: ' + probe, session=member_id)
    trace('the member\'s reach probe', 'READ ', status, out, err)
    others = {name: field(out, name) for name in ('READ', 'WRITE', 'LIST', 'SIGNAL', 'ENVIRON', 'HOME', 'DATA')}
    check('C2 another chat\'s files cannot be read', others['READ'] not in (None, '0') and 'A-ONLY' not in out, others['READ'])
    check('C2 another chat\'s folder cannot be written to or listed', others['WRITE'] not in (None, '0') and others['LIST'] not in (None, '0'),
          f"{others['WRITE']} {others['LIST']}")
    check('C2 another chat\'s processes cannot be signalled or read', others['SIGNAL'] not in (None, '0') and others['ENVIRON'] not in (None, '0'),
          f"{others['SIGNAL']} {others['ENVIRON']}")
    check('C2 another chat\'s home cannot be read', others['HOME'] not in (None, '0'), others['HOME'])
    check('C2 none of the organization server\'s own data is on the chats\' machine (the folder it would be in is empty)', others['DATA'] == '0', others['DATA'])
    status, out, _ = chat(owner, 'bash: cat %s/secretB 2>&1; echo "READ $?"; cat secretA' % member_work, session=second_id)
    check('C2 the same person\'s other chat cannot read a chat either', field(out, 'READ') not in (None, '0') and 'mine' not in out, short(out, 160))

    # C2 beyond files: what every chat on the machine shares and no chat owns. Abstract Unix sockets have no owner and no
    # file mode at all, and /dev/shm and /tmp are one folder for everybody, so the fence and the umask are what keep
    # chats apart there. The first chat leaves each of them behind; the member's chat reaches for them.
    abstract_server = ('import socket\ns = socket.socket(socket.AF_UNIX)\ns.bind("\\0gap")\ns.listen(5)\n'
                       'while True:\n c, _ = s.accept()\n c.sendall(b"ABSTRACT-OPEN")\n c.close()')
    engine_connect = ('import socket, hashlib, os, sys\nw = sys.argv[1]\n'
                      'p = os.path.join(os.path.dirname(w), "home", "v3", "hosts", hashlib.sha256(w.encode()).digest()[:8].hex(), "host.sock")\n'
                      's = socket.socket(socket.AF_UNIX)\ntry:\n s.connect(p)\n print("%s connected")\nexcept OSError as e:\n print("%s refused", e.errno)')
    leave = ("echo a-secret > /dev/shm/gap-a; echo a-secret > /tmp/gap-a; "
             "setsid python3 -c '" + abstract_server + "' </dev/null >/dev/null 2>&1 & sleep 1; "
             'python3 -c \'' + (engine_connect % ('OWNENGINE', 'OWNENGINE')) + '\' "$PWD"')
    status, out, err = chat(owner, 'bash: ' + leave, session=first_id)
    trace('the first chat leaving things behind', 'OWNENGINE ', status, out, err)
    own_engine = field(out, 'OWNENGINE') or ''
    reach = ('cat /dev/shm/gap-a 2>&1; echo "SHM $?"; cat /tmp/gap-a 2>&1; echo "TMP $?"; '
             "python3 -c 'import socket\ns = socket.socket(socket.AF_UNIX)\ns.settimeout(3)\ntry:\n s.connect(\"\\0gap\")\n print(\"ABS connected\", s.recv(20))\n"
             "except OSError as e:\n print(\"ABS refused\", e.errno)'; "
             "python3 -c 'import socket\na = socket.socket(socket.AF_UNIX)\na.bind(\"\\0gap-own\")\na.listen(1)\nb = socket.socket(socket.AF_UNIX)\nb.connect(\"\\0gap-own\")\nprint(\"PAIR ok\")'; "
             "python3 -c '" + (engine_connect % ('ENGINE', 'ENGINE')) + "' " + work)
    status, out, err = chat(member, 'bash: ' + reach, session=member_id)
    trace('the member reaching for what the machine shares', 'SHM ', status, out, err)
    check('C2 the first chat could reach its own engine (so the next check means something)', own_engine.startswith('connected'), own_engine)
    check('C2 a file another chat left in /dev/shm cannot be read (the usual umask is closed to others)', field(out, 'SHM') not in (None, '0') and 'a-secret' not in out, field(out, 'SHM'))
    check('C2 a file another chat left in /tmp cannot be read', field(out, 'TMP') not in (None, '0'), field(out, 'TMP'))
    check('C2 another chat\'s abstract Unix socket cannot be connected to', (field(out, 'ABS') or '').startswith('refused'), field(out, 'ABS'))
    check('C2 a command can still use an abstract socket of its own (so that check means something)', field(out, 'PAIR') == 'ok', field(out, 'PAIR'))
    check('C2 another chat\'s engine cannot be connected to', (field(out, 'ENGINE') or '').startswith('refused'), field(out, 'ENGINE'))

    # C3 and C3b: what a chat's shell can find, and where it can go.
    # `denied` is the kernel refusing the connection (permission denied): the answer a port nobody allowed gives. `closed` is
    # anything else (no such name, nothing listening, no route).
    net = ('p() { e=$(timeout 4 bash -c "exec 3<>/dev/tcp/$1/$2" 2>&1) && echo "NET $1:$2 open" || '
           '{ case "$e" in *ermission*) echo "NET $1:$2 denied";; *) echo "NET $1:$2 closed";; esac; }; }; '
           'h() { exec 3<>/dev/tcp/$1/$2; printf "$3 $4 HTTP/1.0\\r\\nHost: x\\r\\n\\r\\n" >&3; head -n1 <&3; }; '
           'p agentfield@X 8080; p modelstub@X 8890; p swe-af@X 8003; p pr-af@X 8007; p 169.254.169.254 80; p codeaf 8787; p sessionhost 8790; '
           'p 127.0.0.1 8790; '
           'echo "SESSIONHOST $(h sessionhost 8790 GET /v1/sessions 2>&1)"; '
           'echo "ORG $(h codeaf 8787 GET /v1/hosted/sessions 2>&1)"; echo "MODEL $(h codeaf 8787 POST /v1/model/chat/completions 2>&1)"').replace('@X', NET_SUFFIX)
    # What a chat's shell holds in its environment and in its folders is read from the machine's side (hosted_host),
    # because the engine rightly refuses a command that searches for secrets; here the shell only tries to reach things.
    status, out, err = chat(owner, 'bash: ' + net, session=first_id)
    trace('the network probe', 'NET ', status, out, err)
    closed = ('agentfield%s:8080' % NET_SUFFIX, 'modelstub%s:8890' % NET_SUFFIX, 'swe-af%s:8003' % NET_SUFFIX, 'pr-af%s:8007' % NET_SUFFIX, '169.254.169.254:80')
    check('C3b the chat cannot reach AgentField, the nodes, the model stub or the metadata address',
          all('NET %s open' % target not in out and 'NET %s' % target in out for target in closed), short(re.findall(r'^NET .*$', out, re.M), 400))
    # P1-0c: a port nobody allowed is refused by the kernel itself, which is what closes the services of a private network that
    # has no policies of its own (Railway's). Here the chats' machine's own API is the one that answers on this network: its
    # name resolves to the machine the chat runs on, and so does loopback. The organization's own door is the port that is let through.
    denied = ('sessionhost:8790', '127.0.0.1:8790') + (closed[:4] if NET_SUFFIX else ())
    check('P1-0c a command in a chat is REFUSED (permission denied) on the ports of the chats\' machine' + (', AgentField, the nodes and the model stub' if NET_SUFFIX else ''),
          all('NET %s denied' % target in out for target in denied), short(re.findall(r'^NET .*$', out, re.M), 500))
    check('P1-0c the organization server\'s own door is the port that stays open to a chat', 'NET codeaf:8787 open' in out, short(re.findall(r'^NET codeaf.*$', out, re.M), 100))
    status, refused_out, refused_err = chat(owner, 'bash: exec 3<>/dev/tcp/127.0.0.1/8790', session=first_id)
    check('P1-0c the refusal says in one plain sentence what a chat\'s commands may reach', 'only on the ports the organization allows' in refused_out + refused_err,
          short(refused_out + refused_err, 300))
    check('C3b the chats\' machine API cannot be used by a chat that has no secret (refused outright, or 401)',
          '401' in (field(out, 'SESSIONHOST') or '') or 'denied' in (re.findall(r'^NET sessionhost:8790 (\w+)$', out, re.M) or [''])[0], field(out, 'SESSIONHOST'))
    check('C3b the organization server refuses a chat that has no sign-in', any(c in (field(out, 'ORG') or '') for c in ('401', '403')), field(out, 'ORG'))
    check('C3b the model door refuses a chat that has no model bearer', '401' in (field(out, 'MODEL') or ''), field(out, 'MODEL'))

    # C10: the member cannot get, stop, delete or attach the owner's chat, and cannot tell it from one that never was.
    code_u, body_u = http('GET', ORG + '/v1/hosted/sessions/' + UNKNOWN_CHAT, member)
    wire_u = wire_answer(member, UNKNOWN_CHAT)
    check('C10 an unknown chat is "There is no such chat."', code_u == 404 and isinstance(body_u, dict) and body_u.get('error') == NO_SUCH_CHAT, f'{code_u} {body_u}')
    for verb, method, suffix in (('get', 'GET', ''), ('stop', 'POST', '/stop'), ('delete', 'DELETE', '')):
        code, body = http(method, ORG + '/v1/hosted/sessions/' + first_id + suffix, member, {} if method == 'POST' else None)
        code_n, body_n = http(method, ORG + '/v1/hosted/sessions/' + UNKNOWN_CHAT + suffix, member, {} if method == 'POST' else None)
        check(f'C10 another person cannot {verb} a chat, and the answer is the same as for one that does not exist',
              (code, body) == (code_n, body_n) and code == 404, f'{code} {body} | {code_n} {body_n}')
    wire = wire_answer(member, first_id)
    check('C10 another person cannot attach to a chat, with the same answer as for one that does not exist', wire == wire_u and wire[0] == 404, f'{wire} | {wire_u}')
    code, rows = hosted_sessions(owner, {'X-Codeaf-Person': 'priya'})
    check('C10 a person-name header sent by a client changes nothing: the owner still sees only their own chats',
          code == 200 and sorted(r['id'] for r in rows) == sorted([first_id, second_id, third_id]), f'{code} {[r["id"] for r in rows]}')
    code, rows = hosted_sessions(owner)
    check('C10 the owner\'s chats are all still there after the other person\'s attempts', sorted(r['id'] for r in rows) == sorted([first_id, second_id, third_id]))
    code, rows = hosted_sessions(None)
    check('C10 a request with no sign-in is refused', code in (401, 403), f'{code}')

    # C5: leave in the middle of a turn, come back, find the work done.
    CLIENT_CWD.mkdir(parents=True, exist_ok=True)
    running = subprocess.Popen(client_argv('bash: sleep 10; echo C5-DONE > c5.txt', first_id), cwd=CLIENT_CWD, env=client_env(owner),
                               stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(5)
    running.kill()
    running.wait()
    time.sleep(14)
    status, out, err = chat(owner, 'bash: cat c5.txt', session=first_id)
    check('C5 a turn goes on after its client is killed, and the result is there on coming back', 'C5-DONE' in out, short(out + err, 200))
    # A command a killed client left behind can finish by itself; what only a live engine does is carry the turn, so the
    # conversation must not come back saying a tool call was cut off.
    check('C5 the conversation comes back with the turn finished, not cut off', 'cut off' not in (out + err), short(out + err, 200))

    HOSTED_STATE.write_text(json.dumps(shared))
    HOSTED_STATE.chmod(0o600)

    # C7: stopping a chat ends everything it started; a stop is a stop for that chat only.
    code, stopped = http('POST', ORG + '/v1/hosted/sessions/' + second_id + '/stop', owner, {})
    check('C7 stopping a chat is accepted', code in (200, 202, 204), f'{code} {short(stopped, 120)}')
    time.sleep(3)
    code, rows = hosted_sessions(owner)
    states = {r['id']: r.get('state') for r in rows}
    check('C7 the stopped chat is no longer running and the other one still is', states.get(second_id) != 'running' and states.get(first_id) == 'running', states)
    status, out, err = chat(owner, 'hello again', session=third_id) if third_id else (1, '', '')
    check('C7 stopping a chat frees the person\'s slot (the third chat now starts)', status == 0 and 'STUB noted' in out, f'exit {status} {short(out + err, 160)}')
    code, deleted = http('DELETE', ORG + '/v1/hosted/sessions/' + second_id, owner)
    check('C7 deleting a chat is accepted', code in (200, 202, 204), f'{code}')
    code, gone = http('GET', ORG + '/v1/hosted/sessions/' + second_id, owner)
    check('C7 a deleted chat is gone', code == 404 and isinstance(gone, dict) and gone.get('error') == NO_SUCH_CHAT, f'{code} {gone}')

    # C9: nothing a person read above used the machine's words.
    leaked = [word for word in BANNED_WORDS if word in (NO_SUCH_CHAT + sentence).lower()]
    check('C9 the sentences a person reads use none of the machine\'s words', not leaked, leaked)


def hosted_host():
    """Run inside the sessionhost container as root, with `hosted`'s state on stdin."""
    print('--- hosted chats: what only the machine can show')
    state = json.load(sys.stdin)
    stopped = state['owner_second']
    folder = os.path.dirname(stopped['work'])
    uid_of_stopped = os.stat(folder).st_uid if os.path.isdir(folder) else None
    check('C7 the stopped chat\'s folder is gone, or at least no longer holds its process', not stopped['pid'] or not os.path.exists('/proc/' + stopped['pid']),
          stopped['pid'])

    def owned_by(uid):
        found = []
        for entry in os.listdir('/proc'):
            if entry.isdigit():
                try:
                    if os.stat('/proc/' + entry).st_uid == uid:
                        found.append(entry)
                except OSError:
                    pass
        return found
    if uid_of_stopped is not None:
        check('C7 no process of the stopped chat\'s user is left, including one started with setsid', owned_by(uid_of_stopped) == [], owned_by(uid_of_stopped))
    for name in ('owner_first', 'member'):
        row = state[name]
        check(f'C7 the {name} chat\'s own process was not touched by the other chat\'s stop', row['pid'] and os.path.exists('/proc/' + row['pid']), row['pid'])
    secret = os.environ.get('CODEAF_SESSIONHOST_SECRET', '')
    check('C3 the secret was read from this container\'s environment (so the next check means something)', len(secret) >= 32)
    needle = secret.encode()
    forbidden = (b'HOSTED_SECRET', b'SESSIONHOST_SECRET', b'FACTORY_TOKEN', b'AGENTFIELD', b'GH_TOKEN', b'OPENROUTER')
    people = glob.glob('/chats/people/*/hs_*')

    def tree_scan(folder):
        def work():
            files, hits = 0, []
            for root, _, names in os.walk(folder):
                for name in names:
                    path = os.path.join(root, name)
                    try:
                        if os.path.getsize(path) < 50_000_000:
                            files += 1
                            if needle in open(path, 'rb').read():
                                hits.append(path)
                    except OSError:
                        pass
            return {'files': files, 'hits': hits}
        return work

    found, scanned = [], 0
    for folder in people:
        info = os.stat(folder)
        result = as_user(info.st_uid, info.st_gid, tree_scan(folder)) or {'files': 0, 'hits': ['could not look: ' + folder]}
        scanned += result['files']
        found += result['hits']
    check('C3 the shared secret is nowhere in any chat\'s folders (each folder read as its own user)', bool(secret) and scanned > 0 and not found,
          found[:3] or f'{scanned} files read')

    def env_scan(uid):
        def work():
            rows = []
            for entry in os.listdir('/proc'):
                if not entry.isdigit():
                    continue
                try:
                    if os.stat('/proc/' + entry).st_uid != uid:
                        continue
                    environ = open('/proc/%s/environ' % entry, 'rb').read()
                    command = open('/proc/%s/cmdline' % entry, 'rb').read().split(b'\0')[0].decode('utf-8', 'replace')
                    names = sorted(item.split(b'=')[0].decode('utf-8', 'replace') for item in environ.split(b'\0') if item)
                    rows.append({'command': os.path.basename(command), 'names': names,
                                 'secret': needle in environ, 'bad': [f.decode() for f in forbidden if f in environ]})
                except OSError:
                    pass
            return rows
        return work

    rows = []
    for uid in sorted({os.stat(folder).st_uid: None for folder in people}):
        rows += as_user(uid, uid, env_scan(uid)) or []
    shells = [row for row in rows if row['command'] == 'sleep']
    check('C3 the environment of the processes the chats\' shells started was read (so the next checks mean something)', bool(rows) and bool(shells),
          f'{len(rows)} processes, {len(shells)} started from a shell')
    check('C3 no chat process holds the shared secret in its environment', bool(rows) and not [r for r in rows if r['secret']], [r['command'] for r in rows if r['secret']])
    check('C3 the chat\'s shell has none of the organization\'s secrets or model keys in its environment',
          bool(shells) and not [r for r in shells if r['bad'] or 'OPENAI_API_KEY' in r['names']],
          [(r['command'], r['bad'], r['names']) for r in shells][:2])
    # What the kernel answers another chat's user about a chat's process, asked by a plain script running as that user
    # (so the engine's own wording check on commands is not what refuses). The chat's own user is the control.
    import ctypes
    target = state['owner_first']['pid']

    def about_process(pid):
        def work():
            libc = ctypes.CDLL(None, use_errno=True)
            answers = {}
            for name, path, size in (('environ', 'environ', 1), ('mem', 'mem', 0)):
                try:
                    with open('/proc/%s/%s' % (pid, path), 'rb') as handle:
                        if size:
                            handle.read(size)
                    answers[name] = 0
                except OSError as error:
                    answers[name] = error.errno
            answers['ptrace'] = 0 if libc.ptrace(16, int(pid), 0, 0) == 0 else ctypes.get_errno()
            if answers['ptrace'] == 0:
                libc.ptrace(17, int(pid), 0, 0)
            return answers
        return work

    first_dir = os.path.dirname(state['owner_first']['work'])
    member_dir = os.path.dirname(state['member']['work'])
    own = as_user(os.stat(first_dir).st_uid, os.stat(first_dir).st_gid, about_process(target)) if os.path.exists('/proc/' + target) else None
    other = as_user(os.stat(member_dir).st_uid, os.stat(member_dir).st_gid, about_process(target)) if os.path.exists('/proc/' + target) else None
    # Only environ has a meaningful control: reading memory or attaching with ptrace is refused to the same user too where the
    # kernel's Yama setting is restrictive, so for those two the answer is Yama's and the user rule's together (a different
    # user needs CAP_SYS_PTRACE, which no chat has).
    check('C2 the first chat\'s own user can read its own process\'s environment (so the next checks mean something)', bool(own) and own.get('environ') == 0, own)
    for name in ('environ', 'mem', 'ptrace'):
        check(f'C2 another chat\'s user is refused by the kernel: its process\'s {name}', bool(other) and other.get(name) in (1, 13), (other or {}).get(name))

    chat_dir = os.path.dirname(state['owner_first']['work'])
    planted = os.path.join(state['owner_first']['work'], 'planted')
    check('C2 nothing another chat tried to write reached this chat\'s folder', read_as_chat_user(chat_dir, planted) is None, planted)
    mine = os.path.join(state['owner_first']['work'], 'secretA')
    check('C2 and this chat\'s file is as it was left', (read_as_chat_user(chat_dir, mine) or '').strip() == 'A-ONLY')
    out = subprocess.run(['codeaf', 'sessionhost', 'probe'], capture_output=True, text=True)
    try:
        report = json.loads(out.stdout)
    except ValueError:
        report = {}
    check('C4 the machine measured itself and can keep chats apart (mode and fence recorded)', out.returncode == 0 and report.get('ok') is True and bool(report.get('mode')),
          {k: report.get(k) for k in ('ok', 'mode', 'fence_mode', 'require', 'cap_names_eff')})


def hosted_after():
    print('--- hosted chats after the chats\' machine restarted')
    owner = sign_in('owner')
    state = json.loads(HOSTED_STATE.read_text())
    first = state['owner_first']
    code, rows = hosted_sessions(owner)
    check('C6 the owner\'s chat is still listed after the restart', any(r['id'] == first['id'] for r in rows), [r['id'] for r in rows])
    # The chats' machine forgot the model bearers with its memory. Since P1 the organization server notices that when the person
    # attaches (the machine says which access it holds) and hands over a fresh one before anything is sent, so the FIRST attach
    # after a restart works: no "no model access yet", no retry.
    status, out, err = chat(owner, 'bash: cat secretA; cat wt/f; cat c5.txt', session=first['id'])
    check('P1-7 the person is back in the chat on the first attach after the restart, files intact', 'A-ONLY' in out and 'C5-DONE' in out and 'no model access' not in out + err,
          short((status, (out + err).strip()), 300))


def hosted_leftovers():
    """Run on the sessionhost right after it restarted, before anyone goes back in: nothing a chat started is still
    running (found by what it is, never by a process number, which a restarted machine hands out afresh)."""
    print('--- hosted chats: after the restart, before anyone goes back in')
    left = []
    for entry in os.listdir('/proc'):
        if not entry.isdigit():
            continue
        try:
            uid = os.stat('/proc/' + entry).st_uid
            argv = open('/proc/%s/cmdline' % entry, 'rb').read().split(b'\0')
        except OSError:
            continue
        if uid >= 200000 or argv[:2] == [b'sleep', b'600']:
            left.append(entry)
    check('C6 no process a chat started before the restart is still running afterwards', not left, left)
    layout = [name for name in os.listdir('/chats')] if os.path.isdir('/chats') else []
    check('C6 the chats\' volume survived the restart', 'people' in layout, layout)



# ---- P1: the real model door, per-person keys, git, and what survives a restart -------------------------------------------

def stub_rows():
    code, body = http('GET', STUB + '/_calls')
    return (body or {}).get('calls', []) if code == 200 and isinstance(body, dict) else []


def calls_with(key, rows=None):
    return [r for r in (rows if rows is not None else stub_rows()) if r.get('key') == key and r.get('status') == 200]


def git_calls():
    code, body = http('GET', GITSTUB + '/_calls')
    return body if code == 200 and isinstance(body, list) else []


def spent_of(token):
    code, body = http('GET', ORG + '/v1/myspace', token)
    return round(float((body or {}).get('spent_usd', 0)), 2) if code == 200 and isinstance(body, dict) else None


def quiet_count(key, seconds=2, limit=30):
    """The number of stub calls made with `key` once no more are arriving (a chat can make a call after its answer)."""
    last = -1
    deadline = time.time() + limit
    while time.time() < deadline:
        now = len(calls_with(key))
        if now == last:
            return now
        last = now
        time.sleep(seconds)
    return last


def audit_lines(token):
    code, body = http('GET', ORG + '/v1/hosted/audit', token)
    return (body or {}).get('entries', []) if code == 200 and isinstance(body, dict) else []


def p1_state():
    try:
        return json.loads(P1_STATE.read_text())
    except (OSError, ValueError):
        return {}


def p1_save(state):
    P1_STATE.write_text(json.dumps(state))
    P1_STATE.chmod(0o600)


def hosted_p1():
    print('--- hosted chats P1: the model door, keys, spend, caps and git')
    owner, member = sign_in('owner'), sign_in('priya')
    check('P1 both people sign in', bool(owner and member))
    if not (owner and member):
        return
    ids = json.loads(HOSTED_IDS.read_text())
    owner_chat, member_chat, cap_chat = ids['owner_first'], ids['member'], ids['member_cap']
    state = {'own_key': 'sk-own-smoke-' + secrets.token_hex(10), 'member_chat': member_chat, 'cap_chat': cap_chat, 'owner_chat': owner_chat,
             'git_token': GIT_TOKEN, 'hosted_key': HOSTED_ORG_KEY}
    check('P1 the stub reports what it was asked and costs (so metering can be checked)', http('GET', STUB + '/_calls')[0] == 200 and GIT_TOKEN != '',
          f'stub {http("GET", STUB + "/_calls")[0]}, git token set {GIT_TOKEN != ""}')

    # P1-1 and P1-2: a chat's model calls go upstream with the organization's key and are metered into the person's spend to the cent.
    before_calls = quiet_count(HOSTED_ORG_KEY)
    before_spent = spent_of(member)
    for number in range(3):
        status, out, err = chat(member, 'bash: echo meter-%d' % number, session=member_chat)
        check(f'P1 the member\'s message {number + 1} gets an answer through the model door', status == 0 and 'STUB ran' in out, short(out + err, 200))
    after_calls = quiet_count(HOSTED_ORG_KEY)
    made = after_calls - before_calls
    check('P1-1 the model calls reached the upstream with the organization\'s own key', made >= 3, f'{made} calls')
    check('P1-1 no key of a chat (cmt_...) ever reached the upstream', not [r for r in stub_rows() if str(r.get('key', '')).startswith('cmt_')], '')
    spent_after = wait_until(lambda: (lambda value: value if value is not None and round(value - before_spent, 2) == round(made * STUB_COST, 2) else None)(spent_of(member)), 20, 1)
    check('P1-2 the person\'s spend rose by exactly what those calls cost, to the cent', spent_after is not None, f'{made} calls x ${STUB_COST} -> spent {before_spent} then {spent_of(member)}')

    # P1-5: a person's own keys are kept sealed, are only theirs, and are used only for them.
    code, _ = http('PUT', ORG + '/v1/hosted/keys/openrouter', member, {'key': state['own_key']})
    check('P1-5 a person can store their own model key', code == 204, code)
    code, _ = http('PUT', ORG + '/v1/hosted/keys/github', member, {'key': GIT_TOKEN})
    check('P1-5 a person can store their own GitHub access', code == 204, code)
    code, listing = http('GET', ORG + '/v1/hosted/keys', member)
    rows = (listing or {}).get('keys', []) if isinstance(listing, dict) else []
    check('P1-5 the list shows the last four characters and never the key', sorted(r.get('provider') for r in rows) == ['github', 'openrouter']
          and state['own_key'] not in json.dumps(listing) and GIT_TOKEN not in json.dumps(listing)
          and {r['provider']: r.get('last4') for r in rows}.get('openrouter') == state['own_key'][-4:], short(listing, 300))
    code, other = http('GET', ORG + '/v1/hosted/keys', owner)
    check('P1-5 another person sees none of those keys', code == 200 and (other or {}).get('keys') == [], short(other, 200))
    stored = glob.glob('/data/**/person-keys.json', recursive=True)
    body = open(stored[0], 'rb').read() if len(stored) == 1 else b''
    check('P1-5 the stored keys are on disk sealed: mode 0600 and neither key is in the file', len(stored) == 1 and (os.stat(stored[0]).st_mode & 0o777) == 0o600
          and state['own_key'].encode() not in body and GIT_TOKEN.encode() not in body and len(body) > 20, stored)

    own_before = quiet_count(state['own_key'])
    org_before = quiet_count(HOSTED_ORG_KEY)
    spent_before = spent_of(member)
    status, out, err = chat(member, 'bash: echo own-key', session=member_chat)
    own_made = quiet_count(state['own_key']) - own_before
    check('P1-5 with an own key stored, the member\'s calls go upstream with it', status == 0 and own_made >= 1, f'{own_made} calls {short(out + err, 120)}')
    check('P1-5 and none of them went with the organization\'s key', quiet_count(HOSTED_ORG_KEY) == org_before, '')
    own_spent = wait_until(lambda: (lambda value: value if value is not None and round(value - spent_before, 2) == round(own_made * STUB_COST, 2) else None)(spent_of(member)), 20, 1)
    check('P1-5 own-key calls are counted in the person\'s spend too', own_spent is not None, f'{own_made} calls -> {spent_of(member)} from {spent_before}')
    org_before = quiet_count(HOSTED_ORG_KEY)
    own_before = quiet_count(state['own_key'])
    status, out, err = chat(owner, 'bash: echo not-the-members-key', session=owner_chat)
    check('P1-5 one person\'s key is never used for another person\'s chat', status == 0 and quiet_count(state['own_key']) == own_before and quiet_count(HOSTED_ORG_KEY) > org_before,
          f'own {quiet_count(state["own_key"])} vs {own_before}')

    # P1-3: the limits refuse in plain words, and spend nothing once refused.
    code, _ = http('DELETE', ORG + '/v1/hosted/keys/openrouter', member)
    check('P1-5 a person can remove their own model key', code == 204, code)
    code, limit = http('PUT', ORG + '/v1/myspace/limit', owner, {'limit_usd': 0})
    check('P1-3 the operator can set the monthly limit', code == 200, f'{code} {short(limit, 100)}')
    before_refused = len(stub_rows())
    status, out, err = chat(member, 'bash: echo over-the-month', session=member_chat)
    words = out + err
    check('P1-3 at the monthly limit the chat is refused in a plain sentence', 'monthly limit of $0.00' in words and 'paused' in words, short(words, 300))
    check('P1-3 and the refused message made no call upstream', len(stub_rows()) == before_refused, f'{len(stub_rows()) - before_refused} calls')
    code, _ = http('PUT', ORG + '/v1/myspace/limit', owner, {'limit_usd': 20})
    status, out, err = chat(member, 'bash: echo back-in-the-month', session=member_chat)
    check('P1-3 raising the limit lets the chat carry on', status == 0 and 'STUB ran' in out, short(out + err, 200))

    refused_with = ''
    sent = 0
    for sent in range(1, 60):
        status, out, err = chat(member, 'bash: echo fill-%d' % sent, session=cap_chat)
        if 'spending limit' in out + err:
            refused_with = out + err
            break
    check('P1-3 a chat that spends up to its cap is refused in a plain sentence', 'spending limit of $%.2f' % SESSION_CAP in refused_with and 'Start a new chat' in refused_with,
          f'after {sent} messages: {short(refused_with, 300)}')
    cap_calls = quiet_count(HOSTED_ORG_KEY)
    status, out, err = chat(member, 'bash: echo again', session=cap_chat)
    check('P1-3 a message after the refusal is refused again and makes no call upstream', 'spending limit' in out + err and quiet_count(HOSTED_ORG_KEY) == cap_calls, short(out + err, 200))
    state['cap_spent_after'] = spent_of(member)

    # P1-6 and P1-9: git goes through the organization and the GitHub access never reaches the chat.
    before = len(git_calls())
    status, out, err = chat(owner, 'bash: git clone -q http://gitstub:8891/acme/none.git gn 2>&1; echo "GITNOKEY $?"', session=owner_chat)
    check('P1-6 a person with no stored GitHub access is refused, and nothing reached the git host', field(out, 'GITNOKEY') not in (None, '0') and len(git_calls()) == before,
          short(out + err, 200))
    script = ('git clone -q http://gitstub:8891/acme/widgets.git gw 2>&1 && cd gw && echo hello > hello.txt && git add hello.txt && '
              'git -c user.name=smoke -c user.email=smoke@example.invalid commit -q -m first && git push -q origin HEAD:main 2>&1; echo "GIT $?"')
    status, out, err = chat(member, 'bash: ' + script, session=member_chat)
    check('P1-6 a chat clones and pushes through the organization with the person\'s own access', field(out, 'GIT') == '0', short(out + err, 300))
    calls = [c for c in git_calls() if '/acme/widgets.git/' in c.get('path', '')]
    check('P1-6 the git host saw the person\'s access, injected by the organization', len(calls) >= 3 and all(c.get('password_ok') and c.get('user') == 'x-access-token' for c in calls)
          and {c.get('service') for c in calls} >= {'git-upload-pack', 'git-receive-pack'}, short(calls, 300))
    lines = [e for e in audit_lines(member) if 'widgets' in e.get('repo', '')]
    refs = [c for c in calls if c.get('path', '').endswith('/info/refs')]
    check('P1-6 each git operation left one audit line, naming the chat and the repository', len(lines) == len(refs) >= 2 and all(e.get('chat') == member_chat and e.get('kind') == 'git' for e in lines),
          f'{len(lines)} lines for {len(refs)} operations')
    check('P1-6 the audit shows a person only their own lines', not [e for e in audit_lines(owner) if 'widgets' in e.get('repo', '')], '')
    state['audit_lines'] = len(audit_lines(member))
    state['member_spent'] = spent_of(member)
    p1_save(state)


def hosted_p1_host():
    """Run in the sessionhost container as root, with hosted_p1's state on stdin: what only the machine can show. Nothing a person
    stored, and no key of the organization, is anywhere a chat can read: not in a folder, not in a process's environment or
    arguments. The chat's own access is kept to the engine and to the one private file git's helper reads."""
    print('--- hosted chats P1: what is on the chats\' machine')
    state = json.load(sys.stdin)
    needles = {'the person\'s own model key': state['own_key'], 'the person\'s GitHub access': state['git_token'], 'the organization\'s model key': state['hosted_key']}
    people = glob.glob('/chats/people/*/hs_*')
    check('P1 the chats\' machine has chat folders to read (so the scans mean something)', len(people) >= 3, len(people))

    def scan(folder):
        def work():
            hits, files = [], 0
            for root, _, names in os.walk(folder):
                for name in names:
                    path = os.path.join(root, name)
                    try:
                        if os.path.getsize(path) > 50_000_000:
                            continue
                        data = open(path, 'rb').read()
                        files += 1
                        for label, needle in needles.items():
                            if needle.encode() in data:
                                hits.append((label, path))
                    except OSError:
                        pass
            return {'files': files, 'hits': hits}
        return work
    hits, files = [], 0
    for folder in people:
        info = os.stat(folder)
        found = as_user(info.st_uid, info.st_gid, scan(folder)) or {'files': 0, 'hits': [('could not read', folder)]}
        files += found['files']
        hits += found['hits']
    check('P1 neither key and no GitHub access is in any chat\'s folder, transcripts included', files > 0 and not hits, hits[:3] or f'{files} files read')

    # Every chat's own access is held where git's helper reads it (a private file) and in its engine's environment, and nowhere else.
    def read_broker(folder):
        def work():
            for line in open(os.path.join(folder, 'home', '.config', 'codeaf-hosted', 'broker')).read().splitlines():
                if line.startswith('bearer='):
                    return line[len('bearer='):]
            return ''
        return work
    bearers = {}
    for folder in people:
        info = os.stat(folder)
        found = as_user(info.st_uid, info.st_gid, read_broker(folder))
        if found:
            bearers[folder] = found
    leaks = []
    for entry in os.listdir('/proc'):
        if not entry.isdigit():
            continue
        try:
            environ = open('/proc/%s/environ' % entry, 'rb').read()
            cmdline = open('/proc/%s/cmdline' % entry, 'rb').read()
        except OSError:
            continue
        for label, needle in needles.items():
            if needle.encode() in environ or needle.encode() in cmdline:
                leaks.append((label, entry, cmdline.split(b'\0')[0][-30:].decode('utf-8', 'replace')))
        for token in bearers.values():
            if token.encode() in cmdline:
                leaks.append(('a chat\'s access', entry, 'arguments'))
    check('P1 none of those is in any process\'s environment or arguments on the machine', not leaks, leaks[:3])
    check('P1-6 git is set up in the chats that ran (each holds a chat access in the private file its helper reads)', len(bearers) >= 2 and all(t.startswith('cmt_') for t in bearers.values()),
          len(bearers))
    crossed = []
    for folder, token in bearers.items():
        for other in people:
            if other == folder:
                continue
            info = os.stat(other)
            hit = as_user(info.st_uid, info.st_gid, lambda f=other, t=token: [os.path.join(p, n) for p, _, names in os.walk(f) for n in names
                                                                              if t.encode() in open(os.path.join(p, n), 'rb').read()])
            crossed += hit or []
    check('P1 one chat\'s access is not in any other chat\'s folder', not crossed, crossed[:2])
    mine = [token for folder, token in bearers.items() if folder.endswith(state['member_chat'])]
    if mine:
        Path('/tmp/smoke-p1-access').write_text(mine[0])
        os.chmod('/tmp/smoke-p1-access', 0o600)
    brokers = glob.glob('/chats/people/*/hs_*/home/.config/codeaf-hosted/broker')
    check('P1-6 the file git\'s helper reads is closed to everyone but the chat', brokers and all((os.stat(b).st_mode & 0o777) == 0o600 for b in brokers),
          [oct(os.stat(b).st_mode & 0o777) for b in brokers])


def model_door(token, verb='GET', path='/v1/model/models'):
    return http(verb, ORG + path, token)


def hosted_p1_bearer():
    """Run in the sessionhost container as root after the organization server alone was restarted, with the state file on stdin (it
    holds a fresh sign-in): the chat access a running chat held before the restart still works, and stops working the moment
    its chat is stopped."""
    print('--- hosted chats P1: the chat access across a restart of the organization server')
    state = json.load(sys.stdin)
    path = Path('/tmp/smoke-p1-access')
    token = path.read_text() if path.exists() else ''
    check('P1-4 the chat access read before the restart is still here to try', token.startswith('cmt_'), 'read' if token else 'missing')
    if not token:
        return
    code, body = model_door(token)
    check('P1-4 after the organization server restarted, a running chat\'s access still works', code == 200, f'{code} {short(body, 120)}')
    code, stopped = http('POST', ORG + '/v1/hosted/sessions/' + state['member_chat'] + '/stop', state['restart_token'], {})
    check('P1-4 stopping a chat is accepted', code in (200, 202, 204), f'{code}')
    code, body = model_door(token)
    check('P1-4 and its access stops working at once, in a plain sentence', code == 401 and 'model access has ended' in json.dumps(body), f'{code} {short(body, 160)}')
    path.unlink()
    check('P1-4 nothing of the access is left behind on the machine', not path.exists())


def hosted_p1_restart():
    """Run in the codeaf container after the organization server restarted: signs in again and says which chat the machine side
    should stop, in the state file the next step reads."""
    print('--- hosted chats P1: after the organization server restarted')
    member = sign_in('priya')
    state = p1_state()
    check('P1-4 people can sign in again and the keys they stored are still there', bool(member) and [r['provider'] for r in (http('GET', ORG + '/v1/hosted/keys', member)[1] or {}).get('keys', [])] == ['github'],
          short(http('GET', ORG + '/v1/hosted/keys', member)[1], 200) if member else '')
    state['restart_token'] = member
    p1_save(state)


def hosted_p1_after():
    print('--- hosted chats P1: after everything restarted')
    member = sign_in('priya')
    state = p1_state()
    check('P1-4 the person\'s stored GitHub access survived the restart', bool(member) and [r['provider'] for r in (http('GET', ORG + '/v1/hosted/keys', member)[1] or {}).get('keys', [])] == ['github'], '')
    check('P1-2 the person\'s spend survived the restart', spent_of(member) is not None and spent_of(member) >= (state.get('member_spent') or 0), f'{spent_of(member)} vs {state.get("member_spent")}')
    status, out, err = chat(member, 'bash: echo after', session=state['cap_chat'])
    check('P1-3 a chat that reached its cap is still refused after the restart (its spend was kept)', 'spending limit' in out + err, short(out + err, 200))
    lines = audit_lines(member)
    check('P1-6 the audit lines survived the restart', len(lines) >= state.get('audit_lines', 1) >= 2, f'{len(lines)} vs {state.get("audit_lines")}')
    status, out, err = chat(member, 'bash: cd gw && echo more >> hello.txt && git add hello.txt && git -c user.name=smoke -c user.email=smoke@example.invalid commit -q -m second && git push -q origin HEAD:main 2>&1; echo "GIT $?"',
                            session=state['member_chat'])
    check('P1-6 git still works through the organization after the restart', field(out, 'GIT') == '0', short(out + err, 300))
    check('P1-6 and the new operation left an audit line', len(audit_lines(member)) > len(lines), f'{len(audit_lines(member))} vs {len(lines)}')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('section', choices=['all', 'control', 'job', 'org', 'after', 'after-org', 'hosted-prepare', 'hosted-seed', 'hosted', 'hosted-host', 'hosted-after', 'hosted-leftovers',
                                            'hosted-p1', 'hosted-p1-host', 'hosted-p1-restart', 'hosted-p1-bearer', 'hosted-p1-after'])
    parser.add_argument('--operator-link', default='')
    args = parser.parse_args()
    for name, run in (('control', control), ('job', job), ('org', lambda: org(args.operator_link)), ('after', after), ('after-org', after_org),
                      ('hosted-prepare', hosted_prepare), ('hosted-seed', hosted_seed), ('hosted', hosted), ('hosted-host', hosted_host), ('hosted-after', hosted_after), ('hosted-leftovers', hosted_leftovers),
                      ('hosted-p1', hosted_p1), ('hosted-p1-host', hosted_p1_host), ('hosted-p1-restart', hosted_p1_restart), ('hosted-p1-bearer', hosted_p1_bearer),
                      ('hosted-p1-after', hosted_p1_after)):
        if args.section == name or (args.section == 'all' and name in ('control', 'job')):
            run()
    print('RESULT ' + ('PASS' if failures == 0 else f'FAIL ({failures} check(s) failed)'))
    sys.exit(1 if failures else 0)


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""The organization's factory runner.

`codeaf factory serve` (the organization server) keeps people, workstreams and chats, but it does
not run the factory's AgentField node: that is the engine's job (`codeaf app`), and the engine
takes its instructions over a stdio wire. This script is that engine's host for a container:

  1. waits for the AgentField control plane,
  2. starts `codeaf app`,
  3. starts the factory on this machine, hands it the control plane's address and key
     (`factory.agentfield_connect`; the factory registers as the node `codeaf-factory` over
     the node link and sends the run-owner proof AgentField's strict mode asks for),
  4. starts the factory's runner (`factory.local_run`),
  5. serves the engine's wire on a private unix socket (/data/engine.sock) so an operator can
     drive it with `docker compose exec runner python3 /opt/org/rpc.py <method> '<json>'`.

No secret is printed, and none is written anywhere but the engine's own home in the data volume.
"""
import json
import os
import queue
import signal
import socketserver
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path

DATA = Path(os.environ.get('RUNNER_DATA', '/data'))
HOME = Path(os.environ.get('CODEAF_HOME', str(DATA / 'codeaf')))
CALLER = os.environ.get('RUNNER_CALLER', 'org-runner')
SOCKET = DATA / 'engine.sock'
READY = DATA / 'ready.json'


def env_or_file(name):
    """A value from NAME, or from the file NAME_FILE names (a mounted secret)."""
    value = os.environ.get(name, '').strip()
    path = os.environ.get(name + '_FILE', '').strip()
    if not value and path and os.path.isfile(path):
        value = Path(path).read_text().strip()
    return value


def log(message):
    print(time.strftime('%H:%M:%S'), message, flush=True)


def wait_for_control_plane(url, key, seconds=300):
    deadline = time.time() + seconds
    last = None
    while time.time() < deadline:
        try:
            request = urllib.request.Request(url.rstrip('/') + '/api/v1/health')
            if key:
                request.add_header('Authorization', 'Bearer ' + key)
            with urllib.request.urlopen(request, timeout=5):
                return
        except Exception as error:  # noqa: BLE001 - any failure means "not yet"
            last = error
            time.sleep(2)
    raise SystemExit(f'AgentField at {url} did not answer within {seconds} seconds ({last})')


def write_engine_config(model):
    """Point the engine at the organization's model. Keys never go in this file."""
    HOME.mkdir(parents=True, exist_ok=True)
    path = HOME / 'config.json'
    config = {}
    if path.exists():
        try:
            config = json.loads(path.read_text())
        except ValueError:
            config = {}
    if model:
        for key in ('model.talk', 'task.model', 'vision_model', 'models.tiers.low', 'models.tiers.reflex',
                    'models.tiers.worker', 'models.tiers.high', 'models.tiers.mastermind'):
            config[key] = model
    if os.environ.get('CODEAF_BASE_URL'):
        # A custom endpoint (the keyless stub, a gateway): no fallbacks, no model pool, nothing to ask.
        config.update({'models.fallbacks': '', 'memory.enabled': 'off', 'tools.approvalMode': 'allow', 'routing': 'off'})
    path.write_text(json.dumps(config))
    path.chmod(0o600)


def selftest():
    """RUNNER_SELFTEST=1: where nobody can `docker exec` (a platform's service), the runner proves the
    stack itself once it is up. RUNNER_SELFTEST=1 runs the smoke test's control and job sections against
    the scripted stub; RUNNER_SELFTEST=after runs its after-a-restart section instead. Each PASS or FAIL
    line is logged; it spends nothing and prints no secret."""
    sections = ('after',) if os.environ.get('RUNNER_SELFTEST', '').strip().lower() == 'after' else ('control', 'job')
    for section in sections:
        proc = subprocess.run(['python3', '/opt/org/smoke.py', section], capture_output=True, text=True, timeout=1800, check=False)
        for line in (proc.stdout + proc.stderr).splitlines():
            log('selftest ' + line)


def main():
    url = os.environ.get('AGENTFIELD_URL', '').strip()
    key = env_or_file('AGENTFIELD_API_KEY')
    if not url:
        raise SystemExit('AGENTFIELD_URL is required (for example http://agentfield:8080)')
    DATA.mkdir(parents=True, exist_ok=True)
    READY.unlink(missing_ok=True)
    wait_for_control_plane(url, key)
    write_engine_config(os.environ.get('ORG_MODEL', '').strip())

    env = dict(os.environ)
    for name in ('OPENROUTER_API_KEY', 'GH_TOKEN'):
        value = env_or_file(name)
        if value:
            env[name] = value
    if os.environ.get('CODEAF_BASE_URL'):
        env['CODEAF_MODEL_POOL'] = 'off'
    proc = subprocess.Popen(['codeaf', 'app', '--home', str(HOME)], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            text=True, bufsize=1, env=env)

    pending, lock, wlock = {}, threading.Lock(), threading.Lock()
    counter = [0]
    subscribers, slock = set(), threading.Lock()
    hello = [None]

    def reader():
        for line in proc.stdout:
            try:
                message = json.loads(line)
            except ValueError:
                continue
            if 'event' in message:
                if message['event'] == 'hello':
                    hello[0] = message
                with slock:
                    targets = list(subscribers)
                for target in targets:
                    try:
                        target.send(message)
                    except OSError:
                        pass
                continue
            with lock:
                waiting = pending.get(message.get('id'))
            if waiting is not None:
                waiting.put(message)
    threading.Thread(target=reader, daemon=True).start()

    def call(method, params=None, timeout=300):
        with lock:
            counter[0] += 1
            number = counter[0]
            answer = queue.Queue(maxsize=1)
            pending[number] = answer
        try:
            with wlock:
                proc.stdin.write(json.dumps({'id': number, 'method': method, 'params': params or {}}) + '\n')
                proc.stdin.flush()
            message = answer.get(timeout=timeout)
        finally:
            with lock:
                pending.pop(number, None)
        if message.get('error'):
            # Never echo params: a connect request carries the control plane's key.
            raise RuntimeError(method + ': ' + str(message['error'].get('message', 'the engine refused the request')))
        return message.get('result')

    def shutdown(*_):
        READY.unlink(missing_ok=True)
        SOCKET.unlink(missing_ok=True)
        proc.terminate()
        try:
            proc.wait(timeout=25)
        except subprocess.TimeoutExpired:
            proc.kill()
        raise SystemExit(0)
    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)

    class Wire(socketserver.StreamRequestHandler):
        def setup(self):
            super().setup()
            self.out = threading.Lock()

        def send(self, message):
            with self.out:
                self.wfile.write((json.dumps(message) + '\n').encode())
                self.wfile.flush()

        def handle(self):
            for line in self.rfile:
                request = {}
                try:
                    request = json.loads(line)
                    reply = {'result': call(request['method'], request.get('params', {}))}
                except Exception as error:  # noqa: BLE001
                    reply = {'error': {'code': 'runner', 'message': str(error)}}
                if 'id' in request:
                    reply['id'] = request['id']
                self.send(reply)

    try:
        for provider, name in (('openrouter', 'OPENROUTER_API_KEY'), ('github', 'GH_TOKEN')):
            if env.get(name):
                call('keys.set', {'provider': provider, 'key': env[name]})
        local = call('factory.local_start', {'caller': CALLER})
        factory = local['factory']
        factory_id = factory['id'] if isinstance(factory, dict) else factory
        model = os.environ.get('ORG_MODEL', '').strip()
        if model:
            call('factory.spending_set', {'factory': factory_id, 'default_model': model})
        call('factory.agentfield_connect', {'factory': factory_id, 'url': url, 'api_key': key})
        call('factory.local_run', {'caller': CALLER})
        SOCKET.unlink(missing_ok=True)
        server = socketserver.ThreadingUnixStreamServer(str(SOCKET), Wire)
        server.daemon_threads = True
        SOCKET.chmod(0o600)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        READY.write_text(json.dumps({'factory': factory_id}))
        log(f'runner ready: factory {factory_id} is connected to AgentField at {url}')
        if os.environ.get('RUNNER_SELFTEST', '').strip().lower() in ('1', 'true', 'yes', 'on', 'after'):
            threading.Thread(target=selftest, daemon=True).start()
        status = proc.wait()
        raise SystemExit(status or 1)
    except SystemExit:
        raise
    except Exception as error:  # noqa: BLE001
        log(f'runner failed to start: {error}')
        shutdown()


if __name__ == '__main__':
    main()

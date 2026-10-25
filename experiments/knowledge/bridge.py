"""Embedded binary bridge: local capture import and one bounded incremental pass.

No global curiosity/maintenance/reconciliation model pass runs on each chat turn.
Provider model is pinned in provider.py. All source text remains untrusted data.
"""
import argparse
import fcntl
import json
import hashlib
import os
from pathlib import Path
from workspace import Workspace
from workflow import extend
from provider import OpenRouter, MODEL
from topics import reconcile, publish
from storage import save, viewer, write_json


def compile_workspace(root, project, *, force=False, model=None):
    root = Path(root)
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (root / '.bridge.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return {'busy': True}
        partition = {'owner': 'local-profile', 'project': project}
        work = Workspace(root / 'capture', partition)
        status_file = root / 'status.json'
        def status(error='', *, needs_update=True):
            with work.connect() as db:
                pending = db.execute("SELECT count(*) FROM events WHERE partition=? AND status IN ('pending','revisit')", (work.key,)).fetchone()[0]
            cost = 0.0
            for receipt in (root / 'calls').glob('*.json'):
                data = json.loads(receipt.read_text())
                cost += (data.get('usage') or {}).get('cost', 0) or 0
            result = {'pending': pending, 'cost_usd': cost,
                      'model': MODEL, 'last_error': error, 'needs_update': needs_update}
            write_json(status_file, result)
            return result
        try:
            for path in sorted((root / 'queue').glob('*.json')):
                source = json.loads(path.read_text())
                kind = source.pop('kind')
                work.push(source, kind=kind, recorded_at=source['ts'],
                          locator={'session': source['session'], 'message': source['id']})
                # SQLite commit precedes archive move. A crash here replays the same
                # immutable event, without duplicate publication or lost content.
                name = hashlib.sha256(source['id'].encode()).hexdigest() + '.json'
                archived = root / 'archive' / name[:2] / name
                archived.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                if archived.exists():
                    if json.loads(archived.read_text()) != {**source, 'kind': kind}:
                        raise ValueError('Immutable archived source changed')
                    path.unlink()
                else:
                    path.replace(archived)
            status()
            selected_model = model or OpenRouter(root / 'calls', reasoning_effort='none', max_tokens=8000)
            result = work.flush(selected_model, force=force)
            vault = result['vault']
            projection_file = root / 'projection.json'
            from knowledge import identity
            input_hash = identity(vault)
            previous_projection = json.loads(projection_file.read_text()) if projection_file.exists() else {}
            if previous_projection.get('input_hash') == input_hash and (root / 'knowledge' / 'vault.json').exists():
                return status(needs_update=False)
            state_file = root / 'learning.json'
            learning = json.loads(state_file.read_text()) if state_file.exists() else {'subjects': []}
            # Recover the projection even when extraction previously committed before an interruption.
            current = extend(vault, learning, selected_model)
            registry_file = root / 'topics.json'
            old_registry = json.loads(registry_file.read_text()) if registry_file.exists() else None
            registry = reconcile(current, old_registry)
            projected = publish(vault, current, registry)
            save(projected, root / 'knowledge')
            write_json(state_file, current)
            write_json(registry_file, registry)
            report = status()
            viewer({'reconciled': projected}, report,
                   {'architecture': 'Project-scoped incremental knowledge; saved memory remains separate.'}, root / 'viewer')
            write_json(projection_file, {'input_hash': input_hash})
            return status(needs_update=False)
        except Exception as error:
            # No credentials/source text in the visible status; provider receipts remain private.
            status(type(error).__name__ + ': knowledge compilation failed; pending sources are retained')
            raise


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', required=True)
    parser.add_argument('--project', required=True)
    parser.add_argument('--force', action='store_true')
    args = parser.parse_args()
    try:
        print(json.dumps(compile_workspace(args.root, args.project, force=args.force)))
    except Exception as error:
        # Provider exceptions may contain untrusted response content. Keep CLI errors bounded.
        raise SystemExit(type(error).__name__ + ': knowledge compilation failed; inspect private status and receipts')


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""Summarize the paired pilot without treating a timeout or unknown bill as a pass."""
import argparse
import json
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'conversation' / 'lib'))
from receipts import read_guard_usage


def summarize(root):
    manifest = json.loads((root / 'manifest.json').read_text())
    result = []
    for arm in manifest['order']:
        cell = root / ('score-' + arm)
        door_path, reward_path = cell / 'door.json', cell / 'reward.json'
        door = json.loads(door_path.read_text()) if door_path.exists() else None
        reward = json.loads(reward_path.read_text()) if reward_path.exists() else None
        usage = read_guard_usage(str(cell / 'guard-usage.jsonl'))
        finished = door is not None and (reward is not None or (cell / 'grader-error.txt').exists())
        passed = bool(finished and door['ended'] == 'idle' and reward and reward.get('reward') == 1)
        result.append({
            'arm': arm, 'finished': finished, 'success': passed,
            'door': door, 'quality': reward, 'usage': usage,
            'allowed_models': bool(usage and usage['models'] == [manifest['model']]),
            'billing_complete': bool(usage and usage['cost_source'] == 'guard-upstream'),
            'infrastructure_error': (cell / 'grader-error.txt').exists(),
        })
    return {'manifest': manifest, 'results': result,
            'limitation': 'One paired complex-work pilot; not a ranking or quality-equivalence estimate.'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--out', type=Path)
    args = parser.parse_args()
    summary = json.dumps(summarize(args.directory), indent=2) + '\n'
    if args.out:
        args.out.write_text(summary)
    else:
        print(summary, end='')

"""Exercise real scheduler and usage-watch processes with no provider calls."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

from run_campaign import validate_blocks

HERE = Path(__file__).resolve().parent


class ExecutionTests(unittest.TestCase):
    def test_empty_campaign_is_not_a_successful_benchmark(self):
        with self.assertRaises(ValueError):
            validate_blocks({"blocks": [], "arms": {}, "cells": []})

    def test_scheduler_limits_concurrency_keeps_barrier_and_retains_failure(self):
        with tempfile.TemporaryDirectory(prefix="afholdout-scheduler-") as directory:
            root = Path(directory)
            shutil.copy2(HERE / "run_campaign.py", root / "run_campaign.py")
            (root / "run_cell.py").write_text('''import json,sys,time
from pathlib import Path
root=Path(sys.argv[1]);arm,task,seed=sys.argv[2:]
out=root/arm/'cells'/f'{task}-seed{seed}';out.mkdir(parents=True)
started=time.monotonic();time.sleep(.1)
(out/'interval.json').write_text(json.dumps([started,time.monotonic()]))
(out/'result.json').write_text(json.dumps({'status':'infrastructure_error' if task=='broken' else 'completed','grade':{'status':'failed'}}))
''')
            arms = dict.fromkeys(["base", "candidate", "pi", "mini"], {})
            blocks = [[[arm, task, 1] for arm in arms] for task in ["first", "broken", "never"]]
            plan = {"arms": arms, "blocks": blocks, "cells": sum(blocks, []),
                    "preflight_passed": True, "input_hashes": {}}
            (root / "scoring-plan.json").write_text(json.dumps(plan))
            command = [sys.executable, str(root / "run_campaign.py"), str(root)]
            result = subprocess.run(command, capture_output=True, timeout=15)
            self.assertNotEqual(result.returncode, 0)
            rows = json.loads((root / "trial-results.json").read_text())
            self.assertEqual(len(rows), 8)
            self.assertTrue((root / "RUN-NEEDS-REVIEW.json").exists())
            intervals = {}
            for task in ["first", "broken"]:
                intervals[task] = [json.loads((root / arm / "cells" / (task + "-seed1") / "interval.json").read_text()) for arm in arms]
            self.assertLessEqual(max(end for _, end in intervals["first"]), min(start for start, _ in intervals["broken"]))
            events = [(start, 1) for pairs in intervals.values() for start, _ in pairs]
            events += [(end, -1) for pairs in intervals.values() for _, end in pairs]
            active = maximum = 0
            for _, change in sorted(events):
                active += change
                maximum = max(maximum, active)
            self.assertEqual(maximum, 2)
            self.assertFalse(any(root.glob("*/cells/never-seed1")))
            before = (root / "trial-results.json").read_bytes()
            rerun = subprocess.run(command, capture_output=True, timeout=15)
            self.assertNotEqual(rerun.returncode, 0)
            self.assertEqual(before, (root / "trial-results.json").read_bytes())

    def test_native_cost_watch_deduplicates_and_stops_only_its_trial(self):
        with tempfile.TemporaryDirectory(prefix="afholdout-usage-") as directory:
            root = Path(directory)
            runner = root / "runner"
            runner.mkdir()
            entry = runner / "mini_entry.py"
            entry.write_text("import time; time.sleep(60)\n")
            cell = root / "mini/cells/test-seed1"
            cell.mkdir(parents=True)
            other = root / "mini/cells/other-seed1"
            other.mkdir()
            processes = [subprocess.Popen([sys.executable, str(entry), str(path)], start_new_session=True) for path in [cell, other]]
            watcher = None
            try:
                response = {"id": "fixture-generation", "model": "fixture", "usage": {"cost": 5.25}}
                message = {"extra": {"response": response}}
                (cell / "trajectory.json").write_text(json.dumps({"messages": [message, message]}))
                watcher = subprocess.Popen([sys.executable, str(HERE / "native_usage_watch.py"), str(cell)], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                self.assertLess(processes[0].wait(timeout=10), 0)
                self.assertIsNone(processes[1].poll())
                (cell / "result.json").write_text("{}")
                self.assertEqual(watcher.wait(timeout=10), 0)
                receipt = json.loads((root / "mini-usage/test-seed1.json").read_text())
                self.assertEqual(receipt["known_cost_usd"], 5.25)
                self.assertEqual(len(receipt["generations"]), 1)
                self.assertTrue((root / "mini-usage/test-seed1-cost-stop.json").exists())
            finally:
                for process in processes + ([watcher] if watcher is not None else []):
                    if process.poll() is None:
                        process.kill()
                    process.wait()


if __name__ == "__main__":
    unittest.main()

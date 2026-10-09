import { useState } from 'react';
import type { EngineTaskRow } from '../../chat/engine-client';
import { TasksTable } from '../tasks/TasksTable';

const NOW = Date.parse('2026-10-09T10:05:00Z');
const at = (secondsAgo: number) => new Date(NOW - secondsAgo * 1000).toISOString();
const MODEL = 'deepseek/deepseek-v4.1-flash';

const rows: EngineTaskRow[] = [
  { ID: 'g1', Title: 'Ship trailing-comma support', Status: 'running', Started: at(240) },
  { ID: 'a1', Title: 'Update fixtures', Status: 'running', Parent: 'g1', Started: at(120), Model: MODEL, Steps: 7, USD: 0.06, Live: { Step: 7, Command: 'go test ./internal/parse/...', Since: at(8) } },
  { ID: 'a2', Title: 'Port fix to v1 branch', Status: 'paused', Parent: 'g1', Started: at(120), Model: MODEL, Steps: 12, USD: 0.14 },
  { ID: 'a3', Title: 'Decide strict-mode default', Status: 'paused', Parent: 'g1', Started: at(20) },
  { ID: 'a4', Title: 'Write changelog entry', Status: 'pending', Parent: 'g1', Waits: ['a1'] },
  { ID: 'g2', Title: 'Audit config loaders', Status: 'running', Started: at(180) },
  { ID: 'b1', Title: 'Scan env overrides', Status: 'running', Parent: 'g2', Started: at(60), Model: MODEL, Steps: 5, USD: 0.03, Live: { Step: 5, Command: 'rg -n "os.Getenv" ./...', Since: at(4) } },
  { ID: 'b2', Title: 'Migrate old fixtures', Status: 'failed', Parent: 'g2', Started: at(180), Ended: at(0), Model: MODEL, Steps: 8, USD: 0.09 },
  { ID: 'b3', Title: 'Rewrite YAML fixtures', Status: 'pending', Parent: 'b2', Waits: ['b2'] },
  { ID: 'b4', Title: 'List the loaders', Status: 'done', Parent: 'g2', Started: at(170), Ended: at(150) },
  { ID: 'g3', Title: 'Audit error messages', Status: 'running', Paused: true, Started: at(30) },
];

const reasons = { a2: 'Allow 3 git actions?', a3: 'Keep strict? Picks Suggested in 12s' };

/** Fixture rows with the engine's optional figures present; a live table drops whatever the engine omits. */
export function TasksTableSpecimen() {
  const [selected, setSelected] = useState('a2');
  return (
    <div className="specimen-tasks-table">
      <TasksTable tasks={rows} now={NOW} selectedId={selected} onSelect={setSelected} onClose={() => undefined} reasons={reasons} />
    </div>
  );
}

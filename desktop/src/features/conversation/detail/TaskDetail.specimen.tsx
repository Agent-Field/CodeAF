import type { EngineQuestion, EngineTaskRow } from '../../chat/engine-client';
import { SPECIMEN_NOW } from '../specimens/TaskPanel.specimen';
import { TaskDetailPane } from './TaskDetailPane';

const at = (secondsAgo: number) => new Date(SPECIMEN_NOW - secondsAgo * 1000).toISOString();

const waiting: EngineTaskRow = {
  ID: 't-9',
  Title: 'Port fix to v1 branch',
  Status: 'paused',
  Parent: 't-1',
  Steps: 12,
  Started: at(130),
  Model: 'deepseek/deepseek-v4.1-flash',
  USD: 0.14,
  Tokens: 41000,
};

const bare: EngineTaskRow = { ID: 't-10', Title: 'Write changelog entry', Status: 'pending', Parent: 't-1' };

const question: EngineQuestion = {
  id: 1,
  kind: 'consent',
  ask: 'permission',
  head: 'Allow 3 git actions?',
  reason: 'git checkout release/v1, git cherry-pick 3f2a1c, git push origin release/v1',
  options: [
    { key: '1', label: 'allow once' },
    { key: '3', label: 'deny', safe: true },
  ],
  stakes: 'costly',
  asker: { kind: 'task', name: 'Port fix to v1 branch' },
  blocking: { turn: false, tasks: ['t-9'] },
};

/** The pane with everything the engine can say, then with almost nothing. */
export function TaskDetailSpecimen() {
  const open = () => undefined;
  return (
    <div className="specimen-task-detail">
      <TaskDetailPane
        row={waiting}
        parentTitle="Ship trailing-comma support"
        instructions="Cherry-pick the lexer fix onto release/v1 and run the parser suite before pushing."
        question={{ question, busy: false, held: false, onAnswer: () => Promise.resolve(true), onHold: open }}
        now={SPECIMEN_NOW}
        onOpenTask={open}
      />
      <TaskDetailPane row={bare} now={SPECIMEN_NOW} onOpenTask={open} />
    </div>
  );
}

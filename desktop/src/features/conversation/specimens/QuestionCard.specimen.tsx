import type { EngineQuestion } from '../../chat/engine-client';
import { QuestionCard } from '../QuestionCard';

const approval: EngineQuestion = {
  id: 1,
  kind: 'approval',
  head: 'Run a shell command',
  ask: 'May I run `go test ./...` in the project folder?',
  reason: 'The tests tell me whether the change is safe to keep.',
  options: [
    { key: 'allow', label: 'Allow once', safe: true, consequence: 'Runs this command only.' },
    { key: 'deny', label: 'Not now', body: 'I will continue without running it.' },
  ],
};

const freeText: EngineQuestion = {
  id: 2,
  kind: 'question',
  head: 'One detail',
  ask: 'Which folder should the report go in?',
  input: { kind: 'text', prompt: 'Folder name' },
};

const checklist: EngineQuestion = {
  id: 3,
  kind: 'question',
  head: 'Pick the folders',
  ask: 'Which folders should the audit cover?',
  input: { kind: 'checklist' },
  options: [
    { key: 'src', label: 'src' },
    { key: 'docs', label: 'docs' },
    { key: 'tests', label: 'tests' },
  ],
};

const blanks: EngineQuestion = {
  id: 4,
  kind: 'consent',
  head: 'Connect the database',
  ask: 'Fill in the connection details.',
  input: {
    kind: 'blanks',
    blanks: [
      { label: 'Host', default: 'localhost' },
      { label: 'Mode', kind: 'choice', choices: ['read', 'write'], default: 'read' },
    ],
  },
  attach: [{ kind: 'diff', title: 'Planned change', body: '- timeout = 5\n+ timeout = 30' }],
  scope: ['once', 'session'],
};

const settle = () => Promise.resolve(true);

export function QuestionCardSpecimen() {
  return (
    <div>
      <QuestionCard question={approval} busy={false} onAnswer={settle} />
      <QuestionCard question={freeText} busy={false} onAnswer={settle} />
      <QuestionCard question={checklist} busy={false} onAnswer={settle} />
      <QuestionCard question={blanks} busy={false} onAnswer={settle} />
    </div>
  );
}

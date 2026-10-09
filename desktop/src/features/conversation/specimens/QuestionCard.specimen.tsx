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

const settle = () => Promise.resolve(true);

export function QuestionCardSpecimen() {
  return (
    <div>
      <QuestionCard question={approval} busy={false} onAnswer={settle} />
      <QuestionCard question={freeText} busy={false} onAnswer={settle} />
    </div>
  );
}
